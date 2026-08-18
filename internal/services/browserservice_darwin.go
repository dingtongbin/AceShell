//go:build darwin

package services

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
)

// scanBrowsersByPlatform 扫描 macOS 常见浏览器（/Applications 目录）。
func scanBrowsersByPlatform() []BrowserInfo {
	apps := []string{
		"Safari",
		"Google Chrome",
		"Mozilla Firefox",
		"Microsoft Edge",
		"Brave Browser",
		"Arc",
	}
	var list []BrowserInfo
	for _, name := range apps {
		appPath := filepath.Join("/Applications", name+".app")
		if _, err := osStat(appPath); err != nil {
			continue
		}
		id := normalizeBrowserID(name)
		list = append(list, BrowserInfo{ID: id, Name: name, ExecPath: appPath})
	}
	list = append([]BrowserInfo{{ID: "default", Name: "默认浏览器", IsDefault: true}}, list...)
	return list
}

// openUrlByPlatform 打开 URL。target.ID 为 "default" 时使用系统默认方式（open url）。
func openUrlByPlatform(target BrowserInfo, url string) error {
	if target.ID == "default" {
		return exec.Command("open", url).Start()
	}
	if target.ExecPath == "" {
		return errors.New("浏览器应用路径为空")
	}
	if _, err := osStat(target.ExecPath); err != nil {
		return err
	}
	appName := strings.TrimSuffix(filepath.Base(target.ExecPath), ".app")
	return exec.Command("open", "-a", appName, url).Start()
}
