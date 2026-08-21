package wsbridge

import (
	"encoding/binary"
	"fmt"
)

// RDCleanPath 是 IronRDP 浏览器客户端连接 WebSocket 代理时的首包协议(ASN.1 DER)。
// 结构定义见 ironrdp-rdcleanpath crate:
//
//	RDCleanPathPdu ::= SEQUENCE {
//	  version             [0] EXPLICIT INTEGER,
//	  error               [1] EXPLICIT RDCleanPathErr OPTIONAL,
//	  destination         [2] EXPLICIT UTF8String OPTIONAL,
//	  proxy_auth          [3] EXPLICIT UTF8String OPTIONAL,
//	  server_auth         [4] EXPLICIT UTF8String OPTIONAL,
//	  preconnection_blob  [5] EXPLICIT UTF8String OPTIONAL,
//	  x224_connection_pdu [6] EXPLICIT OCTET STRING OPTIONAL,
//	  server_cert_chain   [7] EXPLICIT SEQUENCE OF OCTET STRING OPTIONAL,
//	  server_addr         [9] EXPLICIT UTF8String OPTIONAL,
//	}
const (
	ctxVersion    = 0
	ctxError      = 1
	ctxDest       = 2
	ctxProxyAuth  = 3
	ctxServerAuth = 4
	ctxPCB        = 5
	ctxX224       = 6
	ctxCertChain  = 7
	ctxServerAddr = 9
)

// rdcleanpathVersion 对应 ironrdp-rdcleanpath 的 VERSION_1 (BASE_VERSION 3389 + 1)。
// 客户端检测 PDU 时校验 version 必须等于该值,否则视为无效报文。
const rdcleanpathVersion = 3390

// rdcleanpathRequest RDCleanPath 请求报文(客户端→代理)的已解析字段。
type rdcleanpathRequest struct {
	version     int
	destination string
	proxyAuth   string
	x224Request []byte
}

// decodeRdcleanpathRequest 解析 RDCleanPath 请求,提取目标地址、代理令牌与 X.224 连接请求。
func decodeRdcleanpathRequest(data []byte) (rdcleanpathRequest, error) {
	var req rdcleanpathRequest
	tag, content, rest, err := parseTlv(data)
	if err != nil {
		return req, fmt.Errorf("解析 RDCleanPath 外层结构失败: %w", err)
	}
	if tag != 0x30 || len(rest) != 0 {
		return req, fmt.Errorf("无效的 RDCleanPath 报文(期望单个 SEQUENCE)")
	}
	for len(content) > 0 {
		t, c, r, err := parseTlv(content)
		if err != nil {
			return req, fmt.Errorf("解析 RDCleanPath 字段失败: %w", err)
		}
		content = r
		switch t {
		case ctxVersion | 0xa0:
			v, err := parseIntegerField(c)
			if err != nil {
				return req, fmt.Errorf("version 字段无效: %w", err)
			}
			req.version = v
		case ctxDest | 0xa0:
			s, err := parseUtf8Field(c)
			if err != nil {
				return req, fmt.Errorf("destination 字段无效: %w", err)
			}
			req.destination = s
		case ctxProxyAuth | 0xa0:
			s, err := parseUtf8Field(c)
			if err != nil {
				return req, fmt.Errorf("proxy_auth 字段无效: %w", err)
			}
			req.proxyAuth = s
		case ctxX224 | 0xa0:
			o, err := parseOctetField(c)
			if err != nil {
				return req, fmt.Errorf("x224_connection_pdu 字段无效: %w", err)
			}
			req.x224Request = o
		}
	}
	if req.version != rdcleanpathVersion {
		return req, fmt.Errorf("不支持的 RDCleanPath 协议版本: %d", req.version)
	}
	if req.destination == "" || req.proxyAuth == "" || len(req.x224Request) == 0 {
		return req, fmt.Errorf("RDCleanPath 请求缺少必要字段(destination/proxy_auth/x224_connection_pdu)")
	}
	return req, nil
}

// encodeRdcleanpathResponse 构造 RDCleanPath 响应(代理→客户端)。
// 参数 serverAddr 为解析出的目标服务器地址;x224Response 为服务器返回的
// X.224 连接确认(含后续 TLS 握手字节);certChain 为服务器 TLS 证书链 DER。
func encodeRdcleanpathResponse(serverAddr string, x224Response []byte, certChain [][]byte) []byte {
	var fields []byte
	fields = append(fields, encodeCtxExplicit(ctxVersion, encodeInteger(rdcleanpathVersion))...)
	fields = append(fields, encodeCtxExplicit(ctxX224, encodeOctetString(x224Response))...)
	if len(certChain) > 0 {
		var certs []byte
		for _, c := range certChain {
			certs = append(certs, encodeOctetString(c)...)
		}
		fields = append(fields, encodeCtxExplicit(ctxCertChain, encodeSequence(certs))...)
	}
	fields = append(fields, encodeCtxExplicit(ctxServerAddr, encodeUtf8String(serverAddr))...)
	return encodeSequence(fields)
}

