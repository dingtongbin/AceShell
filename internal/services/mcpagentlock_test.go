package services

// mcpAgentLock 单元测试: 覆盖独占/续期/等待释放/抢占/空闲接管/强制释放。
// 空闲判定用可注入时钟,等待截止用真实时钟,两者解耦。

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeClock 可控时钟。
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{t: time.Now()} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// newTestLock 构造可测锁: idle=空闲接管阈值,wait=竞争等待上限。
func newTestLock(idle, wait time.Duration) (*mcpAgentLock, *fakeClock) {
	l := newMcpAgentLock()
	c := newFakeClock()
	l.now = c.Now
	l.idleFor = idle
	l.waitFor = wait
	return l, c
}

func TestAgentLockExclusiveAndRenew(t *testing.T) {
	l, _ := newTestLock(time.Hour, 100*time.Millisecond)
	ctx := context.Background()

	acq, err := l.acquire(ctx, "agentA", "智能体A", 1)
	if err != nil || acq.Kind != mcpAcqNew {
		t.Fatalf("首次获取失败: %+v, %v", acq, err)
	}
	acq, err = l.acquire(ctx, "agentA", "智能体A", 1)
	if err != nil || acq.Kind != mcpAcqRenew {
		t.Fatalf("同主应续期: %+v, %v", acq, err)
	}
	if _, err := l.acquire(ctx, mcpLockKeyExternalPrefix+"s1", "opencode#s1", mcpPrioExternal); err == nil || !strings.Contains(err.Error(), "MCP_BUSY") {
		t.Fatalf("他主应报 MCP_BUSY, got %v", err)
	}
	if !l.release("agentA") {
		t.Fatal("持有者释放应成功")
	}
	acq, err = l.acquire(ctx, mcpLockKeyExternalPrefix+"s1", "opencode#s1", mcpPrioExternal)
	if err != nil || acq.Kind != mcpAcqNew {
		t.Fatalf("释放后应可获取: %+v, %v", acq, err)
	}
}

func TestAgentLockWaitForRelease(t *testing.T) {
	l, _ := newTestLock(time.Hour, 2*time.Second)
	ctx := context.Background()
	if _, err := l.acquire(ctx, "extA", "claude#aaaa", mcpPrioExternal); err != nil {
		t.Fatalf("A 获取失败: %v", err)
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		l.release("extA")
	}()
	acq, err := l.acquire(ctx, "extB", "opencode#bbbb", mcpPrioExternal)
	if err != nil || acq.Kind != mcpAcqNew {
		t.Fatalf("等待释放后应获得: %+v, %v", acq, err)
	}
}

func TestAgentLockHighPrioPreemptsLow(t *testing.T) {
	l, _ := newTestLock(time.Hour, 100*time.Millisecond)
	ctx := context.Background()
	if _, err := l.acquire(ctx, mcpLockKeyExternalPrefix+"s1", "opencode#s1", 2); err != nil {
		t.Fatalf("低优获取失败: %v", err)
	}
	acq, err := l.acquire(ctx, "agentA", "智能体A", 1)
	if err != nil || acq.Kind != mcpAcqPreempt || acq.Prev != "opencode#s1" {
		t.Fatalf("高优应抢占低优: %+v, %v", acq, err)
	}
	// 低优此时抢不回来(高优活跃)
	if _, err := l.acquire(ctx, mcpLockKeyExternalPrefix+"s1", "opencode#s1", 2); err == nil {
		t.Fatal("低优不应夺回高优活跃持有的锁")
	}
}

func TestAgentLockIdleTakeover(t *testing.T) {
	l, clock := newTestLock(90*time.Second, 100*time.Millisecond)
	ctx := context.Background()
	if _, err := l.acquire(ctx, "extA", "claude#aaaa", mcpPrioExternal); err != nil {
		t.Fatalf("A 获取失败: %v", err)
	}
	clock.Advance(91 * time.Second) // 前主停手超时
	acq, err := l.acquire(ctx, "extB", "opencode#bbbb", mcpPrioExternal)
	if err != nil || acq.Kind != mcpAcqTakeover || acq.Prev != "claude#aaaa" {
		t.Fatalf("空闲应被接管: %+v, %v", acq, err)
	}
}

func TestAgentLockWaitTimeout(t *testing.T) {
	l, _ := newTestLock(time.Hour, 80*time.Millisecond)
	ctx := context.Background()
	if _, err := l.acquire(ctx, "extA", "claude#aaaa", mcpPrioExternal); err != nil {
		t.Fatalf("A 获取失败: %v", err)
	}
	if _, err := l.acquire(ctx, "extB", "opencode#bbbb", mcpPrioExternal); err == nil || !strings.Contains(err.Error(), "MCP_BUSY") {
		t.Fatalf("等待超时应报 MCP_BUSY, got %v", err)
	}
}

func TestAgentLockClear(t *testing.T) {
	l, _ := newTestLock(time.Hour, 50*time.Millisecond)
	ctx := context.Background()
	if _, err := l.acquire(ctx, mcpLockKeyExternalPrefix+"s1", "opencode#s1", mcpPrioExternal); err != nil {
		t.Fatalf("获取失败: %v", err)
	}
	if prev := l.clear(); prev != "opencode#s1" {
		t.Fatalf("clear 应返回前任持有者, got %q", prev)
	}
	if s := l.snapshot(); s != nil {
		t.Fatalf("clear 后快照应为 nil, got %+v", s)
	}
	if prev := l.clear(); prev != "" {
		t.Fatalf("空锁 clear 应返回空, got %q", prev)
	}
}

func TestAgentLockExternalIdentityLabel(t *testing.T) {
	id := newMcpClientIdentity("opencode")
	if !strings.HasPrefix(id.key, mcpLockKeyExternalPrefix) || len(id.key) != len(mcpLockKeyExternalPrefix)+8 {
		t.Fatalf("关联 ID 应为 8 位随机: %q", id.key)
	}
	if label := id.label(); !strings.HasPrefix(label, "opencode#") {
		t.Fatalf("展示名应带客户端名+短码: %q", label)
	}
	id2 := newMcpClientIdentity("")
	if label := id2.label(); !strings.HasPrefix(label, "外部智能体#") {
		t.Fatalf("无名应退化为外部智能体: %q", label)
	}
	if id.key == id2.key {
		t.Fatal("两次构造的关联 ID 不应重复")
	}
}

func TestAgentLockSnapshot(t *testing.T) {
	l, clock := newTestLock(time.Hour, 50*time.Millisecond)
	ctx := context.Background()
	id := newMcpClientIdentity("opencode")
	if _, err := l.acquire(ctx, id.key, id.label(), mcpPrioExternal); err != nil {
		t.Fatalf("获取失败: %v", err)
	}
	clock.Advance(5 * time.Second)
	s := l.snapshot()
	if s == nil || s.Kind != "external" || !strings.HasPrefix(s.Label, "opencode#") || s.HeldForSec < 4 || s.IdleSec < 4 {
		t.Fatalf("快照异常: %+v", s)
	}
}
