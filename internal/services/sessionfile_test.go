package services

import (
	"os"
	"encoding/base64"
	"path/filepath"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestSessionFileService_GetTree(t *testing.T) {
	withTestDataDir(t)
	svc := &SessionFileService{}
	svc.SetApp(nil)

	os.MkdirAll(sessionsDir, 0755)
	defer os.RemoveAll(sessionsDir)

	tomlContent := `[session]
name = "test"
host = "127.0.0.1"
port = 22
username = "root"
password = "pass"
protocol = "ssh"
`
	os.WriteFile(filepath.Join(sessionsDir, "test.toml"), []byte(tomlContent), 0644)
	os.MkdirAll(filepath.Join(sessionsDir, "folder1"), 0755)
	os.WriteFile(filepath.Join(sessionsDir, "folder1", "sub.toml"), []byte(tomlContent), 0644)

	tree := svc.GetTree()
	if tree == "" {
		t.Fatal("GetTree returned empty")
	}
	t.Logf("Tree: %s", tree)
}

// TestSessionFileService_DecryptLegacySalt 验证历史 KDF salt 加密的会话密文可被解密（KDF 迁移兼容）。
func TestSessionFileService_DecryptLegacySalt(t *testing.T) {
	withTestDataDir(t)
	svc := &SessionFileService{}
	svc.SetApp(nil)

	os.MkdirAll(sessionsDir, 0755)
	defer os.RemoveAll(sessionsDir)

	// 模拟旧版:用历史 KDF salt 机器密钥加密的裸密文(非 enc:v1 前缀)
	plain := "legacy-pass-123"
	blob, err := sealWithKey([]byte(plain), deriveMachineKeyWithSalt("FastNetShell-KDF-v2"))
	if err != nil {
		t.Fatalf("seal with legacy key failed: %v", err)
	}
	encoded := base64.StdEncoding.EncodeToString(blob)

	got, err := svc.decrypt(encoded)
	if err != nil {
		t.Fatalf("decrypt with current key + legacy fallback failed: %v", err)
	}
	if got != plain {
		t.Fatalf("decrypt mismatch: got %q want %q", got, plain)
	}
}

func TestSessionFileService_LoadSession(t *testing.T) {
	withTestDataDir(t)
	svc := &SessionFileService{}
	svc.SetApp(nil)

	os.MkdirAll(sessionsDir, 0755)
	defer os.RemoveAll(sessionsDir)

	tomlContent := `[session]
name = "test-ssh"
host = "192.168.1.1"
port = 22
username = "root"
password = "test123"
protocol = "ssh"
`
	os.WriteFile(filepath.Join(sessionsDir, "test.toml"), []byte(tomlContent), 0644)

	result, err := svc.LoadSession("test.toml")
	if err != nil {
		t.Fatalf("LoadSession failed: %v", err)
	}
	if result == "" {
		t.Fatal("LoadSession returned empty")
	}
	t.Logf("LoadSession result: %s", result)
}

func TestSessionFileService_LoadSession_NotFound(t *testing.T) {
	withTestDataDir(t)
	svc := &SessionFileService{}
	svc.SetApp(nil)

	_, err := svc.LoadSession("nonexistent.toml")
	if err == nil {
		t.Fatal("Expected error for nonexistent file")
	}
}

func TestSessionFileService_SaveSession(t *testing.T) {
	withTestDataDir(t)
	svc := &SessionFileService{}
	svc.SetApp(nil)

	os.MkdirAll(sessionsDir, 0755)
	defer os.RemoveAll(sessionsDir)

	tomlData := `name = "test-save"
host = "10.0.0.1"
port = 22
username = "admin"
password = "password123"
protocol = "ssh"
`
	err := svc.SaveSession("", tomlData)
	if err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}

	files, _ := os.ReadDir(sessionsDir)
	t.Logf("Files in sessions dir: %d", len(files))
	for _, f := range files {
		t.Logf("  - %s", f.Name())
	}

	found := false
	for _, f := range files {
		if f.Name() == "test-save.toml" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Saved file not found")
	}
}

