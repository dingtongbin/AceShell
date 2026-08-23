package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// MCP 永久授权(Grant)服务。
//
// 手动审批模式下,用户可对某次 confirm 级操作选择"永久授权":
//   - 仅 confirm 级可永久授权(blocked 永远无审批环节,不存在授权)
//   - 匹配条件: 命令串精确相等 + 命令涉及路径集合匹配(支持 * 通配)
//   - 命令相同但路径不同(如 cat /var/log/x vs cat /etc/shadow)不匹配,
//     必须重新询问 —— 路径级防护是授权的核心,防止白名单失控
//
// 持久化: 数据目录/mcp/grants.json(上限 200 条,超出拒绝新增并提示清理)。

// McpGrantRule 单条永久授权规则。
type McpGrantRule struct {
	ID        string   `json:"id"`        // g-<seq>
	Command   string   `json:"command"`   // 命令串(精确匹配)
	Paths     []string `json:"paths"`     // 涉及路径(排序后存储,支持 * 通配)
	CreatedAt string   `json:"createdAt"` // ISO 时间
}

// mcpGrantStore 授权规则存储(内存 + 文件持久化)。
type mcpGrantStore struct {
	mu     sync.Mutex
	seq    int64
	rules  []McpGrantRule
	path   string
	maxCap int
}

const mcpGrantMax = 200 // 规则上限(有界,防无限增长)

// newMcpGrantStore 创建并加载已有规则。
func newMcpGrantStore(dataDir string) *mcpGrantStore {
	dir := filepath.Join(dataDir, "mcp")
	os.MkdirAll(dir, 0700)
	s := &mcpGrantStore{
		path:   filepath.Join(dir, "grants.json"),
		maxCap: mcpGrantMax,
	}
	s.load()
	return s
}

// load 从磁盘加载规则(文件损坏时重置为空,失败安全)。
func (s *mcpGrantStore) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var rules []McpGrantRule
	if json.Unmarshal(data, &rules) != nil {
		return
	}
	if len(rules) > s.maxCap {
		rules = rules[len(rules)-s.maxCap:]
	}
	s.rules = rules
	for _, r := range rules {
		var n int
		if _, err := fmt.Sscanf(r.ID, "g-%d", &n); err == nil && n > int(s.seq) {
			s.seq = int64(n)
		}
	}
}

// persistLocked 落盘(调用方持锁)。
func (s *mcpGrantStore) persistLocked() error {
	data, err := json.MarshalIndent(s.rules, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(s.path, data, 0600)
}

// Match 检查命令+路径是否命中某条永久授权规则。
// 命中返回规则 ID;未命中返回空串。
func (s *mcpGrantStore) Match(command string, paths []string) string {
	if command == "" {
		return ""
	}
	cmd := strings.TrimSpace(command)
	normalized := normalizePaths(paths)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.rules {
		if r.Command != cmd {
			continue
		}
		if matchPathSet(r.Paths, normalized) {
			return r.ID
		}
	}
	return ""
}

// Add 新增永久授权规则(命令+路径)。
// 达到上限时返回错误,提示用户清理。
func (s *mcpGrantStore) Add(command string, paths []string) (McpGrantRule, error) {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return McpGrantRule{}, fmt.Errorf("命令为空")
	}
	normalized := normalizePaths(paths)
	s.mu.Lock()
	defer s.mu.Unlock()
	// 去重: 完全相同的规则不重复保存
	for _, r := range s.rules {
		if r.Command == cmd && matchPathSet(r.Paths, normalized) {
			return r, nil
		}
	}
	if len(s.rules) >= s.maxCap {
		return McpGrantRule{}, fmt.Errorf("永久授权规则已达上限(%d 条),请先清理", s.maxCap)
	}
	s.seq++
	rule := McpGrantRule{
		ID:        fmt.Sprintf("g-%d", s.seq),
		Command:   cmd,
		Paths:     normalized,
		CreatedAt: time.Now().Format(time.RFC3339),
	}
	s.rules = append(s.rules, rule)
	if err := s.persistLocked(); err != nil {
		// 回滚内存状态,保持与磁盘一致
		s.rules = s.rules[:len(s.rules)-1]
		return McpGrantRule{}, err
	}
	return rule, nil
}

// Remove 删除指定规则。
func (s *mcpGrantStore) Remove(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, r := range s.rules {
		if r.ID == id {
			s.rules = append(s.rules[:i], s.rules[i+1:]...)
			s.persistLocked()
			return true
		}
	}
	return false
}

// Clear 清空全部规则。
func (s *mcpGrantStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rules = nil
	s.persistLocked()
}

// List 返回规则副本。
func (s *mcpGrantStore) List() []McpGrantRule {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]McpGrantRule, len(s.rules))
	copy(out, s.rules)
	return out
}

// ==================== 路径匹配 ====================

