package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// 智能体外接 MCP 客户端: 按主流标准动态加载 MCP 服务器(Streamable HTTP / SSE / stdio / 内置)。
//
// 设计:
//   - 配置驱动: 服务器清单来自 agent-mcp.json(兼容 Claude/Cursor 字段惯例),不耦合任何具体服务
//   - 工具清单缓存 + TTL: agentBuildTools 每轮调用,不能每轮发网络请求
//   - 失败退避: 某 server 不可达时 60s 内不重试,避免拖慢对话
//   - 会话复用: 进程内保持 ClientSession;调用失败时标记失效待下次重建
//   - 计划模式安全: 外部工具不进只读白名单,plan 模式下天然被拒绝(agentExecuteTool 现有逻辑)

const (
	mcpConnectTimeout = 10 * time.Second // 建连+握手+枚举限时
	mcpCallTimeout    = 45 * time.Second // 单次工具调用限时(Context7 文档检索可能较慢)
	mcpToolsTTL       = 10 * time.Minute // 工具清单缓存有效期
	mcpRetryBackoff   = 60 * time.Second // 失败后退避时长内不再尝试连接
)

// remoteToolDef 外部工具描述(供 LLM 的 function calling)。
type remoteToolDef struct {
	Server      string         `json:"server"` // 所属服务器标识
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Schema      map[string]any `json:"-"`
	exposed     string         // 暴露给 LLM 的名字(与 Name 相同或加服务器前缀消歧)
}

// Exposed 返回暴露给 LLM 的工具名。
func (t remoteToolDef) Exposed() string {
	if t.exposed != "" {
		return t.exposed
	}
	return t.Name
}

// exposure 暴露名 → (服务器, 真实工具名) 的路由记录。
type exposure struct {
	server string
	name   string
}

// mcpServerState 单个外部服务器的运行态。
type mcpServerState struct {
	session  *mcp.ClientSession
	lastFail time.Time        // 上次连接/枚举失败时间(退避用)
	toolsAt  time.Time        // 当前工具清单的拉取时间
	tools    []remoteToolDef  // 工具清单缓存
	err      string           // 最近一次错误信息(诊断)
}

// AgentMcpClient 外接 MCP 服务器客户端集合。
type AgentMcpClient struct {
	mu       sync.Mutex
	cfg      *AgentMcpConfig            // 服务器配置(来自 agent-mcp.json)
	states   map[string]*mcpServerState // serverName → 运行态
	client   *mcp.Client                // 协议客户端(共享)
	exposed  map[string]exposure        // 暴露名 → 路由记录
}

// NewAgentMcpClient 从配置文件创建客户端集合。
func NewAgentMcpClient(cfg *AgentMcpConfig) *AgentMcpClient {
	c := &AgentMcpClient{
		cfg:     cfg,
		states:  map[string]*mcpServerState{},
		exposed: map[string]exposure{},
		client:  mcp.NewClient(&mcp.Implementation{Name: "aceshell-agent", Version: "1.0"}, nil),
	}
	if c.cfg == nil {
		c.cfg = &AgentMcpConfig{McpServers: map[string]*AgentMcpServerConfig{}}
	}
	for name, s := range c.cfg.McpServers {
		if s == nil || !s.Enabled {
			continue
		}
		c.states[name] = &mcpServerState{}
	}
	return c
}

// RememberExposure 记录暴露名与真实目标的映射(agentBuildTools 构建工具列表时调用)。
func (c *AgentMcpClient) RememberExposure(server, realName, exposedName string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.exposed[exposedName] = exposure{server: server, name: realName}
}

// LookupExposed 按暴露名查找路由目标;不存在返回 false。
func (c *AgentMcpClient) LookupExposed(exposedName string) (server, realName string, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.exposed[exposedName]
	return e.server, e.name, ok
}

// authClient 构造注入自定义请求头的 HTTP 客户端(鉴权头等;MCP SDK 经此发起请求)。
func authClient(headers map[string]string) *http.Client {
	if len(headers) == 0 {
		return nil // SDK 回落 http.DefaultClient
	}
	return &http.Client{Transport: &headerRoundTripper{headers: headers, base: http.DefaultTransport}}
}

