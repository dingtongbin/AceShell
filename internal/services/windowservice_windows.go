//go:build windows

package services

import (
	"os/exec"
	"strings"
	"syscall"
)

// HideWindow 隐藏子进程窗口,避免弹出黑色控制台(跨平台导出,非 Windows 为空实现)。
func HideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}

// openWithDefault 使用系统默认程序打开文件或目录。
func openWithDefault(filePath string) error {
	return exec.Command("explorer.exe", filePath).Start()
}

// openWithEditor 触发系统的 edit 动作打开文件;失败时回退默认程序。
func openWithEditor(filePath string) error {
	escaped := strings.ReplaceAll(filePath, "'", "''")
	psScript := "$s = New-Object -ComObject Shell.Application; $s.Namespace(0).ParseName('" + escaped + "').InvokeVerb('edit')"
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psScript)
	HideWindow(cmd)
	if err := cmd.Run(); err == nil {
		return nil
	}
	return openWithDefault(filePath)
}
