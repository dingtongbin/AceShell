package services

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/pelletier/go-toml/v2"
)

type AppConfig struct {
	View         ViewConfig         `toml:"view" json:"view"`
	Sections     SectionsConfig     `toml:"sections" json:"sections"`
	Serial       SerialConfig       `toml:"serial" json:"serial"`
	Terminal     TerminalConfig     `toml:"terminal" json:"terminal"`
	FileEditing  FileEditingConfig  `toml:"fileEditing" json:"fileEditing"`
	Mcp          McpConfig          `toml:"mcp" json:"mcp"`
	Plugins      PluginsConfig      `toml:"plugins" json:"plugins"`
	Language     string             `toml:"language" json:"language"`
}

// mainConfigFile 主配置文件(config.toml)落盘结构: 不含 mcp 节(拆分至 mcp.toml)。
// 注意: AppConfig 新增非 mcp 字段时需同步此结构。
type mainConfigFile struct {
	View        ViewConfig        `toml:"view" json:"view"`
	Sections    SectionsConfig    `toml:"sections" json:"sections"`
	Serial      SerialConfig      `toml:"serial" json:"serial"`
	Terminal    TerminalConfig    `toml:"terminal" json:"terminal"`
	FileEditing FileEditingConfig `toml:"fileEditing" json:"fileEditing"`
	Plugins     PluginsConfig     `toml:"plugins" json:"plugins"`
	Language    string            `toml:"language" json:"language"`
}

// PluginsConfig 插件配置: 显式禁用表(未出现的插件默认启用)与捆绑插件卸载记录。
type PluginsConfig struct {
	// Enabled 插件ID → 是否启用。nil/缺项 = 启用; 显式 false = 禁用。
	Enabled map[string]bool `toml:"enabled" json:"enabled"`
	// UninstalledBundled 用户已卸载的捆绑插件 ID(发版升级后不复活)。
	UninstalledBundled []string `toml:"uninstalledBundled" json:"uninstalledBundled"`
}

// McpConfig MCP 服务配置(令牌密文经 encryptSecret 加密,不含明文)。
type McpConfig struct {
	Enabled  bool   `toml:"enabled" json:"enabled"`
	Port     int    `toml:"port" json:"port"`
	TokenEnc string `toml:"tokenEnc" json:"tokenEnc"`
	BallX    int    `toml:"ballX" json:"ballX"` // 悬浮球位置(-1 未初始化)
	BallY    int    `toml:"ballY" json:"ballY"`
	// 单操作可视时延(毫秒): 激活目标标签页后等待,让用户看清操作并可抢占。0-10000。
	OpDelayMs int `toml:"opDelayMs" json:"opDelayMs"`
	// 批量执行命令间隔(毫秒)。
	BatchIntervalMs int `toml:"batchIntervalMs" json:"batchIntervalMs"`
	// 审计日志磁盘保留天数。
	AuditRetentionDays int `toml:"auditRetentionDays" json:"auditRetentionDays"`
	// terminal_read 单次返回上限(字节)。
	TerminalReadMaxBytes int `toml:"terminalReadMaxBytes" json:"terminalReadMaxBytes"`
	// 绝对危险指令字典(正则列表,命中即拦截并挂起 MCP)。空 = 使用内置默认字典。
	DangerousPatterns []string `toml:"dangerousPatterns" json:"dangerousPatterns"`
}

type FileEditingConfig struct {
	AutoSave bool `toml:"autoSave" json:"autoSave"`
}

