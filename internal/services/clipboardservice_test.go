package services

import (
	"errors"
	"strings"
	"testing"
)

func TestClipboardService_Copy_Paste(t *testing.T) {
	writeErr := errors.New("write failed")
	readErr := errors.New("read failed")

	t.Run("正常读写", func(t *testing.T) {
		c := &ClipboardService{}
		clipboardWriteAll = func(s string) error { return nil }
		clipboardReadAll = func() (string, error) { return "hello", nil }
		if got := c.Copy("hello"); got != "" {
			t.Fatalf("Copy() = %q, want empty", got)
		}
		if got := c.Paste(); got != "hello" {
			t.Fatalf("Paste() = %q, want hello", got)
		}
	})

	t.Run("写入失败返回可读错误", func(t *testing.T) {
		c := &ClipboardService{}
		clipboardWriteAll = func(s string) error { return writeErr }
		got := c.Copy("x")
		if got == "" || !strings.Contains(got, "写入剪贴板失败") {
			t.Fatalf("Copy() = %q, want readable error", got)
		}
	})

	t.Run("读取失败返回空字符串", func(t *testing.T) {
		c := &ClipboardService{}
		clipboardReadAll = func() (string, error) { return "", readErr }
		if got := c.Paste(); got != "" {
			t.Fatalf("Paste() = %q, want empty", got)
		}
	})

	t.Run("空文本写入", func(t *testing.T) {
		c := &ClipboardService{}
		clipboardWriteAll = func(s string) error { return nil }
		if got := c.Copy(""); got != "" {
			t.Fatalf("Copy() = %q, want empty", got)
		}
	})
}