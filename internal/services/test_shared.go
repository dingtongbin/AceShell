package services

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

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
