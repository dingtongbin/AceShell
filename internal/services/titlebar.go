package services

import "github.com/wailsapp/wails/v3/pkg/application"

// titleBarPalette 原生标题栏配色（背景/标题文字/窗口边框），与应用前端亮暗主题背景一致。
type titleBarPalette struct {
	bar    uint32
	text   uint32
	border uint32
}

// colorRef 生成 Windows COLORREF 颜色值（0x00BBGGRR）。
func colorRef(r, g, b uint8) uint32 {
	return uint32(r) | uint32(g)<<8 | uint32(b)<<16
}

// 暗色：背景 #343434 / 文字 #d4d4d4 / 边框 #3c3c3c（对应前端 --toolbar-bg/--text-color/--border-color，即左侧竖菜单栏）
var darkTitleBar = titleBarPalette{
	bar:    colorRef(52, 52, 52),
	text:   colorRef(212, 212, 212),
	border: colorRef(60, 60, 60),
}

// 亮色：背景 #e1e1e1 / 文字 #1a1a1a / 边框 #e0e0e0（对应前端 --toolbar-bg/--text-color/--border-color，即左侧竖菜单栏）
var lightTitleBar = titleBarPalette{
	bar:    colorRef(225, 225, 225),
	text:   colorRef(26, 26, 26),
	border: colorRef(224, 224, 224),
}

// TitleBarCustomTheme 返回主窗口创建时使用的 Windows CustomTheme 配置，
// 使原生标题栏在窗口创建及（auto 模式下）系统主题切换时即应用对应颜色。
func TitleBarCustomTheme() application.ThemeSettings {
	return application.ThemeSettings{
		DarkModeActive: &application.WindowTheme{
			TitleBarColour: &darkTitleBar.bar,
			TitleTextColour: &darkTitleBar.text,
			BorderColour:   &darkTitleBar.border,
		},
		DarkModeInactive: &application.WindowTheme{
			TitleBarColour: &darkTitleBar.bar,
			TitleTextColour: &darkTitleBar.text,
			BorderColour:   &darkTitleBar.border,
		},
		LightModeActive: &application.WindowTheme{
			TitleBarColour: &lightTitleBar.bar,
			TitleTextColour: &lightTitleBar.text,
			BorderColour:   &lightTitleBar.border,
		},
		LightModeInactive: &application.WindowTheme{
			TitleBarColour: &lightTitleBar.bar,
			TitleTextColour: &lightTitleBar.text,
			BorderColour:   &lightTitleBar.border,
		},
	}
}
