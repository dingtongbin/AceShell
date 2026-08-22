//go:build production

package main

// killChildProcesses 生产模式空实现。
// 开发期的 taskkill /im node.exe 会误杀用户机器上所有 node.exe
// (其他项目、编辑器插件等),生产构建禁止执行;仅开发模式保留清理逻辑。
func killChildProcesses() {}
