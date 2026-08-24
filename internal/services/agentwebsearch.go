package services

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// 智能体联网搜索:多引擎回退链(全部免 Key,开箱即用)。
//
// 顺序与理由:
//  1. bing-http   必应网页版 HTML 抓取(www.bing.com,国内直连,服务端渲染结构稳定)
//  2. bing-rod    go-rod 驱动系统已装 Edge/Chrome 真实渲染必应(HTTP 版被验证码/反爬拦截时,
//     真实浏览器指纹可通过;Edge 与应用内 WebView2 同内核且系统必装)
//  3. ddg-lite    DuckDuckGo Lite 抓取(海外环境兜底)
//
// 任一引擎返回非空结果即成功;全失败时汇总各引擎错误供模型转告用户。
// 有界: 查询长度/结果数/响应体积/单引擎超时全部受限。

const (
	webSearchQueryMax   = 400              // 查询长度上限(DDG 自身限制约 500)
	webSearchMaxResults = 15               // 单次返回条数上限
	webSearchDefaultN   = 8                // 未指定条数时的默认值
	bingHTTPTimeout     = 12 * time.Second // 必应 HTTP 单引擎限时
	ddgTimeout          = 15 * time.Second // DDG 单引擎限时
	rodPageTimeout      = 25 * time.Second // rod 渲染单次限时

	// webSearchUserAgent 正常浏览器 UA(降低被搜索引擎反爬拦截的概率)
	webSearchUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
)

type webSearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// searchEngine 单个搜索引擎描述。
type searchEngine struct {
	name string
	fn   func(ctx context.Context, query string, maxResults int) ([]webSearchResult, error)
}

// searchEngines 回退链顺序(bing → bing-rendered → ddg)。
var searchEngines = []searchEngine{
	{name: "bing", fn: searchBingHTTP},
	{name: "bing-rendered", fn: searchBingRod},
	{name: "duckduckgo", fn: searchDDGLite},
}

// webSearch 执行联网搜索:按回退链依次尝试,返回最多 maxResults 条结果。
func webSearch(ctx context.Context, query string, maxResults int) ([]webSearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("搜索关键词不能为空")
	}
	if len(query) > webSearchQueryMax {
		query = query[:webSearchQueryMax]
	}
	if maxResults <= 0 {
		maxResults = webSearchDefaultN
	}
	if maxResults > webSearchMaxResults {
		maxResults = webSearchMaxResults
	}

	var errs []string
	for _, e := range searchEngines {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("搜索已取消: %w", ctx.Err())
		}
		results, err := e.fn(ctx, query, maxResults)
		if err == nil && len(results) > 0 {
			return results, nil
		}
		if err != nil {
			errs = append(errs, e.name+": "+err.Error())
		} else {
			errs = append(errs, e.name+": 无结果")
		}
	}
	return nil, fmt.Errorf("全部搜索引擎均失败(%s)", strings.Join(errs, "; "))
}
