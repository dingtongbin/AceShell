// ping: AceShell 捆绑插件 —— 网络连通性探测工具。
//
// 演示真实插件形态: 标签页工作台发起探测, Go 侧逐包执行系统 ping 并经
// EmitUIEvent 流式推送结果(逐包延迟/丢包/统计), 全部钩子均有实际用途:
//
//	Info/Start/Shutdown  生命周期 + 宿主能力连接
//	OnViewVisible/Hidden 面板显隐日志
//	OnTabEvent           标签页生命周期日志
//	Rpc                  start/stop/status + history.*(历史持久化)
//	HostClient           ListSessions / EmitUIEvent / OpenTab
//
// 并发模型(硬约束): 探测是长耗时任务, 只允许跑在子协程里 —— start 的 RPC
// 处理器立即返回, 探测循环 go p.run(...) 独立协程执行; 逐包 exec/超时/文件
// 写盘全部发生在该子协程(或其派生)内, 绝不阻塞插件主循环与 Rpc 处理器。
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	pluginsdk "changeme/pluginsdk"
)

type pingPlugin struct {
	mu     sync.Mutex
	host   *pluginsdk.HostContext
	hostc  *pluginsdk.HostClient
	logf   *pingLogger
	cancel context.CancelFunc // 进行中的探测会话
	target string
	sent   int
	lost   int

	// 历史记录: 持久化于宿主分配的私有数据目录(host.DataDir)/history.json。
	// histMu 与探测计数锁分离, 文件 IO 只发生在探测子协程与短命的 RPC 读路径。
	histMu     sync.Mutex
	histPath   string
	histLoaded bool
	records    []historyRecord
}

var latencyRe = regexp.MustCompile(`[=<]\s*([\d.]+)\s*ms`)

func (p *pingPlugin) Info(ctx context.Context, locale string) (*pluginsdk.PluginInfo, error) {
	// 本地化: 宿主按当前界面语言下发 locale; 不支持的语言回落中文默认文案。
	displayName, viewTitle := "Ping 工具", "Ping"
	if strings.HasPrefix(locale, "en") {
		displayName, viewTitle = "Ping Tool", "Ping"
	}
	return &pluginsdk.PluginInfo{
		ID:          "ping",
		DisplayName: displayName,
		Version:     "0.3.0",
		Icon:        pingIcon,
		AccentColor: "#9aa3ad",
		// 能力声明: 供宿主展示与未来按能力门控
		Capabilities: []string{"net"},
		Views: []pluginsdk.ViewInfo{
			{ID: "main", Title: viewTitle, Icon: pingIcon, ComponentID: "panel"},
		},
	}, nil
}

func (p *pingPlugin) OnLocaleChanged(ctx context.Context, locale string) error {
	// 宿主语言切换通知; 展示名/视图标题由宿主随后的 Info(locale) 重取刷新。
	p.logf.print("界面语言: " + locale)
	return nil
}

func (p *pingPlugin) Start(ctx context.Context, host *pluginsdk.HostContext) error {
	p.host = host
	if err := os.MkdirAll(host.DataDir, 0700); err != nil {
		return err
	}
	p.histPath = filepath.Join(host.DataDir, "history.json")
	p.logf = newPingLogger(filepath.Join(host.DataDir, "ping.log"))
	p.logf.print("Start: 宿主 " + host.HostVersion)
	if hc, err := pluginsdk.DialHost(ctx, host.HostEndpoint, host.Token); err == nil {
		p.hostc = hc
		p.logf.print("HostService 客户端就绪")
	}
	return nil
}

func (p *pingPlugin) Shutdown(ctx context.Context) error {
	p.stop()
	p.logf.print("Shutdown")
	p.logf.close()
	return nil
}

func (p *pingPlugin) OnViewVisible(ctx context.Context, viewID string) error {
	p.logf.print("视图可见: " + viewID)
	return nil
}

func (p *pingPlugin) OnViewHidden(ctx context.Context, viewID string) error {
	p.logf.print("视图隐藏: " + viewID)
	return nil
}

func (p *pingPlugin) OnTabEvent(ctx context.Context, ev *pluginsdk.TabEvent) error {
	p.logf.print("标签页事件: " + ev.Kind + " " + ev.TabKey)
	return nil
}

