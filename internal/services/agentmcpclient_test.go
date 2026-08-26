package services

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentMcpCfg_DefaultBundled(t *testing.T) {
	dir := withTestDataDir(t)
	cfg, err := LoadAgentMcpConfig()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	// 出厂默认捆绑: 内置 web-search + context7(http)
	if cfg.McpServers["web-search"] == nil || cfg.McpServers["web-search"].Type != "builtin" {
		t.Fatal("default config must bundle built-in web-search")
	}
	c7 := cfg.McpServers["context7"]
	if c7 == nil || c7.Type != "http" || !strings.Contains(c7.URL, "mcp.context7.com") {
		t.Fatalf("default config must bundle context7: %+v", c7)
	}
	if !c7.Enabled {
		t.Fatal("context7 should be enabled by default")
	}
	// 默认配置应已落盘(下次启动直接读取)
	if _, err := os.Stat(filepath.Join(dir, "agent-mcp.json")); err != nil {
		t.Fatal("default config file should be written on first load")
	}
}

func TestAgentMcpCfg_RoundTrip(t *testing.T) {
	withTestDataDir(t)
	// 先加载生成默认文件,再保存修改后的配置,重新加载应一致
	cfg, _ := LoadAgentMcpConfig()
	cfg.McpServers["fetch"] = &AgentMcpServerConfig{
		Type: "stdio", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-fetch"}, Enabled: false,
	}
	cfg.McpServers["context7"].Headers = map[string]string{"Authorization": "Bearer ctx7sk-test"}
	if err := SaveAgentMcpConfig(cfg); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	reloaded, err := LoadAgentMcpConfig()
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	f := reloaded.McpServers["fetch"]
	if f == nil || f.Type != "stdio" || len(f.Args) != 2 || f.Enabled {
		t.Fatalf("stdio entry round-trip mismatch: %+v", f)
	}
	h := reloaded.McpServers["context7"].Headers
	if h == nil || h["Authorization"] != "Bearer ctx7sk-test" {
		t.Fatalf("headers round-trip mismatch: %+v", h)
	}
}

func TestAgentMcpCfg_CorruptedFallsBackToDefault(t *testing.T) {
	withTestDataDir(t)
	if err := os.WriteFile(AgentMcpConfigFile(), []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadAgentMcpConfig()
	if err == nil {
		t.Fatal("expected error for corrupted file")
	}
	if cfg.McpServers["web-search"] == nil {
		t.Fatal("corrupted file should fall back to default bundled servers")
	}
}

func TestAgentMcpClient_BuiltinPipeline(t *testing.T) {
	// 内置 server 经 InMemoryTransport 全链路: 发现工具 → 调用 → 参数校验错误透传
	cfg := defaultAgentMcpConfig()
	client := NewAgentMcpClient(cfg)

	tools := client.RemoteTools()
	found := false
	for _, tl := range tools {
		if tl.Name == "web_search" && tl.Server == "web-search" {
			found = true
		}
	}
	if !found {
		t.Fatalf("web_search not discovered via MCP pipeline: %+v", tools)
	}

	// 空查询: 工具级错误按 MCP 规范经 IsError 结果透传(Call 不报传输错误)
	out, callErr := client.Call(context.Background(), "web-search", "web_search", `{"query":"   "}`)
	if callErr != nil {
		t.Fatalf("tool-level error should not be a transport error: %v", callErr)
	}
	if !strings.HasPrefix(out, "ERROR") || !strings.Contains(out, "不能为空") {
		t.Fatalf("empty query should surface tool error text, got %q", out)
	}

	// 正常调用(网络可达时返回结果;不可达时返回带引擎信息的聚合错误——两者均证明链路通)
	out, err := client.Call(context.Background(), "web-search", "web_search", `{"query":"golang testing","max_results":3}`)
	if err != nil {
		t.Logf("network unavailable in test env: %v", err)
		return
	}
	if !strings.Contains(out, "搜索结果") && !strings.Contains(out, "ERROR") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestAgentMcpClient_ExposureDisambiguation(t *testing.T) {
	client := NewAgentMcpClient(defaultAgentMcpConfig())

	// 无冲突原名暴露
	client.RememberExposure("context7", "query-docs", "query-docs")
	server, name, ok := client.LookupExposed("query-docs")
	if !ok || server != "context7" || name != "query-docs" {
		t.Fatalf("plain exposure lookup failed: %v %v %v", ok, server, name)
	}

	// 冲突加前缀
	client.RememberExposure("a", "tool_x", "a_tool_x")
	client.RememberExposure("b", "tool_x", "b_tool_x")
	s1, n1, _ := client.LookupExposed("a_tool_x")
	s2, n2, _ := client.LookupExposed("b_tool_x")
	if s1 != "a" || n1 != "tool_x" || s2 != "b" || n2 != "tool_x" {
		t.Fatalf("prefixed exposure lookup failed: %v/%v %v/%v", s1, n1, s2, n2)
	}

	// 未注册名查不到
	if _, _, ok := client.LookupExposed("nope"); ok {
		t.Fatal("unknown exposure must not resolve")
	}
}

func TestAgentMcpClient_ReloadDropsDisabledServer(t *testing.T) {
	client := NewAgentMcpClient(defaultAgentMcpConfig())
	tools := client.RemoteTools()
	if len(tools) == 0 {
		t.Fatal("expected tools before reload")
	}

	disabled := defaultAgentMcpConfig()
	disabled.McpServers["web-search"].Enabled = false
	disabled.McpServers["context7"].Enabled = false
	client.Reload(disabled)

	if tools := client.RemoteTools(); len(tools) != 0 {
		t.Fatalf("disabled servers should expose no tools, got %+v", tools)
	}
}
