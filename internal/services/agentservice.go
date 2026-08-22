package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// 内嵌智能体服务: OpenAI 兼容对话 + 工具调用(经 McpService.ExecuteEmbeddedOpts
// 走与外部智能体完全相同的分级/审批/审计/仲裁,优先级 P1)。
//
// 绝对特性:
//   - 全局串行会话: 同一时间仅一个会话轮次运行(跨全部会话)
//   - 同会话执行中收到新消息 → 挂起(容量1,新覆盖旧);挂起可"立即发送"
//     (打断当前轮次)或等待当前轮次结束自动执行;跨会话发送被拒绝
//   - 用户可随时强中断(取消 ctx,LLM 流与在途 MCP 操作一并终止;中断同时清空挂起)
//   - 三态权限: plan(只读+方案) / manual(危险操作用户审批) / auto(自动执行,
//     blocked 级仍然绝对拦截)
//   - 会话 jsonl 持久化 + 归档;待办清单(update_todo 工具)悬浮展示
//   - 多 AI 档案: 连接身份(提供商/端点/密钥/模型)按档案保存,切换下一轮生效

const (
	agentPermPlan   = "plan"
	agentPermManual = "manual"
	agentPermAuto   = "auto"

	agentToolResultMax = 8000 // 工具结果进入 LLM 上下文的字符上限(有界)
)

// agentPlanReadableTools 计划模式可用工具白名单(只读)。
var agentPlanReadableTools = map[string]bool{
	"list_sessions": true,
	"list_tabs":     true,
	"terminal_read": true,
}

// agentPendingMsg 挂起消息(容量1:新消息覆盖旧消息)。
type agentPendingMsg struct {
	SessionID string `json:"sessionId"`
	Text      string `json:"text"`
}

// AgentService 智能体服务。
type AgentService struct {
	app   *application.App
	cfg   *ConfigService
	mcp   *McpService
	store *AgentStore

	mu             sync.Mutex
	running        string             // 当前运行中的会话 ID(空 = 空闲)
	runCancel      context.CancelFunc // 强中断
	step           int                // 当前步数(0-based 显示用)
	pending        *agentPendingMsg   // 挂起消息(容量1)
	ctxUsed        int64              // 当前上下文占用(最近一次 LLM 调用 prompt+completion token)
	regenerateAfter string            // 中断后需重跑的会话 ID(刷新对话)
}

// NewAgentService 创建服务。
func NewAgentService(cfg *ConfigService, mcp *McpService) *AgentService {
	return &AgentService{
		cfg:   cfg,
		mcp:   mcp,
		store: NewAgentStore(DataDir()),
	}
}

// SetApp 注入 Wails 应用实例(wireServices 调用)。
func (s *AgentService) SetApp(app *application.App) {
	s.app = app
}

// emit 安全发送事件(锁外调用)。
func (s *AgentService) emit(name string, payload any) {
	if s.app == nil {
		return
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	s.app.Event.Emit(name, string(data))
}

// emitEvent 推送持久化事件到前端。
func (s *AgentService) emitEvent(sessionID string, ev AgentEvent) {
	s.emit("agent-event", map[string]any{"sessionId": sessionID, "event": ev})
}

// agentCtxWindow 档案上下文窗口(0=默认 128K)。
func agentCtxWindow(p AgentProfile) int {
	if p.ContextWindow >= 4096 {
		return p.ContextWindow
	}
	return 128000
}

// statusMap 状态快照。
func (s *AgentService) statusMap() map[string]any {
	s.mu.Lock()
	running, step, pending, ctxUsed := s.running, s.step, s.pending, s.ctxUsed
	s.mu.Unlock()
	acfg := s.cfg.AgentCfg()
	profile := s.cfg.ActiveAgentProfile()
	var pm any
	if pending != nil {
		pm = pending
	}
	lang := "zh-CN"
	if l := s.cfg.GetLanguage(); l != "" {
		lang = l
	}
	return map[string]any{
		"enabled":          acfg.Enabled,
		"permMode":         normalizeAgentPerm(acfg.PermMode),
		"maxSteps":         acfg.MaxSteps,
		"running":          running != "",
		"sessionId":        running,
		"step":             step,
		"pending":          pm,
		"activeProfileId":  profile.ID,
		"activeProfile":    profile.Name,
		"model":            profile.Model,
		"language":         lang,
		"ctxUsed":          ctxUsed,             // 当前上下文占用(最近一次 LLM 调用)
		"ctxWindow":        agentCtxWindow(profile), // 模型上下文窗口上限
	}
}

func normalizeAgentPerm(mode string) string {
	switch mode {
	case agentPermPlan, agentPermManual, agentPermAuto:
		return mode
	default:
		return agentPermManual
	}
}

// emitStatus 推送状态变更。
func (s *AgentService) emitStatus() {
	s.emit("agent-status-changed", s.statusMap())
}

// emitPending 推送挂起状态变更。
func (s *AgentService) emitPending() {
	s.mu.Lock()
	pending := s.pending
	s.mu.Unlock()
	var pm any
	if pending != nil {
		pm = pending
	}
	s.emit("agent-pending-changed", pm)
}

// appendEvent 追加事件(持久化 + 推送)。
func (s *AgentService) appendEvent(sessionID string, ev AgentEvent) AgentEvent {
	saved, err := s.store.Append(sessionID, ev)
	if err != nil {
		s.emit("agent-error", map[string]any{"sessionId": sessionID, "message": "事件持久化失败: " + err.Error()})
		return ev
	}
	s.emitEvent(sessionID, saved)
	return saved
}

// agentErrJSON 错误响应(带 code,前端按 code 做 i18n;error 为中文兜底)。
func agentErrJSON(code, msg string) string {
	return fmt.Sprintf(`{"code":%q,"error":%q}`, code, msg)
}

// ==================== 内置运维技能 ====================

// AgentSkill 技能定义(系统提示词注入片段)。
type AgentSkill struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Desc string `json:"desc"`
	Prompt string `json:"prompt"` // 注入系统提示词的方法论文本
}