// rpcArgs Rpc 参数载体。
type rpcArgs struct {
	Host       string `json:"host"`
	Count      int    `json:"count"`      // 0 = 持续探测
	IntervalMs int    `json:"intervalMs"` // 默认 1000
	TimeoutMs  int    `json:"timeoutMs"`  // 默认 2000
}

// Rpc 控制探测会话与历史记录。结果经 EmitUIEvent 流式推送:
//
//	{type:"result", seq, ok, ms, line}   逐包结果
//	{type:"stopped"}                      用户停止
//	{type:"done", sent, lost, summary}    探测完成
//
// 历史方法: history.list(摘要) / history.get(含逐包) / history.open(打开工具标签页) / history.clear。
// 硬约束: 探测永远在子协程执行(start 即返回), 这里绝不跑长任务。
func (p *pingPlugin) Rpc(ctx context.Context, method, argsJSON string) (string, error) {
	switch method {
	case "start":
		var args rpcArgs
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", fmt.Errorf("参数解析失败: %w", err)
		}
		args.Host = strings.TrimSpace(args.Host)
		if args.Host == "" {
			return "", fmt.Errorf("请输入目标主机")
		}
		if err := p.start(args); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil
	case "stop":
		p.stop()
		return `{"ok":true}`, nil
	case "status":
		p.mu.Lock()
		defer p.mu.Unlock()
		return mustJSON(map[string]any{"running": p.cancel != nil, "target": p.target, "sent": p.sent, "lost": p.lost}), nil
	case "history.list":
		return mustJSON(map[string]any{"records": historyListItems(p.loadHistory())}), nil
	case "history.get":
		var args struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", err
		}
		for _, r := range p.loadHistory() {
			if r.ID == args.ID {
				return mustJSON(map[string]any{"record": r}), nil
			}
		}
		return "", fmt.Errorf("记录不存在: %s", args.ID)
	case "history.open":
		// 打开(或定位)Ping 工具标签页; 带 id 时经 PropsJson 传入要展示的记录。
		var args struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal([]byte(argsJSON), &args)
		props := map[string]any{}
		if args.ID != "" {
			found := false
			for _, r := range p.loadHistory() {
				if r.ID == args.ID {
					found = true
					break
				}
			}
			if !found {
				return "", fmt.Errorf("记录不存在: %s", args.ID)
			}
			props["recordId"] = args.ID
		}
		if p.hostc == nil {
			return "", fmt.Errorf("宿主连接未就绪, 无法打开标签页")
		}
		octx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		tabID, err := p.hostc.OpenTab(octx, &pluginsdk.TabSpec{
			TabKey:      "tool",
			Title:       "Ping 工具",
			ComponentID: "tool",
			Props:       props,
			Icon:        pingIcon,
		})
		if err != nil {
			return "", err
		}
		return mustJSON(map[string]any{"tabId": tabID}), nil
	case "history.clear":
		p.saveHistory(nil)
		return `{"ok":true}`, nil
	default:
		return "", fmt.Errorf("未知方法: %s", method)
	}
}

