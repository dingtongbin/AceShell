//go:build windows

package services

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	crypt32                = syscall.NewLazyDLL("crypt32.dll")
	procCryptProtectData   = crypt32.NewProc("CryptProtectData")
	procCryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
)

// dpapiBlob 对应 DPAPI 的 DATA_BLOB 结构。
type dpapiBlob struct {
	cbData uint32
	pbData *byte
}

// 安全说明:本文件 unsafe 仅用于与 Windows DPAPI(C API)的 FFI 参数传递,
// 指针生命周期严格限制在单次 syscall 调用内,调用返回后不再解引用,
// 不存在逃逸或别名问题。
func blobFromBytes(b []byte) dpapiBlob {
	if len(b) == 0 {
		return dpapiBlob{}
	}
	return dpapiBlob{cbData: uint32(len(b)), pbData: &b[0]}
}

func blobToBytes(b dpapiBlob) []byte {
	out := make([]byte, b.cbData)
	copy(out, unsafe.Slice(b.pbData, b.cbData))
	return out
}

// dpapiProtect 用 Windows DPAPI(当前用户作用域)加密数据。
// 密文仅同一台机器的同一 Windows 用户账户可解,把主密钥真正绑定到
// 本机用户环境,防止 credential.key 被拷贝到其他机器/账户后伪造
// hostname/username 环境变量离线解包。
func dpapiProtect(plaintext []byte) ([]byte, error) {
	if len(plaintext) == 0 {
		return nil, fmt.Errorf("dpapi: 空数据")
	}
	in := blobFromBytes(plaintext)
	var out dpapiBlob
	// 参数:输入blob, 描述(nil), 附加熵(nil), 保留(nil), 提示(nil), flags, 输出blob
	// CRYPTPROTECT_UI_FORBIDDEN(0x1):无 UI 场景必须设置。
	r, _, err := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0, 0, 0, 0, 1,
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, fmt.Errorf("CryptProtectData 失败: %w", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.pbData)))
	return blobToBytes(out), nil
}

// dpapiUnprotect 解开 DPAPI 密文(须同一用户账户)。
func dpapiUnprotect(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) == 0 {
		return nil, fmt.Errorf("dpapi: 空密文")
	}
	in := blobFromBytes(ciphertext)
	var out dpapiBlob
	r, _, err := procCryptUnprotectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0, 0, 0, 0, 1,
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, fmt.Errorf("CryptUnprotectData 失败: %w", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.pbData)))
	return blobToBytes(out), nil
}
