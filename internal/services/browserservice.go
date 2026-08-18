package services

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// BrowserInfo 描述一台可用的浏览器。
type BrowserInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ExecPath  string `json:"execPath"`
	IsDefault bool   `json:"isDefault"`
}

// BrowserService 提供浏览器扫描与 URL 打开能力（HTTP 会话使用）。
type BrowserService struct{}

// 平台实现可被测试替换。
var (
	scanBrowsersFn = scanBrowsersByPlatform
	openUrlImpl    = openUrlByPlatform
	osGetenv       = os.Getenv
	osStat         = os.Stat
)

// normalizeBrowserID 将浏览器名规范化为稳定 ID。
func normalizeBrowserID(name string) string {
	out := make([]rune, 0, len(name))
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			out = append(out, r)
			continue
		}
		if r == ' ' || r == '-' || r == '_' {
			out = append(out, '-')
		}
	}
	return strings.Trim(string(out), "-")
}

// ScanBrowsers 扫描本机可用浏览器，返回 JSON 数组（始终包含 default 条目）。
func (b *BrowserService) ScanBrowsers() string {
	list := scanBrowsersFn()
	hasDefault := false
	for i := range list {
		if list[i].ID == "" {
			list[i].ID = fmt.Sprintf("browser-%d", i)
		}
		if list[i].ID == "default" {
			hasDefault = true
		}
	}
	if !hasDefault {
		list = append([]BrowserInfo{{ID: "default", Name: "默认浏览器", IsDefault: true}}, list...)
	}
	data, err := json.Marshal(list)
	if err != nil {
		return "[]"
	}
	return string(data)
}

// OpenUrl 在指定浏览器中打开 URL。browserID 为空或 "default" 使用系统默认浏览器。
// 成功返回空字符串;失败返回可读错误信息。
func (b *BrowserService) OpenUrl(browserID, url string) string {
	u := strings.TrimSpace(url)
	if !strings.HasPrefix(strings.ToLower(u), "http://") && !strings.HasPrefix(strings.ToLower(u), "https://") {
		return "URL 必须以 http:// 或 https:// 开头"
	}
	if browserID == "" {
		browserID = "default"
	}
	var target *BrowserInfo
	list := scanBrowsersFn()
	for i := range list {
		if list[i].ID == browserID {
			target = &list[i]
			break
		}
	}
	if target == nil {
		return fmt.Sprintf("所选浏览器不存在或不可用（%s），请重新选择浏览器", browserID)
	}
	if err := openUrlImpl(*target, u); err != nil {
		return fmt.Sprintf("打开浏览器失败：%v", err)
	}
	return ""
}
