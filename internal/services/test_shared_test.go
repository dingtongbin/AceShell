package services

import (
	"fmt"
	"os"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// TestMain 全局兜底隔离: 测试二进制启动即把数据目录重定向到进程级临时目录。
// 即使某个测试忘记调用 withTestDataDir,也绝不会读写真实 %AppData%\AceShell,
// 杜绝测试清除已存储密钥(credential.key / agent.toml 密文 / mcp.toml token)。
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "aceshell-test-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "创建测试数据目录失败:", err)
		os.Exit(1)
	}
	restore := SetDataDir(dir)
	code := m.Run()
	restore()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func createDummyApp() *application.App {
	return application.New(application.Options{Name: "test"})
}

// withTestDataDir 将应用数据目录重定向到临时目录(测试隔离),返回临时目录路径。
func withTestDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cleanup := SetDataDir(dir)
	t.Cleanup(cleanup)
	return dir
}

func initTestServices() {
	dummyApp := createDummyApp()

	if AppServiceRegistry["ssh"] == nil {
		SSHSvc := &SSHService{}
		SSHSvc.SetApp(dummyApp)
		AppServiceRegistry["ssh"] = SSHSvc
	}
	if AppServiceRegistry["telnet"] == nil {
		telSvc := &DirectTelnetService{}
		telSvc.SetApp(dummyApp)
		AppServiceRegistry["telnet"] = telSvc
	}
}
