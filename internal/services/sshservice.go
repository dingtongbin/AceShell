package services

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/pelletier/go-toml/v2"
	"golang.org/x/crypto/ssh"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// SSHService 管理所有 SSH 连接会话。
// 每个会话通过唯一 ID 标识，支持多标签并发连接。
type SSHService struct {
	app            *application.App
	sess           map[string]*SSHSession
	mu             sync.RWMutex
	SessionFileSvc *SessionFileService
	SFTPSvc        *SFTPService
	tempHostKeys   map[string]string // 临时指纹：addr -> keyB64（不写入文件）
}

// SSHSession 表示一个 SSH 连接实例，包含终端 I/O 管道。
type SSHSession struct {
	ID     string `json:"id"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
	User   string `json:"user"`
	Status string `json:"status"`
	client *ssh.Client
	session  *ssh.Session
	stdin    io.WriteCloser
	stdout   io.Reader
	stderr   io.Reader
	mu       sync.Mutex
	closed   bool
}

// SetApp 初始化服务，绑定 Wails 应用实例。
func (s *SSHService) SetApp(app *application.App) {
	s.app = app
	s.sess = make(map[string]*SSHSession)
	s.tempHostKeys = make(map[string]string)
}

// emit 安全发送事件，app 为 nil 时跳过。
func (s *SSHService) emit(event string, data string) {
	if s.app != nil {
		s.app.Event.Emit(event, data)
	}
}

// Connect 建立 SSH 密码认证连接: 占位 → 拨号认证 → PTY/管道 → 启动读/保活循环。
// ciphers 为空时使用默认加密算法。
func (s *SSHService) Connect(id, host string, port int, user, password, folder string, ciphers []string) error {
	return s.connectCommon(id, host, port, user, []ssh.AuthMethod{ssh.Password(password)}, folder, ciphers)
}

// ConnectWithKey 使用私钥 PEM 建立 SSH 连接(可为加密 PEM,需提供 keyPass)。
func (s *SSHService) ConnectWithKey(id, host string, port int, user, keyPEM, keyPass, folder string, ciphers []string) error {
	signer, err := parsePrivateKey([]byte(keyPEM), []byte(keyPass))
	if err != nil {
		err = fmt.Errorf("解析私钥失败: %w", err)
		s.emitError(id, err)
		return err
	}
	return s.connectCommon(id, host, port, user, []ssh.AuthMethod{ssh.PublicKeys(signer)}, folder, ciphers)
}

// connectCommon 连接公共路径: 先以 connecting 状态占位并立即释放服务锁,
// 拨号/认证在锁外执行(避免 10s 握手阻塞 Disconnect/GetSessions 等调用方),
// 成功后二次加锁升级为 connected;期间被移除则释放新建资源。
func (s *SSHService) connectCommon(id, host string, port int, user string, auths []ssh.AuthMethod, folder string, ciphers []string) error {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))

	s.mu.Lock()
	if _, exists := s.sess[id]; exists {
		s.mu.Unlock()
		return fmt.Errorf("会话已存在")
	}
	placeholder := &SSHSession{ID: id, Host: host, Port: port, User: user, Status: "connecting"}
	s.sess[id] = placeholder
	s.mu.Unlock()

	client, err := s.dialSSHAuths(addr, user, auths, folder, ciphers)
	if err != nil {
		s.removeSession(id)
		s.emitError(id, err)
		return err
	}

	session, stdin, stdout, stderr, err := s.setupSession(id, client)
	if err != nil {
		client.Close()
		s.removeSession(id)
		s.emitError(id, err)
		return err
	}

	s.mu.Lock()
	if s.sess[id] != placeholder {
		// 连接建立期间会话已被移除(如用户取消): 释放资源
		s.mu.Unlock()
		session.Close()
		client.Close()
		return fmt.Errorf("会话已断开")
	}
	placeholder.client = client
	placeholder.session = session
	placeholder.stdin = stdin
	placeholder.stdout = stdout
	placeholder.stderr = stderr
	placeholder.Status = "connected"
	s.mu.Unlock()

	s.emit("session-status-changed", sessionStatusJSON(id, "connected", ""))

	if MainLogService != nil {
		MainLogService.StartSession(id, "ssh", host, port, user, fmt.Sprintf("%s@%s:%d", user, host, port))
	}

	go s.readLoop(placeholder)
	go s.keepaliveLoop(placeholder)
	return nil
}

// removeSession 移除尚未建立的占位会话(错误事件由调用方负责发送)。
func (s *SSHService) removeSession(id string) {
	s.mu.Lock()
	delete(s.sess, id)
	s.mu.Unlock()
}

// dialSSHAuths 建立 TCP 连接并完成 SSH 认证，返回 ssh.Client。
func (s *SSHService) dialSSHAuths(addr, user string, auths []ssh.AuthMethod, folder string, ciphers []string) (*ssh.Client, error) {
	hostKeyCb := s.buildHostKeyCallback(addr, folder)

	config := &ssh.ClientConfig{
		User:            user,
		Auth:            auths,
		HostKeyCallback: hostKeyCb,
		Timeout:         10 * time.Second,
	}
	if len(ciphers) > 0 {
		config.Ciphers = ciphers
	}

	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, err
	}
	return client, nil
}

// buildHostKeyCallback 构建主机密钥验证回调。
// 若 SessionFileSvc 可用，使用项目级 known_hosts 验证；否则跳过验证。
func (s *SSHService) buildHostKeyCallback(addr, folder string) ssh.HostKeyCallback {
	if s.SessionFileSvc == nil {
		return ssh.InsecureIgnoreHostKey()
	}

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		keyB64 := base64.StdEncoding.EncodeToString(key.Marshal())

		// 1. 检查临时内存指纹（仅一次/跳过）
		// 注意: 此回调在 Connect() 的 s.mu.Lock() 内执行，无需额外加锁
		if tempKey, inTemp := s.tempHostKeys[addr]; inTemp && tempKey == keyB64 {
			return nil
		}

		// 2. 检查文件中的持久化指纹
		ok, err := s.SessionFileSvc.VerifyHostKey(folder, addr, keyB64)
		if err == nil && ok {
			return nil
		}

		// 3. 未找到任何记录 — 拒绝连接（应通过 CheckFingerprint + 对话框前置处理）
		return fmt.Errorf("未知的主机密钥: %s", addr)
	}
}

// setupSession 在指定 client 上创建 SSH channel：请求 PTY、建立管道、启动 Shell。
// 失败时由调用方负责关闭 client（此处不关闭 client，便于内层标签页复用同一连接）。
func (s *SSHService) setupSession(id string, client *ssh.Client) (*ssh.Session, io.WriteCloser, io.Reader, io.Reader, error) {
	session, err := client.NewSession()
	if err != nil {
		return nil, nil, nil, nil, err
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}

	if err := session.RequestPty("xterm-256color", 50, 120, modes); err != nil {
		session.Close()
		return nil, nil, nil, nil, err
	}

	stdin, stdout, stderr, err := s.setupPipes(session)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	if err := session.Shell(); err != nil {
		session.Close()
		return nil, nil, nil, nil, err
	}

	return session, stdin, stdout, stderr, nil
}

// setupPipes 建立 SSH 会话的 stdin/stdout/stderr 管道。
func (s *SSHService) setupPipes(session *ssh.Session) (io.WriteCloser, io.Reader, io.Reader, error) {
	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		return nil, nil, nil, err
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		return nil, nil, nil, err
	}

	stderr, err := session.StderrPipe()
	if err != nil {
		session.Close()
		return nil, nil, nil, err
	}

	return stdin, stdout, stderr, nil
}

// emitError 发送会话错误事件(载荷经 json.Marshal,消息含特殊字符安全)。
func (s *SSHService) emitError(id string, err error) {
	s.emit("session-status-changed", sessionStatusJSON(id, "error", err.Error()))
}

// readLoop 持续读取 SSH 会话输出并发送到前端，直到连接断开。
func (s *SSHService) readLoop(sess *SSHSession) {
	buf := make([]byte, 8192)
	for {
		n, err := sess.stdout.Read(buf)
		if n > 0 {
			output := string(buf[:n])
			s.emit("session-output", sessionOutputJSON(sess.ID, output))
			if MainLogService != nil {
				MainLogService.LogOutput(sess.ID, output)
			}
			if MainMcpService != nil {
				MainMcpService.TapOutput(sess.ID, buf[:n])
			}
		}
		if err != nil {
			reason := "连接已断开"
			if err != io.EOF {
				reason = fmt.Sprintf("连接断开: %s", err.Error())
			}
			if MainLogService != nil {
				MainLogService.EndSession(sess.ID)
			}
			s.emit("session-output", sessionOutputJSON(sess.ID, fmt.Sprintf("\r\n\x1b[33m%s\x1b[0m\r\n", reason)))
			s.closeSession(sess)
			return
		}
	}
}


// keepaliveLoop 周期性发送 SSH 保活请求,检测对端存活;会话关闭后自动退出。
// wantReply=false: 仅发送不等待回复,由 TCP 写失败感知死链,
// 避免无超时阻塞的 SendRequest(true) 在半开连接上永久悬挂 goroutine。
func (s *SSHService) keepaliveLoop(sess *SSHSession) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		sess.mu.Lock()
		if sess.closed || sess.client == nil {
			sess.mu.Unlock()
			return
		}
		client := sess.client
		sess.mu.Unlock()
		if _, _, err := client.SendRequest("keepalive@openssh.com", false, nil); err != nil {
			return
		}
	}
}
// Send 向指定会话发送数据。
func (s *SSHService) Send(id, data string) error {
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
	if sess.stdin == nil {
		return fmt.Errorf("会话尚未就绪")
	}
	_, err := sess.stdin.Write([]byte(data))
	return err
}

// Resize 调整指定会话的终端窗口大小。
func (s *SSHService) Resize(id string, cols, rows int) error {
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
	if sess.session == nil {
		return fmt.Errorf("会话尚未就绪")
	}
	return sess.session.WindowChange(rows, cols)
}


// tempHostKey 线程安全地读取临时内存指纹。
func (s *SSHService) tempHostKey(addr string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	k, ok := s.tempHostKeys[addr]
	return k, ok
}

// closeSession 安全地关闭 SSH 会话资源,幂等(只关闭一次),并联动清理 SFTP 连接。
func (s *SSHService) closeSession(sess *SSHSession) {
	sess.mu.Lock()
	if sess.closed {
		sess.mu.Unlock()
		return
	}
	sess.closed = true
	client := sess.client
	session := sess.session
	sess.mu.Unlock()

	if session != nil {
		session.Close()
	}
	if client != nil {
		client.Close()
	}
	if s.SFTPSvc != nil {
		s.SFTPSvc.Disconnect(sess.ID)
	}
	s.mu.Lock()
	delete(s.sess, sess.ID)
	s.mu.Unlock()
	s.emit("session-status-changed", sessionStatusJSON(sess.ID, "disconnected", ""))
}

// Disconnect 断开指定 SSH 连接并清理资源。
func (s *SSHService) Disconnect(id string) error {
	s.mu.Lock()
	sess, ok := s.sess[id]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("会话未找到")
	}
	s.closeSession(sess)
	return nil
}

// GetSessions 返回所有活跃会话的 JSON 列表。
func (s *SSHService) GetSessions() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var list []map[string]string
	for _, sess := range s.sess {
		list = append(list, map[string]string{
			"id":     sess.ID,
			"host":   sess.Host,
			"status": sess.Status,
		})
	}
	data, _ := json.Marshal(list)
	return string(data)
}

// GetClient 返回指定会话的底层 ssh.Client，用于 SFTP 等子系统。
func (s *SSHService) GetClient(id string) *ssh.Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if sess, ok := s.sess[id]; ok {
		return sess.client
	}
	return nil
}

// CheckFingerprint 检查服务器指纹状态。
// 返回："match"（已保存且匹配）、"mismatch"（已保存但不匹配）、"not_found"（未保存）。
func (s *SSHService) CheckFingerprint(host string, port int, folder string) string {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))

	// 检查临时指纹
	if key, ok := s.tempHostKey(addr); ok {
		_ = key // 临时指纹存在，需要实际连接时验证
	}

	// 拨号获取指纹
	config := &ssh.ClientConfig{
		User:            "probe",
		Auth:             []ssh.AuthMethod{ssh.Password("probe")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	var serverKey string
	config.HostKeyCallback = func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		serverKey = base64.StdEncoding.EncodeToString(key.Marshal())
		return fmt.Errorf("probe done")
	}

	ssh.Dial("tcp", addr, config)

	if serverKey == "" {
		return `{"status":"error","message":"无法获取服务器指纹"}`
	}

	// 检查临时内存指纹
	if key, ok := s.tempHostKey(addr); ok {
		if key == serverKey {
			return `{"status":"match","key":"` + serverKey + `"}`
		}
		return `{"status":"mismatch","key":"` + serverKey + `","saved":"` + key + `"}`
	}

	// 检查已保存的指纹
	if s.SessionFileSvc != nil {
		ok, err := s.SessionFileSvc.VerifyHostKey(folder, addr, serverKey)
		if err == nil && ok {
			return `{"status":"match","key":"` + serverKey + `"}`
		}
		// 检查是否有已保存的指纹（不匹配）
		savedKey := s.getSavedHostKey(folder, addr)
		if savedKey != "" {
			return `{"status":"mismatch","key":"` + serverKey + `","saved":"` + savedKey + `"}`
		}
	}

	return `{"status":"not_found","key":"` + serverKey + `"}`
}

// getSavedHostKey 获取已保存的主机指纹（不验证）。
// 查找顺序:会话文件内指纹(匹配 host:port)→ 文件夹级 known_hosts.json。
func (s *SSHService) getSavedHostKey(folder, addr string) string {
	if s.SessionFileSvc == nil {
		return ""
	}
	// 尝试从会话文件获取（双写后的指纹）
	if stored, ok := s.SessionFileSvc.findSessionHostKey(folder, addr); ok {
		return stored
	}
	// 尝试从文件夹级 known_hosts 获取
	data, _ := os.ReadFile(filepath.Join(sessionsDir, folder, "known_hosts.json"))
	if len(data) > 0 {
		var hosts map[string]string
		json.Unmarshal(data, &hosts)
		if key, ok := hosts[addr]; ok {
			return key
		}
	}
	return ""
}

// SaveTempHostKey 保存临时指纹（不写入文件）。
func (s *SSHService) SaveTempHostKey(host string, port int, keyB64 string) {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	s.mu.Lock()
	s.tempHostKeys[addr] = keyB64
	s.mu.Unlock()
}

// RemoveTempHostKey 移除临时内存指纹。
func (s *SSHService) RemoveTempHostKey(host string, port int) {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	s.mu.Lock()
	delete(s.tempHostKeys, addr)
	s.mu.Unlock()
}

// SavePermanentHostKey 保存永久指纹（写入文件）。
func (s *SSHService) SavePermanentHostKey(host string, port int, folder, keyB64 string) error {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	s.mu.Lock()
	s.tempHostKeys[addr] = keyB64
	s.mu.Unlock()

	if s.SessionFileSvc != nil {
		s.SessionFileSvc.SaveHostKey(folder, addr, keyB64)
	}
	return nil
}

// SkipFingerprint 跳过指纹校验（加入临时白名单）。
func (s *SSHService) SkipFingerprint(host string, port int, keyB64 string) {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	s.mu.Lock()
	s.tempHostKeys[addr] = keyB64
	s.mu.Unlock()
}

// HasPassword 检查会话是否已有密码。
func (s *SSHService) HasPassword(sessionPath string) bool {
	if s.SessionFileSvc == nil {
		return false
	}
	data, err := os.ReadFile(filepath.Join(sessionsDir, sessionPath))
	if err != nil {
		return false
	}
	var fileData SessionFileData
	if err := toml.Unmarshal(data, &fileData); err != nil {
		return false
	}
	return fileData.Session.Password != ""
}

// GetSessionUsername 获取会话的用户名。
func (s *SSHService) GetSessionUsername(sessionPath string) string {
	if s.SessionFileSvc == nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(sessionsDir, sessionPath))
	if err != nil {
		return ""
	}
	var fileData SessionFileData
	if err := toml.Unmarshal(data, &fileData); err != nil {
		return ""
	}
	return fileData.Session.Username
}

// SaveCredentials 保存用户名和密码到会话文件。
func (s *SSHService) SaveCredentials(sessionPath, username, password string, rememberUser, rememberPass bool) error {
	if s.SessionFileSvc == nil {
		return fmt.Errorf("会话文件服务不可用")
	}
	return s.SessionFileSvc.saveCredentials(sessionPath, username, password, rememberUser, rememberPass)
}
