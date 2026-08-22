package services

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// 智能体会话存储: jsonl 逐事件追加持久化。
//
// 文件布局(数据目录/agent/sessions/):
//   - <id>.meta.json   会话元数据(标题/归档/待办/时间戳)
//   - <id>.events.jsonl 事件流(每行一个事件,追加写)
//
// 有界性(防内存无限增长):
//   - 内存常驻会话 LRU 上限 8 个(其余按需从磁盘加载)
//   - 单会话内存事件上限 5000 条(超出丢弃头部,磁盘全量保留)
//   - 分页读取: 内存不足时回源磁盘

const (
	agentSessionLRUCap = 8    // 内存常驻会话数上限(LRU)
	agentEventMemCap   = 5000 // 单会话内存事件上限(超出丢弃头部)
)

// AgentTodoItem 待办清单条目。
type AgentTodoItem struct {
	Content string `json:"content"`
	Status  string `json:"status"` // pending / in_progress / done
}

// AgentEvent 会话事件(持久化与前端渲染的最小单元)。
type AgentEvent struct {
	ID         string          `json:"id"`                   // e-<n>(会话内单调递增)
	TS         string          `json:"ts"`                   // ISO 时间戳
	Role       string          `json:"role"`                 // user / assistant / tool / system
	Kind       string          `json:"kind"`                 // message / tool_call / tool_result / error
	Content    string          `json:"content,omitempty"`    // 文本内容(消息正文/工具结果/错误信息)
	ToolName   string          `json:"toolName,omitempty"`   // 工具名(tool_result 用)
	ToolArgs   string          `json:"toolArgs,omitempty"`   // 工具参数 JSON 预览
	ToolCallID string          `json:"toolCallId,omitempty"` // 关联的调用 ID
	ToolCalls  []agentToolCall `json:"toolCalls,omitempty"`  // 工具调用列表(tool_call 用)
	Todos      []AgentTodoItem `json:"todos,omitempty"`      // 最新待办(todo 更新结果)
	TokensIn     int64           `json:"tokensIn,omitempty"`     // 回合输入 token(缓存未命中,最终回答事件携带)
	TokensCached int64           `json:"tokensCached,omitempty"` // 回合输入 token(缓存命中)
	TokensOut    int64           `json:"tokensOut,omitempty"`    // 回合输出 token
	Ok           bool            `json:"ok"`                     // 工具执行是否成功
}

// AgentSessionMeta 会话元数据。
type AgentSessionMeta struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	CreatedAt string          `json:"createdAt"`
	UpdatedAt string          `json:"updatedAt"`
	Archived  bool            `json:"archived"`
	Todos     []AgentTodoItem `json:"todos"`
	Skills    []string        `json:"skills"` // 已选技能 ID(上限5)
}

// agentSessionState 会话内存态(事件缓存 + 元数据)。
type agentSessionState struct {
	meta    AgentSessionMeta
	events  []AgentEvent // 尾部窗口(与磁盘同步追加)
	memSkip int          // 内存中已丢弃的头部事件数
	lastUse time.Time
}

// AgentStore 会话存储。
type AgentStore struct {
	mu       sync.Mutex
	dir      string
	metas    map[string]*AgentSessionMeta // 全部会话元数据(轻量,常驻)
	sessions map[string]*agentSessionState
}

// NewAgentStore 创建存储并加载全部会话元数据。
func NewAgentStore(dataDir string) *AgentStore {
	dir := filepath.Join(dataDir, "agent", "sessions")
	os.MkdirAll(dir, 0700)
	s := &AgentStore{
		dir:      dir,
		metas:    make(map[string]*AgentSessionMeta),
		sessions: make(map[string]*agentSessionState),
	}
	s.loadAll()
	return s
}

// loadAll 扫描目录加载全部元数据(损坏文件跳过,失败安全)。
func (s *AgentStore) loadAll() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".meta.json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var m AgentSessionMeta
		if json.Unmarshal(data, &m) != nil || m.ID == "" {
			continue
		}
		if m.Title == "" {
			m.Title = "未命名会话"
		}
		s.metas[m.ID] = &m
	}
}

// List 返回全部会话元数据(按更新时间倒序)。
func (s *AgentStore) List() []AgentSessionMeta {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]AgentSessionMeta, 0, len(s.metas))
	for _, m := range s.metas {
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Archived != out[j].Archived {
			return !out[i].Archived // 未归档在前
		}
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	return out
}

