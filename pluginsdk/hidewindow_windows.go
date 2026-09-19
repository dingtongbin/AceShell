//go:build windows

package pluginsdk

import (
	"os/exec"
	"syscall"
)

// HideWindow 隐藏子进程控制台窗口(Windows; 插件内再拉起 ping.exe 等命令行工具时使用)。
func HideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
