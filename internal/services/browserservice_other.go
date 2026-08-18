//go:build !windows && !darwin

package services

import (
	"errors"
	"os/exec"
	"strings"
)

// scanBrowsersByPlatform 通过 PATH 探测常见 Linux 浏览器,并探测 xdg 默认浏览器。
func scanBrowsersByPlatform() []BrowserInfo {
	names := []string{
		"firefox",
		"chromium",
		"chromium-browser",
		"google-chrome",
		"google-chrome-stable",
		"microsoft-edge",
		"brave-browser",
	}
	var list []BrowserInfo
	for _, name := range names {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		id := normalizeBrowserID(name)
		list = append(list, BrowserInfo{ID: id, Name: name, ExecPath: path})
	}
	// 默认浏览器:xdg-settings(失败则以 default 兜底打开)
	defaultName := ""
	if out, err := exec.Command("xdg-settings", "get", "default-web-browser").Output(); err == nil {
		defaultName = strings.TrimSuffix(strings.TrimSpace(string(out)), ".desktop")
	}
	if defaultName != "" {
		for i := range list {
			if list[i].ID == normalizeBrowserID(defaultName) {
				list[i].IsDefault = true
			}
		}
	}
	list = append([]BrowserInfo{{ID: "default", Name: "默认浏览器", IsDefault: true}}, list...)
	return list
}

// openUrlByPlatform 打开 URL。target.ID 为 "default" 时使用 xdg-open。
func openUrlByPlatform(target BrowserInfo, url string) error {
	if target.ID == "default" {
		return exec.Command("xdg-open", url).Start()
	}
	if target.ExecPath == "" {
		return errors.New("浏览器可执行文件路径为空")
	}
	if _, err := osStat(target.ExecPath); err != nil {
		return err
	}
	return exec.Command(target.ExecPath, url).Start()
}
