package services

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
)

// 全局密钥库:会话根目录下 key/ 子目录,每个密钥一个 json 文件。
// 私钥与口令经本机主密钥加密落盘,公钥与指纹明文(非敏感)。
// 会话文件以 key://名称 引用密钥库中的密钥。

const (
	keysDirName        = "key"
	keyReferencePrefix = "key://"
)

// GlobalKeyEntry 密钥条目(不含私钥,用于列表展示)。
type GlobalKeyEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Fingerprint string `json:"fingerprint"`
	Created     string `json:"created"`
	Updated     string `json:"updated"`
}

// GlobalKeyContent 密钥完整内容(内存中使用,含私钥明文)。
type GlobalKeyContent struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	PrivateKey  string `json:"privateKey"`
	Passphrase  string `json:"passphrase"`
	PublicKey   string `json:"publicKey"`
	Fingerprint string `json:"fingerprint"`
}

// globalKeyFile 密钥文件存储结构。
type globalKeyFile struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	PrivateKey  string `json:"privateKey"`
	Passphrase  string `json:"passphrase"`
	PublicKey   string `json:"publicKey"`
	Fingerprint string `json:"fingerprint"`
	Created     string `json:"created"`
	Updated     string `json:"updated"`
}

// GlobalKeyService 全局密钥库服务。
type GlobalKeyService struct {
	mu sync.Mutex
}

// keysDir 返回密钥库目录路径。
func (g *GlobalKeyService) keysDir() string {
	return filepath.Join(sessionsDir, keysDirName)
}

// ensureKeysDir 确保密钥库目录存在。
func (g *GlobalKeyService) ensureKeysDir() error {
	return os.MkdirAll(g.keysDir(), 0700)
}

// keyFilePath 返回密钥文件路径。
func (g *GlobalKeyService) keyFilePath(name string) string {
	return filepath.Join(g.keysDir(), name+".json")
}

// IsGlobalKeyReference 判断是否为全局密钥引用(key://名称)。
func IsGlobalKeyReference(keyPathOrRef string) bool {
	return strings.HasPrefix(keyPathOrRef, keyReferencePrefix)
}

// ListKeys 返回密钥列表 JSON(时间倒序,不含私钥)。
func (g *GlobalKeyService) ListKeys() string {
	entries, err := g.list()
	if err != nil {
		return "[]"
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return "[]"
	}
	return string(data)
}

// list 枚举密钥文件。
func (g *GlobalKeyService) list() ([]GlobalKeyEntry, error) {
	if _, err := os.Stat(g.keysDir()); err != nil {
		return []GlobalKeyEntry{}, nil
	}
	files, err := filepath.Glob(filepath.Join(g.keysDir(), "*.json"))
	if err != nil {
		return nil, err
	}
	var entries []GlobalKeyEntry
	for _, f := range files {
		content, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var kf globalKeyFile
		if json.Unmarshal(content, &kf) != nil || kf.ID == "" {
			continue
		}
		entries = append(entries, GlobalKeyEntry{
			ID:          kf.ID,
			Name:        kf.Name,
			Type:        kf.Type,
			Fingerprint: kf.Fingerprint,
			Created:     kf.Created,
			Updated:     kf.Updated,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Created > entries[j].Created })
	return entries, nil
}

// loadContent 读取密钥完整内容(私钥解密为明文)。
func (g *GlobalKeyService) loadContent(id string) (*GlobalKeyContent, error) {
	files, err := filepath.Glob(filepath.Join(g.keysDir(), "*.json"))
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var kf globalKeyFile
		if json.Unmarshal(data, &kf) != nil || kf.ID != id {
			continue
		}
		priv, err := decryptSecret(kf.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("密钥私钥无法解密(可能来自另一台设备或密钥文件损坏)")
		}
		pass, _ := decryptSecret(kf.Passphrase)
		return &GlobalKeyContent{
			Name:        kf.Name,
			Type:        kf.Type,
			PrivateKey:  priv,
			Passphrase:  pass,
			PublicKey:   kf.PublicKey,
			Fingerprint: kf.Fingerprint,
		}, nil
	}
	return nil, fmt.Errorf("未找到密钥")
}

