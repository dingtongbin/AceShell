package services

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSuspendCapture_Cycle(t *testing.T) {
	s := &McpService{state: mcpStateRunning}

	// 未捕获时 drain 为空
	if got := s.DrainMcpSuspendActivity(); got != "" {
		t.Fatalf("expected empty before capture, got %q", got)
	}

	s.beginSuspendCapture()
	s.mu.Lock()
	s.appendSuspendLocked("tab-1", []byte("ls -la\r\n"))
	s.appendSuspendLocked("tab-1", []byte("total 0\r\n"))
	s.appendSuspendLocked("tab-2", []byte("\x1b[31mERROR\x1b[0m: disk full\r\n"))
	s.mu.Unlock()

	// 捕获中未结束:drain 仍为空(必须 end 后才可读)
	if got := s.DrainMcpSuspendActivity(); got != "" {
		t.Fatalf("capture in progress should not be readable, got %q", got)
	}

	s.endSuspendCapture(true)

	raw := s.DrainMcpSuspendActivity()
	if raw == "" {
		t.Fatal("expected report after end")
	}
	var report mcpSuspendReport
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		t.Fatalf("invalid report JSON: %v", err)
	}
	if len(report.Tabs) != 2 {
		t.Fatalf("expected 2 tabs, got %d", len(report.Tabs))
	}
	byID := map[string]mcpSuspendActivity{}
	for _, tab := range report.Tabs {
		byID[tab.TabID] = tab
	}
	if !strings.Contains(byID["tab-1"].Content, "ls -la") || !strings.Contains(byID["tab-1"].Content, "total 0") {
		t.Errorf("tab-1 content mismatch: %q", byID["tab-1"].Content)
	}
	if strings.Contains(byID["tab-2"].Content, "\x1b[") {
		t.Errorf("ANSI sequences should be stripped: %q", byID["tab-2"].Content)
	}
	if !strings.Contains(byID["tab-2"].Content, "disk full") {
		t.Errorf("tab-2 content mismatch: %q", byID["tab-2"].Content)
	}

	// drain 语义:读后即清
	if got := s.DrainMcpSuspendActivity(); got != "" {
		t.Fatalf("drain should clear report, got %q", got)
	}
}

func TestSuspendCapture_IdempotentBegin(t *testing.T) {
	s := &McpService{state: mcpStateRunning}
	s.beginSuspendCapture()
	s.mu.Lock()
	s.appendSuspendLocked("t", []byte("first"))
	s.mu.Unlock()

	// 重复 begin 不清空已收集内容
	s.beginSuspendCapture()
	s.mu.Lock()
	s.appendSuspendLocked("t", []byte(" second"))
	s.mu.Unlock()
	s.endSuspendCapture(true)

	raw := s.DrainMcpSuspendActivity()
	if !strings.Contains(raw, "first") || !strings.Contains(raw, "second") {
		t.Fatalf("re-begin should not reset buffer: %s", raw)
	}
}

func TestSuspendCapture_TabSlidingWindow(t *testing.T) {
	s := &McpService{state: mcpStateRunning}
	s.beginSuspendCapture()
	chunk := strings.Repeat("A", 4096)
	s.mu.Lock()
	for i := 0; i < 10; i++ { // 40KB > 单页上限 32KB → 滑动丢弃最旧
		s.appendSuspendLocked("t", []byte(chunk))
	}
	s.mu.Unlock()
	s.endSuspendCapture(true)

	raw := s.DrainMcpSuspendActivity()
	var report mcpSuspendReport
	json.Unmarshal([]byte(raw), &report)
	if len(report.Tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(report.Tabs))
	}
	if !report.Tabs[0].Truncated {
		t.Error("expected truncated flag")
	}
	if len(report.Tabs[0].Content) > suspendTabMax {
		t.Errorf("content exceeds tab cap: %d", len(report.Tabs[0].Content))
	}
}

