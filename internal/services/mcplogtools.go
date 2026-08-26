package services

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ==================== MCP 日志工具(search_logs / log_detail) ====================
// 只读工具:不进仲裁车道,不弹审批,RiskAuto 直接执行并落审计。

// registerLogTools 注册日志搜索与详情工具(外部 HTTP MCP 客户端)。
func (s *McpService) registerLogTools(server *mcp.Server) {
	type searchLogsIn struct {
		Query          string `json:"query" jsonschema:"关键字,匹配会话标题/主机/用户名;留空则列出最近的日志"`
		Protocol       string `json:"protocol,omitempty" jsonschema:"按协议过滤:ssh/telnet/serial/shell,留空不过滤"`
		IncludeContent bool   `json:"include_content,omitempty" jsonschema:"是否同时在日志正文中搜索关键字并返回命中行"`
		Limit          int    `json:"limit,omitempty" jsonschema:"最多返回条数(默认 20)"`
	}
	type searchLogsOut struct {
		Total int            `json:"total"`
		Logs  []LogSearchHit `json:"logs"`
	}
	addTrackedTool(s, server, &mcp.Tool{
		Name:        "search_logs",
		Description: "搜索会话自动日志。按关键字匹配会话标题/主机/用户名,可选按协议过滤、可选在日志正文中检索命中行。返回日志 ID 供 log_detail 使用。",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in searchLogsIn) (*mcp.CallToolResult, searchLogsOut, error) {
		res, err := s.toolSearchLogs(mcpSrcExternal, in.Query, in.Protocol, in.IncludeContent, in.Limit)
		if err != nil {
			return nil, searchLogsOut{}, err
		}
		var out searchLogsOut
		json.Unmarshal([]byte(res), &out)
		return nil, out, nil
	})

	type logDetailIn struct {
		LogID     string `json:"log_id" jsonschema:"日志 ID(search_logs 返回的 id)"`
		TailLines int    `json:"tail_lines,omitempty" jsonschema:"返回正文末尾行数(默认 500)"`
	}
	type logDetailOut struct {
		Meta    map[string]any `json:"meta"`
		Content string         `json:"content"`
	}
	addTrackedTool(s, server, &mcp.Tool{
		Name:        "log_detail",
		Description: "查看自动日志详情:返回元数据(协议/主机/端口/用户名/起止时间/行数字节数)与正文尾部内容(已含原始控制序列)。",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in logDetailIn) (*mcp.CallToolResult, logDetailOut, error) {
		res, err := s.toolLogDetail(mcpSrcExternal, in.LogID, in.TailLines)
		if err != nil {
			return nil, logDetailOut{}, err
		}
		var out logDetailOut
		json.Unmarshal([]byte(res), &out)
		return nil, out, nil
	})
}

// toolSearchLogs 搜索自动日志(只读)。
func (s *McpService) toolSearchLogs(source, query, protocol string, includeContent bool, limit int) (string, error) {
	if err := s.checkRunning(); err != nil {
		return "", err
	}
	if MainLogService == nil {
		return "", fmt.Errorf("日志服务不可用")
	}
	hits := MainLogService.SearchLogs(query, protocol, includeContent, limit)
	out := map[string]any{"total": len(hits), "logs": hits}
	s.audit.Append(source, "info", "search_logs", query, fmt.Sprintf("%d 条匹配", len(hits)), RiskAuto, "executed", false)
	data, _ := json.Marshal(out)
	return string(data), nil
}

// toolLogDetail 查看日志详情(元数据+尾部内容,只读)。
func (s *McpService) toolLogDetail(source, logID string, tailLines int) (string, error) {
	if err := s.checkRunning(); err != nil {
		return "", err
	}
	if MainLogService == nil {
		return "", fmt.Errorf("日志服务不可用")
	}
	if !validLogID(logID) {
		return "", fmt.Errorf("非法的日志 ID: %s", logID)
	}
	metaJSON, content := MainLogService.LogDetail(logID, tailLines)
	if metaJSON == "{}" {
		return "", fmt.Errorf("日志不存在: %s", logID)
	}
	meta := map[string]any{}
	json.Unmarshal([]byte(metaJSON), &meta)
	out := map[string]any{"meta": meta, "content": content}
	s.audit.Append(source, "info", "log_detail", logID, "查看日志详情", RiskAuto, "executed", false)
	data, _ := json.Marshal(out)
	return string(data), nil
}
