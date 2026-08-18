package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	appservices "changeme/internal/services"
)

//go:embed all:frontend/dist
var assets embed.FS

// init 注册所有 Wails 事件类型，必须在 main 之前执行。
func init() {
	application.RegisterEvent[string]("theme-changed")
	application.RegisterEvent[string]("session-output")
	application.RegisterEvent[string]("session-status-changed")
	application.RegisterEvent[string]("session-list-changed")
	application.RegisterEvent[string]("session-tree-changed")
	application.RegisterEvent[string]("sftp-transfer-progress")
	application.RegisterEvent[string]("sftp-status-changed")
	application.RegisterEvent[string]("sftp-add-tab")
	application.RegisterEvent[string]("sftp-files-dropped")
}

// services 聚合所有后端服务实例，便于统一初始化和注入。
type services struct {
	directTelnet *appservices.DirectTelnetService
	ssh          *appservices.SSHService
	serial       *appservices.SerialService
	fileTree     *appservices.FileTreeService
	scriptFile   *appservices.ScriptFileService
	window       *appservices.WindowService
	sessionFile  *appservices.SessionFileService
	config       *appservices.ConfigService
	sftp         *appservices.SFTPService
	log          *appservices.LogService
	globalKeys   *appservices.GlobalKeyService
	browser      *appservices.BrowserService
	clipboard    *appservices.ClipboardService
	version      *appservices.VersionService
}

// main 应用入口。
func main() {
	if !appservices.CheckSingleInstance() {
		fmt.Println("AceShell 已在运行")
		return
	}

	setupCleanup()

	svc := initServices()
	app := createApp(svc)
	wireServices(svc, app)
	createMainWindow(app)

	if err := app.Run(); err != nil {
		fmt.Println(err)
	}
}

// setupCleanup 注册退出清理，确保子进程被终止。
func setupCleanup() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		killChildProcesses()
		os.Exit(0)
	}()
}

// killChildProcesses 杀死所有 node 子进程（开发服务器）。
func killChildProcesses() {
	if _, err := exec.LookPath("taskkill"); err == nil {
		cmd := exec.Command("taskkill", "/f", "/im", "node.exe")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		cmd.Run()
	}
}

// initServices 创建并初始化所有后端服务实例。
func initServices() *services {
	svc := &services{
		directTelnet: &appservices.DirectTelnetService{},
		ssh:          &appservices.SSHService{},
		serial:       &appservices.SerialService{},
		fileTree:     &appservices.FileTreeService{},
		scriptFile:   &appservices.ScriptFileService{},
		window:       &appservices.WindowService{},
		sessionFile:  &appservices.SessionFileService{},
		config:       &appservices.ConfigService{},
		sftp:         &appservices.SFTPService{},
		log:          &appservices.LogService{},
		globalKeys:   &appservices.GlobalKeyService{},
		browser:      &appservices.BrowserService{},
		clipboard:    &appservices.ClipboardService{},
		version:      &appservices.VersionService{},
	}

	svc.config.Init()
	svc.log.Init()

	return svc
}

// createApp 创建 Wails 应用实例并注册所有服务。
func createApp(svc *services) *application.App {
	return application.New(application.Options{
		Name:        "AceShell",
		Description: "Network Shell Terminal",
		Services: []application.Service{
			application.NewService(svc.directTelnet),
			application.NewService(svc.ssh),
			application.NewService(svc.serial),
			application.NewService(svc.fileTree),
			application.NewService(svc.scriptFile),
			application.NewService(svc.window),
			application.NewService(svc.sessionFile),
			application.NewService(svc.config),
			application.NewService(svc.sftp),
			application.NewService(svc.log),
			application.NewService(svc.globalKeys),
			application.NewService(svc.browser),
			application.NewService(svc.clipboard),
			application.NewService(svc.version),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})
}

// wireServices 注入服务间依赖关系。
func wireServices(svc *services, app *application.App) {
	svc.directTelnet.SetApp(app)
	svc.ssh.SetApp(app)
	svc.ssh.SessionFileSvc = svc.sessionFile
	svc.sessionFile.SSHSvc = svc.ssh
	svc.sessionFile.GlobalKeys = svc.globalKeys
	svc.serial.SetApp(app)
	svc.window.SetApp(app)
	svc.sessionFile.SetApp(app)
	svc.sftp.SetApp(app)
	svc.sftp.SSHSvc = svc.ssh

	// 全局日志服务
	appservices.MainLogService = svc.log

	appservices.AppServiceRegistry["telnet"] = svc.directTelnet
	appservices.AppServiceRegistry["ssh"] = svc.ssh
	appservices.AppServiceRegistry["serial"] = svc.serial
}

// createMainWindow 创建主窗口。
func createMainWindow(app *application.App) {
	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "AceShell",
		Width:            1100,
		Height:           700,
		BackgroundColour: application.NewRGB(30, 30, 30),
		URL:              "/",
		MinWidth:         800,
		MinHeight:        500,
		EnableFileDrop:   true,
	})

	// 系统文件拖入窗口时转发给前端（仅命中 SFTP 远端拖放目标时）。
	win.OnWindowEvent(events.Common.WindowFilesDropped, func(event *application.WindowEvent) {
		payload, ok := buildSftpDropPayload(event.Context().DroppedFiles(), event.Context().DropTargetDetails())
		if !ok {
			return
		}
		app.Event.Emit("sftp-files-dropped", payload)
	})
}

// buildSftpDropPayload 将系统拖入的文件组装为前端可用的 JSON 字符串。
// 仅当拖放目标命中 SFTP 远端面板（id 以 sftp-remote-drop- 开头）且文件非空时返回可用载荷。
func buildSftpDropPayload(files []string, details *application.DropTargetDetails) (string, bool) {
	if len(files) == 0 || details == nil || !strings.HasPrefix(details.ElementID, "sftp-remote-drop-") {
		return "", false
	}
	payload, err := json.Marshal(map[string]any{
		"files":   files,
		"panelId": details.ElementID,
	})
	if err != nil {
		return "", false
	}
	return string(payload), true
}
