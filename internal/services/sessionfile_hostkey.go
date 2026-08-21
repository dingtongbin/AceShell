package services

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

func (s *SessionFileService) knownHostsPath(folder string) string {
	if folder == "" || folder == "." {
		return filepath.Join(sessionsDir, "known_hosts.json")
	}
	return filepath.Join(sessionsDir, folder, "known_hosts.json")
}

func (s *SessionFileService) loadKnownHosts(filePath string) (map[string]string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]string), nil
		}
		return nil, err
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = make(map[string]string)
	}
	return m, nil
}

func (s *SessionFileService) saveProjectKnownHosts(folder, addr, keyB64 string) error {
	f := s.knownHostsPath(folder)
	os.MkdirAll(filepath.Dir(f), 0700)
	m, err := s.loadKnownHosts(f)
	if err != nil || m == nil {
		m = make(map[string]string)
	}
	m[addr] = keyB64
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return atomicWriteFile(f, data, 0600)
}

// listSessionFiles 列出指定文件夹下的全部会话文件路径。
func (s *SessionFileService) listSessionFiles(folder string) []string {
	dir := filepath.Join(sessionsDir, folder)
	if folder == "" || folder == "." {
		dir = sessionsDir
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		files = append(files, filepath.Join(dir, e.Name()))
	}
	return files
}

// readSessionAddr 读取会话文件的 host:port 与协议(读取失败返回空)。
func readSessionAddr(path string) (addr, protocol string) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	var data SessionFileData
	if toml.Unmarshal(content, &data) != nil {
		return "", ""
	}
	return net.JoinHostPort(data.Session.Host, fmt.Sprintf("%d", data.Session.Port)), data.Session.Protocol
}

// updateSessionHostKeys 将指纹双写进匹配 host:port 的 SSH 会话文件。
func (s *SessionFileService) updateSessionHostKeys(folder, addr, keyB64 string) error {
	for _, f := range s.listSessionFiles(folder) {
		sessionAddr, protocol := readSessionAddr(f)
		if (protocol != "ssh" && protocol != "sftp") || sessionAddr != addr {
			continue
		}
		s.setSessionField(f, func(sess *SessionInfo) { sess.HostKey = keyB64 })
	}
	return nil
}

// clearSessionHostKeys 清除匹配 host:port 的 SSH 会话文件中的指纹。
func (s *SessionFileService) clearSessionHostKeys(folder, addr string) error {
	for _, f := range s.listSessionFiles(folder) {
		sessionAddr, protocol := readSessionAddr(f)
		if (protocol != "ssh" && protocol != "sftp") || sessionAddr != addr {
			continue
		}
		s.setSessionField(f, func(sess *SessionInfo) { sess.HostKey = "" })
	}
	return nil
}

// setSessionField 读取会话文件,应用字段修改后写回。
func (s *SessionFileService) setSessionField(path string, mutate func(*SessionInfo)) {
	content, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var data SessionFileData
	if toml.Unmarshal(content, &data) != nil {
		return
	}
	mutate(&data.Session)
	out, err := toml.Marshal(&data)
	if err != nil {
		return
	}
	atomicWriteFile(path, out, 0600)
}

// findSessionHostKey 在会话文件中查找匹配 host:port 的 SSH 会话指纹。
func (s *SessionFileService) findSessionHostKey(folder, addr string) (string, bool) {
	for _, f := range s.listSessionFiles(folder) {
		sessionAddr, protocol := readSessionAddr(f)
		if (protocol != "ssh" && protocol != "sftp") || sessionAddr != addr {
			continue
		}
		content, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var data SessionFileData
		if toml.Unmarshal(content, &data) != nil {
			continue
		}
		return data.Session.HostKey, true
	}
	return "", false
}

// hasSessionHostKeyRef 检查文件夹下是否存在匹配 host:port 的 SSH 会话(排除 skipPath)。
func (s *SessionFileService) hasSessionHostKeyRef(folder, addr, skipPath string) bool {
	for _, f := range s.listSessionFiles(folder) {
		if f == skipPath {
			continue
		}
		sessionAddr, protocol := readSessionAddr(f)
		if (protocol == "ssh" || protocol == "sftp") && sessionAddr == addr {
			return true
		}
	}
	return false
}

// SaveHostKey 保存主机指纹:写入文件夹级 known_hosts.json,并双写进匹配的 SSH 会话文件。
func (s *SessionFileService) SaveHostKey(folder, addr, keyB64 string) error {
	if err := s.saveProjectKnownHosts(folder, addr, keyB64); err != nil {
		return err
	}
	return s.updateSessionHostKeys(folder, addr, keyB64)
}

func (s *SessionFileService) removeProjectKnownHosts(folder, addr string) error {
	f := s.knownHostsPath(folder)
	m, err := s.loadKnownHosts(f)
	if err != nil {
		return err
	}
	delete(m, addr)
	data, _ := json.Marshal(m)
	return atomicWriteFile(f, data, 0600)
}

// RemoveHostKey 删除主机指纹:清理文件夹级 known_hosts.json 与会话文件中的指纹。
func (s *SessionFileService) RemoveHostKey(folder, addr string) error {
	if err := s.removeProjectKnownHosts(folder, addr); err != nil {
		return err
	}
	return s.clearSessionHostKeys(folder, addr)
}

// VerifyHostKey 校验主机指纹。
// 查找顺序:会话文件内指纹(匹配 host:port)→ 文件夹级 known_hosts.json。
func (s *SessionFileService) VerifyHostKey(folder, addr, keyB64 string) (bool, error) {
	if stored, ok := s.findSessionHostKey(folder, addr); ok && stored != "" {
		return stored == keyB64, nil
	}
	projectKeys, err := s.loadKnownHosts(s.knownHostsPath(folder))
	if err != nil {
		return false, err
	}
	if stored, ok := projectKeys[addr]; ok {
		return stored == keyB64, nil
	}
	return false, nil
}

