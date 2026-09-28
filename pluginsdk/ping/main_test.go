package main

import (
	"context"
	"testing"
)

// TestPingOnce 真实探测回环地址(Windows ping 输出含中文/GBK, 解析只依赖 ASCII 数字)。
func TestPingOnce(t *testing.T) {
	ms, ok, line := pingOnce(context.Background(), "127.0.0.1", 1000)
	if !ok {
		t.Fatalf("ping 127.0.0.1 应成功: ms=%v line=%q", ms, line)
	}
	if ms <= 0 || ms > 100 {
		t.Fatalf("回环延迟异常: %v", ms)
	}
	if line == "" {
		t.Fatal("回复行不应为空")
	}
}

// TestParsePingOutput pingOnce 输出解析: 成功/空输出/失败行/GBK 中文输出(延迟段仍是 ASCII)。
func TestParsePingOutput(t *testing.T) {
	tests := []struct {
		name   string
		out    string
		wantOK bool
		wantMS float64
	}{
		{"win 英文回复", "Reply from 8.8.8.8: bytes=32 time=12ms TTL=115\r\n\r\nPing statistics...", true, 12},
		{"win 中文回复(GBK 畸形解码, 延迟段 ASCII)", "\xd5\xfd\xd4\xda Ping 127.0.0.1...\r\n\xc0\xb4\xd7\xd4 127.0.0.1 \xbb\xd8\xb8\xb4: \xd7\xd6\xbd\xda=32 \xca\xb1\xbc\xe4<1ms TTL=128", true, 1},
		{"win 中文统计行", "    \xd7\xee\xb6\xcc = 0ms\xa3\xac\xd7\xee\xb3\xa4 = 0ms\xa3\xac\xc6\xbd\xbe\xf9 = 0ms", true, 0},
		{"超时无输出", "", false, 0},
		{"请求超时行", "Request timed out.", false, 0},
		{"中文一般故障", "一般故障。", false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms, ok, line := parsePingOutput(tt.out)
			if ok != tt.wantOK {
				t.Fatalf("parsePingOutput(%q) ok = %v, 期望 %v (line=%q ms=%v)", tt.out, ok, tt.wantOK, line, ms)
			}
			if ok && ms != tt.wantMS {
				t.Errorf("parsePingOutput(%q) ms = %v, 期望 %v", tt.out, ms, tt.wantMS)
			}
		})
	}
}

// TestPingOnceUnreachable 不可达地址(保留地址段, 不发往网络)。
func TestPingOnceUnreachable(t *testing.T) {
	_, ok, _ := pingOnce(context.Background(), "240.0.0.1", 500)
	if ok {
		t.Fatal("240.0.0.1 不应可达")
	}
}

// TestLatencyRegex 中英文输出的延迟解析。
func TestLatencyRegex(t *testing.T) {
	cases := map[string]bool{
		"来自 223.5.5.5 的回复: 字节=32 时间=3ms TTL=118":      true,
		"Reply from 8.8.8.8: bytes=32 time=12ms TTL=115": true,
		"time<1ms TTL=64":                                true,
		"一般故障。":                                          false,
		"Request timed out.":                             false,
	}
	for in, want := range cases {
		if got := latencyRe.MatchString(in); got != want {
			t.Errorf("latencyRe(%q) = %v, 期望 %v", in, got, want)
		}
	}
}
