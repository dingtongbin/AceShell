package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// MCP 服务:基于官方 go-sdk(modelcontextprotocol/go-sdk)的 Streamable HTTP 服务。
//
// 架构与安全:
//   - 仅监听 127.0.0.1 字面量(绝不用 localhost,防 hosts 投毒与 IPv6 解析差异)
//   - Bearer Token 鉴权(32 字节随机,经 encryptSecret 加密落盘)
//   - SDK 内置 DNS rebinding 防护(默认开启)+ 本服务中间件鉴权
//   - 服务运行在独立 goroutine(go Serve),绝不阻塞主协程
//   - 状态机: stopped → running ⇄ paused;用户键盘抢占触发自动 paused
//
// 操作分级(见 mcppolicy.go): blocked 拦截并挂起 / confirm 手动授权
// (auto 模式由 AI 判定) / auto 直接执行。
//
// 前端桥接: 工具调用经 "mcp-command" 事件下发前端(复用与用户完全相同的
// UI 路径,多行输入走粘贴确认弹窗),前端通过 McpResolveCommand 回执。

const (
	mcpStateStopped = "stopped"
	mcpStateRunning = "running"
	mcpStatePaused  = "paused"

	mcpDefaultPort     = 8940
	mcpPortRetryMax    = 10
	mcpCmdTimeout      = 60 * time.Second
	mcpOpenTimeout     = 90 * time.Second // open_session 含交互式认证等待
	mcpApprovalTimeout = 60 * time.Second
	mcpOutBufCap       = 64 * 1024 // 每标签页输出环形缓冲上限(有界)
	mcpReadMax         = 32 * 1024 // terminal_read 单次返回上限
	mcpBatchMax        = 50        // batch_execute 单批命令上限(有界)

	// 仲裁优先级: 数字越小越优先。用户(P0)不走队列,键盘抢占直接挂起 MCP。
	mcpPrioEmbedded = 1 // 内嵌智能体(进程内直连)
	mcpPrioExternal = 2 // 外部智能体(HTTP MCP 客户端)

	mcpArbQ1Cap = 16 // 内嵌队列容量(有界)
	mcpArbQ2Cap = 48 // 外部队列容量(有界)

	McpModeManual = "manual" // 默认: confirm 级操作需用户手动授权
	McpModeAuto   = "auto"   // 自动审批: confirm 级操作由 AI 判定放行
)

// MainMcpService 全局 MCP 服务实例(readLoop 输出 tap 使用,nil 时零开销)。
var MainMcpService *McpService

// mcpArbItem 仲裁队列项。
type mcpArbItem struct {
	fn   func() error // 槽内执行体(含 routeCommand 全程)
	done chan error
}

// mcpApprovalDecision 审批决策(含永久授权标记)。
type mcpApprovalDecision struct {
	Approved  bool
	Permanent bool // 批准且永久授权(命令+路径写授权库)
}

// execOpts 工具执行选项(内外智能体共享同一实现,行为差异由此控制)。
type execOpts struct {
	prio        int    // 仲裁优先级(mcpPrioEmbedded / mcpPrioExternal)
	source      string // 审计来源(mcpSrcEmbedded / mcpSrcExternal)
	forceManual bool   // 强制手动审批(内嵌智能体 manual 模式:即使全局为 auto 也需用户审批)
	noPaste     bool   // 跳过前端多行粘贴确认弹窗(授权已由审批弹窗一次性完成)
}

// McpService MCP 集成服务。
type McpService struct {
	app         *application.App
	sessionFile *SessionFileService
	cfg         *ConfigService

	mu         sync.Mutex
	audit      *McpAuditService
	grants     *mcpGrantStore
	server     *http.Server
	listener   net.Listener
	token      string
	state      string
	busy       bool                          // 工具调用进行中(槽占用或任一工具活动,驱动前端"执行中"遮罩)
	slotBusy   bool                          // 仲裁执行槽占用中
	activeCnt  int                           // 槽外进行中的工具调用数(含审批等待/输出回读/只读工具)
	pendingCmd map[string]chan mcpCmdResult  // requestId → 结果通道
	approvals  map[string]*mcpApproval       // approvalId → 审批请求
	outBuf     map[string][]byte             // tabId → 原始输出缓冲
	outCursor  map[string]int                // tabId → terminal_read 游标
	reqSeq     int64
	apprSeq    int64

	// 挂起期间用户活动捕获(见 mcpsuspend.go)
	suspendCapturing bool              // 正在捕获(挂起期间)
	suspendUnread    bool              // 有已结束捕获的未读报告(待智能体 drain)
	suspendBuf       map[string][]byte // tabId → 捕获的增量输出
	suspendTabCut    map[string]bool   // tabId → 该页内容被裁剪过
	suspendGlobalCut bool              // 全局总量超限
	suspendStartedAt time.Time         // 本次捕获开始时间

	// 全局串行仲裁器: 所有标签页操作(内外智能体)单车道执行,优先级出队
	arbP1   chan *mcpArbItem // 内嵌智能体队列
	arbP2   chan *mcpArbItem // 外部智能体队列
	arbQuit chan struct{}
	arbOnce sync.Once
}

// mcpCmdResult 前端命令回执。
type mcpCmdResult struct {
	Result string // JSON 结果(空表示无数据)
	Err    string // 错误信息
}

// mcpApproval 待审批请求。
type mcpApproval struct {
	ID        string    `json:"id"`
	Action    string    `json:"action"`
	Summary   string    `json:"summary"`
	Detail    string    `json:"detail"`
	Risk      string    `json:"risk"`
	Command   string    `json:"command"`           // 原始命令(永久授权用)
	Paths     []string `json:"paths"`              // 命令涉及路径(永久授权+展示用)
	ExpiresAt time.Time `json:"expiresAt"`
	ch        chan mcpApprovalDecision
}

// NewMcpService 创建 MCP 服务。
func NewMcpService(cfg *ConfigService, sessionFile *SessionFileService) *McpService {
	s := &McpService{
		cfg:         cfg,
		sessionFile: sessionFile,
		state:       mcpStateStopped,
		pendingCmd:  make(map[string]chan mcpCmdResult),
		approvals:   make(map[string]*mcpApproval),
		outBuf:      make(map[string][]byte),
		outCursor:   make(map[string]int),
		arbP1:       make(chan *mcpArbItem, mcpArbQ1Cap),
		arbP2:       make(chan *mcpArbItem, mcpArbQ2Cap),
		arbQuit:     make(chan struct{}),
	}
	s.audit = NewMcpAuditService(McpAuditDir())
	s.grants = newMcpGrantStore(DataDir())
	RefreshCustomRules(cfg.McpCustomRules())
	s.startArbiter()
	return s
}

// startArbiter 启动仲裁循环(全局唯一执行槽,P1 优先出队)。
func (s *McpService) startArbiter() {
	go func() {
		for {
			// 优先取 P1(非阻塞),否则双队列阻塞等待
			select {
			case <-s.arbQuit:
				return
			case it := <-s.arbP1:
				s.runArbItem(it)
				continue
			default:
			}
			select {
			case <-s.arbQuit:
				return
			case it := <-s.arbP1:
				s.runArbItem(it)
			case it := <-s.arbP2:
				s.runArbItem(it)
			}
		}
	}()
}

// runArbItem 执行槽内操作(原子:执行中不被抢占,保证命令完整)。
// 槽占用计入忙碌状态,供前端展示"MCP 执行中"并锁定标签页区域。
func (s *McpService) runArbItem(it *mcpArbItem) {
	s.mu.Lock()
	s.slotBusy = true
	changed := s.recomputeBusyLocked()
	s.mu.Unlock()
	if changed {
		s.emitStatus()
	}
	defer func() {
		if r := recover(); r != nil {
			it.done <- fmt.Errorf("内部错误: %v", r)
		}
		s.mu.Lock()
		s.slotBusy = false
		ch := s.recomputeBusyLocked()
		s.mu.Unlock()
		if ch {
			s.emitStatus()
		}
	}()
	it.done <- it.fn()
}

