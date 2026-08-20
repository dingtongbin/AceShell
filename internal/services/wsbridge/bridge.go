package wsbridge

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// Start 启动仅绑定 127.0.0.1 的 WebSocket↔TCP 字节桥服务。
// tokenCheck 校验每会话令牌，防止本机其他进程蹭用；
// 返回基础 URL（如 http://127.0.0.1:51234），前端据此拼桥接地址。
func Start(tokenCheck func(string) bool) (string, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("/bridge", func(w http.ResponseWriter, r *http.Request) {
		handleBridge(w, r, tokenCheck)
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("wsbridge listen: %w", err)
	}
	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(ln) }()
	return "http://" + ln.Addr().String(), nil
}

func handleBridge(w http.ResponseWriter, r *http.Request, tokenCheck func(string) bool) {
	if !tokenCheck(r.URL.Query().Get("token")) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	target := r.URL.Query().Get("target")
	if !validTarget(target) {
		http.Error(w, "bad target", http.StatusBadRequest)
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	if r.URL.Query().Get("rdp") == "1" {
		handleRdcleanpathBridge(ws, r, tokenCheck)
		return
	}
	tcp, err := net.DialTimeout("tcp", target, 10*time.Second)
	if err != nil {
		_ = ws.Close(websocket.StatusInternalError, "dial failed")
		return
	}
	relay(ws, tcp)
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

// relay 将 WebSocket 二进制帧与 TCP 流双向转发，任一方向结束后关闭双方。
func relay(ws *websocket.Conn, tcp net.Conn) {
	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			_, data, err := ws.Read(context.Background())
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
				if werr := ws.Write(context.Background(), websocket.MessageBinary, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	<-done
	_ = ws.Close(websocket.StatusNormalClosure, "")
	_ = tcp.Close()
}