// Create 新建会话。
func (s *AgentStore) Create(title string) (AgentSessionMeta, error) {
	if title == "" {
		title = "新会话"
	}
	title = truncateUtf8(title, 60)
	buf := make([]byte, 4)
	rand.Read(buf)
	id := fmt.Sprintf("s-%d-%s", time.Now().UnixMilli(), hex.EncodeToString(buf))
	now := time.Now().Format(time.RFC3339)
	m := AgentSessionMeta{
		ID:        id,
		Title:     title,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.saveMetaLocked(m); err != nil {
		return AgentSessionMeta{}, err
	}
	s.mu.Lock()
	s.metas[id] = &m
	s.mu.Unlock()
	return m, nil
}

// CreateDebounced 新建会话(防抖): 最新未归档会话若完全无事件,复用它而非重复创建。
// 返回 reused=true 表示复用了既有空会话。
func (s *AgentStore) CreateDebounced(title string) (AgentSessionMeta, bool, error) {
	s.mu.Lock()
	var newest *AgentSessionMeta
	for _, m := range s.metas {
		if m.Archived {
			continue
		}
		if newest == nil || m.UpdatedAt > newest.UpdatedAt {
			newest = m
		}
	}
	if newest != nil && s.eventCountFile(newest.ID) == 0 {
		m := *newest
		s.mu.Unlock()
		return m, true, nil
	}
	s.mu.Unlock()
	m, err := s.Create(title)
	return m, false, err
}

// eventCountFile 统计会话磁盘事件数(jsonl 非空行;磁盘全量保留,调用方可持锁)。
func (s *AgentStore) eventCountFile(id string) int {
	data, err := os.ReadFile(filepath.Join(s.dir, id+".events.jsonl"))
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// AutoTitle 首条消息自动命名: 会话无事件且标题为默认值时,标题改为首行文本(截断60)。
func (s *AgentStore) AutoTitle(id string, text string) {
	first := strings.TrimSpace(strings.SplitN(text, "\n", 2)[0])
	if first == "" {
		return
	}
	s.mu.Lock()
	meta, ok := s.metas[id]
	if !ok {
		s.mu.Unlock()
		return
	}
	if t := meta.Title; t != "" && t != "新会话" && t != "未命名会话" {
		s.mu.Unlock()
		return // 用户已手动命名,不覆盖
	}
	n := s.eventCountFile(id)
	s.mu.Unlock()
	if n > 0 {
		return // 非首条消息
	}
	s.Rename(id, truncateUtf8(first, 60))
}

// Get 获取会话状态(不在内存则从磁盘加载;LRU 淘汰)。
func (s *AgentStore) Get(id string) (*agentSessionState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getLocked(id)
}

// getLocked 获取/加载会话(调用方持锁)。
func (s *AgentStore) getLocked(id string) (*agentSessionState, error) {
	meta, ok := s.metas[id]
	if !ok {
		return nil, fmt.Errorf("会话不存在: %s", id)
	}
	st, ok := s.sessions[id]
	if !ok {
		events, skip := s.loadEventsLocked(id)
		st = &agentSessionState{meta: *meta, events: events, memSkip: skip, lastUse: time.Now()}
		s.sessions[id] = st
		s.evictLocked(id) // 新加载后触发 LRU 淘汰
	}
	st.lastUse = time.Now()
	// 元数据可能被其它路径更新(List 返回副本),以 metas 为准
	st.meta = *meta
	return st, nil
}

// evictLocked LRU 淘汰(保护 excludeID;调用方持锁)。
func (s *AgentStore) evictLocked(excludeID string) {
	for len(s.sessions) > agentSessionLRUCap {
		var oldestID string
		var oldest time.Time
		for id, st := range s.sessions {
			if id == excludeID {
				continue
			}
			if oldestID == "" || st.lastUse.Before(oldest) {
				oldestID = id
				oldest = st.lastUse
			}
		}
		if oldestID == "" {
			return
		}
		delete(s.sessions, oldestID)
	}
}

// loadEventsLocked 从磁盘加载事件(尾窗最多 agentEventMemCap 条;调用方持锁)。
func (s *AgentStore) loadEventsLocked(id string) ([]AgentEvent, int) {
	data, err := os.ReadFile(s.eventsPath(id))
	if err != nil {
		return nil, 0
	}
	var events []AgentEvent
	start := 0
	for len(data) > 0 {
		i := 0
		for i < len(data) && data[i] != '\n' {
			i++
		}
		line := data[:i]
		if i < len(data) {
			data = data[i+1:]
		} else {
			data = nil
		}
		if len(line) == 0 {
			continue
		}
		if start == 0 && len(events) >= agentEventMemCap {
			// 超出内存上限: 整体前移丢弃头部
			events = events[1:]
			start++
		}
		var ev AgentEvent
		if json.Unmarshal(line, &ev) == nil {
			events = append(events, ev)
		}
	}
	return events, start
}

// Append 追加事件(内存 + 磁盘 + 元数据时间戳)。
func (s *AgentStore) Append(id string, ev AgentEvent) (AgentEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.getLocked(id)
	if err != nil {
		return AgentEvent{}, err
	}
	if ev.TS == "" {
		ev.TS = time.Now().Format(time.RFC3339)
	}
	ev.ID = fmt.Sprintf("e-%d", st.memSkip+len(st.events)+1)
	st.events = append(st.events, ev)
	if len(st.events) > agentEventMemCap {
		st.events = st.events[len(st.events)-agentEventMemCap:]
		st.memSkip++
	}
	// 落盘(追加一行)
	data, err := json.Marshal(ev)
	if err != nil {
		return ev, err
	}
	f, err := os.OpenFile(s.eventsPath(id), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return ev, err
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return ev, err
	}
	// 更新元数据
	st.meta.UpdatedAt = ev.TS
	*s.metas[id] = st.meta
	s.saveMetaLocked(st.meta)
	return ev, nil
}

// EventsPage 分页读取事件。
// offset 为全量序列下标(负数从尾部倒数);返回 {events, total, offset, limit}。
func (s *AgentStore) EventsPage(id string, offset, limit int) ([]AgentEvent, int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.getLocked(id)
	if err != nil {
		return []AgentEvent{}, 0, 0
	}
	total := st.memSkip + len(st.events)
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = total + offset
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return []AgentEvent{}, total, offset
	}
	end := offset + limit
	if end > total {
		end = total
	}
	// 内存窗口内的直接切片
	if offset >= st.memSkip {
		return append([]AgentEvent{}, st.events[offset-st.memSkip:end-st.memSkip]...), total, offset
	}
	// 头部已被淘汰: 回源磁盘全量读取
	var all []AgentEvent
	data, err := os.ReadFile(s.eventsPath(id))
	if err != nil {
		return []AgentEvent{}, total, offset
	}
	for len(data) > 0 {
		i := 0
		for i < len(data) && data[i] != '\n' {
			i++
		}
		line := data[:i]
		if i < len(data) {
			data = data[i+1:]
		} else {
			data = nil
		}
		if len(line) == 0 {
			continue
		}
		var ev AgentEvent
		if json.Unmarshal(line, &ev) == nil {
			all = append(all, ev)
		}
	}
	if offset >= len(all) {
		return []AgentEvent{}, total, offset
	}
	if end > len(all) {
		end = len(all)
	}
	return append([]AgentEvent{}, all[offset:end]...), total, offset
}

// AllEvents 返回内存窗口内全部事件(构建 LLM 上下文用)。
func (s *AgentStore) AllEvents(id string) ([]AgentEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.getLocked(id)
	if err != nil {
		return nil, err
	}
	return append([]AgentEvent{}, st.events...), nil
}

// Todos 返回会话当前待办。
func (s *AgentStore) Todos(id string) []AgentTodoItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.metas[id]; ok {
		return m.Todos
	}
	return nil
}

