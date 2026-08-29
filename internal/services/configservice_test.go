package services

import (
	"os"
	"strings"
	"testing"
)

func TestConfigService_GetConfig(t *testing.T) {
	withTestDataDir(t)
	svc := &ConfigService{}
	svc.Init()

	cfg := svc.GetConfig()
	if cfg == "" {
		t.Fatal("GetConfig returned empty")
	}
}

func TestConfigService_SetShowSession(t *testing.T) {
	withTestDataDir(t)
	svc := &ConfigService{}
	svc.Init()

	result := svc.SetShowSession(false)
	if result == "" {
		t.Fatal("SetShowSession returned empty")
	}

	cfg := svc.GetConfig()
	if cfg == "" {
		t.Fatal("GetConfig returned empty after set")
	}

	os.Remove(configFile)
}

func TestConfigService_SetShowHelp(t *testing.T) {
	withTestDataDir(t)
	os.Remove(configFile)
	defer os.Remove(configFile)

	svc := &ConfigService{}
	svc.Init()

	if !svc.config.View.ShowHelp {
		t.Fatal("default ShowHelp should be true")
	}

	svc.SetShowHelp(true)
	cfg := svc.GetConfig()
	if !strings.Contains(cfg, `"showHelp":true`) {
		t.Fatalf("SetShowHelp(true) not reflected in config JSON: %s", cfg)
	}

	svc2 := &ConfigService{}
	svc2.Init()
	cfg2 := svc2.GetConfig()
	if !strings.Contains(cfg2, `"showHelp":true`) {
		t.Fatalf("showHelp=true not persisted to config.toml: %s", cfg2)
	}
}

func TestConfigService_SetSectionsState(t *testing.T) {
	withTestDataDir(t)
	svc := &ConfigService{}
	svc.Init()

	state := `{"session":{"expanded":true,"size":200},"script":{"expanded":false,"size":0}}`
	result := svc.SetSectionsState(state)
	if result == "" {
		t.Fatal("SetSectionsState returned empty")
	}

	cfg := svc.GetConfig()
	if cfg == "" {
		t.Fatal("GetConfig returned empty after set")
	}

	os.Remove(configFile)
}

func TestConfigService_Persistence(t *testing.T) {
	withTestDataDir(t)
	os.Remove(configFile)

	svc1 := &ConfigService{}
	svc1.Init()
	svc1.SetShowSession(false)

	svc2 := &ConfigService{}
	svc2.Init()

	cfg := svc2.GetConfig()
	if cfg == "" {
		t.Fatal("GetConfig returned empty")
	}

	os.Remove(configFile)
}

func TestConfigService_SetTheme(t *testing.T) {
	withTestDataDir(t)
	os.Remove(configFile)
	defer os.Remove(configFile)

	svc := &ConfigService{}
	svc.Init()

	if svc.GetTheme() != "dark" {
		t.Fatalf("Expected default theme 'dark', got %q", svc.GetTheme())
	}

	svc.SetTheme("light")
	if svc.GetTheme() != "light" {
		t.Fatalf("Expected theme 'light', got %q", svc.GetTheme())
	}
}

func TestConfigService_CorruptedConfig(t *testing.T) {
	// 写一个损坏的 TOML 文件，验证 init 不会 panic
	withTestDataDir(t)
	os.WriteFile(configFile, []byte("this is not valid toml {{{"), 0644)
	defer os.Remove(configFile)

	svc := &ConfigService{}
	svc.Init()

	cfg := svc.GetConfig()
	if cfg == "" {
		t.Fatal("GetConfig returned empty with corrupted config - should fallback to defaults")
	}
}

func TestConfigService_ConfigInDataDir(t *testing.T) {
	dir := withTestDataDir(t)
	svc := &ConfigService{}
	svc.Init()

	if svc.GetTheme() != "dark" {
		t.Fatalf("Expected default theme 'dark', got %q", svc.GetTheme())
	}

	svc.SetTheme("light")

	// 配置文件应写入数据目录,而非当前工作目录
	if _, err := os.Stat(configFile); err != nil {
		t.Fatalf("config file not created at %s: %v", configFile, err)
	}
	if ConfigFilePath() != configFile {
		t.Fatalf("ConfigFilePath() = %q, want %q", ConfigFilePath(), configFile)
	}
	if configFile != dir+string(os.PathSeparator)+"config.toml" {
		t.Fatalf("config file not in data dir: %s", configFile)
	}
}

