package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const ddgSampleHTML = `<!DOCTYPE html><html><head><title>test</title></head><body>
<table><tr><td>1.&nbsp;</td><td>
<a rel="nofollow" class="result-link" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgolang.org%2Fdoc%2F&rut=abc123">Go <b>Documentation</b> - The Go Programming Language</a>
</td></tr><tr><td class="result-snippet">The Go programming language is an open source project&#x27;s official docs &amp; more.</td></tr>
<tr><td>2.&nbsp;</td><td>
<a rel="nofollow" class='result-link' href="https://go.dev/blog/">The Go Blog</a>
</td></tr><tr><td class="result-snippet">News and announcements from the Go team.</td></tr>
<tr><td>3.&nbsp;</td><td>
<span class="link-text">sponsored row without result-link falls out</span>
</td></tr>
</table></body></html>`

func TestParseDDGHtml(t *testing.T) {
	results := parseDDGHtml(ddgSampleHTML)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d: %+v", len(results), results)
	}
	first := results[0]
	if first.Title != "Go Documentation - The Go Programming Language" {
		t.Errorf("title mismatch: %q", first.Title)
	}
	if first.URL != "https://golang.org/doc/" {
		t.Errorf("uddg unwrap mismatch: %q", first.URL)
	}
	if !strings.Contains(first.Snippet, "official docs & more") {
		t.Errorf("snippet entity decode mismatch: %q", first.Snippet)
	}
	second := results[1]
	if second.URL != "https://go.dev/blog/" {
		t.Errorf("plain url mismatch: %q", second.URL)
	}
	if second.Snippet != "News and announcements from the Go team." {
		t.Errorf("snippet mismatch: %q", second.Snippet)
	}
}

func TestParseDDGHtml_Empty(t *testing.T) {
	if got := parseDDGHtml("<html></html>"); got != nil {
		t.Fatalf("expected nil for empty page, got %+v", got)
	}
}

