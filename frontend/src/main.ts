import * as Vue from 'vue'
import { createApp } from 'vue'
import App from './App.vue'
import i18n from './i18n'
import { installDiagProbes, diagLog } from './composables/useDiag'

// 插件共享依赖契约的运行时一侧: 插件 ESM 经 import map("vue" → /plugins/_host/vue.js)
// 落到全局 shim, shim 回读此全局拿到宿主构建内嵌的唯一 Vue 实例。
// 放在模块最前, 保证先于任何插件动态 import(prefetch 在启动后触发)。
;(globalThis as any).__ACESHELL_VUE__ = Vue

// 全局错误/未处理拒绝 → 宿主 plugin-runs.log(生产包无 devtools, 依赖此通道拿现场)
installDiagProbes()

// 右键菜单: 仅终端区域保持拦截(终端右键=粘贴,原生菜单会干扰,useXterm 有兜底通道);
// 其余区域放行 WebView2 系统菜单(复制等),终端容器自身的 contextmenu 处理器不受影响。
document.addEventListener('contextmenu', (e) => {
  const t = e.target as HTMLElement | null
  if (!t?.closest?.('.term-area, .xterm')) e.preventDefault()
})

const app = createApp(App).use(i18n)
// 生产构建中 Vue 把组件错误吞进 console.error 且不重新抛出(window.onerror 收不到),
// 必须经 errorHandler 落盘, 否则渲染类崩溃在生产环境完全不可见。
app.config.errorHandler = (err, _instance, info) => {
  diagLog(`vue-error [${info}] ${err instanceof Error ? (err.stack || err.message) : String(err)}`)
}
app.mount('#app')