func TestSessionFileService_UpdateSession_KeepPassword(t *testing.T) {
	withTestDataDir(t)
	svc := &SessionFileService{}
	svc.SetApp(nil)

	os.MkdirAll(sessionsDir, 0755)
	defer os.RemoveAll(sessionsDir)

	tomlData := `name = "keep-pass"
host = "10.0.0.1"
port = 23
username = "test"
password = "fake-secret-pass"
protocol = "telnet"
`
	if err := svc.SaveSession("", tomlData); err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}

	readPassword := func() string {
		content, err := os.ReadFile(filepath.Join(sessionsDir, "keep-pass.toml"))
		if err != nil {
			t.Fatalf("read session failed: %v", err)
		}
		var data SessionFileData
		if err := toml.Unmarshal(content, &data); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}
		if data.Session.Password == "" {
			return ""
		}
		plain, err := svc.decrypt(data.Session.Password)
		if err != nil {
			t.Fatalf("decrypt failed: %v", err)
		}
		return plain
	}

	if got := readPassword(); got != "fake-secret-pass" {
		t.Fatalf("initial password mismatch: got %q", got)
	}

	// 编辑时密码框留空(LoadSession 不回传明文):保存后原密码必须保留
	updateData := `name = "keep-pass"
host = "10.0.0.1"
port = 23
username = "test"
password = ""
protocol = "telnet"
`
	if err := svc.UpdateSession("keep-pass.toml", updateData); err != nil {
		t.Fatalf("UpdateSession failed: %v", err)
	}
	if got := readPassword(); got != "fake-secret-pass" {
		t.Fatalf("password lost after empty update: got %q", got)
	}

	// 显式填写新密码:覆盖旧密码
	updateData2 := `name = "keep-pass"
host = "10.0.0.1"
port = 23
username = "test"
password = "NewPass@456"
protocol = "telnet"
`
	if err := svc.UpdateSession("keep-pass.toml", updateData2); err != nil {
		t.Fatalf("UpdateSession failed: %v", err)
	}
	if got := readPassword(); got != "NewPass@456" {
		t.Fatalf("password not updated: got %q", got)
	}
}

func TestSessionFileService_DeleteSession(t *testing.T) {
	withTestDataDir(t)
	svc := &SessionFileService{}
	svc.SetApp(nil)

	os.MkdirAll(sessionsDir, 0755)
	defer os.RemoveAll(sessionsDir)

	os.WriteFile(filepath.Join(sessionsDir, "del.toml"), []byte("[session]\nname = \"del\"\n"), 0644)

	err := svc.DeleteSession("del.toml")
	if err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(sessionsDir, "del.toml")); !os.IsNotExist(err) {
		t.Fatal("File not deleted")
	}
}

func TestSessionFileService_DeleteSession_NotFound(t *testing.T) {
	withTestDataDir(t)
	svc := &SessionFileService{}
	svc.SetApp(nil)

	err := svc.DeleteSession("nonexistent.toml")
	if err == nil {
		t.Fatal("Expected error for nonexistent file")
	}
}