// beginActivity 标记一个工具调用开始(覆盖审批等待/槽外回读/只读工具的全生命周期)。
func (s *McpService) beginActivity() {
	s.mu.Lock()
	s.activeCnt++
	changed := s.recomputeBusyLocked()
	s.mu.Unlock()
	if changed {
		s.emitStatus()
	}
}

// endActivity 标记一个工具调用结束(与 beginActivity 配对,建议 defer 调用)。
func (s *McpService) endActivity() {
	s.mu.Lock()
	if s.activeCnt > 0 {
		s.activeCnt--
	}
	changed := s.recomputeBusyLocked()
	s.mu.Unlock()
	if changed {
		s.emitStatus()
	}
}

// recomputeBusyLocked 合并槽占用与活动计数得出忙碌状态(调用方持锁),返回是否发生变化。
func (s *McpService) recomputeBusyLocked() bool {
	nb := s.slotBusy || s.activeCnt > 0
	if nb == s.busy {
		return false
	}
	s.busy = nb
	return true
}

// arbitrate 将操作排入串行车道并等待完成。
// prio: mcpPrioEmbedded(1) / mcpPrioExternal(2)。队列满立即拒绝(有界)。
func (s *McpService) arbitrate(ctx context.Context, prio int, fn func() error) error {
	it := &mcpArbItem{fn: fn, done: make(chan error, 1)}
	var q chan *mcpArbItem
	if prio <= mcpPrioEmbedded {
		q = s.arbP1
	} else {
		q = s.arbP2
	}
	select {
	case q <- it:
	default:
		return fmt.Errorf("MCP 操作队列已满,请稍后重试(系统繁忙)")
	}
	select {
	case err := <-it.done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("操作已取消(客户端中断或用户抢占)")
	}
}

// McpAuditDir 返回 MCP 审计日志目录。
func McpAuditDir() string {
	return filepath.Join(DataDir(), "mcp", "audit")
}

// SetApp 注入 Wails 应用实例(wireServices 调用)。
func (s *McpService) SetApp(app *application.App) {
	s.app = app
	s.audit.SetEmitter(func(entry McpAuditEntry) {
		if s.app == nil {
			return
		}
		if data, err := json.Marshal(entry); err == nil {
			s.app.Event.Emit("mcp-audit-appended", string(data))
		}
	})
}

// emit 安全发送事件(锁外调用)。
func (s *McpService) emit(name string, payload any) {
	if s.app == nil {
		return
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	s.app.Event.Emit(name, string(data))
}

// ==================== 生命周期 ====================

// Start 启动 MCP 服务(后台 goroutine,立即返回)。
// 端口被占时自动 +1 重试(最多 10 次),全部失败用随机端口兜底。
func (s *McpService) Start() error {
	s.mu.Lock()
	if s.state != mcpStateStopped {
		s.mu.Unlock()
		return nil
	}
	// 确保 token 存在(首次启用生成并加密落盘)
	if err := s.ensureTokenLocked(); err != nil {
		s.mu.Unlock()
		return err
	}
	port := s.cfg.McpPort()
	if port <= 0 {
		port = mcpDefaultPort
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	for i := 0; i < mcpPortRetryMax && err != nil; i++ {
		port++
		ln, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	}
	if err != nil {
		// 兜底: 随机端口
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			s.mu.Unlock()
			return fmt.Errorf("MCP 监听失败: %w", err)
		}
	}
	if port != s.cfg.McpPort() {
		s.cfg.SetMcpPort(port)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "AceShell", Version: "1.0.0", Title: "AceShell MCP"}, nil)
	s.registerTools(server)

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		SessionTimeout: 10 * time.Minute,
	})

	mux := http.NewServeMux()
	mux.Handle("/mcp", s.authMiddleware(handler))
	s.server = &http.Server{Handler: mux}
	s.listener = ln
	s.state = mcpStateRunning
	s.mu.Unlock()

	// 子协程运行 HTTP 服务,绝不阻塞主协程
	go func() {
		if err := s.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.mu.Lock()
			s.state = mcpStateStopped
			s.mu.Unlock()
			s.audit.Append("system", "error", "server", "", "HTTP 服务异常退出: "+err.Error(), "-", "-", false)
			s.emitStatus()
		}
	}()

	s.audit.Append("system", "system", "start", "", fmt.Sprintf("MCP 服务已启动,监听 127.0.0.1:%d", port), "-", "-", false)
	s.emitStatus()
	return nil
}

// Stop 停止 MCP 服务。
func (s *McpService) Stop() {
	s.mu.Lock()
	if s.state == mcpStateStopped {
		s.mu.Unlock()
		return
	}
	s.state = mcpStateStopped
	server := s.server
	s.cancelPendingLocked("MCP 服务已停止")
	s.server = nil
	s.listener = nil
	s.mu.Unlock()

	// 服务停止:结束捕获且不保留未读报告(下次启动从干净状态开始)
	s.endSuspendCapture(false)

	if server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}
	s.audit.Append("system", "system", "stop", "", "MCP 服务已停止", "-", "-", false)
	s.emitStatus()
}

// Pause 挂起: 拒绝新请求、取消在途请求,端口与 Token 保留。
// 用户手动挂起时开始捕获终端活动,供智能体感知用户中断期间的操作。
func (s *McpService) Pause(byUser bool) {
	s.mu.Lock()
	if s.state != mcpStateRunning {
		s.mu.Unlock()
		return
	}
	s.state = mcpStatePaused
	s.cancelPendingLocked("MCP 已被挂起")
	s.mu.Unlock()

	if byUser {
		s.beginSuspendCapture()
	}
	s.audit.Append("system", "system", "pause", "", "MCP 已挂起,拒绝新请求", "-", "-", byUser)
	s.emitStatus()
}

// Resume 恢复运行,并结束挂起期活动捕获。
func (s *McpService) Resume() {
	s.mu.Lock()
	if s.state != mcpStatePaused {
		s.mu.Unlock()
		return
	}
	s.state = mcpStateRunning
	s.mu.Unlock()

	s.endSuspendCapture(true)
	s.audit.Append("system", "system", "resume", "", "MCP 已恢复运行", "-", "-", true)
	s.emitStatus()
}

// cancelPendingLocked 取消全部在途命令与审批(调用方持锁)。
func (s *McpService) cancelPendingLocked(reason string) {
	for id, ch := range s.pendingCmd {
		select {
		case ch <- mcpCmdResult{Err: reason}:
		default:
		}
		delete(s.pendingCmd, id)
	}
	for id, ap := range s.approvals {
		select {
		case ap.ch <- mcpApprovalDecision{}:
		default:
		}
		delete(s.approvals, id)
		s.emit("mcp-approval-removed", map[string]any{"id": id, "reason": reason})
	}
}

// authMiddleware Bearer Token 鉴权中间件。
func (s *McpService) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		token := s.token
		state := s.state
		s.mu.Unlock()

		if state == mcpStateStopped {
			http.Error(w, "MCP service is stopped", http.StatusServiceUnavailable)
			return
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ensureTokenLocked 确保访问令牌存在(不存在则生成并加密持久化)。
func (s *McpService) ensureTokenLocked() error {
	if s.token != "" {
		return nil
	}
	enc := s.cfg.McpTokenEnc()
	if enc != "" {
		if plain, err := decryptSecret(enc); err == nil && plain != "" {
			s.token = plain
			return nil
		}
	}
	// 生成新 token(32 字节随机 → 64 字符 hex)
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Errorf("生成 Token 失败: %w", err)
	}
	plain := hex.EncodeToString(buf)
	encNew, err := encryptSecret(plain)
	if err != nil {
		return fmt.Errorf("加密 Token 失败: %w", err)
	}
	if err := s.cfg.SetMcpTokenEnc(encNew); err != nil {
		return fmt.Errorf("保存 Token 失败: %w", err)
	}
	s.token = plain
	return nil
}

