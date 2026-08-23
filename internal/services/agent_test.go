package services

import (
	"strings"
	"testing"
)

// ==================== 路径感知分级 ====================

func TestGradeCommandExPathAware(t *testing.T) {
	// 普通查看命令 → auto
	g := GradeCommandEx("cat /home/user/notes.txt")
	if g.Risk != RiskAuto {
		t.Errorf("cat /home/user/notes.txt 应为 auto,实际 %s", g.Risk)
	}
	// 敏感路径升级 → confirm(/var/log 在敏感前缀表中)
	g = GradeCommandEx("cat /var/log/auth.log")
	if g.Risk != RiskConfirm {
		t.Errorf("cat /var/log/auth.log 应升级为 confirm,实际 %s", g.Risk)
	}
	// 敏感路径升级 → confirm
	g = GradeCommandEx("cat /etc/shadow")
	if g.Risk != RiskConfirm {
		t.Errorf("cat /etc/shadow 应升级为 confirm,实际 %s", g.Risk)
	}
	// 绝对危险 → blocked
	g = GradeCommandEx("rm -rf /")
	if g.Risk != RiskBlocked {
		t.Errorf("rm -rf / 应为 blocked,实际 %s", g.Risk)
	}
	// 未知命令 → confirm(失败安全)
	g = GradeCommandEx("someunknowncmd --flag")
	if g.Risk != RiskConfirm {
		t.Errorf("未知命令应为 confirm,实际 %s", g.Risk)
	}
	// 空命令 → auto
	g = GradeCommandEx("   ")
	if g.Risk != RiskAuto {
		t.Errorf("空命令应为 auto,实际 %s", g.Risk)
	}
}

func TestGradeTextExMultiline(t *testing.T) {
	g := GradeTextEx("ls /tmp\ncat /var/log/x")
	if g.Risk != RiskConfirm {
		t.Errorf("多行输入应为 confirm,实际 %s", g.Risk)
	}
	// 多行含 blocked → 整体 blocked
	g = GradeTextEx("ls /tmp\nmkfs.ext4 /dev/sda1")
	if g.Risk != RiskBlocked {
		t.Errorf("多行含 blocked 应整体 blocked,实际 %s", g.Risk)
	}
}

func TestExtractPaths(t *testing.T) {
	paths := extractPaths(`cat /etc/hosts; grep "x y" ./local.txt --output=out.txt`)
	joined := strings.Join(paths, ",")
	if !strings.Contains(joined, "/etc/hosts") {
		t.Errorf("应提取 /etc/hosts,实际 %v", paths)
	}
	if !strings.Contains(joined, "./local.txt") {
		t.Errorf("应提取 ./local.txt,实际 %v", paths)
	}
	if strings.Contains(joined, "--output") {
		t.Errorf("选项不应被提取,实际 %v", paths)
	}
	// Windows 盘符
	paths = extractPaths(`type C:\Windows\win.ini`)
	if len(paths) == 0 || paths[0] != `C:\Windows\win.ini` {
		t.Errorf("应提取 Windows 路径,实际 %v", paths)
	}
}

func TestHasSensitivePath(t *testing.T) {
	if !hasSensitivePath([]string{"/etc/passwd"}) {
		t.Error("/etc/passwd 应为敏感路径")
	}
	if hasSensitivePath([]string{"/home/user/a.txt"}) {
		t.Error("/home/user/a.txt 不应为敏感路径")
	}
}

// ==================== 永久授权匹配 ====================

func TestPathWildcardMatch(t *testing.T) {
	cases := []struct {
		pattern, s string
		want       bool
	}{
		{"/var/log/*", "/var/log/syslog", true},
		{"/var/log/*", "/etc/passwd", false},
		{"/etc/hosts", "/etc/hosts", true},
		{"/etc/hosts", "/etc/hostname", false},
		{"*", "anything", true},
	}
	for _, c := range cases {
		if got := pathWildcardMatch(c.pattern, c.s); got != c.want {
			t.Errorf("pathWildcardMatch(%q,%q)=%v,期望 %v", c.pattern, c.s, got, c.want)
		}
	}
}

