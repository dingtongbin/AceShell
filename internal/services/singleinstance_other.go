//go:build !windows

package services

// CheckSingleInstance 非 Windows 平台暂不限制(返回 true = 允许启动)。
func CheckSingleInstance() bool { return true }

// ShowFatalBox 显示致命错误弹窗(非 Windows 无实现)。
func ShowFatalBox(title, text string) {}
