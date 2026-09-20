// AceShell 插件前端构建契约 —— 所有插件 vite.config 的共享预设(纯数据, 不 import vite,
// 由各插件的 config 自行组合, 保证从插件自己的 node_modules 解析工具链)。
//
// 核心规则: 共享单例依赖由宿主提供(宿主 index.html 的 import map:
// "vue" → /plugins/_host/vue.js), 插件构建一律 external, 严禁打包进产物。
// 同页双 Vue 实例会导致插件组件白屏/响应式失效; 违背契约的裸导入在浏览器
// 得到响亮报错 "Failed to resolve module specifier 'vue'"(加载失败面板可见),
// 这是刻意设计 —— 契约违背必须自诊断, 不允许静默白屏。

/** 宿主经 import map 共享的依赖清单(插件构建必须 external)。 */
export const SHARED_DEPS = ['vue']

/** build.lib 默认值(自包含单文件 entry.js)。 */
export const pluginLibDefaults = {
  formats: ['es'],
  fileName: () => 'entry.js',
}

/** build 其余默认值。 */
export const pluginBuildDefaults = {
  cssCodeSplit: false,
  target: 'es2022',
}