func TestDecodeDDGURL(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fa%3Fb%3D1&rut=x", "https://example.com/a?b=1"},
		{"https://duckduckgo.com/l/?uddg=https://example.org/x", "https://example.org/x"},
		{"//example.com/path", "https://example.com/path"},
		{"https://example.com/plain", "https://example.com/plain"},
		{"not-a-url %%%", "not-a-url %%%"},
	}
	for _, tt := range tests {
		if got := decodeDDGURL(tt.in); got != tt.want {
			t.Errorf("decodeDDGURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestStripHTMLTags(t *testing.T) {
	// stripHTMLTags 仅把标签替换为空格(空白折叠由 collapseSpaces、实体由 htmlUnescape 组合完成)
	got := stripHTMLTags("<b>Hello</b>  <i>world</i><span>more\n\n text</span>")
	want := " Hello    world  more\n\n text "
	if got != want {
		t.Errorf("stripHTMLTags = %q, want %q", got, want)
	}
	full := collapseSpaces(htmlUnescape(stripHTMLTags("<b>Hello</b>&amp;<span>x</span>")))
	if full != "Hello & x" {
		t.Errorf("strip+unescape+collapse = %q", full)
	}
}

const bingSampleHTML = `<html><body><ol id="b_results">
<li class="b_algo"><div class="b_title"><h2 class=" b_topTitle"><a href="https://go.dev/doc/" h="ID=SERP,5138.1">Go Documentation - The Go Programming Language</a></h2></div>
<div class="b_caption"><p class="b_lineclamp4">The Go programming language is an open source project to make programmers productive.</p></div></li>
<li class="b_algo"><h2><a href="https://pkg.go.dev/std">Standard library &amp; packages</a></h2><div class="b_caption"><p>Packages for the standard library.</p></div></li>
<li class="b_nav"><a href="#">navigation row should be skipped</a></li>
</ol></body></html>`

func TestParseBingHtml(t *testing.T) {
	results := parseBingHtml(bingSampleHTML)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d: %+v", len(results), results)
	}
	if results[0].Title != "Go Documentation - The Go Programming Language" {
		t.Errorf("title mismatch: %q", results[0].Title)
	}
	if results[0].URL != "https://go.dev/doc/" {
		t.Errorf("url mismatch: %q", results[0].URL)
	}
	if !strings.Contains(results[0].Snippet, "open source project") {
		t.Errorf("snippet mismatch: %q", results[0].Snippet)
	}
	if results[1].Title != "Standard library & packages" {
		t.Errorf("entity decode mismatch: %q", results[1].Title)
	}
	if results[1].URL != "https://pkg.go.dev/std" {
		t.Errorf("url2 mismatch: %q", results[1].URL)
	}
}

func TestParseBingHtml_Empty(t *testing.T) {
	if got := parseBingHtml("<html><body>captcha page</body></html>"); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestIsDDGCaptcha(t *testing.T) {
	cases := []struct {
		html string
		want bool
	}{
		{`<form class="challenge-form">`, true},
		{`<div id="anomaly-modal">`, true},
		{`Please verify you are a human`, true},
		{`<a class="result-link">normal</a>`, false},
		{"", false},
	}
	for _, c := range cases {
		if got := isDDGCaptcha(c.html); got != c.want {
			t.Errorf("isDDGCaptcha(%q) = %v, want %v", c.html, got, c.want)
		}
	}
}

func TestWebSearch_InputValidation(t *testing.T) {
	ctx := context.Background()
	// 空查询直接报错(不发网络请求)
	if _, err := webSearch(ctx, "   ", 8); err == nil || !strings.Contains(err.Error(), "不能为空") {
		t.Fatalf("empty query should fail fast, got %v", err)
	}

	// 超长查询被截断而非拒绝(不触发网络请求无法在此验证截断,仅验证不 panic)
	long := strings.Repeat("词", 300)
	if _, err := webSearch(ctx, long, 0); err == nil {
		t.Log("network available: search succeeded")
	} else if !strings.Contains(err.Error(), "搜索") && !strings.Contains(err.Error(), "网络") {
		t.Logf("expected network/env error, got: %v", err)
	}
}

func TestAgentConfig_WebSearchDefaultOn(t *testing.T) {
	withTestDataDir(t)
	cfg := &ConfigService{}
	cfg.Init()
	// 默认配置(无 agent.toml 用户覆盖)下联网搜索自动打开
	if !cfg.AgentCfg().WebSearch {
		t.Fatal("web search should default to enabled")
	}
}

func TestAgentConfig_LegacyFileKeepsWebSearchOn(t *testing.T) {
	dir := withTestDataDir(t)
	// 模拟旧版 agent.toml: 不含 webSearch 键(升级用户的真实场景)
	legacy := "[agent]\nenabled = true\npermMode = \"manual\"\nmaxSteps = 24\n"
	if err := os.WriteFile(filepath.Join(dir, "agent.toml"), []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &ConfigService{}
	cfg.Init()
	if !cfg.AgentCfg().WebSearch {
		t.Fatal("legacy config without webSearch key must keep default (enabled)")
	}
}

func TestAgentConfig_ExplicitWebSearchOffRespected(t *testing.T) {
	dir := withTestDataDir(t)
	// 用户显式关闭必须被尊重
	content := "[agent]\nwebSearch = false\n"
	if err := os.WriteFile(filepath.Join(dir, "agent.toml"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &ConfigService{}
	cfg.Init()
	if cfg.AgentCfg().WebSearch {
		t.Fatal("explicit webSearch=false must be respected")
	}
}

func TestFindBrowserBinary(t *testing.T) {
	// 不强制要求本机有浏览器:有则返回存在的路径,无则空串(引擎报明确错误)
	bin := findBrowserBinary()
	if bin != "" {
		if _, err := os.Stat(bin); err != nil {
			t.Fatalf("returned path does not exist: %q", bin)
		}
	}
}

func TestWebSearch_FallbackChainOrder(t *testing.T) {
	// 注入 mock 引擎验证回退语义: 前一个失败/为空 → 尝试下一个;全败 → 汇总错误
	orig := searchEngines
	defer func() { searchEngines = orig }()

	failEngine := func(ctx context.Context, q string, n int) ([]webSearchResult, error) {
		return nil, fmt.Errorf("engine down")
	}

	searchEngines = []searchEngine{
		{name: "fail", fn: failEngine},
		{name: "empty", fn: func(ctx context.Context, q string, n int) ([]webSearchResult, error) {
			return []webSearchResult{}, nil
		}},
		{name: "good", fn: func(ctx context.Context, q string, n int) ([]webSearchResult, error) {
			return []webSearchResult{{Title: "hit", URL: "https://x", Snippet: "s"}}, nil
		}},
	}
	results, err := webSearch(context.Background(), "test", 5)
	if err != nil || len(results) != 1 || results[0].Title != "hit" {
		t.Fatalf("fallback should reach good engine, got %v %v", results, err)
	}

	searchEngines = []searchEngine{
		{name: "fail-a", fn: failEngine},
		{name: "fail-b", fn: failEngine},
	}
	if _, err := webSearch(context.Background(), "test", 5); err == nil || !strings.Contains(err.Error(), "全部搜索引擎均失败") {
		t.Fatalf("all engines failing should produce aggregate error, got %v", err)
	}
}
