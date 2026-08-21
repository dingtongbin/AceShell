package wsbridge

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// startEchoServer 启动一个回环 TCP echo 服务器，返回监听地址。
func startEchoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("echo listen: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 8192)
				for {
					n, err := c.Read(buf)
					if n > 0 {
						if _, werr := c.Write(buf[:n]); werr != nil {
							return
						}
					}
					if err != nil {
						return
					}
				}
			}(conn)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().String()
}

func TestBridge_EchoRoundTrip(t *testing.T) {
	echoAddr := startEchoServer(t)
	baseURL, _, err := Start(func(token string) (string, bool) { return echoAddr, token == "test-token" })
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if !strings.HasPrefix(baseURL, "http://127.0.0.1:") {
		t.Fatalf("unexpected baseURL: %s", baseURL)
	}

	wsURL := strings.Replace(baseURL, "http://", "ws://", 1) + "/bridge?token=test-token&target=" + echoAddr
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ws, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial bridge: %v", err)
	}
	defer ws.Close(websocket.StatusNormalClosure, "")

	payload := []byte("hello-ace-wsbridge-12345")
	if err := ws.Write(ctx, websocket.MessageBinary, payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	typ, data, err := ws.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if typ != websocket.MessageBinary {
		t.Fatalf("expected binary message, got %d", typ)
	}
	if string(data) != string(payload) {
		t.Fatalf("echo mismatch: got %q want %q", data, payload)
	}
}

func TestBridge_RejectsBadToken(t *testing.T) {
	echoAddr := startEchoServer(t)
	baseURL, _, err := Start(func(token string) (string, bool) { return echoAddr, token == "ok" })
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	wsURL := strings.Replace(baseURL, "http://", "ws://", 1) + "/bridge?token=wrong&target=" + echoAddr
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _, err = websocket.Dial(ctx, wsURL, nil)
	if err == nil {
		t.Fatal("expected handshake to be rejected for bad token")
	}
}

func TestBridge_RejectsBadTarget(t *testing.T) {
	cases := []string{
		"127.0.0.1:notaport",
		"../../etc/passwd:22",
		":3389",
		"",
	}
	for _, target := range cases {
		// 令牌绑定一个非法目标,桥必须在校验目标阶段拒绝(防 SSRF)。
		baseURL, _, err := Start(func(token string) (string, bool) { return target, token == "ok" })
		if err != nil {
			t.Fatalf("Start failed: %v", err)
		}
		wsURL := strings.Replace(baseURL, "http://", "ws://", 1) + "/bridge?token=ok&target=" + target
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, _, err = websocket.Dial(ctx, wsURL, nil)
		cancel()
		if err == nil {
			t.Errorf("expected dial to fail for target %q", target)
		}
	}
}

func TestValidTarget(t *testing.T) {
	valid := []string{
		"192.168.1.10:3389",
		"example.com:443",
		"127.0.0.1:1",
		"server.local:65535",
	}
	for _, v := range valid {
		if !validTarget(v) {
			t.Errorf("expected %q to be valid", v)
		}
	}
	invalid := []string{
		"",
		"host",
		"host:0",
		"host:65536",
		"host:abc",
		"host:",
		"",
	}
	for _, v := range invalid {
		if validTarget(v) {
			t.Errorf("expected %q to be invalid", v)
		}
	}
}

func TestIsValidHostname(t *testing.T) {
	if !isValidHostname("example.com") || !isValidHostname("a-b.c") {
		t.Fatal("valid hostnames rejected")
	}
	if isValidHostname("") || isValidHostname("a..b") || isValidHostname("-a.com") || isValidHostname("a b.com") {
		t.Fatal("invalid hostnames accepted")
	}
}

func TestBridge_TargetDialFailure(t *testing.T) {
	// 未监听的目标端口:桥握手成功后应返回 dial failed 并关闭连接
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedAddr := ln.Addr().String()
	_ = ln.Close() // 立即关闭,让地址处于未监听状态

	baseURL, _, err := Start(func(token string) (string, bool) { return closedAddr, token == "ok" })
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	wsURL := strings.Replace(baseURL, "http://", "ws://", 1) + "/bridge?token=ok&target=" + closedAddr
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ws, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial should succeed before TCP dial: %v", err)
	}
	_, _, err = ws.Read(ctx)
	if err == nil {
		t.Fatal("expected connection to close after target dial failure")
	}
	var closeErr websocket.CloseError
	if !errors.As(err, &closeErr) && !errors.Is(err, context.DeadlineExceeded) {
		t.Logf("read error: %v", err)
	}
	ws.Close(websocket.StatusNormalClosure, "")
}