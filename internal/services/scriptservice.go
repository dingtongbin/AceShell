package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ScriptFileService 管理脚本文件的 CRUD 操作。
type ScriptFileService struct{}

// safeScriptPath 验证路径不会逃逸出 rootDir 目录。
func safeScriptPath(rootDir, subPath string) (string, error) {
	absRoot, _ := filepath.Abs(rootDir)
	full := filepath.Join(rootDir, filepath.Clean(subPath))
	absFull, err := filepath.Abs(full)
	if err != nil {
		return "", fmt.Errorf("无效的路径")
	}
	if !strings.HasPrefix(absFull, absRoot+string(filepath.Separator)) && absFull != absRoot {
		return "", fmt.Errorf("不允许的路径遍历操作")
	}
	return full, nil
}

// ScriptFile 表示一个脚本文件的元信息。
type ScriptFile struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
}

// ReadFile 读取指定脚本文件内容;非 UTF-8 文本或过大文件拒绝读取。
func (s *ScriptFileService) ReadFile(filePath string) (string, error) {
	fullPath, err := safeScriptPath(ScriptsDir(), filePath)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", err
	}
	if err := validateEditableText(data); err != nil {
		return "", err
	}
	return string(data), nil
}

// WriteFile 写入内容到指定脚本文件，自动创建目录。
func (s *ScriptFileService) WriteFile(filePath string, content string) error {
	fullPath, err := safeScriptPath(ScriptsDir(), filePath)
	if err != nil {
		return err
	}
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(fullPath, []byte(content), 0644)
}

// ListFiles 列出指定目录下的脚本文件，返回 JSON 数组。
func (s *ScriptFileService) ListFiles(dirPath string) string {
	fullPath, err := safeScriptPath(ScriptsDir(), dirPath)
	if err != nil {
		return "[]"
	}
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return "[]"
	}

	var files []ScriptFile
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, ScriptFile{
			Name:     entry.Name(),
			Path:     filepath.Join(dirPath, entry.Name()),
			Size:     info.Size(),
			Modified: info.ModTime().Format("2006-01-02 15:04:05"),
		})
	}

	data, _ := json.Marshal(files)
	return string(data)
}

// CreateFile 创建空脚本文件，自动创建目录。
func (s *ScriptFileService) CreateFile(filePath string) error {
	fullPath, err := safeScriptPath(ScriptsDir(), filePath)
	if err != nil {
		return err
	}
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(fullPath, []byte{}, 0644)
}

// DeleteFile 删除指定脚本文件或目录。
func (s *ScriptFileService) DeleteFile(filePath string) error {
	fullPath, err := safeScriptPath(ScriptsDir(), filePath)
	if err != nil {
		return err
	}
	return os.RemoveAll(fullPath)
}

// RenameFile 重命名脚本文件。
func (s *ScriptFileService) RenameFile(oldPath, newName string) error {
	fullOld, err := safeScriptPath(ScriptsDir(), oldPath)
	if err != nil {
		return err
	}
	dir := filepath.Dir(fullOld)
	fullNew := filepath.Join(dir, newName)
	absRoot, _ := filepath.Abs(ScriptsDir())
	absNew, _ := filepath.Abs(fullNew)
	if !strings.HasPrefix(absNew, absRoot+string(filepath.Separator)) && absNew != absRoot {
		return fmt.Errorf("不允许的路径遍历操作")
	}
	return os.Rename(fullOld, fullNew)
}

// CreateDir 创建脚本目录。
func (s *ScriptFileService) CreateDir(dirPath string) error {
	fullPath, err := safeScriptPath(ScriptsDir(), dirPath)
	if err != nil {
		return err
	}
	return os.MkdirAll(fullPath, 0755)
}

// GetLanguage 根据文件扩展名返回语法高亮语言标识。
func (s *ScriptFileService) GetLanguage(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	case ".ts", ".tsx":
		return "javascript"
	case ".py", ".pyw":
		return "python"
	case ".html", ".htm":
		return "html"
	case ".css", ".scss", ".less":
		return "css"
	case ".json":
		return "json"
	case ".md", ".markdown":
		return "markdown"
	case ".sh", ".bash":
		return "shell"
	case ".go":
		return "go"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".cxx":
		return "cpp"
	default:
		return "text"
	}
}
