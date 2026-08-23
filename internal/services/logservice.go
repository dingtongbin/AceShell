package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// LogService 管理会话自动日志记录。
type LogService struct {
	mu        sync.Mutex
	metaDir   string
	logDir    string
	buffer    map[string][]string
	lastFlush map[string]time.Time
	done      chan struct{}
}

// LogSessionMeta 会话日志元数据。
type LogSessionMeta struct {
	SessionID  string `toml:"sessionID"`
	Protocol   string `toml:"protocol"`
	Host       string `toml:"host"`
	Port       int    `toml:"port"`
	Username   string `toml:"username"`
	Title      string `toml:"title"`
	StartTime  string `toml:"startTime"`
	EndTime    string `toml:"endTime,omitempty"`
	TotalLines int    `toml:"totalLines"`
	TotalBytes int    `toml:"totalBytes"`
}

// validLogID 校验日志会话 ID 合法性,拒绝空值或含路径分隔符/遍历段的 ID(防路径穿越)。
func validLogID(id string) bool {
	return id != "" && !strings.ContainsAny(id, "/\\") && !strings.Contains(id, "..")
}

func (l *LogService) Init() {
	l.metaDir = filepath.Join(AutoLogDir(), "meta")
	l.logDir = filepath.Join(AutoLogDir(), "logs")
	l.buffer = make(map[string][]string)
	l.lastFlush = make(map[string]time.Time)
	l.done = make(chan struct{})

	os.MkdirAll(l.metaDir, 0700)
	os.MkdirAll(l.logDir, 0700)

	go l.flushLoop()
}

func (l *LogService) flushLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			l.flushAll()
		case <-l.done:
			l.flushAll()
			return
		}
	}
}

func (l *LogService) Stop() {
	close(l.done)
}

func (l *LogService) flushAll() {
	l.mu.Lock()
	pending := make(map[string][]string)
	for id, lines := range l.buffer {
		if len(lines) > 0 {
			pending[id] = lines
			l.buffer[id] = nil
		}
	}
	l.mu.Unlock()
	for id, lines := range pending {
		l.flushLog(id, lines)
	}
}

func (l *LogService) flushLog(id string, lines []string) {
	logPath := filepath.Join(l.logDir, id+".log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		fmt.Printf("log flush error: %v\n", err)
		return
	}
	defer f.Close()

	for _, line := range lines {
		f.WriteString(line)
	}
}

