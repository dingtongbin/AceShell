package services

// PDF 导出公共构建器:A4 竖版 + 系统中文字体(运行时加载,无字体分发成本)。
// 字体回退链均为单 TTF 文件(fpdf 不支持 TTC 容器):
// SimHei(黑体) → Deng(等线) → Dengb → 仿宋 → 楷体。

import (
	"bytes"
	"fmt"
	"os"
	"sync"

	"github.com/go-pdf/fpdf"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	pdfFontFamily = "cjk"
	pdfMargin     = 14.0 // 页边距 mm
	pdfLineH      = 4.6  // 正文行高 mm
)

// pdf 配色(与前端审计面板一致)。
var (
	pdfColorAccent = [3]int{0, 120, 212}   // 蓝(标题栏)
	pdfColorText   = [3]int{60, 60, 60}    // 正文深灰
	pdfColorMuted  = [3]int{128, 128, 128} // 次要灰
	pdfColorLine   = [3]int{225, 225, 225} // 分隔线
	pdfColorRed    = [3]int{228, 88, 88}   // 危险/拒绝
	pdfColorOrange = [3]int{226, 160, 63}  // 需确认/超时
	pdfColorGreen  = [3]int{78, 201, 176}  // 自动/成功
	pdfColorGray   = [3]int{110, 110, 110} // 中性
)

var (
	pdfFontOnce  sync.Once
	pdfFontBytes []byte
	pdfFontOK    bool
)

// loadCJKFontBytes 加载系统中文字体(仅首次;全部失败返回 false)。
func loadCJKFontBytes() ([]byte, bool) {
	pdfFontOnce.Do(func() {
		windir := os.Getenv("WINDIR")
		if windir == "" {
			windir = `C:\Windows`
		}
		candidates := []string{
			windir + `\Fonts\simhei.ttf`,  // 黑体(Win 通用)
			windir + `\Fonts\Deng.ttf`,    // 等线(Win10+)
			windir + `\Fonts\Dengb.ttf`,   // 等线 Bold
			windir + `\Fonts\simfang.ttf`, // 仿宋
			windir + `\Fonts\simkai.ttf`,  // 楷体
		}
		for _, p := range candidates {
			if data, err := os.ReadFile(p); err == nil && len(data) > 10000 {
				pdfFontBytes, pdfFontOK = data, true
				return
			}
		}
	})
	return pdfFontBytes, pdfFontOK
}

// pdfDoc PDF 文档构建器(手动分页,保证内容块完整性)。
type pdfDoc struct {
	pdf     *fpdf.Fpdf
	pageW   float64
	pageH   float64
	bottomY float64
	err     error
}

// newPdfDoc 创建文档并渲染首页标题栏。title/subtitle 为首页头部内容。
func newPdfDoc(title, subtitle string) (*pdfDoc, error) {
	fontBytes, ok := loadCJKFontBytes()
	if !ok {
		return nil, fmt.Errorf("未找到系统中文字体(黑体/等线),无法导出 PDF")
	}
	p := fpdf.New("P", "mm", "A4", "")
	p.SetMargins(pdfMargin, pdfMargin, pdfMargin)
	p.SetAutoPageBreak(false, 0)
	p.AddUTF8FontFromBytes(pdfFontFamily, "", fontBytes)
	p.SetFont(pdfFontFamily, "", 9)
	p.AddPage()

	w, h := p.GetPageSize()
	d := &pdfDoc{pdf: p, pageW: w, pageH: h, bottomY: h - pdfMargin}

	// 标题栏:深蓝底 + 白字标题 + 副标题
	p.SetFillColor(pdfColorAccent[0], pdfColorAccent[1], pdfColorAccent[2])
	p.Rect(0, 0, w, 16, "F")
	p.SetTextColor(255, 255, 255)
	p.SetFontSize(13)
	p.SetXY(pdfMargin, 4.2)
	p.CellFormat(w-2*pdfMargin, 7, title, "", 0, "L", false, 0, "")
	if subtitle != "" {
		p.SetFontSize(7.5)
		p.SetXY(pdfMargin, 11)
		p.CellFormat(w-2*pdfMargin, 4, subtitle, "", 0, "L", false, 0, "")
	}
	p.SetY(22)
	p.SetTextColor(pdfColorText[0], pdfColorText[1], pdfColorText[2])
	p.SetFontSize(9)
	return d, nil
}

