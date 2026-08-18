package services

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const sessionsDirName = "sessions"

var sessionsDir string

func init() {
	sessionsDir = filepath.Join(DataDir(), sessionsDirName)
}

type SessionFileData struct {
	Session SessionInfo `toml:"session"`
}

type SessionInfo struct {
	Name           string   `toml:"name"`
	Host           string   `toml:"host"`
	Port           int      `toml:"port"`
	Username       string   `toml:"username"`
	Password       string   `toml:"password"`
	Protocol       string   `toml:"protocol"`
	URL            string   `toml:"url,omitempty"`
	Browser        string   `toml:"browser,omitempty"`
	Notes          string   `toml:"notes"`
	Created        string   `toml:"created"`
	Updated        string   `toml:"updated"`
	NoConfirmClose bool     `toml:"noConfirmClose"`
	AllowedCiphers []string `toml:"allowedCiphers,omitempty"`
	DataBits       int      `toml:"dataBits,omitempty"`
	StopBits       string   `toml:"stopBits,omitempty"`
	Parity         string   `toml:"parity,omitempty"`
	AuthMode       string   `toml:"authMode,omitempty"`
	Key            string   `toml:"key,omitempty"`
	HostKey        string   `toml:"hostKey,omitempty"`
}

type SessionMeta struct {
	Name           string   `json:"name"`
	Host           string   `json:"host"`
	Port           int      `json:"port"`
	Username       string   `json:"username"`
	Protocol       string   `json:"protocol"`
	URL            string   `json:"url"`
	Browser        string   `json:"browser"`
	Created        string   `json:"created"`
	Updated        string   `json:"updated"`
	NoConfirmClose bool     `json:"noConfirmClose"`
	AllowedCiphers []string `json:"allowedCiphers"`
	DataBits       int      `json:"dataBits"`
	StopBits       string   `json:"stopBits"`
	Parity         string   `json:"parity"`
	AuthMode       string   `json:"authMode"`
	Key            string   `json:"key"`
	HostKey        string   `json:"hostKey"`
}

type TreeNode struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"`
	IsDir    bool        `json:"isDir"`
	Protocol string      `json:"protocol,omitempty"`
	Children []*TreeNode `json:"children,omitempty"`
}

type SessionFileService struct {
	app       *application.App
	SSHSvc    *SSHService
	GlobalKeys *GlobalKeyService
}

func (s *SessionFileService) SetApp(app *application.App) {
	s.app = app
	s.ensureSessionsDir()
}

func (s *SessionFileService) emit(event string, data string) {
	if s.app != nil {
		s.app.Event.Emit(event, data)
	}
}

func (s *SessionFileService) ensureSessionsDir() {
	os.MkdirAll(sessionsDir, 0755)
}

func deriveMachineKey() []byte {
	return deriveMachineKeyWithSalt("AceShell-KDF-v2")
}

// deriveMachineKeyWithSalt 用指定 salt 派生本机密钥。
// salt 变更会导致旧会话密文无法解密，因此保留历史 salt 用于兼容（见 legacyMachineKeySalts）。
func deriveMachineKeyWithSalt(salt string) []byte {
	hostname, _ := os.Hostname()
	if runtime.GOOS == "windows" {
		user := os.Getenv("USERNAME")
		domain := os.Getenv("USERDOMAIN")
		if domain != "" {
			hostname = domain + "\\" + hostname
		}
		if user != "" {
			hostname = hostname + "$" + user
		}
	}
	material := hostname + ":" + salt
	hash := sha256.Sum256([]byte(material))
	return hash[:]
}

// legacyMachineKeySalts 历史版本使用过的 KDF salt（按时间从旧到新），用于解密旧会话文件。
var legacyMachineKeySalts = []string{"WailsNetShell-KDF-v2", "FastNetShell-KDF-v2"}

func (s *SessionFileService) encrypt(plaintext string) (string, error) {
	return encryptSecret(plaintext)
}

func (s *SessionFileService) decrypt(encoded string) (string, error) {
	return decryptSecret(encoded)
}

func (s *SessionFileService) getFilePath(folder, name string) string {
	safeName := strings.ReplaceAll(name, "/", "_")
	safeName = strings.ReplaceAll(safeName, "\\", "_")
	if folder == "" || folder == "." {
		return filepath.Join(sessionsDir, safeName+".toml")
	}
	return filepath.Join(sessionsDir, filepath.Clean(folder), safeName+".toml")
}

