package services

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// rod 渲染引擎: 驱动系统已装 Edge/Chrome 真实渲染必应搜索页。
// 用途: HTTP 抓取被人机验证拦截时的降级路径——真实浏览器指纹可通过验证。
//
// 资源策略:
//   - 浏览器实例进程内复用(sync.Once 懒加载),避免每次搜索冷启动
//   - headless 模式,无窗口闪现
//   - 查找顺序: Edge(系统必装) → Chrome;都找不到则明确报错

var (
	rodMu           sync.Mutex
	rodBrowser      *rod.Browser
	rodBrowserErr   error
	rodBrowserReady bool
)

// findBrowserBinary 定位系统已装的浏览器可执行文件(Edge 优先,与 WebView2 同内核)。
func findBrowserBinary() string {
	var candidates []string
	if pf86 := os.Getenv("ProgramFiles(x86)"); pf86 != "" {
		candidates = append(candidates,
			filepath.Join(pf86, "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(pf86, "Google", "Chrome", "Application", "chrome.exe"))
	}
	if pf := os.Getenv("ProgramFiles"); pf != "" {
		candidates = append(candidates,
			filepath.Join(pf, "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(pf, "Google", "Chrome", "Application", "chrome.exe"))
	}
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		candidates = append(candidates,
			filepath.Join(la, "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(la, "Google", "Chrome", "Application", "chrome.exe"))
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

// getRodBrowser 获取(或启动)进程内共享的无头浏览器实例。
func getRodBrowser() (*rod.Browser, error) {
	rodMu.Lock()
	defer rodMu.Unlock()
	if rodBrowserReady {
		return rodBrowser, rodBrowserErr
	}
	rodBrowserReady = true

	bin := findBrowserBinary()
	if bin == "" {
		rodBrowserErr = fmt.Errorf("未找到系统已装的 Edge/Chrome")
		return nil, rodBrowserErr
	}
	wsURL, err := launcher.New().Bin(bin).Headless(true).Set("disable-gpu").Launch()
	if err != nil {
		rodBrowserErr = fmt.Errorf("启动 %s 失败: %w", filepath.Base(bin), err)
		return nil, rodBrowserErr
	}
	b := rod.New().ControlURL(wsURL)
	if err := b.Connect(); err != nil {
		rodBrowserErr = fmt.Errorf("连接浏览器失败: %w", err)
		return nil, rodBrowserErr
	}
	rodBrowser = b
	return rodBrowser, nil
}

// searchBingRod 通过真实浏览器渲染必应搜索页并提取结果。
func searchBingRod(ctx context.Context, query string, maxResults int) ([]webSearchResult, error) {
	browser, err := getRodBrowser()
	if err != nil {
		return nil, err
	}

	searchURL := bingSearchEndpoint + url.QueryEscape(query)
	page, err := browser.Page(proto.TargetCreateTarget{URL: searchURL})
	if err != nil {
		return nil, fmt.Errorf("打开页面失败: %w", err)
	}
	defer page.Close()

	// 外层 ctx 取消联动 + 单次渲染限时
	page = page.Context(ctx).Timeout(rodPageTimeout)

	if err := page.WaitElementsMoreThan("li.b_algo", 0); err != nil {
		return nil, fmt.Errorf("等待结果超时(可能被人机验证拦截): %w", err)
	}

	content, err := page.HTML()
	if err != nil {
		return nil, fmt.Errorf("获取页面内容失败: %w", err)
	}
	results := parseBingHtml(content)
	if len(results) == 0 {
		return nil, fmt.Errorf("渲染后仍未解析到结果")
	}
	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return results, nil
}
