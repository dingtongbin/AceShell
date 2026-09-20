//go:build !windows

package pluginsdk

import "os/exec"

// HideWindow 非 Windows 平台为空实现(保持调用点跨平台)。
func HideWindow(cmd *exec.Cmd) {}
