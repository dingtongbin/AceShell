package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newRdpTestService(t *testing.T) (*RdpService, *SessionFileService, func()) {
	t.Helper()
	restore := SetDataDir(t.TempDir())
	sf := &SessionFileService{}
	sf.SetApp(nil)
	svc := &RdpService{}
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

func saveRdpSession(t *testing.T, sf *SessionFileService, name string) string {
	t.Helper()
	data := fmt.Sprintf("name=%q\nhost=\"10.0.0.1\"\nport=3389\nusername=\"admin\"\npassword=\"secret123\"\nprotocol=\"rdp\"\n", name)
	if err := sf.SaveSession("", data); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	return name + ".toml"
}

func TestRdpService_GetRdpConnection(t *testing.T) {
	svc, sf, restore := newRdpTestService(t)
	defer restore()
	path := saveRdpSession(t, sf, "我的RDP")

	raw, err := svc.GetRdpConnection(path)
	if err != nil {
		t.Fatalf("GetRdpConnection: %v", err)
	}
	var conn RdpConnection
	if err := json.Unmarshal([]byte(raw), &conn); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if conn.Host != "10.0.0.1" || conn.Port != 3389 || conn.Username != "admin" || conn.Password != "secret123" {
		t.Fatalf("unexpected conn: %+v", conn)
	}
	if !strings.HasPrefix(conn.BridgeWsURL, "ws://127.0.0.1:") || !strings.Contains(conn.BridgeWsURL, "&target=10.0.0.1%3A3389") {
		t.Fatalf("unexpected bridge url: %s", conn.BridgeWsURL)
	}
	if !strings.Contains(conn.BridgeWsURL, "rdp=1") {
		t.Fatalf("bridge url missing rdp mode: %s", conn.BridgeWsURL)
	}
	if conn.AuthToken == "" {
		t.Fatal("expected non-empty auth token")
	}
	if !strings.Contains(conn.BridgeWsURL, "token="+conn.AuthToken) {
		t.Fatalf("bridge url token must match authToken: %s / %s", conn.BridgeWsURL, conn.AuthToken)
	}
}

func TestRdpService_RejectsNonRdp(t *testing.T) {
	svc, sf, restore := newRdpTestService(t)
	defer restore()
	data := "name=\"普通Telnet\"\nhost=\"192.168.1.1\"\nport=23\nprotocol=\"telnet\"\n"
	if err := sf.SaveSession("", data); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetRdpConnection("普通Telnet.toml"); err == nil {
		t.Fatal("expected error for non-rdp session")
	}
}

func TestRdpService_RejectsInvalidPort(t *testing.T) {
	svc, sf, restore := newRdpTestService(t)
	defer restore()
	data := "name=\"坏端口\"\nhost=\"127.0.0.1\"\nport=0\nprotocol=\"rdp\"\n"
	if err := sf.SaveSession("", data); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetRdpConnection("坏端口.toml"); err == nil {
		t.Fatal("expected error for invalid port")
	}
}

func TestRdpService_BridgeTokenRevocation(t *testing.T) {
	svc, sf, restore := newRdpTestService(t)
	defer restore()
	path := saveRdpSession(t, sf, "撤销会话")

	raw, err := svc.GetRdpConnection(path)
	if err != nil {
		t.Fatal(err)
	}
	var conn RdpConnection
	_ = json.Unmarshal([]byte(raw), &conn)
	token := extractToken(conn.BridgeWsURL)
	if token == "" {
		t.Fatal("empty token")
	}
	if _, ok := svc.validToken(token); !ok {
		t.Fatal("expected token valid before release")
	}
	svc.ReleaseRdpConnection(token)
	if _, ok := svc.validToken(token); ok {
		t.Fatal("expected token invalid after release")
	}
}

func extractToken(wsURL string) string {
	i := strings.Index(wsURL, "token=")
	if i < 0 {
		return ""
	}
	rest := wsURL[i+len("token="):]
	if j := strings.Index(rest, "&"); j >= 0 {
		return rest[:j]
	}
	return rest
}

func TestRdpService_NoTestServers(t *testing.T) {
	svc := &RdpService{}
	if raw := svc.GetRdpTestServers(); raw != "[]" {
		t.Fatalf("expected empty array, got %s", raw)
	}
}

func TestRdpService_LoadTestServers(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "internal", "services")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := `{
  "rdp": [
    { "name": "阿里云RDP", "host": "106.12.90.186", "port": 3389, "username": "root", "password": "rdp123" },
    { "name": "无效端口", "host": "127.0.0.1", "port": 0 }
  ]
}`
	if err := os.WriteFile(filepath.Join(dir, "testservers.json"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(tmp)

	svc := &RdpService{}
	raw := svc.GetRdpTestServers()
	var servers []RdpTestServer
	if err := json.Unmarshal([]byte(raw), &servers); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("expected 1 valid server, got %d: %s", len(servers), raw)
	}
	if servers[0].Host != "106.12.90.186" || servers[0].Password != "rdp123" {
		t.Fatalf("unexpected server: %+v", servers[0])
	}
}