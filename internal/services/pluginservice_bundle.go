package services

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
)

// 本文件: 捆绑插件(发版内置, 类似 PyCharm bundled plugins)。
// 生命周期: 构建期嵌入(pluginbundle/) → 启动时落盘/升级 → 用户可卸载(持久记录,
// 升级不复活) → 可从设置页恢复。

// bundledManifests 内置插件清单(ID 升序, 稳定)。
func bundledManifests() []pluginManifest {
	out := []pluginManifest{}
	entries, err := pluginBundleFS.ReadDir(pluginBundleRoot)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		raw, err := pluginBundleFS.ReadFile(pluginBundleRoot + "/" + e.Name() + "/plugin.json")
		if err != nil {
			continue
		}
		var mf pluginManifest
		if json.Unmarshal(raw, &mf) != nil || !pluginIDRe.MatchString(mf.ID) || mf.ID != e.Name() {
			continue
		}
		out = append(out, mf)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// isBundled 插件是否随主程序内置。
func (s *PluginService) isBundled(pluginID string) bool {
	for _, mf := range bundledManifests() {
		if mf.ID == pluginID {
			return true
		}
	}
	return false
}

// ensureBundledPlugins 启动时物化内置插件: 缺失即落盘; 磁盘版本旧于内置版本即升级。
// 用户已卸载的(记录在案)跳过。须在扫描/启动插件之前调用。
func (s *PluginService) ensureBundledPlugins() {
	_ = os.MkdirAll(PluginsDir(), 0700)
	for _, mf := range bundledManifests() {
		if s.cfg.BundledUninstalled(mf.ID) {
			continue
		}
		if !shouldMaterializeBundled(mf, filepath.Join(PluginsDir(), mf.ID)) {
			continue
		}
		if err := materializeBundled(mf.ID); err != nil {
			s.logLine("捆绑插件 " + mf.ID + " 落盘失败: " + err.Error())
		} else {
			s.logLine("捆绑插件 " + mf.ID + " 已落盘 (v" + mf.Version + ")")
		}
	}
}

// shouldMaterializeBundled 判定是否需要落盘: 目录/清单缺失 → 是; 内置版本更新 → 升级。
func shouldMaterializeBundled(bundled pluginManifest, dir string) bool {
	raw, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		return true
	}
	var disk pluginManifest
	if json.Unmarshal(raw, &disk) != nil {
		return true
	}
	return compareVersion(bundled.Version, disk.Version) > 0
}

// materializeBundled 将内置插件目录整体复制到插件安装目录(先清理旧内容)。
func materializeBundled(pluginID string) error {
	finalDir := filepath.Join(PluginsDir(), pluginID)
	if err := os.RemoveAll(finalDir); err != nil {
		return err
	}
	src := pluginBundleRoot + "/" + pluginID
	return fs.WalkDir(pluginBundleFS, src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(finalDir, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		data, err := pluginBundleFS.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0755)
	})
}

// PluginUninstall 卸载插件: 停止 + 删除目录; 捆绑插件额外持久记录(升级不复活)。
func (s *PluginService) PluginUninstall(pluginID string) string {
	inst := s.getInstance(pluginID)
	if inst == nil {
		return `{"error":"未知插件: ` + pluginID + `"}`
	}
	if inst.status != pluginStatusDisabled {
		s.stopInstance(inst)
	}
	bundled := s.isBundled(pluginID)
	if err := os.RemoveAll(inst.dir); err != nil {
		return `{"error":` + mustJSONString(err.Error()) + `}`
	}
	// 连带清理手动安装时可能残留在插件目录旁的压缩包
	// (<id>.zip 及约定命名的 aceshell-<id>-<goos>-<goarch>.zip)。
	for _, pattern := range []string{
		filepath.Join(PluginsDir(), pluginID+".zip"),
		filepath.Join(PluginsDir(), "aceshell-"+pluginID+"-*.zip"),
	} {
		if matches, _ := filepath.Glob(pattern); len(matches) > 0 {
			for _, m := range matches {
				_ = os.Remove(m)
			}
		}
	}
	if bundled {
		s.cfg.SetBundledUninstalled(pluginID, true)
	}
	s.mu.Lock()
	delete(s.instances, pluginID)
	s.mu.Unlock()
	s.emitRegistry()
	return mustJSON(map[string]any{"ok": true, "bundled": bundled})
}

// PluginRestoreBundled 恢复已卸载的捆绑插件(清除卸载记录 + 重新落盘 + 启动)。
func (s *PluginService) PluginRestoreBundled(pluginID string) string {
	if !s.isBundled(pluginID) {
		return `{"error":"非捆绑插件: ` + pluginID + `"}`
	}
	if !s.cfg.BundledUninstalled(pluginID) {
		return `{"error":"插件未被卸载: ` + pluginID + `"}`
	}
	s.cfg.SetBundledUninstalled(pluginID, false)
	var mf pluginManifest
	for _, m := range bundledManifests() {
		if m.ID == pluginID {
			mf = m
			break
		}
	}
	if err := materializeBundled(pluginID); err != nil {
		return `{"error":` + mustJSONString(err.Error()) + `}`
	}
	s.emitRegistry()
	if !s.cfg.PluginEnabled(pluginID) {
		return mustJSON(map[string]any{"ok": true, "launched": false})
	}
	cand := pluginCandidate{
		id:      mf.ID,
		dir:     filepath.Join(PluginsDir(), mf.ID),
		exePath: filepath.Join(PluginsDir(), mf.ID, mf.ID+exeSuffix()),
		manifest: mf,
	}
	go func() {
		_ = s.launch(cand)
		s.emitRegistry()
	}()
	return mustJSON(map[string]any{"ok": true, "launched": true})
}

// exeSuffix 平台可执行文件后缀。
func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}
