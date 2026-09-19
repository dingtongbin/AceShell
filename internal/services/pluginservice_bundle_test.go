package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestShouldMaterializeBundled(t *testing.T) {
	dir := t.TempDir()
	bundled := pluginManifest{ID: "ping", Version: "0.2.0"}

	// 目录缺失 → 落盘
	if !shouldMaterializeBundled(bundled, dir) {
		t.Fatal("目录缺失应落盘")
	}
	// 同版本 → 跳过
	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(`{"id":"ping","version":"0.2.0"}`), 0600)
	if shouldMaterializeBundled(bundled, dir) {
		t.Fatal("同版本不应重新落盘")
	}
	// 磁盘版本更新(用户手动装了更高版本) → 跳过
	_ = os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(`{"id":"ping","version":"1.0.0"}`), 0600)
	if shouldMaterializeBundled(bundled, dir) {
		t.Fatal("磁盘版本更新时不应降级覆盖")
	}
	// 内置版本更新 → 升级
	_ = os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(`{"id":"ping","version":"0.1.0"}`), 0600)
	if !shouldMaterializeBundled(bundled, dir) {
		t.Fatal("内置版本更新时应升级")
	}
	// 清单损坏 → 重落
	_ = os.WriteFile(filepath.Join(dir, "plugin.json"), []byte("{{{"), 0600)
	if !shouldMaterializeBundled(bundled, dir) {
		t.Fatal("清单损坏应重新落盘")
	}
}

func TestBundledManifests(t *testing.T) {
	mfs := bundledManifests()
	// 构建机上有 ping 载荷; CI/干净环境只有 README(0 个)。两种情况都合法。
	for _, mf := range mfs {
		if !pluginIDRe.MatchString(mf.ID) {
			t.Fatalf("内置清单 ID 非法: %+v", mf)
		}
	}
	// 若本机构建了 ping 载荷, 必须能被发现
	if _, err := os.Stat(filepath.Join("pluginbundle", "ping", "plugin.json")); err == nil {
		found := false
		for _, mf := range mfs {
			if mf.ID == "ping" {
				found = true
			}
		}
		if !found {
			t.Fatal("pluginbundle/ping 存在但未被 bundledManifests 发现")
		}
	}
}

func TestBundledUninstalledPersistence(t *testing.T) {
	restore := SetDataDir(t.TempDir())
	defer restore()
	cfg := &ConfigService{}
	cfg.Init()

	if cfg.BundledUninstalled("ping") {
		t.Fatal("初始状态不应为已卸载")
	}
	cfg.SetBundledUninstalled("ping", true)
	if !cfg.BundledUninstalled("ping") {
		t.Fatal("卸载记录未生效")
	}
	cfg.SetBundledUninstalled("other", true)
	cfg.SetBundledUninstalled("ping", false)
	if cfg.BundledUninstalled("ping") {
		t.Fatal("恢复(撤销卸载)未生效")
	}
	if !cfg.BundledUninstalled("other") {
		t.Fatal("其他插件卸载记录不应受影响")
	}

	// 重新 Init(模拟重启)后记录仍在
	cfg2 := &ConfigService{}
	cfg2.Init()
	if !cfg2.BundledUninstalled("other") {
		t.Fatal("卸载记录未持久化")
	}

	// config.json 序列化包含字段
	cfg2.mu.Lock()
	data := cfg2.configJSONLocked()
	cfg2.mu.Unlock()
	var view struct {
		Plugins struct {
			UninstalledBundled []string `json:"uninstalledBundled"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal([]byte(data), &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Plugins.UninstalledBundled) != 1 || view.Plugins.UninstalledBundled[0] != "other" {
		t.Fatalf("uninstalledBundled 序列化异常: %s", data)
	}
}

func TestMaterializeBundledRoundTrip(t *testing.T) {
	restore := SetDataDir(t.TempDir())
	defer restore()
	mfs := bundledManifests()
	if len(mfs) == 0 {
		t.Skip("本机无捆绑载荷, 跳过")
	}
	mf := mfs[0]
	if err := materializeBundled(mf.ID); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(PluginsDir(), mf.ID)
	if _, err := os.Stat(filepath.Join(dir, "plugin.json")); err != nil {
		t.Fatal("plugin.json 未落盘")
	}
	exe := filepath.Join(dir, mf.ID+exeSuffix())
	if _, err := os.Stat(exe); err != nil {
		t.Fatalf("可执行文件未落盘: %v", err)
	}
	// 幂等: 重复落盘不报错
	if err := materializeBundled(mf.ID); err != nil {
		t.Fatal(err)
	}
}
