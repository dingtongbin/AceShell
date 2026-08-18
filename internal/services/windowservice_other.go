//go:build !windows && !darwin

package services

import (
	"os"
	"os/exec"
	"strings"
)

// shellQuote 将参数包裹为单引号,供 sh -c 拼接使用。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// openWithDefault 使用 xdg-open 打开文件或目录。
func openWithDefault(filePath string) error {
	return exec.Command("xdg-open", filePath).Start()
}

// openWithEditor 使用 $EDITOR 打开文件;未设置时回退 xdg-open。
func openWithEditor(filePath string) error {
	if editor := os.Getenv("EDITOR"); editor != "" {
		return exec.Command("sh", "-c", editor+" "+shellQuote(filePath)).Start()
	}
	return openWithDefault(filePath)
}