// normalizePaths 规范化路径集合: 去空白、去重、排序(存储与匹配口径一致)。
func normalizePaths(paths []string) []string {
	seen := make(map[string]bool, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	// 排序保证集合比较稳定
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// matchPathSet 判断实际路径集合是否与规则路径集合匹配。
// 规则路径支持 * 通配(仅完整段,如 /var/log/*);无路径命令(空集合)要求规则也是空集合。
func matchPathSet(rulePaths, actualPaths []string) bool {
	if len(rulePaths) == 0 && len(actualPaths) == 0 {
		return true
	}
	if len(rulePaths) != len(actualPaths) {
		return false
	}
	for i, rp := range rulePaths {
		if !pathWildcardMatch(rp, actualPaths[i]) {
			return false
		}
	}
	return true
}

// pathWildcardMatch 单路径通配匹配(* 匹配任意字符序列)。
func pathWildcardMatch(pattern, s string) bool {
	if !strings.Contains(pattern, "*") {
		return pattern == s
	}
	// 简单 glob: 按 * 分段,依次要求按序出现
	parts := strings.Split(pattern, "*")
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	idx := len(parts[0])
	for i := 1; i < len(parts); i++ {
		if parts[i] == "" {
			continue
		}
		j := strings.Index(s[idx:], parts[i])
		if j < 0 {
			return false
		}
		idx += j + len(parts[i])
	}
	tail := parts[len(parts)-1]
	if tail != "" && !strings.HasSuffix(s, tail) {
		return false
	}
	return true
}

// ==================== 路径提取(供分级引擎使用) ====================

// extractPaths 从单行命令提取涉及的文件系统路径。
// 轻量 shell 词法分析: 按空白分词(处理单/双引号),取以下形态的 token:
//   - 以 / 开头的绝对路径
//   - Windows 盘符路径(C:\ 或 C:/)
//   - 以 ./ ../ ~/ 开头的相对路径
//   - 包含 / 且含扩展名形态的路径参数(如 etc/hosts)
// 选项(-x --xxx)与 URL(http://...)排除。
func extractPaths(line string) []string {
	tokens := shellTokenize(line)
	var paths []string
	for _, tk := range tokens {
		if tk == "" || len(tk) > 512 {
			continue
		}
		lower := strings.ToLower(tk)
		if strings.HasPrefix(tk, "-") || strings.HasPrefix(lower, "http://") ||
			strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "ftp://") {
			continue
		}
		isPath := false
		if strings.HasPrefix(tk, "/") || strings.HasPrefix(tk, "./") ||
			strings.HasPrefix(tk, "../") || strings.HasPrefix(tk, "~") {
			isPath = true
		} else if len(tk) >= 3 && tk[1] == ':' && (tk[2] == '\\' || tk[2] == '/') &&
			tk[0] >= 'A' && tk[0] <= 'z' {
			isPath = true // Windows 盘符
		} else if strings.ContainsAny(tk, "/\\") && strings.Contains(tk, ".") &&
			!isShellWord(tk) && !strings.ContainsAny(tk, "=:,;|&()<>") {
			// 形如 etc/hosts、dir/file.txt 的相对路径(排除赋值/重定向等)
			isPath = true
		}
		if isPath {
			paths = append(paths, tk)
		}
	}
	return paths
}

// isShellWord 判断 token 是否为 shell 保留字/命令名(非路径)。
func isShellWord(tk string) bool {
	switch tk {
	case "sh", "bash", "zsh", "dash", "ksh", "python", "python3", "perl",
		"ruby", "node", "sudo", "su", "doas", "env", "nohup", "xargs",
		"grep", "egrep", "awk", "sed", "find", "sort", "uniq", "head", "tail":
		return true
	}
	return false
}

// shellTokenize 简单 shell 分词(处理单引号/双引号,不处理变量展开)。
func shellTokenize(line string) []string {
	var tokens []string
	var cur strings.Builder
	var quote rune
	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, cur.String())
			cur.Reset()
		}
	}
	for _, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return tokens
}

// sensitivePathPrefixes 敏感路径前缀(命中则命令风险升为 confirm)。
var sensitivePathPrefixes = []string{
	"/etc", "/boot", "/sys", "/proc", "/dev", "/root", "/var/log",
	"/etc/shadow", "/etc/passwd", "/etc/sudoers", "/etc/ssh",
	"c:/windows", "c:\\windows", "c:/program files",
}

// hasSensitivePath 命令路径中是否含敏感位置。
func hasSensitivePath(paths []string) bool {
	for _, p := range paths {
		lp := strings.ToLower(strings.ReplaceAll(p, "\\", "/"))
		for _, sp := range sensitivePathPrefixes {
			spl := strings.ReplaceAll(sp, "\\", "/")
			if lp == spl || strings.HasPrefix(lp, spl+"/") {
				return true
			}
		}
	}
	return false
}

// compileCustomRules 编译用户自定义规则(配置变更时调用,失败规则跳过)。
func compileCustomRules(rules []McpCustomRule) []*mcpCustomCompiled {
	out := make([]*mcpCustomCompiled, 0, len(rules))
	for _, r := range rules {
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			continue
		}
		switch r.Risk {
		case RiskBlocked, RiskConfirm, RiskAuto:
		default:
			continue
		}
		out = append(out, &mcpCustomCompiled{re: re, risk: r.Risk, note: r.Note})
	}
	return out
}

type mcpCustomCompiled struct {
	re   *regexp.Regexp
	risk string
	note string
}
