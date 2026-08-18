package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowService_GetUserHomeDir(t *testing.T) {
	w := &WindowService{}
	home := w.GetUserHomeDir()
	if home == "" {
		t.Fatal("GetUserHomeDir returned empty string")
	}
	info, err := os.Stat(home)
	if err != nil || !info.IsDir() {
		t.Fatalf("GetUserHomeDir(%q) is not a valid directory: %v", home, err)
	}
}

func TestWindowService_LocalFileOps(t *testing.T) {
	w := &WindowService{}
	base := t.TempDir()
	file := filepath.Join(base, "sub", "a.txt")

	if err := w.LocalCreateFile(file); err != nil {
		t.Fatalf("LocalCreateFile failed: %v", err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("created file missing: %v", err)
	}

	if err := w.LocalWriteText(file, "hello"); err != nil {
		t.Fatalf("LocalWriteText failed: %v", err)
	}
	got, err := w.LocalReadText(file)
	if err != nil {
		t.Fatalf("LocalReadText failed: %v", err)
	}
	if got != "hello" {
		t.Fatalf("LocalReadText = %q, want %q", got, "hello")
	}

	dir := filepath.Join(base, "sub", "nested")
	if err := w.LocalCreateDir(dir); err != nil {
		t.Fatalf("LocalCreateDir failed: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("created dir missing: %v", err)
	}

	if err := w.LocalRename(file, "b.txt"); err != nil {
		t.Fatalf("LocalRename failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "sub", "b.txt")); err != nil {
		t.Fatalf("renamed file missing: %v", err)
	}
	if _, err := os.Stat(file); err == nil {
		t.Fatal("old path still exists after rename")
	}
}

func TestWindowService_LocalReadText_Missing(t *testing.T) {
	w := &WindowService{}
	_, err := w.LocalReadText(filepath.Join(t.TempDir(), "nope.txt"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestSFTPService_RemoveAll_NoSession(t *testing.T) {
	svc := &SFTPService{}
	svc.SetApp(nil)
	svc.SSHSvc = &SSHService{}

	err := svc.RemoveAll("nonexistent", "/tmp/dir")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}