type ViewConfig struct {
	ShowSession      bool   `toml:"showSession" json:"showSession"`
	ShowScript       bool   `toml:"showScript" json:"showScript"`
	ShowAutoLog      bool   `toml:"showAutoLog" json:"showAutoLog"`
	ShowSerial       bool   `toml:"showSerial" json:"showSerial"`
	ShowToolbar      bool   `toml:"showToolbar" json:"showToolbar"`
	ShowSftp         bool   `toml:"showSftp" json:"showSftp"`
	ShowFilemanager  bool   `toml:"showFilemanager" json:"showFilemanager"`
	SidebarOrder     string `toml:"sidebarOrder" json:"sidebarOrder"`
	TabOrientation   string `toml:"tabOrientation" json:"tabOrientation"`
	VerticalTabWidth int    `toml:"verticalTabWidth" json:"verticalTabWidth"`
	CloseConfirm     bool   `toml:"closeConfirm" json:"closeConfirm"`
	Theme            string `toml:"theme" json:"theme"`
	// 自定义强调色(#RRGGBB;空 = 默认蓝 #0078d4)。派生主色族/语义色高亮。
	AccentColor  string `toml:"accentColor" json:"accentColor"`
	PanelOpacity int    `toml:"panelOpacity" json:"panelOpacity"`
	Wallpaper        string `toml:"wallpaper" json:"wallpaper"`
	ShowHelp         bool   `toml:"showHelp" json:"showHelp"`
	// 自绘标题栏(Frameless 窗口 + 顶部菜单栏融入窗口控制)开关。
	CustomTitlebar bool `toml:"customTitlebar" json:"customTitlebar"`
	// 资源管理器面板宽度(px)。
	SessionWidth int `toml:"sessionWidth" json:"sessionWidth"`
	// 智能助手总开关(视图菜单): 关闭时隐藏顶栏 MCP 按钮/资源管理器收纳按钮/AI 面板按钮。
	ShowAssistant bool `toml:"showAssistant" json:"showAssistant"`
}

type SectionState struct {
	Expanded bool `toml:"expanded" json:"expanded"`
	Size     int  `toml:"size" json:"size"`
}

type SectionsConfig struct {
	Session SectionState `toml:"session" json:"session"`
	Serial  SectionState `toml:"serial" json:"serial"`
	AutoLog SectionState `toml:"autolog" json:"autolog"`
}

// SerialConfig 串口连接参数
type SerialConfig struct {
	Port     string `toml:"port" json:"port"`
	BaudRate int    `toml:"baudRate" json:"baudRate"`
	DataBits int    `toml:"dataBits" json:"dataBits"`
	StopBits string `toml:"stopBits" json:"stopBits"`
	Parity   string `toml:"parity" json:"parity"`
}

// TerminalConfig 终端个性化与显示参数。
// 未开启个性化(Personalize=false)时,终端颜色跟随主题自动反转,其余外观参数仍生效。
type TerminalConfig struct {
	Personalize  bool    `toml:"personalize" json:"personalize"`
	FontColor    string  `toml:"fontColor" json:"fontColor"`
	BgColor      string  `toml:"bgColor" json:"bgColor"`
	BgOpacity    int     `toml:"bgOpacity" json:"bgOpacity"`
	BgImage      string  `toml:"bgImage" json:"bgImage"`
	FontFamily   string  `toml:"fontFamily" json:"fontFamily"`
	FontSize     int     `toml:"fontSize" json:"fontSize"`
	LineHeight   float64 `toml:"lineHeight" json:"lineHeight"`
	CopyOnSelect bool    `toml:"copyOnSelect" json:"copyOnSelect"`
	CursorBlink  bool    `toml:"cursorBlink" json:"cursorBlink"`
	CursorStyle  string  `toml:"cursorStyle" json:"cursorStyle"`
	Scrollback   int     `toml:"scrollback" json:"scrollback"`
}

type ConfigService struct {
	mu sync.Mutex
	config AppConfig
	// 面板布局防抖写盘: 前端高频调用(拖拽宽度/切换显隐)只更新内存,
	// panelDirty 标记脏数据, panelTimer 周期(1s)落盘一次, 关闭窗口时 Flush 必写。
	panelDirty bool
	panelTimer *time.Timer
}

