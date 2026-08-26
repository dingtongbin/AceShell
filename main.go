package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
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
// 规则:Emit 过的事件必须在此注册;已废弃的事件应同步移除注册与 Emit 点。
func init() {
	application.RegisterEvent[string]("theme-changed")
	application.RegisterEvent[string]("session-output")
	application.RegisterEvent[string]("session-status-changed")
	application.RegisterEvent[string]("session-tree-changed")
	application.RegisterEvent[string]("sftp-transfer-progress")
	application.RegisterEvent[string]("sftp-files-dropped")
	// MCP 服务
	application.RegisterEvent[string]("mcp-command")
	application.RegisterEvent[string]("mcp-approval-requested")
	application.RegisterEvent[string]("mcp-approval-removed")
	application.RegisterEvent[string]("mcp-audit-appended")
	application.RegisterEvent[string]("mcp-status-changed")
	application.RegisterEvent[string]("mcp-critical-blocked")
	// 内嵌智能体
	application.RegisterEvent[string]("agent-event")
	application.RegisterEvent[string]("agent-stream")
	application.RegisterEvent[string]("agent-status-changed")
	application.RegisterEvent[string]("agent-pending-changed")
	application.RegisterEvent[string]("agent-error")
}

// services 聚合所有后端服务实例，便于统一初始化和注入。
type services struct {
	directTelnet *appservices.DirectTelnetService
	ssh          *appservices.SSHService
	serial       *appservices.SerialService
	local        *appservices.LocalService
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
	rdp          *appservices.RdpService
	mcp          *appservices.McpService
	agent        *appservices.AgentService
}

// main 应用入口。
func main() {
	if !appservices.CheckSingleInstance() {
		appservices.ShowFatalBox("AceShell", "AceShell 已在运行,请勿双开(双实例会导致配置不同步)。请使用已打开的窗口,或在任务管理器结束后再启动。")
		return
	}

	svc := initServices()
	setupCleanup(func() { svc.rdp.Stop() })

	app := createApp(svc)
	wireServices(svc, app)
	createMainWindow(app, svc)

	if err := app.Run(); err != nil {
		fmt.Println(err)
	}
	svc.mcp.Stop()
	svc.rdp.Stop()
}

// setupCleanup 注册退出清理，确保子进程被终止、RDP 桥被关闭。
func setupCleanup(onExit func()) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		onExit()
		killChildProcesses()
		os.Exit(0)
	}()
}

// killChildProcesses 的实现按构建模式拆分:
// 开发模式见 main_cleanup_dev.go,生产模式见 main_cleanup_prod.go。

// initServices 创建并初始化所有后端服务实例。
func initServices() *services {
	svc := &services{
		directTelnet: &appservices.DirectTelnetService{},
		ssh:          &appservices.SSHService{},
		serial:       &appservices.SerialService{},
		local:        &appservices.LocalService{},
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
		rdp:          &appservices.RdpService{},
	}

	svc.config.Init()
	svc.log.Init()
	svc.mcp = appservices.NewMcpService(svc.config, svc.sessionFile)
	svc.agent = appservices.NewAgentService(svc.config, svc.mcp)

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
			application.NewService(svc.local),
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
		application.NewService(svc.rdp),
		application.NewService(svc.mcp),
		application.NewService(svc.agent),
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
	svc.local.SetApp(app)
	svc.window.SetApp(app)
	svc.sessionFile.SetApp(app)
	svc.sftp.SetApp(app)
	svc.sftp.SSHSvc = svc.ssh

	// 全局日志服务
	appservices.MainLogService = svc.log

	appservices.AppServiceRegistry["telnet"] = svc.directTelnet
	appservices.AppServiceRegistry["ssh"] = svc.ssh
	appservices.AppServiceRegistry["serial"] = svc.serial
	appservices.AppServiceRegistry["shell"] = svc.local

	// RDP 图形会话桥:启动本机 WebSocket 字节桥(仅 127.0.0.1)
	svc.rdp.SetApp(app)
	if _, err := svc.rdp.Start(); err != nil {
		fmt.Printf("RDP bridge start failed: %v\n", err)
	}
	svc.rdp.SetSessionFiles(svc.sessionFile)

	// MCP 服务:注入应用实例供事件推送;配置了随应用启动则自动拉起
	svc.mcp.SetApp(app)
	appservices.MainMcpService = svc.mcp
	if svc.config.McpEnabled() {
		if err := svc.mcp.Start(); err != nil {
			fmt.Printf("MCP service start failed: %v\n", err)
		}
	}

	// 内嵌智能体:注入应用实例供事件推送
	svc.agent.SetApp(app)
}

// createMainWindow 创建主窗口。
func createMainWindow(app *application.App, svc *services) {
	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "AceShell",
		Width:            1100,
		Height:           700,
		BackgroundColour: application.NewRGB(30, 30, 30),
		URL:              "/",
		MinWidth:         800,
		MinHeight:        500,
		EnableFileDrop:   true,
		// 自绘标题栏:Frameless 窗口,菜单栏融入窗口控制(─□✕)与拖拽区;
		// 关闭时回退系统原生标题栏(设置弹窗可即时切换,运行时走 Window.SetFrameless)
		Frameless: svc.config.CustomTitlebarEnabled(),
		Windows:   buildWindowsOptions(svc),
	})

	// 供 WindowService 在运行时调整原生标题栏主题色
	svc.window.SetMainWindow(win)

	// 系统文件拖入窗口时转发给前端（仅命中 SFTP 远端拖放目标时）。
	win.OnWindowEvent(events.Common.WindowFilesDropped, func(event *application.WindowEvent) {
		payload, ok := buildSftpDropPayload(event.Context().DroppedFiles(), event.Context().DropTargetDetails())
		if !ok {
			return
		}
		app.Event.Emit("sftp-files-dropped", payload)
	})

	// 窗口关闭时强制落盘面板布局(AI 面板显隐/宽度、资源管理器宽度):
	// 面板布局走"内存更新 + 周期写盘",关闭必写保证持久化不丢。
	win.OnWindowEvent(events.Common.WindowClosing, func(event *application.WindowEvent) {
		svc.config.FlushPanelLayout()
	})
}

// buildWindowsOptions 根据已保存的主题模式构建 Windows 平台窗口选项。
// 强制 dark/light 时不注册系统主题监听（避免系统切换覆盖应用主题），auto 跟随系统；
// CustomTheme 让原生标题栏在窗口创建及系统主题变化时即应用与应用背景一致的颜色。
func buildWindowsOptions(svc *services) application.WindowsWindow {
	theme := application.SystemDefault
	switch svc.config.ThemeMode() {
	case "dark":
		theme = application.Dark
	case "light":
		theme = application.Light
	}

	return application.WindowsWindow{
		Theme:       theme,
		CustomTheme: appservices.TitleBarCustomTheme(),
	}
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