// agentBuiltinSkills 内置运维专家技能(只读,不可删除)。
var agentBuiltinSkills = []AgentSkill{
	{
		ID:   "inspect",
		Name: "系统巡检",
		Desc: "CPU/内存/磁盘/服务/日志标准检查流,产出巡检报告",
		Prompt: "【系统巡检技能】按以下流程执行巡检:\n1. 系统概览: uname/uptime/OS 版本\n2. 资源水位: CPU 使用率(top -bn1)、内存(free -m)、磁盘(df -h,标记>85% 的分区)、负载\n3. 服务状态: 关键服务运行状态(systemctl --failed 与业务服务)\n4. 最近日志: 系统日志错误扫描(journalctl -p err -n 50 或 /var/log/messages)\n5. 产出结构化巡检报告: 正常项/风险项/建议项三段,风险标注严重程度",
	},
	{
		ID:   "troubleshoot",
		Name: "故障诊断",
		Desc: "分层排查(网络→服务→日志→配置),先假设后验证",
		Prompt: "【故障诊断技能】遵循分层排查方法论:\n1. 明确故障现象与影响范围(何时开始/影响什么/是否可复现)\n2. 分层排查: 网络层(ping/telnet/路由) → 服务层(进程/端口/状态) → 应用层(日志/错误码) → 配置层(近期变更)\n3. 每一步先提出假设,再用命令验证,记录证据;验证失败则修正假设\n4. 定位根因后给出: 根因结论(附证据链)、修复方案(分临时/永久)、预防措施\n禁止无证据的猜测性结论",
	},
	{
		ID:   "change",
		Name: "变更执行",
		Desc: "变更前快照→分步执行→验证→回滚预案(强制 todo)",
		Prompt: "【变更执行技能】执行任何变更操作必须遵循:\n1. 变更前: 明确变更内容与影响面;记录当前状态快照(配置备份、运行状态);必须先用 update_todo 制定分步计划\n2. 变更中: 严格按 todo 逐步执行,每步完成后验证结果再进行下一步\n3. 变更后: 功能验证(服务可用性/业务连通性)、状态对比\n4. 必须预先准备回滚预案: 每个写操作前说明如何回滚;异常立即停止并回滚,不盲目重试",
	},
	{
		ID:   "security",
		Name: "安全审计",
		Desc: "账号/权限/端口/进程/计划任务/SSH 配置检查",
		Prompt: "【安全审计技能】按以下维度检查:\n1. 账号安全: 异常账号/空密码账号/UID0 重复/sudoers 变更\n2. SSH 配置: 是否禁用 root 直登/密码认证/默认端口/AuthorizedKeys\n3. 网络暴露: 监听端口清单(ss -tlnp),标记非必要对外端口;防火墙规则\n4. 可疑进程与计划任务: 异常进程/近期新增 crontab/systemd timer\n5. 认证痕迹: last/wtmp、失败登录统计\n产出: 风险清单(高/中/低)+加固建议;只读检查,不擅自修改安全配置",
	},
	{
		ID:   "network",
		Name: "网络诊断",
		Desc: "连通性/路由/端口/防火墙定位链路",
		Prompt: "【网络诊断技能】链路定位顺序:\n1. 连通性: ping 目标(带大小/次数)、DNS 解析(nslookup/dig)\n2. 路由: traceroute 定位断点层级;本机路由表(route/ip route)\n3. 端口: 本端监听确认(ss/telnet/nc)、远端端口探测\n4. 防火墙: iptables/nftables/firewalld 规则核对,云环境注意安全组\n5. 抓包佐证(必要时): tcpdump 定定关键握手\n结论必须给出: 断点位置(哪一层)+证据+修复建议",
	},
}

// agentSkillByID 按 ID 查找内置技能。
func agentSkillByID(id string) *AgentSkill {
	for i := range agentBuiltinSkills {
		if agentBuiltinSkills[i].ID == id {
			return &agentBuiltinSkills[i]
		}
	}
	return nil
}

// ==================== 系统提示词与工具集 ====================