// SetPanelLayout 更新面板布局(资源管理器宽度)。
// 仅写内存并标记脏; 由后台定时器周期落盘, 窗口关闭时 Flush 落盘, 保护磁盘。
func (c *ConfigService) SetPanelLayout(sessionWidth int) {
	clamp := func(v, lo, hi int) int {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.View.SessionWidth = clamp(sessionWidth, 60, 600)
	c.panelDirty = true
	// 定时器已启动则不重置: 保证固定周期写盘, 拖拽中不会连续触发 IO。
	if c.panelTimer == nil {
		c.panelTimer = time.AfterFunc(time.Second, func() { c.FlushPanelLayout() })
	}
}

// FlushPanelLayout 立即将面板布局落盘(若有脏数据)。窗口关闭时必调, 保证持久化不丢。
func (c *ConfigService) FlushPanelLayout() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.panelDirty {
		return
	}
	if c.panelTimer != nil {
		c.panelTimer.Stop()
		c.panelTimer = nil
	}
	if err := c.save(); err != nil {
		fmt.Printf("[config] flush panel layout failed: %v\n", err)
	}
	c.panelDirty = false
}

func (c *ConfigService) Init() {
	c.config = AppConfig{
		View: ViewConfig{
			ShowSession:      true,
			ShowScript:       true,
			ShowAutoLog:      true,
			ShowSerial:       true,
			ShowToolbar:      true,
			ShowSftp:         true,
			ShowFilemanager:  true,
			ShowHelp:         true,
			SidebarOrder:     "sftp,filemanager",
			TabOrientation:   "horizontal",
			VerticalTabWidth: 180,
			CloseConfirm:     true,
			Theme:            "dark",
			PanelOpacity:     100,
			// 自绘标题栏默认开启
			CustomTitlebar: true,
			// 资源管理器默认 220px
			SessionWidth:    220,
			// 智能助手(测试功能)默认开启,视图菜单可手动关闭
			ShowAssistant: true,
		},
		Sections: SectionsConfig{
			Session: SectionState{Expanded: true, Size: 0},
			Serial:  SectionState{Expanded: false, Size: 0},
			AutoLog: SectionState{Expanded: false, Size: 0},
		},
		Serial: SerialConfig{
			Port:     "",
			BaudRate: 115200,
			DataBits: 8,
			StopBits: "1",
			Parity:   "none",
		},
		Terminal: TerminalConfig{
			Personalize:  false,
			FontColor:    "#FFFFFF",
			BgColor:      "#0C0C0C",
			BgOpacity:    100,
			BgImage:      "",
			FontFamily:   "Cascadia Code",
			FontSize:     16,
			LineHeight:   1,
			CopyOnSelect: true,
			CursorBlink:  true,
			CursorStyle:  "bar",
			Scrollback:   1000,
		},
		FileEditing: FileEditingConfig{
			AutoSave: true,
		},
		Mcp: McpConfig{
			Enabled:              false,
			Port:                 8940,
			BallX:                -1,
			BallY:                -1,
			OpDelayMs:            1000,
			BatchIntervalMs:      300,
			AuditRetentionDays:   30,
			TerminalReadMaxBytes: 32768,
		},
		// 插件默认全部启用(显式禁用记录于 Enabled[id]=false)
		Plugins: PluginsConfig{
			Enabled: map[string]bool{},
		},
		Language: "zh-CN",
	}
	c.load()
}

func (c *ConfigService) load() {
	os.MkdirAll(filepath.Dir(configFile), 0700)
	data, err := os.ReadFile(configFile)
	rawMain := string(data)
	if err == nil {
		// 旧版主文件可能内嵌 mcp 节,先读入;随后被独立文件覆盖并触发迁移
		toml.Unmarshal(data, &c.config)
	}
	// 独立文件优先(权威源): mcp.toml
	if data, err := os.ReadFile(McpConfigFile()); err == nil {
		var f struct {
			Mcp McpConfig `toml:"mcp"`
		}
		if toml.Unmarshal(data, &f) == nil {
			c.config.Mcp = f.Mcp
		}
	}
	// 旧版主文件内嵌 mcp 节 → 拆分迁移一次(save 重写主文件剔除旧节并落盘独立文件)
	if tomlHasSection(rawMain, "mcp") {
		c.save()
	}
}

