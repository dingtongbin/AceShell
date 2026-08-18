package services

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// 本机主密钥体系:所有平台统一安全加密(不依赖平台 API)。
// 主密钥为 32 字节随机数,落盘前用机器派生密钥(hostname+用户标识+固定 salt)做 AES-GCM 包裹加密,
// 任何平台均不出现明文密钥材料。密钥文件丢失/损坏时自动重建新密钥,
// 历史密文无法解密,读取侧按"无密码"处理,不崩溃。

const (
	masterKeyFileName = "credential.key"
	masterKeyPrefix   = "mk:v1:"
	secretPrefix      = "enc:v1:"
	masterKeySize     = 32
)

var (
	masterKeyMu       sync.Mutex
	masterKeyCache    []byte
	masterKeyCacheDir string
)

// loadMasterKey 加载或创建本机主密钥(进程内缓存,按数据目录隔离)。
func loadMasterKey() ([]byte, error) {
	masterKeyMu.Lock()
	defer masterKeyMu.Unlock()

	path := filepath.Join(DataDir(), masterKeyFileName)
	if masterKeyCache != nil && masterKeyCacheDir == path {
		return masterKeyCache, nil
	}

	if data, err := os.ReadFile(path); err == nil {
		key, err := unwrapMasterKey(data)
		if err == nil && len(key) == masterKeySize {
			masterKeyCacheDir = path
			masterKeyCache = key
			return key, nil
		}
	}

	newKey := make([]byte, masterKeySize)
	if _, err := io.ReadFull(rand.Reader, newKey); err != nil {
		return nil, fmt.Errorf("生成主密钥失败: %w", err)
	}
	blob, err := wrapMasterKey(newKey)
	if err != nil {
		return nil, err
	}
	os.MkdirAll(DataDir(), 0755)
	if err := os.WriteFile(path, blob, 0600); err != nil {
		return nil, fmt.Errorf("写入主密钥文件失败: %w", err)
	}
	masterKeyCacheDir = path
	masterKeyCache = newKey
	return newKey, nil
}

// wrapMasterKey 用机器派生密钥包裹加密主密钥。
func wrapMasterKey(key []byte) ([]byte, error) {
	blob, err := sealWithKey(key, deriveMachineKey())
	if err != nil {
		return nil, err
	}
	return []byte(masterKeyPrefix + base64.StdEncoding.EncodeToString(blob)), nil
}

// unwrapMasterKey 用机器派生密钥解开主密钥。
func unwrapMasterKey(data []byte) ([]byte, error) {
	s := string(data)
	if !strings.HasPrefix(s, masterKeyPrefix) {
		return nil, fmt.Errorf("无效的主密钥文件格式")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, masterKeyPrefix))
	if err != nil {
		return nil, err
	}
	return openWithKey(raw, deriveMachineKey())
}

// sealWithKey 用指定密钥做 AES-256-GCM 加密,返回 nonce|ciphertext|tag。
func sealWithKey(plaintext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return aesGCM.Seal(nonce, nonce, plaintext, nil), nil
}

// openWithKey 用指定密钥解密 AES-256-GCM 密文(nonce|ciphertext|tag)。
func openWithKey(data, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := aesGCM.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("密文数据过短")
	}
	nonce, body := data[:nonceSize], data[nonceSize:]
	return aesGCM.Open(nil, nonce, body, nil)
}

// encryptSecret 用主密钥加密敏感字段;空值原样返回。
func encryptSecret(plaintext string) (string, error) {
	if plaintext == "" {
		return plaintext, nil
	}
	key, err := loadMasterKey()
	if err != nil {
		return "", err
	}
	blob, err := sealWithKey([]byte(plaintext), key)
	if err != nil {
		return "", err
	}
	return secretPrefix + base64.StdEncoding.EncodeToString(blob), nil
}

// decryptSecret 解密敏感字段。
// 兼容顺序:主密钥(enc:v1) → 当前机器派生密钥(旧格式裸密文) → 历史 KDF salt 机器密钥。
func decryptSecret(stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	if strings.HasPrefix(stored, secretPrefix) {
		key, err := loadMasterKey()
		if err != nil {
			return "", err
		}
		raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, secretPrefix))
		if err != nil {
			return "", err
		}
		pt, err := openWithKey(raw, key)
		if err != nil {
			return "", fmt.Errorf("无法解密敏感字段")
		}
		return string(pt), nil
	}

	raw, err := base64.StdEncoding.DecodeString(stored)
	if err != nil {
		return "", err
	}
	for _, key := range machineKeyCandidates() {
		if pt, err := openWithKey(raw, key); err == nil {
			return string(pt), nil
		}
	}
	return "", fmt.Errorf("无法解密敏感字段")
}

// machineKeyCandidates 返回当前及历史机器派生密钥(旧版会话文件兼容)。
func machineKeyCandidates() [][]byte {
	candidates := [][]byte{deriveMachineKey()}
	for _, salt := range legacyMachineKeySalts {
		candidates = append(candidates, deriveMachineKeyWithSalt(salt))
	}
	return candidates
}
