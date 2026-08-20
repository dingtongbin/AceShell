//go:build manual

package wsbridge

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestManual_RealServerRdcleanpath 手动集成验证:通过 wsbridge 连真实 RDP 服务器,
// 模拟 IronRDP 客户端发送 RDCleanPath 请求,检查服务器证书链与 X.224 确认。
// 运行: go test -tags manual -run TestManual_RealServerRdcleanpath -v ./internal/services/wsbridge/
func TestManual_RealServerRdcleanpath(t *testing.T) {
	server := "106.12.90.186:3389"
	token := "manual-test-token"

	baseURL, err := Start(func(tok string) bool { return tok == token })
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	wsURL := strings.Replace(baseURL, "http://", "ws://", 1) + "/bridge?rdp=1&token=" + token + "&target=" + server

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	ws, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial bridge: %v", err)
	}
	defer ws.Close(websocket.StatusNormalClosure, "")

	// 构造 X.224 Connection Request(请求 TLS 加密 PROTOCOL_SSL=0x01)
	x224 := []byte{
		0x03, 0x00, 0x00, 0x13, 0x0e, 0xe0, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x08, 0x00, 0x01,
		0x00, 0x00, 0x00,
	}
	req := encodeFakeRequest(server, token, x224)
	if err := ws.Write(ctx, websocket.MessageBinary, req); err != nil {
		t.Fatalf("write request: %v", err)
	}
	typ, data, err := ws.Read(ctx)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if typ != websocket.MessageBinary {
		t.Fatalf("expected binary, got %d", typ)
	}

	fmt.Printf("response length=%d\n", len(data))
	fmt.Printf("response prefix=%x\n", data[:min(40, len(data))])

	// 提取证书链并验证
	certCount := countCertificatesInResponse(t, data)
	if certCount == 0 {
		t.Fatal("no certificate chain in response")
	}
	fmt.Printf("certificates in chain: %d\n", certCount)

	// 验证响应含 X.224 Connection Confirm 头
	if !bytes.Contains(data, []byte{0x03, 0x00}) {
		t.Fatal("missing TPKT header in response")
	}
}

func countCertificatesInResponse(t *testing.T, data []byte) int {
	t.Helper()
	tag, content, rest, err := parseTlv(data)
	if err != nil || tag != 0x30 || len(rest) != 0 {
		t.Fatalf("bad outer seq: tag=%x err=%v", tag, err)
	}
	count := 0
	for len(content) > 0 {
		tag2, inner, rest2, err := parseTlv(content)
		if err != nil {
			t.Fatal(err)
		}
		content = rest2
		if tag2 == ctxCertChain|0xa0 {
			st, sc, sr, err := parseTlv(inner)
			if err != nil || st != 0x30 || len(sr) != 0 {
				t.Fatalf("bad cert chain: %x err=%v", st, err)
			}
			for len(sc) > 0 {
				ct, cc, cr, err := parseTlv(sc)
				if err != nil {
					t.Fatal(err)
				}
				sc = cr
				if ct != 0x04 {
					t.Fatalf("cert member type %x", ct)
				}
				fmt.Printf("cert[%d] len=%d\n", count, len(cc))
				count++
			}
		}
	}
	return count
}