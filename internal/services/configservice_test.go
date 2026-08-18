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
