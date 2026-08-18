//go:build windows

package services

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

// 解析注册表命令字符串中的可执行文件路径，如 `"C:\path\chrome.exe" --single-argument %1`。
// 无引号时尝试完整路径，失败则按空格逐步截断（路径本身可能含空格）。
func parseExecPathFromCommand(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if len(cmd) > 1 && cmd[0] == '"' {
		if end := strings.IndexByte(cmd[1:], '"'); end >= 0 {
			return cmd[1 : 1+end]
		}
		return strings.Trim(cmd, `"`)
	}
	parts := strings.Split(cmd, " ")
	for i := len(parts); i > 0; i-- {
		candidate := strings.Join(parts[:i], " ")
		if strings.HasSuffix(strings.ToLower(candidate), ".exe") {
			if _, err := osStat(candidate); err == nil {
				return candidate
			}
		}
	}
	return strings.TrimSpace(parts[0])
}

// readRegString 读取注册表字符串值。
func readRegString(root registry.Key, path, name string) (string, error) {
	key, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer key.Close()
	v, _, err := key.GetStringValue(name)
	return v, err
}

// scanWindowsBrowsers 枚举注册表 StartMenuInternet 客户端并探测默认浏览器。
func scanWindowsBrowsers() []BrowserInfo {
	seen := make(map[string]bool)
	var list []BrowserInfo

	// 默认浏览器:优先 UserChoice ProgId（用户"默认应用"设置），失败回退 HKCR http 关联
	defaultExec := ""
	if progID, err := readRegString(registry.CURRENT_USER, `Software\Microsoft\Windows\Shell\Associations\UrlAssociations\http\UserChoice`, "ProgId"); err == nil && progID != "" {
		if key, kerr := registry.OpenKey(registry.CLASSES_ROOT, progID+`\shell\open\command`, registry.QUERY_VALUE); kerr == nil {
			if v, _, verr := key.GetStringValue(""); verr == nil {
				defaultExec = parseExecPathFromCommand(v)
			}
			key.Close()
		}
	}
	if defaultExec == "" {
		if key, err := registry.OpenKey(registry.CLASSES_ROOT, `http\shell\open\command`, registry.QUERY_VALUE); err == nil {
			if v, _, err := key.GetStringValue(""); err == nil {
				defaultExec = parseExecPathFromCommand(v)
			}
			key.Close()
		}
	}

	roots := []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER}
	for _, root := range roots {
		key, err := registry.OpenKey(root, `SOFTWARE\Clients\StartMenuInternet`, registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			continue
		}
		names, _ := key.ReadSubKeyNames(-1)
		key.Close()
		for _, sub := range names {
			subKey, err := registry.OpenKey(root, `SOFTWARE\Clients\StartMenuInternet\`+sub, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			displayName, _, _ := subKey.GetStringValue("")
			if displayName == "" {
				displayName = sub
			}
			cmdKey, err := registry.OpenKey(root, `SOFTWARE\Clients\StartMenuInternet\`+sub+`\shell\open\command`, registry.QUERY_VALUE)
			execPath := ""
			if err == nil {
				if v, _, verr := cmdKey.GetStringValue(""); verr == nil {
					execPath = parseExecPathFromCommand(v)
				}
				cmdKey.Close()
			}
			subKey.Close()
			if execPath == "" || seen[execPath] {
				continue
			}
			seen[execPath] = true
			id := normalizeBrowserID(displayName)
			if id == "" {
				id = normalizeBrowserID(sub)
			}
			list = append(list, BrowserInfo{
				ID:        id,
				Name:      displayName,
				ExecPath:  execPath,
				IsDefault: defaultExec != "" && strings.EqualFold(execPath, defaultExec),
			})
		}
	}

	// 常用浏览器安装路径兜底
	findCommonBrowsers(&list, &seen, defaultExec)

	// 默认浏览器条目
	if defaultExec != "" {
		list = append([]BrowserInfo{{ID: "default", Name: "默认浏览器", ExecPath: defaultExec, IsDefault: true}}, list...)
	} else {
		list = append([]BrowserInfo{{ID: "default", Name: "默认浏览器", IsDefault: true}}, list...)
	}
	return list
}

// findCommonBrowsers 在常见安装路径与 PATH 中探测主流浏览器。
func findCommonBrowsers(list *[]BrowserInfo, seen *map[string]bool, defaultExec string) {
	programFiles := osGetenv("ProgramFiles")
	programFilesX86 := osGetenv("ProgramFiles(x86)")
	localAppData := osGetenv("LOCALAPPDATA")
	candidates := []struct {
		name string
		path string
	}{
		{"Google Chrome", filepath.Join(programFiles, "Google", "Chrome", "Application", "chrome.exe")},
		{"Google Chrome", filepath.Join(programFilesX86, "Google", "Chrome", "Application", "chrome.exe")},
		{"Google Chrome", filepath.Join(localAppData, "Google", "Chrome", "Application", "chrome.exe")},
		{"Mozilla Firefox", filepath.Join(programFiles, "Mozilla", "Firefox", "firefox.exe")},
		{"Mozilla Firefox", filepath.Join(programFilesX86, "Mozilla", "Firefox", "firefox.exe")},
		{"Microsoft Edge", filepath.Join(programFiles, "Microsoft", "Edge", "Application", "msedge.exe")},
		{"Microsoft Edge", filepath.Join(programFilesX86, "Microsoft", "Edge", "Application", "msedge.exe")},
	}
	for _, c := range candidates {
		if c.path == "" {
			continue
		}
		if _, err := osStat(c.path); err != nil {
			continue
		}
		if (*seen)[c.path] {
			continue
		}
		(*seen)[c.path] = true
		id := normalizeBrowserID(c.name)
		if id == "" {
			id = "browser"
		}
		*list = append(*list, BrowserInfo{ID: id, Name: c.name, ExecPath: c.path, IsDefault: strings.EqualFold(c.path, defaultExec)})
	}
}

// openUrlByPlatform 打开 URL。target.ID 为 "default" 时使用系统默认方式。
func openUrlByPlatform(target BrowserInfo, url string) error {
	if target.ID == "default" {
		if target.ExecPath == "" {
			cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
			if err := cmd.Start(); err != nil {
				return err
			}
			return nil
		}
		return startBrowserProcess(target.ExecPath, url)
	}
	if target.ExecPath == "" {
		return errors.New("浏览器可执行文件路径为空")
	}
	if _, err := osStat(target.ExecPath); err != nil {
		return err
	}
	return startBrowserProcess(target.ExecPath, url)
}

func startBrowserProcess(execPath, url string) error {
	cmd := exec.Command(execPath, url)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return nil
}

// scanBrowsersByPlatform 平台入口。
func scanBrowsersByPlatform() []BrowserInfo {
	return scanWindowsBrowsers()
}
