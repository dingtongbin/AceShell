package services

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
)

func TestSessionFileService_ExportImport(t *testing.T) {
	withTestDataDir(t)
	svc := &SessionFileService{}
	svc.SetApp(nil)

	os.MkdirAll(sessionsDir, 0755)
	defer os.RemoveAll(sessionsDir)

	tomlContent := `[session]
name = "export-test"
host = "10.0.0.1"
port = 22
username = "root"
password = "secret"
protocol = "ssh"
`
	os.WriteFile(filepath.Join(sessionsDir, "export.toml"), []byte(tomlContent), 0644)

	exportPath := filepath.Join(sessionsDir, "export.as9")
	err := svc.ExportSessions([]string{"export.toml"}, "TestPass123", exportPath, []string{})
	if err != nil {
		t.Fatalf("ExportSessions failed: %v", err)
	}

	if _, err := os.Stat(exportPath); os.IsNotExist(err) {
		t.Fatal("Export file not created")
	}

	exportData, _ := os.ReadFile(exportPath)
	t.Logf("Export file size: %d bytes", len(exportData))
	if len(exportData) < 9 || string(exportData[:8]) != "ACEAS9V1" {
		t.Fatalf("Expected as9 package header ACEAS9V1, got %x", exportData[:min(9, len(exportData))])
	}
}

// TestValidateExportPassword 楠岃瘉 as9 鍖呭彛浠よ鍒?蹇呭～銆?~64 浣嶃€佸洓绫讳腑鑷冲皯涓夌被銆?
func TestValidateExportPassword(t *testing.T) {
	tests := []struct {
		name    string
		pass    string
		wantErr bool
	}{
		{"empty", "", true},
		{"too short", "Pw123", true},
		{"single category", "abcdefgh", true},
		{"digits only", "0123456789", true},
		{"65 digits", "01234567890123456789012345678901234567890123456789012345678901234", true},
		{"two categories", "abc12345", true},
		{"three categories", "Abc12345", false},
		{"four categories", "Abc123!@", false},
		{"64 chars valid", "Aa1!" + strings.Repeat("x", 60), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateExportPassword(tt.pass)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateExportPassword(%q) error = %v, wantErr %v", tt.pass, err, tt.wantErr)
			}
		})
	}
}

func TestSessionFileService_ExportPasswordRules(t *testing.T) {
	withTestDataDir(t)
	svc := &SessionFileService{}
	svc.SetApp(nil)

	os.MkdirAll(sessionsDir, 0755)
	defer os.RemoveAll(sessionsDir)

	exportPath := filepath.Join(sessionsDir, "rules.as9")
	// 鍙ｄ护鍚堟硶鏃跺厛鎶ユ枃浠朵笉瀛樺湪(璇存槑鍙ｄ护鏍￠獙宸查€氳繃)
	if err := svc.ExportSessions([]string{"nope.toml"}, "Abc12345", exportPath, []string{}); err == nil {
		t.Fatal("Expected error for missing file, got nil")
	}

	// 鍙ｄ护涓嶆弧瓒宠鍒欐椂,鍏堟姤鍙ｄ护閿欒(璇存槑鍙ｄ护鏍￠獙浼樺厛浜庢枃浠跺鐞?
	if err := svc.ExportSessions([]string{"nope.toml"}, "abc", exportPath, []string{}); err == nil {
		t.Fatal("Expected error for short password, got nil")
	}
	if err := svc.ExportSessions([]string{"nope.toml"}, "", exportPath, []string{}); err == nil {
		t.Fatal("Expected error for empty password, got nil")
	}
}

