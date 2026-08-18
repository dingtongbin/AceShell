//go:build !windows && !darwin

package services

import (
	"os"
	"os/exec"
)

// HideWindow 非 Windows 平台无隐藏窗口概念,空实现。
func HideWindow(cmd *exec.Cmd) {}

// openWithDefault 使用 xdg-open 打开文件或目录。
func openWithDefault(filePath string) error {
	return exec.Command("xdg-open", filePath).Start()
}

// openWithEditor 使用 $EDITOR 打开文件;未设置时回退 xdg-open。
// shellQuote 见 globalkey.go。
func openWithEditor(filePath string) error {
	if editor := os.Getenv("EDITOR"); editor != "" {
		return exec.Command("sh", "-c", editor+" "+shellQuote(filePath)).Start()
	}
	return openWithDefault(filePath)
}