// logConfigLoadError 配置文件解析失败留痕(数据目录/configload.log)。
func logConfigLoadError(file string, err error) {
	dir := DataDir()
	line := fmt.Sprintf("%s %s: %v\n", time.Now().Format("2006-01-02 15:04:05"), file, err)
	if f, ferr := os.OpenFile(filepath.Join(dir, "configload.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600); ferr == nil {
		_, _ = f.WriteString(line)
		_ = f.Close()
	}
}

// tomlHasSection 判断 toml 原文是否含指定顶层节([x] / [x.y] / [[x]] / [[x.y]])。
func tomlHasSection(raw, name string) bool {
	for _, line := range strings.Split(raw, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "["+name+"]") || strings.HasPrefix(t, "["+name+".") ||
			strings.HasPrefix(t, "[["+name+"]") || strings.HasPrefix(t, "[["+name+".") {
			return true
		}
	}
	return false
}

func (c *ConfigService) save() error {
	fail := func(step string, err error) error {
		// 写盘失败必须留痕:配置丢失无感知比失败更危险
		logConfigLoadError("config-save", fmt.Errorf("%s: %v", step, err))
		fmt.Printf("[config] save failed (%s): %v\n", step, err)
		return err
	}
	// 主配置: 剔除 mcp(拆分至独立文件,禁止写入 config.toml)
	main := mainConfigFile{
		View:        c.config.View,
		Sections:    c.config.Sections,
		Serial:      c.config.Serial,
		Terminal:    c.config.Terminal,
		FileEditing: c.config.FileEditing,
		Plugins:     c.config.Plugins,
		Language:    c.config.Language,
	}
	mainData, err := toml.Marshal(main)
	if err != nil {
		return fail("marshal-main", err)
	}
	if err := atomicWriteFile(configFile, mainData, 0600); err != nil {
		return fail("write-main", err)
	}
	// mcp.toml
	mcpData, err := toml.Marshal(struct {
		Mcp McpConfig `toml:"mcp"`
	}{c.config.Mcp})
	if err != nil {
		return fail("marshal-mcp", err)
	}
	if err := atomicWriteFile(McpConfigFile(), mcpData, 0600); err != nil {
		return fail("write-mcp", err)
	}
	return nil
}

// ThemeMode 返回已保存的视图主题模式（dark / light / auto），默认 "dark"。
func (c *ConfigService) ThemeMode() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.config.View.Theme
	if t != "dark" && t != "light" && t != "auto" {
		return "dark"
	}
	return t
}

// configJSONLocked 序列化配置为 JSON(调用方持锁);加密密文(Mcp.TokenEnc)
// 不外发,密钥不出后端。
func (c *ConfigService) configJSONLocked() string {
	out := c.config
	out.Mcp.TokenEnc = ""
	data, _ := json.Marshal(out)
	return string(data)
}

// GetConfig 返回当前完整配置的 JSON 字符串。
// 加密密文(Mcp.TokenEnc)不外发: 密钥不出后端。
func (c *ConfigService) GetConfig() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.configJSONLocked()
}

// SetPanelOpacity 设置面板不透明度（百分比 30-100）并持久化，返回最新配置 JSON。
func (c *ConfigService) SetPanelOpacity(opacity int) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if opacity < 30 || opacity > 100 {
		opacity = 70
	}
	c.config.View.PanelOpacity = opacity
	c.save()
	return c.configJSONLocked()
}

// SetWallpaper 设置壁纸图片路径并持久化；空路径表示恢复默认背景，返回最新配置 JSON。
func (c *ConfigService) SetWallpaper(path string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.View.Wallpaper = path
	c.save()
	return c.configJSONLocked()
}

// GetWallpaperData 读取壁纸文件并返回 data URL 供前端直接使用；无壁纸或读取失败返回空串。
func (c *ConfigService) GetWallpaperData() string {
	c.mu.Lock()
	path := c.config.View.Wallpaper
	c.mu.Unlock()
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	ext := strings.ToLower(filepath.Ext(path))
	mime := map[string]string{
		".png":  "image/png",
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".gif":  "image/gif",
		".bmp":  "image/bmp",
		".webp": "image/webp",
	}[ext]
	if mime == "" {
		mime = "application/octet-stream"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// SetShowSession 设置会话管理器可见性并持久化。
func (c *ConfigService) SetShowSession(show bool) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.View.ShowSession = show
	c.save()
	return c.configJSONLocked()
}

// SetShowToolbar 设置工具栏可见性并持久化。
func (c *ConfigService) SetShowToolbar(show bool) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.View.ShowToolbar = show
	c.save()
	return c.configJSONLocked()
}

