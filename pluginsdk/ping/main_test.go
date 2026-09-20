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
