package services

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// MCP 挂起期间用户活动记录:
// 用户抢占/手动挂起 MCP 后,通常会在终端手动执行命令。这些操作发生在
// AI 视野之外,智能体恢复对话时无从知晓,导致后续建议基于过时的终端状态。
// 记录器在挂起期间经 TapOutput 持续收集各标签页新增输出(即用户看到的屏幕
// 内容),由 AgentService 在下一轮对话构建上下文时取走注入(drain,读后即清)。

const (
	suspendTabMax   = 32 * 1024  // 单标签页捕获上限(超出滑动丢弃最旧)
	suspendTotalMax = 128 * 1024 // 总捕获上限(超出丢弃新增并置全局截断标记)
)

// mcpSuspendActivity 单个标签页的挂起期活动。
type mcpSuspendActivity struct {
	TabID     string `json:"tabId"`
	Content   string `json:"content"`   // 剥离控制序列后的屏幕新增输出(截断至单标签页上限)
	Truncated bool   `json:"truncated"` // 该标签页内容因超限被裁剪
}

// mcpSuspendReport 挂起期活动报告(drain 输出)。
type mcpSuspendReport struct {
	Since     time.Time            `json:"since"`
	GlobalTruncated bool           `json:"globalTruncated"`
	Tabs      []mcpSuspendActivity `json:"tabs"`
}

// beginSuspendCapture 开始捕获挂起期活动(幂等:已在捕获中不重置已收集内容)。
func (s *McpService) beginSuspendCapture() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.suspendCapturing {
		return
	}
	s.suspendCapturing = true
	s.suspendBuf = map[string][]byte{}
	s.suspendTabCut = map[string]bool{}
	s.suspendGlobalCut = false
	s.suspendStartedAt = time.Now()
}

// endSuspendCapture 结束捕获;数据保留,等待智能体 drain。
func (s *McpService) endSuspendCapture(markUnread bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.suspendCapturing {
		return
	}
	s.suspendCapturing = false
	if markUnread && len(s.suspendBuf) > 0 {
		s.suspendUnread = true
	}
}

// appendSuspendLocked 追加挂起期增量输出(调用方持 s.mu)。有界:单页滑动窗口 + 全局总量上限。
func (s *McpService) appendSuspendLocked(tabID string, data []byte) {
	if len(data) <= 0 {
		return
	}
	total := 0
	for _, b := range s.suspendBuf {
		total += len(b)
	}
	if total+len(data) > suspendTotalMax {
		s.suspendGlobalCut = true
		return
	}
	combined := append(s.suspendBuf[tabID], data...)
	if cut := len(combined) - suspendTabMax; cut > 0 {
		combined = combined[cut:]
		s.suspendTabCut[tabID] = true
	}
	s.suspendBuf[tabID] = combined
}

// DrainMcpSuspendActivity 取走挂起期间的标签页活动报告(JSON;无未读记录返回空串),读后即清。
// AgentService 在下一轮对话构建上下文时调用,将用户中断期间的手动操作告知模型。
func (s *McpService) DrainMcpSuspendActivity() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.suspendUnread {
		return ""
	}
	report := mcpSuspendReport{Since: s.suspendStartedAt, GlobalTruncated: s.suspendGlobalCut, Tabs: []mcpSuspendActivity{}}
	for tabID, raw := range s.suspendBuf {
		content := strings.TrimSpace(stripAnsi(string(raw)))
		if content == "" {
			continue
		}
		report.Tabs = append(report.Tabs, mcpSuspendActivity{
			TabID: tabID, Content: content, Truncated: s.suspendTabCut[tabID],
		})
	}
	s.suspendUnread = false
	s.suspendBuf = nil
	s.suspendTabCut = nil
	s.suspendGlobalCut = false

	data, err := json.Marshal(report)
	if err != nil {
		return ""
	}
	return string(data)
}

// formatSuspendPrompt 将挂起期活动报告格式化为可注入对话的 system 提示。
func formatSuspendPrompt(reportJSON string) string {
	var report mcpSuspendReport
	if json.Unmarshal([]byte(reportJSON), &report) != nil || len(report.Tabs) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("【用户中断期间的操作】你上一次的工具执行已被用户打断(MCP 挂起)。")
	sb.WriteString(fmt.Sprintf("自 %s 起的挂起期间,用户在以下终端手动进行了操作,其屏幕新增输出如下;", report.Since.Format("15:04:05")))
	sb.WriteString("请在后续分析与建议中考虑这些最新变化,不要依赖中断前的过时状态。")
	for _, tab := range report.Tabs {
		content := truncateUtf8(tab.Content, 4000)
		if tab.Truncated {
			content += "\n...(更早内容已截断)"
		}
		sb.WriteString("\n--- 标签页 " + tab.TabID + " ---\n" + content)
	}
	if report.GlobalTruncated {
		sb.WriteString("\n(注:部分终端输出因容量限制未收录)")
	}
	return sb.String()
}