// y 当前绘制位置。
func (d *pdfDoc) y() float64 { return d.pdf.GetY() }

// setY 设置绘制位置。
func (d *pdfDoc) setY(y float64) { d.pdf.SetY(y) }

// ensureSpace 确保剩余空间;不足则分页(新页仅留顶边距)。
func (d *pdfDoc) ensureSpace(h float64) {
	if d.err != nil {
		return
	}
	if d.y()+h > d.bottomY {
		d.pdf.AddPage()
		d.pdf.SetY(pdfMargin)
	}
}

// newPage 强制分页。
func (d *pdfDoc) newPage() {
	if d.err != nil {
		return
	}
	d.pdf.AddPage()
	d.pdf.SetY(pdfMargin)
}

// textLines 计算文本按宽度换行后的行数(用于分页预判)。
func (d *pdfDoc) textLines(txt string, w float64) int {
	lines := d.pdf.SplitText(txt, w)
	if len(lines) == 0 {
		return 1
	}
	return len(lines)
}

// multiText 渲染自动换行文本块(正文色)。
func (d *pdfDoc) multiText(txt string, w float64, size float64, color [3]int) {
	if d.err != nil || txt == "" {
		return
	}
	d.pdf.SetFontSize(size)
	d.pdf.SetTextColor(color[0], color[1], color[2])
	n := d.textLines(txt, w)
	d.ensureSpace(float64(n) * pdfLineH)
	if d.err != nil {
		return
	}
	d.pdf.MultiCell(w, pdfLineH, txt, "", "L", false)
}

// chip 渲染色块小标签(风险/决策/来源),返回标签右端 x。
func (d *pdfDoc) chip(label string, color [3]int) {
	if d.err != nil || label == "" {
		return
	}
	w := d.pdf.GetStringWidth(label) + 3.5
	d.pdf.SetFontSize(7)
	d.pdf.SetFillColor(color[0], color[1], color[2])
	d.pdf.SetTextColor(255, 255, 255)
	d.pdf.CellFormat(w, 3.8, label, "", 0, "L", true, 0, "")
}

// drawLine 渲染浅色分隔线。
func (d *pdfDoc) drawLine() {
	if d.err != nil {
		return
	}
	d.ensureSpace(1.5)
	d.pdf.SetDrawColor(pdfColorLine[0], pdfColorLine[1], pdfColorLine[2])
	d.pdf.SetLineWidth(0.15)
	d.pdf.Line(pdfMargin, d.y(), d.pageW-pdfMargin, d.y())
	d.pdf.SetY(d.y() + 1.5)
}

// saveViaDialog 弹出保存对话框并写入文件。返回 (路径, 是否已保存, 错误)。
func (d *pdfDoc) saveViaDialog(app *application.App, defaultName string) (string, bool, error) {
	if d.err != nil {
		return "", false, d.err
	}
	if d.pdf.Err() {
		return "", false, fmt.Errorf("PDF 生成失败")
	}
	var buf bytes.Buffer
	if err := d.pdf.Output(&buf); err != nil {
		return "", false, fmt.Errorf("PDF 编码失败: %w", err)
	}
	if app == nil {
		return "", false, fmt.Errorf("服务未初始化")
	}
	dialog := app.Dialog.SaveFile()
	dialog.SetFilename(defaultName)
	dialog.AddFilter("PDF 文档", "*.pdf")
	dialog.AddFilter("所有文件", "*.*")
	path, err := dialog.PromptForSingleSelection()
	if err != nil || path == "" {
		return "", false, nil // 用户取消
	}
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		return "", false, fmt.Errorf("写入文件失败: %w", err)
	}
	return path, true, nil
}