// agentSystemPrompt 构建系统提示词(权限模式 + 会话技能 + 界面语言)。
func agentSystemPrompt(permMode, lang string, skills []string) string {
	var b strings.Builder
	if lang == "en-US" {
		b.WriteString("You are the embedded ops agent of AceShell terminal app, operating terminal sessions and script editor via tools.\n")
		b.WriteString("Rules:\n")
		b.WriteString("1. Observe before act: use list_tabs / terminal_read to learn current state first\n")
		b.WriteString("2. Every tool operation is visible to the user; do not retry rejected/timed-out operations as-is, stop and explain\n")
		b.WriteString("3. Prefer batch_execute for read-only inspection command sequences to reduce round-trips\n")
		b.WriteString("4. Record steps with update_todo when producing plans; keep it updated as you progress\n")
		b.WriteString("5. Reply in English, concise and professional; format replies in real Markdown syntax (headings/lists/tables/bold), and always wrap commands and code in fenced code blocks\n")
	} else {
		b.WriteString("你是 AceShell 终端应用的内嵌运维智能体,通过工具操作终端会话与脚本编辑器。\n")
		b.WriteString("工作规则:\n")
		b.WriteString("1. 先观察后行动: 用 list_tabs / terminal_read 了解当前状态,再决定下一步\n")
		b.WriteString("2. 每个工具操作对用户可见;被用户拒绝或超时的操作不要原样重试,应停下来向用户说明并询问\n")
		b.WriteString("3. 只读巡检类命令序列优先用 batch_execute 一次性执行,减少交互往返\n")
		b.WriteString("4. 产出方案或计划时先用 update_todo 记录步骤,随进度更新状态\n")
		b.WriteString("5. 回复使用中文,简洁专业;最终回答必须使用真实 Markdown 语法组织(标题/列表/表格/加粗),命令与代码一律用围栏代码块包裹\n")
	}
	switch permMode {
	case agentPermPlan:
		if lang == "en-US" {
			b.WriteString("\nCurrent mode: PLAN. Only read-only tools allowed (list_sessions / list_tabs / terminal_read / update_todo).\n")
			b.WriteString("All write operations are forbidden. Gather information, then produce an actionable step-by-step plan; wait for the user to switch to manual/auto mode before executing.\n")
		} else {
			b.WriteString("\n当前为【计划模式】: 仅允许只读工具(list_sessions / list_tabs / terminal_read / update_todo)。\n")
			b.WriteString("禁止执行任何写操作。你应当收集信息后产出可执行的分步实施方案,等用户切换到手动/自动模式后再执行。\n")
		}
	case agentPermManual:
		if lang == "en-US" {
			b.WriteString("\nCurrent mode: MANUAL. Dangerous operations trigger user approval; explain the purpose in one sentence before calling.\n")
		} else {
			b.WriteString("\n当前为【手动模式】: 危险操作会弹出用户审批,请在调用前用一句话说明操作目的,便于用户判断。\n")
		}
	case agentPermAuto:
		if lang == "en-US" {
			b.WriteString("\nCurrent mode: AUTO. Safe operations execute directly, but stay cautious; never run destructive commands.\n")
		} else {
			b.WriteString("\n当前为【自动模式】: 安全操作直接执行,但仍保持谨慎,绝不执行任何破坏性命令。\n")
		}
	}
	// 会话技能注入(按选择顺序)
	for _, id := range skills {
		if sk := agentSkillByID(id); sk != nil {
			b.WriteString("\n" + sk.Prompt + "\n")
		}
	}
	return b.String()
}

// agentBuildTools 构建工具集(MCP 工具定义 + update_todo;计划模式过滤写工具)。
func (s *AgentService) agentBuildTools(permMode string) []chatTool {
	var defs []struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	}
	if err := json.Unmarshal([]byte(s.mcp.EmbeddedToolDefs()), &defs); err != nil {
		defs = nil
	}
	var tools []chatTool
	for _, d := range defs {
		if permMode == agentPermPlan && !agentPlanReadableTools[d.Name] {
			continue
		}
		if d.Parameters == nil {
			d.Parameters = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		tools = append(tools, chatTool{
			Type:     "function",
			Function: chatToolFn{Name: d.Name, Description: d.Description, Parameters: d.Parameters},
		})
	}
	// update_todo: 待办清单(所有模式可用)
	tools = append(tools, chatTool{
		Type: "function",
		Function: chatToolFn{
			Name:        "update_todo",
			Description: "更新当前任务的待办清单(全量覆盖)。制定计划或进度变化时调用,用户会实时看到清单。",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"todos": map[string]any{
						"type":        "array",
						"description": "完整待办清单(全量覆盖,不是增量)",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"content": map[string]any{"type": "string", "description": "待办内容(一句话)"},
								"status":  map[string]any{"type": "string", "enum": []string{"pending", "in_progress", "done"}, "description": "状态"},
							},
							"required": []string{"content", "status"},
						},
					},
				},
				"required": []string{"todos"},
			},
		},
	})
	return tools
}

