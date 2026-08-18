package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSFTPService_CheckSSH_NoSession(t *testing.T) {
	svc := &SFTPService{}
	svc.SetApp(nil)
	svc.SSHSvc = &SSHService{}

	result := svc.CheckSSH("nonexistent")
	if result {
		t.Fatal("Expected false for nonexistent session")
	}
}

func TestSFTPService_Disconnect_NoSession(t *testing.T) {
	svc := &SFTPService{}
	svc.SetApp(nil)

	err := svc.Disconnect("nonexistent")
	if err != nil {
		t.Fatalf("Disconnect on nonexistent session should not error: %v", err)
	}
}

func TestSFTPService_List_NoSSH(t *testing.T) {
	svc := &SFTPService{}
	svc.SetApp(nil)
	svc.SSHSvc = &SSHService{}

	result := svc.List("nonexistent", "/")
	if result == "" {
		t.Fatal("Expected error JSON, got empty")
	}
	t.Logf("List result: %s", result)
}

func TestSFTPService_Stat_NoSSH(t *testing.T) {
	svc := &SFTPService{}
	svc.SetApp(nil)
	svc.SSHSvc = &SSHService{}

	result := svc.Stat("nonexistent", "/tmp")
	if result == "" {
		t.Fatal("Expected error JSON, got empty")
	}
	t.Logf("Stat result: %s", result)
}

func TestSFTPService_Mkdir_NoSSH(t *testing.T) {
	svc := &SFTPService{}
	svc.SetApp(nil)
	svc.SSHSvc = &SSHService{}

	err := svc.Mkdir("nonexistent", "/tmp/testdir")
	if err == nil {
		t.Fatal("Expected error for nonexistent session")
	}
}

func TestSFTPService_Remove_NoSSH(t *testing.T) {
	svc := &SFTPService{}
	svc.SetApp(nil)
	svc.SSHSvc = &SSHService{}

	err := svc.Remove("nonexistent", "/tmp/testfile")
	if err == nil {
		t.Fatal("Expected error for nonexistent session")
	}
}

func TestSFTPService_Rename_NoSSH(t *testing.T) {
	svc := &SFTPService{}
	svc.SetApp(nil)
	svc.SSHSvc = &SSHService{}

	err := svc.Rename("nonexistent", "/tmp/old", "/tmp/new")
	if err == nil {
		t.Fatal("Expected error for nonexistent session")
	}
}

func TestSFTPService_Upload_NoSSH(t *testing.T) {
	svc := &SFTPService{}
	svc.SetApp(nil)
	svc.SSHSvc = &SSHService{}

	result := svc.Upload("nonexistent", "/tmp/local.txt", "/tmp/remote.txt")
	if result == "" {
		t.Fatal("Expected error JSON, got empty")
	}
	t.Logf("Upload result: %s", result)
}

func TestSFTPService_Download_NoSSH(t *testing.T) {
	svc := &SFTPService{}
	svc.SetApp(nil)
	svc.SSHSvc = &SSHService{}

	result := svc.Download("nonexistent", "/tmp/remote.txt", "/tmp/local.txt")
	if result == "" {
		t.Fatal("Expected error JSON, got empty")
	}
	t.Logf("Download result: %s", result)
}

func TestSFTPService_Getwd_NoSSH(t *testing.T) {
	svc := &SFTPService{}
	svc.SetApp(nil)
	svc.SSHSvc = &SSHService{}

	result := svc.Getwd("nonexistent")
	if result == "" {
		t.Fatal("Expected error JSON, got empty")
	}
	t.Logf("Getwd result: %s", result)
}

func TestSFTPService_ReadFile_NoSSH(t *testing.T) {
	svc := &SFTPService{}
	svc.SetApp(nil)
	svc.SSHSvc = &SSHService{}

	result := svc.ReadFile("nonexistent", "/tmp/test.txt")
	if result == "" {
		t.Fatal("Expected error JSON, got empty")
	}
	t.Logf("ReadFile result: %s", result)
}

func TestSFTPService_WriteFile_NoSSH(t *testing.T) {
	svc := &SFTPService{}
	svc.SetApp(nil)
	svc.SSHSvc = &SSHService{}

	err := svc.WriteFile("nonexistent", "/tmp/test.txt", "hello")
	if err == nil {
		t.Fatal("Expected error for nonexistent session")
	}
}

func TestSFTPService_Chmod_NoSSH(t *testing.T) {
	svc := &SFTPService{}
	svc.SetApp(nil)
	svc.SSHSvc = &SSHService{}

	err := svc.Chmod("nonexistent", "/tmp/test.txt", 0644)
	if err == nil {
		t.Fatal("Expected error for nonexistent session")
	}
}

func TestSFTPService_Connect_NilSSH(t *testing.T) {
	svc := &SFTPService{}
	svc.SetApp(nil)
	svc.SSHSvc = nil

	_, err := svc.connect("test")
	if err == nil {
		t.Fatal("Expected error when SSHSvc is nil")
	}
}

func TestSFTPService_ListFiles_Format(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("hello"), 0644)
	os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0755)

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatalf("Info failed: %v", err)
		}
		file := SFTPFileInfo{
			Name:    info.Name(),
			Size:    info.Size(),
			Mode:    info.Mode().String(),
			ModTime: info.ModTime().Format("2006-01-02T15:04:05Z"),
			IsDir:   info.IsDir(),
		}
		t.Logf("File: %+v", file)
	}
}
