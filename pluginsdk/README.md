# AceShell 插件 SDK (pluginsdk)

AceShell 插件 = 独立进程 + 自包含前端产物, 经 [hashicorp/go-plugin](https://github.com/hashicorp/go-plugin)(gRPC + AutoMTLS) 与宿主双向通信。协议契约: [`proto/aceshell.proto`](../proto/aceshell.proto)。

## 安装与发现

```
%APPDATA%\AceShell\plugins\<插件id>\
  ├─ plugin.json     { "id": "<插件id>", "name": "显示名", "version": "0.1.0", "minHost": "0.2.4" }
  ├─ <插件id>.exe    插件进程(go-plugin 服务端)
  └─ dist\
      └─ entry.js    前端模块(ESM, default 导出 { components: {...} })
```

- 安装方式: 手动放置, 或设置-插件页输入 `owner/repo` 从 GitHub Release 拉取
  (资产命名约定: `aceshell-<id>-windows-amd64.zip`)。
- 前端产物由宿主**同源**伺服 `/plugins/<id>/dist/...`(dev 环境经 Vite 代理到宿主 8941 端口),
  宿主动态 `import()` 后直挂侧栏面板与标签页, CSS 变量/暗色/强调色自动联动。

## 捆绑发布(发版内置插件)

主程序支持把插件嵌入二进制随版本发布(类似 PyCharm bundled plugins), 用户可在
设置-插件页卸载(记录持久化, 应用升级不复活)或随时恢复:

1. 插件载荷放入 `internal/services/pluginbundle/<id>/`(`pluginsdk/ping/build.ps1` 会自动完成);
2. `wails3 task build` 已依赖 `plugins:build` 任务, 构建时自动嵌入;
3. 启动时宿主自动落盘: 目录缺失即复制, 磁盘版本旧于内置版本即升级(用户卸载过的跳过)。

## Go 插件最小实现

```go
package main

import (
	"context"

	pluginsdk "changeme/pluginsdk"
)

type myPlugin struct{}

func (m *myPlugin) Info(ctx context.Context) (*pluginsdk.PluginInfo, error) {
	return &pluginsdk.PluginInfo{
		ID: "hello", DisplayName: "Hello", Version: "0.1.0",
		Views: []pluginsdk.ViewInfo{{ID: "main", Title: "Hello", ComponentID: "panel"}},
	}, nil
}
func (m *myPlugin) Start(ctx context.Context, h *pluginsdk.HostContext) error  { return nil }
func (m *myPlugin) Shutdown(ctx context.Context) error                         { return nil }
func (m *myPlugin) OnViewVisible(ctx context.Context, viewID string) error     { return nil }
func (m *myPlugin) OnViewHidden(ctx context.Context, viewID string) error      { return nil }
func (m *myPlugin) OnTabEvent(ctx context.Context, ev *pluginsdk.TabEvent) error { return nil }
func (m *myPlugin) Rpc(ctx context.Context, method, argsJSON string) (string, error) {
	return "{}", nil
}

func main() { pluginsdk.Serve(&myPlugin{}) }
```

完整真实示例(流式探测/EmitUIEvent/HostClient/生命周期): [`ping/`](ping/) ——
AceShell 自带的 Ping 工具插件, 也是捆绑发布的参考实现。

## 前端契约

`dist/entry.js`(自包含 ESM; **`vue` 必须 external**, 见下):

```ts
export default {
  components: {
    panel: PanelComponent, // ViewInfo.componentId → 侧栏面板
    tab: TabComponent,     // TabSpec.componentId → 插件标签页
  },
}
```

宿主给组件注入的 props:

| prop | 说明 |
|---|---|
| `ctx` | `{ pluginID, call(method, args), openTab(spec), toast(msg, level), onEvent(cb), theme: { isDark, accent } }` |
| `view` / 组件标签页 | 标签页为 `propsJson` 合并 `pluginID`/`tabKey`/`ctx` |

`ctx.onEvent(cb)` 订阅插件 Go 侧经 `HostClient.EmitUIEvent(payloadJSON)` 推送的流式事件
(逐包探测结果、进度、日志尾随等), 返回退订函数; 卸载组件时务必退订。

样式建议: 选择器统一加插件前缀, 经 `style.css?inline` 注入(见示例), 颜色一律用宿主 CSS 变量
(`--primary-color`/`--text-color`/`--border-color` 等)以跟随主题。

### 共享依赖契约(必须遵守)

宿主页面只有**一份 Vue 实例**, 经 `index.html` 的 import map 共享:
`"vue"` → `/plugins/_host/vue.js`(宿主构建时从其安装的 vue 自动生成导出面)。

- 插件构建必须 `rollupOptions.external: SHARED_DEPS`(清单见 [`vite-preset.mjs`](vite-preset.mjs)),
  运行时浏览器自动把裸导入 `"vue"` 解析到宿主实例;
- **严禁把 Vue 打进插件产物** —— 同页双实例会导致组件白屏、响应式失效等诡异问题;
- 忘记 external 不会静默: 浏览器对裸导入报 `Failed to resolve module specifier 'vue'`,
  面板显示加载失败与该原因, 按此排查即可;
- 未来宿主扩充共享清单(如 dayjs 等)只需在 import map 与 `SHARED_DEPS` 两侧同步添加。

## 非 Go 语言插件

go-plugin 的 gRPC 协议是跨语言的, 官方 SDK 仅提供 Go。其他语言需自行实现:

1. 启动时读取环境变量 `ACESHELL_PLUGIN_COOKIE`(go-plugin 以 magic cookie 防误启动),
   值必须等于 `aceshell-plugin-v1-handshake`;
2. 在 stdout 打印握手行 `<1|1|tcp|127.0.0.1:<port>|grpc>` 后进入服务;
   若环境变量 `PLUGIN_CLIENT_CERT` 存在(AutoMTLS), gRPC Server 须以该 PEM 证书做 mTLS;
3. 以 gRPC 实现 `aceshell.plugin.v1.AcePlugin` 服务 + 标准 `grpc.health.v1.Health`;
4. 调用宿主能力: 连接 `Start` 下发的 `host_endpoint`, 每次 RPC 携带 metadata
   `x-aceshell-token: <Start 下发的 token>`。

## 重新生成协议代码(仅 proto 变更后)

```
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
go install github.com/bufbuild/buf/cmd/buf@latest
buf generate proto
```

生成物已提交入库, 构建 AceShell 与插件无需 protoc。