// agentBuildContext 从事件构建 LLM 消息上下文(截断到 maxEvents,过滤孤儿工具结果)。
func agentBuildContext(events []AgentEvent, maxEvents int) []chatMessage {
	if len(events) > maxEvents {
		events = events[len(events)-maxEvents:]
	}
	// 第一遍: 收集窗口内有效的工具调用 ID(tool_result 必须有对应 tool_call,
	// 否则 OpenAI 兼容接口报错)
	validCalls := make(map[string]bool, len(events))
	for _, e := range events {
		for _, tc := range e.ToolCalls {
			validCalls[tc.ID] = true
		}
	}
	var msgs []chatMessage
	for _, e := range events {
		switch e.Kind {
		case "message":
			switch e.Role {
			case "user":
				msgs = append(msgs, chatMessage{Role: "user", Content: e.Content})
			case "assistant":
				msgs = append(msgs, chatMessage{Role: "assistant", Content: e.Content})
			case "system":
				// 中断/上限等系统提示: 以 system 身份进入上下文,让模型知晓
				msgs = append(msgs, chatMessage{Role: "system", Content: e.Content})
			}
		case "tool_call":
			m := chatMessage{Role: "assistant"}
			if e.Content != "" {
				m.Content = e.Content
			}
			for _, tc := range e.ToolCalls {
				m.ToolCalls = append(m.ToolCalls, chatToolCall{
					ID:       tc.ID,
					Type:     "function",
					Function: chatToolCallFunc{Name: tc.Name, Arguments: tc.Arguments},
				})
			}
			if len(m.ToolCalls) > 0 {
				msgs = append(msgs, m)
			}
		case "tool_result":
			if !validCalls[e.ToolCallID] {
				continue // 对应 tool_call 已被截断,跳过孤儿结果
			}
			msgs = append(msgs, chatMessage{Role: "tool", ToolCallID: e.ToolCallID, Content: e.Content})
		}
	}
	return msgs
}

// ==================== 运行循环 ====================

// AgentSend 发送用户消息并启动一轮对话。
// 全局串行: 同一时间仅一个会话运行;同会话执行中新消息挂起(覆盖旧挂起),
// 跨会话发送被拒绝。
func (s *AgentService) AgentSend(sessionID string, text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return agentErrJSON("agent.empty", "消息为空")
	}
	acfg := s.cfg.AgentCfg()
	if !acfg.Enabled {
		return agentErrJSON("agent.disabled", "智能体未启用,请先在设置中开启")
	}
	profile := s.cfg.ActiveAgentProfile()
	if _, encStored, keyErr := s.cfg.AgentApiKeyState(); keyErr != nil {
		if encStored {
			return agentErrJSON("agent.nokey", "API Key 解密失败: "+keyErr.Error())
		}
		return agentErrJSON("agent.nokey", "API Key 未配置: "+keyErr.Error())
	}
	if profile.BaseURL == "" || profile.Model == "" {
		return agentErrJSON("agent.noconf", "接口地址或模型未配置")
	}
	if !s.store.Exists(sessionID) {
		return agentErrJSON("agent.nosession", "会话不存在: "+sessionID)
	}

	s.mu.Lock()
	if s.running != "" {
		if s.running != sessionID {
			// 跨会话: 拒绝(保持全局串行,挂起只对当前执行会话有意义)
			cur := s.running
			s.mu.Unlock()
			return agentErrJSON("agent.busy", "会话 "+cur+" 正在执行中(全局串行),请等待完成或先中断")
		}
		// 同会话: 挂起(容量1,新覆盖旧)
		s.pending = &agentPendingMsg{SessionID: sessionID, Text: text}
		s.mu.Unlock()
		s.emitPending()
		return `{"ok":true,"pending":true}`
	}
	s.startTurnLocked(sessionID, text)
	s.mu.Unlock()
	return `{"ok":true}`
}

// startTurnLocked 启动新一轮(调用方持锁且已确认空闲)。
func (s *AgentService) startTurnLocked(sessionID string, text string) {
	runCtx, cancel := context.WithCancel(context.Background())
	s.running = sessionID
	s.runCancel = cancel
	s.step = 0
	s.ctxUsed = 0 // 新回合上下文占用清零(首轮调用后由真实 usage 回填)
	// 启动 goroutine 前先落盘用户消息(顺序保证)
	go func() {
		s.store.AutoTitle(sessionID, text) // 首条消息自动命名(默认标题时)
		s.appendEvent(sessionID, AgentEvent{Role: "user", Kind: "message", Content: text})
		s.store.Archive(sessionID, false) // 发送即自动解除归档
		s.emitStatus()
		s.runTurn(runCtx, sessionID)
	}()
}

// AgentPendingFlush 打断式立即发送挂起消息: 取消当前轮次,
// 轮次收尾时自动以挂起内容开新轮。
func (s *AgentService) AgentPendingFlush() string {
	s.mu.Lock()
	pending := s.pending
	cancel := s.runCancel
	running := s.running
	s.mu.Unlock()
	if pending == nil || cancel == nil || running == "" {
		return `{"ok":false}`
	}
	cancel()
	return `{"ok":true}`
}

// AgentPendingDiscard 丢弃挂起消息。
func (s *AgentService) AgentPendingDiscard() string {
	s.mu.Lock()
	s.pending = nil
	s.mu.Unlock()
	s.emitPending()
	return `{"ok":true}`
}

