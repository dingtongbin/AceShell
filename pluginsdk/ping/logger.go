package main

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// pingLogger 插件本地诊断日志(数据目录 ping.log)。
type pingLogger struct {
	mu sync.Mutex
	f  *os.File
}

func newPingLogger(path string) *pingLogger {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return &pingLogger{}
	}
	return &pingLogger{f: f}
}

func (l *pingLogger) print(line string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return
	}
	fmt.Fprintf(l.f, "%s %s\n", time.Now().Format("15:04:05.000"), line)
}

func (l *pingLogger) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		_ = l.f.Close()
		l.f = nil
	}
}
