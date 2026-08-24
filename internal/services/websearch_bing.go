package services

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// 必应网页版 HTML 抓取引擎(免 Key,国内直连):
//   - www.bing.com/search?q= 服务端渲染,结果结构(li.b_algo)稳定
//   - 国内网络环境直连可达(302 到 cn.bing.com 由 HTTP 客户端自动跟随)
//   - 被人机验证拦截时返回明确错误,由回退链降级到 rod 真实渲染

const (
	bingSearchEndpoint = "https://www.bing.com/search?mkt=zh-CN&setlang=zh-hans&q="
	bingBodyMax        = 2 << 20 // 响应体积上限 2MB
)

var (
	// b_algo 结果块(class 与属性顺序不固定)
	bingBlockRe   = regexp.MustCompile(`(?is)<li\b[^>]*class="[^"]*b_algo[^"]*"[^>]*>([\s\S]*?)</li>`)
	bingAnchorRe  = regexp.MustCompile(`(?is)<h2[^>]*>\s*<a\b[^>]*href=["']([^"']+)["'][^>]*>([\s\S]*?)</a>`)
	bingSnippetRe = regexp.MustCompile(`(?is)<p\b[^>]*>([\s\S]*?)</p>`)
	bingCaptchaRe = regexp.MustCompile(`(?i)(challenge|captcha|verify you are (a )?human|异常流量)`)
)

// searchBingHTTP 通过必应网页版执行搜索。
func searchBingHTTP(ctx context.Context, query string, maxResults int) ([]webSearchResult, error) {
	ctx, cancel := context.WithTimeout(ctx, bingHTTPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		bingSearchEndpoint+url.QueryEscape(query), nil)
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("User-Agent", webSearchUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	buf := make([]byte, 0, bingBodyMax+1)
	tmp := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if len(buf) > bingBodyMax {
			return nil, fmt.Errorf("响应过大")
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return nil, fmt.Errorf("读取失败: %w", readErr)
		}
	}
	page := string(buf)
	if bingCaptchaRe.MatchString(page) && !strings.Contains(page, "b_algo") {
		return nil, fmt.Errorf("被人机验证拦截")
	}
	results := parseBingHtml(page)
	if len(results) == 0 {
		return nil, fmt.Errorf("未解析到结果(页面结构可能已变化)")
	}
	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return results, nil
}

// parseBingHtml 解析必应 SERP:逐个 b_algo 块提取 h2>a 标题链接与首个 <p> 摘要。
func parseBingHtml(page string) []webSearchResult {
	var results []webSearchResult
	for _, block := range bingBlockRe.FindAllStringSubmatch(page, -1) {
		body := block[1]
		am := bingAnchorRe.FindStringSubmatch(body)
		if am == nil {
			continue
		}
		title := collapseSpaces(html.UnescapeString(stripHTMLTags(am[2])))
		link := html.UnescapeString(strings.TrimSpace(am[1]))
		if title == "" || link == "" {
			continue
		}
		snippet := ""
		if sm := bingSnippetRe.FindStringSubmatch(body); sm != nil {
			candidate := collapseSpaces(html.UnescapeString(stripHTMLTags(sm[1])))
			// 排除误匹配的导航/日期行:摘要通常较长且不含链接语法残留
			if len(candidate) >= 12 {
				snippet = candidate
			}
		}
		results = append(results, webSearchResult{Title: title, URL: link, Snippet: snippet})
	}
	return results
}

// collapseSpaces 折叠连续空白为单个空格并去首尾。
func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