// runTurn 单轮对话循环: LLM 调用 → 工具执行 → 回填结果 → 重复,直到无工具调用或达步数上限。
// 结束时若有同会话挂起消息 → 自动开新轮(串行排队)。
func (s *AgentService) runTurn(ctx context.Context, sessionID string) {
	defer func() {
		if r := recover(); r != nil {
			s.appendEvent(sessionID, AgentEvent{Role: "system", Kind: "error", Content: fmt.Sprintf("内部错误: %v", r), Ok: false})
		}
		s.mu.Lock()
		s.running = ""
		s.runCancel = nil
		s.step = 0
		// 挂起自动执行: 同会话且有挂起 → 取出开新轮
		var next *agentPendingMsg
		if s.pending != nil && s.pending.SessionID == sessionID {
			next = s.pending
			s.pending = nil
		}
		// 刷新对话标记: 中断由重跑触发 → 裁剪尾部后重跑
		regen := s.regenerateAfter
		s.regenerateAfter = ""
		s.mu.Unlock()
		s.emitPending()
		s.emitStatus()
		if regen == sessionID {
			s.regenerateNow(sessionID)
			return
		}
		if next != nil {
			s.mu.Lock()
			s.startTurnLocked(sessionID, next.Text)
			s.mu.Unlock()
		}
	}()

	acfg := s.cfg.AgentCfg()
	profile := s.cfg.ActiveAgentProfile()
	permMode := normalizeAgentPerm(acfg.PermMode)
	client := newAgentClient(profile.BaseURL, s.cfg.AgentApiKeyPlain())
	tools := s.agentBuildTools(permMode)
	lang := "zh-CN"
	if l := s.cfg.GetLanguage(); l != "" {
		lang = l
	}
	sysMsg := chatMessage{Role: "system", Content: agentSystemPrompt(permMode, lang, s.store.Skills(sessionID))}

	// 本回合累计 token 消耗(所有 LLM 调用求和): 缓存未命中输入/缓存命中输入/输出
	var turnIn, turnCached, turnOut int64
	for step := 1; step <= acfg.MaxSteps; step++ {
		s.mu.Lock()
		s.step = step
		s.mu.Unlock()
		s.emitStatus()

		events, err := s.store.AllEvents(sessionID)
		if err != nil {
			s.appendEvent(sessionID, AgentEvent{Role: "assistant", Kind: "error", Content: err.Error(), Ok: false,
				TokensIn: turnIn, TokensCached: turnCached, TokensOut: turnOut})
			return
		}
		msgs := append([]chatMessage{sysMsg}, agentBuildContext(events, acfg.ContextMaxEvents)...)

		res, err := client.Chat(ctx, chatRequest{Model: profile.Model, Messages: msgs, Tools: tools}, func(delta string) {
			// 流式增量实时推送(不持久化;最终消息落盘后前端替换)
			s.emit("agent-stream", map[string]any{"sessionId": sessionID, "delta": delta})
		})
		if err != nil {
			// 回合终止(中断/出错): 并入 AI 回合(错误段),携带已累计 token 用量
			if ctx.Err() != nil {
				s.appendEvent(sessionID, AgentEvent{Role: "assistant", Kind: "error", Content: "本轮对话已被用户中断",
					TokensIn: turnIn, TokensCached: turnCached, TokensOut: turnOut})
			} else {
				s.appendEvent(sessionID, AgentEvent{Role: "assistant", Kind: "error", Content: err.Error(), Ok: false,
					TokensIn: turnIn, TokensCached: turnCached, TokensOut: turnOut})
			}
			return
		}

		if res.Usage.TotalTokens > 0 {
			turnIn += res.Usage.PromptTokens - res.Usage.CachedTokens
			turnCached += res.Usage.CachedTokens
			turnOut += res.Usage.CompletionTokens
			// 真实上下文占用 = 最近一次调用的输入+输出(模型实际看到的全部内容)
			s.mu.Lock()
			s.ctxUsed = res.Usage.PromptTokens + res.Usage.CompletionTokens
			s.mu.Unlock()
			s.emitStatus()
		}

		if len(res.ToolCalls) == 0 {
			// 最终回答(Kind=message;携带回合累计 token 用量)
			s.appendEvent(sessionID, AgentEvent{Role: "assistant", Kind: "message", Content: res.Content,
				TokensIn: turnIn, TokensCached: turnCached, TokensOut: turnOut})
			return
		}

		// 含工具调用的助手轮次
		s.appendEvent(sessionID, AgentEvent{
			Role:      "assistant",
			Kind:      "tool_call",
			Content:   res.Content,
			ToolCalls: res.ToolCalls,
		})
		for _, call := range res.ToolCalls {
			if ctx.Err() != nil {
				s.appendEvent(sessionID, AgentEvent{Role: "assistant", Kind: "error", Content: "本轮对话已被用户中断",
					TokensIn: turnIn, TokensCached: turnCached, TokensOut: turnOut})
				return
			}
			ev := s.agentExecuteTool(ctx, sessionID, permMode, call)
			s.appendEvent(sessionID, ev)
		}
	}
	// 步数上限终止: 并入 AI 回合(错误段),携带回合累计 token 用量
	s.appendEvent(sessionID, AgentEvent{
		Role:    "assistant",
		Kind:    "error",
		Content: fmt.Sprintf("已达单轮最大步数上限(%d 步)。可继续发送消息延续任务。", acfg.MaxSteps),
		TokensIn: turnIn, TokensCached: turnCached, TokensOut: turnOut,
	})
}

