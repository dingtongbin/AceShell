package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/grpc/metadata"

	pluginsdk "changeme/pluginsdk"
)

func TestParseGitHubTarget(t *testing.T) {
	cases := []struct {
		in                       string
		owner, repo, tag         string
		wantErr                  bool
	}{
		{"dingtongbin/aceshell-plugin-hello", "dingtongbin", "aceshell-plugin-hello", "", false},
		{"dingtongbin/hello@v1.2.3", "dingtongbin", "hello", "v1.2.3", false},
		{"https://github.com/o/r/releases/latest", "o", "r", "", false},
		{"https://github.com/o/r/releases/tag/v0.1.0", "o", "r", "v0.1.0", false},
		{"github.com/o/r", "o", "r", "", false},
		{"https://github.com/o/r/releases/tag/v0.1.0/", "o", "r", "v0.1.0", false},
		{"", "", "", "", true},
		{"o/r/extra", "", "", "", true},
		{"o", "", "", "", true},
	}
	for _, c := range cases {
		owner, repo, tag, err := parseGitHubTarget(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseGitHubTarget(%q) 应报错", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseGitHubTarget(%q) 意外错误: %v", c.in, err)
			continue
		}
		if owner != c.owner || repo != c.repo || tag != c.tag {
			t.Errorf("parseGitHubTarget(%q) = %q/%q/%q, 期望 %q/%q/%q", c.in, owner, repo, tag, c.owner, c.repo, c.tag)
		}
	}
}

func TestPickPlatformAsset(t *testing.T) {
	assets := []ghAsset{
		{Name: "aceshell-hello-linux-arm64.zip"},
		{Name: "checksums.txt"},
		{Name: "aceshell-hello-windows-amd64.zip"},
		{Name: "hello-windows-amd64.zip"},
	}
	got := pickPlatformAsset(assets, "windows", "amd64")
	if got == nil || got.Name != "aceshell-hello-windows-amd64.zip" {
		t.Fatalf("约定命名资产应优先: %+v", got)
	}
	// 无约定前缀时回落包含平台标识者
	assets2 := []ghAsset{{Name: "hello-windows-amd64.zip"}, {Name: "hello-linux-amd64.zip"}}
	if got := pickPlatformAsset(assets2, "windows", "amd64"); got == nil || !strings.Contains(got.Name, "windows-amd64") {
		t.Fatalf("回落匹配失败: %+v", got)
	}
	// 唯一 zip 兜底
	assets3 := []ghAsset{{Name: "whatever.zip"}}
	if got := pickPlatformAsset(assets3, "windows", "amd64"); got == nil {
		t.Fatal("唯一 zip 应回落命中")
	}
	// 平台不符且多 zip → 不选
	assets4 := []ghAsset{{Name: "hello-linux-amd64.zip"}, {Name: "hello-darwin-arm64.zip"}}
	if got := pickPlatformAsset(assets4, "windows", "amd64"); got != nil {
		t.Fatalf("平台不符不应命中: %+v", got)
	}
}

func TestCompareVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.2.4", "0.2.4", 0},
		{"0.2.5", "0.2.4", 1},
		{"0.2.3", "0.2.4", -1},
		{"v1.0.0", "0.9.9", 1},
		{"0.2", "0.2.0", 0},
	}
	for _, c := range cases {
		if got := compareVersion(c.a, c.b); got != c.want {
			t.Errorf("compareVersion(%q,%q) = %d, 期望 %d", c.a, c.b, got, c.want)
		}
	}
}

