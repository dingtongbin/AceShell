package wsbridge

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

// handleRdcleanpathBridge 处理 RDP 模式的桥接:解析 IronRDP 的 RDCleanPath 握手,
// 校验代理令牌后连接目标,以 TLS 客户端终止服务器加密会话,把服务器 X.224 确认
// 与证书链装回响应,随后以解密后的明文 RDP 流与客户端双向透传。
func handleRdcleanpathBridge(ws *websocket.Conn, r *http.Request, tokenCheck func(string) bool) {
	ctx := context.Background()
	ws.SetReadLimit(1 << 20)
	_, data, err := ws.Read(ctx)
	if err != nil {
		_ = ws.Close(websocket.StatusProtocolError, "missing rdcleanpath request")
		return
	}
	req, err := decodeRdcleanpathRequest(data)
	if err != nil {
		_ = ws.Close(websocket.StatusProtocolError, "invalid rdcleanpath request")
		return
	}
	if !tokenCheck(req.proxyAuth) {
		_ = ws.Close(websocket.StatusPolicyViolation, "forbidden")
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

// bufferedConn 保留已读缓冲的 net.Conn 包装,供 TLS 客户端继续使用。
type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (b *bufferedConn) Read(p []byte) (int, error) { return b.reader.Read(p) }

// readServerGreeting 读取 RDP 服务器初始响应:
// 1. 读取 X.224 连接确认(TPKT),解析协商协议;
// 2. 服务器要求 TLS 时,以 TLS 客户端完成握手并提取服务器证书链;
// 返回 X.224 确认字节、证书链 DER 及后续透传使用的流(已升级或原始 TCP)。
func readServerGreeting(conn net.Conn) ([]byte, [][]byte, net.Conn, error) {
	if err := conn.SetReadDeadline(time.Now().Add(15 * time.Second)); err != nil {
		return nil, nil, nil, err
	}
	defer conn.SetReadDeadline(time.Time{})

	r := bufio.NewReader(conn)
	var buf bytes.Buffer
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return nil, nil, nil, fmt.Errorf("读取 X.224 确认失败: %w", err)
	}
	if hdr[0] != 0x03 || hdr[1] != 0x00 {
		return nil, nil, nil, fmt.Errorf("无效的 TPKT 头: %x", hdr[:2])
	}
	length := int(hdr[2])<<8 | int(hdr[3])
	if length < 4 {
		return nil, nil, nil, fmt.Errorf("无效的 TPKT 长度: %d", length)
	}
	body := make([]byte, length-4)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, nil, nil, fmt.Errorf("读取 X.224 确认内容失败: %w", err)
	}
	buf.Write(hdr)
	buf.Write(body)

	selected := x224SelectedProtocol(body)
	if selected == 0 {
		return nil, nil, nil, fmt.Errorf("服务器未启用 TLS 加密,暂不支持(请配置 security_layer=tls)")
	}

	tlsConn := tls.Client(&bufferedConn{Conn: conn, reader: r}, &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS12,
	})
	if err := tlsConn.Handshake(); err != nil {
		return nil, nil, nil, fmt.Errorf("TLS 握手失败: %w", err)
	}
	var certs [][]byte
	for _, cert := range tlsConn.ConnectionState().PeerCertificates {
		certs = append(certs, cert.Raw)
	}
	return buf.Bytes(), certs, tlsConn, nil
}

// x224SelectedProtocol 从 X.224 连接确认中解析服务器选定的 RDP 安全协议。
// RDP 协商响应(type=0x02)结构: type(1) flags(1) length(2) selected(4, 小端)。
func x224SelectedProtocol(body []byte) uint32 {
	for i := 0; i+8 <= len(body); i++ {
		if body[i] == 0x02 && body[i+1] == 0x01 {
			return binary.LittleEndian.Uint32(body[i+4 : i+8])
		}
	}
	return 0
}

// ensureX224RequestsTLS 改写 X.224 连接请求的 RDP 协商 requested_protocols,
// 强制包含 PROTOCOL_SSL(0x01)。IronRDP 默认请求 HYBRID(NLA),无 NLA 支持的
// 服务器(如 xrdp 默认配置)会以 "client did not request TLS" 拒绝,加上 SSL 位后
// 服务器回退到 TLS 加密。
func ensureX224RequestsTLS(x224 []byte) []byte {
	if len(x224) < 8 || x224[0] != 0x03 || x224[1] != 0x00 {
		return x224
	}
	length := int(x224[2])<<8 | int(x224[3])
	if length < 4 || length > len(x224) {
		return x224
	}
	body := x224[4:length]
	for i := 0; i+8 <= len(body); i++ {
		// RDP_NEG_REQ: type=0x01, flags=0x00, len(2), requested(4, 小端)
		if body[i] == 0x01 && body[i+1] == 0x00 {
			reqLen := int(body[i+2])<<8 | int(body[i+3])
			if reqLen >= 8 && i+8 <= len(body) {
				requested := binary.LittleEndian.Uint32(body[i+4 : i+8])
				binary.LittleEndian.PutUint32(body[i+4:i+8], requested|0x01)
			}
			break
		}
	}
	return x224[:length]
}