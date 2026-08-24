package services

import (
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	iac  = 0xFF
	will = 0xFB
	wont = 0xFC
	doo  = 0xFD
	dont = 0xFE
	sb   = 0xFA
	se   = 0xF0
	nop  = 0xF1
)

const (
	optEcho         = 0x01
	optSuppressGA   = 0x03
	optTerminalType = 0x18
	optWindowSize   = 0x1F
	optNAWS         = 0x23
)

// negotiateTelnet 通用 Telnet 协议协商，解析 IAC 命令并写入响应。
// 返回清理后的纯数据。
func negotiateTelnet(data []byte, writer io.Writer) []byte {
	result := make([]byte, 0, len(data))
	i := 0
	for i < len(data) {
		if data[i] == iac && i+1 < len(data) {
			cmd := data[i+1]
			switch cmd {
			case iac:
				result = append(result, 0xFF)
				i += 2
			case will:
				if i+2 < len(data) {
					opt := data[i+2]
					if shouldAcceptWill(opt) {
						writer.Write([]byte{iac, doo, opt})
					} else {
						writer.Write([]byte{iac, dont, opt})
					}
					i += 3
				} else {
					i += 2
				}
			case wont:
				if i+2 < len(data) {
					opt := data[i+2]
					writer.Write([]byte{iac, dont, opt})
					i += 3
				} else {
					i += 2
				}
			case doo:
				if i+2 < len(data) {
					opt := data[i+2]
					if shouldAcceptDo(opt) {
						writer.Write([]byte{iac, will, opt})
					} else {
						writer.Write([]byte{iac, wont, opt})
					}
					i += 3
				} else {
					i += 2
				}
			case dont:
				if i+2 < len(data) {
					opt := data[i+2]
					writer.Write([]byte{iac, wont, opt})
					i += 3
				} else {
					i += 2
				}
			case sb:
				j := handleSubNegotiation(data, i, writer)
				i = j
			default:
				i += 2
			}
		} else {
			result = append(result, data[i])
			i++
		}
	}
	return result
}

// handleSubNegotiation 处理 Telnet 子协商（SB...SE），返回下一个解析位置。
func handleSubNegotiation(data []byte, start int, writer io.Writer) int {
	j := start + 2
	subData := []byte{}
	for j < len(data)-1 {
		if data[j] == iac && data[j+1] == se {
			j += 2
			break
		}
		subData = append(subData, data[j])
		j++
	}
	if len(subData) >= 2 && subData[0] == optTerminalType && subData[1] == 0x01 {
		termType := []byte("VT220")
		resp := append([]byte{iac, sb, optTerminalType, 0x00}, termType...)
		resp = append(resp, iac, se)
		writer.Write(resp)
	}
	return j
}

func shouldAcceptWill(opt byte) bool {
	switch opt {
	case optEcho, optSuppressGA, optTerminalType:
		return true
	}
	return false
}

func shouldAcceptDo(opt byte) bool {
	switch opt {
	case optSuppressGA, optEcho, optWindowSize:
		return true
	}
	return false
}

// connWriter 是 net.Conn 适配 io.Writer（忽略返回值）。
type connWriter struct {
	conn net.Conn
}

func (w *connWriter) Write(p []byte) (int, error) {
	return w.conn.Write(p)
}

// escapeJSON 将字符串转义为可安全嵌入 JSON 字符串的字节序列。

// TelnetSession 表示一条 Telnet 连接会话。
type TelnetSession struct {
	ID   string
	Host string
	Port int
	conn net.Conn
	mu   sync.Mutex
	// 自动登录:loginState 0=等待 login 提示,1=已发送账号等待 Password,2=完成
	username   string
	password   string
	loginState int
	tail       string
	closed     bool
}

// DirectTelnetService 提供 Telnet 会话的直连管理(连接/收发/断开)。
type DirectTelnetService struct {
	app  *application.App
	sess map[string]*TelnetSession
	mu   sync.RWMutex
}

func (d *DirectTelnetService) SetApp(app *application.App) {
	d.app = app
	d.sess = make(map[string]*TelnetSession)
}

func (d *DirectTelnetService) emit(event string, data string) {
	if d.app != nil {
		d.app.Event.Emit(event, data)
	}
}

// Connect 连接 Telnet 服务器。
func (d *DirectTelnetService) Connect(id, host string, port int) error {
	return d.connect(id, host, port, "", "")
}

// ConnectWithCreds 连接 Telnet 并携带账号密码；连接后检测 login/Password 提示自动登录。
func (d *DirectTelnetService) ConnectWithCreds(id, host string, port int, username, password string) error {
	return d.connect(id, host, port, username, password)
}