func TestConfigService_McpAgentSplitFiles(t *testing.T) {
	withTestDataDir(t)
	os.Remove(configFile)
	os.Remove(McpConfigFile())
	os.Remove(AgentConfigFile())
	defer func() {
		os.Remove(configFile)
		os.Remove(McpConfigFile())
		os.Remove(AgentConfigFile())
	}()

	svc := &ConfigService{}
	svc.Init()

	// 修改 mcp/agent 配置触发保存
	svc.SetMcpPort(8951)
	svc.SetAgentBehavior("auto", 200, 400)

	// 1. config.toml 不得包含 mcp/agent 节
	main, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("config.toml 未创建: %v", err)
	}
	if tomlHasSection(string(main), "mcp") || tomlHasSection(string(main), "agent") {
		t.Fatalf("config.toml 不应包含 mcp/agent 节:\n%s", main)
	}
	// 2. 独立文件存在且包含对应节
	mcp, err := os.ReadFile(McpConfigFile())
	if err != nil || !tomlHasSection(string(mcp), "mcp") {
		t.Fatalf("mcp.toml 缺失或无 [mcp] 节: err=%v", err)
	}
	agent, err := os.ReadFile(AgentConfigFile())
	if err != nil || !tomlHasSection(string(agent), "agent") {
		t.Fatalf("agent.toml 缺失或无 [agent] 节: err=%v", err)
	}
	// 3. 重启后从独立文件恢复
	svc2 := &ConfigService{}
	svc2.Init()
	cfg := svc2.GetConfig()
	if !strings.Contains(cfg, `"port":8951`) {
		t.Fatalf("mcp 配置未从 mcp.toml 恢复: %s", cfg)
	}
	if !strings.Contains(cfg, `"permMode":"auto"`) {
		t.Fatalf("agent 配置未从 agent.toml 恢复: %s", cfg)
	}
}

func TestConfigService_SplitMigrationFromLegacy(t *testing.T) {
	withTestDataDir(t)
	os.Remove(configFile)
	os.Remove(McpConfigFile())
	os.Remove(AgentConfigFile())
	defer func() {
		os.Remove(configFile)
		os.Remove(McpConfigFile())
		os.Remove(AgentConfigFile())
	}()

	// 旧版: mcp/agent 节内嵌在 config.toml
	legacy := `
[mcp]
enabled = true
port = 8962

[agent]
enabled = true
permMode = "plan"
`
	if err := os.WriteFile(configFile, []byte(legacy), 0600); err != nil {
		t.Fatalf("写入旧版配置失败: %v", err)
	}

	svc := &ConfigService{}
	svc.Init()

	// 迁移后: 主文件剔除旧节,独立文件承接
	main, _ := os.ReadFile(configFile)
	if tomlHasSection(string(main), "mcp") || tomlHasSection(string(main), "agent") {
		t.Fatalf("迁移后 config.toml 仍含 mcp/agent 节:\n%s", main)
	}
	mcp, err := os.ReadFile(McpConfigFile())
	if err != nil || !strings.Contains(string(mcp), "port = 8962") {
		t.Fatalf("mcp 配置未迁移到 mcp.toml: err=%v content=%s", err, mcp)
	}
	agent, err := os.ReadFile(AgentConfigFile())
	if err != nil || !strings.Contains(string(agent), "permMode = 'plan'") {
		t.Fatalf("agent 配置未迁移到 agent.toml: err=%v content=%s", err, agent)
	}
	// 内存态正确
	cfg := svc.GetConfig()
	if !strings.Contains(cfg, `"port":8962`) || !strings.Contains(cfg, `"permMode":"plan"`) {
		t.Fatalf("迁移后内存配置不正确: %s", cfg)
	}
}