func TestSessionFileService_ExportAndImportWithReencrypt(t *testing.T) {
	withTestDataDir(t)
	svc := &SessionFileService{}
	svc.SetApp(nil)

	os.MkdirAll(sessionsDir, 0755)
	defer os.RemoveAll(sessionsDir)

	tomlContent := `[session]
name = "plain-export"
host = "10.0.0.2"
port = 22
username = "root"
password = "plain-secret"
protocol = "ssh"
`
	os.WriteFile(filepath.Join(sessionsDir, "plain.toml"), []byte(tomlContent), 0644)

	exportPath := filepath.Join(sessionsDir, "plain.as9")
	if err := svc.ExportSessions([]string{"plain.toml"}, "Pw12345a", exportPath, []string{}); err != nil {
		t.Fatalf("ExportSessions failed: %v", err)
	}

	data, _ := os.ReadFile(exportPath)
	if len(data) < 9 || string(data[:8]) != "ACEAS9V1" {
		t.Fatalf("Expected as9 header ACEAS9V1, got %x", data[:min(9, len(data))])
	}

	if _, err := svc.readAndDecrypt(exportPath, "wrong-pass"); err == nil {
		t.Fatal("Expected error with wrong password")
	}

	plaintext, err := svc.readAndDecrypt(exportPath, "Pw12345a")
	if err != nil {
		t.Fatalf("readAndDecrypt failed: %v", err)
	}
	if !strings.Contains(string(plaintext), "plain-secret") {
		t.Fatal("Decrypted package should contain plaintext password")
	}

	importDir := filepath.Join(sessionsDir, "imported")
	if _, _, err := svc.extractTarArchive(plaintext, importDir, false, nil); err != nil {
		t.Fatalf("extractTarArchive failed: %v", err)
	}

	imported, err := os.ReadFile(filepath.Join(importDir, "plain.toml"))
	if err != nil {
		t.Fatalf("imported session not found: %v", err)
	}

	var data2 SessionFileData
	if err := toml.Unmarshal(imported, &data2); err != nil {
		t.Fatalf("imported toml parse failed: %v", err)
	}
	if data2.Session.Password == "plain-secret" {
		t.Fatal("Imported password should be re-encrypted, not plaintext")
	}
	dec, err := svc.decrypt(data2.Session.Password)
	if err != nil {
		t.Fatalf("decrypt imported password failed: %v", err)
	}
	if dec != "plain-secret" {
		t.Fatalf("decrypted password mismatch: got %q", dec)
	}
}

func TestSessionFileService_ImportOverwritePolicy(t *testing.T) {
	withTestDataDir(t)
	svc := &SessionFileService{}
	svc.SetApp(nil)

	os.MkdirAll(sessionsDir, 0755)
	defer os.RemoveAll(sessionsDir)

	tomlContent := `[session]
name = "dup"
host = "10.0.0.3"
port = 22
username = "root"
password = "new-secret"
protocol = "ssh"
`
	os.WriteFile(filepath.Join(sessionsDir, "dup.toml"), []byte(tomlContent), 0644)
	exportPath := filepath.Join(sessionsDir, "dup.as9")
	if err := svc.ExportSessions([]string{"dup.toml"}, "Abc12345", exportPath, []string{}); err != nil {
		t.Fatalf("ExportSessions failed: %v", err)
	}

	plaintext, err := svc.readAndDecrypt(exportPath, "Abc12345")
	if err != nil {
		t.Fatalf("readAndDecrypt failed: %v", err)
	}

	os.MkdirAll(filepath.Join(sessionsDir, "target"), 0755)
	oldContent := `[session]
name = "dup"
host = "10.0.0.99"
port = 22
username = "root"
password = "old-secret"
protocol = "ssh"
`
	os.WriteFile(filepath.Join(sessionsDir, "target", "dup.toml"), []byte(oldContent), 0644)

	if _, _, err := svc.extractTarArchive(plaintext, filepath.Join(sessionsDir, "target"), false, nil); err != nil {
		t.Fatalf("extract skip failed: %v", err)
	}
	skipped, _ := os.ReadFile(filepath.Join(sessionsDir, "target", "dup.toml"))
	if !strings.Contains(string(skipped), "old-secret") {
		t.Fatal("Skip policy should keep existing file untouched")
	}

	if _, _, err := svc.extractTarArchive(plaintext, filepath.Join(sessionsDir, "target"), true, nil); err != nil {
		t.Fatalf("extract overwrite failed: %v", err)
	}
	overwritten, _ := os.ReadFile(filepath.Join(sessionsDir, "target", "dup.toml"))
	var data SessionFileData
	if err := toml.Unmarshal(overwritten, &data); err != nil {
		t.Fatalf("overwritten toml parse failed: %v", err)
	}
	dec, err := svc.decrypt(data.Session.Password)
	if err != nil || dec != "new-secret" {
		t.Fatalf("Overwrite policy should replace with imported (decrypted=%q err=%v)", dec, err)
	}
}

