package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScriptFileService_CreateFile(t *testing.T) {
	withTestDataDir(t)
	svc := &ScriptFileService{}
	os.MkdirAll(ScriptsDir(), 0755)

	err := svc.CreateFile("test.txt")
	if err != nil {
		t.Fatalf("CreateFile failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(ScriptsDir(), "test.txt")); os.IsNotExist(err) {
		t.Fatal("File not created")
	}
}

func TestScriptFileService_WriteReadFile(t *testing.T) {
	withTestDataDir(t)
	svc := &ScriptFileService{}
	os.MkdirAll(ScriptsDir(), 0755)

	content := "hello world"
	err := svc.WriteFile("test.txt", content)
	if err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	read, err := svc.ReadFile("test.txt")
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	if read != content {
		t.Fatalf("Content mismatch: got %q, want %q", read, content)
	}
}

func TestScriptFileService_ListFiles(t *testing.T) {
	withTestDataDir(t)
	svc := &ScriptFileService{}
	os.MkdirAll(filepath.Join(ScriptsDir(), "sub"), 0755)
	os.WriteFile(filepath.Join(ScriptsDir(), "a.txt"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(ScriptsDir(), "sub", "b.txt"), []byte("b"), 0644)

	result := svc.ListFiles("")
	if result == "[]" {
		t.Fatal("Expected files, got empty")
	}
}

func TestScriptFileService_GetLanguage(t *testing.T) {
	svc := &ScriptFileService{}
	tests := []struct{ name, want string }{
		{"app.js", "javascript"},
		{"main.py", "python"},
		{"index.html", "html"},
		{"style.css", "css"},
		{"data.json", "json"},
		{"readme.md", "markdown"},
		{"run.sh", "shell"},
		{"main.go", "go"},
		{"unknown.xyz", "text"},
	}
	for _, tt := range tests {
		got := svc.GetLanguage(tt.name)
		if got != tt.want {
			t.Errorf("GetLanguage(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestScriptFileService_CreateDir(t *testing.T) {
	withTestDataDir(t)
	svc := &ScriptFileService{}
	os.MkdirAll(ScriptsDir(), 0755)

	err := svc.CreateDir("mydir/sub")
	if err != nil {
		t.Fatalf("CreateDir failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(ScriptsDir(), "mydir", "sub")); os.IsNotExist(err) {
		t.Fatal("Dir not created")
	}
}

func TestScriptFileService_DeleteFile(t *testing.T) {
	withTestDataDir(t)
	svc := &ScriptFileService{}
	os.MkdirAll(ScriptsDir(), 0755)
	os.WriteFile(filepath.Join(ScriptsDir(), "del.txt"), []byte("x"), 0644)

	err := svc.DeleteFile("del.txt")
	if err != nil {
		t.Fatalf("DeleteFile failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(ScriptsDir(), "del.txt")); !os.IsNotExist(err) {
		t.Fatal("File not deleted")
	}
}

func TestScriptFileService_RenameFile(t *testing.T) {
	withTestDataDir(t)
	svc := &ScriptFileService{}
	os.MkdirAll(ScriptsDir(), 0755)
	os.WriteFile(filepath.Join(ScriptsDir(), "old.txt"), []byte("x"), 0644)

	err := svc.RenameFile("old.txt", "new.txt")
	if err != nil {
		t.Fatalf("RenameFile failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(ScriptsDir(), "new.txt")); os.IsNotExist(err) {
		t.Fatal("File not renamed")
	}
}

func TestScriptFileService_ReadFile_RejectsBinary(t *testing.T) {
	withTestDataDir(t)
	svc := &ScriptFileService{}
	os.MkdirAll(ScriptsDir(), 0755)
	os.WriteFile(filepath.Join(ScriptsDir(), "bin.dat"), []byte{0x00, 0xff, 0x80}, 0644)

	_, err := svc.ReadFile("bin.dat")
	if err == nil {
		t.Fatal("expected error for binary file")
	}
}
