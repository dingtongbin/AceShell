package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// 智能体外接 MCP 服务器配置(独立文件 agent-mcp.json,与主流客户端格式兼容):
//
//	{
//	  "mcpServers": {
//	    "context7":   { "type": "http", "url": "https://mcp.context7.com/mcp", "headers": {...}, "enabled": true },
//	    "web-search": { "type": "builtin", "enabled": true },
//	    "fetch":      { "command": "npx", "args": ["-y", "@modelcontextprotocol/server-fetch"], "env": {}, "enabled": false }
//	  }
//	}
//
// 字段惯例对齐 Claude Desktop / Cursor(command/args/env/url/headers),
// 社区分享的 MCP 配置片段可直接粘贴使用;enabled 为 AceShell 扩展(启停开关)。

// AgentMcpServerConfig 单个 MCP 服务器条目。
type AgentMcpServerConfig struct {
	Type    string            `json:"type"`              // http(Streamable) / sse / stdio / builtin(AceShell 内置)
	Command string            `json:"command,omitempty"` // stdio: 可执行命令(npx 等)
	Args    []string          `json:"args,omitempty"`    // stdio: 命令参数
	Env     map[string]string `json:"env,omitempty"`     // stdio: 注入子进程的环境变量
	URL     string            `json:"url,omitempty"`     // http/sse: 远程端点
	Headers map[string]string `json:"headers,omitempty"` // http/sse: 附加请求头(鉴权等)
	Enabled bool              `json:"enabled"`           // 启停开关
}

// AgentMcpConfig agent-mcp.json 根结构。
type AgentMcpConfig struct {
	McpServers map[string]*AgentMcpServerConfig `json:"mcpServers"`
}

// AgentMcpConfigFile 返回配置文件路径(数据目录下独立文件)。
func AgentMcpConfigFile() string {
	return filepath.Join(DataDir(), "agent-mcp.json")
}

// defaultAgentMcpConfig 出厂默认捆绑: web-search(内置搜索回退链) + context7(文档检索,免 Key 匿名可用)。
func defaultAgentMcpConfig() *AgentMcpConfig {
	return &AgentMcpConfig{
		McpServers: map[string]*AgentMcpServerConfig{
			"web-search": {Type: "builtin", Enabled: true},
			"context7":   {Type: "http", URL: "https://mcp.context7.com/mcp", Enabled: true},
		},
	}
}

// LoadAgentMcpConfig 读取 agent-mcp.json;文件不存在时返回出厂默认捆绑并落盘。
// 解析失败时同样回退默认(留痕),避免坏文件导致智能体工具全失。
func LoadAgentMcpConfig() (*AgentMcpConfig, error) {
	path := AgentMcpConfigFile()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := defaultAgentMcpConfig()
			if serr := saveAgentMcpConfig(cfg); serr != nil {
				return cfg, fmt.Errorf("默认 MCP 配置写入失败: %w", serr)
			}
			return cfg, nil
		}
		return nil, err
	}
	var cfg AgentMcpConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		logConfigLoadError("agent-mcp.json", err)
		def := defaultAgentMcpConfig()
		cfg = *def
		return &cfg, fmt.Errorf("agent-mcp.json 解析失败已回退默认: %w", err)
	}
	if cfg.McpServers == nil {
		cfg.McpServers = map[string]*AgentMcpServerConfig{}
	}
	return &cfg, nil
}

// SaveAgentMcpConfig 写盘(原子写)。
func SaveAgentMcpConfig(cfg *AgentMcpConfig) error {
	if cfg == nil || cfg.McpServers == nil {
		return fmt.Errorf("无效的 MCP 配置")
	}
	return saveAgentMcpConfig(cfg)
}

func saveAgentMcpConfig(cfg *AgentMcpConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(AgentMcpConfigFile(), data, 0600)
}
