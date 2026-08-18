package services

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateEditableText(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr bool
	}{
		{"valid utf8", []byte("hello\nworld"), false},
		{"empty", []byte{}, false},
		{"utf8 with chinese", []byte("中文内容"), false},
		{"binary bytes", []byte{0xff, 0xfe, 0x00, 0x01, 0x80}, true},
		{"nul bytes", []byte{'a', 0x00, 'b'}, true},
		{"exact size ok", bytes.Repeat([]byte{'a'}, maxEditableSize), false},
		{"over size", bytes.Repeat([]byte{'a'}, maxEditableSize+1), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEditableText(tt.data)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateEditableText() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestWindowService_LocalReadText_RejectsBinary(t *testing.T) {
	w := &WindowService{}
	path := filepath.Join(t.TempDir(), "data.bin")
	if err := os.WriteFile(path, []byte{0x00, 0xff, 0x80, 0x01}, 0644); err != nil {
		t.Fatal(err)
	}
	_, err := w.LocalReadText(path)
	if err == nil || !strings.Contains(err.Error(), "二进制") {
		t.Fatalf("expected binary error for binary file, got %v", err)
	}
}

func TestWindowService_LocalReadText_RejectsLarge(t *testing.T) {
	w := &WindowService{}
	path := filepath.Join(t.TempDir(), "big.log")
	if err := os.WriteFile(path, make([]byte, maxEditableSize+1), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := w.LocalReadText(path)
	if err == nil || !strings.Contains(err.Error(), "过大") {
		t.Fatalf("expected size error, got %v", err)
	}
}