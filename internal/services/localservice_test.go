package services

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestResolveShellCommand(t *testing.T) {
	tests := []struct {
		name     string
		ref      string
		wantName string
		wantArgs []string
	}{
		{"empty uses ref as-is", "", "", nil},
		{"plain path", `C:\Windows\System32\cmd.exe`, `C:\Windows\System32\cmd.exe`, nil},
		{"wsl distro", "wsl://Ubuntu-22.04", "wsl.exe", []string{"-d", "Ubuntu-22.04"}},
		{"wsl prefix only falls back", "wsl://", "wsl://", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, args := resolveShellCommand(tt.ref)
			if name != tt.wantName {
				t.Errorf("resolveShellCommand(%q) name = %q, want %q", tt.ref, name, tt.wantName)
			}
			if len(args) != len(tt.wantArgs) {
				t.Fatalf("args = %v, want %v", args, tt.wantArgs)
			}
			for i := range args {
				if args[i] != tt.wantArgs[i] {
					t.Errorf("args[%d] = %q, want %q", i, args[i], tt.wantArgs[i])
				}
			}
		})
	}
}

func TestShellRefDisplayName(t *testing.T) {
	tests := []struct {
		ref  string
		want string
	}{
		{"wsl://Ubuntu-22.04", "WSL: Ubuntu-22.04"},
		{`C:\Windows\System32\cmd.exe`, "cmd"},
		{`C:\Program Files\Git\bin\bash.exe`, "bash"},
		{"/bin/bash", "bash"},
		{"pwsh.exe", "pwsh"},
		{"PowerShell", "PowerShell"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := shellRefDisplayName(tt.ref); got != tt.want {
			t.Errorf("shellRefDisplayName(%q) = %q, want %q", tt.ref, got, tt.want)
		}
	}
}

func TestParseWSLDistros(t *testing.T) {
	// UTF-16LE with BOM(wsl.exe 实际输出格式)
	utf16Out := []byte{0xFF, 0xFE}
	for _, r := range "Ubuntu-22.04\r\ndebian\r\n\r\ndocker-desktop\r\n" {
		utf16Out = append(utf16Out, byte(r), byte(r>>8))
	}
	got := parseWSLDistros(utf16Out)
	if len(got) != 2 {
		t.Fatalf("expected 2 distros, got %v", got)
	}
	if got[0] != "Ubuntu-22.04" || got[1] != "debian" {
		t.Errorf("distros mismatch: %v", got)
	}

	// UTF-8 回退
	plain := parseWSLDistros([]byte("*Ubuntu\ndocker-desktop\n"))
	if len(plain) != 1 || plain[0] != "Ubuntu" {
		t.Errorf("utf8 fallback mismatch: %v", plain)
	}

	// 空输入
	if got := parseWSLDistros(nil); got != nil {
		t.Errorf("expected nil for empty input, got %v", got)
	}
}

func TestLocalService_NilCheck(t *testing.T) {
	svc := &LocalService{}

	if err := svc.Connect("id1", ""); err == nil || !strings.Contains(err.Error(), "未初始化") {
		t.Errorf("Connect on uninitialized service should fail, got %v", err)
	}
	if err := svc.Send("id1", "x"); err == nil {
		t.Error("Send on uninitialized service should fail")
	}
	if err := svc.Disconnect("id1"); err == nil {
		t.Error("Disconnect on uninitialized service should fail")
	}
	if err := svc.Resize("id1", 80, 24); err == nil {
		t.Error("Resize on uninitialized service should fail")
	}
}

func TestLocalService_ListShells(t *testing.T) {
	withTestDataDir(t)
	svc := &LocalService{}
	raw := svc.ListShells()
	if raw == "" || raw == "null" {
		t.Fatal("ListShells returned empty/null")
	}
	if !strings.Contains(raw, "[") {
		t.Fatalf("ListShells not a JSON array: %q", raw)
	}
}

func TestCurrentUserName(t *testing.T) {
	// 不校验具体值(跨平台),只保证可调用且不含路径分隔符
	name := currentUserName()
	if strings.ContainsAny(name, "/\\") {
		t.Errorf("currentUserName contains path separator: %q", name)
	}
}

func TestLocalService_ConnectDuplicate(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode: skip PTY spawn test")
	}
	withTestDataDir(t)
	MainLogService = nil
	svc := &LocalService{}
	svc.SetApp(nil)

	id := "local-test-" + time.Now().Format("150405.000")
	if err := svc.Connect(id, ""); err != nil {
		t.Skipf("PTY/shell unavailable in this environment: %v", err)
	}
	defer os.RemoveAll("./sessions")

	// 重复连接同 ID 应报错
	if err := svc.Connect(id, ""); err == nil {
		t.Fatal("duplicate Connect should fail")
	}

	// 发送一条命令验证写入不报错
	if err := svc.Send(id, "echo ok\r\n"); err != nil {
		t.Errorf("Send failed: %v", err)
	}

	time.Sleep(300 * time.Millisecond)
	if err := svc.Disconnect(id); err != nil {
		t.Errorf("Disconnect failed: %v", err)
	}
	// 幂等断开
	if err := svc.Disconnect(id); err == nil {
		t.Log("second Disconnect returned nil (session already removed)")
	}
}
