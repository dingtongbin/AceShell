package services

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/pelletier/go-toml/v2"
)

type AppConfig struct {
	View         ViewConfig         `toml:"view" json:"view"`
	Sections     SectionsConfig     `toml:"sections" json:"sections"`
	Serial       SerialConfig       `toml:"serial" json:"serial"`
	Terminal     TerminalConfig     `toml:"terminal" json:"terminal"`
	FileEditing  FileEditingConfig  `toml:"fileEditing" json:"fileEditing"`
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
	PanelOpacity     int    `toml:"panelOpacity" json:"panelOpacity"`
	Wallpaper        string `toml:"wallpaper" json:"wallpaper"`
	ShowHelp         bool   `toml:"showHelp" json:"showHelp"`
	ShowGithub       bool   `toml:"showGithub" json:"showGithub"`
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
	mu     sync.Mutex
	config AppConfig
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
			ShowGithub:       true,
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
	}
	c.load()
}

func (c *ConfigService) load() {
	os.MkdirAll(filepath.Dir(configFile), 0755)
	data, err := os.ReadFile(configFile)
	if err != nil {
		return
	}
	if err := toml.Unmarshal(data, &c.config); err != nil {
		return
	}
}

func (c *ConfigService) save() error {
	data, err := toml.Marshal(c.config)
	if err != nil {
		return err
	}
	return os.WriteFile(configFile, data, 0644)
}

// GetConfig 返回当前完整配置的 JSON 字符串。
func (c *ConfigService) GetConfig() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, _ := json.Marshal(c.config)
	return string(data)
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
	data, _ := json.Marshal(c.config)
	return string(data)
}

// SetWallpaper 设置壁纸图片路径并持久化；空路径表示恢复默认背景，返回最新配置 JSON。
func (c *ConfigService) SetWallpaper(path string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.View.Wallpaper = path
	c.save()
	data, _ := json.Marshal(c.config)
	return string(data)
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
	data, _ := json.Marshal(c.config)
	return string(data)
}

// SetShowToolbar 设置工具栏可见性并持久化。
func (c *ConfigService) SetShowToolbar(show bool) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.View.ShowToolbar = show
	c.save()
	data, _ := json.Marshal(c.config)
	return string(data)
}

// SetShowAutoLog 设置自动日志面板可见性并持久化。
func (c *ConfigService) SetShowAutoLog(show bool) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.View.ShowAutoLog = show
	c.save()
	data, _ := json.Marshal(c.config)
	return string(data)
}

// SetShowSerial 设置串口管理器可见性并持久化。
func (c *ConfigService) SetShowSerial(show bool) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.View.ShowSerial = show
	c.save()
	data, _ := json.Marshal(c.config)
	return string(data)
}

// SetShowHelp 设置帮助按钮与帮助弹窗可见性并持久化。
func (c *ConfigService) SetShowHelp(show bool) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.View.ShowHelp = show
	c.save()
	data, _ := json.Marshal(c.config)
	return string(data)
}

// SetShowSftp 设置 SFTP 面板侧边栏可见性并持久化。
func (c *ConfigService) SetShowSftp(show bool) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.View.ShowSftp = show
	c.save()
	data, _ := json.Marshal(c.config)
	return string(data)
}

// SetShowFilemanager 设置文件管理器侧边栏可见性并持久化。
func (c *ConfigService) SetShowFilemanager(show bool) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.View.ShowFilemanager = show
	c.save()
	data, _ := json.Marshal(c.config)
	return string(data)
}

// SetSidebarOrder 设置侧边栏菜单项排序（逗号分隔的 key 列表）并持久化。
func (c *ConfigService) SetSidebarOrder(order string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.View.SidebarOrder = order
	c.save()
	data, _ := json.Marshal(c.config)
	return string(data)
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
	data, _ := json.Marshal(c.config)
	return string(data)
}

// SetCloseConfirm 设置关闭标签页时是否弹出确认对话框。
func (c *ConfigService) SetCloseConfirm(show bool) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.View.CloseConfirm = show
	c.save()
	data, _ := json.Marshal(c.config)
	return string(data)
}

// SetFileEditingAutoSave 设置文件编辑器的自动保存开关(即时生效)。
func (c *ConfigService) SetFileEditingAutoSave(autoSave bool) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.FileEditing.AutoSave = autoSave
	c.save()
	data, _ := json.Marshal(c.config)
	return string(data)
}

// SetShowGithub 设置左侧工具栏 GitHub 按钮显隐(即时生效)。
func (c *ConfigService) SetShowGithub(show bool) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.View.ShowGithub = show
	c.save()
	data, _ := json.Marshal(c.config)
	return string(data)
}

// SetTabOrientation 设置标签页方向（horizontal/vertical）并持久化。
func (c *ConfigService) SetTabOrientation(orientation string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.View.TabOrientation = orientation
	c.save()
	data, _ := json.Marshal(c.config)
	return string(data)
}

// SetVerticalTabWidth 设置纵向标签页宽度并持久化。
func (c *ConfigService) SetVerticalTabWidth(width int) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.View.VerticalTabWidth = width
	c.save()
	data, _ := json.Marshal(c.config)
	return string(data)
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
	data, _ := json.Marshal(c.config)
	return string(data)
}

// SetSerialConfig 更新串口配置并持久化。
func (c *ConfigService) SetSerialConfig(jsonStr string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var sc SerialConfig
	if err := json.Unmarshal([]byte(jsonStr), &sc); err != nil {
		return `{"error":"invalid json"}`
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
	data, _ := json.Marshal(c.config)
	return string(data)
}
