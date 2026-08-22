package services

import (
	"regexp"
	"strings"
	"sync"
)

// MCP 操作危险分级引擎。
// 三级模型:
//   - RiskBlocked: 绝对危险(rm -rf、mkfs、关机重启等),任何模式下都拒绝,
//     触发即拦截并自动挂起 MCP
//   - RiskConfirm: 常规危险(文件写入、软件操作、配置修改等),默认需用户
//     手动授权;自动审批模式下由 AI 判定直接放行
//   - RiskAuto:    安全操作(查看类命令、打开标签页、读输出),直接执行
//
// 判定顺序: blocked → confirm → safe(白名单) → 未知默认 confirm(失败安全:
// 不认识的命令一律要求确认,绝不静默放行)。

const (
	RiskAuto     = "auto"
	RiskConfirm  = "confirm"
	RiskBlocked  = "blocked"
	ReasonPrefix = ""
)

// blockedPatterns 绝对危险指令(命中即拦截+挂起)。
var blockedPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\brm\s+(-[a-z]*r[a-z]*f|-[a-z]*f[a-z]*r)(\s|$)`), // rm -rf / rm -fr
	regexp.MustCompile(`(?i)\bmkfs(\.\w+)?\b`),                              // 格式化文件系统
	regexp.MustCompile(`(?i)\bdd\b[^|]*\bof=/dev/`),                        // dd 写裸设备
	regexp.MustCompile(`(?i):\(\)\s*\{.*\}\s*;:`),                          // fork 炸弹
	regexp.MustCompile(`(?i)\bchmod\s+(-[a-z]+\s+)*-?\w*R?\w*777\s+/(\s|$)`), // chmod 777 根目录
	regexp.MustCompile(`(?i)\b(shutdown|reboot|halt|poweroff)\b`),          // 关机重启
	regexp.MustCompile(`(?i)\binit\s+[06]\b`),                              // 切运行级
	regexp.MustCompile(`(?i)>\s*/dev/(sd|hd|nvme|disk)`),                   // 重定向写磁盘设备
	regexp.MustCompile(`(?i)\bformat\s+[a-z]:`),                            // Windows format
	regexp.MustCompile(`(?i)\bdel\s+(/[fqs]\s*)+/`),                        // Windows del /f /s /q
	regexp.MustCompile(`(?i)\b(rd|rmdir)\s+/s`),                            // Windows rd /s
	regexp.MustCompile(`(?i)\b(reload|erase\s+startup-config|write\s+erase)\b`), // 网络设备重启/擦配置
	regexp.MustCompile(`(?i)\b(wipe\s+fs|reset\s+saved-configuration)\b`),  // 设备擦除配置
}

// confirmPatterns 常规危险指令(默认需授权)。
var confirmPatterns = []*regexp.Regexp{
	// 文件写入/修改/删除
	regexp.MustCompile(`(?i)\b(rm|rmdir|mv|cp|mkdir|touch|truncate|tee|chmod|chown|ln|shred)\b`),
	regexp.MustCompile(`(>>?)\s*\S`), // 重定向写入
	// 软件安装/卸载/服务操作
	regexp.MustCompile(`(?i)\b(apt|apt-get|yum|dnf|brew|pacman|choco|winget)\b.*\b(install|remove|purge|upgrade|uninstall)\b`),
	regexp.MustCompile(`(?i)\b(pip3?|npm|yarn|pnpm|cargo|go)\b.*\b(install|uninstall)\b`),
	regexp.MustCompile(`(?i)\b(systemctl|service)\b`),
	// 提权/进程/用户管理
	regexp.MustCompile(`(?i)\b(sudo|su|doas|kill|pkill|killall)\b`),
	regexp.MustCompile(`(?i)\b(useradd|userdel|usermod|passwd|chpasswd|groupadd|groupdel)\b`),
	// 定时任务/防火墙
	regexp.MustCompile(`(?i)\b(crontab|iptables|firewall-cmd|ufw|netsh)\b`),
	// 管道进解释器(下载执行链)
	regexp.MustCompile(`(?i)\|\s*(ba|z|da|k)?sh\b`),
	regexp.MustCompile(`(?i)\|\s*(python3?|perl|ruby|node)\b`),
	// 下载落盘
	regexp.MustCompile(`(?i)\b(wget|curl|invoke-webrequest)\b.*(-o|--output|-OutFile)`),
	// find 危险参数
	regexp.MustCompile(`(?i)\bfind\b.*(-delete|-exec)`),
	// 网络设备配置模式/保存
	regexp.MustCompile(`(?im)^\s*(sys|system-view|configure\s+terminal|config\s+t)\s*$`),
	regexp.MustCompile(`(?i)\b(save|write\s+memory|copy\s+running-config\s+startup-config)\b`),
	// 容器/编排
	regexp.MustCompile(`(?i)\b(docker|kubectl|podman)\b`),
	// git 破坏性操作
	regexp.MustCompile(`(?i)\bgit\b.*\b(push\s+--force|reset\s+--hard|clean\s+-[a-z]*f|checkout\s+--)\b`),
}

// safePatterns 安全指令白名单(查看类,直接放行)。
var safePatterns = []*regexp.Regexp{
	// 网络设备查看命令(display/show 为华为/思科查看族)
	regexp.MustCompile(`(?im)^\s*(display|show)\b`),
	// 通用只读命令
	regexp.MustCompile(`(?im)^\s*(ls|ll|la|dir|cat|head|tail|less|more|grep|egrep|find|pwd|whoami|id|uname|date|cal|wc|which|whereis|file|stat|du|df|free|ps|top|htop|uptime|netstat|ss|ifconfig|ipconfig|ping|traceroute|tracert|arp|nslookup|dig|who|w|last|history|echo|printf|type|help|man|exit|quit|logout|return)\b`),
	// ip 查看子命令
	regexp.MustCompile(`(?im)^\s*ip\s+(addr|route|link|neigh)\b`),
	// 设备分屏设置
	regexp.MustCompile(`(?im)^\s*(screen\s+length|terminal\s+length|screen-length)\b`),
	// 清屏
	regexp.MustCompile(`(?im)^\s*clear\b`),
}

// GradeResult 分级结果:风险等级 + 原因 + 涉及路径(供审批展示与永久授权匹配)。
type GradeResult struct {
	Risk   string   `json:"risk"`
	Reason string   `json:"reason"`
	Paths  []string `json:"paths"`
}

// GradeCommand 对单行命令做危险分级(路径感知版)。
// 多行文本由调用方先行处理(多行整体视为 confirm,经前端粘贴确认弹窗)。
func GradeCommand(line string) (risk string, reason string) {
	r := GradeCommandEx(line)
	return r.Risk, r.Reason
}

// GradeCommandEx 路径感知分级: 提取命令涉及路径;命中敏感路径时 auto 升 confirm。
// 自定义规则优先于内置规则(用户显式配置优先)。
func GradeCommandEx(line string) GradeResult {
	cmd := strings.TrimSpace(line)
	if cmd == "" {
		return GradeResult{Risk: RiskAuto, Reason: "empty"}
	}
	// 1. 用户自定义规则优先
	if custom := gradeCustom(cmd); custom != nil {
		return *custom
	}
	// 2. 内置 blocked
	for _, p := range blockedPatterns {
		if p.MatchString(cmd) {
			return GradeResult{Risk: RiskBlocked, Reason: "命中绝对危险指令规则: " + p.String(), Paths: extractPaths(cmd)}
		}
	}
	// 3. 内置 confirm
	for _, p := range confirmPatterns {
		if p.MatchString(cmd) {
			return GradeResult{Risk: RiskConfirm, Reason: "常规危险操作,需确认", Paths: extractPaths(cmd)}
		}
	}
	// 4. 安全白名单(敏感路径仍升级: cat /etc/shadow 虽以 cat 开头但必须确认)
	paths := extractPaths(cmd)
	for _, p := range safePatterns {
		if p.MatchString(cmd) {
			if hasSensitivePath(paths) {
				return GradeResult{Risk: RiskConfirm, Reason: "命令涉及敏感路径,需确认", Paths: paths}
			}
			return GradeResult{Risk: RiskAuto, Reason: "安全查看类命令", Paths: paths}
		}
	}
	// 5. 未知命令:失败安全,要求确认
	return GradeResult{Risk: RiskConfirm, Reason: "未识别命令,默认需确认", Paths: paths}
}

// gradeCustom 应用用户自定义规则(全局规则集由 McpService 注入更新)。
func gradeCustom(cmd string) *GradeResult {
	mcpCustomRulesMu.RLock()
	defer mcpCustomRulesMu.RUnlock()
	for _, c := range mcpCustomRulesCompiled {
		if c.re.MatchString(cmd) {
			reason := "自定义规则: " + c.note
			return &GradeResult{Risk: c.risk, Reason: reason, Paths: extractPaths(cmd)}
		}
	}
	return nil
}

// 全局自定义规则集(编译后缓存;配置变更时 RefreshCustomRules 重建)。
var (
	mcpCustomRulesMu        sync.RWMutex
	mcpCustomRulesCompiled  []*mcpCustomCompiled
)

// RefreshCustomRules 重建自定义规则缓存(McpService 配置变更时调用)。
func RefreshCustomRules(rules []McpCustomRule) {
	compiled := compileCustomRules(rules)
	mcpCustomRulesMu.Lock()
	mcpCustomRulesCompiled = compiled
	mcpCustomRulesMu.Unlock()
}

// GradeText 对整段输入分级:多行 → confirm(走粘贴弹窗);单行 → GradeCommand。
func GradeText(text string) (risk string, reason string) {
	r := GradeTextEx(text)
	return r.Risk, r.Reason
}

// GradeTextEx 路径感知版整段分级(多行收集全部路径)。
func GradeTextEx(text string) GradeResult {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) > 1 {
		var allPaths []string
		for _, l := range lines {
			g := GradeCommandEx(l)
			if g.Risk == RiskBlocked {
				return g
			}
			allPaths = append(allPaths, g.Paths...)
		}
		return GradeResult{Risk: RiskConfirm, Reason: "多行批量输入,需确认", Paths: normalizePaths(allPaths)}
	}
	return GradeCommandEx(text)
}
