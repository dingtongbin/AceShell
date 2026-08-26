//go:build windows

package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
)

// listShells 扫描 Windows 可用 shell:pwsh → powershell → cmd → Git Bash → bash → WSL 发行版。
func listShells() []ShellInfo {
	shells := []ShellInfo{}
	seen := make(map[string]bool)

	add := func(name, path string) {
		resolved := resolveShellPath(path)
		if resolved == "" {
			return
		}
		key := strings.ToLower(filepath.ToSlash(resolved))
		if seen[key] {
			return
		}
		seen[key] = true
		shells = append(shells, ShellInfo{Name: name, Path: resolved})
	}

	add("PowerShell 7", "pwsh.exe")
	add("Windows PowerShell", "powershell.exe")
	add("CMD", windowsCmdExe())
	for _, p := range gitBashCandidates() {
		add("Git Bash", p)
	}
	add("Bash", "bash.exe")
	for _, d := range listWSLDistros() {
		shells = append(shells, ShellInfo{Name: "WSL: " + d, Path: "wsl://" + d})
	}
	return shells
}

// resolveShellPath 解析 shell 路径:优先 PATH 查找,绝对路径回退到文件存在性检查。
func resolveShellPath(path string) string {
	if path == "" {
		return ""
	}
	if abs, err := exec.LookPath(path); err == nil {
		return abs
	}
	if filepath.IsAbs(path) {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// windowsCmdExe 解析 cmd.exe 绝对路径(优先 ComSpec/SystemRoot,防 PATH 劫持)。
func windowsCmdExe() string {
	if comspec := os.Getenv("ComSpec"); comspec != "" {
		return comspec
	}
	if root := os.Getenv("SystemRoot"); root != "" {
		return filepath.Join(root, "System32", "cmd.exe")
	}
	return "cmd.exe"
}

// gitBashCandidates 列出 Git for Windows 常见安装路径下的 bash.exe。
func gitBashCandidates() []string {
	var roots []string
	for _, env := range []string{"ProgramFiles", "ProgramW6432", "ProgramFiles(x86)"} {
		if v := os.Getenv(env); v != "" {
			roots = append(roots, v)
		}
	}
	if v := os.Getenv("LOCALAPPDATA"); v != "" {
		roots = append(roots, filepath.Join(v, "Programs"))
	}
	var out []string
	for _, r := range roots {
		out = append(out,
			filepath.Join(r, "Git", "bin", "bash.exe"),
			filepath.Join(r, "Git", "usr", "bin", "bash.exe"),
		)
	}
	return out
}

// listWSLDistros 枚举 WSL 发行版(wsl.exe -l -q),失败或未安装返回空。
func listWSLDistros() []string {
	cmd := exec.Command("wsl.exe", "-l", "-q")
	cmd.SysProcAttr = hideWindowSysProcAttr()
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	return parseWSLDistros(out)
}

// parseWSLDistros 解析 wsl.exe -l -q 输出,过滤 docker-desktop 与空行。
func parseWSLDistros(raw []byte) []string {
	content := decodeWSLOutput(raw)
	var distros []string
	seen := make(map[string]bool)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(strings.ReplaceAll(line, "\x00", ""))
		line = strings.TrimSpace(strings.TrimPrefix(line, "*"))
		if line == "" || line == "-" {
			continue
		}
		if strings.Contains(strings.ToLower(line), "docker-desktop") {
			continue
		}
		if !seen[line] {
			seen[line] = true
			distros = append(distros, line)
		}
	}
	return distros
}

// decodeWSLOutput 将 wsl.exe 输出按 BOM 解码(UTF-16LE 或 UTF-8 回退)。
func decodeWSLOutput(raw []byte) string {
	if len(raw) >= 2 && raw[0] == 0xFF && raw[1] == 0xFE {
		u16 := make([]uint16, 0, (len(raw)-2)/2)
		for i := 2; i+1 < len(raw); i += 2 {
			u16 = append(u16, uint16(raw[i])|uint16(raw[i+1])<<8)
		}
		return string(utf16.Decode(u16))
	}
	return string(raw)
}

// hideWindowSysProcAttr 返回隐藏窗口的进程属性(扫描 shell 时不闪控制台黑框)。
func hideWindowSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
