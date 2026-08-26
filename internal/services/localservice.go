package services

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/aymanbagabas/go-pty"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// LocalSession 表示一条本地终端会话。
type LocalSession struct {
	ID       string `json:"id"`
	Shell    string `json:"shell"`    // 显示名
	ShellRef string `json:"shellRef"` // 会话保存的 shell 引用(可执行路径或 wsl://发行版)
	pt       pty.Pty
	cmd      *pty.Cmd
	mu       sync.Mutex
	closed   bool
}

// LocalService 提供本地终端会话管理(Windows ConPTY / Unix PTY)。
type LocalService struct {
	app  *application.App
	sess map[string]*LocalSession
	mu   sync.RWMutex
}

// ShellInfo 可用的本地 shell 条目(新建会话下拉列表)。
type ShellInfo struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func (s *LocalService) SetApp(app *application.App) {
	s.app = app
	s.sess = make(map[string]*LocalSession)
}

func (s *LocalService) safeEmit(event, data string) {
	if s.app != nil {
		s.app.Event.Emit(event, data)
	}
}

// ListShells 扫描系统可用 shell(PowerShell/CMD/Git Bash/WSL 等),返回 JSON 数组。
func (s *LocalService) ListShells() string {
	data, err := json.Marshal(listShells())
	if err != nil {
		return "[]"
	}
	return string(data)
}

// Connect 启动本地 shell 会话;shellRef 为空时使用扫描列表中的第一个可用 shell。
func (s *LocalService) Connect(id, shellRef string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sess == nil {
		return fmt.Errorf("服务未初始化")
	}
	if _, exists := s.sess[id]; exists {
		return fmt.Errorf("会话已存在")
	}

	ref := shellRef
	if ref == "" {
		shells := listShells()
		if len(shells) == 0 {
			return fmt.Errorf("未找到可用终端")
		}
		ref = shells[0].Path
	}

	sess, err := s.startProcess(id, ref)
	if err != nil {
		s.safeEmit("session-status-changed", sessionStatusJSON(id, "error", err.Error()))
		return err
	}
	s.sess[id] = sess

	if MainLogService != nil {
		MainLogService.StartSession(id, "shell", sess.Shell, 0, currentUserName(),
			fmt.Sprintf("%s - %s@local", sess.Shell, currentUserName()))
	}

	s.safeEmit("session-status-changed", sessionStatusJSON(id, "connected", ""))

	go s.readLoop(sess)
	go s.waitLoop(sess)
	return nil
}

// startProcess 创建伪终端并启动 shell 进程。
func (s *LocalService) startProcess(id, shellRef string) (*LocalSession, error) {
	name, args := resolveShellCommand(shellRef)
	pt, err := pty.New()
	if err != nil {
		return nil, fmt.Errorf("创建伪终端失败: %w", err)
	}
	cmd := pt.Command(name, args...)
	if home, homeErr := os.UserHomeDir(); homeErr == nil && home != "" {
		cmd.Dir = home
	}
	if err := cmd.Start(); err != nil {
		pt.Close()
		return nil, fmt.Errorf("启动 %s 失败: %w", name, err)
	}
	return &LocalSession{ID: id, Shell: shellRefDisplayName(shellRef), ShellRef: shellRef, pt: pt, cmd: cmd}, nil
}

// readLoop 循环读取 PTY 输出并分发到界面事件、自动日志与 MCP 输出缓冲。
func (s *LocalService) readLoop(sess *LocalSession) {
	buf := make([]byte, 4096)
	for {
		n, err := sess.pt.Read(buf)
		if n > 0 {
			output := string(buf[:n])
			s.safeEmit("session-output", sessionOutputJSON(sess.ID, output))
			if MainLogService != nil {
				MainLogService.LogOutput(sess.ID, output)
			}
			if MainMcpService != nil {
				MainMcpService.TapOutput(sess.ID, buf[:n])
			}
		}
		if err != nil {
			return
		}
	}
}

// waitLoop 等待 shell 进程退出后清理会话。
func (s *LocalService) waitLoop(sess *LocalSession) {
	_ = sess.cmd.Wait()
	s.closeSession(sess)
}

// Send 向本地终端写入数据。
func (s *LocalService) Send(id, data string) error {
	s.mu.RLock()
	sess, ok := s.sess[id]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("会话未找到")
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.closed {
		return fmt.Errorf("会话已关闭")
	}
	_, err := sess.pt.Write([]byte(data))
	return err
}

// Resize 调整本地终端伪终端尺寸(列x行)。
func (s *LocalService) Resize(id string, cols, rows int) error {
	s.mu.RLock()
	sess, ok := s.sess[id]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("会话未找到")
	}
	if cols <= 0 || rows <= 0 || cols > 1000 || rows > 1000 {
		return fmt.Errorf("非法的终端尺寸 %dx%d", cols, rows)
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.closed {
		return fmt.Errorf("会话已关闭")
	}
	return sess.pt.Resize(cols, rows)
}

// Disconnect 断开本地终端会话。
func (s *LocalService) Disconnect(id string) error {
	s.mu.Lock()
	sess, ok := s.sess[id]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("会话未找到")
	}
	s.closeSession(sess)
	return nil
}

// closeSession 幂等地关闭本地会话:只关闭一次 PTY、删除会话并发送 disconnected 事件。
func (s *LocalService) closeSession(sess *LocalSession) {
	sess.mu.Lock()
	if sess.closed {
		sess.mu.Unlock()
		return
	}
	sess.closed = true
	pt := sess.pt
	sess.mu.Unlock()
	if pt != nil {
		pt.Close()
	}
	s.mu.Lock()
	delete(s.sess, sess.ID)
	s.mu.Unlock()
	if MainLogService != nil {
		MainLogService.EndSession(sess.ID)
	}
	s.safeEmit("session-status-changed", sessionStatusJSON(sess.ID, "disconnected", ""))
}

// resolveShellCommand 将 shell 引用解析为可执行命令与参数。
// 支持 wsl://发行版 前缀(经 wsl.exe 进入指定发行版)与普通可执行路径。
func resolveShellCommand(shellRef string) (string, []string) {
	if after, ok := strings.CutPrefix(shellRef, "wsl://"); ok && after != "" {
		return "wsl.exe", []string{"-d", after}
	}
	return shellRef, nil
}

// shellRefDisplayName 从 shell 引用生成显示名(wsl://Ubuntu → "WSL: Ubuntu";绝对路径取文件名去 .exe)。
func shellRefDisplayName(shellRef string) string {
	if after, ok := strings.CutPrefix(shellRef, "wsl://"); ok && after != "" {
		return "WSL: " + after
	}
	name := strings.TrimSuffix(shellRef, ".exe")
	if idx := strings.LastIndexAny(name, `/\`); idx >= 0 {
		name = name[idx+1:]
	}
	return name
}

// currentUserName 返回当前操作系统用户名(日志元数据用),失败返回空。
func currentUserName() string {
	if u := os.Getenv("USERNAME"); u != "" {
		return u
	}
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return ""
}