// start 启动探测循环(同刻仅一个会话, 重复 start 先停旧的)。
func (p *pingPlugin) start(args rpcArgs) error {
	if args.IntervalMs <= 0 {
		args.IntervalMs = 1000
	}
	if args.IntervalMs < 100 {
		args.IntervalMs = 100
	}
	if args.TimeoutMs <= 0 {
		args.TimeoutMs = 2000
	}

	p.mu.Lock()
	if p.cancel != nil {
		p.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.target = args.Host
	p.sent, p.lost = 0, 0
	p.mu.Unlock()

	p.logf.print("开始探测: " + args.Host)
	go p.run(ctx, args)
	return nil
}

func (p *pingPlugin) stop() {
	p.mu.Lock()
	cancel := p.cancel
	p.cancel = nil
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// run 探测主循环: 逐包执行系统 ping, 结果即时推送; 会话结束(完成/停止)落一条历史记录。
// 本函数只在 start 派生的子协程里运行, 所有 exec/统计/文件写盘都不碰主循环。
func (p *pingPlugin) run(ctx context.Context, args rpcArgs) {
	emit := func(payload map[string]any) {
		if p.hostc == nil {
			return
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return
		}
		ectx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = p.hostc.EmitUIEvent(ectx, string(data))
	}

	var minMs, maxMs, sumMs, jitterSum float64
	var okCount int
	var lastOk float64
	seq := 0
	startedAt := time.Now().Format(time.RFC3339)
	recID := fmt.Sprintf("r%d", time.Now().UnixNano())
	packets := make([]packetRecord, 0, 64)

	// finalize 会话收尾: 有包才落历史(子协程内执行文件 IO)
	finalize := func(reason string) {
		p.mu.Lock()
		sent, lost := p.sent, p.lost
		p.mu.Unlock()
		if sent == 0 {
			return
		}
		avg, jitter := 0.0, 0.0
		if okCount > 0 {
			avg = sumMs / float64(okCount)
		}
		if okCount > 1 {
			jitter = jitterSum / float64(okCount-1)
		}
		p.appendHistory(historyRecord{
			ID: recID, Target: args.Host, StartedAt: startedAt,
			Count: args.Count, IntervalMs: args.IntervalMs, TimeoutMs: args.TimeoutMs,
			Sent: sent, Lost: lost, MinMs: minMs, MaxMs: maxMs, AvgMs: avg, JitterMs: jitter,
			Packets: packets,
		})
		p.logf.print("记录已保存(" + reason + "): " + args.Host + " " + recID)
	}

	for {
		select {
		case <-ctx.Done():
			emit(map[string]any{"type": "stopped"})
			finalize("stopped")
			return
		default:
		}
		seq++
		ms, ok, line := pingOnce(ctx, args.Host, args.TimeoutMs)
		p.mu.Lock()
		p.sent++
		if !ok {
			p.lost++
		}
		sent, lost := p.sent, p.lost
		p.mu.Unlock()
		if ok {
			okCount++
			if minMs == 0 || ms < minMs {
				minMs = ms
			}
			if ms > maxMs {
				maxMs = ms
			}
			sumMs += ms
			if lastOk > 0 {
				jitterSum += absF(ms - lastOk)
			}
			lastOk = ms
		}
		packets = append(packets, packetRecord{Seq: seq, OK: ok, MS: ms, Line: line})
		if len(packets) > historyMaxPackets {
			packets = packets[len(packets)-historyMaxPackets:]
		}
		emit(map[string]any{"type": "result", "seq": seq, "ok": ok, "ms": ms, "line": line})
		if args.Count > 0 && seq >= args.Count {
			summary := ""
			if okCount > 0 {
				summary = fmt.Sprintf("已发送 %d, 丢失 %d (%.0f%% 丢包), 最快 %.1fms, 最慢 %.1fms, 平均 %.1fms",
					sent, lost, float64(lost)*100/float64(sent), minMs, maxMs, sumMs/float64(okCount))
			} else {
				summary = fmt.Sprintf("已发送 %d, 全部丢失", sent)
			}
			emit(map[string]any{"type": "done", "sent": sent, "lost": lost, "summary": summary})
			p.mu.Lock()
			p.cancel = nil
			p.mu.Unlock()
			p.logf.print("探测完成: " + args.Host + " " + summary)
			finalize("done")
			return
		}
		select {
		case <-ctx.Done():
			emit(map[string]any{"type": "stopped"})
			finalize("stopped")
			return
		case <-time.After(time.Duration(args.IntervalMs) * time.Millisecond):
		}
	}
}

func absF(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// pingOnce 执行单次系统 ping 并解析延迟(跨平台命令; 解析仅取 ASCII 数字, 免编码问题)。
func pingOnce(ctx context.Context, host string, timeoutMs int) (ms float64, ok bool, line string) {
	var cmd *exec.Cmd
	perPing := time.Duration(timeoutMs+1500) * time.Millisecond
	cctx, cancel := context.WithTimeout(ctx, perPing)
	defer cancel()
	switch runtime.GOOS {
	case "windows":
		cmd = exec.CommandContext(cctx, "ping", "-n", "1", "-w", strconv.Itoa(timeoutMs), host)
		pluginsdk.HideWindow(cmd)
	case "darwin":
		cmd = exec.CommandContext(cctx, "ping", "-c", "1", "-W", strconv.Itoa(timeoutMs), host)
	default:
		cmd = exec.CommandContext(cctx, "ping", "-c", "1", "-W", strconv.Itoa(timeoutMs/1000), host)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	_ = cmd.Run()

	sc := bufio.NewScanner(bytes.NewReader(out.Bytes()))
	sc.Buffer(make([]byte, 64*1024), 64*1024)
	replyLine := ""
	for sc.Scan() {
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		replyLine = text
		if m := latencyRe.FindStringSubmatch(text); m != nil {
			if v, err := strconv.ParseFloat(m[1], 64); err == nil {
				return v, true, text
			}
		}
	}
	if ctx.Err() != nil {
		return 0, false, "已停止"
	}
	if replyLine == "" {
		return 0, false, "无响应(超时)"
	}
	// 回复行无延迟字段: 不可达/超时等失败情形
	return 0, false, replyLine
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// ==================== 历史记录(持久化于宿主分配的私有数据目录) ====================

type packetRecord struct {
	Seq  int     `json:"seq"`
	OK   bool    `json:"ok"`
	MS   float64 `json:"ms"`
	Line string  `json:"line"`
}

type historyRecord struct {
	ID         string         `json:"id"`
	Target     string         `json:"target"`
	StartedAt  string         `json:"startedAt"`
	Count      int            `json:"count"`
	IntervalMs int            `json:"intervalMs"`
	TimeoutMs  int            `json:"timeoutMs"`
	Sent       int            `json:"sent"`
	Lost       int            `json:"lost"`
	MinMs      float64        `json:"minMs"`
	MaxMs      float64        `json:"maxMs"`
	AvgMs      float64        `json:"avgMs"`
	JitterMs   float64        `json:"jitterMs"`
	Packets    []packetRecord `json:"packets,omitempty"`
}

const (
	historyMaxRecords = 50  // FIFO 上限
	historyMaxPackets = 600 // 单条记录保留的逐包上限(滑窗保留最近)
)

// loadHistory 读取历史(懒加载 + 容错: 文件缺失/损坏视为空, 不影响探测)。
func (p *pingPlugin) loadHistory() []historyRecord {
	p.histMu.Lock()
	defer p.histMu.Unlock()
	return p.loadHistoryLocked()
}

func (p *pingPlugin) loadHistoryLocked() []historyRecord {
	if p.histLoaded {
		return p.records
	}
	p.histLoaded = true
	p.records = []historyRecord{}
	raw, err := os.ReadFile(p.histPath)
	if err != nil {
		return p.records
	}
	var wrapper struct {
		Records []historyRecord `json:"records"`
	}
	if json.Unmarshal(raw, &wrapper) != nil {
		return p.records
	}
	if wrapper.Records != nil {
		p.records = wrapper.Records
	}
	return p.records
}

// saveHistory 全量落盘(临时文件 + 改名, 避免半截文件)。
func (p *pingPlugin) saveHistory(recs []historyRecord) {
	p.histMu.Lock()
	defer p.histMu.Unlock()
	p.records = recs
	p.histLoaded = true
	data, err := json.MarshalIndent(struct {
		Records []historyRecord `json:"records"`
	}{recs}, "", "  ")
	if err != nil {
		return
	}
	tmp := p.histPath + ".tmp"
	if os.WriteFile(tmp, data, 0644) != nil {
		return
	}
	_ = os.Rename(tmp, p.histPath)
}

// appendHistory 新记录置顶(FIFO 上限内)。仅在探测子协程调用。
func (p *pingPlugin) appendHistory(rec historyRecord) {
	p.histMu.Lock()
	recs := p.loadHistoryLocked()
	recs = append([]historyRecord{rec}, recs...)
	if len(recs) > historyMaxRecords {
		recs = recs[:historyMaxRecords]
	}
	p.histMu.Unlock()
	p.saveHistory(recs)
}

// historyListItems 列表视图: 不带逐包数组(体积可控), 逐包走 history.get。
func historyListItems(recs []historyRecord) []map[string]any {
	items := make([]map[string]any, 0, len(recs))
	for _, r := range recs {
		items = append(items, map[string]any{
			"id": r.ID, "target": r.Target, "startedAt": r.StartedAt,
			"count": r.Count, "intervalMs": r.IntervalMs, "timeoutMs": r.TimeoutMs,
			"sent": r.Sent, "lost": r.Lost,
			"minMs": r.MinMs, "maxMs": r.MaxMs, "avgMs": r.AvgMs, "jitterMs": r.JitterMs,
		})
	}
	return items
}

func main() {
	pluginsdk.Serve(&pingPlugin{})
}
