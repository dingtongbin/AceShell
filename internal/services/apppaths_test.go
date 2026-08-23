package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDataDir_Default(t *testing.T) {
	// TestMain 已全局兜底, DataDir() 不再返回平台默认值;
	// 改为验证纯函数 defaultDataDir(): 只计算路径, 不做任何 I/O。
	dir := defaultDataDir()
	if dir == "" {
		t.Fatal("defaultDataDir returned empty")
	}
	// 默认目录必须落在平台应用数据目录下,而非当前工作目录
	cwd, _ := os.Getwd()
	if strings.HasPrefix(dir, cwd) {
		t.Fatalf("defaultDataDir %q must not be under cwd %q", dir, cwd)
	}
	if !filepath.IsAbs(dir) {
		t.Fatalf("defaultDataDir %q must be absolute", dir)
	}
}

func TestDataDir_PathFunctions(t *testing.T) {
	withTestDataDir(t)
	want := filepath.Join(DataDir(), "config.toml")
	if ConfigFilePath() != want {
		t.Fatalf("ConfigFilePath() = %q, want %q", ConfigFilePath(), want)
	}
	if DBFilePath() != filepath.Join(DataDir(), "aceshell.db") {
		t.Fatalf("DBFilePath() = %q", DBFilePath())
	}
	if SessionsDir() != filepath.Join(DataDir(), "sessions") {
		t.Fatalf("SessionsDir() = %q", SessionsDir())
	}
	if AutoLogDir() != filepath.Join(DataDir(), "autolog") {
		t.Fatalf("AutoLogDir() = %q", AutoLogDir())
	}
	if ScriptsDir() != filepath.Join(DataDir(), "script") {
		t.Fatalf("ScriptsDir() = %q", ScriptsDir())
	}
}

func TestDataDir_SetDataDir_Restore(t *testing.T) {
	before := DataDir()
	cleanup := SetDataDir(t.TempDir())
	if DataDir() == before {
		t.Fatal("SetDataDir did not change data dir")
	}
	cleanup()
	if DataDir() != before {
		t.Fatalf("restore failed: got %q, want %q", DataDir(), before)
	}
}