// agentExecuteTool 执行单个工具调用并生成结果事件。
func (s *AgentService) agentExecuteTool(ctx context.Context, sessionID, permMode string, call agentToolCall) AgentEvent {
	ev := AgentEvent{
		Role:       "tool",
		Kind:       "tool_result",
		ToolCallID: call.ID,
		ToolName:   call.Name,
		ToolArgs:   truncateUtf8(call.Arguments, 400),
	}

	// update_todo: 本地处理
	if call.Name == "update_todo" {
		var in struct {
			Todos []AgentTodoItem `json:"todos"`
		}
		if err := json.Unmarshal([]byte(call.Arguments), &in); err != nil {
			ev.Content = "参数解析失败: " + err.Error()
			return ev
		}
		if len(in.Todos) > 50 { // 有界
			in.Todos = in.Todos[:50]
		}
		for i := range in.Todos {
			in.Todos[i].Content = truncateUtf8(strings.TrimSpace(in.Todos[i].Content), 120)
			switch in.Todos[i].Status {
			case "pending", "in_progress", "done":
			default:
				in.Todos[i].Status = "pending"
			}
		}
		if err := s.store.SetTodos(sessionID, in.Todos); err != nil {
			ev.Content = "待办更新失败: " + err.Error()
			return ev
		}
		ev.Ok = true
		ev.Content = "待办清单已更新"
		ev.Todos = in.Todos
		return ev
	}

	// 计划模式: 只读白名单外的工具一律拒绝
	if permMode == agentPermPlan && !agentPlanReadableTools[call.Name] {
		ev.Content = "计划模式下禁止执行写操作: " + call.Name + "。请仅使用只读工具收集信息并产出方案。"
		return ev
	}

	// MCP 工具: 与外部智能体同一执行路径(P1 优先级)
	opts := execOpts{
		prio:        mcpPrioEmbedded,
		source:      mcpSrcEmbedded,
		forceManual: permMode == agentPermManual,
		noPaste:     true, // 审批弹窗已一次性授权,不再触发前端粘贴确认
	}
	result, err := s.mcp.ExecuteEmbeddedOpts(ctx, call.Name, call.Arguments, opts)
	if err != nil {
		ev.Content = "ERROR: " + err.Error()
		return ev
	}
	ev.Ok = true
	ev.Content = truncateUtf8(result, agentToolResultMax)
	return ev
}

// AgentInterrupt 强中断当前运行中的会话轮次(LLM 流与在途工具一并取消),
// 同时清空挂起消息(用户意图是完全停下来)。
func (s *AgentService) AgentInterrupt() string {
	s.mu.Lock()
	cancel := s.runCancel
	running := s.running
	s.pending = nil // 中断 = 全部停止,挂起一并清空
	s.mu.Unlock()
	s.emitPending()
	if cancel == nil || running == "" {
		return `{"ok":false}`
	}
	cancel()
	return `{"ok":true}`
}

// AgentRegenerate 刷新对话: 移除末尾的助手回答(含其工具链)后重跑。
// 执行中调用 = 强中断后重跑。
func (s *AgentService) AgentRegenerate(sessionID string) string {
	if !s.store.Exists(sessionID) {
		return agentErrJSON("agent.nosession", "会话不存在: "+sessionID)
	}
	s.mu.Lock()
	if s.running != "" {
		if s.running != sessionID {
			cur := s.running
			s.mu.Unlock()
			return agentErrJSON("agent.busy", "会话 "+cur+" 正在执行中(全局串行),请等待完成或先中断")
		}
		// 同会话执行中: 中断并标记重跑
		s.pending = nil
		cancel := s.runCancel
		s.regenerateAfter = sessionID
		s.mu.Unlock()
		cancel()
		return `{"ok":true}`
	}
	s.mu.Unlock()
	return s.regenerateNow(sessionID)
}

// regenerateNow 立即重跑(空闲状态): 裁剪尾部后以最后一条用户消息重跑。
func (s *AgentService) regenerateNow(sessionID string) string {
	text, err := s.store.TrimTailForRegenerate(sessionID)
	if err != nil {
		return agentErrJSON("agent.empty", err.Error())
	}
	if text == "" {
		return agentErrJSON("agent.empty", "没有可重跑的用户消息")
	}
	s.mu.Lock()
	if s.running != "" {
		s.mu.Unlock()
		return agentErrJSON("agent.busy", "全局串行,请等待当前对话完成")
	}
	s.startTurnLocked(sessionID, text)
	s.mu.Unlock()
	return `{"ok":true}`
}

// ==================== 前端绑定 ====================

// GetAgentStatus 返回智能体状态 JSON。
func (s *AgentService) GetAgentStatus() string {
	data, _ := json.Marshal(s.statusMap())
	return string(data)
}

// GetAgentSkills 返回内置技能列表 JSON。
func (s *AgentService) GetAgentSkills() string {
	data, _ := json.Marshal(agentBuiltinSkills)
	return string(data)
}

// AgentSessionSkills 返回会话已选技能 ID 列表 JSON。
func (s *AgentService) AgentSessionSkills(sessionID string) string {
	data, _ := json.Marshal(s.store.Skills(sessionID))
	return string(data)
}

