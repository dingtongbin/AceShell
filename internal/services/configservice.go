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
	Agent        AgentConfig        `toml:"agent" json:"agent"`
	Language     string             `toml:"language" json:"language"`
}

// mainConfigFile 主配置文件(config.toml)落盘结构: 不含 mcp/agent 节(拆分至 mcp.toml/agent.toml)。
// 注意: AppConfig 新增非 mcp/agent 字段时需同步此结构。
type mainConfigFile struct {
	View        ViewConfig        `toml:"view" json:"view"`
	Sections    SectionsConfig    `toml:"sections" json:"sections"`
	Serial      SerialConfig      `toml:"serial" json:"serial"`
	Terminal    TerminalConfig    `toml:"terminal" json:"terminal"`
	FileEditing FileEditingConfig `toml:"fileEditing" json:"fileEditing"`
	Language    string            `toml:"language" json:"language"`
}

// McpConfig MCP 服务配置(令牌密文经 encryptSecret 加密,不含明文)。
type McpConfig struct {
	Enabled  bool   `toml:"enabled" json:"enabled"`
	Port     int    `toml:"port" json:"port"`
	TokenEnc string `toml:"tokenEnc" json:"tokenEnc"`
	Mode     string `toml:"mode" json:"mode"` // manual / auto
	BallX    int    `toml:"ballX" json:"ballX"` // 悬浮球位置(-1 未初始化)
	BallY    int    `toml:"ballY" json:"ballY"`
	// 单操作可视时延(毫秒): 激活目标标签页后等待,让用户看清操作。0-10000。
	OpDelayMs int `toml:"opDelayMs" json:"opDelayMs"`
	// 批量执行命令间隔(毫秒)。
	BatchIntervalMs int `toml:"batchIntervalMs" json:"batchIntervalMs"`
	// 永久授权机制开关(命令+路径双精确匹配的免审批规则)。
	GrantsEnabled bool `toml:"grantsEnabled" json:"grantsEnabled"`
	// 审计日志磁盘保留天数。
	AuditRetentionDays int `toml:"auditRetentionDays" json:"auditRetentionDays"`
	// terminal_read 单次返回上限(字节)。
	TerminalReadMaxBytes int `toml:"terminalReadMaxBytes" json:"terminalReadMaxBytes"`
	// 用户自定义分级规则(正则),优先于内置规则。
	CustomRules []McpCustomRule `toml:"customRules" json:"customRules"`
}

// McpCustomRule 用户自定义 MCP 分级规则。
type McpCustomRule struct {
	Pattern string `toml:"pattern" json:"pattern"` // 正则表达式
	Risk    string `toml:"risk" json:"risk"`       // blocked / confirm / auto
	Note    string `toml:"note" json:"note"`       // 备注说明
}

// AgentProfile AI 服务档案(连接身份: 提供商/端点/密钥/模型)。
// 密钥密文经 encryptSecret 加密,不含明文;档案可添加任意多个。
type AgentProfile struct {
	ID             string   `toml:"id" json:"id"`
	Name           string   `toml:"name" json:"name"`
	Provider       string   `toml:"provider" json:"provider"`                          // 提供商预设名
	BaseURL        string   `toml:"baseURL" json:"baseURL"`                            // OpenAI 兼容端点
	ApiKeyEnc      string   `toml:"apiKeyEnc" json:"apiKeyEnc"`
	Model          string   `toml:"model" json:"model"`
	ApiMode        string   `toml:"apiMode" json:"apiMode"`                            // chat / responses
	ContextWindow  int      `toml:"contextWindow,omitzero" json:"contextWindow,omitzero"` // 模型上下文窗口(token;0=默认128K)
	CustomModels   []string `toml:"customModels,omitempty" json:"customModels,omitempty"` // 自定义模型(接口不可用时手动补充;下拉里排在自动获取之前)
}