// parseTlv 解析单层 TLV:返回标签、内容、剩余字节。
func parseTlv(data []byte) (tag byte, content []byte, rest []byte, err error) {
	if len(data) < 2 {
		return 0, nil, nil, fmt.Errorf("数据过短")
	}
	tag = data[0]
	length, n, err := parseDerLength(data[1:])
	if err != nil {
		return 0, nil, nil, err
	}
	start := 1 + n
	if length < 0 || start+length > len(data) {
		return 0, nil, nil, fmt.Errorf("长度越界")
	}
	return tag, data[start : start+length], data[start+length:], nil
}

// parseDerLength 解析 DER 长度(支持长格式),返回长度值及长度字段占用字节数。
func parseDerLength(data []byte) (int, int, error) {
	if len(data) == 0 {
		return 0, 0, fmt.Errorf("缺少长度字节")
	}
	b := data[0]
	if b < 0x80 {
		return int(b), 1, nil
	}
	num := int(b & 0x7f)
	if num == 0 || num > 8 || num+1 > len(data) {
		return 0, 0, fmt.Errorf("无效的长格式长度")
	}
	var v int
	for i := 1; i <= num; i++ {
		v = v<<8 | int(data[i])
	}
	if v == 0 {
		return 0, 0, fmt.Errorf("长格式长度不得为零值")
	}
	return v, 1 + num, nil
}

func parseUtf8Field(inner []byte) (string, error) {
	tag, content, rest, err := parseTlv(inner)
	if err != nil {
		return "", err
	}
	if tag != 0x0c || len(rest) != 0 {
		return "", fmt.Errorf("非 UTF8String 字段")
	}
	return string(content), nil
}

func parseOctetField(inner []byte) ([]byte, error) {
	tag, content, rest, err := parseTlv(inner)
	if err != nil {
		return nil, err
	}
	if tag != 0x04 || len(rest) != 0 {
		return nil, fmt.Errorf("非 OCTET STRING 字段")
	}
	return content, nil
}

// parseIntegerField 解析 ASN.1 INTEGER 字段内容(正数)。
func parseIntegerField(inner []byte) (int, error) {
	tag, content, rest, err := parseTlv(inner)
	if err != nil {
		return 0, err
	}
	if tag != 0x02 || len(rest) != 0 {
		return 0, fmt.Errorf("非 INTEGER 字段")
	}
	var v int
	for _, b := range content {
		v = v<<8 | int(b)
	}
	return v, nil
}

// encodeInteger 编码 ASN.1 INTEGER(小正整数,去前导零;最高字节 bit7 置位时补 0x00)。
func encodeInteger(v int) []byte {
	if v == 0 {
		return []byte{0x02, 0x01, 0x00}
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(v))
	start := 0
	for start < 7 && buf[start] == 0 {
		start++
	}
	if buf[start]&0x80 != 0 {
		start--
	}
	out := []byte{0x02}
	out = append(out, derLength(8-start)...)
	return append(out, buf[start:]...)
}

func encodeUtf8String(s string) []byte {
	out := []byte{0x0c}
	out = append(out, derLength(len(s))...)
	return append(out, s...)
}

func encodeOctetString(b []byte) []byte {
	out := []byte{0x04}
	out = append(out, derLength(len(b))...)
	return append(out, b...)
}

func encodeSequence(fields []byte) []byte {
	out := []byte{0x30}
	out = append(out, derLength(len(fields))...)
	return append(out, fields...)
}

// encodeCtxExplicit 编码 context-specific 显式标签:外层标签 0xa0|tag,内层为完整编码。
func encodeCtxExplicit(tag int, inner []byte) []byte {
	out := []byte{0xa0 | byte(tag)}
	out = append(out, derLength(len(inner))...)
	return append(out, inner...)
}

// derLength 编码 DER 长度(短格式优先,长格式兜底)。
func derLength(n int) []byte {
	if n < 0x80 {
		return []byte{byte(n)}
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte(n)
		n >>= 8
	}
	out := []byte{0x80 | byte(len(buf)-i)}
	return append(out, buf[i:]...)
}
