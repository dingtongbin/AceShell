package services

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"go.bug.st/serial"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type SerialService struct {
	app  *application.App
	sess map[string]*SerialSession
	mu   sync.RWMutex
}

type SerialSession struct {
	ID       string `json:"id"`
	PortName string `json:"portName"`
	Status   string `json:"status"`
	port     serial.Port
	mu       sync.Mutex
	closed   bool
}

func (s *SerialService) SetApp(app *application.App) {
	s.app = app
	s.sess = make(map[string]*SerialSession)
}

func (s *SerialService) safeEmit(event, data string) {
	if s.app != nil {
		s.app.Event.Emit(event, data)
	}
}

// Connect 打开串口连接并启动数据读取循环。
func (s *SerialService) Connect(id, portName string, baudRate, dataBits int, stopBits, parity string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sess == nil {
		return fmt.Errorf("服务未初始化")
	}

	if _, exists := s.sess[id]; exists {
		return fmt.Errorf("会话已存在")
	}

	if dataBits < 5 || dataBits > 8 {
		dataBits = 8
	}

	mode := &serial.Mode{
		BaudRate: baudRate,
		DataBits: dataBits,
		Parity:   parseParity(parity),
		StopBits: parseStopBits(stopBits),
	}

	port, err := serial.Open(portName, mode)
	if err != nil {
		s.safeEmit("session-status-changed", sessionStatusJSON(id, "error", err.Error()))
		return err
	}

	sess := &SerialSession{
		ID:       id,
		PortName: portName,
		Status:   "connected",
		port:     port,
	}
	s.sess[id] = sess

	if MainLogService != nil {
		MainLogService.StartSession(id, "serial", portName, baudRate, "", fmt.Sprintf("%s@%d", portName, baudRate))
	}

	s.safeEmit("session-status-changed", sessionStatusJSON(id, "connected", ""))

	go s.readLoop(sess)
	return nil
}

func (s *SerialService) readLoop(sess *SerialSession) {
	buf := make([]byte, 4096)
	for {
		n, err := sess.port.Read(buf)
		if n > 0 {
			s.safeEmit("session-output", sessionOutputJSON(sess.ID, string(buf[:n])))
			if MainLogService != nil { MainLogService.LogOutput(sess.ID, string(buf[:n])) }
			if MainMcpService != nil { MainMcpService.TapOutput(sess.ID, buf[:n]) }
		}
		if err != nil {
			if err != io.EOF {
				s.safeEmit("session-status-changed", sessionStatusJSON(sess.ID, "error", err.Error()))
			}
			s.closeSession(sess)
			return
		}
	}
}

// Send 向串口发送数据。
func (s *SerialService) Send(id, data string) error {
	s.mu.RLock()
	if s.sess == nil {
		s.mu.RUnlock()
		return fmt.Errorf("服务未初始化")
	}
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
	_, err := sess.port.Write([]byte(data))
	return err
}

// Disconnect 断开串口连接并清理资源。
func (s *SerialService) Disconnect(id string) error {
	s.mu.Lock()
	if s.sess == nil {
		s.mu.Unlock()
		return fmt.Errorf("服务未初始化")
	}
	sess, ok := s.sess[id]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("会话未找到")
	}
	s.closeSession(sess)
	return nil
}

// closeSession 幂等地关闭串口会话:只关闭一次端口、删除会话并发送 disconnected 事件。
func (s *SerialService) closeSession(sess *SerialSession) {
	sess.mu.Lock()
	if sess.closed {
		sess.mu.Unlock()
		return
	}
	sess.closed = true
	port := sess.port
	sess.mu.Unlock()
	if port != nil {
		port.Close()
	}
	s.mu.Lock()
	delete(s.sess, sess.ID)
	s.mu.Unlock()
	if MainLogService != nil {
		MainLogService.EndSession(sess.ID)
	}
	s.safeEmit("session-status-changed", sessionStatusJSON(sess.ID, "disconnected", ""))
}

// ListPorts 列出系统可用串口。
func (s *SerialService) ListPorts() string {
	ports, err := serial.GetPortsList()
	if err != nil {
		return `[]`
	}
	data, _ := json.Marshal(ports)
	return string(data)
}

func parseParity(s string) serial.Parity {
	switch s {
	case "odd":
		return serial.OddParity
	case "even":
		return serial.EvenParity
	case "mark":
		return serial.MarkParity
	case "space":
		return serial.SpaceParity
	default:
		return serial.NoParity
	}
}

func parseStopBits(s string) serial.StopBits {
	switch s {
	case "1.5":
		return serial.OnePointFiveStopBits
	case "2":
		return serial.TwoStopBits
	default:
		return serial.OneStopBit
	}
}