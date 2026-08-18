package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLogService_StartEndSession(t *testing.T) {
	withTestDataDir(t)
	svc := &LogService{}
	svc.Init()
	defer os.RemoveAll(filepath.Join(sessionsDir, "..", "autolog"))

	id := "test-log-" + time.Now().Format("150405")
	svc.StartSession(id, "ssh", "192.168.1.1", 22, "root", "Test Session")

	svc.LogOutput(id, "ls\r\n")
	svc.LogOutput(id, "whoami\r\n")
	time.Sleep(200 * time.Millisecond) // 等 flush

	svc.EndSession(id)
	time.Sleep(200 * time.Millisecond)

	// 检查元数据文件
	metaPath := filepath.Join(sessionsDir, "..", "autolog", "meta", id+".toml")
	if _, err := os.Stat(metaPath); os.IsNotExist(err) {
		t.Fatal("metadata file not created")
	}

	// 检查日志文件
	logPath := filepath.Join(sessionsDir, "..", "autolog", "logs", id+".log")
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Fatal("log file not created")
	}
}

func TestLogService_GetLogTree(t *testing.T) {
	withTestDataDir(t)
	svc := &LogService{}
	svc.Init()
	defer os.RemoveAll(filepath.Join(sessionsDir, "..", "autolog"))

	id := "tree-" + time.Now().Format("150405")
	svc.StartSession(id, "ssh", "10.0.0.1", 22, "admin", "SSH Test")
	svc.LogOutput(id, "test data")
	svc.EndSession(id)
	time.Sleep(200 * time.Millisecond)

	tree := svc.GetLogTree()
	if tree == "" || tree == "[]" {
		t.Fatal("GetLogTree returned empty")
	}
	if !strings.Contains(tree, "SSH Test") {
		t.Fatal("log tree missing session name")
	}
}

func TestLogService_GetLogContent(t *testing.T) {
	withTestDataDir(t)
	svc := &LogService{}
	svc.Init()
	defer os.RemoveAll(filepath.Join(sessionsDir, "..", "autolog"))

	id := "content-" + time.Now().Format("150405")
	svc.StartSession(id, "telnet", "10.0.0.1", 23, "user", "Telnet Test")
	svc.LogOutput(id, "hello world")
	svc.EndSession(id)
	time.Sleep(200 * time.Millisecond)

	content := svc.GetLogContent(id)
	if content == "" {
		t.Fatal("GetLogContent returned empty")
	}
	if content != "hello world" {
		t.Fatalf("content mismatch: got %q", content)
	}
}

func TestLogService_GetLogMeta(t *testing.T) {
	withTestDataDir(t)
	svc := &LogService{}
	svc.Init()
	defer os.RemoveAll(filepath.Join(sessionsDir, "..", "autolog"))

	id := "meta-" + time.Now().Format("150405")
	svc.StartSession(id, "shell", "", 0, "", "Local Shell")
	svc.EndSession(id)
	time.Sleep(200 * time.Millisecond)

	meta := svc.GetLogMeta(id)
	if meta == "" || meta == "{}" {
		t.Fatal("GetLogMeta returned empty")
	}
	if !strings.Contains(meta, "Local Shell") {
		t.Fatal("meta missing session title")
	}
}

func TestLogService_Reconnect(t *testing.T) {
	withTestDataDir(t)
	svc := &LogService{}
	svc.Init()
	defer os.RemoveAll(filepath.Join(sessionsDir, "..", "autolog"))

	id := "recon-" + time.Now().Format("150405")
	svc.StartSession(id, "ssh", "10.0.0.1", 22, "root", "Reconnect Test")
	svc.LogOutput(id, "session 1\r\n")
	time.Sleep(200 * time.Millisecond)
	svc.EndSession(id)

	// 重连后再次记录
	svc.StartSession(id, "ssh", "10.0.0.1", 22, "root", "Reconnect Test")
	svc.LogOutput(id, "session 2\r\n")
	time.Sleep(200 * time.Millisecond)
	svc.EndSession(id)
	time.Sleep(200 * time.Millisecond)

	content := svc.GetLogContent(id)
	if !strings.Contains(content, "session 2") {
		t.Fatal("reconnect data not appended")
	}
}

func TestLogService_EmptyLog(t *testing.T) {
	withTestDataDir(t)
	svc := &LogService{}
	svc.Init()

	content := svc.GetLogContent("nonexistent")
	if content != "" {
		t.Fatal("expected empty for nonexistent log")
	}

	meta := svc.GetLogMeta("nonexistent")
	if meta != "{}" {
		t.Fatal("expected {} for nonexistent meta")
	}
}

func TestLogSessionMeta_Structure(t *testing.T) {
	meta := LogSessionMeta{
		SessionID:  "test-id",
		Protocol:   "ssh",
		Host:       "192.168.1.1",
		Port:       22,
		Username:   "root",
		Title:      "Test",
		StartTime:  "2025-01-01T00:00:00Z",
		TotalLines: 100,
		TotalBytes: 1024,
	}
	if meta.SessionID != "test-id" {
		t.Fatal("SessionID mismatch")
	}
	if meta.Protocol != "ssh" {
		t.Fatal("Protocol mismatch")
	}
}

func TestProtoLabel(t *testing.T) {
	tests := []struct{ proto, want string }{
		{"ssh", "SSH 连接"},
		{"telnet", "Telnet 连接"},
		{"serial", "串口 连接"},
		{"shell", "本地 Shell"},
		{"unknown", "unknown"},
	}
	for _, tt := range tests {
		if got := protoLabel(tt.proto); got != tt.want {
			t.Errorf("protoLabel(%q) = %q, want %q", tt.proto, got, tt.want)
		}
	}
}