func TestSessionFileService_GetImportPackageTree(t *testing.T) {
	withTestDataDir(t)
	svc := &SessionFileService{}
	svc.SetApp(nil)

	os.MkdirAll(sessionsDir, 0755)
	defer os.RemoveAll(sessionsDir)

	os.MkdirAll(filepath.Join(sessionsDir, "sub", "deep"), 0755)
	tomlContent := `[session]
name = "nested"
host = "10.0.0.4"
port = 22
username = "root"
password = ""
protocol = "ssh"
`
	os.WriteFile(filepath.Join(sessionsDir, "sub", "a.toml"), []byte(tomlContent), 0644)
	os.WriteFile(filepath.Join(sessionsDir, "sub", "deep", "b.toml"), []byte(tomlContent), 0644)

	exportPath := filepath.Join(sessionsDir, "tree.as9")
	if err := svc.ExportSessions([]string{"sub"}, "Abc12345", exportPath, []string{}); err != nil {
		t.Fatalf("ExportSessions failed: %v", err)
	}

	jsonStr := svc.GetImportPackageTree(exportPath, "Abc12345")
	var nodes []*TreeNode
	if err := json.Unmarshal([]byte(jsonStr), &nodes); err != nil {
		t.Fatalf("tree parse failed: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Name != "sub" {
		t.Fatalf("Expected root folder sub, got %+v", nodes)
	}
	var deepFound bool
	for _, c := range nodes[0].Children {
		if c.Name == "deep" {
			deepFound = true
		}
	}
	if !deepFound {
		t.Fatal("Expected nested folder deep inside sub")
	}
}

// TestSessionFileService_ExportImportWithKeys 楠岃瘉 as9 鍖呮惡甯﹀瘑閽ュ鍑轰笌瀵煎叆:
// 瀵煎嚭绉侀挜瑙ｅ瘑鍏ュ寘;瀵煎叆鍚庡瘑閽ラ噸鏂拌惤搴?绉侀挜缁忎富瀵嗛挜鍔犲瘑),涓斿彲琚?resolveName 浣跨敤銆?
func TestSessionFileService_ExportImportWithKeys(t *testing.T) {
	withTestDataDir(t)
	svc := &SessionFileService{}
	svc.SetApp(nil)
	svc.GlobalKeys = &GlobalKeyService{}
	os.MkdirAll(sessionsDir, 0755)

	keyID, err := svc.GlobalKeys.CreateKey("packkey", "ed25519", "")
	if err != nil {
		t.Fatalf("CreateKey failed: %v", err)
	}

	tomlContent := `[session]
name = "withkey"
host = "10.0.0.6"
port = 22
username = "root"
password = "withkey-secret"
protocol = "ssh"
`
	os.WriteFile(filepath.Join(sessionsDir, "withkey.toml"), []byte(tomlContent), 0644)

	exportPath := filepath.Join(sessionsDir, "withkey.as9")
	if err := svc.ExportSessions([]string{"withkey.toml"}, "Abc12345", exportPath, []string{keyID}); err != nil {
		t.Fatalf("ExportSessions with keys failed: %v", err)
	}

	plaintext, err := svc.readAndDecrypt(exportPath, "Abc12345")
	if err != nil {
		t.Fatalf("readAndDecrypt failed: %v", err)
	}
	if !strings.Contains(string(plaintext), "PRIVATE KEY") {
		t.Fatal("Package should contain plaintext private key")
	}

	keysJSON := svc.GetImportPackageKeys(exportPath, "Abc12345")
	if !strings.Contains(keysJSON, "packkey") {
		t.Fatalf("GetImportPackageKeys should list packkey, got %s", keysJSON)
	}

	// 瀵煎叆鍒?in 鏂囦欢澶?瀵嗛挜搴旈噸鏂拌惤搴?
	if _, _, err := svc.extractTarArchive(plaintext, filepath.Join(sessionsDir, "in"), false, nil); err != nil {
		t.Fatalf("extractTarArchive failed: %v", err)
	}
	keyDir := filepath.Join(sessionsDir, "key")
	entries, err := os.ReadDir(keyDir)
	if err != nil {
		t.Fatalf("key dir should exist after import: %v", err)
	}
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "packkey") {
			found = true
			raw, _ := os.ReadFile(filepath.Join(keyDir, e.Name()))
			if !strings.Contains(string(raw), secretPrefix) {
				t.Fatal("Imported key private key should be re-encrypted with master key")
			}
		}
	}
	if !found {
		t.Fatal("Imported key not found in key dir")
	}

	// 瀵煎叆鐨勫瘑閽ュ彲琚紩鐢ㄤ娇鐢?resolveName 杩斿洖鐨勭閽ュ凡鏄槑鏂?PEM)
	content, err := svc.GlobalKeys.resolveName("key://packkey")
	if err != nil || content.PrivateKey == "" {
		t.Fatalf("imported key not resolvable: err=%v", err)
	}
	if !strings.Contains(content.PrivateKey, "PRIVATE KEY") {
		t.Fatalf("imported key private key should be plaintext PEM, got %q", content.PrivateKey[:min(40, len(content.PrivateKey))])
	}
}

