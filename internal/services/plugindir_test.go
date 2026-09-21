package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeFile 建目录并写文件(测试工具)。
func writePluginFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSwapPluginDirFreshInstall(t *testing.T) {
	restore := SetDataDir(t.TempDir())
	defer restore()

	src := filepath.Join(PluginsDir(), dirInstallPrefix+"abc")
	writePluginFile(t, filepath.Join(src, "plugin.json"), `{"id":"ping","version":"0.2.0"}`)

	if err := swapPluginDir("ping", src); err != nil {
		t.Fatal(err)
	}
	final := filepath.Join(PluginsDir(), "ping")
	if _, err := os.Stat(filepath.Join(final, "plugin.json")); err != nil {
		t.Fatalf("新目录未就位: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("src 应已被消费(不再存在)")
	}
}

func TestSwapPluginDirReplacesAndDropsOld(t *testing.T) {
	restore := SetDataDir(t.TempDir())
	defer restore()

	final := filepath.Join(PluginsDir(), "ping")
	writePluginFile(t, filepath.Join(final, "plugin.json"), `{"id":"ping","version":"0.1.0"}`)
	writePluginFile(t, filepath.Join(final, "stale-only.txt"), "old")

	src := filepath.Join(PluginsDir(), dirInstallPrefix+"xyz")
	writePluginFile(t, filepath.Join(src, "plugin.json"), `{"id":"ping","version":"0.2.0"}`)

	if err := swapPluginDir("ping", src); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(final, "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "0.2.0") {
		t.Fatalf("新版本未生效: %s", raw)
	}
	if _, err := os.Stat(filepath.Join(final, "stale-only.txt")); !os.IsNotExist(err) {
		t.Fatal("旧版本内容应被整体替换(不应残留)")
	}
	// 留档目录应已被清掉
	entries, _ := os.ReadDir(PluginsDir())
	for _, e := range entries {
		if strings.Contains(e.Name(), dirStaleMarker) {
			t.Fatalf("留档目录未清理: %s", e.Name())
		}
	}
}

func TestSwapPluginDirKeepsOldOnBadSource(t *testing.T) {
	restore := SetDataDir(t.TempDir())
	defer restore()

	final := filepath.Join(PluginsDir(), "ping")
	writePluginFile(t, filepath.Join(final, "plugin.json"), `{"id":"ping","version":"0.1.0"}`)

	// src 不存在: 必须在动旧目录之前就失败
	err := swapPluginDir("ping", filepath.Join(PluginsDir(), dirInstallPrefix+"missing"))
	if err == nil {
		t.Fatal("src 缺失应报错")
	}
	if _, serr := os.Stat(filepath.Join(final, "plugin.json")); serr != nil {
		t.Fatalf("失败时旧版本必须保持完整: %v", serr)
	}
}

func TestSwapPluginDirRejectsBadID(t *testing.T) {
	restore := SetDataDir(t.TempDir())
	defer restore()
	src := filepath.Join(PluginsDir(), dirInstallPrefix+"bad")
	writePluginFile(t, filepath.Join(src, "plugin.json"), `{}`)
	for _, id := range []string{"../escape", "Ping", "ping/x", ""} {
		if err := swapPluginDir(id, src); err == nil {
			t.Fatalf("非法 ID %q 应被拒绝", id)
		}
	}
}

func TestWaitUnlockedOnFreeFile(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "ping"+exeSuffix())
	writePluginFile(t, exe, "binary")
	if err := waitUnlocked(exe, 200*time.Millisecond); err != nil {
		t.Fatalf("未占用的文件不应等待失败: %v", err)
	}
	// 文件不存在 = 全新安装路径, 直接放行
	if err := waitUnlocked(filepath.Join(dir, "absent.exe"), time.Millisecond); err != nil {
		t.Fatalf("文件不存在应放行: %v", err)
	}
}

func TestIsStalePluginDirName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{".install-abc123", true},
		{"ping.old-1758300000000000000", true},
		{"my-plugin.old-1", true},
		{"ping", false},          // 合法插件目录
		{"my-plugin", false},     // 合法插件目录
		{"ping.old-", false},     // 无时间戳
		{"ping.old-abc", false},  // 时间戳非数字
		{".old-123", false},      // 缺少 ID
		{"Ping.old-1", false},    // ID 非法
		{"ping.backup-1", false}, // 非留档标记
		{"install-x", false},     // 前缀必须是 .install-
	}
	for _, c := range cases {
		if got := isStalePluginDirName(c.name); got != c.want {
			t.Errorf("isStalePluginDirName(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestCleanupStalePluginDirs(t *testing.T) {
	restore := SetDataDir(t.TempDir())
	defer restore()

	// 合法插件目录 + 两种残留
	writePluginFile(t, filepath.Join(PluginsDir(), "ping", "plugin.json"), `{"id":"ping"}`)
	writePluginFile(t, filepath.Join(PluginsDir(), "ping.old-1758300000000000000", "plugin.json"), `{"id":"ping"}`)
	writePluginFile(t, filepath.Join(PluginsDir(), dirInstallPrefix+"tmp1", "plugin.json"), `{}`)

	all, err := os.ReadDir(PluginsDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("前置状态异常, 期望 3 个目录, 实得 %d", len(all))
	}
	// ReadDir 返回顺序按文件名升序; 逐个确认判定结果
	var stale int
	for _, e := range all {
		if isStalePluginDirName(e.Name()) {
			stale++
		}
	}
	if stale != 2 {
		t.Fatalf("应识别 2 个残留, 实得 %d", stale)
	}

	if removed := cleanupStalePluginDirs(); removed != 2 {
		t.Fatalf("应清理 2 个残留, 实得 %d", removed)
	}
	if _, err := os.Stat(filepath.Join(PluginsDir(), "ping", "plugin.json")); err != nil {
		t.Fatalf("合法插件目录被误删: %v", err)
	}
	after, _ := os.ReadDir(PluginsDir())
	if len(after) != 1 || after[0].Name() != "ping" {
		t.Fatalf("清理后应只剩 ping, 实得 %v", after)
	}
}
