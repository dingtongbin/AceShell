package wsbridge

import (
	"bytes"
	"testing"
)

func TestEncodeInteger(t *testing.T) {
	cases := []struct {
		v    int
		want []byte
	}{
		{0, []byte{0x02, 0x01, 0x00}},
		{1, []byte{0x02, 0x01, 0x01}},
		{127, []byte{0x02, 0x01, 0x7f}},
		{128, []byte{0x02, 0x02, 0x00, 0x80}},
		{256, []byte{0x02, 0x02, 0x01, 0x00}},
	}
	for _, c := range cases {
		got := encodeInteger(c.v)
		if !bytes.Equal(got, c.want) {
			t.Errorf("encodeInteger(%d) = %x, want %x", c.v, got, c.want)
		}
	}
}

func TestDerLength(t *testing.T) {
	if !bytes.Equal(derLength(5), []byte{0x05}) {
		t.Fatalf("short form failed")
	}
	got := derLength(0x80)
	want := []byte{0x81, 0x80}
	if !bytes.Equal(got, want) {
		t.Fatalf("long form 0x80: got %x want %x", got, want)
	}
	got = derLength(0x1234)
	want = []byte{0x82, 0x12, 0x34}
	if !bytes.Equal(got, want) {
		t.Fatalf("long form 0x1234: got %x want %x", got, want)
	}
}

func TestRdcleanpathRequestRoundTrip(t *testing.T) {
	x224 := []byte{0x03, 0x00, 0x00, 0x13, 0x0e, 0xe0, 0x00, 0x00, 0x00, 0x00, 0x00}
	encoded := encodeSequence(
		bytes.Join([][]byte{
			encodeCtxExplicit(ctxVersion, encodeInteger(rdcleanpathVersion)),
			encodeCtxExplicit(ctxDest, encodeUtf8String("10.0.0.1:3389")),
			encodeCtxExplicit(ctxProxyAuth, encodeUtf8String("secret-token")),
			encodeCtxExplicit(ctxX224, encodeOctetString(x224)),
		}, nil),
	)

	req, err := decodeRdcleanpathRequest(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if req.destination != "10.0.0.1:3389" {
		t.Errorf("destination = %q", req.destination)
	}
	if req.proxyAuth != "secret-token" {
		t.Errorf("proxy_auth = %q", req.proxyAuth)
	}
	if !bytes.Equal(req.x224Request, x224) {
		t.Errorf("x224 mismatch: %x", req.x224Request)
	}
}

func TestRdcleanpathRequestMissingFields(t *testing.T) {
	encoded := encodeSequence(
		bytes.Join([][]byte{
			encodeCtxExplicit(ctxVersion, encodeInteger(rdcleanpathVersion)),
			encodeCtxExplicit(ctxDest, encodeUtf8String("10.0.0.1:3389")),
		}, nil),
	)
	if _, err := decodeRdcleanpathRequest(encoded); err == nil {
		t.Fatal("expected error for missing proxy_auth/x224")
	}
}

func TestRdcleanpathRequestRejectsGarbage(t *testing.T) {
	bad := [][]byte{
		{},
		{0x30},
		{0x30, 0x05, 0xff, 0x01, 0x01, 0x00},
		{0x02, 0x01, 0x01},
	}
	for _, b := range bad {
		if _, err := decodeRdcleanpathRequest(b); err == nil {
			t.Errorf("expected error for input %x", b)
		}
	}
}

func TestRdcleanpathResponseRoundTrip(t *testing.T) {
	x224Resp := []byte{0x03, 0x00, 0x00, 0x13, 0x0f, 0xf0, 0x80, 0x21, 0x80}
	certA := []byte("fake-cert-a-der-bytes")
	certB := []byte("fake-cert-b-der-bytes")

	encoded := encodeRdcleanpathResponse("10.0.0.1", x224Resp, [][]byte{certA, certB})

	// 反解验证字段顺序与内容
	tag, content, rest, err := parseTlv(encoded)
	if err != nil || tag != 0x30 || len(rest) != 0 {
		t.Fatalf("bad outer sequence: tag=%x rest=%d err=%v", tag, len(rest), err)
	}
	gotX224 := false
	gotCerts := false
	gotAddr := false
	gotVersion := false
	for len(content) > 0 {
		tag2, inner, rest2, err := parseTlv(content)
		if err != nil {
			t.Fatal(err)
		}
		content = rest2
		switch tag2 {
		case ctxVersion | 0xa0:
			v, err := parseIntegerField(inner)
			if err != nil || v != rdcleanpathVersion {
				t.Fatalf("version mismatch: %d err=%v", v, err)
			}
			gotVersion = true
		case ctxX224 | 0xa0:
			o, err := parseOctetField(inner)
			if err != nil || !bytes.Equal(o, x224Resp) {
				t.Fatalf("x224 response mismatch: %x err=%v", o, err)
			}
			gotX224 = true
		case ctxCertChain | 0xa0:
			st, sc, sr, err := parseTlv(inner)
			if err != nil || st != 0x30 || len(sr) != 0 {
				t.Fatalf("bad cert chain seq: %x err=%v", st, err)
			}
			var certs [][]byte
			for len(sc) > 0 {
				ct, cc, cr, err := parseTlv(sc)
				if err != nil {
					t.Fatal(err)
				}
				sc = cr
				if ct != 0x04 {
					t.Fatalf("cert chain member not octet string: %x", ct)
				}
				certs = append(certs, cc)
			}
			if len(certs) != 2 || !bytes.Equal(certs[0], certA) || !bytes.Equal(certs[1], certB) {
				t.Fatalf("cert chain mismatch: %d certs", len(certs))
			}
			gotCerts = true
		case ctxServerAddr | 0xa0:
			s, err := parseUtf8Field(inner)
			if err != nil || s != "10.0.0.1" {
				t.Fatalf("server addr mismatch: %q err=%v", s, err)
			}
			gotAddr = true
		}
	}
	if !gotX224 || !gotCerts || !gotAddr || !gotVersion {
		t.Fatalf("missing fields: x224=%v certs=%v addr=%v version=%v", gotX224, gotCerts, gotAddr, gotVersion)
	}
}

func TestRdcleanpathResponseEmptyChain(t *testing.T) {
	encoded := encodeRdcleanpathResponse("10.0.0.1", []byte{0x03}, nil)
	if bytes.Contains(encoded, []byte{0xa7}) {
		t.Fatal("response with empty chain must not include cert chain field")
	}
	if bytes.Contains(encoded, []byte{0xa2}) {
		t.Fatal("response must not include destination field")
	}
}