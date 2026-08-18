package services

import (
	"bytes"
	"fmt"
	"unicode/utf8"
)

// maxEditableSize 可编辑文本文件的大小上限,防止大文件解码卡死界面。
const maxEditableSize = 2 << 20

// validateEditableText 校验待编辑的文本内容:拒绝过大文件、含 NUL 字节或非 UTF-8 编码(二进制)。
func validateEditableText(data []byte) error {
	if len(data) > maxEditableSize {
		return fmt.Errorf("文件过大(超过 %d KB),不支持编辑", maxEditableSize/1024)
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return fmt.Errorf("文件包含二进制数据,不支持编辑")
	}
	if !utf8.Valid(data) {
		return fmt.Errorf("文件不是有效的 UTF-8 文本(可能是二进制文件),不支持编辑")
	}
	return nil
}