func (s *SessionFileService) safeSessionPath(subPath string) (string, error) {
	clean := filepath.Clean(subPath)
	full := filepath.Join(sessionsDir, clean)
	absSessions, _ := filepath.Abs(sessionsDir)
	absFull, err := filepath.Abs(full)
	if err != nil {
		return "", fmt.Errorf("无效的路径")
	}
	if !strings.HasPrefix(absFull, absSessions+string(filepath.Separator)) && absFull != absSessions {
		return "", fmt.Errorf("不允许的路径遍历操作")
	}
	return full, nil
}

func (s *SessionFileService) CreateFolder(folderPath string) error {
	s.ensureSessionsDir()
	fullPath, err := s.safeSessionPath(folderPath)
	if err != nil {
		return err
	}
	err = os.MkdirAll(fullPath, 0755)
	if err != nil {
		return err
	}
	s.emit("session-tree-changed", s.GetTree())
	return nil
}

func (s *SessionFileService) DeleteFolder(folderPath string) error {
	if folderPath == keysDirName {
		return fmt.Errorf("密钥库目录不可删除")
	}
	fullPath, err := s.safeSessionPath(folderPath)
	if err != nil {
		return err
	}

	err = os.RemoveAll(fullPath)
	if err != nil {
		return err
	}
	s.emit("session-tree-changed", s.GetTree())
	return nil
}

func (s *SessionFileService) UpdateSession(filePath string, data string) error {
	s.ensureSessionsDir()

	fullPath, err := s.safeSessionPath(filePath)
	if err != nil {
		return err
	}

	oldContent, err := os.ReadFile(fullPath)
	if err != nil {
		return err
	}
	var oldData SessionFileData
	if err := toml.Unmarshal(oldContent, &oldData); err != nil {
		return err
	}

	var info SessionInfo
	if err := toml.Unmarshal([]byte(data), &info); err != nil {
		return fmt.Errorf("无效的会话数据: %w", err)
	}

	if info.Password != "" {
		encrypted, err := s.encrypt(info.Password)
		if err != nil {
			return fmt.Errorf("加密失败: %w", err)
		}
		info.Password = encrypted
	} else {
		// 编辑会话时密码框留空(LoadSession 不回传明文密码):保留原密码,避免覆盖丢失
		info.Password = oldData.Session.Password
	}

	if info.Created == "" {
		info.Created = oldData.Session.Created
	}

	fileData := SessionFileData{Session: info}
	content, err := toml.Marshal(fileData)
	if err != nil {
		return err
	}

	if err := os.WriteFile(fullPath, content, 0644); err != nil {
		return err
	}

	newPath := s.getFilePath(filepath.Dir(filePath), info.Name)
	if newPath != fullPath {
		os.MkdirAll(filepath.Dir(newPath), 0755)
		os.Rename(fullPath, newPath)
	}

	s.emit("session-tree-changed", s.GetTree())
	return nil
}

// SetSessionBrowser 设置 HTTP 会话使用的浏览器 ID，写入会话文件（浏览器不可用时由前端切换调用）。
func (s *SessionFileService) SetSessionBrowser(filePath, browser string) error {
	fullPath, err := s.safeSessionPath(filePath)
	if err != nil {
		return err
	}
	s.setSessionField(fullPath, func(sess *SessionInfo) { sess.Browser = browser })
	return nil
}