// SetTodos 更新会话待办并持久化。
func (s *AgentStore) SetTodos(id string, todos []AgentTodoItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.getLocked(id)
	if err != nil {
		return err
	}
	st.meta.Todos = todos
	st.meta.UpdatedAt = time.Now().Format(time.RFC3339)
	*s.metas[id] = st.meta
	return s.saveMetaLocked(st.meta)
}

// Skills 返回会话已选技能 ID 列表。
func (s *AgentStore) Skills(id string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.metas[id]; ok {
		return m.Skills
	}
	return nil
}

// SetSkills 设置会话技能并持久化。
func (s *AgentStore) SetSkills(id string, skills []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.getLocked(id)
	if err != nil {
		return err
	}
	st.meta.Skills = skills
	st.meta.UpdatedAt = time.Now().Format(time.RFC3339)
	*s.metas[id] = st.meta
	return s.saveMetaLocked(st.meta)
}

// TrimTailForRegenerate 刷新对话: 从事件流尾部移除到最后一条用户消息为止
// (该用户消息也一并移除,返回其文本用于重跑);磁盘与内存同步裁剪。
func (s *AgentStore) TrimTailForRegenerate(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.getLocked(id)
	if err != nil {
		return "", err
	}
	// 头部已淘汰 → 回源磁盘全量(防内存窗口不完整导致丢事件)
	var events []AgentEvent
	if st.memSkip > 0 {
		events = s.readAllEventsLocked(id)
		if events == nil {
			return "", fmt.Errorf("读取事件文件失败")
		}
	} else {
		events = st.events
	}
	lastUser := -1
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Role == "user" && events[i].Kind == "message" {
			lastUser = i
			break
		}
	}
	if lastUser < 0 {
		return "", nil // 没有可重跑的用户消息
	}
	text := events[lastUser].Content
	keep := events[:lastUser] // 移除该用户消息及其后全部
	if err := s.rewriteEventsLocked(id, keep); err != nil {
		return "", err
	}
	// 内存窗口同步(可能比之前的窗口更大,仍受 memCap 约束)
	if len(keep) > agentEventMemCap {
		st.events = append([]AgentEvent{}, keep[len(keep)-agentEventMemCap:]...)
		st.memSkip = len(keep) - agentEventMemCap
	} else {
		st.events = append([]AgentEvent{}, keep...)
		st.memSkip = 0
	}
	return text, nil
}

