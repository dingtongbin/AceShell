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

func TestConfigService_McpSplitFiles(t *testing.T) {
	withTestDataDir(t)
	os.Remove(configFile)
	os.Remove(McpConfigFile())
	defer func() {
		os.Remove(configFile)
		os.Remove(McpConfigFile())
	}()

	svc := &ConfigService{}
	svc.Init()

	// 修改 mcp 配置触发保存
	svc.SetMcpPort(8951)

	// 1. config.toml 不得包含 mcp 节
	main, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("config.toml 未创建: %v", err)
	}
	if tomlHasSection(string(main), "mcp") {
		t.Fatalf("config.toml 不应包含 mcp 节:\n%s", main)
	}
	// 2. 独立文件存在且包含对应节
	mcp, err := os.ReadFile(McpConfigFile())
	if err != nil || !tomlHasSection(string(mcp), "mcp") {
		t.Fatalf("mcp.toml 缺失或无 [mcp] 节: err=%v", err)
	}
	// 3. 重启后从独立文件恢复
	svc2 := &ConfigService{}
	svc2.Init()
	cfg := svc2.GetConfig()
	if !strings.Contains(cfg, `"port":8951`) {
		t.Fatalf("mcp 配置未从 mcp.toml 恢复: %s", cfg)
	}
}

func TestConfigService_SplitMigrationFromLegacy(t *testing.T) {
	withTestDataDir(t)
	os.Remove(configFile)
	os.Remove(McpConfigFile())
	defer func() {
		os.Remove(configFile)
		os.Remove(McpConfigFile())
	}()

	// 旧版: mcp 节内嵌在 config.toml
	legacy := `
[mcp]
enabled = true
port = 8962
`
	if err := os.WriteFile(configFile, []byte(legacy), 0600); err != nil {
		t.Fatalf("写入旧版配置失败: %v", err)
	}

	svc := &ConfigService{}
	svc.Init()

	// 迁移后: 主文件剔除旧节,独立文件承接
	main, _ := os.ReadFile(configFile)
	if tomlHasSection(string(main), "mcp") {
		t.Fatalf("迁移后 config.toml 仍含 mcp 节:\n%s", main)
	}
	mcp, err := os.ReadFile(McpConfigFile())
	if err != nil || !strings.Contains(string(mcp), "port = 8962") {
		t.Fatalf("mcp 配置未迁移到 mcp.toml: err=%v content=%s", err, mcp)
	}
	// 内存态正确
	cfg := svc.GetConfig()
	if !strings.Contains(cfg, `"port":8962`) {
		t.Fatalf("迁移后内存配置不正确: %s", cfg)
	}
}