// StartSession 记录会话开始，创建元数据文件。
func (l *LogService) StartSession(id, protocol, host string, port int, username, title string) {
	if !validLogID(id) {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	meta := LogSessionMeta{
		SessionID: id,
		Protocol:  protocol,
		Host:      host,
		Port:      port,
		Username:  username,
		Title:     title,
		StartTime: time.Now().Format(time.RFC3339),
	}

	data, _ := toml.Marshal(meta)
	os.WriteFile(filepath.Join(l.metaDir, id+".toml"), data, 0600)
}

// LogOutput 记录会话输出（加入缓冲区，定时批量写入）。
func (l *LogService) LogOutput(id string, data string) {
	if !validLogID(id) {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buffer[id] = append(l.buffer[id], data)
}

// EndSession 记录会话结束，更新元数据。
func (l *LogService) EndSession(id string) {
	if !validLogID(id) {
		return
	}
	l.mu.Lock()
	var lines []string
	if ls, ok := l.buffer[id]; ok && len(ls) > 0 {
		lines = ls
		delete(l.buffer, id)
	}
	l.mu.Unlock()
	if len(lines) > 0 {
		l.flushLog(id, lines)
	}

	// 更新元数据(锁外执行,避免持锁做 I/O)
	metaPath := filepath.Join(l.metaDir, id+".toml")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return
	}
	var meta LogSessionMeta
	if err := toml.Unmarshal(data, &meta); err != nil {
		return
	}
	meta.EndTime = time.Now().Format(time.RFC3339)

	logPath := filepath.Join(l.logDir, id+".log")
	logData, _ := os.ReadFile(logPath)
	meta.TotalLines = len(strings.Split(string(logData), "\n"))
	meta.TotalBytes = len(logData)

	updated, _ := toml.Marshal(meta)
	os.WriteFile(metaPath, updated, 0600)
}

// LogNode 日志树节点。
type LogNode struct {
	Name     string     `json:"name"`
	Path     string     `json:"path"`
	IsDir    bool       `json:"isDir"`
	Protocol string     `json:"protocol,omitempty"`
	Children []*LogNode `json:"children,omitempty"`
	sortTime time.Time  `json:"-"`
}

// GetLogTree 返回日志树结构 JSON。
func (l *LogService) GetLogTree() string {
	entries, err := os.ReadDir(l.metaDir)
	if err != nil {
		return "[]"
	}

	protocolMap := make(map[string][]*LogNode)

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".toml") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".toml")
		metaPath := filepath.Join(l.metaDir, entry.Name())
		metaData, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var meta LogSessionMeta
		if err := toml.Unmarshal(metaData, &meta); err != nil {
			continue
		}

		node := &LogNode{
			Name:     fmt.Sprintf("%s - %s (%s)", meta.Title, meta.Host, meta.Protocol),
			Path:     id,
			IsDir:    false,
			Protocol: meta.Protocol,
			sortTime: parseMetaTime(meta.StartTime),
		}
		protocolMap[meta.Protocol] = append(protocolMap[meta.Protocol], node)
	}

	var tree []*LogNode
	for _, proto := range []string{"ssh", "telnet", "serial", "shell"} {
		nodes, ok := protocolMap[proto]
		if !ok {
			continue
		}
		// 本地终端（shell）按终端名二次分组：本地终端 → 终端名 → 日志
		if proto == "shell" {
			byName := make(map[string][]*LogNode)
			for _, n := range nodes {
				name := shellDisplayName(n.Name)
				byName[name] = append(byName[name], n)
			}
			type namedGroup struct {
				name   string
				nodes  []*LogNode
				latest time.Time
			}
			var groups []namedGroup
			for _, name := range sortedKeys(byName) {
				sortNodesByTime(byName[name])
				g := namedGroup{name: name, nodes: byName[name]}
				if len(g.nodes) > 0 {
					g.latest = g.nodes[0].sortTime
				}
				groups = append(groups, g)
			}
			sort.SliceStable(groups, func(i, j int) bool {
				if groups[i].latest.Equal(groups[j].latest) {
					return groups[i].name < groups[j].name
				}
				return groups[i].latest.After(groups[j].latest)
			})
			shellGroup := &LogNode{Name: "本地终端", Path: "group:shell", IsDir: true, Protocol: proto}
			for _, g := range groups {
				shellGroup.Children = append(shellGroup.Children, &LogNode{
					Name:     g.name,
					Path:     "group:shell:" + g.name,
					IsDir:    true,
					Protocol: proto,
					Children: g.nodes,
				})
			}
			tree = append(tree, shellGroup)
			continue
		}
		sortNodesByTime(nodes)
		tree = append(tree, &LogNode{
			Name:     protoLabel(proto),
			Path:     "group:" + proto,
			IsDir:    true,
			Protocol: proto,
			Children: nodes,
		})
	}

	data, _ := json.Marshal(tree)
	return string(data)
}

// parseMetaTime 解析日志会话开始时间（RFC3339），失败返回零值。
func parseMetaTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// sortNodesByTime 按会话开始时间倒序排序（最新在前），时间相同按名称排序。
func sortNodesByTime(nodes []*LogNode) {
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].sortTime.Equal(nodes[j].sortTime) {
			return nodes[i].Name < nodes[j].Name
		}
		return nodes[i].sortTime.After(nodes[j].sortTime)
	})
}

