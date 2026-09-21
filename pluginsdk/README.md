# AceShell 插件 SDK (pluginsdk)

AceShell 插件 = 独立进程 + 自包含前端产物, 经 [hashicorp/go-plugin](https://github.com/hashicorp/go-plugin)(gRPC + AutoMTLS) 与宿主双向通信。协议契约: [`proto/aceshell.proto`](../proto/aceshell.proto)。

## 安装与发现

```
%APPDATA%\AceShell\plugins\<插件id>\
  ├─ plugin.json     { "id": "<插件id>", "name": "显示名", "version": "0.1.0", "minHost": "0.2.4",
  │                    "docs": { "default": "docs/README.md", "en-US": "docs/README.en-US.md" } }
  ├─ <插件id>.exe    插件进程(go-plugin 服务端)
  ├─ docs\           可选: 文档目录(plugin.json 的 docs 钩子指向的 md 放这里)
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

**样式标记契约(强制)**: 插件注入的 `<style>` 必须带 `data-aceshell-plugin="<pluginID>"`。
宿主在插件重载/更新/禁用/卸载时按该标记移除样式(重新加载的模块会自动重新注入, 幂等)。

## 文档钩子(docs)与多语言

`plugin.json` 的 `docs` 字段是详情页文档的**显式声明钩子**——值为 locale → 插件目录内
相对路径(md) 的映射, `default` 为无匹配语言时的兜底:

```json
{
  "id": "ping",
  "docs": { "default": "docs/README.md", "en-US": "docs/README.en-US.md" }
}
```

- 解析顺序: **当前界面语言 → `default` → 任一可用项**;宿主不做翻译, 只按标签取文件;
- md 经插件资产服务同源伺服(`/plugins/<id>/<路径>`), 详情页 fetch 后用
  marked + DOMPurify 渲染(代码块高亮, XSS 防护);
- 语言切换即时生效(前端监听 locale 重新拉取);文件缺失时详情页显示降级提示, 不报错;
- 路径是插件目录内相对路径, 宿主资产服务已有防目录穿越清洗, 不要写 `../`。

## 多语言 (i18n)

`Info` 的第二个参数 `locale` 是宿主当前界面语言 (BCP-47, 如 `zh-CN`/`en-US`):

```go
func (p *MyPlugin) Info(ctx context.Context, locale string) (*pluginsdk.PluginInfo, error) {
    name, title := "我的插件", "我的插件"
    if strings.HasPrefix(locale, "en") {
        name, title = "My Plugin", "My Plugin"
    }
    ...
}
```

- 用户切换界面语言时宿主调用 `OnLocaleChanged(ctx, locale)` 通知所有运行中插件
  (旧版插件未实现则静默忽略), 随后以新 locale 重新拉取 `Info` 并刷新注册表 ——
  显示名/视图标题即时本地化, 插件自身只需在 `Info` 里按 locale 返回文案;
- 历史等业务数据的语言由插件自行处理 (参考 ping 的多语言文档钩子)。

## 能力声明 (capabilities)

`PluginInfo.Capabilities` 是插件的可选能力标签 (如 `"net"`/`"fs"`/`"terminal"`),
会显示在插件详情页, 并为宿主未来按能力门控 RPC 预留:

```go
Capabilities: []string{"net"},
```

## 自定义渲染逃逸舱 (React / Svelte / 原生 DOM)

默认契约是导出 Vue 组件。若插件想用其他框架或直接操作 DOM, 导出
`{ __aceshellCustom: true, mount }` 形态即可 —— 宿主以薄壳 Vue 组件承载容器,
渲染与卸载完全交给插件:

```js
// entry.js (React 示例)
import { createElement } from 'react'
import { createRoot } from 'react-dom/client'
export default {
  components: {
    tool: {
      __aceshellCustom: true,
      mount(el, props) {                    // el: 宿主提供的容器(100%×100%)
        const root = createRoot(el)
        root.render(createElement(App, props)) // props 含 pluginID/ctx/...
        return () => root.unmount()           // 返回 dispose(可选), 宿主卸载时回调
      },
    },
  },
}
```

约定: `mount` 必须同步返回; 返回的 dispose 函数在宿主卸载组件时调用 (退订事件等);
容器尺寸为宿主给定的 100%×100%, 不要自行改写父级样式。

## 热重载与失效协议

宿主在插件**重载/更新/禁用/卸载**时会向前端广播 `plugin-invalidated` 事件并递增该插件的
**代次(epoch)**。前端据此: 清空组件/ctx/订阅缓存 → 用新模块(URL 携带 `?v=<version>-<epoch>`
cache-bust)重建面板与标签页(标签页就地重建, 保留标题与状态) → 移除插件注入的样式。

对插件作者的含义:

- **重载 = 热更新**: 设置页点「重载」后, 无需重启宿主, 面板与标签页即刻换用新前端产物;
  开发时替换插件目录(`build.ps1` 的 deploy 即是)后点重载, 后端二进制与前端产物一并换代;
- 组件的 `ctx` 引用在失效后会更换, **不要**在模块顶层长期持有旧 `ctx`/旧 DOM 引用;
  在 `onUnmounted`/`unmount` 中退订 `ctx.onEvent`, 否则订阅泄漏;
- 卸载与禁用不保留界面: 面板关闭、标签页关闭、样式移除。

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
