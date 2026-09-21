package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
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
	application.RegisterEvent[string]("mcp-audit-appended")
	application.RegisterEvent[string]("mcp-status-changed")
	application.RegisterEvent[string]("mcp-critical-blocked")
	// 插件服务
	application.RegisterEvent[string]("plugin-registry-changed")
	application.RegisterEvent[string]("plugin-invalidated")
	application.RegisterEvent[string]("plugin-status-changed")
	application.RegisterEvent[string]("plugin-open-tab")
	application.RegisterEvent[string]("plugin-tab-updated")
	application.RegisterEvent[string]("plugin-tab-closed")
	application.RegisterEvent[string]("plugin-toast")
	application.RegisterEvent[string]("plugin-event")
	// 全局错误收集器: 每条新错误实时推送前端
	application.RegisterEvent[string]("app-error-collected")
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
	vnc          *appservices.VncService
	mcp          *appservices.McpService
	plugins      *appservices.PluginService
}

// main 应用入口。
func main() {
	if !appservices.CheckSingleInstance() {
		appservices.ShowFatalBox("AceShell", "AceShell 已在运行,请勿双开(双实例会导致配置不同步)。请使用已打开的窗口,或在任务管理器结束后再启动。")
		return
	}

	svc := initServices()
	setupCleanup(func() { svc.rdp.Stop(); svc.vnc.Stop(); svc.plugins.StopAll() })

	app := createApp(svc)
	wireServices(svc, app)
	createMainWindow(app, svc)

	if err := app.Run(); err != nil {
		fmt.Println(err)
	}
	svc.mcp.Stop()
	svc.rdp.Stop()
	svc.vnc.Stop()
	svc.plugins.StopAll()
}

// setupCleanup 注册退出清理，确保子进程被终止、图形会话桥(RDP/VNC)被关闭。
func setupCleanup(onExit func()) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		onExit()
		killChildProcesses()
		appservices.MainErrors.Close()
		os.Exit(0)
	}()
}

// killChildProcesses 的实现按构建模式拆分:
// 开发模式见 main_cleanup_dev.go,生产模式见 main_cleanup_prod.go。

// initServices 创建并初始化所有后端服务实例。
func initServices() *services {
	// 错误收集器最先装配: 之后任何服务的启动失败都可被收集
	// (CollectError nil 容忍, 但尽早初始化可覆盖启动期窗口)。
	appservices.MainErrors = appservices.NewErrorCollector(appservices.ErrorLogDir())
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
		vnc:          &appservices.VncService{},
	}

	svc.config.Init()
	svc.log.Init()
	svc.mcp = appservices.NewMcpService(svc.config, svc.sessionFile)
	svc.plugins = appservices.NewPluginService(svc.config, svc.sessionFile, hostVueShimSource())

	return svc
}

// hostVueShimSource 从内嵌前端产物读取宿主 Vue shim(构建时由 vite 插件
// hostVueShim 自动生成, 导出面与宿主安装的 vue 同步)。缺失时返回空串,
// PluginService 的资产处理器退回内置兜底清单。
func hostVueShimSource() string {
	raw, err := fs.ReadFile(assets, "frontend/dist/plugins-host-vue.js")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "读取宿主 Vue shim 失败: %v\n", err)
	}
	return string(raw)
}

// webviewDebugArgs 诊断用追加参数(空环境变量返回 nil, 不影响正常启动)。
func webviewDebugArgs() []string {
	v := strings.TrimSpace(os.Getenv("ACESHELL_WEBVIEW_ARGS"))
	if v == "" {
		return nil
	}
	return strings.Fields(v)
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
			application.NewService(svc.vnc),
			application.NewService(svc.mcp),
			application.NewService(svc.plugins),
		},
		Assets: application.AssetOptions{
			Handler: func() http.Handler {
				pluginsAsset := svc.plugins.AssetHandler()
				embedded := application.AssetFileServerFS(assets)
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					// 插件前端资产(/plugins/...)优先; 其余走内嵌资源
					if strings.HasPrefix(r.URL.Path, "/plugins/") {
						pluginsAsset.ServeHTTP(w, r)
						return
					}
					embedded.ServeHTTP(w, r)
				})
			}(),
		},
		Windows: application.WindowsOptions{
			// 诊断开关: ACESHELL_WEBVIEW_ARGS="--remote-debugging-port=9333" 追加
			// WebView2 浏览器参数(设置页卡死类问题需 CDP 现场排查); 未设置时零影响。
			AdditionalBrowserArgs: webviewDebugArgs(),
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

	// 全局错误收集器: app 可用后装配前端实时推送
	appservices.MainErrors.SetEmitter(func(e appservices.ErrorEntry) {
		if data, err := json.Marshal(e); err == nil {
			app.Event.Emit("app-error-collected", string(data))
		}
	})

	appservices.AppServiceRegistry["telnet"] = svc.directTelnet
	appservices.AppServiceRegistry["ssh"] = svc.ssh
	appservices.AppServiceRegistry["serial"] = svc.serial
	appservices.AppServiceRegistry["shell"] = svc.local

	// RDP 图形会话桥:启动本机 WebSocket 字节桥(仅 127.0.0.1)
	svc.rdp.SetApp(app)
	if _, err := svc.rdp.Start(); err != nil {
		fmt.Printf("RDP bridge start failed: %v\n", err)
		appservices.CollectError("rdp", "start", err)
	}
	svc.rdp.SetSessionFiles(svc.sessionFile)

	// VNC 图形会话桥:同一 wsbridge 普通透传路径(仅 127.0.0.1)
	if _, err := svc.vnc.Start(); err != nil {
		fmt.Printf("VNC bridge start failed: %v\n", err)
		appservices.CollectError("vnc", "start", err)
	}
	svc.vnc.SetSessionFiles(svc.sessionFile)

	// MCP 服务:注入应用实例供事件推送;配置了随应用启动则自动拉起
	svc.mcp.SetApp(app)
	appservices.MainMcpService = svc.mcp
	if svc.config.McpEnabled() {
		if err := svc.mcp.Start(); err != nil {
			fmt.Printf("MCP service start failed: %v\n", err)
			appservices.CollectError("mcp", "start", err)
		}
	}

	// 插件宿主:注入应用实例后扫描并启动全部已启用插件
	svc.plugins.SetApp(app)
	svc.plugins.StartAll()
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