// TestSessionFileService_FingerprintDualWrite 楠岃瘉鎸囩汗鍙屽啓涓庢牎楠?
// SaveHostKey 鍐欏叆鏂囦欢澶?json 涓庝細璇濇枃浠?VerifyHostKey 涓ょ鏉ユ簮鍧囧彲鍛戒腑;涓嶄竴鑷存嫆缁濄€?
func TestSessionFileService_ImportEncryptedPackage(t *testing.T) {
	withTestDataDir(t)
	svc := &SessionFileService{}
	svc.SetApp(nil)

	os.MkdirAll(sessionsDir, 0755)
	defer os.RemoveAll(sessionsDir)

	tomlContent := `[session]
name = "enc-import"
host = "10.0.0.5"
port = 22
username = "root"
password = "enc-secret"
protocol = "ssh"
`
	os.WriteFile(filepath.Join(sessionsDir, "enc.toml"), []byte(tomlContent), 0644)
	exportPath := filepath.Join(sessionsDir, "enc.as9")
	if err := svc.ExportSessions([]string{"enc.toml"}, "Abc12345", exportPath, []string{}); err != nil {
		t.Fatalf("ExportSessions failed: %v", err)
	}

	if _, err := svc.readAndDecrypt(exportPath, "wrong"); err == nil {
		t.Fatal("Expected error with wrong password")
	}

	plaintext, err := svc.readAndDecrypt(exportPath, "Abc12345")
	if err != nil {
		t.Fatalf("readAndDecrypt failed: %v", err)
	}
	if !strings.Contains(string(plaintext), "enc-secret") {
		t.Fatal("Encrypted package should contain plaintext password after decrypt")
	}
}


func TestAcquireImportLock_Conflict(t *testing.T) {
	withTestDataDir(t)
	os.MkdirAll(sessionsDir, 0755)

	release, err := acquireImportLock()
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	if _, err := acquireImportLock(); err == nil {
		t.Fatal("expected lock conflict while held")
	}
	release()
	release2, err := acquireImportLock()
	if err != nil {
		t.Fatalf("acquire after release failed: %v", err)
	}
	release2()
}

func TestAcquireImportLock_StaleCleanup(t *testing.T) {
	withTestDataDir(t)
	os.MkdirAll(sessionsDir, 0755)

	old := importLockMaxAge
	importLockMaxAge = time.Nanosecond
	defer func() { importLockMaxAge = old }()

	if err := os.WriteFile(importLockPath(), []byte("123"), 0644); err != nil {
		t.Fatalf("create stale lock failed: %v", err)
	}
	past := time.Now().Add(-time.Minute)
	if err := os.Chtimes(importLockPath(), past, past); err != nil {
		t.Fatalf("chtimes failed: %v", err)
	}
	release, err := acquireImportLock()
	if err != nil {
		t.Fatalf("stale lock should be cleaned: %v", err)
	}
	release()
}

func TestExtractTarArchive_SelectedPaths(t *testing.T) {
	withTestDataDir(t)
	svc := &SessionFileService{}
	svc.SetApp(nil)
	os.MkdirAll(sessionsDir, 0755)
	defer os.RemoveAll(sessionsDir)

	os.WriteFile(filepath.Join(sessionsDir, "keep.toml"), []byte("[session]\nname=\"keep\"\n"), 0644)
	os.MkdirAll(filepath.Join(sessionsDir, "sub"), 0755)
	os.WriteFile(filepath.Join(sessionsDir, "sub", "skip.toml"), []byte("[session]\nname=\"skip\"\n"), 0644)

	var buf bytes.Buffer
	if err := svc.writeTarArchive([]string{"keep.toml", "sub"}, nil, &buf); err != nil {
		t.Fatalf("pack failed: %v", err)
	}

	dest := filepath.Join(sessionsDir, "dest")
	os.MkdirAll(dest, 0755)
	// 仅选中 keep.toml:sub/skip.toml 不应被解压
	if _, _, err := svc.extractTarArchive(buf.Bytes(), dest, false, []string{"keep.toml"}); err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "keep.toml")); err != nil {
		t.Fatal("keep.toml should exist")
	}
	if _, err := os.Stat(filepath.Join(dest, "sub", "skip.toml")); !os.IsNotExist(err) {
		t.Fatal("sub/skip.toml should not exist")
	}
}