// TestAgentProfilesRoundTrip 端到端:保存档案(含密钥)→重启加载→解密读取。
// 覆盖"空密钥保存=保留原密文"语义。
func TestAgentProfilesRoundTrip(t *testing.T) {
	restore := SetDataDir(t.TempDir())
	defer restore()
	c1 := &ConfigService{}
	c1.Init()
	raw := `{"activeProfileId":"p1","profiles":[{"id":"p1","name":"t","provider":"zhipu","baseURL":"https://x","model":"m","apiMode":"chat","apiKey":"sk-test-123"}]}`
	if out := c1.AgentProfilesSet(raw); strings.Contains(out, "error") {
		t.Fatalf("保存失败: %s", out)
	}
	if c1.AgentApiKeyPlain() != "sk-test-123" {
		t.Fatalf("内存读取失败: %q", c1.AgentApiKeyPlain())
	}
	// 模拟重启
	c2 := &ConfigService{}
	c2.Init()
	if c2.AgentApiKeyPlain() != "sk-test-123" {
		t.Fatalf("重启后读取失败: %q", c2.AgentApiKeyPlain())
	}
	// 空密钥保存 = 保留原密文
	if out := c2.AgentProfilesSet(`{"activeProfileId":"p1","profiles":[{"id":"p1","name":"t","provider":"zhipu","baseURL":"https://x","model":"m","apiMode":"chat","apiKey":""}]}`); strings.Contains(out, "error") {
		t.Fatalf("空密钥保存失败: %s", out)
	}
	if c2.AgentApiKeyPlain() != "sk-test-123" {
		t.Fatalf("空密钥保存后密文丢失: %q", c2.AgentApiKeyPlain())
	}
}

// TestAgentProfilesSet_MemoryLossKeepsDiskKey 回归防护:
// 内存密文丢失(未知覆盖/状态异常)时,空密钥保存不得清空磁盘已存密文。
// 场景对应 2026-08-22 15:56:50 事故: profiles-set key=empty encLen=0 覆盖磁盘。
func TestAgentProfilesSet_MemoryLossKeepsDiskKey(t *testing.T) {
	restore := SetDataDir(t.TempDir())
	defer restore()
	c1 := &ConfigService{}
	c1.Init()
	if out := c1.AgentProfilesSet(`{"activeProfileId":"p1","profiles":[{"id":"p1","name":"t","provider":"zhipu","baseURL":"https://x","model":"m","apiMode":"chat","apiKey":"sk-keep-me"}]}`); strings.Contains(out, "error") {
		t.Fatalf("保存失败: %s", out)
	}
	// 磁盘已有密文
	before, err := os.ReadFile(AgentConfigFile())
	if err != nil || !strings.Contains(string(before), "apiKeyEnc = ") {
		t.Fatalf("磁盘无密文: err=%v", err)
	}
	// 模拟内存密文丢失
	c1.mu.Lock()
	c1.config.Agent.Profiles[0].ApiKeyEnc = ""
	c1.mu.Unlock()
	// 用户不输新密钥直接保存 → 必须保留磁盘密文
	if out := c1.AgentProfilesSet(`{"activeProfileId":"p1","profiles":[{"id":"p1","name":"t","provider":"zhipu","baseURL":"https://x","model":"m","apiMode":"chat","apiKey":""}]}`); strings.Contains(out, "error") {
		t.Fatalf("内存丢失后保存失败: %s", out)
	}
	if c1.AgentApiKeyPlain() != "sk-keep-me" {
		t.Fatalf("内存丢失后密文被清: %q", c1.AgentApiKeyPlain())
	}
	after, _ := os.ReadFile(AgentConfigFile())
	if !strings.Contains(string(after), "apiKeyEnc = ") {
		t.Fatal("磁盘密文被空值覆盖")
	}
	// save() 通用保护: 内存丢密文后任意写路径(SetAgentEnabled)也不得清盘
	c1.mu.Lock()
	c1.config.Agent.Profiles[0].ApiKeyEnc = ""
	c1.mu.Unlock()
	c1.SetAgentEnabled(true)
	fresh := &ConfigService{}
	fresh.Init()
	if fresh.AgentApiKeyPlain() != "sk-keep-me" {
		t.Fatalf("写盘保护失效,重启后密钥丢失: %q", fresh.AgentApiKeyPlain())
	}
}
