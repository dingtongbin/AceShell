//go:build windows

package services

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"
)

// applyNativeTitleBarTheme 运行时将原生标题栏切换为应用亮/暗主题对应颜色。
// 通过 DWM 属性着色（DWMWA_CAPTION_COLOR/TEXT_COLOR/BORDER_COLOR），
// 标题栏按钮与窗口行为保持完全原生；Win10 不支持自定义颜色时回退系统原生暗/亮标题栏。
func applyNativeTitleBarTheme(win *application.WebviewWindow, dark bool) {
	hwnd := uintptr(win.NativeWindow())
	p := lightTitleBar
	if dark {
		p = darkTitleBar
	}
	w32.SetTheme(hwnd, dark)
	w32.SetTitleBarColour(hwnd, p.bar)
	w32.SetTitleTextColour(hwnd, p.text)
	w32.SetBorderColour(hwnd, p.border)
}
