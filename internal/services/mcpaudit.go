package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// MCP 审计日志服务:所有 MCP 操作(工具调用、审批、拦截、挂起/恢复)均落盘
// 持久化为按日轮转的 JSONL 文件,同时保留内存环形缓冲供设置面板实时展示。

// McpAuditEntry 单条审计记录。
type McpAuditEntry struct {
	ID       string `json:"id"`       // 条目唯一 ID(a-<seq>)
	TS       string `json:"ts"`       // ISO 时间戳
	Level    string `json:"level"`    // info / confirm / blocked / error / system
	Action   string `json:"action"`   // 工具名或动作名
	Subject  string `json:"subject"`  // 操作对象(tabId / 会话路径 / 文件路径)
	Detail   string `json:"detail"`   // 内容预览(截断,不含完整敏感内容)
	Risk     string `json:"risk"`     // auto / confirm / blocked / -
	Decision string `json:"decision"` // executed / rejected / approved / denied / timeout / preempted / granted / -
	ByUser   bool   `json:"byUser"`   // 是否用户手动决策
	Source   string `json:"source"`   // external(外部智能体) / embedded(内嵌智能体) / system
	BatchID  string `json:"batchId"`  // 批量执行关联 ID(非批量为空)
}

const (
	mcpAuditMemCap   = 500              // 内存环形缓冲容量(有界,防无限增长)
	mcpAuditFileMax  = 5 * 1024 * 1024  // 单文件 5MB 上限,超过轮转
	mcpAuditKeepDays = 30               // 磁盘保留天数
)

// McpAuditService 审计日志服务。
type McpAuditService struct {
	mu      sync.Mutex
	seq     int64
	dir     string
	memory  []McpAuditEntry
	file    *os.File
	fileDay string
	emitFn  func(entry McpAuditEntry) // 事件推送回调(由 McpService 注入,可为 nil)
}

// NewMcpAuditService 创建审计服务并加载当日已有日志(截取最近若干条)。
func NewMcpAuditService(dir string) *McpAuditService {
	s := &McpAuditService{dir: dir}
	os.MkdirAll(dir, 0700)
	s.loadRecent()
	s.cleanupOld()
	return s
}

// SetEmitter 注入前端事件推送回调。
func (s *McpAuditService) SetEmitter(fn func(entry McpAuditEntry)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emitFn = fn
}

// Append 追加一条审计记录:写入内存缓冲、持久化文件并推送前端事件。
// source: external / embedded / system。
func (s *McpAuditService) Append(source, level, action, subject, detail, risk, decision string, byUser bool) McpAuditEntry {
	return s.AppendBatch(source, level, action, subject, detail, risk, decision, byUser, "")
}

// Close 关闭当前打开的审计文件句柄(服务停止或测试清理时调用,幂等)。
func (s *McpAuditService) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file != nil {
		s.file.Close()
		s.file = nil
	}
}

// AppendBatch 追加带批量关联 ID 的审计记录。
func (s *McpAuditService) AppendBatch(source, level, action, subject, detail, risk, decision string, byUser bool, batchID string) McpAuditEntry {
	if source == "" {
		source = "system"
	}
	s.mu.Lock()
	s.seq++
	entry := McpAuditEntry{
		ID:       fmt.Sprintf("a-%d", s.seq),
		TS:       time.Now().Format(time.RFC3339),
		Level:    level,
		Action:   action,
		Subject:  subject,
		Detail:   truncateUtf8(detail, 200),
		Risk:     risk,
		Decision: decision,
		ByUser:   byUser,
		Source:   source,
		BatchID:  batchID,
	}
	s.memory = append(s.memory, entry)
	if len(s.memory) > mcpAuditMemCap {
		s.memory = s.memory[len(s.memory)-mcpAuditMemCap:]
	}
	s.persistLocked(entry)
	emit := s.emitFn
	s.mu.Unlock()

	if emit != nil {
		emit(entry)
	}
	return entry
}

// persistLocked 落盘(调用方持锁)。按日轮转文件;超限轮转为 .old。
func (s *McpAuditService) persistLocked(entry McpAuditEntry) {
	day := time.Now().Format("20060102")
	if s.file == nil || s.fileDay != day {
		if s.file != nil {
			s.file.Close()
		}
		path := filepath.Join(s.dir, "audit-"+day+".jsonl")
		s.fileDay = day
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			s.file = nil
			return
		}
		s.file = f
		// 超限轮转:当日文件过大时归档为 .old(仅保留一个备份)
		if st, err := f.Stat(); err == nil && st.Size() > mcpAuditFileMax {
			f.Close()
			old := path + ".old"
			os.Remove(old)
			os.Rename(path, old)
			f2, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
			if err != nil {
				s.file = nil
				return
			}
			s.file = f2
		}
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	s.file.Write(append(data, '\n'))
}

// loadRecent 启动时加载当日最近条目,供面板打开即有数据。
func (s *McpAuditService) loadRecent() {
	path := filepath.Join(s.dir, "audit-"+time.Now().Format("20060102")+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var entries []McpAuditEntry
	lineStart := 0
	for i := 0; i <= len(data); i++ {
		if i == len(data) || data[i] == '\n' {
			line := data[lineStart:i]
			lineStart = i + 1
			if len(line) == 0 {
				continue
			}
			var e McpAuditEntry
			if json.Unmarshal(line, &e) == nil {
				entries = append(entries, e)
			}
		}
	}
	if len(entries) > mcpAuditMemCap {
		entries = entries[len(entries)-mcpAuditMemCap:]
	}
	s.memory = entries
}

// cleanupOld 清理超过保留期的历史文件(含 .old)。
func (s *McpAuditService) cleanupOld() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -mcpAuditKeepDays).Format("20060102")
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && (len(name) > 20 && name[:6] == "audit-") {
			day := name[6:14]
			if day < cutoff {
				os.Remove(filepath.Join(s.dir, name))
			}
		}
	}
}

// Query 查询内存缓冲:offset 为起始下标(负数从尾部倒数),limit 上限。
func (s *McpAuditService) Query(offset, limit int) []McpAuditEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.memory)
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = n + offset
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= n {
		return []McpAuditEntry{}
	}
	end := offset + limit
	if end > n {
		end = n
	}
	out := make([]McpAuditEntry, end-offset)
	copy(out, s.memory[offset:end])
	return out
}

// truncateUtf8 按字符数安全截断(避免截断 UTF-8 多字节字符)。
func truncateUtf8(s string, maxRunes int) string {
	if len(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