func (s *SessionFileService) SaveSession(folder string, data string) error {
	s.ensureSessionsDir()

	var info SessionInfo
	if err := toml.Unmarshal([]byte(data), &info); err != nil {
		return fmt.Errorf("无效的会话数据: %w", err)
	}

	if info.Password != "" {
		encrypted, err := s.encrypt(info.Password)
		if err != nil {
			return fmt.Errorf("加密失败: %w", err)
		}
		info.Password = encrypted
	}

	filePath := s.getFilePath(folder, info.Name)

	if folder != "" && folder != "." {
		dir := filepath.Dir(filePath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	fileData := SessionFileData{Session: info}
	content, err := toml.Marshal(fileData)
	if err != nil {
		return err
	}

	err = os.WriteFile(filePath, content, 0644)
	if err != nil {
		return err
	}

	s.emit("session-tree-changed", s.GetTree())
	return nil
}

func (s *SessionFileService) LoadSession(filePath string) (string, error) {
	fullPath, err := s.safeSessionPath(filePath)
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return "", err
	}
	var data SessionFileData
	if err := toml.Unmarshal(content, &data); err != nil {
		return "", err
	}
	meta := SessionMeta{
		Name:           data.Session.Name,
		Host:           data.Session.Host,
		Port:           data.Session.Port,
		Username:       data.Session.Username,
		Protocol:       data.Session.Protocol,
		URL:            data.Session.URL,
		Browser:        data.Session.Browser,
		Created:        data.Session.Created,
		Updated:        data.Session.Updated,
		NoConfirmClose: data.Session.NoConfirmClose,
		AllowedCiphers: data.Session.AllowedCiphers,
		DataBits:       data.Session.DataBits,
		StopBits:       data.Session.StopBits,
		Parity:         data.Session.Parity,
		AuthMode:       data.Session.AuthMode,
		Key:            data.Session.Key,
		HostKey:        data.Session.HostKey,
	}
	result, _ := json.Marshal(meta)
	return string(result), nil
}

func (s *SessionFileService) ConnectToSession(filePath string, connID string) error {
	return s.connectToSessionWithCreds(filePath, connID, "", "")
}

// connectToSessionWithCreds 连接到会话，支持直接传入凭证（临时凭证不写入文件）。
func (s *SessionFileService) connectToSessionWithCreds(filePath string, connID string, overrideUser string, overridePass string) error {
	fullPath, err := s.safeSessionPath(filePath)
	if err != nil {
		return err
	}
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return err
	}
	var data SessionFileData
	if err := toml.Unmarshal(content, &data); err != nil {
		return err
	}
	var password string
	if data.Session.Password != "" {
		var err error
		password, err = s.decrypt(data.Session.Password)
		if err != nil {
			return fmt.Errorf("本地保存的密码无法解密（可能来自旧版本或另一台设备），请在连接对话框中重新输入密码并勾选记住密码")
		}
	}
	// 临时凭证覆盖文件中的值
	if overrideUser != "" {
		data.Session.Username = overrideUser
	}
	if overridePass != "" {
		password = overridePass
	}
	info := data.Session
	id := connID
	if id == "" {
		id = filePath
	}
	switch info.Protocol {
	case "ssh", "sftp":
		if SSHSvc, ok := AppServiceRegistry["ssh"]; ok {
			folder := filepath.Dir(filePath)
		if info.AuthMode == "key" {
			if info.Key == "" {
				return fmt.Errorf("会话配置为密钥登录但未选择密钥，请在会话编辑中选择密钥")
			}
			keyPEM, keyPass, err := s.resolveKeyMaterial(info.Key)
			if err != nil {
				return err
			}
			return SSHSvc.(*SSHService).ConnectWithKey(id, info.Host, info.Port, info.Username, keyPEM, keyPass, folder, info.AllowedCiphers)
		}
			if password == "" {
				return fmt.Errorf("未提供 SSH 密码，无法连接，请在连接对话框中输入密码")
			}
			return SSHSvc.(*SSHService).Connect(id, info.Host, info.Port, info.Username, password, folder, info.AllowedCiphers)
		}
		return fmt.Errorf("SSH 服务不可用")
	case "serial":
		if serSvc, ok := AppServiceRegistry["serial"]; ok {
			return serSvc.(*SerialService).Connect(id, info.Host, info.Port, info.DataBits, info.StopBits, info.Parity)
		}
		return fmt.Errorf("串口服务不可用")
	default:
		if telSvc, ok := AppServiceRegistry["telnet"]; ok {
			return telSvc.(*DirectTelnetService).ConnectWithCreds(id, info.Host, info.Port, info.Username, password)
		}
		return fmt.Errorf("Telnet 服务不可用")
	}
}

// ConnectToSessionWithCreds 导出方法，供前端调用。
func (s *SessionFileService) ConnectToSessionWithCreds(filePath string, connID string, username string, password string) error {
	return s.connectToSessionWithCreds(filePath, connID, username, password)
}

