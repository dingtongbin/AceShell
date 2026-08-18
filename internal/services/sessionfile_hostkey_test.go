package services

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

)

func TestSessionFileService_FingerprintDualWrite(t *testing.T) {
	withTestDataDir(t)
	svc := &SessionFileService{}
	svc.SetApp(nil)
	os.MkdirAll(filepath.Join(sessionsDir, "f"), 0755)

	tomlContent := `[session]
name = "fp"
host = "10.0.0.7"
port = 22
username = "root"
password = ""
protocol = "ssh"
`
	os.WriteFile(filepath.Join(sessionsDir, "f", "fp.toml"), []byte(tomlContent), 0644)

	addr := "10.0.0.7:22"
	keyA := "fingerprint-A"
	keyB := "fingerprint-B"

	if err := svc.SaveHostKey("f", addr, keyA); err != nil {
		t.Fatalf("SaveHostKey failed: %v", err)
	}

	// 会话文件双写
	stored, ok := svc.findSessionHostKey("f", addr)
	if !ok || stored != keyA {
		t.Fatalf("session file hostKey not dual-written: ok=%v stored=%q", ok, stored)
	}

	// 文件夹 json 命中
	ok, err := svc.VerifyHostKey("f", addr, keyA)
	if err != nil || !ok {
		t.Fatalf("VerifyHostKey should match via session file: ok=%v err=%v", ok, err)
	}

	// 不同指纹不匹配
	ok, err = svc.VerifyHostKey("f", addr, keyB)
	if err != nil || ok {
		t.Fatalf("VerifyHostKey should reject different fingerprint: ok=%v err=%v", ok, err)
	}

	// 无记录 → not found
	ok, err = svc.VerifyHostKey("f", "10.0.0.8:22", keyA)
	if err != nil || ok {
		t.Fatalf("VerifyHostKey unknown addr should be false: ok=%v err=%v", ok, err)
	}

	// RemoveHostKey 清理双处
	if err := svc.RemoveHostKey("f", addr); err != nil {
		t.Fatalf("RemoveHostKey failed: %v", err)
	}
	if stored, _ := svc.findSessionHostKey("f", addr); stored != "" {
		t.Fatal("session file hostKey should be cleared")
	}
	ok, _ = svc.VerifyHostKey("f", addr, keyA)
	if ok {
		t.Fatal("VerifyHostKey should not match after removal")
	}
}

// TestSessionFileService_DeleteSessionCleansFingerprint 验证删除会话时指纹清理:
// 仅当无其他同 host:port 会话时删除文件夹 json 键。
func TestSessionFileService_DeleteSessionCleansFingerprint(t *testing.T) {
	withTestDataDir(t)
	svc := &SessionFileService{}
	svc.SetApp(nil)
	os.MkdirAll(filepath.Join(sessionsDir, "d"), 0755)

	base := `[session]
name = "%s"
host = "10.0.0.9"
port = 22
username = "root"
password = ""
protocol = "ssh"
`
	os.WriteFile(filepath.Join(sessionsDir, "d", "a.toml"), []byte(fmt.Sprintf(base, "a")), 0644)
	os.WriteFile(filepath.Join(sessionsDir, "d", "b.toml"), []byte(fmt.Sprintf(base, "b")), 0644)

	addr := "10.0.0.9:22"
	if err := svc.SaveHostKey("d", addr, "fp-1"); err != nil {
		t.Fatalf("SaveHostKey failed: %v", err)
	}

	// 删除一个会话后,仍存在同 addr 会话,文件夹 json 键保留
	if err := svc.DeleteSession("d/a.toml"); err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}
	ok, _ := svc.VerifyHostKey("d", addr, "fp-1")
	if !ok {
		t.Fatal("known_hosts key should remain while another session uses the address")
	}

	// 删除最后一个同 addr 会话后,文件夹 json 键清理
	if err := svc.DeleteSession("d/b.toml"); err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}
	ok, _ = svc.VerifyHostKey("d", addr, "fp-1")
	if ok {
		t.Fatal("known_hosts key should be removed after last session deleted")
	}
}