func TestSessionFileService_CreateFolder(t *testing.T) {
	withTestDataDir(t)
	svc := &SessionFileService{}
	svc.SetApp(nil)

	os.MkdirAll(sessionsDir, 0755)
	defer os.RemoveAll(sessionsDir)

	err := svc.CreateFolder("new-folder")
	if err != nil {
		t.Fatalf("CreateFolder failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(sessionsDir, "new-folder")); os.IsNotExist(err) {
		t.Fatal("Folder not created")
	}
}

func TestSessionFileService_DeleteFolder(t *testing.T) {
	withTestDataDir(t)
	svc := &SessionFileService{}
	svc.SetApp(nil)

	os.MkdirAll(sessionsDir, 0755)
	defer os.RemoveAll(sessionsDir)

	os.MkdirAll(filepath.Join(sessionsDir, "del-folder"), 0755)

	err := svc.DeleteFolder("del-folder")
	if err != nil {
		t.Fatalf("DeleteFolder failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(sessionsDir, "del-folder")); !os.IsNotExist(err) {
		t.Fatal("Folder not deleted")
	}

	// 密钥库目录不可删除
	os.MkdirAll(filepath.Join(sessionsDir, "key"), 0755)
	if err := svc.DeleteFolder("key"); err == nil {
		t.Fatal("Expected error deleting key dir")
	}
	if _, err := os.Stat(filepath.Join(sessionsDir, "key")); os.IsNotExist(err) {
		t.Fatal("Key dir should not be deleted")
	}
}

func TestSessionFileService_RenameItem(t *testing.T) {
	withTestDataDir(t)
	svc := &SessionFileService{}
	svc.SetApp(nil)

	os.MkdirAll(sessionsDir, 0755)
	defer os.RemoveAll(sessionsDir)

	os.WriteFile(filepath.Join(sessionsDir, "old.toml"), []byte("[session]\nname = \"old\"\n"), 0644)

	err := svc.RenameItem("old.toml", "new")
	if err != nil {
		t.Fatalf("RenameItem failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(sessionsDir, "new.toml")); os.IsNotExist(err) {
		t.Fatal("File not renamed")
	}
}

func TestSessionFileService_MoveFile(t *testing.T) {
	withTestDataDir(t)
	svc := &SessionFileService{}
	svc.SetApp(nil)

	os.MkdirAll(sessionsDir, 0755)
	defer os.RemoveAll(sessionsDir)

	os.WriteFile(filepath.Join(sessionsDir, "move.toml"), []byte("[session]\nname = \"move\"\n"), 0644)
	os.MkdirAll(filepath.Join(sessionsDir, "dest"), 0755)

	err := svc.MoveFile("move.toml", "dest")
	if err != nil {
		t.Fatalf("MoveFile failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(sessionsDir, "dest", "move.toml")); os.IsNotExist(err) {
		t.Fatal("File not moved")
	}
}

func TestSessionFileService_MoveFolder(t *testing.T) {
	withTestDataDir(t)
	svc := &SessionFileService{}
	svc.SetApp(nil)

	os.MkdirAll(sessionsDir, 0755)
	defer os.RemoveAll(sessionsDir)

	os.MkdirAll(filepath.Join(sessionsDir, "src-folder"), 0755)
	os.WriteFile(filepath.Join(sessionsDir, "src-folder", "test.toml"), []byte("[session]\nname = \"test\"\n"), 0644)
	os.MkdirAll(filepath.Join(sessionsDir, "dest"), 0755)

	err := svc.MoveFolder("src-folder", "dest")
	if err != nil {
		t.Fatalf("MoveFolder failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(sessionsDir, "dest", "src-folder", "test.toml")); os.IsNotExist(err) {
		t.Fatal("Folder not moved")
	}
}

func TestSessionFileService_EncryptDecrypt(t *testing.T) {
	withTestDataDir(t)
	svc := &SessionFileService{}
	svc.SetApp(nil)

	password := "MySecurePass123"
	encrypted, err := svc.encrypt(password)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	decrypted, err := svc.decrypt(encrypted)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}

	if decrypted != password {
		t.Fatalf("Password mismatch: expected %q, got %q", password, decrypted)
	}
}

func TestSessionFileService_Encrypt_EmptyPassword(t *testing.T) {
	withTestDataDir(t)
	svc := &SessionFileService{}
	svc.SetApp(nil)

	encrypted, err := svc.encrypt("")
	if err != nil {
		t.Fatalf("encrypt empty password failed: %v", err)
	}

	decrypted, err := svc.decrypt(encrypted)
	if err != nil {
		t.Fatalf("decrypt empty password failed: %v", err)
	}

	if decrypted != "" {
		t.Fatalf("Expected empty password, got %q", decrypted)
	}
}

func TestSessionFileService_Encrypt_SpecialChars(t *testing.T) {
	withTestDataDir(t)
	svc := &SessionFileService{}
	svc.SetApp(nil)

	passwords := []string{
		"p@ss!#$%^&*()",
		"中文密码",
		"very long password with many characters 1234567890",
		"a",
	}

	for _, pwd := range passwords {
		encrypted, err := svc.encrypt(pwd)
		if err != nil {
			t.Fatalf("encrypt %q failed: %v", pwd, err)
		}
		decrypted, err := svc.decrypt(encrypted)
		if err != nil {
			t.Fatalf("decrypt %q failed: %v", pwd, err)
		}
		if decrypted != pwd {
			t.Fatalf("Password mismatch for %q: got %q", pwd, decrypted)
		}
	}
}

func TestSessionFileService_SetNoConfirmClose(t *testing.T) {
	withTestDataDir(t)
	svc := &SessionFileService{}
	svc.SetApp(nil)

	os.MkdirAll(sessionsDir, 0755)
	defer os.RemoveAll(sessionsDir)

	tomlContent := `[session]
name = "test"
host = "127.0.0.1"
port = 22
username = "root"
password = "pass"
protocol = "ssh"
`
	os.WriteFile(filepath.Join(sessionsDir, "test.toml"), []byte(tomlContent), 0644)

	err := svc.SetNoConfirmClose("test.toml", true)
	if err != nil {
		t.Fatalf("SetNoConfirmClose failed: %v", err)
	}

	result, err := svc.LoadSession("test.toml")
	if err != nil {
		t.Fatalf("LoadSession failed: %v", err)
	}
	t.Logf("Session after SetNoConfirmClose: %s", result)
}

func TestSessionFileData_Structure(t *testing.T) {
	data := SessionFileData{
		Session: SessionInfo{
			Name:     "test",
			Host:     "192.168.1.1",
			Port:     22,
			Username: "root",
			Password: "encrypted",
			Protocol: "ssh",
			Notes:    "test notes",
			Created:  "2024-01-01",
			Updated:  "2024-01-01",
		},
	}

	if data.Session.Name != "test" {
		t.Fatalf("Expected name 'test', got %q", data.Session.Name)
	}
	if data.Session.Port != 22 {
		t.Fatalf("Expected port 22, got %d", data.Session.Port)
	}
}

func TestTreeNode_Structure(t *testing.T) {
	node := TreeNode{
		Name:     "folder",
		Path:     "folder",
		IsDir:    true,
		Protocol: "",
		Children: []*TreeNode{
			{Name: "test.toml", Path: "folder/test.toml", IsDir: false, Protocol: "ssh"},
		},
	}

	if !node.IsDir {
		t.Fatal("Expected IsDir=true")
	}
	if len(node.Children) != 1 {
		t.Fatalf("Expected 1 child, got %d", len(node.Children))
	}
	if node.Children[0].Protocol != "ssh" {
		t.Fatalf("Expected protocol 'ssh', got %q", node.Children[0].Protocol)
	}
}

func TestSessionMeta_Structure(t *testing.T) {
	meta := SessionMeta{
		Name:           "test",
		Host:           "10.0.0.1",
		Port:           22,
		Username:       "admin",
		Protocol:       "ssh",
		Created:        "2024-01-01",
		Updated:        "2024-01-01",
		NoConfirmClose: true,
	}

	if meta.Name != "test" {
		t.Fatalf("Expected name 'test', got %q", meta.Name)
	}
	if !meta.NoConfirmClose {
		t.Fatal("Expected NoConfirmClose=true")
	}
}

func TestSessionFileService_PathTraversal(t *testing.T) {
	// 必须先隔离数据目录,否则下方 RemoveAll 会删除真实 sessions 目录
	withTestDataDir(t)
	os.RemoveAll(sessionsDir)
	os.MkdirAll(sessionsDir, 0755)
	defer os.RemoveAll(sessionsDir)

	svc := &SessionFileService{}

	// 路径遍历应该被阻止
	err := svc.CreateFolder("../escape-test")
	if err == nil {
		t.Fatal("Expected error for path traversal")
	}

	// 合法路径应该成功
	err = svc.CreateFolder("valid-folder")
	if err != nil {
		t.Fatalf("Expected success for valid path, got: %v", err)
	}

	// 验证 escape-test 目录未在 sessions 外创建
	if _, err := os.Stat(filepath.Join(sessionsDir, "..", "escape-test")); !os.IsNotExist(err) {
		t.Fatal("Path traversal created directory outside sessions!")
	}

	// 验证 valid-folder 在 sessions 内创建
	if _, err := os.Stat(filepath.Join(sessionsDir, "valid-folder")); os.IsNotExist(err) {
		t.Fatal("Valid folder not created in sessions")
	}
}

func TestSessionFileService_SafeSessionPath(t *testing.T) {
	withTestDataDir(t)
	svc := &SessionFileService{}

	tests := []struct {
		name    string
		subPath string
		wantErr bool
	}{
		{"valid subfolder", "sub", false},
		{"valid nested", "a/b/c", false},
		{"empty path", "", false},
		{"dot path", ".", false},
		{"traversal with ../", "../escape", true},
		{"traversal deep", "a/../../../escape", true},
		{"traversal with backslash", "..\\escape", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.safeSessionPath(tt.subPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("safeSessionPath(%q) error = %v, wantErr %v", tt.subPath, err, tt.wantErr)
			}
		})
	}
}
