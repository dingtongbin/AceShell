package services

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// 内置 MCP 服务器: 把 AceShell 自有能力(web_search 联网搜索回退链)以标准 MCP 工具形态提供,
// 经 InMemoryTransport 与智能体客户端进程内直连——与外接 MCP 走完全相同的发现/调用管线。

var (
	builtinOnce    sync.Once
	builtinClientT *mcp.InMemoryTransport
	builtinErr     error
)

// builtinSearchIn web_search 工具参数。
type builtinSearchIn struct {
	Query      string `json:"query" jsonschema:"搜索关键词(中英文均可,可用空格组合多个词)"`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"返回条数上限(默认 8,最大 15)"`
}

// setupBuiltinServer 初始化内置服务器与进程内传输(仅执行一次)。
func setupBuiltinServer() {
	server := mcp.NewServer(&mcp.Implementation{Name: "aceshell-builtin", Version: "1.0"}, nil)

	type searchOut struct{}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "web_search",
		Description: "联网搜索最新公开信息。当任务涉及时效性内容(新闻/版本发布/价格/安全通告)、不确定事实或需要外部资料佐证时调用;返回标题+链接+摘要列表。",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in builtinSearchIn) (*mcp.CallToolResult, searchOut, error) {
		results, err := webSearch(ctx, in.Query, in.MaxResults)
		if err != nil {
			return nil, searchOut{}, err
		}
		var b strings.Builder
		fmt.Fprintf(&b, "共 %d 条搜索结果:\n", len(results))
		for i, r := range results {
			fmt.Fprintf(&b, "%d. %s\n   %s\n", i+1, r.Title, r.URL)
			if r.Snippet != "" {
				fmt.Fprintf(&b, "   %s\n", r.Snippet)
			}
		}
		report := truncateUtf8(b.String(), agentToolResultMax)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: report}}}, searchOut{}, nil
	})

	clientTr, serverTr := mcp.NewInMemoryTransports()
	// 服务端循环随 transport 关闭而结束;应用进程生命周期内常驻
	go func() { _ = server.Run(context.Background(), serverTr) }()
	builtinClientT = clientTr
}

// builtinMcpTransport 返回连接到内置服务器的客户端传输(懒初始化;失败时返回会报错的占位)。
func builtinMcpTransport() mcp.Transport {
	builtinOnce.Do(func() { setupBuiltinServer() })
	if builtinClientT == nil || builtinErr != nil {
		return errTransport{err: fmt.Errorf("内置服务器不可用")}
	}
	return builtinClientT
}

// errTransport 始终失败的占位传输(内置服务器初始化异常时的兜底)。
type errTransport struct{ err error }

func (e errTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	return nil, e.err
}