func TestSuspendCapture_GlobalCap(t *testing.T) {
	s := &McpService{state: mcpStateRunning}
	s.beginSuspendCapture()
	big := strings.Repeat("B", 32 * 1024)
	s.mu.Lock()
	// 单页被滑动窗口钳制在 32KB;全局上限防护的是多页累计:
	// 4 页 × 32KB = 128KB 恰好不超;第 5 页触发全局截断
	for _, id := range []string{"a", "b", "c", "d"} {
		s.appendSuspendLocked(id, []byte(big))
	}
	s.mu.Unlock()
	if s.suspendGlobalCut {
		t.Fatal("4 tabs should not exceed global cap")
	}
	s.mu.Lock()
	s.appendSuspendLocked("e", []byte(big))
	s.mu.Unlock()
	s.endSuspendCapture(true)

	raw := s.DrainMcpSuspendActivity()
	var report mcpSuspendReport
	json.Unmarshal([]byte(raw), &report)
	if !report.GlobalTruncated {
		t.Error("expected globalTruncated flag after 5th tab")
	}
	for _, tab := range report.Tabs {
		if len(tab.Content) > suspendTabMax+1024 {
			t.Errorf("tab content unexpectedly large: %d", len(tab.Content))
		}
	}
}

func TestFormatSuspendPrompt(t *testing.T) {
	// 正常报告
	report := mcpSuspendReport{
		Since: time.Now(),
		Tabs: []mcpSuspendActivity{
			{TabID: "tab-a", Content: "systemctl restart nginx", Truncated: false},
		},
	}
	raw, _ := json.Marshal(report)
	prompt := formatSuspendPrompt(string(raw))
	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}
	for _, want := range []string{"用户中断期间的操作", "tab-a", "systemctl restart nginx"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}

	// 空 tabs → 空串
	raw, _ = json.Marshal(mcpSuspendReport{})
	if got := formatSuspendPrompt(string(raw)); got != "" {
		t.Fatalf("empty report should yield empty prompt, got %q", got)
	}

	// 非法 JSON → 空串
	if got := formatSuspendPrompt("not-json"); got != "" {
		t.Fatalf("invalid json should yield empty prompt, got %q", got)
	}
}

func TestActivityTracking_BusyMerge(t *testing.T) {
	withTestDataDir(t)
	cfg := &ConfigService{}
	cfg.Init()
	s := &McpService{state: mcpStateRunning, cfg: cfg, outBuf: map[string][]byte{}, outCursor: map[string]int{}}

	busyAt := func() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.busy }

	// 端到端:GetMcpStatus 输出的 busy 字段随活动翻转(前端消费的就是这个 JSON)
	statusBusy := func() bool {
		var m map[string]any
		if err := json.Unmarshal([]byte(s.GetMcpStatus()), &m); err != nil {
			t.Fatalf("invalid status json: %v", err)
		}
		v, _ := m["busy"].(bool)
		return v
	}

	s.beginActivity()
	if !busyAt() || !statusBusy() {
		t.Fatalf("activity should be busy: inMem=%v inStatus=%v", busyAt(), statusBusy())
	}
	// 嵌套活动:一个结束另一个仍在,保持忙碌
	s.beginActivity()
	s.endActivity()
	if !busyAt() || !statusBusy() {
		t.Fatal("nested activity should keep busy")
	}
	s.endActivity()
	if busyAt() || statusBusy() {
		t.Fatal("all activities ended should clear busy")
	}

	// 槽占用与活动计数合并
	s.mu.Lock()
	s.slotBusy = true
	ch := s.recomputeBusyLocked()
	s.mu.Unlock()
	if !ch || !busyAt() {
		t.Fatal("slot busy should drive busy")
	}
	s.mu.Lock()
	s.slotBusy = false
	s.recomputeBusyLocked()
	s.mu.Unlock()

	// endActivity 防御:多减不致负数
	s.beginActivity()
	s.endActivity()
	s.endActivity()
	s.mu.Lock()
	cnt := s.activeCnt
	s.recomputeBusyLocked()
	s.mu.Unlock()
	if cnt != 0 {
		t.Fatalf("activeCnt should never go negative, got %d", cnt)
	}
}

func TestTapOutput_CapturesOnlyWhileSuspending(t *testing.T) {
	s := &McpService{state: mcpStateRunning, outBuf: map[string][]byte{}, outCursor: map[string]int{}}
	s.TapOutput("t", []byte("before")) // 未在捕获期:不进捕获缓冲

	s.beginSuspendCapture()
	s.TapOutput("t", []byte("during-1"))
	s.TapOutput("t", []byte("during-2"))
	s.endSuspendCapture(true)

	raw := s.DrainMcpSuspendActivity()
	if !strings.Contains(raw, "during-1") {
		t.Fatalf("capture missing during output: %s", raw)
	}
	if strings.Contains(raw, "before") {
		t.Error("pre-suspend output leaked into capture")
	}
}
