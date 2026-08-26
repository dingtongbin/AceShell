package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// DuckDuckGo Lite 抓取引擎(免 Key,海外环境兜底):
//   - lite.duckduckgo.com/lite/ POST 表单即返回无 JS 纯 HTML SERP,
//     无需 vqd token 握手
//   - 被限流时返回 challenge/anomaly 页面 → 明确报错

const (
	ddgSearchEndpoint = "https://lite.duckduckgo.com/lite/"
	ddgBodyMax        = 1 << 20 // 响应体积上限 1MB(lite 页通常 <150KB)
)

var (
	// result-link 锚点行(class 与 href 属性顺序不固定,整体匹配后单独提取 href)
	ddgLinkRe    = regexp.MustCompile(`(?i)<a\b[^>]*class=["'][^"']*result-link[^>]*>([\s\S]*?)</a>`)
	ddgHrefRe    = regexp.MustCompile(`(?i)href=["']([^"']+)["']`)
	ddgSnippetRe = regexp.MustCompile(`(?i)<td\b[^>]*class=["'][^"']*result-snippet[^>]*>([\s\S]*?)</td>`)
)

// searchDDGLite 通过 DuckDuckGo Lite 执行搜索。
func searchDDGLite(ctx context.Context, query string, maxResults int) ([]webSearchResult, error) {
	ctx, cancel := context.WithTimeout(ctx, ddgTimeout)
	defer cancel()

	form := url.Values{"q": {query}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ddgSearchEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", webSearchUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Referer", ddgSearchEndpoint)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// 超时/连接失败大多为网络不可达;DefaultClient 自动使用 HTTP(S)_PROXY 环境变量
		return nil, fmt.Errorf("请求失败(若无法直连 DuckDuckGo 可配置 HTTPS_PROXY): %w", err)
	}
	defer resp.Body.Close()

	// DDG 对过快请求返回 202 软限流
	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("服务暂时限流(HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	buf := make([]byte, 0, ddgBodyMax+1)
	tmp := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if len(buf) > ddgBodyMax {
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
	if isDDGCaptcha(page) {
		return nil, fmt.Errorf("触发人机验证(请求过于频繁)")
	}
	results := parseDDGHtml(page)
	if len(results) == 0 {
		return nil, fmt.Errorf("未解析到结果")
	}
	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return results, nil
}

// isDDGCaptcha 检测 DDG 人机验证页(challenge-form / anomaly-modal / 明文提示)。
func isDDGCaptcha(page string) bool {
	for _, marker := range []string{"challenge-form", "anomaly-modal", `class="anomaly"`, "Please verify you are a human"} {
		if strings.Contains(page, marker) {
			return true
		}
	}
	return false
}

// parseDDGHtml 解析 Lite SERP:以 result-link 锚点定位结果行,
// 在其后的小窗口内寻找可选的 result-snippet 摘要。
func parseDDGHtml(page string) []webSearchResult {
	var results []webSearchResult
	for _, m := range ddgLinkRe.FindAllStringSubmatchIndex(page, -1) {
		anchor := page[m[0]:m[1]]
		title := collapseSpaces(htmlUnescape(stripHTMLTags(page[m[2]:m[3]])))
		href := ""
		if hm := ddgHrefRe.FindStringSubmatch(anchor); hm != nil {
			href = hm[1]
		}
		realURL := decodeDDGURL(href)
		if realURL == "" || title == "" {
			continue
		}
		snippet := ""
		windowEnd := m[1] + 2000
		if windowEnd > len(page) {
			windowEnd = len(page)
		}
		if sm := ddgSnippetRe.FindStringSubmatch(page[m[1]:windowEnd]); sm != nil {
			snippet = collapseSpaces(htmlUnescape(stripHTMLTags(sm[1])))
		}
		results = append(results, webSearchResult{Title: title, URL: realURL, Snippet: snippet})
	}
	return results
}

// decodeDDGURL 解包 DDG 重定向链接:
// "//duckduckgo.com/l/?uddg=<encoded>&rut=..." → 真实 URL;其余原样规整。
func decodeDDGURL(href string) string {
	if href == "" {
		return ""
	}
	normalized := href
	if strings.HasPrefix(normalized, "//") {
		normalized = "https:" + normalized
	}
	u, err := url.Parse(normalized)
	if err != nil {
		return href
	}
	if strings.HasSuffix(u.Hostname(), "duckduckgo.com") && u.Path == "/l/" {
		if inner := u.Query().Get("uddg"); inner != "" {
			return inner
		}
	}
	return normalized
}

// htmlUnescape 解码常见 HTML 实体(lite 页摘要基本为纯文本,标准库全量解码亦可)。
func htmlUnescape(s string) string {
	r := strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", "\"", "&#x27;", "'", "&nbsp;", " ")
	return r.Replace(s)
}

// stripHTMLTags 去除标签并折叠空白。
func stripHTMLTags(s string) string {
	s = htmlTagRe.ReplaceAllString(s, " ")
	return s
}

var htmlTagRe = regexp.MustCompile(`<[^>]*>`)
