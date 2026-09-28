package services

// 本文件: 全局错误收集器(错误日志)。
//
// 基于两个开源库:
//   - github.com/rs/zerolog      结构化 JSON 日志(每行一条, 便于检索)
//   - gopkg.in/natefinch/lumberjack.v2  日志文件轮转(按大小 + 保留策略)
//
// 设计:
//   - 落盘: <DataDir>/errors/errors.log(JSONL), 10MB 轮转, 保留 7 份 / 30 天
//   - 内存: 环形缓冲 300 条, 供设置页实时展示与前端查询
//   - 事件: 每条错误经 SetEmitter 回调推送前端("app-error-collected")
//   - 全局入口: MainErrors + CollectError/CollectErrorMsg/CollectPanic 包级函数,
//     nil 容忍(未装配时零开销), 与 MainMcpService/MainLogService 同一惯例
//
// 使用约定:
//   - 服务中"此前被静默吞掉"的错误路径(_ = err / if err != nil { return })应改调
//     CollectError(source, action, err); source 标识子系统(plugin/mcp/app/frontend),
//     action 为具体动作(如 "plugin:crash:ping"、"install-github")。
//   - recover() 捕获的 panic 走 CollectPanic, 附带完整调用栈。

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	errorsDirName    = "errors"     // 数据目录下的错误日志目录名
	errorsFileName   = "errors.log" // 错误日志文件名
	errMemCap        = 300          // 内存环形缓冲容量(有界)
	errFileMaxSizeMB = 10           // 单文件上限(MB), 超过轮转
	errFileMaxBak    = 7            // 保留的历史轮转文件数
	errFileMaxAgeD   = 30           // 轮转文件保留天数
)

// ErrorEntry 单条错误记录(内存查询与前端推送用)。
type ErrorEntry struct {
	ID      string `json:"id"`              // 条目唯一 ID(e-<seq>)
	TS      string `json:"ts"`              // RFC3339 时间戳
	Source  string `json:"source"`          // 来源子系统: plugin / mcp / app / frontend / ...
	Action  string `json:"action"`          // 具体动作标识
	Message string `json:"message"`         // 错误信息
	Stack   string `json:"stack,omitempty"` // 调用栈(仅 panic 收集时附带)
}

// MainErrors 全局错误收集器(main.go 装配; nil 时收集函数零开销)。
var MainErrors *ErrorCollector

// ErrorLogDir 返回错误日志目录。
func ErrorLogDir() string {
	return filepath.Join(DataDir(), errorsDirName)
}

// ErrorCollector 错误收集服务。
type ErrorCollector struct {
	mu     sync.Mutex
	seq    int64
	lj     *lumberjack.Logger
	logger zerolog.Logger
	memory []ErrorEntry
	emitFn func(ErrorEntry)
}

// NewErrorCollector 创建错误收集器(目录不存在则创建)。
func NewErrorCollector(dir string) *ErrorCollector {
	_ = os.MkdirAll(dir, 0700)
	lj := &lumberjack.Logger{
		Filename:   filepath.Join(dir, errorsFileName),
		MaxSize:    errFileMaxSizeMB,
		MaxBackups: errFileMaxBak,
		MaxAge:     errFileMaxAgeD,
	}
	c := &ErrorCollector{lj: lj}
	c.logger = zerolog.New(lj).With().Timestamp().Logger()
	return c
}

// SetEmitter 注入前端事件推送回调。
func (c *ErrorCollector) SetEmitter(fn func(ErrorEntry)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.emitFn = fn
}

// Collect 记录一条错误(来源/动作/错误对象)。
func (c *ErrorCollector) Collect(source, action string, err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	c.CollectMsg(source, action, msg)
}

// CollectMsg 记录一条错误消息(来源/动作/消息文本; 空消息忽略)。
func (c *ErrorCollector) CollectMsg(source, action, msg string) {
	if c == nil || msg == "" {
		return
	}
	if len(msg) > 4096 {
		msg = msg[:4096]
	}
	c.mu.Lock()
	c.seq++
	entry := ErrorEntry{
		ID:      fmt.Sprintf("e-%d", c.seq),
		TS:      time.Now().Format(time.RFC3339),
		Source:  source,
		Action:  action,
		Message: msg,
	}
	c.memory = append(c.memory, entry)
	if len(c.memory) > errMemCap {
		c.memory = c.memory[len(c.memory)-errMemCap:]
	}
	// zerolog → lumberjack(JSONL 落盘; 写失败由 zerolog 静默丢弃, 不反压业务路径)
	c.logger.Error().
		Str("id", entry.ID).
		Str("source", entry.Source).
		Str("action", entry.Action).
		Msg(entry.Message)
	emit := c.emitFn
	c.mu.Unlock()

	if emit != nil {
		emit(entry)
	}
}

// CollectPanic 记录 recover() 捕获的 panic(附带完整调用栈)。
func (c *ErrorCollector) CollectPanic(source, action string, r any) {
	if c == nil || r == nil {
		return
	}
	c.CollectMsg(source, action, fmt.Sprintf("panic: %v\n%s", r, debug.Stack()))
}

// Query 查询内存缓冲: offset 起始下标(负数从尾部倒数), limit 上限。
func (c *ErrorCollector) Query(offset, limit int) []ErrorEntry {
	if c == nil {
		return []ErrorEntry{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	n := len(c.memory)
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
		return []ErrorEntry{}
	}
	end := offset + limit
	if end > n {
		end = n
	}
	out := make([]ErrorEntry, end-offset)
	copy(out, c.memory[offset:end])
	return out
}

// Close 关闭底层日志文件(应用退出或测试清理时调用, 幂等)。
func (c *ErrorCollector) Close() {
	if c == nil || c.lj == nil {
		return
	}
	_ = c.lj.Close()
}

// ==================== 包级 nil 容忍入口 ====================

// CollectError 收集一条错误(MainErrors 未装配时零开销)。
func CollectError(source, action string, err error) {
	MainErrors.Collect(source, action, err)
}

// CollectErrorMsg 收集一条错误消息(MainErrors 未装配时零开销)。
func CollectErrorMsg(source, action, msg string) {
	MainErrors.CollectMsg(source, action, msg)
}

// CollectPanic 收集 recover() 捕获的 panic(MainErrors 未装配时零开销)。
func CollectPanic(source, action string, r any) {
	MainErrors.CollectPanic(source, action, r)
}