type headerRoundTripper struct {
	headers map[string]string
	base    http.RoundTripper
}

func (h *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	for k, v := range h.headers {
		if clone.Header.Get(k) == "" {
			clone.Header.Set(k, v)
		}
	}
	return h.base.RoundTrip(clone)
}

// transportFor 按 type 构造传输层(调用方持锁读配置)。
func transportFor(s *AgentMcpServerConfig) (mcp.Transport, error) {
	switch strings.ToLower(strings.TrimSpace(s.Type)) {
	case "", "http", "streamable-http", "streamable_http":
		return &mcp.StreamableClientTransport{Endpoint: s.URL, HTTPClient: authClient(s.Headers)}, nil
	case "sse":
		return &mcp.SSEClientTransport{Endpoint: s.URL, HTTPClient: authClient(s.Headers)}, nil
	case "stdio":
		bin, err := exec.LookPath(s.Command)
		if err != nil {
			return nil, fmt.Errorf("命令不存在: %s", s.Command)
		}
		cmd := exec.Command(bin, s.Args...)
		if len(s.Env) > 0 {
			env := cmd.Environ()
			for k, v := range s.Env {
				env = append(env, k+"="+v)
			}
			cmd.Env = env
		}
		return &mcp.CommandTransport{Command: cmd}, nil
	case "builtin":
		return builtinMcpTransport(), nil
	default:
		return nil, fmt.Errorf("不支持的类型: %s", s.Type)
	}
}

// connect 建立单个服务器的会话(调用方持锁)。
func (c *AgentMcpClient) connectLocked(ctx context.Context, name string) (*mcp.ClientSession, error) {
	s := c.cfg.McpServers[name]
	st := c.states[name]

	ctx, cancel := context.WithTimeout(ctx, mcpConnectTimeout)
	defer cancel()

	tr, err := transportFor(s)
	if err != nil {
		st.lastFail = time.Now()
		st.err = err.Error()
		return nil, err
	}
	session, err := c.client.Connect(ctx, tr, nil)
	if err != nil {
		st.lastFail = time.Now()
		st.err = fmt.Sprintf("连接失败: %v", err)
		return nil, fmt.Errorf("%s", st.err)
	}
	st.session = session
	st.err = ""
	return session, nil
}

// refreshTools 刷新全部启用服务器的工具清单(TTL/退避内直接用缓存)。
func (c *AgentMcpClient) refreshTools(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for name := range c.states {
		st := c.states[name]
		if len(st.tools) > 0 && now.Sub(st.toolsAt) < mcpToolsTTL {
			continue
		}
		if !st.lastFail.IsZero() && now.Sub(st.lastFail) < mcpRetryBackoff {
			continue
		}

		if st.session == nil {
			session, err := c.connectLocked(ctx, name)
			if err != nil {
				continue
			}
			st.session = session
		}

		lctx, cancel := context.WithTimeout(ctx, mcpConnectTimeout)
		resp, err := st.session.ListTools(lctx, nil)
		cancel()
		if err != nil {
			st.lastFail = time.Now()
			st.err = fmt.Sprintf("工具枚举失败: %v", err)
			st.session = nil // 会话可能已坏,下次重建
			continue
		}
		tools := make([]remoteToolDef, 0, len(resp.Tools))
		for _, t := range resp.Tools {
			def := remoteToolDef{Server: name, Name: t.Name, Description: t.Description}
			if t.InputSchema != nil {
				if raw, merr := json.Marshal(t.InputSchema); merr == nil {
					var schema map[string]any
					if json.Unmarshal(raw, &schema) == nil && schema != nil {
						def.Schema = schema
					}
				}
			}
			tools = append(tools, def)
		}
		st.tools = tools
		st.toolsAt = now
		st.err = ""
	}
}