func TestBuildPackageDirTree_IncludesFiles(t *testing.T) {
	withTestDataDir(t)
	svc := &SessionFileService{}
	svc.SetApp(nil)
	os.MkdirAll(sessionsDir, 0755)
	defer os.RemoveAll(sessionsDir)

	os.WriteFile(filepath.Join(sessionsDir, "a.toml"), []byte("[session]\nname=\"a\"\n"), 0644)
	os.MkdirAll(filepath.Join(sessionsDir, "dir"), 0755)
	os.WriteFile(filepath.Join(sessionsDir, "dir", "b.toml"), []byte("[session]\nname=\"b\"\n"), 0644)

	var buf bytes.Buffer
	if err := svc.writeTarArchive([]string{"a.toml", "dir"}, nil, &buf); err != nil {
		t.Fatalf("pack failed: %v", err)
	}
	tree := buildPackageDirTree(buf.Bytes())
	if len(tree) == 0 {
		t.Fatal("tree should not be empty")
	}
	// 椤跺眰搴旀湁鏂囦欢 a.toml
	foundFile := false
	var dirNode *TreeNode
	for _, n := range tree {
		if n.IsDir {
			dirNode = n
		} else if n.Name == "a.toml" {
			foundFile = true
		}
	}
	if !foundFile {
		t.Fatal("tree should include session file a.toml")
	}
	if dirNode == nil || len(dirNode.Children) == 0 || dirNode.Children[0].Name != "b.toml" {
		t.Fatalf("dir should include session file b.toml, got %+v", dirNode)
	}
}

func TestSessionFileService_ExportImportKeys(t *testing.T) {
	withTestDataDir(t)
	svc := &SessionFileService{}
	svc.SetApp(nil)
	svc.GlobalKeys = &GlobalKeyService{}
	os.MkdirAll(sessionsDir, 0755)
	defer os.RemoveAll(sessionsDir)

	// 创建密钥与一个会话
	os.MkdirAll(filepath.Join(sessionsDir, keysDirName), 0755)
	if _, err := svc.GlobalKeys.CreateKey("pack-key", "ed25519", ""); err != nil {
		t.Fatalf("CreateKey failed: %v", err)
	}
	os.WriteFile(filepath.Join(sessionsDir, "sess.toml"), []byte("[session]\nname=\"sess\"\n"), 0644)

	// 导出:勾选会话 + key 目录
	exportPath := filepath.Join(sessionsDir, "keys.as9")
	if err := svc.ExportSessions([]string{"sess.toml", keysDirName}, "Abc12345", exportPath, nil); err != nil {
		t.Fatalf("ExportSessions failed: %v", err)
	}

	// 包内树应含 keys 目录与密钥文件
	plaintext, err := svc.readAndDecrypt(exportPath, "Abc12345")
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}
	tree := buildPackageDirTree(plaintext)
	keysNodeFound := false
	var walk func(nodes []*TreeNode)
	walk = func(nodes []*TreeNode) {
		for _, n := range nodes {
			if n.IsDir && n.Path == "keys" {
				keysNodeFound = true
			}
			if len(n.Children) == 0 { continue }
			walk(n.Children)
		}
	}
	walk(tree)
	if !keysNodeFound {
		t.Fatal("package tree should include keys folder")
	}

	// 导入:覆盖策略写入新密钥库目录(先清除导出侧留下的密钥,模拟目标机)
	os.RemoveAll(filepath.Join(sessionsDir, keysDirName))
	importSvc := &SessionFileService{}
	importSvc.SetApp(nil)
	importSvc.GlobalKeys = &GlobalKeyService{}
	summary, err := importSvc.ImportSessions("Abc12345", ".", exportPath, false, []string{"sess.toml", "keys"})
	if err != nil {
		t.Fatalf("ImportSessions failed: %v", err)
	}
	if !strings.Contains(summary, "密钥导入 1 个") {
		t.Fatalf("expected key import summary, got: %s", summary)
	}
	if _, err := os.Stat(filepath.Join(importSvc.GlobalKeys.keysDir(), "pack-key.json")); err != nil {
		t.Fatal("imported key should exist in keys dir")
	}

	// 再次导入(skip 策略):同名密钥应跳过
	summary2, err := importSvc.ImportSessions("Abc12345", ".", exportPath, false, []string{"keys"})
	if err != nil {
		t.Fatalf("ImportSessions 2 failed: %v", err)
	}
	if !strings.Contains(summary2, "密钥跳过 1 个") {
		t.Fatalf("expected key skip summary, got: %s", summary2)
	}
}