// SetShowAutoLog 设置自动日志面板可见性并持久化。
func (c *ConfigService) SetShowAutoLog(show bool) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.View.ShowAutoLog = show
	c.save()
	return c.configJSONLocked()
}

// SetShowSerial 设置串口管理器可见性并持久化。
func (c *ConfigService) SetShowSerial(show bool) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.View.ShowSerial = show
	c.save()
	return c.configJSONLocked()
}

// SetShowAssistant 设置智能助手总开关(视图菜单)并持久化。
// 关闭时前端隐藏顶栏 MCP 按钮/资源管理器收纳按钮/AI 面板按钮。
func (c *ConfigService) SetShowAssistant(show bool) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.View.ShowAssistant = show
	c.save()
	return c.configJSONLocked()
}

// SetShowHelp 设置帮助按钮与帮助弹窗可见性并持久化。
func (c *ConfigService) SetShowHelp(show bool) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.View.ShowHelp = show
	c.save()
	return c.configJSONLocked()
}

// SetCustomTitlebar 设置自绘标题栏开关并持久化;即时生效由前端调用 Window.SetFrameless。
func (c *ConfigService) SetCustomTitlebar(enabled bool) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.View.CustomTitlebar = enabled
	c.save()
	return c.configJSONLocked()
}

// CustomTitlebarEnabled 返回自绘标题栏开关(main.go 创建窗口时读取初始值)。
func (c *ConfigService) CustomTitlebarEnabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.config.View.CustomTitlebar
}

// SetShowSftp 设置 SFTP 面板侧边栏可见性并持久化。
func (c *ConfigService) SetShowSftp(show bool) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.View.ShowSftp = show
	c.save()
	return c.configJSONLocked()
}

// SetShowFilemanager 设置文件管理器侧边栏可见性并持久化。
func (c *ConfigService) SetShowFilemanager(show bool) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.View.ShowFilemanager = show
	c.save()
	return c.configJSONLocked()
}

// SetSidebarOrder 设置侧边栏菜单项排序（逗号分隔的 key 列表）并持久化。
func (c *ConfigService) SetSidebarOrder(order string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.View.SidebarOrder = order
	c.save()
	return c.configJSONLocked()
}

// SetSectionsState 更新资源管理器各分组的展开/折叠状态并持久化。
func (c *ConfigService) SetSectionsState(jsonStr string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var sections SectionsConfig
	if err := json.Unmarshal([]byte(jsonStr), &sections); err != nil {
		return `{"error":"invalid json"}`
	}
	c.config.Sections = sections
	c.save()
	return c.configJSONLocked()
}

// SetCloseConfirm 设置关闭标签页时是否弹出确认对话框。
func (c *ConfigService) SetCloseConfirm(show bool) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.View.CloseConfirm = show
	c.save()
	return c.configJSONLocked()
}

// SetFileEditingAutoSave 设置文件编辑器的自动保存开关(即时生效)。
func (c *ConfigService) SetFileEditingAutoSave(autoSave bool) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.FileEditing.AutoSave = autoSave
	c.save()
	return c.configJSONLocked()
}

// SetTabOrientation 设置标签页方向（horizontal/vertical）并持久化。
func (c *ConfigService) SetTabOrientation(orientation string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.View.TabOrientation = orientation
	c.save()
	return c.configJSONLocked()
}

// SetVerticalTabWidth 设置纵向标签页宽度并持久化。
func (c *ConfigService) SetVerticalTabWidth(width int) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.View.VerticalTabWidth = width
	c.save()
	return c.configJSONLocked()
}

// GetTheme 返回当前主题模式（dark / light / auto）。
func (c *ConfigService) GetTheme() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.config.View.Theme
}

// SetTheme 设置主题模式并持久化（dark / light / auto）。
func (c *ConfigService) SetTheme(theme string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.View.Theme = theme
	c.save()
	return c.configJSONLocked()
}

