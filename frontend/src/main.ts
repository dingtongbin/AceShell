import { createApp } from 'vue'
import App from './App.vue'
import i18n from './i18n'

// 右键菜单: 仅终端区域保持拦截(终端右键=粘贴,原生菜单会干扰,useXterm 有兜底通道);
// 其余区域放行 WebView2 系统菜单(复制等),终端容器自身的 contextmenu 处理器不受影响。
document.addEventListener('contextmenu', (e) => {
  const t = e.target as HTMLElement | null
  if (!t?.closest?.('.term-area, .xterm')) e.preventDefault()
})

createApp(App).use(i18n).mount('#app')