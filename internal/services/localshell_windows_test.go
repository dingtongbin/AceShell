//go:build windows

package services

import "testing"

// TestParseWSLDistros 验证 wsl.exe 输出解析(仅 Windows 编译;parseWSLDistros 为 Windows 专属实现)。
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
