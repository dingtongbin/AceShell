package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlobalKeyService_CreateAndList(t *testing.T) {
	withTestDataDir(t)
	g := &GlobalKeyService{}
	os.MkdirAll(sessionsDir, 0755)

	id1, err := g.CreateKey("mykey", "ed25519", "")
	if err != nil {
		t.Fatalf("CreateKey failed: %v", err)
	}
	if id1 == "" {
		t.Fatal("CreateKey returned empty id")
	}

	id2, err := g.CreateKey("mykey", "ed25519", "")
	if err != nil {
		t.Fatalf("CreateKey duplicate name failed: %v", err)
	}
	if id1 == id2 {
		t.Fatal("Duplicate name should create distinct keys")
	}

	// 重名文件名带序号
	if g.uniqueKeyFileName("mykey") != "mykey-3.json" {
		t.Fatalf("uniqueKeyFileName expected mykey-3.json, got %s", g.uniqueKeyFileName("mykey"))
	}

	list := g.ListKeys()
	if list == "" {
		t.Fatal("ListKeys returned empty")
	}
	var entries []GlobalKeyEntry
	if err := json.Unmarshal([]byte(list), &entries); err != nil {
		t.Fatalf("ListKeys JSON parse failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("Expected 2 keys, got %d", len(entries))
	}

	if err := g.DeleteKey(id1); err != nil {
		t.Fatalf("DeleteKey failed: %v", err)
	}
	if err := g.DeleteKey("nonexistent-id"); err == nil {
		t.Fatal("Expected error deleting nonexistent key")
	}
	list2 := g.ListKeys()
	var entries2 []GlobalKeyEntry
	json.Unmarshal([]byte(list2), &entries2)
	if len(entries2) != 1 {
		t.Fatalf("Expected 1 key after delete, got %d", len(entries2))
	}
}

func TestGlobalKeyService_CreateTypes(t *testing.T) {
	withTestDataDir(t)
	g := &GlobalKeyService{}
	os.MkdirAll(sessionsDir, 0755)

	for _, kt := range []string{"ed25519", "rsa2048", "rsa4096"} {
		id, err := g.CreateKey("type-"+kt, kt, "")
		if err != nil {
			t.Fatalf("CreateKey %s failed: %v", kt, err)
		}
		content, err := g.loadContent(id)
		if err != nil {
			t.Fatalf("loadContent %s failed: %v", kt, err)
		}
		if !strings.Contains(content.PrivateKey, "PRIVATE KEY") {
			t.Fatalf("%s private key should be PEM", kt)
		}
		if content.PublicKey == "" || content.Fingerprint == "" {
			t.Fatalf("%s public key/fingerprint empty", kt)
		}
	}

	if _, err := g.CreateKey("bad-type", "dsa", ""); err == nil {
		t.Fatal("Expected error for unsupported key type")
	}
}

func TestGlobalKeyService_PassphraseRoundTrip(t *testing.T) {
	withTestDataDir(t)
	g := &GlobalKeyService{}
	os.MkdirAll(sessionsDir, 0755)

	id, err := g.CreateKey("locked", "ed25519", "my-pass-123")
	if err != nil {
		t.Fatalf("CreateKey with passphrase failed: %v", err)
	}
	content, err := g.loadContent(id)
	if err != nil {
		t.Fatalf("loadContent failed: %v", err)
	}
	if content.Passphrase != "my-pass-123" {
		t.Fatalf("Passphrase mismatch: got %q", content.Passphrase)
	}
	// 落盘文件不得出现明文口令
	raw, err := os.ReadFile(g.keyFilePath("locked"))
	if err != nil {
		t.Fatalf("read key file failed: %v", err)
	}
	if strings.Contains(string(raw), "my-pass-123") {
		t.Fatal("Key file must not contain plaintext passphrase")
	}
	if !strings.HasPrefix(content.PrivateKey, "-----BEGIN") {
		t.Fatal("Stored private key should be encrypted PEM (begins with BEGIN)")
	}
}

func TestGlobalKeyService_ResolveName(t *testing.T) {
	withTestDataDir(t)
	g := &GlobalKeyService{}
	os.MkdirAll(sessionsDir, 0755)

	id, err := g.CreateKey("resolvable", "rsa2048", "")
	if err != nil {
		t.Fatalf("CreateKey failed: %v", err)
	}

	content, err := g.resolveName("key://resolvable")
	if err != nil {
		t.Fatalf("resolveName by name failed: %v", err)
	}
	if content.PrivateKey == "" {
		t.Fatal("resolveName returned empty private key")
	}

	if _, err := g.resolveName("key://不存在"); err == nil {
		t.Fatal("Expected error for unknown key name")
	}
	if _, err := g.resolveName("plain-text"); err == nil {
		t.Fatal("Expected error for non-key-reference")
	}

	if !IsGlobalKeyReference("key://abc") {
		t.Fatal("IsGlobalKeyReference should accept key:// prefix")
	}
	if IsGlobalKeyReference("abc") || IsGlobalKeyReference("") {
		t.Fatal("IsGlobalKeyReference should reject non-prefixed strings")
	}

	// 通过 uuid id 解析(导出携带 keyIDs 使用)
	content2, err := g.loadContent(id)
	if err != nil || content2.PrivateKey == "" {
		t.Fatalf("loadContent by id failed: %v", err)
	}
}

func TestGlobalKeyService_SshCopyKey_Unreachable(t *testing.T) {
	withTestDataDir(t)
	g := &GlobalKeyService{}
	os.MkdirAll(sessionsDir, 0755)

	id, err := g.CreateKey("copytest", "ed25519", "")
	if err != nil {
		t.Fatalf("CreateKey failed: %v", err)
	}

	// 不可达地址应返回错误而非 panic/卡死;空指纹 fail-close
	if _, err := g.SshCopyKey(id, "127.0.0.1", 1, "root", "x", ""); err == nil {
		t.Fatal("Expected error for unreachable host")
	}
	if _, err := g.SshCopyKey(id, "127.0.0.1", 22, "root", "x", ""); err == nil {
		t.Fatal("Expected error for empty host key (fail-close)")
	}
	if _, err := g.SshCopyKey("nonexistent-id", "127.0.0.1", 22, "root", "x", "AAAAC3NzaC1lZDI1NTE5AAAAItest"); err == nil {
		t.Fatal("Expected error for unknown key id")
	}
}

func TestCredentialKey_RoundTrip(t *testing.T) {
	withTestDataDir(t)

	// 加密解密往返(首次调用会创建主密钥文件)
	secret := "super-secret-密码-123"
	enc, err := encryptSecret(secret)
	if err != nil {
		t.Fatalf("encryptSecret failed: %v", err)
	}
	if !strings.HasPrefix(enc, secretPrefix) {
		t.Fatalf("encrypted secret should start with %s", secretPrefix)
	}
	if strings.Contains(enc, secret) {
		t.Fatal("encrypted secret must not contain plaintext")
	}
	dec, err := decryptSecret(enc)
	if err != nil {
		t.Fatalf("decryptSecret failed: %v", err)
	}
	if dec != secret {
		t.Fatalf("decrypt mismatch: got %q want %q", dec, secret)
	}

	// 主密钥文件应在数据目录创建,且不出现明文
	keyFile := filepath.Join(DataDir(), masterKeyFileName)
	if _, err := os.Stat(keyFile); os.IsNotExist(err) {
		t.Fatal("master key file not created")
	}
	raw, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatalf("read master key file failed: %v", err)
	}
	if !strings.HasPrefix(string(raw), masterKeyV1Prefix) && !strings.HasPrefix(string(raw), masterKeyV2Prefix) {
		t.Fatalf("master key file should start with %s or %s", masterKeyV1Prefix, masterKeyV2Prefix)
	}

	// 空值原样
	if v, _ := encryptSecret(""); v != "" {
		t.Fatal("empty secret should stay empty")
	}
	if v, _ := decryptSecret(""); v != "" {
		t.Fatal("empty stored should decrypt to empty")
	}

	// 损坏密文报错而非崩溃
	if _, err := decryptSecret(secretPrefix + "!!!not-base64!!!"); err == nil {
		t.Fatal("Expected error for corrupted ciphertext")
	}
}