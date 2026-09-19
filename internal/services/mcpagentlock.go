package services

// 智能体级独占锁(GIL 语义): 同一时刻仅允许一个智能体持有 MCP 操作权。
//
// 规则:
//   - 同一持有者再次调用 = 续期(刷新活跃时间)
//   - 其他智能体: 有界等待释放,超时报 MCP_BUSY(调用方把话术转告其模型自行重试)
//   - 优先级抢占: 内嵌智能体(P1)可立即接管外部客户端(P2);外部之间永不抢占
//     (与仲裁车道"用户 > 内嵌 > 外部"的取向一致)
//   - 空闲释放: 持有者超过 idleFor 无活动视为已停手,竞争者可直接接管——
//     智能体没有"我用完了"的显式信号,租约式空闲判定是唯一可靠的释放途径
//   - 用户挂起/键盘抢占/服务停止/内嵌轮次结束/外部会话关闭 = 立即释放

import (
	"context"
	"fmt"
	"sync"
	"time"
)

const (
	mcpLockKeyExternalPrefix = "external:"
	mcpLockDefaultIdle       = 90 * time.Second // 持有者空闲多久后可被接管
	mcpLockDefaultWait       = 20 * time.Second // 竞争者等待释放的上限
)

// mcpAcqKind acquire 造成的所有权变化类型(供审计与状态推送)。
type mcpAcqKind int

const (
	mcpAcqRenew    mcpAcqKind = iota // 同主续期,无事件
	mcpAcqNew                        // 锁空闲,直接持有
	mcpAcqPreempt                    // 高优先级抢占
	mcpAcqTakeover                   // 前持有者空闲超时,接管
)

// mcpAcqResult 获取结果。Prev 为被接管的上一任持有者展示名(Preempt/Takeover 时有效)。
type mcpAcqResult struct {
	Kind mcpAcqKind
	Prev string
}

// mcpLockSnapshot 锁状态快照(前端展示;无持有者时为 nil)。
type mcpLockSnapshot struct {
	Owner      string `json:"owner"`
	Label      string `json:"label"`
	Kind       string `json:"kind"` // 持有者类别(当前恒为 external,保留字段兼容前端)
	HeldForSec int    `json:"heldForSec"`
	IdleSec    int    `json:"idleSec"`
}

// mcpAgentLock GIL 式独占锁。无等待队列: 竞争者在信号通道上有界等待,
// 所有权每次变化广播一次(close + 换新),唤醒全部等待者重新判定。
//
// 全部方法容忍 nil 接收者: 零值 McpService(测试字面量构造)即"无锁语义",
// acquire 恒放行、快照恒空,与 McpService 其他零值容忍惯例一致。
type mcpAgentLock struct {
	mu      sync.Mutex
	owner   string // 持有者标识(外部 "external:<关联ID>")
	label   string // 展示名(审计与前端)
	prio    int
	since   time.Time // 本任持有者的获取时间
	lastAct time.Time // 最近一次续期时间(空闲判定基准)
	idleFor time.Duration
	waitFor time.Duration
	now     func() time.Time // 可注入时钟(测试空闲判定)
	changed chan struct{}
}

func newMcpAgentLock() *mcpAgentLock {
	return &mcpAgentLock{
		idleFor: mcpLockDefaultIdle,
		waitFor: mcpLockDefaultWait,
		now:     time.Now,
		changed: make(chan struct{}),
	}
}

// takeLocked 换主(调用方持锁)。
func (l *mcpAgentLock) takeLocked(key, label string, prio int) {
	l.owner = key
	l.label = label
	l.prio = prio
	now := l.now()
	l.since = now
	l.lastAct = now
	close(l.changed)
	l.changed = make(chan struct{})
}

// clearLocked 释放(调用方持锁),返回是否确有持有者被释放。
func (l *mcpAgentLock) clearLocked() bool {
	if l.owner == "" {
		return false
	}
	l.owner = ""
	l.label = ""
	l.prio = 0
	close(l.changed)
	l.changed = make(chan struct{})
	return true
}

// acquire 获取独占权。
// 同主续期;他主时内嵌抢占外部、前主空闲超限则接管,否则有界等待,超时报
// MCP_BUSY。等待期用真实时钟计截止,空闲判定用可注入时钟,两者解耦。
func (l *mcpAgentLock) acquire(ctx context.Context, key, label string, prio int) (mcpAcqResult, error) {
	if l == nil {
		return mcpAcqResult{Kind: mcpAcqRenew}, nil
	}
	deadline := time.Now().Add(l.waitFor)
	for {
		l.mu.Lock()
		if l.owner == key {
			l.lastAct = l.now()
			l.mu.Unlock()
			return mcpAcqResult{Kind: mcpAcqRenew}, nil
		}
		holder := l.label
		switch {
		case l.owner == "":
			l.takeLocked(key, label, prio)
			l.mu.Unlock()
			return mcpAcqResult{Kind: mcpAcqNew}, nil
		case prio < l.prio: // 内嵌(P1)抢占外部(P2)
			l.takeLocked(key, label, prio)
			l.mu.Unlock()
			return mcpAcqResult{Kind: mcpAcqPreempt, Prev: holder}, nil
		case l.now().Sub(l.lastAct) >= l.idleFor:
			l.takeLocked(key, label, prio)
			l.mu.Unlock()
			return mcpAcqResult{Kind: mcpAcqTakeover, Prev: holder}, nil
		}
		sig := l.changed
		l.mu.Unlock()

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return mcpAcqResult{}, fmt.Errorf("MCP_BUSY: 操作权正被 %s 占用,请稍后重试", holder)
		}
		timer := time.NewTimer(remaining)
		select {
		case <-sig:
			timer.Stop()
		case <-timer.C:
		case <-ctx.Done():
			return mcpAcqResult{}, fmt.Errorf("MCP_BUSY: 等待 %s 释放操作权期间请求被取消", holder)
		}
	}
}

// release 持有者主动释放(仅 owner 匹配时生效)。
func (l *mcpAgentLock) release(key string) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.owner != key {
		return false
	}
	return l.clearLocked()
}

// clear 无条件释放(挂起/抢占/停止),返回被释放的持有者展示名(无则空)。
func (l *mcpAgentLock) clear() string {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	prev := l.label
	l.clearLocked()
	return prev
}

// snapshot 当前持有者快照(空闲时 nil)。
func (l *mcpAgentLock) snapshot() *mcpLockSnapshot {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.owner == "" {
		return nil
	}
	now := l.now()
	return &mcpLockSnapshot{
		Owner:      l.owner,
		Label:      l.label,
		Kind:       "external",
		HeldForSec: int(now.Sub(l.since) / time.Second),
		IdleSec:    int(now.Sub(l.lastAct) / time.Second),
	}
}
