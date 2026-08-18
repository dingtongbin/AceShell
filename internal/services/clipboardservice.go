package services

import (
	"github.com/atotto/clipboard"
)

// ClipboardService 提供系统剪贴板读写能力（终端选中复制、右键粘贴、工具栏按钮共用）。
// 前端 navigator.clipboard 在部分 WebView2 环境受限时,以此作为可靠兜底通道。
type ClipboardService struct{}

// 可被测试替换的实现层。
var (
	clipboardWriteAll = clipboard.WriteAll
	clipboardReadAll  = clipboard.ReadAll
)

// Copy 将文本写入系统剪贴板。成功返回空字符串;失败返回可读错误信息。
func (c *ClipboardService) Copy(text string) string {
	if err := clipboardWriteAll(text); err != nil {
		return "写入剪贴板失败：" + err.Error()
	}
	return ""
}

// Paste 读取系统剪贴板文本。失败返回空字符串。
func (c *ClipboardService) Paste() string {
	text, err := clipboardReadAll()
	if err != nil {
		return ""
	}
	return text
}