//go:build windows

package services

import (
	"fmt"
	"os"
	"time"
)

// waitUnlocked 等待 Windows 上运行中的 exe 释放文件锁。
//
// 判定方式: 以 O_RDWR 打开目标文件。
// Windows 加载器映射 PE 镜像后会以"只允许读共享"的方式持有句柄,
// 此时任何请求写访问的新句柄都会得到 ERROR_SHARING_VIOLATION —— 这比
// "尝试改名再改回"安全得多(改名探测一旦在两步之间崩溃, exe 会永久改名,
// 插件再也起不来)。
//
// 文件不存在视为无需等待(全新安装路径)。
func waitUnlocked(path string, budget time.Duration) error {
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	deadline := time.Now().Add(budget)
	for {
		f, err := os.OpenFile(path, os.O_RDWR, 0)
		if err == nil {
			_ = f.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("等待可执行文件锁释放超时(%s): %w", budget, err)
		}
		time.Sleep(dirLockPoll)
	}
}
