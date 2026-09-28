package services

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "changeme/pluginsdk/proto"
)

// TestHostServiceOpenTab_PropsTooLarge OpenTab 超大 propsJson 应拒绝(InvalidArgument)。
func TestHostServiceOpenTab_PropsTooLarge(t *testing.T) {
	h := &hostServiceServer{svc: &PluginService{}}
	req := &pb.OpenTabRequest{Spec: &pb.TabSpec{
		TabKey:      "tool",
		ComponentId: "panel",
		PropsJson:   strings.Repeat("a", pluginEventMaxBytes+1),
	}}
	if _, err := h.OpenTab(context.Background(), req); err == nil {
		t.Fatal("超大 propsJson 应被拒绝")
	} else if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("应返回 InvalidArgument, 实得 %v", status.Code(err))
	}
}

// TestPluginLogRotate 写满上限后自动轮转: 旧内容落 plugin-runs.old.log, 新文件从头计数。
func TestPluginLogRotate(t *testing.T) {
	restore := SetDataDir(t.TempDir())
	defer restore()

	s := &PluginService{}
	f, err := os.OpenFile(filepath.Join(DataDir(), pluginRunsLogName), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("打开日志失败: %v", err)
	}
	// 测试结束必须关句柄: 否则 TempDir cleanup 在 Windows 上因文件占用而失败
	defer func() {
		s.mu.Lock()
		if s.logFile != nil {
			_ = s.logFile.Close()
			s.logFile = nil
		}
		s.mu.Unlock()
	}()
	s.logFile = f
	s.logWritten = pluginLogMaxBytes - 5

	w := &pluginLogWriter{svc: s, prefix: "t "}
	if _, err := w.Write([]byte("0123456789\n")); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	// 轮转后: 旧文件应留档且含该行, 新文件存在但不含该行
	oldData, err := os.ReadFile(filepath.Join(DataDir(), "plugin-runs.old.log"))
	if err != nil {
		t.Fatalf("轮转留档文件缺失: %v", err)
	}
	if !strings.Contains(string(oldData), "0123456789") {
		t.Fatalf("留档内容缺失: %q", oldData)
	}
	if s.logFile == nil {
		t.Fatal("轮转后应重新打开日志文件")
	}
	if s.logWritten >= pluginLogMaxBytes {
		t.Fatalf("轮转后计数应重新开始, 实得 %d", s.logWritten)
	}
}

// TestOpenPluginsDir_PlatformCommand openPluginsDir 在命令缺失时不 panic 且返回错误。
// (仅覆盖构造路径; CI 无桌面环境时启动失败属预期, 不作为失败条件。)
func TestOpenPluginsDir_PlatformCommand(t *testing.T) {
	restore := SetDataDir(t.TempDir())
	defer restore()
	_ = os.MkdirAll(PluginsDir(), 0700)
	if err := openPluginsDir(); err != nil {
		t.Logf("openPluginsDir 返回错误(无桌面环境属预期): %v", err)
	}
}

// TestExeSuffix 平台可执行文件后缀与 runtime 一致。
func TestExeSuffix(t *testing.T) {
	want := ""
	if runtime.GOOS == "windows" {
		want = ".exe"
	}
	if got := exeSuffix(); got != want {
		t.Fatalf("exeSuffix() = %q, 期望 %q", got, want)
	}
}
