package services

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func newVncTestService(t *testing.T) (*VncService, *SessionFileService, func()) {
	t.Helper()
	restore := SetDataDir(t.TempDir())
	sf := &SessionFileService{}
	sf.SetApp(nil)
	svc := &VncService{}
	svc.SetSessionFiles(sf)
	base, err := svc.Start()
	if err != nil {
		restore()
		t.Fatalf("Start: %v", err)
	}
	if !strings.HasPrefix(base, "http://127.0.0.1:") {
		restore()
		t.Fatalf("unexpected bridge base: %s", base)
	}
	return svc, sf, restore
}

func saveVncSession(t *testing.T, sf *SessionFileService, name string) string {
	t.Helper()
	data := fmt.Sprintf("name=%q\nhost=\"10.0.0.2\"\nport=5901\nusername=\"ops\"\npassword=\"vncpass\"\nprotocol=\"vnc\"\n", name)
	if err := sf.SaveSession("", data); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	return name + ".toml"
}

func TestVncService_GetVncConnection(t *testing.T) {
	svc, sf, restore := newVncTestService(t)
	defer restore()
	path := saveVncSession(t, sf, "我的VNC")

	raw, err := svc.GetVncConnection(path)
	if err != nil {
		t.Fatalf("GetVncConnection: %v", err)
	}
	var conn VncConnection
	if err := json.Unmarshal([]byte(raw), &conn); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if conn.Host != "10.0.0.2" || conn.Port != 5901 || conn.Username != "ops" || conn.Password != "vncpass" {
		t.Fatalf("unexpected conn: %+v", conn)
	}
	// 普通透传路径:ws:// 前缀 + 令牌;绝不能出现 rdp=1 或 target= 参数
	if !strings.HasPrefix(conn.BridgeWsURL, "ws://127.0.0.1:") {
		t.Fatalf("unexpected bridge url: %s", conn.BridgeWsURL)
	}
	if strings.Contains(conn.BridgeWsURL, "rdp=1") || strings.Contains(conn.BridgeWsURL, "target=") {
		t.Fatalf("plain bridge url must not carry rdp/target params: %s", conn.BridgeWsURL)
	}
	if !strings.Contains(conn.BridgeWsURL, "token="+conn.AuthToken) {
		t.Fatalf("bridge url token must match authToken: %s / %s", conn.BridgeWsURL, conn.AuthToken)
	}
	if conn.AuthToken == "" {
		t.Fatal("expected non-empty auth token")
	}
}

func TestVncService_RejectsNonVnc(t *testing.T) {
	svc, sf, restore := newVncTestService(t)
	defer restore()
	data := "name=\"普通SSH\"\nhost=\"192.168.1.1\"\nport=22\nprotocol=\"ssh\"\n"
	if err := sf.SaveSession("", data); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetVncConnection("普通SSH.toml"); err == nil {
		t.Fatal("expected error for non-vnc session")
	}
}

func TestVncService_RejectsInvalidPort(t *testing.T) {
	svc, sf, restore := newVncTestService(t)
	defer restore()
	data := "name=\"坏端口\"\nhost=\"127.0.0.1\"\nport=0\nprotocol=\"vnc\"\n"
	if err := sf.SaveSession("", data); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetVncConnection("坏端口.toml"); err == nil {
		t.Fatal("expected error for invalid port")
	}
}

func TestVncService_BridgeTokenRevocation(t *testing.T) {
	svc, sf, restore := newVncTestService(t)
	defer restore()
	path := saveVncSession(t, sf, "撤销会话")

	raw, err := svc.GetVncConnection(path)
	if err != nil {
		t.Fatal(err)
	}
	var conn VncConnection
	_ = json.Unmarshal([]byte(raw), &conn)
	token := extractToken(conn.BridgeWsURL)
	if token == "" {
		t.Fatal("empty token")
	}
	if _, ok := svc.validToken(token); !ok {
		t.Fatal("expected token valid before release")
	}
	svc.ReleaseVncConnection(token)
	if _, ok := svc.validToken(token); ok {
		t.Fatal("expected token invalid after release")
	}
}

func TestVncService_EmptyPasswordAllowed(t *testing.T) {
	// None 认证的 VNC 服务端无密码,空密码应正常下发连接信息
	svc, sf, restore := newVncTestService(t)
	defer restore()
	data := "name=\"无密码VNC\"\nhost=\"127.0.0.1\"\nport=5900\nprotocol=\"vnc\"\n"
	if err := sf.SaveSession("", data); err != nil {
		t.Fatal(err)
	}
	raw, err := svc.GetVncConnection("无密码VNC.toml")
	if err != nil {
		t.Fatalf("GetVncConnection: %v", err)
	}
	var conn VncConnection
	if err := json.Unmarshal([]byte(raw), &conn); err != nil {
		t.Fatal(err)
	}
	if conn.Password != "" {
		t.Fatalf("expected empty password, got %q", conn.Password)
	}
}
