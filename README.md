# AceShell

一个界面干净的跨平台网络终端管理器：把 SSH、Telnet、串口、SFTP、HTTP、RDP、本地终端装进同一个窗口，配一套真正可扩展的插件系统。

![License](https://img.shields.io/badge/license-GPL--3.0-blue) ![Release](https://img.shields.io/github/v/release/dingtongbin/AceShell) ![CI](https://github.com/dingtongbin/AceShell/actions/workflows/ci.yml/badge.svg)

**Windows · macOS · Linux** ｜ Go + Wails v3 + Vue 3

## 为什么是 AceShell

- **一个入口，七类会话**：树形会话管理，加密存储，主机指纹 TOFU 校验，密码/密钥登录
- **真·终端体验**：xterm.js 渲染（真色彩、TUI、回滚、选择即复制），拖拽分屏，状态指示，ConPTY/Unix PTY 直连
- **不止是终端**：SFTP 双栏传输与在线编辑、IronRDP 远程桌面全屏会话、脚本管理与连接日志
- **可扩展**：gRPC 插件系统，从 GitHub Release 或本地 zip 一键安装，热重载即时生效
- **安全打底**：会话加密、令牌鉴权、MCP 危险指令拦截、错误可审计

## 功能一览

| 协议 | 能力 |
|---|---|
| SSH / Telnet / HTTP | 标签页终端、状态指示、拖拽排序与分屏 |
| 本地终端 | PowerShell / CMD / Git Bash / WSL 自动扫描 |
| 串口 | 参数持久化 |
| SFTP | 双栏上传下载、断点续传、在线编辑、拖拽 |
| RDP | IronRDP 全屏会话、物理 1:1 缩放 |

配套：脚本编辑器（语法高亮/自动保存）、连接日志（自动记录可检索）、加密导入导出（.as9）、中英双语、深浅色主题/壁纸/面板透明度。

## 插件系统

插件是独立进程，经 gRPC + AutoMTLS 与宿主双向通信；前端产物同源伺服，动态挂载到侧栏与标签页，主题与语言自动跟随。在设置 → 插件页完成安装、启停、重载、卸载。开发文档见 [pluginsdk/README.md](pluginsdk/README.md)。

## 快速开始

从 [Releases](https://github.com/dingtongbin/AceShell/releases) 下载对应平台产物直接运行，或自行构建：

```bash
# 前置：Go 1.25+、Node.js 20+、wails3 CLI
wails3 build   # 生产构建 → bin/
wails3 dev     # 开发模式（前后端热重载）
```

## 测试

```bash
go test ./...                        # 后端单元测试（数据目录隔离）
cd frontend && npx vue-tsc --noEmit  # 前端类型检查
```

## 许可证

[GPL-3.0](LICENSE)。第三方依赖许可见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)，更新日志见 [CHANGELOG.md](CHANGELOG.md)。
