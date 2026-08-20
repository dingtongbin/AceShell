package services

import (
	"encoding/json"
	"os"
	"testing"
)

// testServersConfig 测试服务器连接信息(本地文件,不入库)。
// 真实凭据写入 internal/services/testservers.json(gitignore 排除),
// 模板见 testservers.json.example;未配置时外部服务器测试自动跳过。
// 结构定义见 rdpservice.go(testServersConfig)。

// loadTestServers 加载测试服务器配置;配置缺失或未填写真实主机时跳过测试。
func loadTestServers(t *testing.T) *testServersConfig {
	t.Helper()
	cfg := &testServersConfig{}
	data, err := os.ReadFile("testservers.json")
	if err != nil {
		t.Skipf("testservers.json 不存在(复制 testservers.json.example 并填写真实服务器),跳过外部测试: %v", err)
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		t.Fatalf("解析 testservers.json 失败: %v", err)
	}
	if cfg.SSH.Host == "" || cfg.SSH.Password == "" {
		t.Skip("testservers.json 未配置 SSH 测试服务器,跳过外部测试")
	}
	return cfg
}