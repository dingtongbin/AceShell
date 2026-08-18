package services

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestSSHConnect(t *testing.T) {
	cfg := loadTestServers(t)
	config := &ssh.ClientConfig{
		User: cfg.SSH.Username,
		Auth: []ssh.AuthMethod{
			ssh.Password(cfg.SSH.Password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	addr := sshAddr(cfg)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		t.Fatalf("SSH 连接失败: %v", err)
	}
	defer client.Close()
	t.Log("SSH 连接成功")
}

func TestSSHShell(t *testing.T) {
	cfg := loadTestServers(t)
	config := &ssh.ClientConfig{
		User: cfg.SSH.Username,
		Auth: []ssh.AuthMethod{
			ssh.Password(cfg.SSH.Password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	client, err := ssh.Dial("tcp", sshAddr(cfg), config)
	if err != nil {
		t.Fatalf("SSH 连接失败: %v", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("创建 Session 失败: %v", err)
	}
	defer session.Close()

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty("xterm-256color", 50, 120, modes); err != nil {
		t.Fatalf("请求 PTY 失败: %v", err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe 失败: %v", err)
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe 失败: %v", err)
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		t.Fatalf("StderrPipe 失败: %v", err)
	}
	_ = stderr

	if err := session.Shell(); err != nil {
		t.Fatalf("Shell 请求失败: %v", err)
	}

	buf := make([]byte, 8192)
	go func() {
		time.Sleep(2 * time.Second)
		stdin.Write([]byte("whoami\n"))
		time.Sleep(1 * time.Second)
		stdin.Write([]byte("exit\n"))
	}()

	output := ""
	done := time.After(10 * time.Second)
loop:
	for {
		select {
		case <-done:
			break loop
		default:
		}
		n, err := stdout.Read(buf)
		if n > 0 {
			output += string(buf[:n])
			t.Logf("输出: %q", string(buf[:n]))
		}
		if err != nil {
			break
		}
	}

	t.Logf("完整输出: %s", output)

	if output == "" {
		t.Fatal("SSH 无输出")
	}

	session.Wait()
}

// sshAddr 拼接 SSH 测试地址(host:port)。
func sshAddr(cfg *testServersConfig) string {
	return cfg.SSH.Host + ":" + strconv.Itoa(cfg.SSH.Port)
}

// TestSSHService_Real 连接真实服务器的集成测试(凭据来自 testservers.json,未配置时跳过)。
func TestSSHService_Real(t *testing.T) {
	cfg := loadTestServers(t)
	svc := &SSHService{}
	svc.SetApp(createDummyApp())

	err := svc.Connect("test-ssh", cfg.SSH.Host, cfg.SSH.Port, cfg.SSH.Username, cfg.SSH.Password, ".", nil)
	if err != nil {
		t.Fatalf("SSHService.Connect 失败: %v", err)
	}
	t.Log("SSHService.Connect 成功")

	time.Sleep(1 * time.Second)
	err = svc.Send("test-ssh", "whoami\n")
	if err != nil {
		t.Fatalf("SSHService.Send 失败: %v", err)
	}
	t.Log("SSHService.Send 成功")

	time.Sleep(2 * time.Second)
	err = svc.Disconnect("test-ssh")
	if err != nil {
		t.Fatalf("SSHService.Disconnect 失败: %v", err)
	}
	t.Log("SSHService.Disconnect 成功")
}

func TestConnectToSession(t *testing.T) {
	withTestDataDir(t)
	cfg := loadTestServers(t)
	svc := &SessionFileService{}
	svc.SetApp(nil)

	// 创建测试会话文件（密码需加密）
	encrypted, err := svc.encrypt(cfg.SSH.Password)
	if err != nil {
		t.Fatalf("加密密码失败: %v", err)
	}

	os.MkdirAll(filepath.Join(sessionsDir, "test"), 0755)
	tomlContent := `[session]
name = "SSH-Test"
host = "` + cfg.SSH.Host + `"
port = ` + strconv.Itoa(cfg.SSH.Port) + `
username = "` + cfg.SSH.Username + `"
password = "` + encrypted + `"
protocol = "ssh"
`
	os.WriteFile(filepath.Join(sessionsDir, "test", "SSH-Test.toml"), []byte(tomlContent), 0644)
	defer os.RemoveAll(filepath.Join(sessionsDir, "test"))

	initTestServices()

	err = svc.ConnectToSession("test/SSH-Test.toml", "test-ssh-conn")
	if err != nil {
		t.Fatalf("ConnectToSession(SSH) 失败: %v", err)
	}
	t.Log("ConnectToSession(SSH) 成功")

	time.Sleep(2 * time.Second)
	if SSHSvc, ok := AppServiceRegistry["ssh"]; ok {
		SSHSvc.(*SSHService).Disconnect("test-ssh-conn")
	}
}

// TestConnectToSession_SFTP 验证 sftp 协议会话走 SSH 连接分发（错误路径，不联网）。
func TestConnectToSession_SFTP(t *testing.T) {
	withTestDataDir(t)
	svc := &SessionFileService{}
	svc.SetApp(nil)
	initTestServices()

	os.MkdirAll(filepath.Join(sessionsDir, "test"), 0755)
	defer os.RemoveAll(filepath.Join(sessionsDir, "test"))

	writeSftpSession := func(extra string) {
		tomlContent := `[session]
name = "SFTP-Test"
host = "10.0.0.1"
port = 22
username = "root"
protocol = "sftp"
` + extra
		os.WriteFile(filepath.Join(sessionsDir, "test", "SFTP-Test.toml"), []byte(tomlContent), 0644)
	}

	// 密码为空:应命中 SSH 分支并报"未提供 SSH 密码"(不发起真实连接)
	writeSftpSession("")
	err := svc.ConnectToSession("test/SFTP-Test.toml", "sftp-conn-1")
	if err == nil || !strings.Contains(err.Error(), "未提供 SSH 密码") {
		t.Fatalf("sftp 空密码应命中 SSH 分支并报密码缺失, got: %v", err)
	}

	// 密钥模式但未选密钥:应命中 SSH 分支的密钥校验
	writeSftpSession("authMode = \"key\"\nkey = \"\"\n")
	err = svc.ConnectToSession("test/SFTP-Test.toml", "sftp-conn-2")
	if err == nil || !strings.Contains(err.Error(), "密钥登录但未选择密钥") {
		t.Fatalf("sftp 密钥模式未选密钥应命中 SSH 分支校验, got: %v", err)
	}
}