// AgentSetSessionSkills 设置会话技能(仅接受内置技能 ID,上限 5 个)。
func (s *AgentService) AgentSetSessionSkills(sessionID string, skillsJSON string) string {
	var ids []string
	if err := json.Unmarshal([]byte(skillsJSON), &ids); err != nil {
		return agentErrJSON("agent.badskills", "参数错误")
	}
	if len(ids) > 5 { // 有界
		ids = ids[:5]
	}
	valid := ids[:0]
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !seen[id] && agentSkillByID(id) != nil {
			seen[id] = true
			valid = append(valid, id)
		}
	}
	if err := s.store.SetSkills(sessionID, valid); err != nil {
		return agentErrJSON("agent.nosession", err.Error())
	}
	return `{"ok":true}`
}

// AgentListModels 拉取可用模型列表。
// profileID 非空: 用该档案的端点+密钥(选择器下钻预览,不切换活动档案);
// 否则优先用传入参数(设置弹窗正在编辑的表单值);均留空则回退活动档案配置。
func (s *AgentService) AgentListModels(baseURL string, apiKey string, profileID string) string {
	if profileID != "" {
		u, k, found := s.cfg.AgentProfileByIDPlain(profileID)
		if !found {
			return agentErrJSON("agent.noprofile", "档案不存在")
		}
		baseURL, apiKey = u, k
	} else {
		if baseURL == "" && apiKey == "" {
			profile := s.cfg.ActiveAgentProfile()
			baseURL = profile.BaseURL
		}
		if apiKey == "" {
			apiKey = s.cfg.AgentApiKeyPlain()
		}
	}
	if baseURL == "" {
		return agentErrJSON("agent.noconf", "接口地址未配置")
	}
	client := newAgentClient(baseURL, apiKey)
	ids, err := client.ListModels(context.Background())
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	data, _ := json.Marshal(ids)
	return string(data)
}

// AgentSessionList 返回会话列表 JSON。
func (s *AgentService) AgentSessionList() string {
	data, _ := json.Marshal(s.store.List())
	return string(data)
}

// AgentNewSession 新建会话,返回会话元数据 JSON(含 reused 标记: 复用了既有空会话)。
func (s *AgentService) AgentNewSession(title string) string {
	m, reused, err := s.store.CreateDebounced(title)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	out := struct {
		AgentSessionMeta
		Reused bool `json:"reused"`
	}{AgentSessionMeta: m, Reused: reused}
	data, _ := json.Marshal(out)
	return string(data)
}

// AgentRenameSession 重命名会话。
func (s *AgentService) AgentRenameSession(id string, title string) string {
	if err := s.store.Rename(id, title); err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	return `{"ok":true}`
}

// AgentArchiveSession 设置归档状态(运行中的会话不可归档)。
func (s *AgentService) AgentArchiveSession(id string, archived bool) string {
	s.mu.Lock()
	running := s.running
	s.mu.Unlock()
	if running == id {
		return agentErrJSON("agent.busy", "会话执行中,请先中断再归档")
	}
	if err := s.store.Archive(id, archived); err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	return `{"ok":true}`
}

// AgentDeleteSession 删除会话(运行中的会话不可删除)。
func (s *AgentService) AgentDeleteSession(id string) string {
	s.mu.Lock()
	running := s.running
	s.mu.Unlock()
	if running == id {
		return agentErrJSON("agent.busy", "会话执行中,请先中断再删除")
	}
	if err := s.store.Delete(id); err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	return `{"ok":true}`
}

// AgentSessionEvents 分页读取会话事件(懒加载)。
// offset 为全量下标(负数从尾部倒数,如 -50 = 最近 50 条);limit 为页大小。
func (s *AgentService) AgentSessionEvents(sessionID string, offset int, limit int) string {
	events, total, actualOffset := s.store.EventsPage(sessionID, offset, limit)
	out := map[string]any{
		"events": events,
		"total":  total,
		"offset": actualOffset,
		"limit":  limit,
	}
	data, _ := json.Marshal(out)
	return string(data)
}

// AgentSessionTodos 返回会话当前待办清单 JSON。
func (s *AgentService) AgentSessionTodos(sessionID string) string {
	data, _ := json.Marshal(s.store.Todos(sessionID))
	return string(data)
}

