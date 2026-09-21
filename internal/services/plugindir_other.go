//go:build !windows

package services

import "time"

// waitUnlocked 非 Windows 平台无需等待: 运行中的可执行文件可以直接改名/删除
// (inode 语义), 目录替换与卸载都不会被文件锁挡住。
//
// 注意: 不要在这里尝试用 O_RDWR 探测 —— Linux 上对正在执行的二进制请求写访问
// 会返回 ETXTBSY, 反而会把每次重载都拖到超时。
func waitUnlocked(_ string, _ time.Duration) error { return nil }
