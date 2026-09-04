package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"changeme/internal/services/wsbridge"

	"github.com/pelletier/go-toml/v2"
)

// VncConnection VNC 连接信息(供前端 noVNC 客户端使用)。
// Password 为明文,仅在内存中经 WS 返回前端,不落盘。
// AuthToken 用于桥接令牌校验;桥接走 wsbridge 普通透传路径(非 RDP 握手)。
type VncConnection struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	AuthToken   string `json:"authToken"`
	BridgeWsURL string `json:"bridgeWsUrl"`
}

// bridgeTok 记录 VNC 桥接令牌绑定的目标地址与过期时间。
// target 由后端在生成令牌时确定,桥接时绝不信任客户端 URL 中的目标参数(防 SSRF)。
type vncBridgeTok struct {
	target string
	expiry time.Time
}

// VncService 提供 VNC 图形会话的桥接连接信息。
// 与 RdpService 平行:桥仅绑定 127.0.0.1,每会话使用一次性随机 token
// 防本机其他进程蹭用;前端 noVNC 经 wsbridge 普通 TCP 透传路径直连 RFB。
type VncService struct {
	sessionFiles *SessionFileService
	mu           sync.RWMutex
	bridgeBase   string
	tokens       map[string]vncBridgeTok // token -> 绑定的目标与过期时间
	httpSrv      *http.Server
}

// SetSessionFiles 注入会话文件服务。
func (v *VncService) SetSessionFiles(sf *SessionFileService) {
	v.sessionFiles = sf
}

// Start 启动本机 WebSocket 字节桥(普通透传模式),返回基础 URL。
func (v *VncService) Start() (string, error) {
	base, srv, err := wsbridge.Start(v.validToken)
	if err != nil {
		return "", err
	}
	v.mu.Lock()
	if v.httpSrv != nil {
		_ = v.httpSrv.Shutdown(context.Background())
	}
	v.bridgeBase = base
	v.httpSrv = srv
	v.tokens = map[string]vncBridgeTok{}
	v.mu.Unlock()
	return base, nil
}

// Stop 关闭本机 VNC 桥 HTTP 服务,释放监听端口。
func (v *VncService) Stop() {
	v.mu.Lock()
	srv := v.httpSrv
	v.httpSrv = nil
	v.mu.Unlock()
	if srv != nil {
		_ = srv.Shutdown(context.Background())
	}
}

// validToken 校验令牌并返回其绑定的目标地址。
// 返回 ("", false) 表示令牌无效或已过期;过期项在此惰性删除,避免 map 无限增长。
func (v *VncService) validToken(token string) (string, bool) {
	v.mu.RLock()
	bt, ok := v.tokens[token]
	v.mu.RUnlock()
	if !ok {
		return "", false
	}
	if time.Now().After(bt.expiry) {
		v.mu.Lock()
		delete(v.tokens, token)
		v.mu.Unlock()
		return "", false
	}
	return bt.target, true
}

// GetVncConnection 根据会话路径返回 VNC 连接信息(含解密后的密码与桥接 WS 地址)。
func (v *VncService) GetVncConnection(sessionPath string) (string, error) {
	conn, err := v.buildVncConnection(sessionPath)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(conn)
	if err != nil {
		return "", fmt.Errorf("序列化 VNC 连接信息失败: %w", err)
	}
	return string(data), nil
}

func (v *VncService) buildVncConnection(sessionPath string) (*VncConnection, error) {
	if v.sessionFiles == nil {
		return nil, fmt.Errorf("会话服务不可用")
	}
	v.mu.RLock()
	base := v.bridgeBase
	v.mu.RUnlock()
	if base == "" {
		return nil, fmt.Errorf("VNC 桥未启动")
	}

	fullPath, err := v.sessionFiles.safeSessionPath(sessionPath)
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
	if info.Protocol != "vnc" {
		return nil, fmt.Errorf("会话 %s 不是 VNC 会话(protocol=%s)", info.Name, info.Protocol)
	}
	if info.Port <= 0 || info.Port > 65535 {
		return nil, fmt.Errorf("无效的 VNC 端口: %d", info.Port)
	}

	password := ""
	if info.Password != "" {
		password, err = v.sessionFiles.decrypt(info.Password)
		if err != nil {
			return nil, fmt.Errorf("本地保存的密码无法解密,请在连接对话框中重新输入密码")
		}
	}

	token, err := newBridgeToken()
	if err != nil {
		return nil, fmt.Errorf("生成桥接令牌失败: %w", err)
	}
	target := net.JoinHostPort(info.Host, fmt.Sprintf("%d", info.Port))
	v.mu.Lock()
	v.tokens[token] = vncBridgeTok{target: target, expiry: time.Now().Add(10 * time.Minute)}
	v.mu.Unlock()

	return &VncConnection{
		Host:        info.Host,
		Port:        info.Port,
		Username:    info.Username,
		Password:    password,
		AuthToken:   token,
		BridgeWsURL: plainBridgeWsURL(base, token),
	}, nil
}

// ReleaseVncConnection 释放会话桥接令牌,使已生成的桥接地址失效。
func (v *VncService) ReleaseVncConnection(token string) {
	v.mu.Lock()
	delete(v.tokens, token)
	v.mu.Unlock()
}

// plainBridgeWsURL 组装桥接 WebSocket 地址(普通透传路径,无 rdp=1)。
// 不携带 target 参数:目标在令牌绑定阶段即确定,桥接端强制使用令牌绑定目标。
func plainBridgeWsURL(base, token string) string {
	return strings.Replace(base, "http://", "ws://", 1) + "/bridge?token=" + token
}