// shellDisplayName 从日志节点名称中提取终端显示名（节点名为 "<终端名> - <路径> (shell)"）。
func shellDisplayName(nodeName string) string {
	if idx := strings.Index(nodeName, " - "); idx > 0 {
		return nodeName[:idx]
	}
	return nodeName
}

// sortedKeys 返回 map 的键并排序，保证分组顺序稳定。
func sortedKeys(m map[string][]*LogNode) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// GetLogContent 返回指定会话的日志内容。
func (l *LogService) GetLogContent(id string) string {
	if !validLogID(id) {
		return ""
	}
	l.mu.Lock()
	var lines []string
	if ls, ok := l.buffer[id]; ok && len(ls) > 0 {
		lines = ls
		l.buffer[id] = nil
	}
	l.mu.Unlock()
	if len(lines) > 0 {
		l.flushLog(id, lines)
	}

	logPath := filepath.Join(l.logDir, id+".log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		return ""
	}
	return string(data)
}

// GetLogMeta 返回指定会话的元数据 JSON。
func (l *LogService) GetLogMeta(id string) string {
	if !validLogID(id) {
		return "{}"
	}
	metaPath := filepath.Join(l.metaDir, id+".toml")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return "{}"
	}
	var meta LogSessionMeta
	if err := toml.Unmarshal(data, &meta); err != nil {
		return "{}"
	}
	result, _ := json.Marshal(meta)
	return string(result)
}

// GetLogTail 返回指定会话日志末尾最多 maxLines 行（避免大日志一次性读入内存）。
func (l *LogService) GetLogTail(id string, maxLines int) string {
	if !validLogID(id) {
		return ""
	}
	l.mu.Lock()
	var pending []string
	if ls, ok := l.buffer[id]; ok && len(ls) > 0 {
		pending = ls
		l.buffer[id] = nil
	}
	l.mu.Unlock()
	if len(pending) > 0 {
		l.flushLog(id, pending)
	}

	if maxLines <= 0 {
		maxLines = 50000
	}

	logPath := filepath.Join(l.logDir, id+".log")
	tail, err := tailFile(logPath, maxLines)
	if err != nil {
		return ""
	}
	return tail
}

// tailFile 从文件末尾读取最多 maxLines 行，避免加载整个大文件。
func tailFile(path string, maxLines int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", err
	}
	size := fi.Size()
	if size == 0 {
		return "", nil
	}

	const chunk = 64 * 1024
	var chunks [][]byte
	lineCount := 0
	pos := size
	for pos > 0 && lineCount < maxLines+1 {
		readSize := int64(chunk)
		if pos < readSize {
			readSize = pos
		}
		start := pos - readSize
		buf := make([]byte, readSize)
		if _, err := f.ReadAt(buf, start); err != nil {
			return "", err
		}
		chunks = append(chunks, buf)
		for _, b := range buf {
			if b == '\n' {
				lineCount++
			}
		}
		pos = start
	}

	// chunks 逆序拼接即为文件尾部区间。
	var full bytes.Buffer
	for i := len(chunks) - 1; i >= 0; i-- {
		full.Write(chunks[i])
	}
	data := full.Bytes()

	// 若总行数超过上限,丢弃开头的多余行。
	drop := lineCount - maxLines
	if drop > 0 {
		cnt := 0
		idx := 0
		for i := 0; i < len(data); i++ {
			if data[i] == '\n' {
				cnt++
				if cnt == drop {
					idx = i + 1
					break
				}
			}
		}
		data = data[idx:]
	}
	if lineCount >= maxLines+1 && len(data) > 0 && data[0] == '\n' {
		data = data[1:]
	}
	return string(data), nil
}

func protoLabel(p string) string {
	switch p {
	case "ssh":
		return "SSH 连接"
	case "telnet":
		return "Telnet 连接"
	case "serial":
		return "串口 连接"
	case "shell":
		return "本地 Shell"
	default:
		return p
	}
}