func TestPluginScanAndConfigGate(t *testing.T) {
	restore := SetDataDir(t.TempDir())
	defer restore()

	// 两个插件: hello(启用) 与 bye(禁用) 与一个无效目录(缺清单)
	helloDir := filepath.Join(PluginsDir(), "hello")
	_ = os.MkdirAll(helloDir, 0700)
	writeFile := func(p, content string) {
		if err := os.WriteFile(p, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(filepath.Join(helloDir, "plugin.json"), `{"id":"hello","name":"Hello","version":"0.1.0"}`)
	writeFile(filepath.Join(helloDir, "hello"+exeSuffix()), "fake")
	byeDir := filepath.Join(PluginsDir(), "bye")
	_ = os.MkdirAll(byeDir, 0700)
	writeFile(filepath.Join(byeDir, "plugin.json"), `{"id":"bye","name":"Bye","version":"0.1.0"}`)
	writeFile(filepath.Join(byeDir, "bye"+exeSuffix()), "fake")
	_ = os.MkdirAll(filepath.Join(PluginsDir(), "Incomplete"), 0700)

	cfg := &ConfigService{}
	cfg.Init()
	if !cfg.PluginEnabled("hello") {
		t.Fatal("未显式禁用的插件应默认启用")
	}
	_ = cfg.SetPluginEnabled("bye", false)
	if cfg.PluginEnabled("bye") {
		t.Fatal("显式禁用未生效")
	}

	svc := NewPluginService(cfg, &SessionFileService{}, "")
	candidates := svc.scanPluginsDir()
	if len(candidates) != 2 {
		t.Fatalf("应扫描出 2 个插件, 得到 %d", len(candidates))
	}
	byID := map[string]pluginCandidate{}
	for _, c := range candidates {
		byID[c.id] = c
	}
	hello, ok := byID["hello"]
	if !ok || hello.exePath == "" {
		t.Fatalf("hello 候选无效: %+v", hello)
	}
}

func TestPluginAssetHandler(t *testing.T) {
	restore := SetDataDir(t.TempDir())
	defer restore()
	dir := filepath.Join(PluginsDir(), "hello", "dist")
	_ = os.MkdirAll(dir, 0700)
	if err := os.WriteFile(filepath.Join(dir, "entry.js"), []byte("export default {}"), 0600); err != nil {
		t.Fatal(err)
	}

	svc := NewPluginService(&ConfigService{}, &SessionFileService{}, "")
	handler := svc.AssetHandler()

	do := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	rec := do("/plugins/hello/dist/entry.js")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "export default") {
		t.Fatalf("正常资产获取失败: code=%d body=%q", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Fatalf("应禁用缓存, 得到 %q", cc)
	}
	if rec := do("/plugins/hello/dist"); rec.Code != http.StatusNotFound {
		t.Fatalf("目录访问应 404, 得到 %d", rec.Code)
	}
	if rec := do("/plugins/../config.toml"); rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "tokenEnc") {
		t.Fatal("路径穿越防护失效")
	}
	if rec := do("/plugins/hello/../../config.toml"); rec.Code != http.StatusNotFound {
		t.Fatalf("穿越路径应 404, 得到 %d", rec.Code)
	}
}

func TestPluginTabIDDeterministic(t *testing.T) {
	if got := pluginTabID("hello", "demo"); got != "plugin://hello/demo" {
		t.Fatalf("pluginTabID = %q", got)
	}
}

func TestHostVueShimServed(t *testing.T) {
	svc := NewPluginService(&ConfigService{}, &SessionFileService{}, "export const ref = V.ref\nexport default V\n")
	handler := svc.AssetHandler()

	do := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	rec := do("/plugins/_host/vue.js")
	if rec.Code != http.StatusOK {
		t.Fatalf("shim 获取失败: code=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "export const ref = V.ref") {
		t.Fatalf("shim 内容异常: %q", rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Fatalf("shim 应禁用缓存, 得到 %q", cc)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/javascript") {
		t.Fatalf("shim Content-Type 异常: %q", ct)
	}
	// 未注入生成产物时退回内置兜底清单
	svc2 := NewPluginService(&ConfigService{}, &SessionFileService{}, "")
	handler2 := svc2.AssetHandler()
	req2 := httptest.NewRequest(http.MethodGet, "/plugins/_host/vue.js", nil)
	rec3 := httptest.NewRecorder()
	handler2.ServeHTTP(rec3, req2)
	if rec3.Code != http.StatusOK || !strings.Contains(rec3.Body.String(), "export const ref = V.ref") {
		t.Fatalf("兜底 shim 未生效: code=%d body头=%q", rec3.Code, rec3.Body.String())
	}
}

func TestPluginManifestAndInfoRoundTrip(t *testing.T) {
	// manifest 反序列化
	var mf pluginManifest
	if err := json.Unmarshal([]byte(`{"id":"hello","name":"Hello","version":"0.1.0","minHost":"0.2.4"}`), &mf); err != nil {
		t.Fatal(err)
	}
	if mf.ID != "hello" || mf.MinHost != "0.2.4" {
		t.Fatalf("manifest 解析异常: %+v", mf)
	}
	// 注册表快照包含视图
	inst := &pluginInstance{
		id: "hello", manifest: mf, status: pluginStatusRunning,
		info: &pluginsdk.PluginInfo{
			DisplayName: "Hello 插件", Version: "0.9.0",
			Views: []pluginsdk.ViewInfo{{ID: "main", Title: "Hello 面板", ComponentID: "panel"}},
		},
	}
	svc := NewPluginService(&ConfigService{}, &SessionFileService{}, "")
	svc.instances = map[string]*pluginInstance{"hello": inst}
	svc.mu.Lock()
	snap := svc.snapshotLocked()
	svc.mu.Unlock()
	if len(snap) != 1 || snap[0].DisplayName != "Hello 插件" || len(snap[0].Views) != 1 || snap[0].Views[0].ComponentID != "panel" {
		t.Fatalf("注册表快照异常: %+v", snap)
	}
}

func TestHostServiceAuthInterceptor(t *testing.T) {
	svc := NewPluginService(&ConfigService{}, &SessionFileService{}, "")
	svc.tokens = map[string]string{"tok-1": "hello"}

	// 无令牌 → 拒绝
	if _, err := svc.authInterceptor(context.Background(), nil, nil, func(ctx context.Context, _ any) (any, error) {
		return nil, nil
	}); err == nil {
		t.Fatal("无令牌调用应被拒绝")
	}
	// 带令牌 → 通过并注入 caller
	if _, err := svc.authInterceptor(metadataCtx("tok-1"), nil, nil, func(ctx context.Context, _ any) (any, error) {
		if got := callerID(ctx); got != "hello" {
			t.Fatalf("callerID = %q", got)
		}
		return nil, nil
	}); err != nil {
		t.Fatalf("有效令牌应通过: %v", err)
	}
	// 错误令牌 → 拒绝
	if _, err := svc.authInterceptor(metadataCtx("bad"), nil, nil, func(ctx context.Context, _ any) (any, error) {
		return nil, nil
	}); err == nil {
		t.Fatal("无效令牌应被拒绝")
	}
}

func metadataCtx(token string) context.Context {
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-aceshell-token", token))
}

func TestRandomToken(t *testing.T) {
	a, err := randomToken()
	if err != nil || len(a) < 32 {
		t.Fatalf("randomToken 异常: %q %v", a, err)
	}
	b, _ := randomToken()
	if a == b {
		t.Fatal("随机令牌不应重复")
	}
}