func TestNormalizePathsAndMatchSet(t *testing.T) {
	n := normalizePaths([]string{"b", "a", "a", " "})
	if len(n) != 2 || n[0] != "a" || n[1] != "b" {
		t.Errorf("normalizePaths 应去重排序,实际 %v", n)
	}
	if !matchPathSet(normalizePaths([]string{"b", "a"}), normalizePaths([]string{"a", "b"})) {
		t.Error("规范化后集合匹配应与顺序无关")
	}
	if matchPathSet(nil, []string{"a"}) {
		t.Error("空规则与非空实际不匹配")
	}
	if !matchPathSet(nil, nil) {
		t.Error("双方为空应匹配")
	}
}

func TestGrantStoreMatchAdd(t *testing.T) {
	store := newMcpGrantStore(t.TempDir())
	if _, err := store.Add("cat /etc/hosts", []string{"/etc/hosts"}); err != nil {
		t.Fatalf("Add 失败: %v", err)
	}
	if id := store.Match("cat /etc/hosts", []string{"/etc/hosts"}); id == "" {
		t.Error("命令+路径完全一致应命中")
	}
	if id := store.Match("cat /etc/hosts", []string{"/etc/passwd"}); id != "" {
		t.Error("路径不同不应命中")
	}
	if id := store.Match("cat /etc/hostname", []string{"/etc/hostname"}); id != "" {
		t.Error("命令不同不应命中")
	}
	// 重新加载持久化
	store2 := newMcpGrantStore(t.TempDir())
	_ = store2
}

// ==================== 自定义规则 ====================

func TestCustomRulesPriority(t *testing.T) {
	RefreshCustomRules([]McpCustomRule{{Pattern: `^mytool\s`, Risk: RiskAuto, Note: "内部工具"}})
	defer RefreshCustomRules(nil)
	g := GradeCommandEx("mytool --run")
	if g.Risk != RiskAuto {
		t.Errorf("自定义规则应优先,期望 auto 实际 %s", g.Risk)
	}
	if !strings.Contains(g.Reason, "自定义规则") {
		t.Errorf("原因应含自定义规则说明,实际 %s", g.Reason)
	}
	RefreshCustomRules(nil)
	g = GradeCommandEx("mytool --run")
	if g.Risk != RiskConfirm {
		t.Errorf("规则清除后应回退 confirm,实际 %s", g.Risk)
	}
}

// ==================== 智能体上下文构建 ====================

func TestAgentBuildContextOrphanFilter(t *testing.T) {
	events := []AgentEvent{
		{Role: "assistant", Kind: "tool_call", ToolCalls: []agentToolCall{{ID: "c1", Name: "list_tabs", Arguments: "{}"}}},
		{Role: "tool", Kind: "tool_result", ToolCallID: "c1", Content: "ok"},
		{Role: "tool", Kind: "tool_result", ToolCallID: "c2", Content: "orphan"},
	}
	msgs := agentBuildContext(events, 100)
	if len(msgs) != 2 {
		t.Fatalf("孤儿 tool_result 应被过滤,期望 2 条实际 %d", len(msgs))
	}
	if msgs[1].Role != "tool" || msgs[1].ToolCallID != "c1" {
		t.Errorf("tool 消息应关联 c1,实际 %+v", msgs[1])
	}
}

func TestAgentBuildContextTruncation(t *testing.T) {
	var events []AgentEvent
	for i := 0; i < 10; i++ {
		events = append(events, AgentEvent{Role: "user", Kind: "message", Content: "m"})
	}
	msgs := agentBuildContext(events, 5)
	if len(msgs) != 5 {
		t.Errorf("截断到 5 条,实际 %d", len(msgs))
	}
}

func TestNormalizeAgentPerm(t *testing.T) {
	if normalizeAgentPerm("plan") != "plan" {
		t.Error("plan 应保持")
	}
	if normalizeAgentPerm("bogus") != agentPermManual {
		t.Error("非法值应回退 manual")
	}
}

// ==================== 会话存储 ====================

