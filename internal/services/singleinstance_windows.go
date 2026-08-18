//go:build windows
// +build windows

package services

import (
	"syscall"
	"unsafe"
)

var (
	kernel32        = syscall.NewLazyDLL("kernel32.dll")
	procCreateMutex = kernel32.NewProc("CreateMutexW")
)

// CheckSingleInstance 检查是否已有实例运行。
// 返回 true 表示是第一个实例，false 表示已有实例在运行。
func CheckSingleInstance() bool {
	mutexName, _ := syscall.UTF16PtrFromString("AceShell_SingleInstance_Mutex")
	handle, _, _ := procCreateMutex.Call(0, 0, uintptr(unsafe.Pointer(mutexName)))
	// ERROR_ALREADY_EXISTS = 183
	err := syscall.GetLastError()
	if err != nil && err.Error() == "The operation completed successfully." {
		return true
	}
	return handle != 0
}
