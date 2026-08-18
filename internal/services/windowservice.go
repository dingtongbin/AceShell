package services

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type WindowService struct {
	app *application.App
}

func (w *WindowService) SetApp(app *application.App) {
	w.app = app
}

func (w *WindowService) emit(event string, data string) {
	if w.app != nil {
		w.app.Event.Emit(event, data)
	}
}

// SetDarkMode 切换前端为暗色主题。
func (w *WindowService) SetDarkMode() {
	w.emit("theme-changed", "dark")
}

// SetLightMode 切换前端为亮色主题。
func (w *WindowService) SetLightMode() {
	w.emit("theme-changed", "light")
}

// IsAdmin 检查当前进程是否以管理员权限运行。
func (w *WindowService) IsAdmin() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	cmd := exec.Command("net", "session")
	hideWindow(cmd)
	err := cmd.Run()
	return err == nil
}

// MoveToRecycleBin 将文件移到回收站（Windows）或直接删除（其他系统）。
func (w *WindowService) MoveToRecycleBin(filePath string) error {
	if runtime.GOOS == "windows" {
		// 使用参数数组传递命令，PowerShell 单引号字符串中只需转义单引号
		psScript := fmt.Sprintf(
			"$shell = New-Object -ComObject Shell.Application; $shell.Namespace(0xA).MoveHere('%s')",
			strings.ReplaceAll(filePath, "'", "''"),
		)
		cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psScript)
		hideWindow(cmd)
		err := cmd.Run()
		if err == nil {
			return nil
		}
	}
	return os.Remove(filePath)
}

type dirFileEntry struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	IsDir   bool   `json:"isDir"`
	ModTime string `json:"modTime"`
}

// ListDirFiles 列出指定目录中的文件，返回 JSON。
func (w *WindowService) ListDirFiles(dir string) string {
	if dir == "" {
		return `{"error":"目录路径为空"}`
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}
	var files []dirFileEntry
	for _, entry := range entries {
		info, _ := entry.Info()
		files = append(files, dirFileEntry{
			Name:    entry.Name(),
			Size:    info.Size(),
			IsDir:   entry.IsDir(),
			ModTime: info.ModTime().Format(time.DateTime),
		})
	}
	if files == nil {
		files = []dirFileEntry{}
	}
	data, _ := json.Marshal(map[string]interface{}{"files": files, "dir": dir})
	return string(data)
}

// CopyFileToDir 将源文件复制到目标目录，返回目标文件路径。
func (w *WindowService) CopyFileToDir(srcPath, destDir string) string {
	os.MkdirAll(destDir, 0755)
	destPath := filepath.Join(destDir, filepath.Base(srcPath))

	srcFile, err := os.Open(srcPath)
	if err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}
	defer srcFile.Close()

	destFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, srcFile); err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}

	data, _ := json.Marshal(map[string]interface{}{"path": destPath, "name": filepath.Base(destPath)})
	return string(data)
}

// OpenFolderDialog 打开系统文件夹选择对话框，返回所选路径。
func (w *WindowService) OpenFolderDialog() string {
	dialog := w.app.Dialog.OpenFile()
	dialog.CanChooseDirectories(true)
	dialog.CanChooseFiles(false)
	dialog.SetTitle("选择 FTP 挂载目录")
	result, err := dialog.PromptForSingleSelection()
	if err != nil {
		return ""
	}
	return result
}

// OpenFileDialog 打开文件选择对话框，返回所选文件路径。
func (w *WindowService) OpenFileDialog(title string, filterName string, filterPattern string) string {
	if w.app == nil {
		return ""
	}
	dialog := w.app.Dialog.OpenFile()
	dialog.CanChooseFiles(true)
	dialog.CanChooseDirectories(false)
	if title != "" {
		dialog.SetTitle(title)
	}
	if filterName != "" && filterPattern != "" {
		dialog.AddFilter(filterName, filterPattern)
	}
	dialog.AddFilter("所有文件", "*.*")
	result, err := dialog.PromptForSingleSelection()
	if err != nil || result == "" {
		return ""
	}
	return result
}

// DownloadFile 弹出保存文件对话框，将本地文件复制到用户选择的路径。
func (w *WindowService) DownloadFile(srcPath string) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	dialog := w.app.Dialog.SaveFile()
	dialog.SetFilename(filepath.Base(srcPath))
	dialog.AddFilter("所有文件", "*.*")
	result, err := dialog.PromptForSingleSelection()
	if err != nil {
		return err
	}
	if result == "" {
		return nil
	}
	dir := filepath.Dir(result)
	os.MkdirAll(dir, 0755)
	return os.WriteFile(result, data, 0644)
}

// SaveLogFile 弹出保存文件对话框，将日志内容写入用户选择的路径。
func (w *WindowService) SaveLogFile(content string, defaultName string) error {
	dialog := w.app.Dialog.SaveFile()
	dialog.SetFilename(defaultName)
	dialog.AddFilter("日志文件", "*.log")
	dialog.AddFilter("文本文件", "*.txt")
	dialog.AddFilter("所有文件", "*.*")

	result, err := dialog.PromptForSingleSelection()
	if err != nil {
		return err
	}
	if result == "" {
		return nil
	}

	dir := filepath.Dir(result)
	os.MkdirAll(dir, 0755)

	return os.WriteFile(result, []byte(content), 0644)
}

// ReadFileBase64 读取本地文件并返回 data URI（base64 编码），用于前端预览。
func (w *WindowService) ReadFileBase64(filePath string) string {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}
	ext := strings.ToLower(filepath.Ext(filePath))
	mimeType := mime.TypeByExtension(ext)
	if mimeType == "" {
		if ext == ".svg" {
			mimeType = "image/svg+xml"
		} else {
			mimeType = "application/octet-stream"
		}
	}
	if ext == ".svg" {
		return "data:" + mimeType + "," + string(data)
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// GetUserHomeDir 返回当前用户主目录,作为 SFTP 本地面板的默认目录。
func (w *WindowService) GetUserHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "."
	}
	return home
}

// LocalCreateFile 在本地创建空文件,自动创建父目录。
func (w *WindowService) LocalCreateFile(filePath string) error {
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return err
	}
	return os.WriteFile(filePath, []byte{}, 0644)
}

// LocalCreateDir 在本地创建目录。
func (w *WindowService) LocalCreateDir(dirPath string) error {
	return os.MkdirAll(dirPath, 0755)
}

// LocalRename 重命名本地文件或目录(仅允许在所在目录内改名)。
func (w *WindowService) LocalRename(oldPath, newName string) error {
	dir := filepath.Dir(oldPath)
	return os.Rename(oldPath, filepath.Join(dir, newName))
}

// LocalReadText 读取本地文本文件内容,拒绝二进制与大文件。
func (w *WindowService) LocalReadText(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	if err := validateEditableText(data); err != nil {
		return "", err
	}
	return string(data), nil
}

// LocalWriteText 将文本内容写入本地文件。
func (w *WindowService) LocalWriteText(filePath, content string) error {
	return os.WriteFile(filePath, []byte(content), 0644)
}

// OpenWithDefault 使用系统默认程序打开本地文件或目录。
func (w *WindowService) OpenWithDefault(filePath string) error {
	return openWithDefault(filePath)
}

// OpenWithEditor 使用系统默认文本编辑器打开本地文件。
func (w *WindowService) OpenWithEditor(filePath string) error {
	return openWithEditor(filePath)
}