// resolveKeyMaterial 解析会话的密钥登录材料,返回私钥 PEM 与口令。
// 支持 key://名称(密钥库引用)与本地私钥文件绝对路径。
func (s *SessionFileService) resolveKeyMaterial(keyRef string) (string, string, error) {
	if IsGlobalKeyReference(keyRef) {
		if s.GlobalKeys == nil {
			return "", "", fmt.Errorf("全局密钥服务不可用")
		}
		content, err := s.GlobalKeys.resolveName(keyRef)
		if err != nil {
			return "", "", err
		}
		return content.PrivateKey, content.Passphrase, nil
	}
	if filepath.IsAbs(keyRef) {
		data, err := os.ReadFile(keyRef)
		if err != nil {
			return "", "", fmt.Errorf("读取私钥文件失败: %v", err)
		}
		return string(data), "", nil
	}
	return "", "", fmt.Errorf("无效的密钥引用: %s", keyRef)
}

func (s *SessionFileService) DeleteSession(filePath string) error {
	fullPath, err := s.safeSessionPath(filePath)
	if err != nil {
		return err
	}

	// 清理指纹:会话文件随删除消失;若该文件夹下无其他同 host:port 会话,清理目录 json 键
	content, err := os.ReadFile(fullPath)
	if err == nil {
		var data SessionFileData
		if err := toml.Unmarshal(content, &data); err == nil && (data.Session.Protocol == "ssh" || data.Session.Protocol == "sftp") {
			addr := net.JoinHostPort(data.Session.Host, fmt.Sprintf("%d", data.Session.Port))
			folder := filepath.Dir(filePath)
			if !s.hasSessionHostKeyRef(folder, addr, fullPath) {
				s.removeProjectKnownHosts(folder, addr)
			}
			// 清理 SSH 临时内存指纹
			if s.SSHSvc != nil {
				s.SSHSvc.RemoveTempHostKey(data.Session.Host, data.Session.Port)
			}
		}
	}

	err = os.Remove(fullPath)
	if err != nil {
		return err
	}
	s.emit("session-tree-changed", s.GetTree())
	return nil
}

func (s *SessionFileService) RenameSession(oldPath, newName string) error {
	oldFullPath, err := s.safeSessionPath(oldPath)
	if err != nil {
		return err
	}

	content, err := os.ReadFile(oldFullPath)
	if err != nil {
		return err
	}

	var data SessionFileData
	if err := toml.Unmarshal(content, &data); err != nil {
		return err
	}

	data.Session.Name = newName

	newContent, err := toml.Marshal(data)
	if err != nil {
		return err
	}

	dir := filepath.Dir(oldFullPath)
	newFullPath := filepath.Join(dir, newName+".toml")

	if err := os.WriteFile(newFullPath, newContent, 0644); err != nil {
		return err
	}

	if oldFullPath != newFullPath {
		os.Remove(oldFullPath)
	}

	s.emit("session-tree-changed", s.GetTree())
	return nil
}

func (s *SessionFileService) RenameItem(oldPath, newName string) error {
	fullPath, err := s.safeSessionPath(oldPath)
	if err != nil {
		return err
	}
	info, err := os.Stat(fullPath)
	if err != nil {
		return err
	}

	dir := filepath.Dir(fullPath)

	if info.IsDir() {
		newFullPath := filepath.Join(dir, newName)
		if err := os.Rename(fullPath, newFullPath); err != nil {
			return err
		}
	} else {
		content, err := os.ReadFile(fullPath)
		if err != nil {
			return err
		}
		var data SessionFileData
		if err := toml.Unmarshal(content, &data); err != nil {
			return err
		}
		data.Session.Name = newName
		out, err := toml.Marshal(data)
		if err != nil {
			return err
		}
		if err := os.WriteFile(fullPath, out, 0644); err != nil {
			return err
		}
		newFullPath := filepath.Join(dir, newName+".toml")
		if fullPath != newFullPath {
			os.Rename(fullPath, newFullPath)
		}
	}

	s.emit("session-tree-changed", s.GetTree())
	return nil
}

func (s *SessionFileService) GetTree() string {
	s.ensureSessionsDir()
	tree := s.buildTree(sessionsDir, "")
	result, err := json.Marshal(tree)
	if err != nil {
		return "[]"
	}
	return string(result)
}