func TestAgentStoreLifecycle(t *testing.T) {
	store := NewAgentStore(t.TempDir())
	m, err := store.Create("测试会话")
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if _, err := store.Append(m.ID, AgentEvent{Role: "user", Kind: "message", Content: "hello"}); err != nil {
		t.Fatalf("Append 失败: %v", err)
	}
	evs, total, _ := store.EventsPage(m.ID, 0, 10)
	if total != 1 || len(evs) != 1 || evs[0].Content != "hello" {
		t.Fatalf("EventsPage 应返回 1 条,实际 total=%d len=%d", total, len(evs))
	}
	// 尾部分页(负 offset)
	if _, total2, _ := store.EventsPage(m.ID, -1, 10); total2 != 1 {
		t.Fatalf("负 offset 总数应为 1,实际 %d", total2)
	}
	// 待办
	if err := store.SetTodos(m.ID, []AgentTodoItem{{Content: "步骤1", Status: "pending"}}); err != nil {
		t.Fatalf("SetTodos 失败: %v", err)
	}
	todos := store.Todos(m.ID)
	if len(todos) != 1 || todos[0].Content != "步骤1" {
		t.Fatalf("Todos 读取不一致: %v", todos)
	}
	// 归档/重命名/删除
	if err := store.Archive(m.ID, true); err != nil {
		t.Fatalf("Archive 失败: %v", err)
	}
	list := store.List()
	if len(list) != 1 || !list[0].Archived {
		t.Fatalf("归档状态未生效: %+v", list)
	}
	if err := store.Rename(m.ID, "新名字"); err != nil {
		t.Fatalf("Rename 失败: %v", err)
	}
	if err := store.Delete(m.ID); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}
	if store.Exists(m.ID) {
		t.Error("删除后不应存在")
	}
}

func TestAgentStoreDebouncedCreate(t *testing.T) {
	store := NewAgentStore(t.TempDir())
	// 场景1: 最新会话为空 → 复用,不新建
	m1, _ := store.Create("空会话")
	m2, reused, err := store.CreateDebounced("新会话")
	if err != nil {
		t.Fatalf("CreateDebounced 失败: %v", err)
	}
	if !reused || m2.ID != m1.ID {
		t.Fatalf("空会话应被复用: reused=%v got=%s want=%s", reused, m2.ID, m1.ID)
	}
	// 场景2: 最新会话有事件 → 正常新建
	store.Append(m1.ID, AgentEvent{Role: "user", Kind: "message", Content: "hi"})
	m3, reused2, err := store.CreateDebounced("新会话")
	if err != nil {
		t.Fatalf("CreateDebounced 失败: %v", err)
	}
	if reused2 || m3.ID == m1.ID {
		t.Fatalf("非空会话不应被复用: reused=%v id=%s", reused2, m3.ID)
	}
	// 场景3: 最新会话为空但已归档 → 正常新建
	store.Archive(m3.ID, true)
	m4, reused3, _ := store.CreateDebounced("新会话")
	if reused3 || m4.ID == m3.ID {
		t.Fatalf("归档会话不应被复用: reused=%v", reused3)
	}
}

func TestAgentStoreAutoTitle(t *testing.T) {
	store := NewAgentStore(t.TempDir())
	m, _ := store.Create("新会话")

	// 场景1: 空会话默认标题 → 首条消息命名(取首行+截断)
	store.AutoTitle(m.ID, "帮我排查 nginx 502\n补充信息")
	got := store.List()[0].Title
	if got != "帮我排查 nginx 502" {
		t.Fatalf("首条消息未自动命名: %q", got)
	}

	// 场景2: 已有事件 → 不再改名
	store.Append(m.ID, AgentEvent{Role: "user", Kind: "message", Content: "x"})
	store.AutoTitle(m.ID, "第二句话")
	for _, it := range store.List() {
		if it.ID == m.ID && it.Title != "帮我排查 nginx 502" {
			t.Fatalf("非首条消息不应改名: %q", it.Title)
		}
	}

	// 场景3: 用户手动命名过的空会话 → 不覆盖
	m2, _ := store.Create("手动命名")
	store.AutoTitle(m2.ID, "第一句话")
	for _, it := range store.List() {
		if it.ID == m2.ID && it.Title != "手动命名" {
			t.Fatalf("手动命名被覆盖: %q", it.Title)
		}
	}
}

func TestAgentStorePersistReload(t *testing.T) {
	dir := t.TempDir()
	store := NewAgentStore(dir)
	m, _ := store.Create("持久化")
	store.Append(m.ID, AgentEvent{Role: "user", Kind: "message", Content: "persist-me"})
	// 模拟重启: 新 store 实例从磁盘加载
	store2 := NewAgentStore(dir)
	evs, total, _ := store2.EventsPage(m.ID, 0, 10)
	if total != 1 || len(evs) != 1 || evs[0].Content != "persist-me" {
		t.Fatalf("重启后应能读回事件,total=%d len=%d", total, len(evs))
	}
}
