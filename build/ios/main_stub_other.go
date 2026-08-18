//go:build !ios

package main

// 非 iOS 平台构建占位入口，使 go build ./... 可对 build/ios 包进行编译检查。
// iOS 构建时本文件被排除，由 main_ios.go 提供 WailsIOSMain 导出入口。
func main() {}
