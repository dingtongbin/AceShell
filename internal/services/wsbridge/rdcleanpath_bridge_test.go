package wsbridge

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"fmt"
	"io"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// makeSelfSignedCert 生成自签测试证书,返回 DER 编码与 tls.Certificate。
func makeSelfSignedCert(t *testing.T) ([]byte, tls.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-rdp-server"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return der, tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// x224Confirm 返回模拟服务器发送的 X.224 连接确认(selected=PROTOCOL_SSL)。
var x224Confirm = []byte{
	0x03, 0x00, 0x00, 0x13, 0x0e, 0xd0, 0x00, 0x00,
	0x12, 0x34, 0x00, 0x02, 0x01, 0x08, 0x00, 0x01,
	0x00, 0x00, 0x00,
}

// startFakeRDPServer 启动模拟 RDP 服务器:读 X.224 请求 → 回 X.224 确认 →
// TLS 握手(真实 tls.Server) → echo 明文数据。
func startFakeRDPServer(t *testing.T) (string, []byte) {
	t.Helper()
	certDER, tlsCert := makeSelfSignedCert(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				hdr := make([]byte, 4)
				if _, err := io.ReadFull(c, hdr); err != nil {
					return
				}
				body := make([]byte, int(hdr[2])<<8|int(hdr[3])-4)
				if _, err := io.ReadFull(c, body); err != nil {
					return
				}
				if _, err := c.Write(x224Confirm); err != nil {
					return
				}
				tlsSrv := tls.Server(c, &tls.Config{Certificates: []tls.Certificate{tlsCert}})
				if err := tlsSrv.Handshake(); err != nil {
					return
				}
				buf := make([]byte, 1024)
				for {
					n, err := tlsSrv.Read(buf)
					if n > 0 {
						if _, werr := tlsSrv.Write(buf[:n]); werr != nil {
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
	return ln.Addr().String(), certDER
}

// encodeFakeRequest 构造 RDCleanPath 请求。
func encodeFakeRequest(destination, token string, x224 []byte) []byte {
	return encodeSequence(bytes.Join([][]byte{
		encodeCtxExplicit(ctxVersion, encodeInteger(rdcleanpathVersion)),
		encodeCtxExplicit(ctxDest, encodeUtf8String(destination)),
		encodeCtxExplicit(ctxProxyAuth, encodeUtf8String(token)),
		encodeCtxExplicit(ctxX224, encodeOctetString(x224)),
	}, nil))
}

func TestRdcleanpathBridgeE2E(t *testing.T) {
	addr, certDER := startFakeRDPServer(t)
	baseURL, _, err := Start(func(token string) (string, bool) { return addr, token == "my-token" })
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	wsURL := strings.Replace(baseURL, "http://", "ws://", 1) + "/bridge?rdp=1&token=my-token&target=" + addr
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ws, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial bridge: %v", err)
	}
	defer ws.Close(websocket.StatusNormalClosure, "")

	x224 := []byte{0x03, 0x00, 0x00, 0x13, 0x0e, 0xe0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	req := encodeFakeRequest(addr, "my-token", x224)
	if err := ws.Write(ctx, websocket.MessageBinary, req); err != nil {
		t.Fatalf("write request: %v", err)
	}
	typ, data, err := ws.Read(ctx)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if typ != websocket.MessageBinary {
		t.Fatalf("expected binary response, got %d", typ)
	}

	if !bytes.Contains(data, certDER) {
		t.Fatal("response missing server certificate chain")
	}
	if !bytes.Contains(data, []byte{0x03, 0x00, 0x00, 0x13}) {
		t.Fatal("response missing X.224 confirm")
	}

	if err := ws.Write(ctx, websocket.MessageBinary, []byte("hello-rdp")); err != nil {
		t.Fatalf("write relay data: %v", err)
	}
	typ, echoed, err := ws.Read(ctx)
	if err != nil {
		t.Fatalf("read relay data: %v", err)
	}
	if typ != websocket.MessageBinary || string(echoed) != "hello-rdp" {
		t.Fatalf("echo mismatch: type=%d data=%q", typ, echoed)
	}
}

func TestRdcleanpathBridgeBadToken(t *testing.T) {
	addr, _ := startFakeRDPServer(t)
	baseURL, _, err := Start(func(token string) (string, bool) { return addr, token == "good" })
	if err != nil {
		t.Fatal(err)
	}
	wsURL := strings.Replace(baseURL, "http://", "ws://", 1) + "/bridge?rdp=1&token=good&target=" + addr
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ws, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer ws.Close(websocket.StatusNormalClosure, "")
	req := encodeFakeRequest(addr, "wrong-token", []byte{0x03, 0x00, 0x00, 0x13})
	if err := ws.Write(ctx, websocket.MessageBinary, req); err != nil {
		t.Fatal(err)
	}
	_, _, err = ws.Read(ctx)
	if err == nil {
		t.Fatal("expected connection close for bad token")
	}
}

func TestRdcleanpathBridgeServerDown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedAddr := ln.Addr().String()
	_ = ln.Close()

	baseURL, _, err := Start(func(token string) (string, bool) { return closedAddr, token == "good" })
	if err != nil {
		t.Fatal(err)
	}
	wsURL := strings.Replace(baseURL, "http://", "ws://", 1) + "/bridge?rdp=1&token=good&target=" + closedAddr
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ws, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer ws.Close(websocket.StatusNormalClosure, "")
	req := encodeFakeRequest(closedAddr, "good", []byte{0x03, 0x00, 0x00, 0x13})
	if err := ws.Write(ctx, websocket.MessageBinary, req); err != nil {
		t.Fatal(err)
	}
	_, _, err = ws.Read(ctx)
	if err == nil {
		t.Fatal("expected close when server unreachable")
	}
}

func TestRdcleanpathBridgeNoTLS(t *testing.T) {
	// 服务器不回 TLS 协商 → bridge 应报错关闭连接
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		hdr := make([]byte, 4)
		if _, err := io.ReadFull(conn, hdr); err != nil {
			return
		}
		body := make([]byte, int(hdr[2])<<8|int(hdr[3])-4)
		if _, err := io.ReadFull(conn, body); err != nil {
			return
		}
		_, _ = conn.Write([]byte{0x03, 0x00, 0x00, 0x0b, 0x06, 0xd0, 0x00, 0x00, 0x12, 0x34, 0x00})
	}()
	t.Cleanup(func() { _ = ln.Close() })

	baseURL, _, err := Start(func(token string) (string, bool) { return ln.Addr().String(), token == "good" })
	if err != nil {
		t.Fatal(err)
	}
	wsURL := strings.Replace(baseURL, "http://", "ws://", 1) + "/bridge?rdp=1&token=good&target=" + ln.Addr().String()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ws, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer ws.Close(websocket.StatusNormalClosure, "")
	req := encodeFakeRequest(ln.Addr().String(), "good", []byte{0x03, 0x00, 0x00, 0x13})
	if err := ws.Write(ctx, websocket.MessageBinary, req); err != nil {
		t.Fatal(err)
	}
	_, _, err = ws.Read(ctx)
	if err == nil {
		t.Fatal("expected close when server has no TLS")
	}
}

func TestX224SelectedProtocol(t *testing.T) {
	body := []byte{0x0e, 0xd0, 0x00, 0x00, 0x12, 0x34, 0x00, 0x02, 0x01, 0x08, 0x00, 0x01, 0x00, 0x00, 0x00}
	if got := x224SelectedProtocol(body); got != 1 {
		t.Fatalf("expected selected=1 (SSL), got %d", got)
	}
	if got := x224SelectedProtocol([]byte{0x0e, 0xd0, 0x00, 0x00}); got != 0 {
		t.Fatalf("expected 0 for no negotiation, got %d", got)
	}
}

func TestEnsureX224RequestsTLS(t *testing.T) {
	// HYBRID-only 请求(0x02) → 改写后应包含 SSL(0x01)
	req := []byte{
		0x03, 0x00, 0x00, 0x13, 0x0e, 0xe0, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x08, 0x00, 0x02,
		0x00, 0x00, 0x00,
	}
	out := ensureX224RequestsTLS(req)
	want := []byte{
		0x03, 0x00, 0x00, 0x13, 0x0e, 0xe0, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x08, 0x00, 0x03,
		0x00, 0x00, 0x00,
	}
	if !bytes.Equal(out, want) {
		t.Fatalf("rewrite mismatch:\n got %x\nwant %x", out, want)
	}
	// 非法 TPKT 原样返回
	if !bytes.Equal(ensureX224RequestsTLS([]byte{0x01, 0x02, 0x03}), []byte{0x01, 0x02, 0x03}) {
		t.Fatal("non-TPKT input should pass through unchanged")
	}
	// 无协商请求不崩溃,原样返回
	plain := []byte{0x03, 0x00, 0x00, 0x0b, 0x0e, 0xe0, 0x00, 0x00, 0x12, 0x34, 0x00}
	if !bytes.Equal(ensureX224RequestsTLS(plain), plain) {
		t.Fatal("request without negotiation should pass through unchanged")
	}
}

func TestDerLengthBinary(t *testing.T) {
	if v, n, err := parseDerLength([]byte{0x82, 0x12, 0x34}); err != nil || n != 3 || v != 0x1234 {
		t.Fatalf("parseDerLength(0x1234) = %d/%d/%v", v, n, err)
	}
	if v, n, err := parseDerLength([]byte{0x05}); err != nil || n != 1 || v != 5 {
		t.Fatalf("parseDerLength(5) = %d/%d/%v", v, n, err)
	}
	if _, _, err := parseDerLength([]byte{0x81, 0x00}); err == nil {
		t.Fatal("expected error for zero-length long form")
	}
	if _, _, err := parseDerLength([]byte{}); err == nil {
		t.Fatal("expected error for missing length")
	}
}

var _ = binary.BigEndian
var _ = fmt.Sprintf