// GetThemeAccent 返回自定义强调色（#RRGGBB；空 = 默认蓝）。
func (c *ConfigService) GetThemeAccent() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.config.View.AccentColor
}

// SetThemeAccent 设置自定义强调色并持久化（#RRGGBB；非法值忽略）。
func (c *ConfigService) SetThemeAccent(color string) string {
	if len(color) == 7 && color[0] == '#' {
		if _, err := fmt.Sscanf(color[1:], "%06x", new(uint32)); err == nil {
			c.mu.Lock()
			c.config.View.AccentColor = strings.ToLower(color)
			c.save()
			out := c.configJSONLocked()
			c.mu.Unlock()
			return out
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.configJSONLocked()
}

// GetLanguage 返回当前语言（如 zh-CN / en-US）。
func (c *ConfigService) GetLanguage() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.config.Language
}

// SetLanguage 设置语言并持久化（如 zh-CN / en-US）。
func (c *ConfigService) SetLanguage(lang string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.Language = lang
	c.save()
	return c.configJSONLocked()
}

// PluginEnabled 查询插件是否启用(缺项=启用;仅显式记录的 false 视为禁用)。
func (c *ConfigService) PluginEnabled(pluginID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.config.Plugins.Enabled[pluginID]; ok {
		return v
	}
	return true
}

// SetPluginEnabled 设置插件启用状态并持久化。
func (c *ConfigService) SetPluginEnabled(pluginID string, enabled bool) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.config.Plugins.Enabled == nil {
		c.config.Plugins.Enabled = map[string]bool{}
	}
	if enabled {
		// 启用即移除显式禁用项,保持落盘最小化
		delete(c.config.Plugins.Enabled, pluginID)
	} else {
		c.config.Plugins.Enabled[pluginID] = false
	}
	c.save()
	return c.configJSONLocked()
}

// BundledUninstalled 查询捆绑插件是否已被用户卸载。
func (c *ConfigService) BundledUninstalled(pluginID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range c.config.Plugins.UninstalledBundled {
		if id == pluginID {
			return true
		}
	}
	return false
}

// SetBundledUninstalled 记录/撤销捆绑插件卸载状态并持久化(调用方负责落盘插件目录)。
func (c *ConfigService) SetBundledUninstalled(pluginID string, uninstalled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	list := c.config.Plugins.UninstalledBundled
	out := list[:0]
	if uninstalled {
		for _, id := range list {
			if id != pluginID {
				out = append(out, id)
			}
		}
		c.config.Plugins.UninstalledBundled = append(out, pluginID)
	} else {
		for _, id := range list {
			if id != pluginID {
				out = append(out, id)
			}
		}
		c.config.Plugins.UninstalledBundled = out
	}
	c.save()
}

// SetSerialConfig 更新串口配置并持久化。
func (c *ConfigService) SetSerialConfig(jsonStr string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var sc SerialConfig
	if err := json.Unmarshal([]byte(jsonStr), &sc); err != nil {
		return `{"error":"invalid json"}`
	}
	// 边界校验,避免写入损坏/越界配置
	if sc.BaudRate < 110 || sc.BaudRate > 921600 {
		sc.BaudRate = 115200
	}
	if sc.DataBits < 5 || sc.DataBits > 8 {
		sc.DataBits = 8
	}
	if sc.StopBits != "1" && sc.StopBits != "1.5" && sc.StopBits != "2" {
		sc.StopBits = "1"
	}
	if sc.Parity != "none" && sc.Parity != "odd" && sc.Parity != "even" && sc.Parity != "mark" && sc.Parity != "space" {
		sc.Parity = "none"
	}
	c.config.Serial = sc
	c.save()
	data, _ := json.Marshal(c.config.Serial)
	return string(data)
}

// terminalPayload 终端设置表单提交的数据(含工具栏开关)。
type terminalPayload struct {
	ShowToolbar  bool    `json:"showToolbar"`
	Personalize  bool    `json:"personalize"`
	FontColor    string  `json:"fontColor"`
	BgColor      string  `json:"bgColor"`
	BgOpacity    int     `json:"bgOpacity"`
	BgImage      string  `json:"bgImage"`
	FontFamily   string  `json:"fontFamily"`
	FontSize     int     `json:"fontSize"`
	LineHeight   float64 `json:"lineHeight"`
	CopyOnSelect bool    `json:"copyOnSelect"`
	CursorBlink  bool    `json:"cursorBlink"`
	CursorStyle  string  `json:"cursorStyle"`
	Scrollback   int     `json:"scrollback"`
}

