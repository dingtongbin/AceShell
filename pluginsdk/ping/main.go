// ping: AceShell 捆绑插件 —— 网络连通性探测工具。
//
// 演示真实插件形态: 侧栏面板发起探测, Go 侧逐包执行系统 ping 并经
// EmitUIEvent 流式推送结果(逐包延迟/丢包/统计), 全部钩子均有实际用途:
//
//	Info/Start/Shutdown  生命周期 + 宿主能力连接
//	OnViewVisible/Hidden 面板显隐日志
//	OnTabEvent           (预留: 结果页可开为标签页)
//	Rpc                  start/stop/status 控制探测
//	HostClient           ListSessions / EmitUIEvent
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
}

var latencyRe = regexp.MustCompile(`[=<]\s*([\d.]+)\s*ms`)

func (p *pingPlugin) Info(ctx context.Context) (*pluginsdk.PluginInfo, error) {
	return &pluginsdk.PluginInfo{
		ID:          "ping",
		DisplayName: "Ping 工具",
		Version:     "0.2.0",
		Icon:        pingIcon,
		AccentColor: "#9aa3ad",
		Views: []pluginsdk.ViewInfo{
			{ID: "main", Title: "Ping", Icon: pingIcon, ComponentID: "panel"},
		},
	}, nil
}

func (p *pingPlugin) Start(ctx context.Context, host *pluginsdk.HostContext) error {
	p.host = host
	if err := os.MkdirAll(host.DataDir, 0700); err != nil {
		return err
	}
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

// Rpc 控制探测会话。结果经 EmitUIEvent 流式推送:
//
//	{type:"result", seq, ok, ms, line}   逐包结果
//	{type:"stopped"}                      用户停止
//	{type:"done", sent, lost, summary}    探测完成
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

// run 探测主循环: 逐包执行系统 ping, 结果即时推送。
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

	var minMs, maxMs, sumMs float64
	var okCount int
	seq := 0
	for {
		select {
		case <-ctx.Done():
			emit(map[string]any{"type": "stopped"})
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
			return
		}
		select {
		case <-ctx.Done():
			emit(map[string]any{"type": "stopped"})
			return
		case <-time.After(time.Duration(args.IntervalMs) * time.Millisecond):
		}
	}
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

func main() {
	pluginsdk.Serve(&pingPlugin{})
}