// AgentConfig 内嵌智能体配置。
// 行为参数(权限模式/步数/窗口)全局一份,与档案解耦——切模型不改安全策略。
type AgentConfig struct {
	Enabled           bool   `toml:"enabled" json:"enabled"`
	PermMode          string `toml:"permMode" json:"permMode"`                 // plan / manual / auto(全局)
	HistoryWindow     int    `toml:"historyWindow" json:"historyWindow"`       // 前端渲染窗口(分页大小)
	ContextMaxEvents  int    `toml:"contextMaxEvents" json:"contextMaxEvents"` // 上下文截断上限
	WebSearch         bool   `toml:"webSearch" json:"webSearch"`               // 联网搜索工具开关(默认开启,AI 视情况调用)
	ActiveProfileID   string `toml:"activeProfileId" json:"activeProfileId"`   // 当前活动档案
	Profiles          []AgentProfile `toml:"profile" json:"profiles"`          // 多 AI 档案
	// 旧版单份配置(仅作迁移源读取;迁移完成后零值)
	Provider  string `toml:"provider,omitempty" json:"provider,omitempty"`
	BaseURL   string `toml:"baseURL,omitempty" json:"baseURL,omitempty"`
	ApiKeyEnc string `toml:"apiKeyEnc,omitempty" json:"apiKeyEnc,omitempty"`
	Model     string `toml:"model,omitempty" json:"model,omitempty"`
	ApiMode   string `toml:"apiMode,omitempty" json:"apiMode,omitempty"`
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
	// 自绘标题栏(Frameless 窗口 + 顶部菜单栏融入窗口控制)开关。
	CustomTitlebar bool `toml:"customTitlebar" json:"customTitlebar"`
	// AI 聊天面板: 显隐 + 宽度(px)。
	ShowAgentPanel bool `toml:"showAgentPanel" json:"showAgentPanel"`
	AgentPanelWidth int  `toml:"agentPanelWidth" json:"agentPanelWidth"`
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

// SetPanelLayout 更新面板布局(AI 面板显隐/宽度 + 资源管理器宽度)。
// 仅写内存并标记脏; 由后台定时器周期落盘, 窗口关闭时 Flush 落盘, 保护磁盘。
func (c *ConfigService) SetPanelLayout(showAgentPanel bool, agentPanelWidth int, sessionWidth int) {
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
	c.config.View.ShowAgentPanel = showAgentPanel
	c.config.View.AgentPanelWidth = clamp(agentPanelWidth, 240, 720)
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
			// AI 聊天面板默认收纳,宽度默认 300px;资源管理器默认 220px
			ShowAgentPanel:  false,
			AgentPanelWidth: 300,
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
			Enabled: false,
			Port:    8940,
			Mode:    "manual",
			BallX:   -1,
			BallY:   -1,
			OpDelayMs:           1000,
			BatchIntervalMs:     300,
			GrantsEnabled:       true,
			AuditRetentionDays:  30,
			TerminalReadMaxBytes: 32768,
		},
		Agent: AgentConfig{
			Enabled:          true,
			ApiMode:          "chat",
			PermMode:         "manual",
			HistoryWindow:    200,
			ContextMaxEvents: 400,
			WebSearch:        true, // 联网搜索默认开启
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
		// 旧版主文件可能内嵌 mcp/agent 节,先读入;随后被独立文件覆盖并触发迁移
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
	// 独立文件优先(权威源): agent.toml
	if data, err := os.ReadFile(AgentConfigFile()); err == nil {
		var f struct {
			Agent AgentConfig `toml:"agent"`
		}
		if uerr := toml.Unmarshal(data, &f); uerr == nil {
			// 旧版配置文件不含 webSearch 键: 反序列化零值会整体覆盖默认值,导致联网搜索被静默关闭——回退到默认值
			if !strings.Contains(string(data), "webSearch") {
				f.Agent.WebSearch = c.config.Agent.WebSearch
			}
			c.config.Agent = f.Agent
		} else {
			// 解析失败留痕(静默吞掉会导致档案/密钥"凭空消失")
			logConfigLoadError("agent.toml", uerr)
		}
	}
	c.ensureAgentMigrated()
	// 旧版主文件内嵌 mcp/agent 节 → 拆分迁移一次(save 重写主文件剔除旧节并落盘独立文件)
	if tomlHasSection(rawMain, "mcp") || tomlHasSection(rawMain, "agent") {
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
	// 主配置: 剔除 mcp/agent(拆分至独立文件,禁止写入 config.toml)
	main := mainConfigFile{
		View:        c.config.View,
		Sections:    c.config.Sections,
		Serial:      c.config.Serial,
		Terminal:    c.config.Terminal,
		FileEditing: c.config.FileEditing,
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
	// agent.toml(写盘前密文保护: 内存密文为空但磁盘已有密文 → 合并保留,
	// 防止任何调用路径因内存状态丢失而清空已存密钥)
	c.protectAgentKeysLocked()
	agentData, err := toml.Marshal(struct {
		Agent AgentConfig `toml:"agent"`
	}{c.config.Agent})
	if err != nil {
		return fail("marshal-agent", err)
	}
	if err := atomicWriteFile(AgentConfigFile(), agentData, 0600); err != nil {
		return fail("write-agent", err)
	}
	return nil
}

// mergeDiskKeysLocked 将磁盘 agent.toml 中的密文合并进 keys(调用方持锁)。
// 仅在内存值为空时取磁盘值 —— 磁盘是权威源,内存丢失不得清空已存密钥。
func (c *ConfigService) mergeDiskKeysLocked(keys map[string]string) {
	data, err := os.ReadFile(AgentConfigFile())
	if err != nil {
		return
	}
	var f struct {
		Agent AgentConfig `toml:"agent"`
	}
	if toml.Unmarshal(data, &f) != nil {
		return
	}
	for _, p := range f.Agent.Profiles {
		if p.ID != "" && p.ApiKeyEnc != "" && keys[p.ID] == "" {
			keys[p.ID] = p.ApiKeyEnc
		}
	}
}

// protectAgentKeysLocked 写盘前密文保护(调用方持锁):
// 逐档案检查,内存密文为空但磁盘已有 → 从磁盘取回,留审计痕迹。
func (c *ConfigService) protectAgentKeysLocked() {
	need := false
	for i := range c.config.Agent.Profiles {
		if c.config.Agent.Profiles[i].ApiKeyEnc == "" {
			need = true
			break
		}
	}
	if !need {
		return
	}
	disk := make(map[string]string, len(c.config.Agent.Profiles))
	for _, p := range c.config.Agent.Profiles {
		if p.ApiKeyEnc != "" {
			disk[p.ID] = p.ApiKeyEnc
		}
	}
	c.mergeDiskKeysLocked(disk)
	saved := make([]string, 0, 2)
	for i := range c.config.Agent.Profiles {
		p := &c.config.Agent.Profiles[i]
		if p.ApiKeyEnc == "" && disk[p.ID] != "" {
			p.ApiKeyEnc = disk[p.ID]
			if len(saved) < 2 {
				saved = append(saved, p.ID)
			}
		}
	}
	if len(saved) > 0 {
		logConfigLoadError("agent.toml", fmt.Errorf("写盘密文保护: 内存密文丢失已从磁盘保留(档案=%v)", strings.Join(saved, ",")))
	}
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

// configJSONLocked 序列化配置为 JSON(调用方持锁);加密密文(Mcp.TokenEnc /
// Agent.Profiles[].ApiKeyEnc)不外发,前端仅凭 hasKey 判断是否已存密钥。
func (c *ConfigService) configJSONLocked() string {
	out := c.config
	out.Mcp.TokenEnc = ""
	for i := range out.Agent.Profiles {
		out.Agent.Profiles[i].ApiKeyEnc = ""
	}
	data, _ := json.Marshal(out)
	return string(data)
}

// GetConfig 返回当前完整配置的 JSON 字符串。
// 加密密文(Mcp.TokenEnc/Agent.Profiles[].ApiKeyEnc)不外发: 密钥不出后端,
// 前端仅凭 hasKey 标记判断是否已存密钥(与 AgentProfilesGet 口径一致)。
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

// McpMode 返回审批模式(manual / auto,默认 manual)。
func (c *ConfigService) McpMode() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.config.Mcp.Mode != "manual" && c.config.Mcp.Mode != "auto" {
		return "manual"
	}
	return c.config.Mcp.Mode
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

// SetMcpMode 设置审批模式并持久化。
func (c *ConfigService) SetMcpMode(mode string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if mode != "manual" && mode != "auto" {
		mode = "manual"
	}
	c.config.Mcp.Mode = mode
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

// McpGrantsEnabled 返回永久授权机制开关。
func (c *ConfigService) McpGrantsEnabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.config.Mcp.GrantsEnabled
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

// McpCustomRules 返回用户自定义分级规则副本。
func (c *ConfigService) McpCustomRules() []McpCustomRule {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]McpCustomRule, len(c.config.Mcp.CustomRules))
	copy(out, c.config.Mcp.CustomRules)
	return out
}

// SetMcpExecTuning 持久化执行参数(时延/批量间隔/授权开关/审计保留天数)。
func (c *ConfigService) SetMcpExecTuning(opDelayMs, batchIntervalMs int, grantsEnabled bool, auditRetentionDays, terminalReadMax int) string {
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
	c.config.Mcp.GrantsEnabled = grantsEnabled
	c.config.Mcp.AuditRetentionDays = auditRetentionDays
	c.config.Mcp.TerminalReadMaxBytes = terminalReadMax
	c.save()
	mcp := c.config.Mcp
	mcp.TokenEnc = "" // 密文不出后端
	data, _ := json.Marshal(mcp)
	return string(data)
}

// SetMcpCustomRules 持久化用户自定义分级规则(逐条校验正则与风险值)。
func (c *ConfigService) SetMcpCustomRules(jsonStr string) string {
	var rules []McpCustomRule
	if err := json.Unmarshal([]byte(jsonStr), &rules); err != nil {
		return `{"error":"invalid json"}`
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(rules) > 100 { // 有界: 自定义规则上限 100 条
		rules = rules[:100]
	}
	for i := range rules {
		if _, err := regexp.Compile(rules[i].Pattern); err != nil {
			return marshalJSON(map[string]string{"error": "invalid regex: " + rules[i].Pattern})
		}
		switch rules[i].Risk {
		case RiskBlocked, RiskConfirm, RiskAuto:
		default:
			return marshalJSON(map[string]string{"error": "invalid risk: " + rules[i].Risk})
		}
		rules[i].Note = truncateUtf8(rules[i].Note, 100)
	}
	c.config.Mcp.CustomRules = rules
	c.save()
	data, _ := json.Marshal(rules)
	return string(data)
}

// ==================== 智能体配置 ====================

// ensureAgentMigrated 旧版单份配置 → 多档案结构(加载后调用一次)。
func (c *ConfigService) ensureAgentMigrated() {
	a := &c.config.Agent
	if len(a.Profiles) == 0 {
		// 旧版有配置 → 迁移为首个档案
		if a.BaseURL != "" {
			apiMode := a.ApiMode
			if apiMode == "" {
				apiMode = "chat"
			}
			a.Profiles = []AgentProfile{{
				ID:        "p-default",
				Name:      "默认",
				Provider:  a.Provider,
				BaseURL:   a.BaseURL,
				ApiKeyEnc: a.ApiKeyEnc,
				Model:     a.Model,
				ApiMode:   apiMode,
			}}
			a.ActiveProfileID = "p-default"
		}
	} else if a.ActiveProfileID == "" {
		a.ActiveProfileID = a.Profiles[0].ID
	}
	if len(a.Profiles) > 0 {
		// 旧字段零值(不再落盘)
		a.Provider, a.BaseURL, a.ApiKeyEnc, a.Model, a.ApiMode = "", "", "", "", ""
	}
}

// ActiveAgentProfile 返回活动档案副本;无档案返回空 ID。
func (c *ConfigService) ActiveAgentProfile() AgentProfile {
	c.mu.Lock()
	defer c.mu.Unlock()
	a := c.config.Agent
	for _, p := range a.Profiles {
		if p.ID == a.ActiveProfileID {
			return p
		}
	}
	if len(a.Profiles) > 0 {
		return a.Profiles[0]
	}
	return AgentProfile{}
}

// AgentCfg 返回智能体配置副本(密文不出后端,档案带 hasKey 标记)。
func (c *ConfigService) AgentCfg() AgentConfig {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.config.Agent
	for i := range out.Profiles {
		out.Profiles[i].ApiKeyEnc = ""
	}
	return out
}

// agentProfileHasKey 判断档案是否已存密钥(内部)。
func (c *ConfigService) agentProfileHasKey(id string) bool {
	for _, p := range c.config.Agent.Profiles {
		if p.ID == id {
			return p.ApiKeyEnc != ""
		}
	}
	return false
}

// AgentProfilesSet 全量保存档案列表与活动档案。
// 输入 JSON: {"activeProfileId":"...","profiles":[{"id","name","provider","baseURL","model","apiMode","apiKey?"}]}
// apiKey 非空则重新加密;为空则保留该档案原密文。
func (c *ConfigService) AgentProfilesSet(jsonStr string) string {
	var in struct {
		ActiveProfileID string `json:"activeProfileId"`
		Profiles        []struct {
			ID            string   `json:"id"`
			Name          string   `json:"name"`
			Provider      string   `json:"provider"`
			BaseURL       string   `json:"baseURL"`
			Model         string   `json:"model"`
			ApiMode       string   `json:"apiMode"`
			ApiKey        string   `json:"apiKey"`
			ContextWindow int      `json:"contextWindow"`
			CustomModels  []string `json:"customModels"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &in); err != nil {
		return `{"error":"invalid json"}`
	}
	if len(in.Profiles) > 20 { // 有界
		return `{"error":"too many profiles"}`
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	old := c.config.Agent
	oldKeys := make(map[string]string, len(old.Profiles))
	for _, p := range old.Profiles {
		oldKeys[p.ID] = p.ApiKeyEnc
	}
	// 权威源兜底: 内存密文丢失(未知覆盖/状态异常)时从磁盘取回,
	// 保证"留空=保留原密文"语义永远成立,绝不把空密文写盘清掉已有密钥。
	c.mergeDiskKeysLocked(oldKeys)
	var out []AgentProfile
	auditParts := make([]string, 0, len(in.Profiles))
	for _, p := range in.Profiles {
		p.ID = strings.TrimSpace(p.ID)
		p.Name = truncateUtf8(strings.TrimSpace(p.Name), 40)
		p.BaseURL = strings.TrimSpace(p.BaseURL)
		p.Model = strings.TrimSpace(p.Model)
		if p.ApiMode != "responses" {
			p.ApiMode = "chat"
		}
		enc := ""
		keySrc := "empty"
		if p.ApiKey != "" {
			var err error
			enc, err = encryptSecret(p.ApiKey)
			if err != nil {
				return `{"error":"加密 API Key 失败"}`
			}
			keySrc = "new"
		} else if old, ok := oldKeys[p.ID]; ok && old != "" {
			enc = old // 保留原密文
			keySrc = "keep"
		}
		// 自定义模型: 去重去空 + 有界(最多 50 个,防止无限增长)
		custom := make([]string, 0, len(p.CustomModels))
		seen := make(map[string]bool, len(p.CustomModels))
		for _, m := range p.CustomModels {
			m = strings.TrimSpace(m)
			if m != "" && !seen[m] {
				seen[m] = true
				custom = append(custom, m)
			}
			if len(custom) >= 50 {
				break
			}
		}
		// 上下文窗口: 0=默认 128K;有效值钳制 [4096, 10,000,000]
		ctxWin := p.ContextWindow
		if ctxWin != 0 {
			if ctxWin < 4096 {
				ctxWin = 4096
			}
			if ctxWin > 10_000_000 {
				ctxWin = 10_000_000
			}
		}
		auditParts = append(auditParts, fmt.Sprintf("id=%s key=%s encLen=%d", p.ID, keySrc, len(enc)))
		out = append(out, AgentProfile{
			ID: p.ID, Name: p.Name, Provider: p.Provider, BaseURL: p.BaseURL,
			ApiKeyEnc: enc, Model: p.Model, ApiMode: p.ApiMode,
			ContextWindow: ctxWin, CustomModels: custom,
		})
	}
	// 审计留痕: 每次全量保存记录档案与密钥来源,便于追踪密文丢失
	logConfigLoadError("profiles-set", fmt.Errorf("active=%s %s", strings.TrimSpace(in.ActiveProfileID), strings.Join(auditParts, " | ")))
	// 活动档案必须存在
	active := strings.TrimSpace(in.ActiveProfileID)
	found := false
	for _, p := range out {
		if p.ID == active {
			found = true
			break
		}
	}
	if !found && len(out) > 0 {
		active = out[0].ID
	}
	c.config.Agent.Profiles = out
	c.config.Agent.ActiveProfileID = active
	c.save()
	return c.agentProfilesViewLocked()
}

// AgentSetActiveProfile 切换活动档案(下一轮对话生效)。
func (c *ConfigService) AgentSetActiveProfile(id string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, p := range c.config.Agent.Profiles {
		if p.ID == id {
			c.config.Agent.ActiveProfileID = id
			c.save()
			return `{"ok":true}`
		}
	}
	return `{"error":"profile not found"}`
}

// agentProfilesViewLocked 档案视图(不含密文;调用方持锁)。
func (c *ConfigService) agentProfilesViewLocked() string {
	type profileView struct {
		AgentProfile
		HasKey bool `json:"hasKey"`
	}
	out := struct {
		ActiveProfileID string        `json:"activeProfileId"`
		Profiles        []profileView `json:"profiles"`
	}{ActiveProfileID: c.config.Agent.ActiveProfileID}
	for _, p := range c.config.Agent.Profiles {
		pv := profileView{AgentProfile: p, HasKey: p.ApiKeyEnc != ""}
		pv.ApiKeyEnc = ""
		out.Profiles = append(out.Profiles, pv)
	}
	data, _ := json.Marshal(out)
	return string(data)
}

// AgentProfilesGet 返回档案视图 JSON(不含密文)。
func (c *ConfigService) AgentProfilesGet() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.agentProfilesViewLocked()
}

// SetAgentBehavior 保存全局行为参数(权限模式/窗口)。
func (c *ConfigService) SetAgentBehavior(permMode string, historyWindow int, contextMaxEvents int) string {
	switch permMode {
	case "plan", "manual", "auto":
	default:
		permMode = "manual"
	}
	if historyWindow < 20 {
		historyWindow = 200
	}
	if contextMaxEvents < 20 {
		contextMaxEvents = 400
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.Agent.PermMode = permMode
	c.config.Agent.HistoryWindow = historyWindow
	c.config.Agent.ContextMaxEvents = contextMaxEvents
	c.save()
	return `{"ok":true}`
}

// SetAgentEnabled 启用/停用智能体。
func (c *ConfigService) SetAgentEnabled(enabled bool) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.Agent.Enabled = enabled
	c.save()
	return `{"ok":true}`
}

// AgentApiKeyPlain 解密返回活动档案 API Key 明文(仅智能体服务内部使用)。
func (c *ConfigService) AgentApiKeyPlain() string {
	plain, _, _ := c.AgentApiKeyState()
	return plain
}

// AgentProfileByIDPlain 返回指定档案的 baseURL 与 API Key 明文(仅智能体服务
// 内部使用;密钥不出后端)。档案不存在返回 found=false;未存密钥/解密失败时
// apiKey 为空但 found=true(由调用方让端点自然报鉴权错)。
func (c *ConfigService) AgentProfileByIDPlain(id string) (baseURL string, apiKey string, found bool) {
	c.mu.Lock()
	var enc string
	for _, p := range c.config.Agent.Profiles {
		if p.ID == id {
			baseURL, enc = p.BaseURL, p.ApiKeyEnc
			break
		}
	}
	c.mu.Unlock()
	if baseURL == "" && enc == "" {
		return "", "", false
	}
	if enc == "" {
		return baseURL, "", true
	}
	plain, err := decryptSecret(enc)
	if err != nil {
		return baseURL, "", true
	}
	return baseURL, plain, true
}

// AgentApiKeyState 诊断用:区分"未存密钥"与"存了但解密失败"。
// 返回 明文 / 是否已存密文 / 解密错误。
// 自愈:内存中活动档案无密文时,先从磁盘重载一次(兜住双开实例/未知覆盖
// 造成的内存与磁盘不一致——磁盘是权威源)。
func (c *ConfigService) AgentApiKeyState() (plain string, encStored bool, err error) {
	plain, encStored, err = c.agentApiKeyStateMem()
	if err == nil {
		return plain, encStored, nil
	}
	// 内存不可用 → 尝试磁盘自愈重载
	if c.reloadAgentFromDisk() {
		p2, e2, err2 := c.agentApiKeyStateMem()
		if err2 == nil {
			logConfigLoadError("agent.toml", fmt.Errorf("内存密钥不可用已从磁盘自愈: %v", err))
			return p2, e2, nil
		}
		logConfigLoadError("agent.toml", fmt.Errorf("内存密钥不可用且磁盘自愈失败: 内存=%v 磁盘=%v", err, err2))
		return p2, e2, err2
	}
	return plain, encStored, err
}

// agentApiKeyStateMem 纯内存检查。
func (c *ConfigService) agentApiKeyStateMem() (plain string, encStored bool, err error) {
	c.mu.Lock()
	enc := ""
	profileCount := len(c.config.Agent.Profiles)
	activeID := c.config.Agent.ActiveProfileID
	for _, p := range c.config.Agent.Profiles {
		if p.ID == c.config.Agent.ActiveProfileID {
			enc = p.ApiKeyEnc
			break
		}
	}
	c.mu.Unlock()
	if enc == "" {
		return "", false, fmt.Errorf("活动档案无密文(档案数=%d 活动=%q)", profileCount, activeID)
	}
	plain, err = decryptSecret(enc)
	if err != nil {
		return "", true, fmt.Errorf("解密失败: %w", err)
	}
	if plain == "" {
		return "", true, fmt.Errorf("解密结果为空")
	}
	return plain, true, nil
}

// reloadAgentFromDisk 从磁盘 agent.toml 重载 agent 节覆盖内存(权威源)。
// 磁盘无档案/解析失败返回 false。
func (c *ConfigService) reloadAgentFromDisk() bool {
	data, rerr := os.ReadFile(AgentConfigFile())
	if rerr != nil {
		return false
	}
	var f struct {
		Agent AgentConfig `toml:"agent"`
	}
	if uerr := toml.Unmarshal(data, &f); uerr != nil || len(f.Agent.Profiles) == 0 {
		return false
	}
	c.mu.Lock()
	c.config.Agent = f.Agent
	c.mu.Unlock()
	return true
}
