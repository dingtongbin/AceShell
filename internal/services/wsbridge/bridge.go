package wsbridge

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// Start 启动仅绑定 127.0.0.1 的 WebSocket↔TCP 字节桥服务。
// tokenCheck 校验每会话令牌并返回其绑定的目标地址,防止本机其他进程利用
// 一个有效令牌让桥连到任意内网 TCP 服务(SSRF 跳板)。返回基础 URL 与
// *http.Server,调用方应在退出时 Shutdown 以释放监听端口。
func Start(tokenCheck func(string) (string, bool)) (string, *http.Server, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("/bridge", func(w http.ResponseWriter, r *http.Request) {
		handleBridge(w, r, tokenCheck)
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, fmt.Errorf("wsbridge listen: %w", err)
	}
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() { _ = server.Serve(ln) }()
	return "http://" + ln.Addr().String(), server, nil
}

func handleBridge(w http.ResponseWriter, r *http.Request, tokenCheck func(string) (string, bool)) {
	if !originAllowed(r.Header.Get("Origin")) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	boundTarget, ok := tokenCheck(r.URL.Query().Get("token"))
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if r.URL.Query().Get("rdp") == "1" {
		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		handleRdcleanpathBridge(ws, r, boundTarget)
		return
	}
	// 非 RDP 路径:强制使用令牌绑定的目标,忽略 URL 中的 target 参数(防 SSRF)。
	// 目标在令牌绑定阶段即确定,握手前校验,避免为非法目标做无意义 Accept。
	if !validTarget(boundTarget) {
		http.Error(w, "bad target", http.StatusBadRequest)
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	tcp, err := net.DialTimeout("tcp", boundTarget, 10*time.Second)
	if err != nil {
		_ = ws.Close(websocket.StatusInternalError, "dial failed")
		return
	}
	relay(ws, tcp)
}

// handleRdcleanpathBridge 处理 RDP 模式的桥接:解析 IronRDP 的 RDCleanPath 握手,
// 校验代理令牌后连接目标,以 TLS 客户端终止服务器加密会话,把服务器 X.224 确认
// 与证书链装回响应,随后以解密后的明文 RDP 流与客户端双向透传。
func handleRdcleanpathBridge(ws *websocket.Conn, r *http.Request, boundTarget string) {
	ctx := context.Background()
	ws.SetReadLimit(-1)
	// 首包(握手请求)限时 15s: 恶意/半开客户端不发数据时及时释放,
	// 避免 goroutine 在无界 Read 上永久悬挂。
	readCtx, readCancel := context.WithTimeout(ctx, 15*time.Second)
	_, data, err := ws.Read(readCtx)
	readCancel()
	if err != nil {
		_ = ws.Close(websocket.StatusProtocolError, "missing rdcleanpath request")
		return
	}
	req, err := decodeRdcleanpathRequest(data)
	if err != nil {
		_ = ws.Close(websocket.StatusProtocolError, "invalid rdcleanpath request")
		return
	}
	// RDP 路径:校验客户端 PDU 中的 destination 与令牌绑定目标一致(防 SSRF 跳板)。
	if req.destination != boundTarget {
		_ = ws.Close(websocket.StatusPolicyViolation, "target mismatch")
		return
	}
	tcp, err := net.DialTimeout("tcp", req.destination, 10*time.Second)
	if err != nil {
		_ = ws.Close(websocket.StatusInternalError, "dial failed")
		return
	}
	defer tcp.Close()
	if _, err := tcp.Write(ensureX224RequestsTLS(req.x224Request)); err != nil {
		return
	}

	resp, certs, stream, err := readServerGreeting(tcp)
	if err != nil {
		_ = ws.Close(websocket.StatusInternalError, err.Error())
		return
	}
	if len(certs) == 0 {
		_ = ws.Close(websocket.StatusInternalError, "no server certificate")
		return
	}
	serverAddr, _, err := net.SplitHostPort(req.destination)
	if err != nil {
		serverAddr = req.destination
	}
	response := encodeRdcleanpathResponse(serverAddr, resp, certs)
	if err := ws.Write(ctx, websocket.MessageBinary, response); err != nil {
		return
	}
	relay(ws, stream)
}

// originAllowed 校验 WebSocket 握手 Origin 头,防止用户浏览器中的恶意网页
// 连接本机桥接端口(浏览器强制同源策略,网页 JS 无法伪造 Origin 头)。
// 允许:
//   - 空:非浏览器本机客户端(如后端自身、curl)不发 Origin
//   - null:自定义 scheme webview(macOS/Linux/iOS 加载 wails:// 页面时 origin 序列化为 null)
//   - wails.localhost:Wails 各平台 webview 固定 origin(http/https)
//   - localhost/127.0.0.1 任意端口:开发模式前端 dev server
func originAllowed(origin string) bool {
	if origin == "" || origin == "null" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "wails.localhost", "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// validTarget 校验 target 为合法的 host:port。
func validTarget(target string) bool {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return false
	}
	if net.ParseIP(host) == nil && !isValidHostname(host) {
		return false
	}
	p, err := net.LookupPort("tcp", port)
	return err == nil && p > 0 && p <= 65535
}

// isValidHostname 校验主机名为合法 DNS 标签序列。
func isValidHostname(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	for _, label := range strings.Split(h, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// relay 将 WebSocket 二进制帧与 TCP 流双向转发,任一方向结束后关闭双方。
// 任一方向出错即整体取消,避免对端静默挂死导致 goroutine 永久阻塞;
// TCP→WS 的写入设置 30s 超时,防止慢/挂死的对端耗尽连接。
func relay(ws *websocket.Conn, tcp net.Conn) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			_, data, err := ws.Read(ctx)
			if err != nil {
				return
			}
			if _, err := tcp.Write(data); err != nil {
				return
			}
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		buf := make([]byte, 64*1024)
		for {
			n, err := tcp.Read(buf)
			if n > 0 {
				wctx, wcancel := context.WithTimeout(ctx, 30*time.Second)
				werr := ws.Write(wctx, websocket.MessageBinary, buf[:n])
				wcancel()
				if werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	<-done
	cancel()
	_ = ws.Close(websocket.StatusNormalClosure, "")
	_ = tcp.Close()
}
