package services

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/sftp"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type SFTPService struct {
	app       *application.App
	SSHSvc    *SSHService
	clients   map[string]*sftp.Client
	mu        sync.RWMutex
	cancels   map[string]chan struct{}
	cancelMu  sync.Mutex
}

type SFTPFileInfo struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Mode    string `json:"mode"`
	ModTime string `json:"modTime"`
	IsDir   bool   `json:"isDir"`
}

type SFTPTransfer struct {
	ID        string `json:"id"`
	Filename  string `json:"filename"`
	Size      int64  `json:"size"`
	Transferred int64 `json:"transferred"`
	Direction string `json:"direction"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}

func (s *SFTPService) SetApp(app *application.App) {
	s.app = app
	s.clients = make(map[string]*sftp.Client)
	s.cancels = make(map[string]chan struct{})
}

func (s *SFTPService) connect(sessionID string) (*sftp.Client, error) {
	s.mu.RLock()
	if client, ok := s.clients[sessionID]; ok {
		s.mu.RUnlock()
		return client, nil
	}
	s.mu.RUnlock()

	if s.SSHSvc == nil {
		return nil, fmt.Errorf("SSH 服务不可用")
	}

	sshClient := s.SSHSvc.GetClient(sessionID)
	if sshClient == nil {
		return nil, fmt.Errorf("SSH 会话未找到或未连接")
	}

	client, err := sftp.NewClient(sshClient, sftp.UseConcurrentWrites(true))
	if err != nil {
		return nil, fmt.Errorf("SFTP 连接失败: %s", err.Error())
	}

	s.mu.Lock()
	s.clients[sessionID] = client
	s.mu.Unlock()

	return client, nil
}

// Disconnect 断开指定会话的 SFTP 连接。
func (s *SFTPService) Disconnect(sessionID string) error {
	s.mu.Lock()
	client, ok := s.clients[sessionID]
	if ok {
		delete(s.clients, sessionID)
	}
	s.mu.Unlock()

	if ok && client != nil {
		return client.Close()
	}
	return nil
}

// List 列出目录内容，返回 JSON 字符串。sessionID 为 "__local__" 时列本地目录。
func (s *SFTPService) List(sessionID, remotePath string) string {
	if sessionID == "__local__" {
		return s.listLocal(remotePath)
	}
	client, err := s.connect(sessionID)
	if err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}
	entries, err := client.ReadDir(remotePath)
	if err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}
	var files []SFTPFileInfo
	for _, entry := range entries {
		files = append(files, SFTPFileInfo{
			Name:    entry.Name(),
			Size:    entry.Size(),
			Mode:    entry.Mode().String(),
			ModTime: entry.ModTime().Format(time.RFC3339),
			IsDir:   entry.IsDir(),
		})
	}
	data, _ := json.Marshal(map[string]interface{}{"path": remotePath, "files": files})
	return string(data)
}

func (s *SFTPService) listLocal(dir string) string {
	if dir == "" || dir == "." { dir, _ = os.Getwd() }
	entries, err := os.ReadDir(dir)
	if err != nil { return fmt.Sprintf(`{"error":"%s"}`, err.Error()) }
	var files []SFTPFileInfo
	for _, entry := range entries {
		info, _ := entry.Info()
		files = append(files, SFTPFileInfo{
			Name: entry.Name(), Size: info.Size(), Mode: info.Mode().String(),
			ModTime: info.ModTime().Format(time.RFC3339), IsDir: entry.IsDir(),
		})
	}
	data, _ := json.Marshal(map[string]interface{}{"path": dir, "files": files})
	return string(data)
}

// Stat 获取远程文件/目录信息，返回 JSON 字符串。
func (s *SFTPService) Stat(sessionID, remotePath string) string {
	client, err := s.connect(sessionID)
	if err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}

	info, err := client.Stat(remotePath)
	if err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}

	file := SFTPFileInfo{
		Name:    info.Name(),
		Size:    info.Size(),
		Mode:    info.Mode().String(),
		ModTime: info.ModTime().Format(time.RFC3339),
		IsDir:   info.IsDir(),
	}

	data, _ := json.Marshal(file)
	return string(data)
}

// Mkdir 在远程服务器创建目录。
func (s *SFTPService) Mkdir(sessionID, remotePath string) error {
	client, err := s.connect(sessionID)
	if err != nil {
		return err
	}
	return client.Mkdir(remotePath)
}

// Rmdir 删除远程目录。
func (s *SFTPService) Rmdir(sessionID, remotePath string) error {
	client, err := s.connect(sessionID)
	if err != nil {
		return err
	}
	return client.RemoveDirectory(remotePath)
}

// Remove 删除远程文件。
func (s *SFTPService) Remove(sessionID, remotePath string) error {
	client, err := s.connect(sessionID)
	if err != nil {
		return err
	}
	return client.Remove(remotePath)
}

// RemoveAll 递归删除远程文件或目录(含子目录全部内容)。
func (s *SFTPService) RemoveAll(sessionID, remotePath string) error {
	client, err := s.connect(sessionID)
	if err != nil {
		return err
	}
	stat, err := client.Stat(remotePath)
	if err != nil {
		return err
	}
	if !stat.IsDir() {
		return client.Remove(remotePath)
	}
	entries, err := client.ReadDir(remotePath)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		child := strings.TrimSuffix(remotePath, "/") + "/" + entry.Name()
		if entry.IsDir() {
			if err := s.RemoveAll(sessionID, child); err != nil {
				return err
			}
			continue
		}
		if err := client.Remove(child); err != nil {
			return err
		}
	}
	return client.RemoveDirectory(remotePath)
}

// Rename 重命名远程文件或目录。
func (s *SFTPService) Rename(sessionID, oldPath, newPath string) error {
	client, err := s.connect(sessionID)
	if err != nil {
		return err
	}
	return client.Rename(oldPath, newPath)
}

// Upload 上传本地文件到远程服务器，返回传输结果 JSON。
func (s *SFTPService) Upload(sessionID, localPath, remotePath string) string {
	client, err := s.connect(sessionID)
	if err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}

	localFile, err := os.Open(localPath)
	if err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}
	defer localFile.Close()

	stat, err := localFile.Stat()
	if err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}

	remoteFile, err := client.Create(remotePath)
	if err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}
	defer remoteFile.Close()

	written, err := io.Copy(remoteFile, localFile)
	if err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}

	data, _ := json.Marshal(map[string]interface{}{
		"filename": filepath.Base(localPath),
		"size":     stat.Size(),
		"written":  written,
	})
	return string(data)
}

// Download 从远程服务器下载文件到本地，返回传输结果 JSON。
func (s *SFTPService) Download(sessionID, remotePath, localPath string) string {
	client, err := s.connect(sessionID)
	if err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}

	remoteFile, err := client.Open(remotePath)
	if err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}
	defer remoteFile.Close()

	stat, err := remoteFile.Stat()
	if err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}

	os.MkdirAll(filepath.Dir(localPath), 0755)
	localFile, err := os.Create(localPath)
	if err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}
	defer localFile.Close()

	written, err := io.Copy(localFile, remoteFile)
	if err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}

	data, _ := json.Marshal(map[string]interface{}{
		"filename": filepath.Base(remotePath),
		"size":     stat.Size(),
		"written":  written,
	})
	return string(data)
}

// Getwd 获取远程服务器当前工作目录。
func (s *SFTPService) Getwd(sessionID string) string {
	client, err := s.connect(sessionID)
	if err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}

	dir, err := client.Getwd()
	if err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}

	return fmt.Sprintf(`{"path":"%s"}`, dir)
}

// ReadFile 读取远程文件内容并返回字符串;非 UTF-8 文本或过大文件返回错误 JSON。
func (s *SFTPService) ReadFile(sessionID, remotePath string) string {
	client, err := s.connect(sessionID)
	if err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}

	f, err := client.Open(remotePath)
	if err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}

	if err := validateEditableText(data); err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}

	return string(data)
}

// WriteFile 将内容写入远程文件。
func (s *SFTPService) WriteFile(sessionID, remotePath, content string) error {
	client, err := s.connect(sessionID)
	if err != nil {
		return err
	}

	f, err := client.Create(remotePath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.Write([]byte(content))
	return err
}

// Chmod 修改远程文件权限。
func (s *SFTPService) Chmod(sessionID, remotePath string, mode os.FileMode) error {
	client, err := s.connect(sessionID)
	if err != nil {
		return err
	}
	return client.Chmod(remotePath, mode)
}

func (s *SFTPService) isSSHConnected(sessionID string) bool {
	if s.SSHSvc == nil {
		return false
	}
	client := s.SSHSvc.GetClient(sessionID)
	return client != nil
}

// CheckSSH 检查指定会话的 SSH 连接是否可用。
func (s *SFTPService) CheckSSH(sessionID string) bool {
	return s.isSSHConnected(sessionID)
}

func (s *SFTPService) emitProgress(id, name, direction string, size, transferred int64, status, errMsg string) {
	if s.app == nil {
		return
	}
	p := SFTPTransfer{
		ID: id, Filename: name, Size: size,
		Transferred: transferred, Direction: direction,
		Status: status, Error: errMsg,
	}
	data, _ := json.Marshal(p)
	s.app.Event.Emit("sftp-transfer-progress", string(data))
}

type progressReader struct {
	reader    io.Reader
	total     int64
	read      *atomic.Int64
	ticker    *time.Ticker
	done      chan struct{}
}

func newProgressReader(r io.Reader, total int64) *progressReader {
	return &progressReader{
		reader: r,
		total:  total,
		read:   &atomic.Int64{},
		ticker: time.NewTicker(200 * time.Millisecond),
		done:   make(chan struct{}),
	}
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.reader.Read(b)
	p.read.Add(int64(n))
	return n, err
}

func (p *progressReader) Progress() int64 { return p.read.Load() }
func (p *progressReader) Stop()          { p.ticker.Stop(); close(p.done) }

type progressWriter struct {
	writer    io.Writer
	total     int64
	written   *atomic.Int64
	ticker    *time.Ticker
	done      chan struct{}
}

func newProgressWriter(w io.Writer, total int64) *progressWriter {
	return &progressWriter{
		writer:  w,
		total:   total,
		written: &atomic.Int64{},
		ticker:  time.NewTicker(200 * time.Millisecond),
		done:    make(chan struct{}),
	}
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.writer.Write(b)
	p.written.Add(int64(n))
	return n, err
}

func (p *progressWriter) Progress() int64 { return p.written.Load() }
func (p *progressWriter) Stop()          { p.ticker.Stop(); close(p.done) }

// UploadProgress 带进度和取消支持的上传，transferID 为空时自动生成。
func (s *SFTPService) UploadProgress(sessionID, localPath, remotePath, transferID string) string {
	if transferID == "" {
		transferID = fmt.Sprintf("up_%d", time.Now().UnixNano())
	}

	cancel := make(chan struct{})
	s.cancelMu.Lock()
	s.cancels[transferID] = cancel
	s.cancelMu.Unlock()

	defer func() {
		s.cancelMu.Lock()
		delete(s.cancels, transferID)
		s.cancelMu.Unlock()
	}()

	client, err := s.connect(sessionID)
	if err != nil {
		s.emitProgress(transferID, filepath.Base(localPath), "upload", 0, 0, "error", err.Error())
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}

	localFile, err := os.Open(localPath)
	if err != nil {
		s.emitProgress(transferID, filepath.Base(localPath), "upload", 0, 0, "error", err.Error())
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}
	defer localFile.Close()

	stat, err := localFile.Stat()
	if err != nil {
		s.emitProgress(transferID, filepath.Base(localPath), "upload", 0, 0, "error", err.Error())
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}

	remoteFile, err := client.Create(remotePath)
	if err != nil {
		s.emitProgress(transferID, filepath.Base(localPath), "upload", 0, 0, "error", err.Error())
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}
	defer remoteFile.Close()

	pr := newProgressReader(localFile, stat.Size())
	defer pr.Stop()

	go func() {
		for {
			select {
			case <-pr.ticker.C:
				s.emitProgress(transferID, filepath.Base(localPath), "upload", stat.Size(), pr.Progress(), "transferring", "")
			case <-pr.done:
				return
			}
		}
	}()

	errCh := make(chan error, 1)
	go func() {
		_, err := io.Copy(remoteFile, pr)
		errCh <- err
	}()

	select {
	case <-cancel:
		s.emitProgress(transferID, filepath.Base(localPath), "upload", stat.Size(), pr.Progress(), "cancelled", "")
		return fmt.Sprintf(`{"cancelled":true}`)
	case err := <-errCh:
		if err != nil {
			s.emitProgress(transferID, filepath.Base(localPath), "upload", stat.Size(), pr.Progress(), "error", err.Error())
			return fmt.Sprintf(`{"error":"%s"}`, err.Error())
		}
	}

	s.emitProgress(transferID, filepath.Base(localPath), "upload", stat.Size(), stat.Size(), "done", "")
	data, _ := json.Marshal(map[string]interface{}{
		"filename": filepath.Base(localPath),
		"size":     stat.Size(),
		"written":  pr.Progress(),
	})
	return string(data)
}

// DownloadProgress 带进度和取消支持的下载，transferID 为空时自动生成。
func (s *SFTPService) DownloadProgress(sessionID, remotePath, localPath, transferID string) string {
	if transferID == "" {
		transferID = fmt.Sprintf("dn_%d", time.Now().UnixNano())
	}

	cancel := make(chan struct{})
	s.cancelMu.Lock()
	s.cancels[transferID] = cancel
	s.cancelMu.Unlock()

	defer func() {
		s.cancelMu.Lock()
		delete(s.cancels, transferID)
		s.cancelMu.Unlock()
	}()

	client, err := s.connect(sessionID)
	if err != nil {
		s.emitProgress(transferID, filepath.Base(remotePath), "download", 0, 0, "error", err.Error())
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}

	remoteFile, err := client.Open(remotePath)
	if err != nil {
		s.emitProgress(transferID, filepath.Base(remotePath), "download", 0, 0, "error", err.Error())
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}
	defer remoteFile.Close()

	stat, err := remoteFile.Stat()
	if err != nil {
		s.emitProgress(transferID, filepath.Base(remotePath), "download", 0, 0, "error", err.Error())
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}

	os.MkdirAll(filepath.Dir(localPath), 0755)
	localFile, err := os.Create(localPath)
	if err != nil {
		s.emitProgress(transferID, filepath.Base(remotePath), "download", 0, 0, "error", err.Error())
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}
	defer localFile.Close()

	pw := newProgressWriter(localFile, stat.Size())
	defer pw.Stop()

	go func() {
		for {
			select {
			case <-pw.ticker.C:
				s.emitProgress(transferID, filepath.Base(remotePath), "download", stat.Size(), pw.Progress(), "transferring", "")
			case <-pw.done:
				return
			}
		}
	}()

	errCh := make(chan error, 1)
	go func() {
		_, err := io.Copy(pw, remoteFile)
		errCh <- err
	}()

	select {
	case <-cancel:
		s.emitProgress(transferID, filepath.Base(remotePath), "download", stat.Size(), pw.Progress(), "cancelled", "")
		return fmt.Sprintf(`{"cancelled":true}`)
	case err := <-errCh:
		if err != nil {
			s.emitProgress(transferID, filepath.Base(remotePath), "download", stat.Size(), pw.Progress(), "error", err.Error())
			return fmt.Sprintf(`{"error":"%s"}`, err.Error())
		}
	}

	s.emitProgress(transferID, filepath.Base(remotePath), "download", stat.Size(), stat.Size(), "done", "")
	data, _ := json.Marshal(map[string]interface{}{
		"filename": filepath.Base(remotePath),
		"size":     stat.Size(),
		"written":  pw.Progress(),
	})
	return string(data)
}

// CancelTransfer 取消指定 ID 的传输。
func (s *SFTPService) CancelTransfer(transferID string) error {
	s.cancelMu.Lock()
	ch, ok := s.cancels[transferID]
	s.cancelMu.Unlock()
	if !ok {
		return fmt.Errorf("传输任务未找到: %s", transferID)
	}
	close(ch)
	return nil
}
