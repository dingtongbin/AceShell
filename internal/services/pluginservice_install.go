package services

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
		CollectError("plugin-install", "github:"+owner+"/"+repo, err)
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

	// SHA256 强校验: release 附带 checksums 文件时按其核对下载内容,
	// 防止下载被劫持/截断; 找不到对应条目视为校验失败(拒绝安装)。
	if csAsset := pickChecksumAsset(rel.Assets); csAsset != nil {
		checksums, err := fetchChecksums(ctx, csAsset.BrowserDownloadURL)
		if err != nil {
			return "", "", fmt.Errorf("获取校验文件失败: %w", err)
		}
		want := matchChecksum(checksums, asset.Name)
		if want == "" {
			return "", "", fmt.Errorf("checksums 中没有 %s 的哈希, 拒绝安装", asset.Name)
		}
		got, err := fileSHA256(tmpZipPath)
		if err != nil {
			return "", "", fmt.Errorf("计算 SHA256 失败: %w", err)
		}
		if !strings.EqualFold(got, want) {
			return "", "", fmt.Errorf("SHA256 校验失败: 期望 %s, 实际 %s", want, got)
		}
	} else {
		s.logLine("GitHub 安装: release 未提供 checksums 文件, 跳过哈希校验 (" + asset.Name + ")")
	}

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

	// 停旧实例解 exe 文件锁 → 原子换目录 → 按配置启动
	s.mu.Lock()
	old := s.instances[mf.ID]
	s.mu.Unlock()
	if old != nil {
		s.stopInstance(old)
	}
	finalDir := filepath.Join(PluginsDir(), mf.ID)
	// 原子替换(旧版本先留档, 失败可回滚; 内部等待 exe 文件锁释放)。
	// 原先是 RemoveAll → Rename, Rename 失败会让插件彻底消失且无法回滚。
	if err := swapPluginDir(mf.ID, root); err != nil {
		return "", "", err
	}
	// root 可能是包裹目录内的子路径, 落盘后统一以 finalDir 重扫
	cand := pluginCandidate{id: mf.ID, dir: finalDir,
		exePath: filepath.Join(finalDir, exeName), manifest: *mf}
	if !s.cfg.PluginEnabled(mf.ID) {
		s.mu.Lock()
		s.instances[mf.ID] = &pluginInstance{
			id: cand.id, dir: cand.dir, exePath: cand.exePath,
			manifest: cand.manifest, status: pluginStatusDisabled,
			done: make(chan struct{}), startedAt: time.Now(),
		}
		s.mu.Unlock()
		return mf.ID, firstNonEmpty(mf.Version, rel.TagName), nil
	}
	if err := s.launch(cand); err != nil {
		return "", "", fmt.Errorf("安装成功但启动失败: %w", err)
	}
	s.emitInvalidated(mf.ID, "update")
	return mf.ID, firstNonEmpty(mf.Version, rel.TagName), nil
}

// PluginInstallZip 从本地 zip 包安装(或更新)插件。
// 包结构与 GitHub 资产一致: plugin.json + <id> 可执行文件 (+ dist/ docs/)。
// zipPath 为空串视为用户取消。返回 JSON: {ok,id,version} 或 {error}。
func (s *PluginService) PluginInstallZip(zipPath string) string {
	zipPath = strings.TrimSpace(zipPath)
	if zipPath == "" {
		return `{"error":"未选择插件包"}`
	}
	if st, err := os.Stat(zipPath); err != nil || st.IsDir() {
		return `{"error":"文件不存在: ` + mustJSONString(zipPath) + `"}`
	}
	if !strings.HasSuffix(strings.ToLower(zipPath), ".zip") {
		return `{"error":"仅支持 .zip 插件包"}`
	}
	id, version, err := s.installFromZip(zipPath)
	if err != nil {
		s.logLine("本地安装失败: " + err.Error())
		return `{"error":` + mustJSONString(err.Error()) + `}`
	}
	s.emitRegistry()
	return mustJSON(map[string]any{"ok": true, "id": id, "version": version})
}