// emitStatus 推送状态变更事件。
func (s *McpService) emitStatus() {
	s.emit("mcp-status-changed", s.statusMap())
}

func (s *McpService) statusMap() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	port := s.cfg.McpPort()
	enabled := s.cfg.McpEnabled()
	mode := s.cfg.McpMode()
	ballX, ballY := s.cfg.McpBallPos()
	return map[string]any{
		"enabled":            enabled,
		"state":              s.state,
		"busy":               s.busy,
		"mode":               mode,
		"url":                fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
		"token":              s.token,
		"port":               port,
		"pendingApprovals":   len(s.approvals),
		"ballX":              ballX,
		"ballY":              ballY,
		"opDelayMs":          s.cfg.McpOpDelayMs(),
		"batchIntervalMs":    s.cfg.McpBatchIntervalMs(),
		"grantsEnabled":      s.cfg.McpGrantsEnabled(),
		"auditRetentionDays": s.cfg.McpAuditRetentionDays(),
		"terminalReadMax":    s.cfg.McpTerminalReadMax(),
	}
}

// ==================== 输出 tap(终端读取通道) ====================

// TapOutput 终端输出挂钩(readLoop 调用;MainMcpService 为 nil 时零开销)。
// 有界: 每标签页最多保留 64KB 原始输出,超限从头裁剪。
func (s *McpService) TapOutput(tabID string, data []byte) {
	if len(data) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	buf := append(s.outBuf[tabID], data...)
	if len(buf) > mcpOutBufCap {
		cut := len(buf) - mcpOutBufCap
		buf = buf[cut:]
		// 游标同步前移,避免读越界
		if c := s.outCursor[tabID]; c > cut {
			s.outCursor[tabID] = c - cut
		} else {
			s.outCursor[tabID] = 0
		}
	}
	s.outBuf[tabID] = buf
	// 挂起期间同步捕获(供智能体感知用户手动操作)
	if s.suspendCapturing {
		s.appendSuspendLocked(tabID, data)
	}
}

// readOutput 读取自上次调用以来的新增输出(剥离 ANSI,限 32KB)。
func (s *McpService) readOutput(tabID string) string {
	s.mu.Lock()
	buf := s.outBuf[tabID]
	cur := s.outCursor[tabID]
	if cur > len(buf) {
		cur = len(buf)
	}
	var chunk []byte
	if cur < len(buf) {
		chunk = buf[cur:]
		if len(chunk) > mcpReadMax {
			chunk = chunk[len(chunk)-mcpReadMax:]
		}
	}
	s.outCursor[tabID] = len(buf)
	s.mu.Unlock()
	if len(chunk) == 0 {
		return ""
	}
	return stripAnsi(string(chunk))
}