// isHexColor 校验 #RRGGBB 格式颜色。
func isHexColor(s string) bool {
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	for _, r := range s[1:] {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

// SetTerminalConfig 批量保存终端设置(工具栏开关一并写入)并持久化,返回最新配置 JSON。
// 越界/非法值回退到默认值,避免写入损坏配置。
func (c *ConfigService) SetTerminalConfig(jsonStr string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var p terminalPayload
	if err := json.Unmarshal([]byte(jsonStr), &p); err != nil {
		return `{"error":"invalid json"}`
	}
	if !isHexColor(p.FontColor) {
		p.FontColor = "#FFFFFF"
	}
	if !isHexColor(p.BgColor) {
		p.BgColor = "#0C0C0C"
	}
	if p.BgOpacity < 0 || p.BgOpacity > 100 {
		p.BgOpacity = 100
	}
	if p.BgImage != "" {
		if info, err := os.Stat(p.BgImage); err != nil || info.IsDir() {
			p.BgImage = ""
		}
	}
	if p.FontFamily == "" {
		p.FontFamily = "Cascadia Code"
	}
	if p.FontSize < 10 || p.FontSize > 32 {
		p.FontSize = 16
	}
	if p.LineHeight < 0.8 || p.LineHeight > 2 {
		p.LineHeight = 1
	}
	if p.CursorStyle != "bar" && p.CursorStyle != "block" && p.CursorStyle != "underline" {
		p.CursorStyle = "bar"
	}
	if p.Scrollback < 100 || p.Scrollback > 100000 {
		p.Scrollback = 1000
	}
	c.config.View.ShowToolbar = p.ShowToolbar
	c.config.Terminal = TerminalConfig{
		Personalize:  p.Personalize,
		FontColor:    p.FontColor,
		BgColor:      p.BgColor,
		BgOpacity:    p.BgOpacity,
		BgImage:      p.BgImage,
		FontFamily:   p.FontFamily,
		FontSize:     p.FontSize,
		LineHeight:   p.LineHeight,
		CopyOnSelect: p.CopyOnSelect,
		CursorBlink:  p.CursorBlink,
		CursorStyle:  p.CursorStyle,
		Scrollback:   p.Scrollback,
	}
	c.save()
	return c.configJSONLocked()
}

// ==================== MCP 配置 ====================

// McpEnabled 返回 MCP 服务是否随应用启动。
func (c *ConfigService) McpEnabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.config.Mcp.Enabled
}

// McpPort 返回 MCP 监听端口(默认 8940)。
func (c *ConfigService) McpPort() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.config.Mcp.Port <= 0 {
		return 8940
	}
	return c.config.Mcp.Port
}

// McpTokenEnc 返回加密后的访问令牌密文。
func (c *ConfigService) McpTokenEnc() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.config.Mcp.TokenEnc
}

// SetMcpEnabled 设置 MCP 启用状态并持久化。
func (c *ConfigService) SetMcpEnabled(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.Mcp.Enabled = enabled
	c.save()
}

// SetMcpPort 设置 MCP 监听端口并持久化。
func (c *ConfigService) SetMcpPort(port int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if port <= 0 || port > 65535 {
		port = 8940
	}
	c.config.Mcp.Port = port
	c.save()
}

// SetMcpTokenEnc 保存加密后的访问令牌并持久化。
func (c *ConfigService) SetMcpTokenEnc(enc string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.Mcp.TokenEnc = enc
	return c.save()
}

// SetMcpBallPos 持久化悬浮球位置。
func (c *ConfigService) SetMcpBallPos(x int, y int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.Mcp.BallX = x
	c.config.Mcp.BallY = y
	c.save()
}