// resolveName 解析名称引用或 ID,返回密钥内容。
func (g *GlobalKeyService) resolveName(nameOrID string) (*GlobalKeyContent, error) {
	ref := strings.TrimPrefix(nameOrID, keyReferencePrefix)
	if IsGlobalKeyReference(nameOrID) {
		return g.loadByName(ref)
	}
	return g.loadContent(nameOrID)
}

// loadByName 按名称查找密钥(忽略大小写,取最近一条)。
func (g *GlobalKeyService) loadByName(name string) (*GlobalKeyContent, error) {
	entries, err := g.list()
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if strings.EqualFold(e.Name, name) {
			return g.loadContent(e.ID)
		}
	}
	return nil, fmt.Errorf("未找到名为 %q 的密钥", name)
}

// CreateKey 生成新密钥并保存。keyType: ed25519 / rsa2048 / rsa4096。
// passphrase 可空;非空时私钥 PEM 以该口令加密(口令本身再经主密钥加密存储)。
func (g *GlobalKeyService) CreateKey(name, keyType, passphrase string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.ensureKeysDir(); err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("密钥名称不能为空")
	}
	if sanitizeName(name) != name {
		return "", fmt.Errorf("密钥名称包含非法字符(不能包含 / \\ 或 ..)")
	}
	if passphrase != "" && len(passphrase) < 4 {
		return "", fmt.Errorf("口令至少 4 个字符")
	}

	var privPEM, pubPEM []byte
	var fingerprint string
	switch keyType {
	case "ed25519":
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return "", err
		}
		privPEM, err = marshalPrivateKey(priv)
		if err != nil {
			return "", err
		}
		sshPub, err := ssh.NewPublicKey(pub)
		if err != nil {
			return "", err
		}
		pubPEM = ssh.MarshalAuthorizedKey(sshPub)
		fingerprint = ssh.FingerprintSHA256(sshPub)
	case "rsa2048", "rsa4096":
		bits := 2048
		if keyType == "rsa4096" {
			bits = 4096
		}
		priv, err := rsa.GenerateKey(rand.Reader, bits)
		if err != nil {
			return "", err
		}
		privPEM, err = marshalPrivateKey(priv)
		if err != nil {
			return "", err
		}
		sshPub, err := ssh.NewPublicKey(&priv.PublicKey)
		if err != nil {
			return "", err
		}
		pubPEM = ssh.MarshalAuthorizedKey(sshPub)
		fingerprint = ssh.FingerprintSHA256(sshPub)
	default:
		return "", fmt.Errorf("不支持的密钥类型: %s", keyType)
	}

	privPEM = append(privPEM, '\n')
	pubStr := strings.TrimSpace(string(pubPEM))
	if passphrase != "" {
		encrypted, err := encryptPEMWithPassphrase(privPEM, passphrase)
		if err != nil {
			return "", err
		}
		privPEM = encrypted
	}

	encPriv, err := encryptSecret(string(privPEM))
	if err != nil {
		return "", err
	}
	encPass, _ := encryptSecret(passphrase)

	now := time.Now().Format("2006-01-02 15:04:05")
	kf := globalKeyFile{
		ID:          uuid.NewString(),
		Name:        name,
		Type:        keyType,
		PrivateKey:  encPriv,
		Passphrase:  encPass,
		PublicKey:   pubStr,
		Fingerprint: fingerprint,
		Created:     now,
		Updated:     now,
	}

	fileName := g.uniqueKeyFileName(name)
	data, _ := json.MarshalIndent(kf, "", "  ")
	if err := os.WriteFile(filepath.Join(g.keysDir(), fileName), data, 0600); err != nil {
		return "", err
	}
	return kf.ID, nil
}

// uniqueKeyFileName 生成不重名的密钥文件名(重名追加序号)。
func (g *GlobalKeyService) uniqueKeyFileName(name string) string {
	base := name
	for i := 1; ; i++ {
		candidate := base
		if i > 1 {
			candidate = fmt.Sprintf("%s-%d", base, i)
		}
		if _, err := os.Stat(g.keyFilePath(candidate)); err != nil {
			return candidate + ".json"
		}
	}
}

