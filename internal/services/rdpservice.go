package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"changeme/internal/services/wsbridge"

	"github.com/pelletier/go-toml/v2"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// RdpConnection RDP 连接信息(供前端 IronRDP 客户端使用)。
// Password/AuthToken 为明文,仅在内存中经 WS 返回前端,不落盘。
// AuthToken 同时用于 IronRDP 的 withAuthToken 与桥接令牌校验(两者必须一致)。
type RdpConnection struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	AuthToken   string `json:"authToken"`
	BridgeWsURL string `json:"bridgeWsUrl"`
}

// RdpTestServer RDP 测试服务器(来自 gitignored 的 testservers.json)。
type RdpTestServer struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// bridgeTok 记录 RDP 桥接令牌绑定的目标地址与过期时间。
// target 由后端在生成令牌时确定,桥接时绝不信任客户端 URL/PDU 中的目标参数(防 SSRF)。
type bridgeTok struct {
	target string
	expiry time.Time
}

// RdpService 提供 RDP 图形会话的桥接连接信息。
// 桥仅绑定 127.0.0.1,每会话使用一次性随机 token 防本机其他进程蹭用。
type RdpService struct {
	app          *application.App
	sessionFiles *SessionFileService
	mu           sync.RWMutex
	bridgeBase   string
	tokens       map[string]bridgeTok // token -> 绑定的目标与过期时间
	httpSrv      *http.Server
}

func (r *RdpService) SetApp(app *application.App) {
	r.app = app
}

func (r *RdpService) SetSessionFiles(sf *SessionFileService) {
	r.sessionFiles = sf
}

// Start 启动本机 WebSocket 字节桥,返回基础 URL。
func (r *RdpService) Start() (string, error) {
	base, srv, err := wsbridge.Start(r.validToken)
	if err != nil {
		return "", err
	}
	r.mu.Lock()
	if r.httpSrv != nil {
		_ = r.httpSrv.Shutdown(context.Background())
	}
	r.bridgeBase = base
	r.httpSrv = srv
	r.tokens = map[string]bridgeTok{}
	r.mu.Unlock()
	return base, nil
}

// Stop 关闭本机 RDP 桥 HTTP 服务,释放监听端口。
func (r *RdpService) Stop() {
	r.mu.Lock()
	srv := r.httpSrv
	r.httpSrv = nil
	r.mu.Unlock()
	if srv != nil {
		_ = srv.Shutdown(context.Background())
	}
}

// validToken 校验令牌并返回其绑定的目标地址。
// 返回 ("", false) 表示令牌无效或已过期;过期项在此惰性删除,避免 map 无限增长。
func (r *RdpService) validToken(token string) (string, bool) {
	r.mu.RLock()
	bt, ok := r.tokens[token]
	r.mu.RUnlock()
	if !ok {
		return "", false
	}
	if time.Now().After(bt.expiry) {
		r.mu.Lock()
		delete(r.tokens, token)
		r.mu.Unlock()
		return "", false
	}
	return bt.target, true
}

// GetRdpConnection 根据会话路径返回 RDP 连接信息(含解密后的密码与桥接 WS 地址)。
func (r *RdpService) GetRdpConnection(sessionPath string) (string, error) {
	conn, err := r.buildRdpConnection(sessionPath)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(conn)
	if err != nil {
		return "", fmt.Errorf("序列化 RDP 连接信息失败: %w", err)
	}
	return string(data), nil
}

func (r *RdpService) buildRdpConnection(sessionPath string) (*RdpConnection, error) {
	if r.sessionFiles == nil {
		return nil, fmt.Errorf("会话服务不可用")
	}
	r.mu.RLock()
	base := r.bridgeBase
	r.mu.RUnlock()
	if base == "" {
		return nil, fmt.Errorf("RDP 桥未启动")
	}

	fullPath, err := r.sessionFiles.safeSessionPath(sessionPath)
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, err
	}
	var data SessionFileData
	if err := toml.Unmarshal(content, &data); err != nil {
		return nil, err
	}
	info := data.Session
	if info.Protocol != "rdp" {
		return nil, fmt.Errorf("会话 %s 不是 RDP 会话(protocol=%s)", info.Name, info.Protocol)
	}
	if info.Port <= 0 || info.Port > 65535 {
		return nil, fmt.Errorf("无效的 RDP 端口: %d", info.Port)
	}

	password := ""
	if info.Password != "" {
		password, err = r.sessionFiles.decrypt(info.Password)
		if err != nil {
			return nil, fmt.Errorf("本地保存的密码无法解密,请在连接对话框中重新输入密码")
		}
	}

	token, err := newBridgeToken()
	if err != nil {
		return nil, fmt.Errorf("生成桥接令牌失败: %w", err)
	}
	target := net.JoinHostPort(info.Host, fmt.Sprintf("%d", info.Port))
	r.mu.Lock()
	r.tokens[token] = bridgeTok{target: target, expiry: time.Now().Add(10 * time.Minute)}
	r.mu.Unlock()

	return &RdpConnection{
		Host:        info.Host,
		Port:        info.Port,
		Username:    info.Username,
		Password:    password,
		AuthToken:   token,
		BridgeWsURL: bridgeWsURL(base, token, target),
	}, nil
}

// ReleaseRdpConnection 释放会话桥接令牌,使已生成的桥接地址失效。
func (r *RdpService) ReleaseRdpConnection(token string) {
	r.mu.Lock()
	delete(r.tokens, token)
	r.mu.Unlock()
}

// bridgeWsURL 组装桥接 WebSocket 地址(rdp=1 表示走 RDCleanPath 代理握手)。
// target 经过 url.QueryEscape,避免 IPv6 等含特殊字符的地址解析出错。
func bridgeWsURL(base, token, target string) string {
	return strings.Replace(base, "http://", "ws://", 1) + "/bridge?rdp=1&token=" + token + "&target=" + url.QueryEscape(target)
}

// newBridgeToken 生成 32 字节随机十六进制令牌。
func newBridgeToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// GetRdpTestServers 返回 testservers.json 中配置的 RDP 测试服务器列表(JSON 数组)。
// 配置文件不入库,缺失或未配置时返回空数组,由前端展示空下拉。
func (r *RdpService) GetRdpTestServers() string {
	servers := r.loadRdpTestServers()
	if len(servers) == 0 {
		return "[]"
	}
	data, err := json.Marshal(servers)
	if err != nil {
		return "[]"
	}
	return string(data)
}

// testServersJSONPath 定位 testservers.json:开发期位于项目内 internal/services/,
// 打包后回退到可执行文件同目录。
func testServersJSONPath() string {
	candidates := []string{}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "internal", "services", "testservers.json"))
	}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates, filepath.Join(exeDir, "testservers.json"))
		candidates = append(candidates, filepath.Join(exeDir, "internal", "services", "testservers.json"))
	}
	for _, p := range candidates {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

// testServersConfig 测试服务器配置文件的完整结构(SSH/Telnet 供外部集成测试复用)。
type testServersConfig struct {
	SSH struct {
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password"`
	} `json:"ssh"`
	Telnet struct {
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password"`
	} `json:"telnet"`
	RDP []RdpTestServer `json:"rdp"`
}

func (r *RdpService) loadRdpTestServers() []RdpTestServer {
	path := testServersJSONPath()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cfg testServersConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	servers := make([]RdpTestServer, 0, len(cfg.RDP))
	for _, s := range cfg.RDP {
		if s.Host == "" || s.Port <= 0 {
			continue
		}
		servers = append(servers, s)
	}
	return servers
}
