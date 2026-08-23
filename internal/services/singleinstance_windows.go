//go:build windows

package services

import (
	"golang.org/x/sys/windows"
)

const singleInstanceMutexName = "Global\\AceShell_SingleInstance_Mutex_v2"

// singleInstanceHandle 单实例互斥体句柄(持有到进程退出,由 OS 自动释放)。
var singleInstanceHandle windows.Handle

// CheckSingleInstance 检查是否已有实例运行。
// 返回 true 表示是第一个实例,false 表示已有实例在运行。
//
// 正确性说明:CreateMutex 在互斥体已存在时仍返回有效句柄,
// 必须通过 ERROR_ALREADY_EXISTS 判定重复——旧实现仅判断句柄非 0,
// 导致双开永远放行,双实例内存配置不同步(表现为密钥"保存了却读不到")。
func CheckSingleInstance() bool {
	name, err := windows.UTF16PtrFromString(singleInstanceMutexName)
	if err != nil {
		return true // 异常不阻塞启动
	}
	handle, err := windows.CreateMutex(nil, true, name)
	if err == windows.ERROR_ALREADY_EXISTS {
		return false
	}
	if err != nil && handle == 0 {
		return true // 创建失败不阻塞启动(权限受限环境)
	}
	singleInstanceHandle = handle // 故意不释放:生命周期绑定进程
	return true
}

// ShowFatalBox 显示致命错误弹窗(启动早期无 GUI 时使用)。
func ShowFatalBox(title, text string) {
	t, _ := windows.UTF16PtrFromString(title)
	x, _ := windows.UTF16PtrFromString(text)
	_, _ = windows.MessageBox(0, x, t, windows.MB_ICONERROR|windows.MB_OK)
}
