//go:build !windows

package services

import "fmt"

var errDPAPIUnavailable = fmt.Errorf("DPAPI 不可用")

// dpapiProtect/dpapiUnprotect 非 Windows 平台不可用,
// 主密钥包裹回退到机器派生密钥方案(见 credentialkey.go 的 mk:v1 格式)。
func dpapiProtect([]byte) ([]byte, error)       { return nil, errDPAPIUnavailable }
func dpapiUnprotect([]byte) ([]byte, error)     { return nil, errDPAPIUnavailable }
