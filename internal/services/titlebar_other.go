//go:build !windows

package services

import "github.com/wailsapp/wails/v3/pkg/application"

func applyNativeTitleBarTheme(win *application.WebviewWindow, dark bool) {
	// macOS/Linux: 原生标题栏跟随系统，不做自定义
}