// McpBallPos 返回悬浮球位置(-1,-1 表示未初始化)。
func (c *ConfigService) McpBallPos() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.config.Mcp.BallX, c.config.Mcp.BallY
}

// McpOpDelayMs 返回单操作可视时延(毫秒,默认 1000,范围 0-10000)。
func (c *ConfigService) McpOpDelayMs() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	v := c.config.Mcp.OpDelayMs
	if v <= 0 {
		return 0
	}
	if v > 10000 {
		return 10000
	}
	return v
}

// McpBatchIntervalMs 返回批量执行命令间隔(毫秒,默认 300,范围 50-10000)。
func (c *ConfigService) McpBatchIntervalMs() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	v := c.config.Mcp.BatchIntervalMs
	if v < 50 {
		return 300
	}
	if v > 10000 {
		return 10000
	}
	return v
}

// McpAuditRetentionDays 返回审计日志磁盘保留天数(默认 30,范围 1-365)。
func (c *ConfigService) McpAuditRetentionDays() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	v := c.config.Mcp.AuditRetentionDays
	if v < 1 {
		return 30
	}
	if v > 365 {
		return 365
	}
	return v
}

// McpTerminalReadMax 返回 terminal_read 单次返回上限(字节,默认 32KB)。
func (c *ConfigService) McpTerminalReadMax() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	v := c.config.Mcp.TerminalReadMaxBytes
	if v < 1024 {
		return 32768
	}
	if v > 262144 {
		return 262144
	}
	return v
}

// McpDangerousPatterns 返回绝对危险指令字典 JSON(空配置时返回内置默认字典)。
func (c *ConfigService) McpDangerousPatterns() string {
	c.mu.Lock()
	patterns := make([]string, len(c.config.Mcp.DangerousPatterns))
	copy(patterns, c.config.Mcp.DangerousPatterns)
	c.mu.Unlock()
	if len(patterns) == 0 {
		patterns = DefaultDangerousPatterns()
	}
	data, _ := json.Marshal(patterns)
	return string(data)
}

// SetMcpExecTuning 持久化执行参数(时延/批量间隔/审计保留天数)。
func (c *ConfigService) SetMcpExecTuning(opDelayMs, batchIntervalMs int, auditRetentionDays, terminalReadMax int) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if opDelayMs < 0 {
		opDelayMs = 0
	}
	if opDelayMs > 10000 {
		opDelayMs = 10000
	}
	if batchIntervalMs < 50 {
		batchIntervalMs = 50
	}
	if batchIntervalMs > 10000 {
		batchIntervalMs = 10000
	}
	if auditRetentionDays < 1 {
		auditRetentionDays = 30
	}
	if auditRetentionDays > 365 {
		auditRetentionDays = 365
	}
	if terminalReadMax < 1024 {
		terminalReadMax = 1024
	}
	if terminalReadMax > 262144 {
		terminalReadMax = 262144
	}
	c.config.Mcp.OpDelayMs = opDelayMs
	c.config.Mcp.BatchIntervalMs = batchIntervalMs
	c.config.Mcp.AuditRetentionDays = auditRetentionDays
	c.config.Mcp.TerminalReadMaxBytes = terminalReadMax
	c.save()
	mcp := c.config.Mcp
	mcp.TokenEnc = "" // 密文不出后端
	data, _ := json.Marshal(mcp)
	return string(data)
}

// SetMcpDangerousPatterns 持久化绝对危险指令字典(逐条校验正则)。
// 空列表 = 恢复内置默认字典。返回新配置 JSON 或 {"error":...}。
func (c *ConfigService) SetMcpDangerousPatterns(jsonStr string) string {
	var patterns []string
	if err := json.Unmarshal([]byte(jsonStr), &patterns); err != nil {
		return `{"error":"invalid json"}`
	}
	if len(patterns) > 100 { // 有界: 字典上限 100 条
		patterns = patterns[:100]
	}
	for _, p := range patterns {
		if _, err := regexp.Compile(p); err != nil {
			return marshalJSON(map[string]string{"error": "invalid regex: " + p})
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.Mcp.DangerousPatterns = patterns
	c.save()
	data, _ := json.Marshal(patterns)
	return string(data)
}