// AgentDiagnose 运行时自诊断:逐项评估发送前置条件 + 可选真实连通测试。
// 用于定位"配置看起来正常但对话报错"类问题,结果同时落审计日志。
func (s *AgentService) AgentDiagnose(ping bool) string {
	acfg := s.cfg.AgentCfg()
	profile := s.cfg.ActiveAgentProfile()
	plain, encStored, keyErr := s.cfg.AgentApiKeyState()

	out := map[string]any{
		"enabled":       acfg.Enabled,
		"profileCount":  len(acfg.Profiles),
		"activeFound":   profile.ID != "",
		"activeId":      profile.ID,
		"baseURL":       profile.BaseURL,
		"model":         profile.Model,
		"keyEncStored":  encStored,
		"keyDecryptOk":  keyErr == nil,
		"keyLen":        len(plain),
		"keyError":      "",
		"dataDir":       DataDir(),
		"masterKeyFile": filepath.Join(DataDir(), "credential.key"),
	}
	if keyErr != nil {
		out["keyError"] = keyErr.Error()
	}

	// 前置条件汇总(任一 false 即发送会被拒)
	blocked := ""
	switch {
	case !acfg.Enabled:
		blocked = "智能体未启用"
	case profile.ID == "":
		blocked = "无活动档案"
	case keyErr != nil:
		blocked = "API Key 不可用: " + keyErr.Error()
	case profile.BaseURL == "":
		blocked = "接口地址为空"
	case profile.Model == "":
		blocked = "模型为空"
	}
	out["sendBlocked"] = blocked

	// 可选: 真实连通测试
	if ping && blocked == "" {
		client := newAgentClient(profile.BaseURL, plain)
		if err := client.Test(); err != nil {
			out["pingOk"] = false
			out["pingError"] = err.Error()
		} else {
			out["pingOk"] = true
		}
	}

	// 落诊断日志便于事后排查(数据目录/agent/diagnose.log)
	if raw, err := json.Marshal(out); err == nil {
		dir := filepath.Join(DataDir(), "agent")
		_ = os.MkdirAll(dir, 0700)
		if f, err := os.OpenFile(filepath.Join(dir, "diagnose.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600); err == nil {
			line := fmt.Sprintf(`{"ts":%q,"diag":%s}`+"\n", time.Now().Format("2006-01-02 15:04:05"), string(raw))
			_, _ = f.WriteString(line)
			_ = f.Close()
		}
	}
	data, _ := json.Marshal(out)
	return string(data)
}

// AgentTestConnection 测试指定(或活动)档案连通性。
func (s *AgentService) AgentTestConnection(baseURL string, apiKey string) string {
	// 优先用传入参数(设置弹窗正在编辑的档案);密钥留空则用已存密钥
	if apiKey == "" && baseURL == "" {
		profile := s.cfg.ActiveAgentProfile()
		client := newAgentClient(profile.BaseURL, s.cfg.AgentApiKeyPlain())
		if err := client.Test(); err != nil {
			return fmt.Sprintf(`{"ok":false,"error":%q}`, err.Error())
		}
		return `{"ok":true}`
	}
	if apiKey == "" {
		// 用已存密钥测新地址: 尝试各档案密钥(取第一个有密钥的)
		apiKey = s.cfg.AgentApiKeyPlain()
	}
	client := newAgentClient(baseURL, apiKey)
	if err := client.Test(); err != nil {
		return fmt.Sprintf(`{"ok":false,"error":%q}`, err.Error())
	}
	return `{"ok":true}`
}

// AgentProviderPreset 提供商预设。
type AgentProviderPreset struct {
	Name    string   `json:"name"`    // 预设标识(profile.Provider 存此值)
	Label   string   `json:"label"`   // 显示名
	BaseURL string   `json:"baseURL"` // OpenAI 兼容端点
	Models  []string `json:"models"`  // 建议模型列表
	Note    string   `json:"note"`    // 说明
}

// agentProviderPresets 主流提供商预设(Anthropic 走 OpenAI 兼容网关接入)。
var agentProviderPresets = []AgentProviderPreset{
	{Name: "openai", Label: "OpenAI", BaseURL: "https://api.openai.com/v1",
		Models: []string{"gpt-4o", "gpt-4o-mini", "gpt-4.1", "o4-mini"}},
	{Name: "deepseek", Label: "DeepSeek", BaseURL: "https://api.deepseek.com/v1",
		Models: []string{"deepseek-chat", "deepseek-reasoner"}},
	{Name: "qwen", Label: "通义千问(阿里)", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Models: []string{"qwen-max", "qwen-plus", "qwen-turbo", "qwen3-max"}},
	{Name: "zhipu", Label: "智谱 GLM", BaseURL: "https://open.bigmodel.cn/api/paas/v4",
		Models: []string{"glm-4.5", "glm-4.5-air", "glm-4-plus"}},
	{Name: "moonshot", Label: "月之暗面 Kimi", BaseURL: "https://api.moonshot.cn/v1",
		Models: []string{"kimi-k2-0905-preview", "kimi-k2-turbo-preview", "moonshot-v1-32k"}},
	{Name: "ollama", Label: "Ollama(本地)", BaseURL: "http://127.0.0.1:11434/v1",
		Models: []string{"qwen3", "llama3", "deepseek-r1"}},
	{Name: "lmstudio", Label: "LM Studio(本地)", BaseURL: "http://127.0.0.1:1234/v1",
		Models: []string{}},
	{Name: "openrouter", Label: "OpenRouter", BaseURL: "https://openrouter.ai/api/v1",
		Models: []string{"openai/gpt-4o", "anthropic/claude-sonnet-4.5", "deepseek/deepseek-chat"}},
	{Name: "anthropic", Label: "Anthropic Claude(经兼容网关)", BaseURL: "",
		Models: []string{"claude-sonnet-4-5", "claude-opus-4-1", "claude-3-7-sonnet-latest"},
		Note:   "Claude 原生 API 非 OpenAI 兼容格式:请填入兼容网关地址(OneAPI/NewAPI 等中转的 /v1 端点)后使用"},
	{Name: "custom", Label: "自定义(OpenAI 兼容)", BaseURL: "",
		Models: []string{}},
}

// GetAgentPresets 返回提供商预设列表 JSON。
func (s *AgentService) GetAgentPresets() string {
	data, _ := json.Marshal(agentProviderPresets)
	return string(data)
}