func (d *DirectTelnetService) connect(id, host string, port int, username, password string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, exists := d.sess[id]; exists {
		return nil
	}

	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, fmt.Sprintf("%d", port)), 10*time.Second)
	if err != nil {
		d.emit("session-status-changed", sessionStatusJSON(id, "error", err.Error()))
		return err
	}

	sess := &TelnetSession{ID: id, Host: host, Port: port, conn: conn, username: username, password: password}
	d.sess[id] = sess

	if MainLogService != nil {
		MainLogService.StartSession(id, "telnet", host, port, username, fmt.Sprintf("%s@%s:%d", username, host, port))
	}

	d.emit("session-status-changed", sessionStatusJSON(id, "connected", ""))

	conn.Write([]byte{iac, will, optSuppressGA})
	conn.Write([]byte{iac, will, optEcho})
	conn.Write([]byte{iac, doo, optSuppressGA})

	go d.readLoop(sess)
	go d.keepaliveLoop(sess)
	return nil
}

func (d *DirectTelnetService) readLoop(sess *TelnetSession) {
	buf := make([]byte, 4096)
	for {
		n, err := sess.conn.Read(buf)
		if n > 0 {
			cleaned := d.negotiate(sess, buf[:n])
			if len(cleaned) > 0 {
				output := string(cleaned)
				d.emit("session-output", sessionOutputJSON(sess.ID, output))
				if MainLogService != nil { MainLogService.LogOutput(sess.ID, output) }
				if MainMcpService != nil { MainMcpService.TapOutput(sess.ID, cleaned) }
				d.tryAutoLogin(sess, output)
			}
		}
		if err != nil {
			if MainLogService != nil { MainLogService.EndSession(sess.ID) }
			if err != io.EOF {
				d.emit("session-status-changed", sessionStatusJSON(sess.ID, "error", err.Error()))
			}
			d.closeSession(sess)
			return
		}
	}
}

// tryAutoLogin 检测登录提示并自动发送账号密码。
// 仅当会话携带凭据且尚未完成登录时生效；提示可能分块到达，用滑动窗口匹配。
func (d *DirectTelnetService) tryAutoLogin(sess *TelnetSession, output string) {
	if sess.loginState >= 2 || sess.username == "" {
		return
	}
	sess.tail += output
	if len(sess.tail) > 128 {
		sess.tail = sess.tail[len(sess.tail)-128:]
	}
	low := strings.ToLower(sess.tail)
	switch sess.loginState {
	case 0:
		if tailHasPrompt(low, "login:") {
			sess.sendLine(sess.username)
			sess.loginState = 1
			sess.tail = ""
		}
	case 1:
		if tailHasPrompt(low, "password:") {
			sess.sendLine(sess.password)
			sess.loginState = 2
			sess.tail = ""
		}
	}
}

// tailHasPrompt 检查文本末尾是否以提示词结尾；大小写不敏感，允许提示词后跟空白。
func tailHasPrompt(text, prompt string) bool {
	low := strings.ToLower(text)
	idx := strings.LastIndex(low, prompt)
	if idx < 0 {
		return false
	}
	return strings.TrimSpace(low[idx+len(prompt):]) == ""
}

func (s *TelnetSession) sendLine(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		s.conn.Write([]byte(line + "\r\n"))
	}
}

// negotiate 处理 Telnet 协议协商，返回清理后的纯数据。
func (d *DirectTelnetService) negotiate(sess *TelnetSession, data []byte) []byte {
	return negotiateTelnet(data, &connWriter{sess.conn})
}

func (s *TelnetSession) sendIAC(cmd byte, opt byte) {
	s.conn.Write([]byte{iac, cmd, opt})
}

// Send 向 Telnet 会话发送数据。
func (d *DirectTelnetService) Send(id, data string) error {
	d.mu.RLock()
	sess, ok := d.sess[id]
	d.mu.RUnlock()
	if !ok {
		return fmt.Errorf("会话未找到")
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.closed {
		return fmt.Errorf("会话已关闭")
	}
	_, err := sess.conn.Write([]byte(data))
	return err
}

// Disconnect 断开 Telnet 会话。
func (d *DirectTelnetService) Disconnect(id string) error {
	d.mu.Lock()
	sess, ok := d.sess[id]
	d.mu.Unlock()
	if !ok {
		return fmt.Errorf("会话未找到")
	}
	d.closeSession(sess)
	return nil
}

// closeSession 幂等地关闭 Telnet 会话:只关闭一次连接、删除会话并发送 disconnected 事件。
func (d *DirectTelnetService) closeSession(sess *TelnetSession) {
	sess.mu.Lock()
	if sess.closed {
		sess.mu.Unlock()
		return
	}
	sess.closed = true
	conn := sess.conn
	sess.mu.Unlock()
	if conn != nil {
		conn.Close()
	}
	d.mu.Lock()
	delete(d.sess, sess.ID)
	d.mu.Unlock()
	d.emit("session-status-changed", sessionStatusJSON(sess.ID, "disconnected", ""))
}

// keepaliveLoop 周期性发送 Telnet NOP 保活探测,检测对端存活;会话关闭后退出。
func (d *DirectTelnetService) keepaliveLoop(sess *TelnetSession) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		sess.mu.Lock()
		if sess.closed || sess.conn == nil {
			sess.mu.Unlock()
			return
		}
		conn := sess.conn
		sess.mu.Unlock()
		conn.Write([]byte{iac, nop})
	}
}
