package services

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPdfDocRenderSmoke 冒烟: 字体加载 + 中文渲染 + 多页输出。
func TestPdfDocRenderSmoke(t *testing.T) {
	if _, ok := loadCJKFontBytes(); !ok {
		t.Skip("系统中文字体不可用(非 Windows 环境),跳过")
	}
	doc, err := newPdfDoc("AceShell MCP 审计报告", "生成时间 2026-08-22 17:30 · 共 3 条记录")
	if err != nil {
		t.Fatalf("newPdfDoc: %v", err)
	}
	w := doc.pageW - 2*pdfMargin

	// 渲染若干记录块(含长中文文本触发自动换行与分页)
	for i := 0; i < 80; i++ {
		doc.ensureSpace(pdfLineH * 3)
		doc.pdf.SetFontSize(7.5)
		doc.pdf.SetTextColor(pdfColorMuted[0], pdfColorMuted[1], pdfColorMuted[2])
		doc.pdf.CellFormat(22, pdfLineH, "08-22 17:30:0"+strings.Repeat("1", 0+1%10), "", 0, "L", false, 0, "")
		doc.chip("危险", pdfColorRed)
		doc.pdf.SetX(doc.pdf.GetX() + 1.2)
		doc.chip("已拒绝", pdfColorRed)
		doc.pdf.SetFontSize(9)
		doc.pdf.SetTextColor(pdfColorText[0], pdfColorText[1], pdfColorText[2])
		doc.pdf.CellFormat(0, pdfLineH, "terminal_write", "", 1, "L", false, 0, "")
		doc.multiText("/etc/nginx/nginx.conf", w, 8, pdfColorMuted)
		doc.multiText("这是一段用于验证中文自动换行行为的详情文本。"+strings.Repeat("审计详情内容重复填充。", 30), w, 7.5, pdfColorGray)
		doc.drawLine()
	}

	var buf bytes.Buffer
	if err := doc.pdf.Output(&buf); err != nil {
		t.Fatalf("Output: %v", err)
	}
	data := buf.Bytes()
	if len(data) < 1000 {
		t.Fatalf("PDF 输出过小: %d 字节", len(data))
	}
	if !bytes.HasPrefix(data, []byte("%PDF")) {
		t.Fatalf("输出不是合法 PDF 文件头")
	}
	if !bytes.Contains(data, []byte("%%EOF")) {
		t.Fatalf("PDF 未正常结束(缺 %%EOF)")
	}
}

// TestPdfRiskDecisionText 标签映射(中英文)。
func TestPdfRiskDecisionText(t *testing.T) {
	if l, _ := pdfRiskText("blocked", "zh-CN"); l != "危险" {
		t.Errorf("blocked 应映射为 危险, got %q", l)
	}
	if l, _ := pdfRiskText("confirm", "zh-CN"); l != "需确认" {
		t.Errorf("confirm 应映射为 需确认, got %q", l)
	}
	if l, _ := pdfRiskText("-", "zh-CN"); l != "" {
		t.Errorf("- 应映射为空, got %q", l)
	}
	if l, _ := pdfRiskText("blocked", "en-US"); l != "Blocked" {
		t.Errorf("blocked(en) 应映射为 Blocked, got %q", l)
	}
	if l, _ := pdfDecisionText("granted", "zh-CN"); l != "永久授权" {
		t.Errorf("granted 应映射为 永久授权, got %q", l)
	}
	if l, _ := pdfDecisionText("timeout", "zh-CN"); l != "超时" {
		t.Errorf("timeout 应映射为 超时, got %q", l)
	}
	if l, _ := pdfDecisionText("-", "zh-CN"); l != "" {
		t.Errorf("- 应映射为空, got %q", l)
	}
	if l, _ := pdfDecisionText("timeout", "en-US"); l != "Timeout" {
		t.Errorf("timeout(en) 应映射为 Timeout, got %q", l)
	}
	if pdfSourceText("external", "zh-CN") != "外部智能体" || pdfSourceText("embedded", "zh-CN") != "内嵌智能体" {
		t.Errorf("来源映射错误")
	}
	if pdfSourceText("external", "en-US") != "External agent" {
		t.Errorf("来源英文映射错误")
	}
}

// TestPdfShortTime 时间格式化。
func TestPdfShortTime(t *testing.T) {
	got := pdfShortTime("2026-08-22T17:30:33+08:00")
	if got != "08-22 17:30:33" {
		t.Errorf("pdfShortTime = %q", got)
	}
	// 非法输入原样返回
	if got := pdfShortTime("abc"); got != "abc" {
		t.Errorf("非法输入应原样返回, got %q", got)
	}
}

// TestSanitizeFilename 文件名安全化。
func TestSanitizeFilename(t *testing.T) {
	cases := map[string]string{
		"a/b\\c:d": "a-b-c-d",
		"正常标题":     "正常标题",
		"":        "session",
	}
	for in, want := range cases {
		if got := sanitizeFilename(in); got != want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
	if got := sanitizeFilename(strings.Repeat("长", 60)); len([]rune(got)) != 40 {
		t.Errorf("超长标题应截断到 40 字符, got %d", len([]rune(got)))
	}
}

// TestLoadAuditAllEmpty 空目录返回 nil。
func TestLoadAuditAllEmpty(t *testing.T) {
	dir := t.TempDir()
	fake := &McpAuditService{dir: dir}
	s := &McpService{audit: fake}
	if got := s.loadAuditAll(); got != nil {
		t.Errorf("空目录应返回 nil, got %d 条", len(got))
	}
}

// TestLoadAuditAllMultiFile 多文件合并排序。
func TestLoadAuditAllMultiFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "audit-20260821.jsonl"), []byte(
		`{"id":"a-2","ts":"2026-08-21T10:00:00+08:00","action":"b"}`+"\n"+
			`{"id":"a-3","ts":"2026-08-21T11:00:00+08:00","action":"c"}`+"\n"), 0600)
	os.WriteFile(filepath.Join(dir, "audit-20260822.jsonl"), []byte(
		`{"id":"a-1","ts":"2026-08-22T09:00:00+08:00","action":"a"}`+"\n"+
			`无效行`+"\n"), 0600)
	os.WriteFile(filepath.Join(dir, "audit-20260822.jsonl.old"), []byte(
		`{"id":"a-0","ts":"2026-08-22T08:00:00+08:00","action":"z"}`+"\n"), 0600)
	os.WriteFile(filepath.Join(dir, "other.txt"), []byte("x"), 0600)

	fake := &McpAuditService{dir: dir}
	s := &McpService{audit: fake}
	got := s.loadAuditAll()
	if len(got) != 4 {
		t.Fatalf("应解析 4 条(跳过无效行), got %d", len(got))
	}
	// 21日(b,c) 在 22日(z,a) 之前;同日内按时间升序
	wantOrder := []string{"b", "c", "z", "a"}
	for i, e := range got {
		if e.Action != wantOrder[i] {
			t.Errorf("第 %d 条应为 %q, got %q(未按时间排序)", i, wantOrder[i], e.Action)
		}
	}
}
