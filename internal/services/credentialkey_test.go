package services

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// withTestMasterKeyEnv 将数据目录重定向到临时目录,并清空主密钥缓存。
func withTestMasterKeyEnv(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	restore := SetDataDir(dir)
	masterKeyMu.Lock()
	masterKeyCache = nil
	masterKeyCacheDir = ""
	masterKeyMu.Unlock()
	t.Cleanup(func() {
		restore()
		masterKeyMu.Lock()
		masterKeyCache = nil
		masterKeyCacheDir = ""
		masterKeyMu.Unlock()
	})
	return dir
}

func TestEncryptDecryptSecretRoundtrip(t *testing.T) {
	withTestMasterKeyEnv(t)
	for _, secret := range []string{"", "password-123", "中文密码🔐", strings.Repeat("x", 4096)} {
		enc, err := encryptSecret(secret)
		if err != nil {
			t.Fatalf("encryptSecret(%q): %v", secret, err)
		}
		if secret == "" {
			if enc != "" {
				t.Fatalf("空值应原样返回,得到 %q", enc)
			}
			continue
		}
		if !strings.HasPrefix(enc, secretPrefix) {
			t.Fatalf("密文缺少 %s 前缀: %q", secretPrefix, enc)
		}
		dec, err := decryptSecret(enc)
		if err != nil {
			t.Fatalf("decryptSecret: %v", err)
		}
		if dec != secret {
			t.Fatalf("roundtrip mismatch: got %q want %q", dec, secret)
		}
	}
}

func TestMasterKeyFileFormat(t *testing.T) {
	dir := withTestMasterKeyEnv(t)
	key1, err := loadMasterKey()
	if err != nil {
		t.Fatalf("loadMasterKey: %v", err)
	}
	if len(key1) != masterKeySize {
		t.Fatalf("密钥长度错误: %d", len(key1))
	}
	data, err := os.ReadFile(filepath.Join(dir, masterKeyFileName))
	if err != nil {
		t.Fatalf("读取密钥文件: %v", err)
	}
	// Windows 上应为 DPAPI(v2)格式;其他平台回退 v1。
	if runtime.GOOS == "windows" && !strings.HasPrefix(string(data), masterKeyV2Prefix) {
		t.Fatalf("Windows 上新密钥应为 %s 格式,实际前缀: %q", masterKeyV2Prefix, string(data[:10]))
	}
	// 再次加载应命中缓存且密钥一致。
	key2, err := loadMasterKey()
	if err != nil {
		t.Fatalf("第二次 loadMasterKey: %v", err)
	}
	if !bytes.Equal(key1, key2) {
		t.Fatal("两次加载的密钥不一致")
	}
}

// TestMasterKeyV1CompatAndMigration 构造 v1(机器派生密钥)格式的密钥文件:
// 加载必须成功解出原密钥;Windows 上还应自动迁移为 v2(DPAPI)格式。
func TestMasterKeyV1CompatAndMigration(t *testing.T) {
	dir := withTestMasterKeyEnv(t)
	legacyKey := bytes.Repeat([]byte{0xA7}, masterKeySize)
	blob, err := sealWithKey(legacyKey, deriveMachineKey())
	if err != nil {
		t.Fatalf("sealWithKey: %v", err)
	}
	v1 := []byte(masterKeyV1Prefix + base64.StdEncoding.EncodeToString(blob))
	path := filepath.Join(dir, masterKeyFileName)
	if err := os.WriteFile(path, v1, 0600); err != nil {
		t.Fatalf("写入 v1 密钥文件: %v", err)
	}

	got, err := loadMasterKey()
	if err != nil {
		t.Fatalf("加载 v1 密钥失败: %v", err)
	}
	if !bytes.Equal(got, legacyKey) {
		t.Fatal("v1 兼容解出的密钥与原密钥不一致")
	}

	if runtime.GOOS == "windows" {
		migrated, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读取迁移后的密钥文件: %v", err)
		}
		if !strings.HasPrefix(string(migrated), masterKeyV2Prefix) {
			t.Fatal("Windows 上 v1 密钥应自动迁移为 v2(DPAPI)格式")
		}
		// 迁移后再加载,仍应解出同一密钥。
		masterKeyMu.Lock()
		masterKeyCache = nil
		masterKeyCacheDir = ""
		masterKeyMu.Unlock()
		got2, err := loadMasterKey()
		if err != nil {
			t.Fatalf("加载迁移后(v2)密钥失败: %v", err)
		}
		if !bytes.Equal(got2, legacyKey) {
			t.Fatal("迁移后解出的密钥与原密钥不一致")
		}
	}
}

// TestMasterKeyCorruptFileRebuild 密钥文件损坏时应自动重建新密钥而非崩溃。
func TestMasterKeyCorruptFileRebuild(t *testing.T) {
	dir := withTestMasterKeyEnv(t)
	path := filepath.Join(dir, masterKeyFileName)
	if err := os.WriteFile(path, []byte("mk:v2:not-base64-!!!"), 0600); err != nil {
		t.Fatalf("写入损坏密钥文件: %v", err)
	}
	key, err := loadMasterKey()
	if err != nil {
		t.Fatalf("损坏文件应触发重建: %v", err)
	}
	if len(key) != masterKeySize {
		t.Fatalf("重建密钥长度错误: %d", len(key))
	}
}
