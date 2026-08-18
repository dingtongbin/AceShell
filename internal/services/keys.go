package services

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"

	"golang.org/x/crypto/ssh"
)

// parsePrivateKey 解析 PEM 私钥为 ssh.Signer。
// 优先使用 x/crypto/ssh 原生解析(OpenSSH 格式、PKCS#1、EC、Ed25519);
// 对 PKCS#8("PRIVATE KEY" 头)回退到 x509 解析,兼容无加密与传统
// Proc-Type 加密的 PKCS#8 密钥;PBES2 加密的 PKCS#8 无法用标准库解密,
// 返回明确错误提示。
func parsePrivateKey(pemBytes, passphrase []byte) (ssh.Signer, error) {
	if len(passphrase) == 0 {
		signer, err := ssh.ParsePrivateKey(pemBytes)
		if err == nil {
			return signer, nil
		}
		return parsePKCS8PrivateKey(pemBytes, nil, err)
	}
	signer, err := ssh.ParsePrivateKeyWithPassphrase(pemBytes, passphrase)
	if err == nil {
		return signer, nil
	}
	return parsePKCS8PrivateKey(pemBytes, passphrase, err)
}

// parsePKCS8PrivateKey 尝试按 PKCS#8 解析;非 PKCS#8 时返回原错误。
func parsePKCS8PrivateKey(pemBytes, passphrase []byte, origErr error) (ssh.Signer, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, origErr
	}
	der := block.Bytes
	if len(passphrase) > 0 {
		if x509.IsEncryptedPEMBlock(block) {
			dec, err := x509.DecryptPEMBlock(block, passphrase)
			if err != nil {
				return nil, fmt.Errorf("私钥口令错误: %w", err)
			}
			der = dec
		} else if isEncryptedPKCS8(der) {
			return nil, fmt.Errorf("不支持 PBES2 加密的 PKCS#8 私钥,请用 ssh-keygen 转换为 OpenSSH 格式后重新导入")
		}
	}
	key, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("PKCS#8 私钥解析失败: %w", err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		return nil, fmt.Errorf("私钥转 Signer 失败: %w", err)
	}
	return signer, nil
}

// isEncryptedPKCS8 检测 PKCS#8 DER 是否为 PBES2 加密(ASN.1 序列内含 PBES2 OID)。
func isEncryptedPKCS8(der []byte) bool {
	// PKCS#8 加密结构: SEQUENCE { SEQUENCE { OID 1.2.840.113549.1.5.13 (PBES2), ... }, OCTET STRING }
	if len(der) < 16 {
		return false
	}
	for i := 0; i+11 <= len(der); i++ {
		if der[i] == 0x06 && der[i+1] == 0x09 && der[i+2] == 0x2a && der[i+3] == 0x86 &&
			der[i+4] == 0x48 && der[i+5] == 0x86 && der[i+6] == 0xf7 && der[i+7] == 0x0d &&
			der[i+8] == 0x01 && der[i+9] == 0x05 && der[i+10] == 0x0d {
			return true
		}
	}
	return false
}