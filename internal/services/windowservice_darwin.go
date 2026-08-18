//go:build darwin

package services

import "os/exec"

// HideWindow 非 Windows 平台无隐藏窗口概念,空实现。
func HideWindow(cmd *exec.Cmd) {}

// openWithDefault 使用系统默认程序打开文件或目录。
func openWithDefault(filePath string) error {
	return exec.Command("open", filePath).Start()
}

// openWithEditor 使用系统默认文本编辑器(TextEdit)打开文件。
func openWithEditor(filePath string) error {
	return exec.Command("open", "-e", filePath).Start()
}
