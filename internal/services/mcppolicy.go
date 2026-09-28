package services

import (
	"encoding/json"
	"regexp"
	"strings"
	"sync"
)

// MCP 操作危险分级引擎。
// 两级模型:
//   - RiskBlocked: 绝对危险(rm -rf、mkfs、关机重启等),命中即拒绝执行、
//     记审计并自动挂起 MCP,需用户手动恢复
//   - RiskAuto:    其余操作一律放行(含原"常规危险"级: 经可控操作时延放行)
//
// 绝对危险字典支持用户配置(设置页编辑,空 = 内置默认字典)。

const (
	RiskAuto     = "auto"
	RiskBlocked  = "blocked"
	ReasonPrefix = ""
)

// defaultDangerousPatterns 内置绝对危险指令字典(命中即拦截+挂起)。
var defaultDangerousPatterns = []string{
	`(?i)\brm\s+(-[a-z]*r[a-z]*f|-[a-z]*f[a-z]*r)(\s|$)`,      // rm -rf / rm -fr
	`(?i)\bmkfs(\.\w+)?\b`,                                    // 格式化文件系统
	`(?i)\bdd\b[^|]*\bof=/dev/`,                               // dd 写裸设备
	`(?i):\(\)\s*\{.*\}\s*;:`,                                 // fork 炸弹
	`(?i)\bchmod\s+(-[a-z]+\s+)*-?\w*R?\w*777\s+/(\s|$)`,      // chmod 777 根目录
	`(?i)\b(shutdown|reboot|halt|poweroff)\b`,                 // 关机重启
	`(?i)\binit\s+[06]\b`,                                     // 切运行级
	`(?i)>\s*/dev/(sd|hd|nvme|disk)`,                          // 重定向写磁盘设备
	`(?i)\bformat\s+[a-z]:`,                                   // Windows format
	`(?i)\bdel\s+(/[fqs]\s*)+/`,                               // Windows del /f /s /q
	`(?i)\b(rd|rmdir)\s+/s`,                                   // Windows rd /s
	`(?i)\b(reload|erase\s+startup-config|write\s+erase)\b`,   // 网络设备重启/擦配置
	`(?i)\b(wipe\s+fs|reset\s+saved-configuration)\b`,         // 设备擦除配置
}

// GradeResult 分级结果:风险等级 + 原因。
type GradeResult struct {
	Risk   string `json:"risk"`
	Reason string `json:"reason"`
}

// GradeCommand 对单行命令做危险分级(两级: 绝对危险拦截,其余放行)。
func GradeCommand(line string) (risk string, reason string) {
	r := GradeCommandEx(line)
	return r.Risk, r.Reason
}

// GradeCommandEx 两级分级: 命中绝对危险字典即拦截,否则放行。
func GradeCommandEx(line string) GradeResult {
	cmd := strings.TrimSpace(line)
	if cmd == "" {
		return GradeResult{Risk: RiskAuto, Reason: "empty"}
	}
	for _, p := range dangerousPatterns() {
		if p.re.MatchString(cmd) {
			return GradeResult{Risk: RiskBlocked, Reason: "命中绝对危险指令规则: " + p.source}
		}
	}
	return GradeResult{Risk: RiskAuto, Reason: "常规操作,直接执行"}
}

// GradeText 对整段输入分级: 逐行检查,任一行命中绝对危险即整段拦截。
func GradeText(text string) (risk string, reason string) {
	r := GradeTextEx(text)
	return r.Risk, r.Reason
}

// GradeTextEx 整段分级(逐行判定)。
func GradeTextEx(text string) GradeResult {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for _, l := range lines {
		g := GradeCommandEx(l)
		if g.Risk == RiskBlocked {
			return g
		}
	}
	return GradeResult{Risk: RiskAuto, Reason: "常规操作,直接执行"}
}

// ==================== 绝对危险字典(编译缓存) ====================

type compiledPattern struct {
	re     *regexp.Regexp
	source string
}

// 全局危险字典(编译后缓存;配置变更时 SetDangerousPatterns 重建)。
var (
	dangerousMu       sync.RWMutex
	dangerousCompiled []compiledPattern
)

// DefaultDangerousPatterns 返回内置绝对危险字典副本。
func DefaultDangerousPatterns() []string {
	out := make([]string, len(defaultDangerousPatterns))
	copy(out, defaultDangerousPatterns)
	return out
}

// SetDangerousPatterns 重建危险字典缓存(McpService 配置变更时调用)。
// 编译失败的条目跳过(入口已校验,此处为双保险)。
func SetDangerousPatterns(patterns []string) {
	compiled := make([]compiledPattern, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			continue
		}
		compiled = append(compiled, compiledPattern{re: re, source: p})
	}
	dangerousMu.Lock()
	dangerousCompiled = compiled
	dangerousMu.Unlock()
}

// SetDangerousPatternsJSON 从配置 JSON 重建危险字典缓存。
// JSON 非法时保持现有缓存不变。空列表 = 恢复内置默认字典。
func SetDangerousPatternsJSON(jsonStr string) {
	var patterns []string
	if err := json.Unmarshal([]byte(jsonStr), &patterns); err != nil {
		return
	}
	if len(patterns) == 0 {
		patterns = DefaultDangerousPatterns()
	}
	SetDangerousPatterns(patterns)
}

// dangerousPatterns 返回当前生效的编译后字典(空配置 = 内置默认)。
func dangerousPatterns() []compiledPattern {
	dangerousMu.RLock()
	compiled := dangerousCompiled
	dangerousMu.RUnlock()
	if len(compiled) > 0 {
		return compiled
	}
	// 双保险: 缓存未装配时回退内置默认,绝不静默放行
	compiled = make([]compiledPattern, 0, len(defaultDangerousPatterns))
	for _, p := range defaultDangerousPatterns {
		if re, err := regexp.Compile(p); err == nil {
			compiled = append(compiled, compiledPattern{re: re, source: p})
		}
	}
	return compiled
}

// init 装配内置默认字典(配置加载后由 McpService 以用户配置覆盖)。
func init() {
	SetDangerousPatterns(DefaultDangerousPatterns())
}