// RemoteTools 返回当前可用的外部工具清单(触发缓存刷新;失败的 server 返回已缓存内容或空)。
func (c *AgentMcpClient) RemoteTools() []remoteToolDef {
	ctx, cancel := context.WithTimeout(context.Background(), mcpCallTimeout)
	defer cancel()
	c.refreshTools(ctx)

	c.mu.Lock()
	defer c.mu.Unlock()
	var out []remoteToolDef
	for _, st := range c.states {
		out = append(out, st.tools...)
	}
	return out
}

// Call 调用指定外部工具,argsJSON 为 LLM 生成的参数 JSON。
func (c *AgentMcpClient) Call(ctx context.Context, server, toolName, argsJSON string) (string, error) {
	c.mu.Lock()
	st := c.states[server]
	if st == nil {
		c.mu.Unlock()
		return "", fmt.Errorf("未启用的外部服务器: %s", server)
	}
	session := st.session
	c.mu.Unlock()
	if session == nil {
		c.mu.Lock()
		var err error
		session, err = c.connectLocked(ctx, server)
		c.mu.Unlock()
		if err != nil {
			return "", fmt.Errorf("连接 %s 失败: %w", server, err)
		}
	}

	var args map[string]any
	if strings.TrimSpace(argsJSON) != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", fmt.Errorf("参数解析失败: %w", err)
		}
	}

	ctx, cancel := context.WithTimeout(ctx, mcpCallTimeout)
	defer cancel()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: toolName, Arguments: args})
	if err != nil {
		// 会话可能失效,丢弃待下次重建
		c.mu.Lock()
		if st2 := c.states[server]; st2 != nil && st2.session == session {
			st2.session = nil
		}
		c.mu.Unlock()
		return "", fmt.Errorf("工具调用失败: %w", err)
	}

	var b strings.Builder
	for _, content := range res.Content {
		if tc, ok := content.(*mcp.TextContent); ok && tc.Text != "" {
			b.WriteString(tc.Text)
			b.WriteString("\n")
		}
	}
	text := strings.TrimSpace(b.String())
	if text == "" && res.IsError {
		return "", fmt.Errorf("工具返回错误(无详情)")
	}
	if text == "" {
		return "工具执行完成(无文本输出)", nil
	}
	if res.IsError {
		return "ERROR: " + truncateUtf8(text, agentToolResultMax), nil
	}
	return truncateUtf8(text, agentToolResultMax), nil
}

// Reload 替换配置并重置全部运行态(设置保存后调用;旧会话关闭,下次使用时重建)。
func (c *AgentMcpClient) Reload(cfg *AgentMcpConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, st := range c.states {
		if st.session != nil {
			_ = st.session.Close()
		}
	}
	c.cfg = cfg
	if c.cfg == nil || c.cfg.McpServers == nil {
		c.cfg = &AgentMcpConfig{McpServers: map[string]*AgentMcpServerConfig{}}
	}
	c.states = map[string]*mcpServerState{}
	for name, s := range c.cfg.McpServers {
		if s == nil || !s.Enabled {
			continue
		}
		c.states[name] = &mcpServerState{}
	}
	c.exposed = map[string]exposure{}
}

// ServerStatus 外部服务器状态(设置页展示)。
type ServerStatus struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Enabled bool     `json:"enabled"`
	URL     string   `json:"url,omitempty"`
	Command string   `json:"command,omitempty"`
	Tools   []string `json:"tools,omitempty"` // 已发现的工具名
	Err     string   `json:"err,omitempty"`
}

// Statuses 返回全部服务器状态(供设置页诊断;触发刷新)。
func (c *AgentMcpClient) Statuses() []ServerStatus {
	c.refreshTools(context.Background())

	c.mu.Lock()
	defer c.mu.Unlock()
	out := []ServerStatus{}
	for name, s := range c.cfg.McpServers {
		if s == nil {
			continue
		}
		status := ServerStatus{Name: name, Type: s.Type, Enabled: s.Enabled, URL: s.URL, Command: s.Command}
		if st := c.states[name]; st != nil {
			status.Err = st.err
			for _, t := range st.tools {
				status.Tools = append(status.Tools, t.Name)
			}
		}
		out = append(out, status)
	}
	return out
}
