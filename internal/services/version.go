package services

// AppVersion 应用版本号。
// 构建时可注入覆盖,例如:
//
//	go build -ldflags "-X changeme/internal/services.AppVersion=1.2.3"
var AppVersion = "0.1.0"

// VersionService 提供应用版本信息。
type VersionService struct{}

// GetVersion 返回应用版本号。
func (v *VersionService) GetVersion() string {
	return AppVersion
}
