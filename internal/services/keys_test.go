package services

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func generateTestRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	return key
}

func TestParsePrivateKey_OpenSSHFormat(t *testing.T) {
	key := generateTestRSAKey(t)
	sshPEM := pem.EncodeToMemory(&pem.Block{Type: "OPENSSH PRIVATE KEY", Bytes: []byte("dummy")})
	t.Run("openssh-without-passphrase", func(t *testing.T) {
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
		signer, err := parsePrivateKey(keyPEM, nil)
		if err != nil {
			t.Fatalf("parsePrivateKey(PKCS1) failed: %v", err)
		}
		if signer == nil {
			t.Fatal("expected non-nil signer")
		}
	})
	t.Run("openssh-garbage", func(t *testing.T) {
		_, err := parsePrivateKey(sshPEM, nil)
		if err == nil {
			t.Fatal("expected error for garbage OpenSSH key")
		}
	})
}

func TestParsePrivateKey_PKCS8(t *testing.T) {
	key := generateTestRSAKey(t)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey failed: %v", err)
	}
	pkcs8PEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	t.Run("plain-pkcs8", func(t *testing.T) {
		signer, err := parsePrivateKey(pkcs8PEM, nil)
		if err != nil {
			t.Fatalf("parsePrivateKey(pkcs8) failed: %v", err)
		}
		if signer == nil {
			t.Fatal("expected non-nil signer")
		}
	})

	t.Run("plain-pkcs8-with-empty-passphrase", func(t *testing.T) {
		signer, err := parsePrivateKey(pkcs8PEM, []byte{})
		if err != nil {
			t.Fatalf("parsePrivateKey(pkcs8, empty) failed: %v", err)
		}
		if signer == nil {
			t.Fatal("expected non-nil signer")
		}
	})

	t.Run("legacy-encrypted-pkcs8", func(t *testing.T) {
		encBlock, err := x509.EncryptPEMBlock(rand.Reader, "PRIVATE KEY", der, []byte("secret"), x509.PEMCipherAES256)
		if err != nil {
			t.Fatalf("EncryptPEMBlock failed: %v", err)
		}
		encPEM := pem.EncodeToMemory(encBlock)
		signer, err := parsePrivateKey(encPEM, []byte("secret"))
		if err != nil {
			t.Fatalf("parsePrivateKey(encrypted pkcs8) failed: %v", err)
		}
		if signer == nil {
			t.Fatal("expected non-nil signer")
		}
	})

	t.Run("legacy-encrypted-wrong-passphrase", func(t *testing.T) {
		encBlock, err := x509.EncryptPEMBlock(rand.Reader, "PRIVATE KEY", der, []byte("secret"), x509.PEMCipherAES256)
		if err != nil {
			t.Fatalf("EncryptPEMBlock failed: %v", err)
		}
		_, err = parsePrivateKey(pem.EncodeToMemory(encBlock), []byte("wrong"))
		if err == nil || !strings.Contains(err.Error(), "口令错误") {
			t.Fatalf("expected wrong-passphrase error, got: %v", err)
		}
	})
}

func TestParsePrivateKey_NotPrivateKeyFallsBack(t *testing.T) {
	key := generateTestRSAKey(t)
	invalidPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	_, err := parsePrivateKey(invalidPEM, nil)
	if err == nil {
		t.Fatal("expected error for non-key PEM")
	}
}

func TestIsEncryptedPKCS8(t *testing.T) {
	pbes2DER := []byte{0x30, 0x1e, 0x30, 0x0b, 0x06, 0x09, 0x2a, 0x86, 0x48, 0x86, 0xf7, 0x0d, 0x01, 0x05, 0x0d, 0x04, 0x0f}
	if !isEncryptedPKCS8(pbes2DER) {
		t.Fatal("expected PBES2 OID detection to succeed")
	}
	if isEncryptedPKCS8([]byte{0x30, 0x02, 0x01, 0x00}) {
		t.Fatal("expected short/plain DER not to be detected as PBES2")
	}
}

func TestSignerRoundTrip(t *testing.T) {
	key := generateTestRSAKey(t)
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	pkcs8PEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	signer, err := parsePrivateKey(pkcs8PEM, nil)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if signer.PublicKey().Type() != ssh.KeyAlgoRSA {
		t.Fatalf("expected RSA key algo, got %s", signer.PublicKey().Type())
	}
}