// DeleteKey 删除指定密钥。
func (g *GlobalKeyService) DeleteKey(id string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	files, err := filepath.Glob(filepath.Join(g.keysDir(), "*.json"))
	if err != nil {
		return err
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var kf globalKeyFile
		if json.Unmarshal(data, &kf) == nil && kf.ID == id {
			return os.Remove(f)
		}
	}
	return fmt.Errorf("未找到密钥")
}

// GetKeyContent 返回密钥完整内容 JSON(供前端连接时使用,含私钥明文,仅内存传递)。
func (g *GlobalKeyService) GetKeyContent(id string) (string, error) {
	content, err := g.loadContent(id)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(content)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// GetKeyMaterial 返回可用的 ssh.Signer(私钥 PEM + 口令)。
func (g *GlobalKeyService) signerFromContent(c *GlobalKeyContent) (ssh.Signer, error) {
	key, err := parsePrivateKey([]byte(c.PrivateKey), []byte(c.Passphrase))
	if err != nil {
		return nil, fmt.Errorf("密钥解析失败: %v", err)
	}
	return key, nil
}

// SshCopyKey 用密码认证登录目标主机,将该密钥的公钥追加到 ~/.ssh/authorized_keys。
// hostKeyB64 为目标主机公钥 base64(前端经指纹核对后传入),连接时强制 pin 校验,
// 防止中间人截获密码与公钥;为空时拒绝连接(fail-close)。
// keyRef 支持 key://名称 引用或密钥 id。返回部署结果消息。
func (g *GlobalKeyService) SshCopyKey(keyRef, host string, port int, user, password, hostKeyB64 string) (string, error) {
	if port <= 0 || port > 65535 {
		port = 22
	}
	if strings.TrimSpace(hostKeyB64) == "" {
		return "", fmt.Errorf("未提供主机指纹,已取消部署(防止中间人截获密码)")
	}
	content, err := g.resolveName(keyRef)
	if err != nil {
		return "", err
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{ssh.Password(password)},
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			if base64.StdEncoding.EncodeToString(key.Marshal()) != strings.TrimSpace(hostKeyB64) {
				return fmt.Errorf("主机指纹与确认时不一致,连接已中止")
			}
			return nil
		},
		Timeout: 10 * time.Second,
	}
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return "", fmt.Errorf("连接 %s 失败: %v", addr, err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	pub := strings.TrimSpace(content.PublicKey)
	cmd := "mkdir -p ~/.ssh && chmod 700 ~/.ssh && touch ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys"
	cmd += " && (grep -qF " + shellQuote(pub) + " ~/.ssh/authorized_keys || echo " + shellQuote(pub) + " >> ~/.ssh/authorized_keys)"
	if err := session.Run(cmd); err != nil {
		return "", fmt.Errorf("部署公钥失败: %v", err)
	}
	return "公钥已部署到 " + user + "@" + addr, nil
}

// shellQuote 简单 shell 单引号转义。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// marshalPrivateKey 将私钥序列化为 PKCS8 PEM(支持 Ed25519 与 RSA)。
func marshalPrivateKey(priv any) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

// encryptPEMWithPassphrase 用口令加密 PEM(兼容 ssh.ParsePrivateKeyWithPassphrase 解析)。
func encryptPEMWithPassphrase(privPEM []byte, passphrase string) ([]byte, error) {
	block, _ := pem.Decode(privPEM)
	if block == nil {
		return nil, fmt.Errorf("无效的私钥 PEM")
	}
	encBlock, err := x509.EncryptPEMBlock(rand.Reader, block.Type, block.Bytes, []byte(passphrase), x509.PEMCipherAES256)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(encBlock), nil
}

// parsePublicKeyFingerprint 计算公钥指纹(供导出等场景)。
func parsePublicKeyFingerprint(pubPEM []byte) string {
	pub, _, _, _, err := ssh.ParseAuthorizedKey(pubPEM)
	if err != nil {
		return ""
	}
	return ssh.FingerprintSHA256(pub)
}
