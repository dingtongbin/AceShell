package services

import (
	"os"
	"path/filepath"
	"runtime"
)

const (
	configFileName = "config.toml"
	dbFileName     = "aceshell.db"
	autoLogDirName = "autolog"
	scriptsDirName = "script"
)

// dataDir 当前生效的应用数据目录(默认平台应用数据目录,测试可重定向)。
var dataDir string

// configFile 全局配置文件路径(位于数据目录)。
var configFile string

func init() {
	dataDir = defaultDataDir()
	configFile = filepath.Join(dataDir, configFileName)
}

// defaultDataDir 返回平台应用数据目录:
// Windows: %AppData%\AceShell;macOS: ~/Library/Application Support/AceShell;
// Linux: $XDG_CONFIG_HOME/aceshell 或 ~/.config/aceshell。
func defaultDataDir() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	if runtime.GOOS == "linux" {
		return filepath.Join(base, "aceshell")
	}
	return filepath.Join(base, "AceShell")
}

// SetDataDir 重定向应用数据目录(测试用),返回恢复函数。
func SetDataDir(dir string) func() {
	prev := dataDir
	dataDir = dir
	configFile = filepath.Join(dir, configFileName)
	sessionsDir = filepath.Join(dir, sessionsDirName)
	return func() {
		dataDir = prev
		configFile = filepath.Join(prev, configFileName)
		sessionsDir = filepath.Join(prev, sessionsDirName)
	}
}

// DataDir 返回当前应用数据目录。
func DataDir() string {
	return dataDir
}

// ConfigFilePath 返回全局配置文件路径。
func ConfigFilePath() string {
	return configFile
}

// McpConfigFile 返回 MCP 独立配置文件路径(与主配置同目录)。
func McpConfigFile() string {
	return filepath.Join(filepath.Dir(configFile), "mcp.toml")
}

// AgentConfigFile 返回智能体独立配置文件路径(与主配置同目录)。
func AgentConfigFile() string {
	return filepath.Join(filepath.Dir(configFile), "agent.toml")
}

// DBFilePath 返回本地数据库文件路径。
func DBFilePath() string {
	return filepath.Join(dataDir, dbFileName)
}

// SessionsDir 返回会话根目录。
func SessionsDir() string {
	return filepath.Join(dataDir, sessionsDirName)
}

// AutoLogDir 返回自动日志目录。
func AutoLogDir() string {
	return filepath.Join(dataDir, autoLogDirName)
}

// ScriptsDir 返回脚本目录。
func ScriptsDir() string {
	return filepath.Join(dataDir, scriptsDirName)
}
