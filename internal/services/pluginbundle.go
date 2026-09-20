package services

import "embed"

// 捆绑插件载荷目录: 构建期由 `wails3 task plugins:build`(或插件 build 脚本)填充,
// 每个插件一个子目录(<id>/{<id>.exe, plugin.json, dist/...}), 随主程序发版内置
// (类似 PyCharm 捆绑插件)。用户可在设置页卸载(记录于 config [plugins].uninstalledBundled,
// 升级后不复活), 也可恢复。
//
// 本目录内容除 README.md 外均不入库(.gitignore); 空目录时 go:embed 仅嵌入 README,
// 构建可正常通过(无捆绑插件)。

//go:embed all:pluginbundle
var pluginBundleFS embed.FS

const pluginBundleRoot = "pluginbundle"
