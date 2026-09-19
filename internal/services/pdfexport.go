package services

// MCP 审计记录的 PDF 导出(调用方式均为同步绑定方法,
// 由前端按钮触发;保存对话框取消不视为错误)。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	pdfAuditMaxEntries = 10000 // 审计导出条数上限(防极端量卡死)
)

// pdfResultJSON 统一返回 {"ok":..,"path":..,"error":..}。
func pdfResultJSON(path string, err error) string {
	if err != nil {
		data, _ := json.Marshal(map[string]any{"ok": false, "error": err.Error()})
		return string(data)
	}
	data, _ := json.Marshal(map[string]any{"ok": true, "path": path})
	return string(data)
}

// pdfT PDF 文案双语选择(默认中文)。
func pdfT(lang, zh, en string) string {
	if lang == "en-US" {
		return en
	}
	return zh
}

// pdfRiskText 风险等级标签与颜色(按语言)。
func pdfRiskText(risk, lang string) (string, [3]int) {
	switch risk {
	case "blocked":
		return pdfT(lang, "危险", "Blocked"), pdfColorRed
	case "confirm":
		return pdfT(lang, "需确认", "Confirm"), pdfColorOrange
	case "auto":
		return pdfT(lang, "自动", "Auto"), pdfColorGreen
	default:
		return "", pdfColorGray
	}
}

// pdfDecisionText 决策标签与颜色(按语言)。
func pdfDecisionText(decision, lang string) (string, [3]int) {
	switch decision {
	case "executed", "approved", "granted":
		if decision == "granted" {
			return pdfT(lang, "永久授权", "Granted"), pdfColorGreen
		}
		return pdfT(lang, "已执行", "Executed"), pdfColorGreen
	case "rejected", "denied":
		return pdfT(lang, "已拒绝", "Rejected"), pdfColorRed
	case "timeout":
		return pdfT(lang, "超时", "Timeout"), pdfColorOrange
	case "preempted":
		return pdfT(lang, "被抢占", "Preempted"), pdfColorAccent
	case "-":
		return "", pdfColorGray
	default:
		return decision, pdfColorGray
	}
}

// pdfSourceText 来源标签(按语言)。
func pdfSourceText(source, lang string) string {
	switch source {
	case "external":
		return pdfT(lang, "外部智能体", "External agent")
	case "embedded":
		return pdfT(lang, "内嵌智能体", "Embedded agent")
	case "system":
		return pdfT(lang, "系统", "System")
	default:
		return source
	}
}

// pdfShortTime RFC3339 → "MM-DD HH:MM:SS"。
func pdfShortTime(ts string) string {
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		return t.Format("01-02 15:04:05")
	}
	return ts
}

