# AceShell

AceShell 是一个跨平台（Windows / macOS / Linux）网络终端管理工具，基于 **Go + Wails v3 + Vue 3 + TypeScript + Naive UI** 构建，支持 SSH / Telnet / 串口 / SFTP / HTTP / RDP / 本地终端的统一管理，并内置 AI 智能助手。

## 功能特性

- **会话管理**：七类会话树形组织、加密存储、主机指纹 TOFU 校验、密码与密钥登录、串口参数持久化
- **本地终端**：跨平台 PTY（Windows ConPTY / Unix PTY）驱动 PowerShell / CMD / Git Bash / WSL，自动扫描本机可用终端，数据流直连 xterm.js
- **智能助手**（测试功能，默认开启，可在视图菜单或设置中手动关闭）：内置 AI 运维智能体——深度思考先行、MCP 工具总线（Streamable HTTP / SSE / stdio / 内置）、联网搜索（必应 → 真实浏览器渲染 → DuckDuckGo 三级回退，零 API Key）、Context7 文档检索预置、计划/手动/自动三档权限、执行期间标签页遮罩防误操作；操作折叠行完整可追溯（命令/查询参数/结果全量展示），对话历史持久化可回溯
- **标签页终端**：状态指示（连接中/成功/失败/超时）、溢出滚动、拖拽排序与拖拽分屏（四象限特效）、关闭确认
- **终端渲染**（xterm.js）：真色彩、TUI 全屏程序、超链接、emoji 宽字符、回滚缓冲、选择即复制、多行粘贴确认
- **远程桌面**（RDP）：IronRDP 全屏图形会话、鼠标键盘交互、物理 1:1 显示（按设备像素比缩放，黑边保留）、脏区域缓冲合并绘制
- **SFTP**：本地/远端双栏、上传下载、断点续传、应用内在线编辑（文本白名单 + UTF-8 校验 + 行号）、右键菜单、系统文件拖拽
- **脚本与文件编辑**：树形管理、标签页编辑器（行号 + 语法高亮 + Ctrl+S + 固定间隔自动保存）、未保存关闭确认、标签页状态指示
- **导入导出**：加密导出包（.as9）、树形多选、导入二次确认、sessions 目录导入锁、密钥文件自动分离到本机密钥库
- **连接日志**：自动记录输入输出（元数据含协议/主机/端口/起止时间），树形浏览、大日志尾部读取；AI 可经 MCP 工具检索与查看详情
- **国际化**：内置简体中文（默认）与英文，设置中一键切换并持久化，文案即时生效，可扩展新语言
- **外观**：深色 / 浅色 / 跟随系统主题、面板透明度、壁纸、终端个性化（字体/颜色/光标/背景）
- **状态栏**：协议与登录用户、地址端口、文件类型、编码、光标行列

## 存储目录

应用数据存储在**平台应用数据目录**（不依赖可执行文件位置）：

| 平台 | 数据目录 |
|---|---|
| Windows | `%AppData%\AceShell\` |
| macOS | `~/Library/Application Support/AceShell/` |
| Linux | `~/.config/aceshell/` |

目录结构：

```
<数据目录>/
├── config.toml       # 全局配置（主题/透明度/开关等）
├── agent-mcp.json    # 智能体外接 MCP 服务器配置（兼容 Claude/Cursor 字段惯例）
├── sessions/         # 会话目录（.toml 会话文件 + 指纹 + 密钥库 key/）
├── autolog/          # 自动连接日志（logs/ 日志正文 + meta/ 会话元数据）
└── script/           # 脚本目录
```

## 构建

前置要求：Go 1.25+、Node.js 20+、[wails3 CLI](https://v3.wails.io)（`go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.3`）。

```bash
# 生产构建（输出 bin/AceShell）
wails3 build

# 开发模式（前后端热重载）
wails3 dev
```

### Windows 发布

打标签自动触发 GitHub Actions 构建三平台产物并创建 Release：

```bash
git tag v0.2.0
git push --tags
```

CI 产物：Windows / Linux / macOS 二进制 + 未签名 MSIX（`AceShell-<版本>.msix`，供微软商店提交，商店侧自动重签）。

本地生成 MSIX：

```powershell
powershell -ExecutionPolicy Bypass -File build\windows\msix\build-msix.ps1 -Version 0.2.0
```

> 上架微软商店前，需将 `build/windows/msix/app_manifest.xml` 中的 `Publisher` 与包名替换为商店后台分配的标识。

## 测试

```bash
# 后端单元测试（数据目录隔离，不触碰真实数据）
go test ./...

# 前端类型检查
cd frontend && npx vue-tsc --noEmit
```

部分测试（SSH / Telnet 真实连接）依赖外部服务器，凭据不提交仓库：

1. 复制模板：`cp internal/services/testservers.json.example internal/services/testservers.json`
2. 填入真实服务器信息（该文件已被 `.gitignore` 排除）
3. 未配置时相关测试自动跳过，不影响 CI

## 许可证

[GPL-3.0](LICENSE)。第三方依赖许可见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。

更新日志见 [CHANGELOG.md](CHANGELOG.md)。
