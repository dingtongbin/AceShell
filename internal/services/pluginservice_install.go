package services

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// 本文件: GitHub Release 安装器。
// 流程: 解析 owner/repo(@tag|releases 链接) → 拉取 release 元数据 → 匹配平台资产
// → 下载解压到临时目录 → 校验 plugin.json/可执行文件 → 停旧实例(解 exe 文件锁)
// → 落盘 <PluginsDir>/<id>/ → 按配置启动 → 推送注册表。

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

const (
	ghAPITimeout     = 20 * time.Second
	ghDownloadExpiry = 120 * time.Second
)

// PluginInstallFromGitHub 从 GitHub Release 安装(或更新)插件。
// input 支持: owner/repo | owner/repo@v1.2.3 | https://github.com/owner/repo/releases/latest
// | https://github.com/owner/repo/releases/tag/v1.2.3 。返回 JSON: {ok,id,version} 或 {error}。
func (s *PluginService) PluginInstallFromGitHub(input string) string {
	owner, repo, tag, err := parseGitHubTarget(input)
	if err != nil {
		return `{"error":"` + mustJSONString(err.Error()) + `"}`
	}
	id, version, err := s.installFromGitHub(owner, repo, tag)
	if err != nil {
		s.logLine("GitHub 安装失败: " + err.Error())
		return `{"error":` + mustJSONString(err.Error()) + `}`
	}
	s.emitRegistry()
	return mustJSON(map[string]any{"ok": true, "id": id, "version": version})
}

// installFromGitHub 安装主体;返回插件 ID 与版本。
func (s *PluginService) installFromGitHub(owner, repo, tag string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ghDownloadExpiry)
	defer cancel()

	rel, err := fetchGitHubRelease(ctx, owner, repo, tag)
	if err != nil {
		return "", "", err
	}
	asset := pickPlatformAsset(rel.Assets, runtime.GOOS, runtime.GOARCH)
	if asset == nil {
		return "", "", fmt.Errorf("release %s 无匹配 %s/%s 的 zip 资产(约定命名: aceshell-<id>-%s-%s.zip)",
			rel.TagName, runtime.GOOS, runtime.GOARCH, runtime.GOOS, runtime.GOARCH)
	}

	// 下载到临时文件
	tmpZip, err := os.CreateTemp("", "aceshell-plugin-*.zip")
	if err != nil {
		return "", "", err
	}
	tmpZipPath := tmpZip.Name()
	defer os.Remove(tmpZipPath)
	if err := downloadFile(ctx, tmpZip, asset.BrowserDownloadURL); err != nil {
		tmpZip.Close()
		return "", "", fmt.Errorf("下载失败: %w", err)
	}
	tmpZip.Close()

	// 解压到 PluginsDir 同卷临时目录(保证 Rename 原子性)
	_ = os.MkdirAll(PluginsDir(), 0700)
	tmpDir, err := os.MkdirTemp(PluginsDir(), ".install-")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(tmpDir)
	if err := unzipTo(tmpZipPath, tmpDir); err != nil {
		return "", "", fmt.Errorf("解压失败: %w", err)
	}

	// 定位 plugin.json(根目录或单一包裹目录)
	root, mf, err := locateExtractedPlugin(tmpDir)
	if err != nil {
		return "", "", err
	}
	exeName := mf.ID
	if runtime.GOOS == "windows" {
		exeName += ".exe"
	}
	if st, err := os.Stat(filepath.Join(root, exeName)); err != nil || st.IsDir() {
		return "", "", fmt.Errorf("包内缺少可执行文件 %s", exeName)
	}
	if mf.MinHost != "" && compareVersion(AppVersion, mf.MinHost) < 0 {
		return "", "", fmt.Errorf("插件 %s 需要宿主 >= %s, 当前 %s", mf.ID, mf.MinHost, AppVersion)
	}

	// 停旧实例解 exe 文件锁 → 换目录 → 按配置启动
	s.mu.Lock()
	old := s.instances[mf.ID]
	s.mu.Unlock()
	if old != nil {
		s.stopInstance(old)
	}
	finalDir := filepath.Join(PluginsDir(), mf.ID)
	if err := os.RemoveAll(finalDir); err != nil {
		return "", "", fmt.Errorf("清理旧版本失败: %w", err)
	}
	if err := os.Rename(root, finalDir); err != nil {
		return "", "", fmt.Errorf("安装落盘失败: %w", err)
	}
	// root 可能是包裹目录内的子路径, 落盘后统一以 finalDir 重扫
	cand := pluginCandidate{id: mf.ID, dir: finalDir,
		exePath: filepath.Join(finalDir, exeName), manifest: *mf}
	if !s.cfg.PluginEnabled(mf.ID) {
		s.mu.Lock()
		s.instances[mf.ID] = &pluginInstance{
			id: cand.id, dir: cand.dir, exePath: cand.exePath,
			manifest: cand.manifest, status: pluginStatusDisabled,
			done: make(chan struct{}),
		}
		s.mu.Unlock()
		return mf.ID, firstNonEmpty(mf.Version, rel.TagName), nil
	}
	if err := s.launch(cand); err != nil {
		return "", "", fmt.Errorf("安装成功但启动失败: %w", err)
	}
	return mf.ID, firstNonEmpty(mf.Version, rel.TagName), nil
}