// ansiEscapeRe ANSI 转义序列(CSI/OSC/单字符转义)与控制字符。
var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[@-_]|\r|\x07|\x08`)

// stripAnsi 剥离 ANSI 转义序列,供 AI 阅读纯文本。
func stripAnsi(s string) string {
	return ansiEscapeRe.ReplaceAllString(s, "")
}

// ==================== 工具注册(外部 HTTP,P2 优先级) ====================

// addTrackedTool 注册工具并自动跟踪调用生命周期(beginActivity/endActivity 包裹),
// 使外部客户端的任意工具调用(含只读)都驱动前端忙碌状态。
func addTrackedTool[In, Out any](s *McpService, server *mcp.Server, tool *mcp.Tool, h func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error)) {
	mcp.AddTool(server, tool, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		s.beginActivity()
		defer s.endActivity()
		return h(ctx, req, in)
	})
}

func (s *McpService) registerTools(server *mcp.Server) {
	s.registerLogTools(server)

	type listSessionsOut struct {
		Sessions []mcpSessionInfo `json:"sessions"`
	}
	addTrackedTool(s, server, &mcp.Tool{
		Name:        "list_sessions",
		Description: "列出全部已保存的会话(不含任何密码或密钥)。返回路径供 open_session 使用。",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, listSessionsOut, error) {
		if err := s.checkRunning(); err != nil {
			return nil, listSessionsOut{}, err
		}
		out := listSessionsOut{Sessions: []mcpSessionInfo{}}
		out.Sessions = append(out.Sessions, flattenTree(s.sessionFile.GetTree())...)
		s.audit.Append(mcpSrcExternal, "info", "list_sessions", "", fmt.Sprintf("%d 个会话", len(out.Sessions)), RiskAuto, "executed", false)
		return nil, out, nil
	})

	type listTabsOut struct {
		Tabs []mcpTabInfo `json:"tabs"`
	}
	addTrackedTool(s, server, &mcp.Tool{
		Name:        "list_tabs",
		Description: "列出当前打开的全部标签页(终端与脚本编辑器),含状态与标签页 ID。",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, listTabsOut, error) {
		res, err := s.toolListTabs(ctx, mcpSrcExternal)
		if err != nil {
			return nil, listTabsOut{}, err
		}
		out := listTabsOut{Tabs: []mcpTabInfo{}}
		json.Unmarshal([]byte(res), &out.Tabs)
		return nil, out, nil
	})

	type openSessionIn struct {
		SessionPath string `json:"session_path" jsonschema:"会话相对路径(list_sessions 返回的 path)"`
	}
	type openSessionOut struct {
		TabID  string `json:"tab_id"`
		Status string `json:"status"`
	}
	addTrackedTool(s, server, &mcp.Tool{
		Name:        "open_session",
		Description: "在标签页中打开终端会话(SSH/Telnet/串口)。已打开则定位激活该标签页。界面会同步跳转,与用户手动打开完全一致。",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in openSessionIn) (*mcp.CallToolResult, openSessionOut, error) {
		res, err := s.toolOpenSession(ctx, mcpExternalOpts(), in.SessionPath)
		if err != nil {
			return nil, openSessionOut{}, err
		}
		var out openSessionOut
		json.Unmarshal([]byte(res), &out)
		return nil, out, nil
	})

	type terminalSendIn struct {
		TabID string `json:"tab_id" jsonschema:"目标终端标签页 ID"`
		Text  string `json:"text" jsonschema:"要输入的文本。单行为命令;多行视为批量输入,与用户多行粘贴相同逻辑"`
	}
	type terminalSendOut struct {
		Ok   bool   `json:"ok"`
		Note string `json:"note,omitempty"`
	}
	addTrackedTool(s, server, &mcp.Tool{
		Name:        "terminal_send",
		Description: "向终端标签页输入文本。输入会显示在终端上,与用户手动输入完全一致;目标标签页会被激活。单行命令会自动等待并返回执行输出(output 字段),无需再调 terminal_read;多行输入与用户粘贴相同逻辑。",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in terminalSendIn) (*mcp.CallToolResult, terminalSendOut, error) {
		res, err := s.toolTerminalSend(ctx, mcpExternalOpts(), in.TabID, in.Text)
		if err != nil {
			return nil, terminalSendOut{}, err
		}
		var out terminalSendOut
		json.Unmarshal([]byte(res), &out)
		return nil, out, nil
	})

	type terminalReadIn struct {
		TabID string `json:"tab_id" jsonschema:"目标终端标签页 ID"`
	}
	type terminalReadOut struct {
		Output string `json:"output"`
	}
	addTrackedTool(s, server, &mcp.Tool{
		Name:        "terminal_read",
		Description: "读取终端标签页自上次调用以来的新增输出(已剥离控制序列的纯文本)。首次调用返回最近的历史输出。",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in terminalReadIn) (*mcp.CallToolResult, terminalReadOut, error) {
		if err := s.checkRunning(); err != nil {
			return nil, terminalReadOut{}, err
		}
		return nil, terminalReadOut{Output: s.readOutput(in.TabID)}, nil
	})

	type batchIn struct {
		TabID      string   `json:"tab_id" jsonschema:"目标终端标签页 ID"`
		Commands   []string `json:"commands" jsonschema:"要顺序执行的命令列表(每条单行)"`
		IntervalMs int      `json:"interval_ms,omitempty" jsonschema:"命令间隔毫秒(默认 300,最小 50)"`
	}
	type batchOut struct {
		Ok      bool   `json:"ok"`
		Count   int    `json:"count"`
		Note    string `json:"note,omitempty"`
	}
	addTrackedTool(s, server, &mcp.Tool{
		Name:        "batch_execute",
		Description: "批量顺序执行命令(不切换标签页,前台界面不跳转)。整批共用一次授权确认;任一命令为绝对危险则整批拒绝。适合巡检等只读命令序列。",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in batchIn) (*mcp.CallToolResult, batchOut, error) {
		res, err := s.toolBatchExecute(ctx, mcpExternalOpts(), in.TabID, in.Commands, in.IntervalMs)
		if err != nil {
			return nil, batchOut{}, err
		}
		var out batchOut
		json.Unmarshal([]byte(res), &out)
		return nil, out, nil
	})

	type openScriptIn struct {
		FilePath string `json:"file_path" jsonschema:"脚本文件路径"`
	}
	type openScriptOut struct {
		TabID string `json:"tab_id"`
	}
	addTrackedTool(s, server, &mcp.Tool{
		Name:        "open_script",
		Description: "在脚本编辑器标签页中打开脚本文件。已打开则定位激活。与用户手动打开完全一致。",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in openScriptIn) (*mcp.CallToolResult, openScriptOut, error) {
		res, err := s.toolOpenScript(ctx, mcpExternalOpts(), in.FilePath)
		if err != nil {
			return nil, openScriptOut{}, err
		}
		var out openScriptOut
		json.Unmarshal([]byte(res), &out)
		return nil, out, nil
	})

	type scriptWriteIn struct {
		FilePath string `json:"file_path" jsonschema:"目标脚本文件路径"`
		Content  string `json:"content" jsonschema:"要写入的完整内容"`
	}
	type scriptWriteOut struct {
		Ok   bool   `json:"ok"`
		Note string `json:"note,omitempty"`
	}
	addTrackedTool(s, server, &mcp.Tool{
		Name:        "script_write",
		Description: "向脚本编辑器写入内容(编辑器会显示为未保存/自动保存状态,与用户手动编辑完全一致)。属常规危险操作,默认需用户授权。",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in scriptWriteIn) (*mcp.CallToolResult, scriptWriteOut, error) {
		res, err := s.toolScriptWrite(ctx, mcpExternalOpts(), in.FilePath, in.Content)
		if err != nil {
			return nil, scriptWriteOut{}, err
		}
		var out scriptWriteOut
		json.Unmarshal([]byte(res), &out)
		return nil, out, nil
	})

	type closeTabIn struct {
		TabID string `json:"tab_id" jsonschema:"要关闭的标签页 ID"`
	}
	type closeTabOut struct {
		Ok   bool   `json:"ok"`
		Note string `json:"note,omitempty"`
	}
	addTrackedTool(s, server, &mcp.Tool{
		Name:        "close_tab",
		Description: "关闭指定标签页。与用户手动关闭完全一致(未保存脚本会弹确认)。",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in closeTabIn) (*mcp.CallToolResult, closeTabOut, error) {
		res, err := s.toolCloseTab(ctx, mcpExternalOpts(), in.TabID)
		if err != nil {
			return nil, closeTabOut{}, err
		}
		var out closeTabOut
		json.Unmarshal([]byte(res), &out)
		return nil, out, nil
	})
}

// mcpExternalOpts 外部 HTTP 客户端默认执行选项。
func mcpExternalOpts() execOpts {
	return execOpts{prio: mcpPrioExternal, source: mcpSrcExternal}
}

// mcpSrcExternal / mcpSrcEmbedded 审计来源常量。
const (
	mcpSrcExternal = "external"
	mcpSrcEmbedded = "embedded"
)

// ==================== 工具主体(内外智能体共享同一实现) ====================

// toolListTabs 列出标签页(纯读,不进仲裁车道)。
func (s *McpService) toolListTabs(ctx context.Context, source string) (string, error) {
	if err := s.checkRunning(); err != nil {
		return "", err
	}
	return s.routeCommand(ctx, "list_tabs", map[string]any{}, mcpCmdTimeout, RiskAuto, "list_tabs", source)
}

// toolOpenSession 打开会话(进仲裁车道;打开动作本身即激活标签页)。
func (s *McpService) toolOpenSession(ctx context.Context, opts execOpts, sessionPath string) (string, error) {
	if err := s.checkRunning(); err != nil {
		return "", err
	}
	meta, err := s.loadSessionMeta(sessionPath)
	if err != nil {
		return "", err
	}
	switch meta["protocol"] {
	case "rdp", "http", "sftp":
		return "", fmt.Errorf("MCP 仅支持终端类会话(SSH/Telnet/串口),不支持 %v", meta["protocol"])
	}
	return s.execRouted(ctx, opts.prio, "open_session", map[string]any{
		"sessionPath": sessionPath,
	}, mcpOpenTimeout, RiskAuto, "open_session:"+sessionPath, opts.source)
}

// toolTerminalSend 终端输入(分级 → 永久授权 → 审批 → 仲裁执行)。
func (s *McpService) toolTerminalSend(ctx context.Context, opts execOpts, tabID, text string) (string, error) {
	if err := s.checkRunning(); err != nil {
		return "", err
	}
	g := GradeTextEx(text)
	if g.Risk == RiskBlocked {
		s.audit.Append(opts.source, "blocked", "terminal_send", tabID, text, RiskBlocked, "rejected", false)
		s.emit("mcp-critical-blocked", map[string]any{"command": firstLine(text), "reason": g.Reason})
		go s.Pause(false)
		return "", fmt.Errorf("绝对危险指令已被拦截: %s。MCP 已自动挂起,需用户手动恢复", g.Reason)
	}
	manual := s.cfg.McpMode() == McpModeManual || opts.forceManual
	multiline := strings.Contains(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	needPasteConfirm := multiline && manual && !opts.noPaste

	if g.Risk == RiskConfirm {
		if manual && !needPasteConfirm {
			// 永久授权(命令+路径双精确)命中则免审批
			if s.cfg.McpGrantsEnabled() && s.grants.Match(text, g.Paths) != "" {
				s.audit.Append(opts.source, "confirm", "terminal_send", tabID, text, RiskConfirm, "granted", false)
			} else {
				dec, err := s.requestApproval(opts.source, "terminal_send", summarizeText(text), text, RiskConfirm, text, g.Paths)
				if err != nil {
					return "", err
				}
				if !dec.Approved {
					s.audit.Append(opts.source, "confirm", "terminal_send", tabID, text, RiskConfirm, "denied", true)
					return "", fmt.Errorf("用户拒绝了本次输入")
				}
				s.audit.Append(opts.source, "confirm", "terminal_send", tabID, text, RiskConfirm, "approved", true)
			}
		} else if manual && needPasteConfirm {
			// 多行手动模式(外部客户端): 前端多行粘贴确认弹窗承担人工授权
			s.audit.Append(opts.source, "confirm", "terminal_send", tabID, summarizeText(text), RiskConfirm, "pending", false)
		} else {
			// 自动审批模式: AI 已判定放行
			s.audit.Append(opts.source, "confirm", "terminal_send", tabID, text, RiskConfirm, "approved", false)
		}
	} else {
		s.audit.Append(opts.source, "info", "terminal_send", tabID, text, RiskAuto, "executed", false)
	}

	// 基线在发送前建立: 之后缓冲增长均为命令回显+执行结果
	base := s.alignCursor(tabID)
	res, err := s.execRouted(ctx, opts.prio, "terminal_send", map[string]any{
		"tabId":            tabID,
		"text":             text,
		"multiline":        multiline,
		"needPasteConfirm": needPasteConfirm,
	}, mcpApprovalTimeout+mcpCmdTimeout, g.Risk, "terminal_send:"+tabID, opts.source)
	if err != nil {
		return "", err
	}
	// 单行命令: 自动等待并带回新增输出(命令回显+执行结果),
	// 免去 AI 二次 terminal_read;多行/粘贴确认流程不回读。
	if !multiline && !needPasteConfirm {
		if output := s.waitNewOutput(ctx, tabID, base); output != "" {
			var out map[string]any
			if json.Unmarshal([]byte(res), &out) == nil && out != nil {
				out["output"] = output
				if data, err2 := json.Marshal(out); err2 == nil {
					return string(data), nil
				}
			}
		}
	}
	return res, nil
}

// alignCursor 将读取游标对齐到当前缓冲末尾,返回对齐点长度。
// 用于 terminal_send 前建立基线: 之后缓冲增长均为本次命令产生的新输出,
// 同时丢弃未读历史(游标对齐意味着 readOutput 只回读新增)。
func (s *McpService) alignCursor(tabID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.outBuf[tabID])
	s.outCursor[tabID] = n
	return n
}

// outputLen 返回当前缓冲长度(无锁读取由 mu 保护)。
func (s *McpService) outputLen(tabID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.outBuf[tabID])
}

// waitNewOutput 轮询等待新增输出静止(或超时)后,从游标读取到末尾。
// 有界: 最长 1.5s,150ms 轮询,连续 3 次长度不变视为静止。
func (s *McpService) waitNewOutput(ctx context.Context, tabID string, base int) string {
	const (
		pollInterval = 150 * time.Millisecond
		maxWait      = 1500 * time.Millisecond
		idleRound    = 3
	)
	deadline := time.Now().Add(maxWait)
	prev := s.outputLen(tabID)
	idle := 0
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ""
		case <-time.After(pollInterval):
		}
		cur := s.outputLen(tabID)
		if cur == prev {
			idle++
		} else {
			idle = 0
			prev = cur
		}
		if prev > base && idle >= idleRound {
			break
		}
	}
	if s.outputLen(tabID) <= base {
		return "" // 无新增
	}
	return s.readOutput(tabID)
}

// toolBatchExecute 批量执行(不切标签页;整批一次授权;blocked 整批拒绝)。
func (s *McpService) toolBatchExecute(ctx context.Context, opts execOpts, tabID string, commands []string, intervalMs int) (string, error) {
	if err := s.checkRunning(); err != nil {
		return "", err
	}
	if len(commands) == 0 {
		return "", fmt.Errorf("命令列表为空")
	}
	if len(commands) > mcpBatchMax {
		return "", fmt.Errorf("单批命令超过上限 %d 条", mcpBatchMax)
	}
	if intervalMs < 50 {
		intervalMs = s.cfg.McpBatchIntervalMs()
	}
	if intervalMs > 10000 {
		intervalMs = 10000
	}
	// 逐条分级: 任一 blocked 整批拒绝;任一 confirm 整批按 confirm 审批
	overall := RiskAuto
	var allPaths []string
	var listPreview []string
	for _, c := range commands {
		listPreview = append(listPreview, firstLine(c))
		g := GradeCommandEx(c)
		if g.Risk == RiskBlocked {
			s.audit.Append(opts.source, "blocked", "batch_execute", tabID, firstLine(c), RiskBlocked, "rejected", false)
			s.emit("mcp-critical-blocked", map[string]any{"command": firstLine(c), "reason": g.Reason})
			go s.Pause(false)
			return "", fmt.Errorf("批量中含绝对危险指令,整批已拒绝并挂起 MCP: %s", g.Reason)
		}
		if g.Risk == RiskConfirm {
			overall = RiskConfirm
		}
		allPaths = append(allPaths, g.Paths...)
	}
	if overall == RiskConfirm && (s.cfg.McpMode() == McpModeManual || opts.forceManual) {
		detail := strings.Join(listPreview, "\n")
		dec, err := s.requestApproval(opts.source, "batch_execute", fmt.Sprintf("批量执行 %d 条命令(含需确认项)", len(commands)), detail, RiskConfirm, "", nil)
		if err != nil {
			return "", err
		}
		if !dec.Approved {
			s.audit.Append(opts.source, "confirm", "batch_execute", tabID, detail, RiskConfirm, "denied", true)
			return "", fmt.Errorf("用户拒绝了本批量执行")
		}
		s.audit.Append(opts.source, "confirm", "batch_execute", tabID, detail, RiskConfirm, "approved", true)
	}
	batchID := fmt.Sprintf("b-%d-%d", time.Now().UnixMilli(), len(commands))
	// 批量超时 = 命令数 × (间隔 + 余量)
	timeout := mcpCmdTimeout + time.Duration(len(commands))*time.Duration(intervalMs+2500)*time.Millisecond
	res, err := s.execRoutedRaw(ctx, opts.prio, "batch_execute", map[string]any{
		"tabId":      tabID,
		"commands":   commands,
		"intervalMs": intervalMs,
	}, timeout, overall, "batch_execute:"+tabID, opts.source)
	if err != nil {
		return "", err
	}
	// 逐条审计(关联 batchID)
	for _, c := range commands {
		s.audit.AppendBatch(opts.source, "info", "batch_execute", tabID, firstLine(c), overall, "executed", false, batchID)
	}
	return res, nil
}

// toolOpenScript 打开脚本(进仲裁车道;打开即激活)。
func (s *McpService) toolOpenScript(ctx context.Context, opts execOpts, filePath string) (string, error) {
	if err := s.checkRunning(); err != nil {
		return "", err
	}
	return s.execRouted(ctx, opts.prio, "open_script", map[string]any{
		"filePath": filePath,
	}, mcpCmdTimeout, RiskAuto, "open_script:"+filePath, opts.source)
}

// toolScriptWrite 写脚本(审批在槽外,执行在车道内)。
func (s *McpService) toolScriptWrite(ctx context.Context, opts execOpts, filePath, content string) (string, error) {
	if err := s.checkRunning(); err != nil {
		return "", err
	}
	preview := previewContent(content)
	if s.cfg.McpMode() == McpModeManual || opts.forceManual {
		// 脚本写入的永久授权: 命令为 script_write:<path>,路径集合为 [path]
		grantCmd := "script_write:" + filePath
		if s.cfg.McpGrantsEnabled() && s.grants.Match(grantCmd, []string{filePath}) != "" {
			s.audit.Append(opts.source, "confirm", "script_write", filePath, preview, RiskConfirm, "granted", false)
		} else {
			dec, err := s.requestApproval(opts.source, "script_write", filePath+" ("+fmt.Sprintf("%d 字节", len(content))+")", preview, RiskConfirm, grantCmd, []string{filePath})
			if err != nil {
				return "", err
			}
			if !dec.Approved {
				s.audit.Append(opts.source, "confirm", "script_write", filePath, preview, RiskConfirm, "denied", true)
				return "", fmt.Errorf("用户拒绝了本次写入")
			}
			s.audit.Append(opts.source, "confirm", "script_write", filePath, preview, RiskConfirm, "approved", true)
		}
	} else {
		s.audit.Append(opts.source, "confirm", "script_write", filePath, preview, RiskConfirm, "approved", false)
	}
	return s.execRouted(ctx, opts.prio, "script_write", map[string]any{
		"filePath": filePath,
		"content":  content,
	}, mcpCmdTimeout, RiskConfirm, "script_write:"+filePath, opts.source)
}

// toolCloseTab 关闭标签页(审批在槽外,执行在车道内)。
func (s *McpService) toolCloseTab(ctx context.Context, opts execOpts, tabID string) (string, error) {
	if err := s.checkRunning(); err != nil {
		return "", err
	}
	if s.cfg.McpMode() == McpModeManual || opts.forceManual {
		dec, err := s.requestApproval(opts.source, "close_tab", tabID, "关闭标签页 "+tabID, RiskConfirm, "", nil)
		if err != nil {
			return "", err
		}
		if !dec.Approved {
			s.audit.Append(opts.source, "confirm", "close_tab", tabID, "关闭标签页", RiskConfirm, "denied", true)
			return "", fmt.Errorf("用户拒绝了关闭操作")
		}
		s.audit.Append(opts.source, "confirm", "close_tab", tabID, "关闭标签页", RiskConfirm, "approved", true)
	}
	return s.execRouted(ctx, opts.prio, "close_tab", map[string]any{
		"tabId": tabID,
	}, mcpCmdTimeout, RiskConfirm, "close_tab:"+tabID, opts.source)
}

// ==================== 内嵌智能体执行入口(P1 优先级) ====================

// ExecuteEmbedded 内嵌智能体工具执行入口(默认行为,不强制审批)。
func (s *McpService) ExecuteEmbedded(ctx context.Context, name string, argsJSON string) (string, error) {
	return s.ExecuteEmbeddedOpts(ctx, name, argsJSON, execOpts{
		prio:   mcpPrioEmbedded,
		source: mcpSrcEmbedded,
	})
}

// ExecuteEmbeddedOpts 内嵌智能体工具执行入口(带执行选项)。
// 与外部 HTTP 客户端走完全相同的分级/审批/审计/仲裁,仅优先级更高(P1)。
// 智能体手动模式传 forceManual=true(忽略全局 auto 设置强制审批);
// noPaste=true 时多行文本不再触发前端粘贴弹窗(审批弹窗已一次性授权)。
// 全程计入忙碌状态(含审批等待与输出回读),驱动前端"执行中"遮罩。
func (s *McpService) ExecuteEmbeddedOpts(ctx context.Context, name string, argsJSON string, opts execOpts) (string, error) {
	s.beginActivity()
	defer s.endActivity()
	var args map[string]any
	if argsJSON != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", fmt.Errorf("参数解析失败: %v", err)
		}
	}
	getStr := func(k string) string {
		if v, ok := args[k].(string); ok {
			return v
		}
		return ""
	}
	switch name {
	case "list_sessions":
		if err := s.checkRunning(); err != nil {
			return "", err
		}
		sessions := flattenTree(s.sessionFile.GetTree())
		out := map[string]any{"sessions": sessions}
		s.audit.Append(opts.source, "info", "list_sessions", "", fmt.Sprintf("%d 个会话", len(sessions)), RiskAuto, "executed", false)
		data, _ := json.Marshal(out)
		return string(data), nil
	case "list_tabs":
		return s.toolListTabs(ctx, opts.source)
	case "open_session":
		return s.toolOpenSession(ctx, opts, getStr("session_path"))
	case "terminal_send":
		return s.toolTerminalSend(ctx, opts, getStr("tab_id"), getStr("text"))
	case "terminal_read":
		if err := s.checkRunning(); err != nil {
			return "", err
		}
		out := map[string]any{"output": s.readOutput(getStr("tab_id"))}
		data, _ := json.Marshal(out)
		return string(data), nil
	case "batch_execute":
		var cmds []string
		if raw, ok := args["commands"].([]any); ok {
			for _, c := range raw {
				if s2, ok := c.(string); ok {
					cmds = append(cmds, s2)
				}
			}
		}
		interval := 0
		if v, ok := args["interval_ms"].(float64); ok {
			interval = int(v)
		}
		return s.toolBatchExecute(ctx, opts, getStr("tab_id"), cmds, interval)
	case "open_script":
		return s.toolOpenScript(ctx, opts, getStr("file_path"))
	case "script_write":
		return s.toolScriptWrite(ctx, opts, getStr("file_path"), getStr("content"))
	case "close_tab":
		return s.toolCloseTab(ctx, opts, getStr("tab_id"))
	case "search_logs":
		if err := s.checkRunning(); err != nil {
			return "", err
		}
		includeContent := false
		if v, ok := args["include_content"].(bool); ok {
			includeContent = v
		}
		limit := 0
		if v, ok := args["limit"].(float64); ok {
			limit = int(v)
		}
		return s.toolSearchLogs(opts.source, getStr("query"), getStr("protocol"), includeContent, limit)
	case "log_detail":
		if err := s.checkRunning(); err != nil {
			return "", err
		}
		tailLines := 0
		if v, ok := args["tail_lines"].(float64); ok {
			tailLines = int(v)
		}
		return s.toolLogDetail(opts.source, getStr("log_id"), tailLines)
	default:
		return "", fmt.Errorf("未知工具: %s", name)
	}
}

// EmbeddedToolDefs 返回内嵌智能体可用工具定义(name+description+参数 schema JSON),
// 供 AgentService 构造 openai tools 列表(与 MCP 注册共享同一描述)。
func (s *McpService) EmbeddedToolDefs() string {
	type param struct {
		Type        string   `json:"type"`
		Description string   `json:"description,omitempty"`
		Items       *param   `json:"items,omitempty"`
		Enum        []string `json:"enum,omitempty"`
	}
	type fnDef struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  map[string]any  `json:"parameters"`
	}
	str := func(desc string) param { return param{Type: "string", Description: desc} }
	arr := func(desc string) param { return param{Type: "array", Description: desc, Items: &param{Type: "string"}} }
	intp := func(desc string) param { return param{Type: "integer", Description: desc} }
	boolp := func(desc string) param { return param{Type: "boolean", Description: desc} }
	obj := func(props map[string]param, required ...string) map[string]any {
		raw, _ := json.Marshal(props)
		var p map[string]any
		json.Unmarshal(raw, &p)
		return map[string]any{"type": "object", "properties": p, "required": required}
	}
	defs := []fnDef{
		{Name: "list_sessions", Description: "列出全部已保存的会话(不含任何密码或密钥)。返回路径供 open_session 使用。", Parameters: obj(map[string]param{})},
		{Name: "list_tabs", Description: "列出当前打开的全部标签页(终端与脚本编辑器),含状态与标签页 ID。", Parameters: obj(map[string]param{})},
		{Name: "open_session", Description: "在标签页中打开终端会话(SSH/Telnet/串口)。已打开则定位激活该标签页。界面会同步跳转,与用户手动打开完全一致。", Parameters: obj(map[string]param{"session_path": str("会话相对路径(list_sessions 返回的 path)")}, "session_path")},
		{Name: "terminal_send", Description: "向终端标签页输入文本。输入会显示在终端上,与用户手动输入完全一致;目标标签页会被激活。单行命令会自动等待并返回执行输出(output 字段),无需再调 terminal_read;多行输入与用户粘贴相同逻辑。", Parameters: obj(map[string]param{"tab_id": str("目标终端标签页 ID"), "text": str("要输入的文本。单行为命令(自动带回执行输出);多行视为批量输入,与用户多行粘贴相同逻辑")}, "tab_id", "text")},
		{Name: "terminal_read", Description: "读取终端标签页自上次调用以来的新增输出(已剥离控制序列的纯文本)。首次调用返回最近的历史输出。", Parameters: obj(map[string]param{"tab_id": str("目标终端标签页 ID")}, "tab_id")},
		{Name: "batch_execute", Description: "批量顺序执行命令(不切换标签页,前台界面不跳转)。整批共用一次授权确认;任一命令为绝对危险则整批拒绝。适合巡检等只读命令序列。", Parameters: obj(map[string]param{"tab_id": str("目标终端标签页 ID"), "commands": arr("要顺序执行的命令列表(每条单行)"), "interval_ms": intp("命令间隔毫秒(默认 300,最小 50)")}, "tab_id", "commands")},
		{Name: "open_script", Description: "在脚本编辑器标签页中打开脚本文件。已打开则定位激活。与用户手动打开完全一致。", Parameters: obj(map[string]param{"file_path": str("脚本文件路径")}, "file_path")},
		{Name: "script_write", Description: "向脚本编辑器写入内容(编辑器会显示为未保存/自动保存状态,与用户手动编辑完全一致)。属常规危险操作,默认需用户授权。", Parameters: obj(map[string]param{"file_path": str("目标脚本文件路径"), "content": str("要写入的完整内容")}, "file_path", "content")},
		{Name: "close_tab", Description: "关闭指定标签页。与用户手动关闭完全一致(未保存脚本会弹确认)。", Parameters: obj(map[string]param{"tab_id": str("要关闭的标签页 ID")}, "tab_id")},
		{Name: "search_logs", Description: "搜索会话自动日志。按关键字匹配会话标题/主机/用户名,可选协议过滤(ssh/telnet/serial/shell)与正文检索。返回日志 ID 供 log_detail 使用。", Parameters: obj(map[string]param{"query": str("关键字,留空则列出最近的日志"), "protocol": str("可选协议过滤:ssh/telnet/serial/shell,留空不过滤"), "include_content": boolp("是否同时在日志正文中检索命中行"), "limit": intp("最多返回条数(默认 20)")})},
		{Name: "log_detail", Description: "查看自动日志详情:元数据(协议/主机/端口/用户名/起止时间/行数字节数)与正文尾部内容。", Parameters: obj(map[string]param{"log_id": str("日志 ID(search_logs 返回的 id)"), "tail_lines": intp("返回正文末尾行数(默认 500)")}, "log_id")},
	}
	data, _ := json.Marshal(defs)
	return string(data)
}

// checkRunning 服务可用性检查(拒绝 stopped/paused 请求)。
func (s *McpService) checkRunning() error {
	s.mu.Lock()
	state := s.state
	s.mu.Unlock()
	switch state {
	case mcpStateRunning:
		return nil
	case mcpStatePaused:
		return fmt.Errorf("MCP 已被用户挂起(MCP_PAUSED)。请用户在悬浮球或设置面板中恢复后再试")
	default:
		return fmt.Errorf("MCP 服务未启动")
	}
}

// mcpSessionInfo list_sessions 输出条目(仅非敏感字段)。
type mcpSessionInfo struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
}

// mcpTabInfo list_tabs 输出条目。
type mcpTabInfo struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Kind        string `json:"kind"`
	Protocol    string `json:"protocol"`
	Status      string `json:"status"`
	SessionPath string `json:"sessionPath"`
}

// flattenTree 递归展开会话树(仅文件节点)。
func flattenTree(treeJSON string) []mcpSessionInfo {
	var nodes []*TreeNode
	if err := json.Unmarshal([]byte(treeJSON), &nodes); err != nil {
		return nil
	}
	var out []mcpSessionInfo
	var walk func(list []*TreeNode)
	walk = func(list []*TreeNode) {
		for _, n := range list {
			if n.IsDir {
				walk(n.Children)
			} else {
				out = append(out, mcpSessionInfo{Path: n.Path, Name: n.Name, Protocol: n.Protocol})
			}
		}
	}
	walk(nodes)
	return out
}

// loadSessionMeta 读取会话元数据(LoadSession 已脱敏,不含密码)。
func (s *McpService) loadSessionMeta(sessionPath string) (map[string]any, error) {
	raw, err := s.sessionFile.LoadSession(sessionPath)
	if err != nil {
		return nil, fmt.Errorf("会话不存在或不可读: %s", sessionPath)
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return nil, fmt.Errorf("会话不存在或不可读: %s", sessionPath)
	}
	if meta == nil || meta["protocol"] == nil {
		return nil, fmt.Errorf("会话数据异常: %s", sessionPath)
	}
	return meta, nil
}

// ==================== 命令路由与审批 ====================

// execRouted 仲裁 + 路由执行(激活目标标签页 + 可视时延)。
// 所有标签页写操作经此进入全局串行车道;审批已在槽外完成。
func (s *McpService) execRouted(ctx context.Context, prio int, cmdType string, payload map[string]any, timeout time.Duration, risk string, subject string, source string) (string, error) {
	p := make(map[string]any, len(payload)+2)
	for k, v := range payload {
		p[k] = v
	}
	p["activateTab"] = true
	p["opDelayMs"] = s.cfg.McpOpDelayMs()
	var res string
	err := s.arbitrate(ctx, prio, func() error {
		var e error
		res, e = s.routeCommand(ctx, cmdType, p, timeout, risk, subject, source)
		return e
	})
	return res, err
}

// execRoutedRaw 仲裁 + 路由执行(不激活标签页,前台不跳转)。
// batch_execute 等后台化操作使用。
func (s *McpService) execRoutedRaw(ctx context.Context, prio int, cmdType string, payload map[string]any, timeout time.Duration, risk string, subject string, source string) (string, error) {
	p := make(map[string]any, len(payload)+2)
	for k, v := range payload {
		p[k] = v
	}
	p["activateTab"] = false
	var res string
	err := s.arbitrate(ctx, prio, func() error {
		var e error
		res, e = s.routeCommand(ctx, cmdType, p, timeout, risk, subject, source)
		return e
	})
	return res, err
}

// routeCommand 下发命令到前端并等待回执。
func (s *McpService) routeCommand(ctx context.Context, cmdType string, payload map[string]any, timeout time.Duration, risk string, subject string, source string) (string, error) {
	s.mu.Lock()
	if s.state != mcpStateRunning {
		s.mu.Unlock()
		return "", fmt.Errorf("MCP 已被挂起或停止")
	}
	s.reqSeq++
	reqID := fmt.Sprintf("r-%d", s.reqSeq)
	ch := make(chan mcpCmdResult, 1)
	s.pendingCmd[reqID] = ch
	s.mu.Unlock()

	cmdPayload := map[string]any{
		"requestId": reqID,
		"type":      cmdType,
		"payload":   payload,
	}
	s.emit("mcp-command", cmdPayload)

	// 等待前端回执 / 超时 / 挂起取消
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case res := <-ch:
		if res.Err != "" {
			return "", fmt.Errorf("%s", res.Err)
		}
		return res.Result, nil
	case <-timer.C:
		s.mu.Lock()
		delete(s.pendingCmd, reqID)
		s.mu.Unlock()
		s.audit.Append(source, "error", cmdType, subject, "等待前端执行超时", risk, "timeout", false)
		return "", fmt.Errorf("命令执行超时(%s)", cmdType)
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.pendingCmd, reqID)
		s.mu.Unlock()
		return "", fmt.Errorf("客户端已取消请求")
	}
}

// requestApproval 发起人工审批并等待决策(手动模式)。
// command/paths 非空时,用户可选"永久授权"(命令+路径写入授权库)。
func (s *McpService) requestApproval(source, action, summary, detail, risk, command string, paths []string) (mcpApprovalDecision, error) {
	s.mu.Lock()
	if s.state != mcpStateRunning {
		s.mu.Unlock()
		return mcpApprovalDecision{}, fmt.Errorf("MCP 已被挂起或停止")
	}
	s.apprSeq++
	ap := &mcpApproval{
		ID:        fmt.Sprintf("ap-%d", s.apprSeq),
		Action:    action,
		Summary:   summary,
		Detail:    detail,
		Risk:      risk,
		Command:   command,
		Paths:     paths,
		ExpiresAt: time.Now().Add(mcpApprovalTimeout),
		ch:        make(chan mcpApprovalDecision, 1),
	}
	s.approvals[ap.ID] = ap
	s.mu.Unlock()

	s.emit("mcp-approval-requested", ap)
	s.audit.Append(source, "confirm", action, summary, detail, risk, "pending", false)

	timer := time.NewTimer(mcpApprovalTimeout)
	defer timer.Stop()
	select {
	case dec := <-ap.ch:
		s.mu.Lock()
		delete(s.approvals, ap.ID)
		s.mu.Unlock()
		s.emitStatus()
		// 批准且勾选永久授权: 命令+路径写入授权库(仅 confirm 级)
		if dec.Approved && dec.Permanent && command != "" && s.cfg.McpGrantsEnabled() {
			if _, err := s.grants.Add(command, paths); err != nil {
				// 授权库写满等失败不影响本次执行,仅记录
				s.audit.Append("system", "system", "grant_add", command, err.Error(), risk, "-", false)
			} else {
				s.audit.Append("system", "system", "grant_add", command, strings.Join(paths, ", "), risk, "granted", true)
			}
		}
		return dec, nil
	case <-timer.C:
		s.mu.Lock()
		delete(s.approvals, ap.ID)
		s.mu.Unlock()
		s.emit("mcp-approval-removed", map[string]any{"id": ap.ID, "reason": "timeout"})
		s.emitStatus()
		s.audit.Append(source, "confirm", action, summary, detail, risk, "timeout", false)
		return mcpApprovalDecision{}, fmt.Errorf("等待用户授权超时(60 秒),已自动拒绝")
	}
}

// ==================== 前端绑定方法 ====================

// GetMcpStatus 返回 MCP 服务状态 JSON。
func (s *McpService) GetMcpStatus() string {
	data, _ := json.Marshal(s.statusMap())
	return string(data)
}

// SetMcpEnabled 开关 MCP 服务。
func (s *McpService) SetMcpEnabled(enabled bool) string {
	s.cfg.SetMcpEnabled(enabled)
	if enabled {
		if err := s.Start(); err != nil {
			s.audit.Append("system", "error", "start", "", err.Error(), "-", "-", false)
			return marshalJSON(map[string]string{"error": err.Error()})
		}
	} else {
		s.Stop()
	}
	data, _ := json.Marshal(s.statusMap())
	return string(data)
}

// SetMcpMode 设置审批模式(manual / auto)。
func (s *McpService) SetMcpMode(mode string) string {
	if mode != McpModeManual && mode != McpModeAuto {
		mode = McpModeManual
	}
	s.cfg.SetMcpMode(mode)
	s.audit.Append("system", "system", "mode", "", "审批模式切换为 "+mode, "-", "-", true)
	data, _ := json.Marshal(s.statusMap())
	return string(data)
}

// McpPause 挂起 MCP(用户手动)。
func (s *McpService) McpPause() string {
	s.Pause(true)
	data, _ := json.Marshal(s.statusMap())
	return string(data)
}

// McpResume 恢复 MCP(仅用户手动恢复)。
func (s *McpService) McpResume() string {
	s.Resume()
	data, _ := json.Marshal(s.statusMap())
	return string(data)
}

// McpResolveApproval 前端回执审批决策。
// permanent=true 且 approved=true 时,命令+路径写入永久授权库。
func (s *McpService) McpResolveApproval(approvalID string, approved bool, permanent bool) string {
	s.mu.Lock()
	ap := s.approvals[approvalID]
	if ap != nil {
		delete(s.approvals, approvalID)
	}
	s.mu.Unlock()
	if ap == nil {
		return `{"ok":false}`
	}
	select {
	case ap.ch <- mcpApprovalDecision{Approved: approved, Permanent: permanent}:
	default:
	}
	return `{"ok":true}`
}

// McpResolveCommand 前端回执命令执行结果。
func (s *McpService) McpResolveCommand(requestID string, result string, errMsg string) string {
	s.mu.Lock()
	ch := s.pendingCmd[requestID]
	if ch != nil {
		delete(s.pendingCmd, requestID)
	}
	s.mu.Unlock()
	if ch == nil {
		return `{"ok":false}`
	}
	ch <- mcpCmdResult{Result: result, Err: errMsg}
	return `{"ok":true}`
}

// McpNotifyPreemption 用户键盘抢占: 立即挂起并取消全部 MCP 在途操作。
func (s *McpService) McpNotifyPreemption() string {
	s.mu.Lock()
	state := s.state
	if state != mcpStateRunning {
		s.mu.Unlock()
		return `{"ok":false}`
	}
	s.state = mcpStatePaused
	// 抢占: 全部在途命令以 USER_PREEMPTED 拒绝,审批视同拒绝
	for id, ch := range s.pendingCmd {
		select {
		case ch <- mcpCmdResult{Err: "USER_PREEMPTED: 用户已手动接管终端,本次操作被中断"}:
		default:
		}
		delete(s.pendingCmd, id)
	}
	for id, ap := range s.approvals {
		select {
		case ap.ch <- mcpApprovalDecision{}:
		default:
		}
		delete(s.approvals, id)
	}
	s.mu.Unlock()

	// 用户抢占:开始捕获挂起期间的终端活动(供智能体感知用户手动操作)
	s.beginSuspendCapture()
	s.audit.Append("system", "system", "preempt", "", "检测到用户键盘输入,MCP 已自动挂起(用户优先)", "-", "preempted", true)
	s.emitStatus()
	return `{"ok":true}`
}

// GetMcpAuditLog 查询审计日志(offset 起始下标,负数从尾部倒数)。
func (s *McpService) GetMcpAuditLog(offset int, limit int) string {
	entries := s.audit.Query(offset, limit)
	data, _ := json.Marshal(entries)
	return string(data)
}

// ResetMcpToken 重置访问令牌(旧令牌立即失效)。
func (s *McpService) ResetMcpToken() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return `{"error":"生成失败"}`
	}
	plain := hex.EncodeToString(buf)
	enc, err := encryptSecret(plain)
	if err != nil {
		return `{"error":"加密失败"}`
	}
	s.cfg.SetMcpTokenEnc(enc)
	s.mu.Lock()
	s.token = plain
	s.mu.Unlock()
	s.audit.Append("system", "system", "reset_token", "", "访问令牌已重置", "-", "-", true)
	data, _ := json.Marshal(s.statusMap())
	return string(data)
}

// ==================== 永久授权管理(前端绑定) ====================

// GetMcpGrants 返回永久授权规则列表 JSON。
func (s *McpService) GetMcpGrants() string {
	data, _ := json.Marshal(s.grants.List())
	return string(data)
}

// RemoveMcpGrant 删除指定永久授权规则。
func (s *McpService) RemoveMcpGrant(id string) string {
	ok := s.grants.Remove(id)
	if ok {
		s.audit.Append("system", "system", "grant_remove", id, "永久授权规则已删除", "-", "-", true)
	}
	return fmt.Sprintf(`{"ok":%t}`, ok)
}

// ClearMcpGrants 清空全部永久授权规则。
func (s *McpService) ClearMcpGrants() string {
	s.grants.Clear()
	s.audit.Append("system", "system", "grant_clear", "", "全部永久授权规则已清空", "-", "-", true)
	return `{"ok":true}`
}

// SetMcpExecTuning 持久化执行参数(时延/批量间隔/授权开关/审计保留/读取上限)。
func (s *McpService) SetMcpExecTuning(opDelayMs int, batchIntervalMs int, grantsEnabled bool, auditRetentionDays int, terminalReadMax int) string {
	res := s.cfg.SetMcpExecTuning(opDelayMs, batchIntervalMs, grantsEnabled, auditRetentionDays, terminalReadMax)
	s.audit.Append("system", "system", "exec_tuning", "", fmt.Sprintf("时延=%dms 批量间隔=%dms 授权开关=%t", opDelayMs, batchIntervalMs, grantsEnabled), "-", "-", true)
	return res
}

// SetMcpCustomRules 持久化自定义分级规则并即时生效。
func (s *McpService) SetMcpCustomRules(jsonStr string) string {
	res := s.cfg.SetMcpCustomRules(jsonStr)
	if !strings.Contains(res, `"error"`) {
		RefreshCustomRules(s.cfg.McpCustomRules())
		s.audit.Append("system", "system", "custom_rules", "", "自定义分级规则已更新并生效", "-", "-", true)
	}
	return res
}

// SetMcpBallPos 持久化悬浮球位置。
func (s *McpService) SetMcpBallPos(x int, y int) string {
	s.cfg.SetMcpBallPos(x, y)
	return `{"ok":true}`
}

// ==================== 辅助 ====================

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func summarizeText(s string) string {
	line := firstLine(strings.ReplaceAll(s, "\r\n", "\n"))
	if n := strings.Count(s, "\n"); n > 0 {
		return fmt.Sprintf("(多行 %d 行) %s", n+1, truncateUtf8(line, 80))
	}
	return truncateUtf8(line, 120)
}

func previewContent(s string) string {
	return truncateUtf8(s, 400)
}