func (s *SessionFileService) buildTree(dir, parentPath string) []*TreeNode {
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Printf("buildTree read %s: %v\n", dir, err)
		return nil
	}

	var dirs []*TreeNode
	var files []*TreeNode

	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") || entry.Name() == keysDirName {
			continue
		}
		nodePath := entry.Name()
		if parentPath != "" {
			nodePath = parentPath + "/" + entry.Name()
		}

		if entry.IsDir() {
			children := s.buildTree(filepath.Join(dir, entry.Name()), nodePath)
			dirs = append(dirs, &TreeNode{
				Name:     entry.Name(),
				Path:     nodePath,
				IsDir:    true,
				Children: children,
			})
		} else if strings.HasSuffix(entry.Name(), ".toml") {
			displayName := strings.TrimSuffix(entry.Name(), ".toml")
			protocol := ""
			content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err == nil {
				var data SessionFileData
				if toml.Unmarshal(content, &data) == nil {
					protocol = data.Session.Protocol
				}
			}
			files = append(files, &TreeNode{
				Name:     displayName,
				Path:     nodePath,
				IsDir:    false,
				Protocol: protocol,
			})
		}
	}

	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })

	result := append(dirs, files...)
	return result
}

func (s *SessionFileService) MoveFolder(folderPath, destFolder string) error {
	oldPath, err := s.safeSessionPath(folderPath)
	if err != nil {
		return err
	}
	newDir, err := s.safeSessionPath(destFolder)
	if err != nil {
		return err
	}

	absOld, _ := filepath.Abs(oldPath)
	absNew, _ := filepath.Abs(newDir)

	if strings.HasPrefix(absNew, absOld+string(os.PathSeparator)) {
		return fmt.Errorf("不能将文件夹移动到自身内部")
	}

	if err := os.MkdirAll(filepath.Dir(newDir), 0755); err != nil {
		return err
	}

	newPath := filepath.Join(newDir, filepath.Base(folderPath))

	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("移动失败: %w", err)
	}

	s.emit("session-tree-changed", s.GetTree())
	return nil
}

func (s *SessionFileService) MoveFile(filePath, destFolder string) error {
	oldPath, err := s.safeSessionPath(filePath)
	if err != nil {
		return err
	}
	newDir, err := s.safeSessionPath(destFolder)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(newDir, 0755); err != nil {
		return err
	}

	newPath := filepath.Join(newDir, filepath.Base(filePath))

	if err := os.Rename(oldPath, newPath); err != nil {
		return err
	}

	s.emit("session-tree-changed", s.GetTree())
	return nil
}

func (s *SessionFileService) SetNoConfirmClose(filePath string, value bool) error {
	fullPath, err := s.safeSessionPath(filePath)
	if err != nil {
		return err
	}
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return err
	}
	var data SessionFileData
	if err := toml.Unmarshal(content, &data); err != nil {
		return err
	}
	data.Session.NoConfirmClose = value
	out, err := toml.Marshal(data)
	if err != nil {
		return err
	}
	return os.WriteFile(fullPath, out, 0644)
}

// saveCredentials 保存用户名和密码到会话文件。
// 仅在 remember 标志为 true 时持久化写入。
func (s *SessionFileService) saveCredentials(sessionPath, username, password string, rememberUser, rememberPass bool) error {
	fullPath, err := s.safeSessionPath(sessionPath)
	if err != nil {
		return err
	}
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return err
	}
	var data SessionFileData
	if err := toml.Unmarshal(content, &data); err != nil {
		return err
	}

	if rememberUser && username != "" {
		data.Session.Username = username
	}
	if rememberPass && password != "" {
		encrypted, err := s.encrypt(password)
		if err != nil {
			return fmt.Errorf("加密失败: %w", err)
		}
		data.Session.Password = encrypted
	}

	if !rememberUser && !rememberPass {
		return nil
	}

	out, err := toml.Marshal(data)
	if err != nil {
		return err
	}
	return os.WriteFile(fullPath, out, 0644)
}

// ExportSessions 将选中的会话导出为标准 as9 包文件。
// 口令必填(8~64 位,大写/小写/数字/符号至少三类);keyIDs 为勾选携带的全局密钥。

var AppServiceRegistry = map[string]interface{}{}
var MainLogService *LogService

func init() {
	_ = time.Now()
}
