//go:build !production

package main

import (
	"os/exec"

	appservices "changeme/internal/services"
)

// killChildProcesses 杀死所有 node 子进程（开发服务器）。
// 仅开发模式构建生效:生产环境绝不能按镜像名误杀用户机器上
// 其他无关的 node.exe 进程(见 main_cleanup_prod.go)。
func killChildProcesses() {
	if _, err := exec.LookPath("taskkill"); err == nil {
		cmd := exec.Command("taskkill", "/f", "/im", "node.exe")
		appservices.HideWindow(cmd)
		cmd.Run()
	}
}
