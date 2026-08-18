package services

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestBrowserService_ScanBrowsers(t *testing.T) {
	svc := &BrowserService{}
	raw := svc.ScanBrowsers()
	var list []BrowserInfo
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		t.Fatalf("ScanBrowsers 返回非法 JSON: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("浏览器列表为空")
	}
	hasDefault := false
	for _, b := range list {
		if b.ID == "default" {
			hasDefault = true
			if !b.IsDefault {
				t.Fatal("default 条目必须标记 IsDefault")
			}
		}
		if b.ID == "" {
			t.Fatalf("浏览器条目缺少 ID: %+v", b)
		}
	}
	if !hasDefault {
		t.Fatal("浏览器列表缺少 default 条目")
	}
}

func TestBrowserService_OpenUrl_InvalidURL(t *testing.T) {
	svc := &BrowserService{}
	if msg := svc.OpenUrl("default", "not-a-url"); msg == "" || !strings.Contains(msg, "http://") {
		t.Fatalf("非法 URL 应返回格式提示, got: %q", msg)
	}
}

func TestBrowserService_OpenUrl_BrowserNotFound(t *testing.T) {
	svc := &BrowserService{}
	oldScan := scanBrowsersFn
	scanBrowsersFn = func() []BrowserInfo {
		return []BrowserInfo{{ID: "default", Name: "默认浏览器", IsDefault: true}}
	}
	defer func() { scanBrowsersFn = oldScan }()

	if msg := svc.OpenUrl("chrome", "https://example.com"); msg == "" || !strings.Contains(msg, "浏览器不存在") {
		t.Fatalf("不存在的浏览器应返回提示, got: %q", msg)
	}
}

func TestBrowserService_OpenUrl_Success(t *testing.T) {
	svc := &BrowserService{}
	oldScan := scanBrowsersFn
	oldOpen := openUrlImpl
	var gotID, gotURL string
	scanBrowsersFn = func() []BrowserInfo {
		return []BrowserInfo{
			{ID: "default", Name: "默认浏览器", IsDefault: true},
			{ID: "chrome", Name: "Google Chrome", ExecPath: "C:\\chrome.exe"},
		}
	}
	openUrlImpl = func(target BrowserInfo, url string) error {
		gotID, gotURL = target.ID, url
		return nil
	}
	defer func() { scanBrowsersFn, openUrlImpl = oldScan, oldOpen }()

	if msg := svc.OpenUrl("chrome", "https://example.com"); msg != "" {
		t.Fatalf("成功打开不应返回错误, got: %q", msg)
	}
	if gotID != "chrome" || gotURL != "https://example.com" {
		t.Fatalf("打开参数错误: id=%q url=%q", gotID, gotURL)
	}

	// 空 ID 应落到 default
	if msg := svc.OpenUrl("", "http://example.com"); msg != "" {
		t.Fatalf("空 ID 打开默认浏览器失败: %q", msg)
	}
	if gotID != "default" {
		t.Fatalf("空 ID 应打开 default, got %q", gotID)
	}
}

func TestBrowserService_OpenUrl_OpenFailure(t *testing.T) {
	svc := &BrowserService{}
	oldScan := scanBrowsersFn
	oldOpen := openUrlImpl
	scanBrowsersFn = func() []BrowserInfo {
		return []BrowserInfo{{ID: "default", Name: "默认浏览器", IsDefault: true}}
	}
	openUrlImpl = func(target BrowserInfo, url string) error {
		return errors.New("启动失败")
	}
	defer func() { scanBrowsersFn, openUrlImpl = oldScan, oldOpen }()

	if msg := svc.OpenUrl("default", "https://example.com"); msg == "" || !strings.Contains(msg, "打开浏览器失败") {
		t.Fatalf("启动失败应返回可读错误, got: %q", msg)
	}
}

func TestNormalizeBrowserID(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Google Chrome", "google-chrome"},
		{"Mozilla Firefox", "mozilla-firefox"},
		{"微软浏览器", ""},
	}
	for _, c := range cases {
		if got := normalizeBrowserID(c.in); got != c.want {
			t.Fatalf("normalizeBrowserID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
