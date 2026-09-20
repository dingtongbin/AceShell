<script setup lang="ts">
import { ref, provide, onMounted, watchEffect, watch, computed } from 'vue'
import { NConfigProvider, NMessageProvider, NDialogProvider, zhCN, enUS } from 'naive-ui'
import ShellPanel from './components/ShellPanel.vue'
import SettingsDialog from './components/SettingsDialog.vue'
import { useTheme, applyThemeVars } from './stores/theme'
import { GetConfig, GetWallpaperData } from '../bindings/changeme/internal/services/configservice.js'
import { ApplyTitleBarTheme } from '../bindings/changeme/internal/services/windowservice.js'
import { warmupRdpRuntime } from './composables/useRdp'
import i18n, { setLocale, currentLocale } from './i18n'

const { isDark, theme, themeOverrides, accent, initTheme, setAccent } = useTheme()
const showSettings = ref(false)
const panelOpacity = ref(100)
const wallpaper = ref('')

const naiveLocale = computed(() => (currentLocale() === 'en-US' ? enUS : zhCN))

function handleOpenSettings() {
  showSettings.value = true
}

function handleCloseSettings() {
  showSettings.value = false
}

async function applyWallpaperStyle() {
  const el = document.body
  if (wallpaper.value) {
    try {
      const dataUrl = await GetWallpaperData()
      if (dataUrl) {
        el.style.backgroundImage = `url("${dataUrl}")`
        el.style.backgroundSize = 'cover'
        el.style.backgroundPosition = 'center'
        el.style.backgroundRepeat = 'no-repeat'
        return
      }
    } catch {
      // 壁纸读取失败时回退默认背景
    }
  }
  el.style.backgroundImage = 'none'
}

onMounted(async () => {
  try {
    const cfg = JSON.parse(await GetConfig())
    initTheme(cfg.view?.theme ?? 'dark')
    if (cfg.view?.accentColor && /^#[0-9a-fA-F]{6}$/.test(cfg.view.accentColor)) {
      setAccent(cfg.view.accentColor)
    }
    panelOpacity.value = cfg.view?.panelOpacity ?? 100
    wallpaper.value = cfg.view?.wallpaper || ''
    setLocale(cfg.language ?? 'zh-CN')
  } catch {
    initTheme('dark')
    setLocale('zh-CN')
  }
  await applyWallpaperStyle()
  window.addEventListener('config-changed', onConfigChanged)
  warmupRdpRuntime()
})

async function onConfigChanged() {
  try {
    const cfg = JSON.parse(await GetConfig())
    if (cfg.view?.accentColor && /^#[0-9a-fA-F]{6}$/.test(cfg.view.accentColor)) {
      setAccent(cfg.view.accentColor)
    }
    panelOpacity.value = cfg.view?.panelOpacity ?? 100
    wallpaper.value = cfg.view?.wallpaper || ''
    setLocale(cfg.language ?? 'zh-CN')
  } catch {
    // 忽略配置读取失败
  }
}

watch(wallpaper, () => { applyWallpaperStyle() })

watch(isDark, (dark) => {
  ApplyTitleBarTheme(dark).catch(() => {})
})

// 全局令牌应用: 深浅表面层 + 强调色派生 + 语义色(单一来源 stores/tokens.ts)
watchEffect(() => {
  document.documentElement.classList.toggle('dark', isDark.value)
  applyThemeVars(isDark.value, panelOpacity.value, accent.value)
})
</script>

<template>
  <n-config-provider :theme="theme" :theme-overrides="themeOverrides" :locale="naiveLocale">
    <n-message-provider>
      <n-dialog-provider>
        <div class="app-root">
          <ShellPanel @open-settings="handleOpenSettings" />
          <SettingsDialog :show="showSettings" @close="handleCloseSettings" />
        </div>
      </n-dialog-provider>
    </n-message-provider>
  </n-config-provider>
</template>

<style>
*,
*::before,
*::after {
  margin: 0;
  padding: 0;
  box-sizing: border-box;
}

html {
  overflow: hidden !important;
}

body {
  width: 100vw;
  height: 100vh;
  overflow: hidden !important;
  background: var(--body-bg);
  color: var(--text-color);
}

::-webkit-scrollbar {
  display: none !important;
}

* {
  scrollbar-width: none !important;
  -ms-overflow-style: none !important;
}

#app {
  width: 100vw;
  height: 100vh;
  overflow: hidden !important;
}

/* 资源管理器 & 串口管理器通用折叠区样式 */
.section-header {
  display: flex;
  align-items: center;
  gap: 4px;
  height: 28px;
  min-height: 28px;
  padding: 0 8px;
  cursor: pointer;
  user-select: none;
  background: rgba(255,255,255,0.03);
  border-bottom: 1px solid var(--sidebar-shadow);
  transition: background 0.15s;
}
.section-header:hover { background: rgba(255,255,255,0.06); }
.section-arrow { color: #888; transition: transform 0.15s ease; flex-shrink: 0; }
.section-arrow.rotated { transform: rotate(90deg); }
.section-icon { color: #888; }
.section-label { font-size: 12px; font-weight: 600; color: var(--text-color); text-transform: uppercase; letter-spacing: 0.5px; }
.section-actions { margin-left: auto; display: flex; gap: 2px; }
.sm-field {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.sm-label {
  font-size: 11px;
  color: var(--text-color);
  opacity: 0.7;
}
.sm-row-inline {
  display: flex;
  gap: 6px;
  align-items: center;
}

/* 终端背景透明化，跟随 --term-bg 透明度设置（覆盖 xterm.css 硬编码黑底） */
.xterm,
.xterm-viewport,
.xterm-screen,
.xterm .xterm-viewport {
  background-color: transparent !important;
}

/* 深色模式: 结构区域边界不画线(表面明度差已足够分区,消除亮色细线);
   亮色模式保留标准细线。全局 border-box 下,固定高度容器移除边线无布局跳动。 */
html.dark .top-menu-bar,
html.dark .section-header,
html.dark .resource-header,
html.dark .resource-tabs,
html.dark .section-wrapper,
html.dark .mcp-section,
html.dark .mcp-log-toolbar,
html.dark .mcp-log-item {
  border-bottom: none;
  border-top: none;
}

/* 非阻塞提示浮层(message/notification)置于自绘标题栏之上,避免被标题栏遮挡;
   弹窗/抽屉等阻塞型遮罩保持低于标题栏(标题栏 z-index: 1000000) */
.n-message-container,
.n-notification-container {
  z-index: 1000001 !important;
}
.n-message-container {
  top: 44px !important;
}
</style>

<style scoped>
.app-root {
  position: fixed;
  left: 0;
  top: 0;
  right: 0;
  bottom: 0;
  overflow: hidden;
}
</style>
