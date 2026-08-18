//go:build !windows
// +build !windows

package services

// CheckSingleInstance 非 Windows 平台暂不检查单实例。
func CheckSingleInstance() bool {
	return true
}