// parseGitHubTarget 解析安装输入。
func parseGitHubTarget(input string) (owner, repo, tag string, err error) {
	v := strings.TrimSpace(input)
	v = strings.TrimPrefix(v, "https://github.com/")
	v = strings.TrimPrefix(v, "http://github.com/")
	v = strings.TrimPrefix(v, "github.com/")
	v = strings.Trim(v, "/")
	switch {
	case v == "":
		return "", "", "", errors.New("请输入 owner/repo 或 release 链接")
	case strings.HasSuffix(v, "/releases/latest"):
		v = strings.TrimSuffix(v, "/releases/latest")
	case strings.Contains(v, "/releases/tag/"):
		idx := strings.Index(v, "/releases/tag/")
		tag = strings.TrimPrefix(v[idx:], "/releases/tag/")
		tag = strings.Trim(tag, "/")
		v = v[:idx]
	}
	parts := strings.Split(v, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", "", fmt.Errorf("无法识别的输入: %s", input)
	}
	if tag == "" {
		// 支持 owner/repo@v1.2.3
		if i := strings.Index(parts[1], "@"); i > 0 {
			tag = parts[1][i+1:]
			parts[1] = parts[1][:i]
		}
	}
	return parts[0], parts[1], tag, nil
}

// fetchGitHubRelease 拉取 release 元数据(tag 为空取 latest)。
func fetchGitHubRelease(ctx context.Context, owner, repo, tag string) (*ghRelease, error) {
	api := "https://api.github.com/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/releases/"
	if tag == "" || tag == "latest" {
		api += "latest"
	} else {
		api += "tags/" + url.PathEscape(tag)
	}
	cctx, cancel := context.WithTimeout(ctx, ghAPITimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, api, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "AceShell/"+AppVersion)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API 访问失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("仓库或 release 不存在: %s/%s@%s", owner, repo, firstNonEmpty(tag, "latest"))
	}
	if resp.StatusCode == http.StatusForbidden {
		return nil, errors.New("GitHub API 限流(匿名 60 次/小时), 请稍后再试")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API 返回 %d", resp.StatusCode)
	}
	var rel ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&rel); err != nil {
		return nil, fmt.Errorf("release 数据解析失败: %w", err)
	}
	return &rel, nil
}

// pickPlatformAsset 按约定命名匹配平台资产: aceshell-<id>-<goos>-<goarch>.zip。
// 匹配优先级: 约定全匹配 > 含 <goos>-<goarch> 的 zip > 唯一 zip。
func pickPlatformAsset(assets []ghAsset, goos, goarch string) *ghAsset {
	var zips []*ghAsset
	for i := range assets {
		if strings.HasSuffix(strings.ToLower(assets[i].Name), ".zip") {
			zips = append(zips, &assets[i])
		}
	}
	if len(zips) == 0 {
		return nil
	}
	plat := strings.ToLower(goos + "-" + goarch)
	for _, a := range zips {
		n := strings.ToLower(a.Name)
		if strings.HasPrefix(n, "aceshell-") && strings.Contains(n, plat) {
			return a
		}
	}
	for _, a := range zips {
		if strings.Contains(strings.ToLower(a.Name), plat) {
			return a
		}
	}
	if len(zips) == 1 {
		return zips[0]
	}
	return nil
}

// downloadFile 流式下载(走系统代理默认配置)。
func downloadFile(ctx context.Context, f *os.File, rawURL string) error {
	cctx, cancel := context.WithTimeout(ctx, ghDownloadExpiry)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "AceShell/"+AppVersion)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载返回 %d", resp.StatusCode)
	}
	_, err = io.Copy(f, io.LimitReader(resp.Body, 512<<20))
	return err
}

// unzipTo 解压 zip 到目标目录(防路径穿越)。
func unzipTo(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		name := filepath.Clean(f.Name)
		if strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			return fmt.Errorf("zip 内含非法路径: %s", f.Name)
		}
		target := filepath.Join(destDir, name)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0700); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, io.LimitReader(rc, 512<<20))
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// locateExtractedPlugin 在解压目录中定位插件根(plugin.json 所在)。
func locateExtractedPlugin(dir string) (string, *pluginManifest, error) {
	if mf, err := readManifest(filepath.Join(dir, "plugin.json")); err == nil {
		return dir, mf, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", nil, err
	}
	subdirs := 0
	var only string
	for _, e := range entries {
		if e.IsDir() {
			subdirs++
			only = e.Name()
		}
	}
	if subdirs == 1 {
		p := filepath.Join(dir, only)
		if mf, err := readManifest(filepath.Join(p, "plugin.json")); err == nil {
			return p, mf, nil
		}
	}
	return "", nil, errors.New("包内缺少 plugin.json")
}

func readManifest(path string) (*pluginManifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var mf pluginManifest
	if json.Unmarshal(raw, &mf) != nil || !pluginIDRe.MatchString(mf.ID) {
		return nil, fmt.Errorf("plugin.json 无效")
	}
	return &mf, nil
}

// compareVersion 语义化版本比较(仅数字段): a<b → -1, a>b → 1, 相等 → 0。
func compareVersion(a, b string) int {
	as := strings.Split(strings.TrimPrefix(a, "v"), ".")
	bs := strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var ai, bi int
		if i < len(as) {
			fmt.Sscanf(as[i], "%d", &ai)
		}
		if i < len(bs) {
			fmt.Sscanf(bs[i], "%d", &bi)
		}
		if ai != bi {
			if ai < bi {
				return -1
			}
			return 1
		}
	}
	return 0
}
