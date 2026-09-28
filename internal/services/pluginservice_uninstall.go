package services

import (
	"os"
	"path/filepath"
	"runtime"
)

// 本文件: 插件卸载与平台可执行文件后缀。
// (原 bundled 捆绑机制已移除: 插件统一经 GitHub Release / 本地 zip 安装, 不再随主程序内置。)

// PluginUninstall 卸载插件: 停止 + 删除目录。
func (s *PluginService) PluginUninstall(pluginID string) string {
	inst := s.getInstance(pluginID)
	if inst == nil {
		return `{"error":` + mustJSONString("未知插件: "+pluginID) + `}`
	}
	if inst.status != pluginStatusDisabled {
		s.stopInstance(inst)
	}
	// 进程刚被 Kill, exe 文件锁在 Windows 上可能尚未释放。不等锁直接删会出现
	// "卸载报错但插件已从注册表消失"的不一致状态, 故先等锁再重试删除。
	if err := waitUnlocked(filepath.Join(inst.dir, pluginID+exeSuffix()), dirLockBudget); err != nil {
		CollectError("plugin:"+pluginID, "uninstall-waitlock", err)
		return `{"error":` + mustJSONString(err.Error()) + `}`
	}
	if err := removeAllWithRetry(inst.dir, dirLockBudget); err != nil {
		CollectError("plugin:"+pluginID, "uninstall-remove", err)
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
	s.mu.Lock()
	delete(s.instances, pluginID)
	delete(s.crashAttempts, pluginID)
	s.mu.Unlock()
	s.emitRegistry()
	s.emitInvalidated(pluginID, "uninstall")
	return mustJSON(map[string]any{"ok": true})
}

// exeSuffix 平台可执行文件后缀。
func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}
