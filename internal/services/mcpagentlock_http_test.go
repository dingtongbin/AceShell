package services

// GIL 锁真实 HTTP 全链路集成测试: 经真实 Start()(HTTP 监听+中间件+SDK 服务端)
// 与官方 go-sdk 客户端(与 opencode/Claude 等外部客户端同路径)验证:
//   1. 首个客户端取得操作权并正常执行工具
//   2. 第二个客户端报 MCP_BUSY
//   3. 内嵌智能体(P1)抢占外部客户端(P2)
//   4. 挂起清锁;恢复后重新竞争
//   5. 客户端关闭会话(DELETE)自动归还操作权
// 身份展示名来自 initialize 握手的 clientInfo.name(经中间件侦测)。

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// bearerRT 注入 Bearer Token 的 HTTP 传输(模拟外部客户端鉴权)。
type bearerRT struct {
	token string
	base  http.RoundTripper
}

func (b *bearerRT) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(clone)
}

func newExtClientSession(t *testing.T, svc *McpService, url, name string) *mcp.ClientSession {
	t.Helper()
	tr := &mcp.StreamableClientTransport{
		Endpoint:   url,
		HTTPClient: &http.Client{Transport: &bearerRT{token: svc.token, base: http.DefaultTransport}},
	}
	cl := mcp.NewClient(&mcp.Implementation{Name: name, Version: "0.0.1"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cs, err := cl.Connect(ctx, tr, nil)
	if err != nil {
		t.Fatalf("客户端 %s 初始化失败: %v", name, err)
	}
	return cs
}

func callListSessions(t *testing.T, cs *mcp.ClientSession) (text string, isErr bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "list_sessions", Arguments: map[string]any{}})
	if err != nil {
		return err.Error(), true
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String(), res.IsError
}

func TestMcpAgentLock_HTTPIntegration(t *testing.T) {
	withTestDataDir(t)
	cfg := &ConfigService{}
	cfg.Init()
	svc := NewMcpService(cfg, &SessionFileService{})
	svc.agentLock.waitFor = 600 * time.Millisecond // 缩短竞争等待
	if err := svc.Start(); err != nil {
		t.Fatalf("启动 MCP 服务失败: %v", err)
	}
	t.Cleanup(func() {
		svc.Stop()
		svc.audit.Close() // 释放审计日志句柄,避免 TempDir 清理失败
	})
	baseURL := "http://127.0.0.1:" + strconv.Itoa(cfg.McpPort()) + "/mcp"

	// 1) 客户端 A 取得操作权,工具正常执行
	csA := newExtClientSession(t, svc, baseURL, "lock-a")
	text, isErr := callListSessions(t, csA)
	if isErr {
		t.Fatalf("A 首次调用不应失败: %s", text)
	}
	snap := svc.agentLock.snapshot()
	if snap == nil || snap.Kind != "external" || !strings.HasPrefix(snap.Label, "lock-a#") {
		t.Fatalf("A 应持有锁且展示名带会话短码: %+v", snap)
	}

	// 2) 客户端 B 被拒(MCP_BUSY)
	csB := newExtClientSession(t, svc, baseURL, "lock-b")
	text, isErr = callListSessions(t, csB)
	if !isErr || !strings.Contains(text, "MCP_BUSY") {
		t.Fatalf("B 应报 MCP_BUSY, got isErr=%v text=%s", isErr, text)
	}

	// 3) 高优先级智能体抢占外部持有(经真实锁路径,验证抢占事件与状态推送)
	if err := svc.acquireAgentLock(context.Background(), "agent:hi", "高优智能体", 0); err != nil {
		t.Fatalf("高优应抢占成功: %v", err)
	}
	snap = svc.agentLock.snapshot()
	if snap == nil || snap.Kind != "external" || snap.Owner != "agent:hi" {
		t.Fatalf("高优应持有锁: %+v", snap)
	}

	// 4) 外部 B 再次被拒
	if _, isErr = callListSessions(t, csB); !isErr {
		t.Fatal("高优活跃期间 B 应被拒")
	}

	// 5) 挂起清锁 → 恢复后 B 重新取得
	svc.Pause()
	if snap := svc.agentLock.snapshot(); snap != nil {
		t.Fatalf("挂起后锁应清空: %+v", snap)
	}
	svc.Resume()
	text, isErr = callListSessions(t, csB)
	if isErr {
		t.Fatalf("恢复后 B 应成功: %s", text)
	}
	snap = svc.agentLock.snapshot()
	if snap == nil || !strings.HasPrefix(snap.Label, "lock-b#") {
		t.Fatalf("B 应重新持有锁: %+v", snap)
	}

	// 6) B 关闭会话(DELETE)→ 自动归还操作权
	if err := csB.Close(); err != nil {
		t.Fatalf("关闭会话失败: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for svc.agentLock.snapshot() != nil && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if snap := svc.agentLock.snapshot(); snap != nil {
		t.Fatalf("会话关闭后锁应归还: %+v", snap)
	}

	// 7) A 仍可重新取得(锁空闲)
	if _, isErr = callListSessions(t, csA); isErr {
		t.Fatal("锁空闲后 A 应成功")
	}
	_ = csA.Close()
}