// readAllEventsLocked 从磁盘读取全量事件(调用方持锁)。
func (s *AgentStore) readAllEventsLocked(id string) []AgentEvent {
	data, err := os.ReadFile(s.eventsPath(id))
	if err != nil {
		return nil
	}
	var events []AgentEvent
	for len(data) > 0 {
		i := 0
		for i < len(data) && data[i] != '\n' {
			i++
		}
		line := data[:i]
		if i < len(data) {
			data = data[i+1:]
		} else {
			data = nil
		}
		if len(line) == 0 {
			continue
		}
		var ev AgentEvent
		if json.Unmarshal(line, &ev) == nil {
			events = append(events, ev)
		}
	}
	return events
}

// rewriteEventsLocked 全量重写事件文件(内存窗口部分;调用方持锁)。
// 注意: 仅当头部未淘汰(memSkip==0)时可用于裁剪;此处重跑场景必须保 memSkip==0。
func (s *AgentStore) rewriteEventsLocked(id string, events []AgentEvent) error {
	var b []byte
	for _, ev := range events {
		data, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		b = append(b, data...)
		b = append(b, '\n')
	}
	return atomicWriteFile(s.eventsPath(id), b, 0600)
}

// Rename 重命名会话。
func (s *AgentStore) Rename(id, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("标题为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.getLocked(id)
	if err != nil {
		return err
	}
	st.meta.Title = truncateUtf8(title, 60)
	st.meta.UpdatedAt = time.Now().Format(time.RFC3339)
	*s.metas[id] = st.meta
	return s.saveMetaLocked(st.meta)
}

// Archive 设置归档状态。
func (s *AgentStore) Archive(id string, archived bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.getLocked(id)
	if err != nil {
		return err
	}
	st.meta.Archived = archived
	st.meta.UpdatedAt = time.Now().Format(time.RFC3339)
	*s.metas[id] = st.meta
	return s.saveMetaLocked(st.meta)
}

// Delete 删除会话(磁盘文件一并移除)。
func (s *AgentStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.metas[id]; !ok {
		return fmt.Errorf("会话不存在: %s", id)
	}
	delete(s.metas, id)
	delete(s.sessions, id)
	os.Remove(s.metaPath(id))
	os.Remove(s.eventsPath(id))
	return nil
}

// Exists 会话是否存在。
func (s *AgentStore) Exists(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.metas[id]
	return ok
}

func (s *AgentStore) metaPath(id string) string {
	return filepath.Join(s.dir, id+".meta.json")
}

func (s *AgentStore) eventsPath(id string) string {
	return filepath.Join(s.dir, id+".events.jsonl")
}

// saveMetaLocked 元数据落盘(调用方持锁)。
func (s *AgentStore) saveMetaLocked(m AgentSessionMeta) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(s.metaPath(m.ID), data, 0600)
}