// installFromZip 本地包安装主体: 复用 GitHub 安装的 解压→定位→校验→原子换目录→启动 管线。
func (s *PluginService) installFromZip(zipPath string) (string, string, error) {
	// 解压到 PluginsDir 同卷临时目录(保证 Rename 原子性)
	_ = os.MkdirAll(PluginsDir(), 0700)
	tmpDir, err := os.MkdirTemp(PluginsDir(), ".install-")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(tmpDir)
	if err := unzipTo(zipPath, tmpDir); err != nil {
		return "", "", fmt.Errorf("解压失败: %w", err)
	}
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

	s.mu.Lock()
	old := s.instances[mf.ID]
	s.mu.Unlock()
	if old != nil {
		s.stopInstance(old)
	}
	finalDir := filepath.Join(PluginsDir(), mf.ID)
	if err := swapPluginDir(mf.ID, root); err != nil {
		return "", "", err
	}
	cand := pluginCandidate{id: mf.ID, dir: finalDir,
		exePath: filepath.Join(finalDir, exeName), manifest: *mf}
	if !s.cfg.PluginEnabled(mf.ID) {
		s.mu.Lock()
		s.instances[mf.ID] = &pluginInstance{
			id: cand.id, dir: cand.dir, exePath: cand.exePath,
			manifest: cand.manifest, status: pluginStatusDisabled,
			done: make(chan struct{}), startedAt: time.Now(),
		}
		s.mu.Unlock()
		return mf.ID, mf.Version, nil
	}
	if err := s.launch(cand); err != nil {
		return "", "", fmt.Errorf("安装成功但启动失败: %w", err)
	}
	s.emitInvalidated(mf.ID, "update")
	return mf.ID, mf.Version, nil
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

// ==================== SHA256 校验 ====================
// release 提供 checksums 文件时强制校验下载的 zip; 未提供时降级记日志放行。
// 校验文件格式为 sha256sum 输出: "<64位hex>  <文件名>" 每行一条。

// pickChecksumAsset 挑选 release 资产中的校验文件。匹配名称含 "checksum"
// 或 "sha256sum" 的资产(大小写不敏感, 覆盖 checksums.txt / SHA256SUMS 等常见命名)。
func pickChecksumAsset(assets []ghAsset) *ghAsset {
	for i := range assets {
		n := strings.ToLower(assets[i].Name)
		if strings.Contains(n, "checksum") || strings.Contains(n, "sha256sum") {
			return &assets[i]
		}
	}
	return nil
}

// fetchChecksums 下载并解析校验文件为 {文件名: hex哈希}。
func fetchChecksums(ctx context.Context, rawURL string) (map[string]string, error) {
	cctx, cancel := context.WithTimeout(ctx, ghAPITimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "AceShell/"+AppVersion)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载校验文件返回 %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 2 && len(fields[0]) == 64 {
			out[filepath.Base(fields[1])] = fields[0]
		}
	}
	return out, nil
}

// matchChecksum 查文件名对应的哈希(fetchChecksums 已按 base 名归一)。
func matchChecksum(checksums map[string]string, name string) string {
	return checksums[filepath.Base(name)]
}

// fileSHA256 计算文件 SHA256 的十六进制摘要。
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
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
// 预发布号(如 1.2.3-beta)在截断后按 1.2.3 参与 —— 数字段比较无法理解
// "beta < 正式版", 带着尾巴比只会产生随机结果。
func compareVersion(a, b string) int {
	as := strings.Split(trimVersionSuffix(a), ".")
	bs := strings.Split(trimVersionSuffix(b), ".")
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

// trimVersionSuffix 去掉 v 前缀并截断预发布号: "v1.2.3-beta.2" → "1.2.3"。
func trimVersionSuffix(v string) string {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.Index(v, "-"); i >= 0 {
		v = v[:i]
	}
	return v
}
