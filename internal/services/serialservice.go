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
		s.safeEmit("session-status-changed", fmt.Sprintf(`{"id":"%s","status":"error","message":"%s"}`, id, err.Error()))
		return err
	}

	sess := &SerialSession{
		ID:       id,
		PortName: portName,
		Status:   "connected",
		port:     port,
	}
	s.sess[id] = sess

	s.safeEmit("session-status-changed", fmt.Sprintf(`{"id":"%s","status":"connected"}`, id))

	go s.readLoop(sess)
	return nil
}

func (s *SerialService) readLoop(sess *SerialSession) {
	buf := make([]byte, 4096)
	for {
		n, err := sess.port.Read(buf)
		if n > 0 {
			s.safeEmit("session-output", fmt.Sprintf(`{"id":"%s","data":"%s"}`, sess.ID, escapeJSON(string(buf[:n]))))
		}
		if err != nil {
			if err != io.EOF {
				s.safeEmit("session-status-changed", fmt.Sprintf(`{"id":"%s","status":"error","message":"%s"}`, sess.ID, err.Error()))
			}
			s.mu.Lock()
			delete(s.sess, sess.ID)
			s.mu.Unlock()
			sess.port.Close()
			s.safeEmit("session-status-changed", fmt.Sprintf(`{"id":"%s","status":"disconnected"}`, sess.ID))
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
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("会话未找到")
	}
	delete(s.sess, id)
	s.mu.Unlock()

	sess.port.Close()
	s.safeEmit("session-status-changed", fmt.Sprintf(`{"id":"%s","status":"disconnected"}`, id))
	return nil
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