// ExportAuditPdf 导出全部 MCP 审计记录为 PDF(磁盘保留期内全量,上限 pdfAuditMaxEntries)。
func (s *McpService) ExportAuditPdf(lang string) string {
	entries := s.loadAuditAll()
	if len(entries) == 0 {
		return pdfResultJSON("", errPDFEmpty(pdfT(lang, "审计记录", "audit records")))
	}
	if len(entries) > pdfAuditMaxEntries {
		entries = entries[len(entries)-pdfAuditMaxEntries:]
	}

	now := time.Now().Format("2006-01-02 15:04")
	subtitle := pdfT(lang, "生成时间 ", "Generated ") + now + " · " + pdfT(lang, "共 ", "") + itoa(len(entries)) + pdfT(lang, " 条记录", " records")
	if len(entries) == pdfAuditMaxEntries {
		subtitle += "(" + pdfT(lang, "仅最近 ", "last ") + itoa(pdfAuditMaxEntries) + pdfT(lang, " 条", "") + ")"
	}
	doc, err := newPdfDoc(pdfT(lang, "AceShell MCP 审计报告", "AceShell MCP Audit Report"), subtitle)
	if err != nil {
		return pdfResultJSON("", err)
	}
	w := doc.pageW - 2*pdfMargin

	for i, e := range entries {
		// 预估块高: 首行 + 对象行 + 详情行
		headLines := 1
		if e.Subject != "" {
			headLines++
		}
		detailLines := 0
		if e.Detail != "" {
			detailLines = doc.textLines(e.Detail, w)
		}
		doc.ensureSpace(float64(headLines+detailLines)*pdfLineH + 3)
		if doc.err != nil {
			break
		}

		// 行1: 时间 + 标签组 + 动作
		doc.pdf.SetFontSize(7.5)
		doc.pdf.SetTextColor(pdfColorMuted[0], pdfColorMuted[1], pdfColorMuted[2])
		doc.pdf.CellFormat(22, pdfLineH, pdfShortTime(e.TS), "", 0, "L", false, 0, "")
		doc.pdf.SetFontSize(7)
		if label, color := pdfRiskText(e.Risk, lang); label != "" {
			doc.chip(label, color)
			doc.pdf.SetX(doc.pdf.GetX() + 1.2)
		}
		if label, color := pdfDecisionText(e.Decision, lang); label != "" {
			doc.chip(label, color)
			doc.pdf.SetX(doc.pdf.GetX() + 1.2)
		}
		doc.pdf.SetFontSize(7.5)
		doc.pdf.SetTextColor(pdfColorGray[0], pdfColorGray[1], pdfColorGray[2])
		srcText := pdfSourceText(e.Source, lang)
		doc.pdf.CellFormat(doc.pdf.GetStringWidth(srcText)+2, pdfLineH, srcText, "", 0, "L", false, 0, "")
		if e.BatchID != "" {
			doc.pdf.SetTextColor(pdfColorAccent[0], pdfColorAccent[1], pdfColorAccent[2])
			doc.pdf.CellFormat(14, pdfLineH, "["+pdfT(lang, "批量", "batch")+"]", "", 0, "L", false, 0, "")
		}
		doc.pdf.SetFontSize(9)
		doc.pdf.SetTextColor(pdfColorText[0], pdfColorText[1], pdfColorText[2])
		doc.pdf.CellFormat(0, pdfLineH, e.Action, "", 1, "L", false, 0, "")
		if doc.pdf.Err() {
			doc.err = fmt.Errorf("PDF 生成失败")
			break
		}

		// 行2: 对象
		if e.Subject != "" {
			doc.multiText(e.Subject, w, 8, pdfColorMuted)
		}
		// 行3: 详情
		if e.Detail != "" {
			doc.multiText(e.Detail, w, 7.5, pdfColorGray)
		}
		if i < len(entries)-1 {
			doc.setY(doc.y() + 1.2)
			doc.drawLine()
		}
	}

	path, saved, err := doc.saveViaDialog(s.app, "aceshell-audit-"+time.Now().Format("20060102")+".pdf")
	if err != nil {
		return pdfResultJSON("", err)
	}
	if !saved {
		return pdfResultJSON("", nil) // 用户取消,ok=true path=""
	}
	return pdfResultJSON(path, nil)
}

// loadAuditAll 加载磁盘保留期内全部审计记录(内存缓冲是磁盘子集,无需合并)。
func (s *McpService) loadAuditAll() []McpAuditEntry {
	dir := s.audit.dir
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	// audit-YYYYMMDD.jsonl 及其 .old 轮转文件,按文件名升序即按日期升序
	var files []string
	for _, it := range items {
		name := it.Name()
		if it.IsDir() || !strings.HasPrefix(name, "audit-") {
			continue
		}
		if strings.HasSuffix(name, ".jsonl") || strings.HasSuffix(name, ".jsonl.old") {
			files = append(files, filepath.Join(dir, name))
		}
	}
	sort.Strings(files)

	var all []McpAuditEntry
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var e McpAuditEntry
			if json.Unmarshal([]byte(line), &e) == nil && e.TS != "" {
				all = append(all, e)
			}
		}
	}
	// 稳定排序修正跨文件时间顺序(同秒保持文件内顺序)
	sort.SliceStable(all, func(i, j int) bool { return all[i].TS < all[j].TS })
	return all
}

// errPDFEmpty 无可导出内容错误(双语)。
func errPDFEmpty(what string) error {
	return &pdfEmptyError{what: what}
}

type pdfEmptyError struct{ what string }

func (e *pdfEmptyError) Error() string {
	if strings.Contains(e.what, " ") { // 英文文案已完整
		return "Nothing to export: " + e.what
	}
	return "没有可导出的" + e.what
}

// itoa 整数转字符串(避免 strconv import 别名冲突)。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
