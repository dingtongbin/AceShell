<script setup lang="ts">
import { ref, provide, onMounted, watchEffect, watch } from 'vue'
import { NConfigProvider, NMessageProvider } from 'naive-ui'
import ShellPanel from './components/ShellPanel.vue'
import SettingsDialog from './components/SettingsDialog.vue'
import { useTheme } from './stores/theme'
import { GetConfig, GetWallpaperData } from '../bindings/changeme/internal/services/configservice.js'
import { ApplyTitleBarTheme } from '../bindings/changeme/internal/services/windowservice.js'
import { warmupRdpRuntime } from './composables/useRdp'

const { isDark, theme, themeOverrides, initTheme } = useTheme()
const showSettings = ref(false)
const panelOpacity = ref(100)
const wallpaper = ref('')

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
    panelOpacity.value = cfg.view?.panelOpacity ?? 100
    wallpaper.value = cfg.view?.wallpaper || ''
  } catch {
    initTheme('dark')
  }
  await applyWallpaperStyle()
  window.addEventListener('config-changed', onConfigChanged)
  warmupRdpRuntime()
})

async function onConfigChanged() {
  try {
    const cfg = JSON.parse(await GetConfig())
    panelOpacity.value = cfg.view?.panelOpacity ?? 100
    wallpaper.value = cfg.view?.wallpaper || ''
  } catch {
    // 忽略配置读取失败
  }
}

watch(wallpaper, () => { applyWallpaperStyle() })

watch(isDark, (dark) => {
  ApplyTitleBarTheme(dark).catch(() => {})
})

watchEffect(() => {
  document.documentElement.classList.toggle('dark', isDark.value)
  const d = document.documentElement.style
  const a = Math.min(100, Math.max(30, panelOpacity.value)) / 100
  if (isDark.value) {
    d.setProperty('--sidebar-bg', `rgba(32,32,32,${a})`)
    d.setProperty('--sidebar-shadow', '#3c3c3c')
    d.setProperty('--body-bg', `rgba(38,38,38,${a})`)
    d.setProperty('--text-color', '#d4d4d4')
    d.setProperty('--icon-color', '#6e6e6e')
    d.setProperty('--icon-hover', '#c5c5c5')
    d.setProperty('--toolbar-bg', `rgba(52,52,52,${a})`)
    d.setProperty('--card-bg', `rgba(44,44,44,${a})`)
    d.setProperty('--border-color', '#3c3c3c')
    d.setProperty('--active-color', '#ffffff')
    d.setProperty('--hover-bg', 'rgba(255,255,255,0.05)')
    d.setProperty('--close-hover-bg', 'rgba(255,255,255,0.1)')
    d.setProperty('--tab-active-bg', `rgba(22,22,22,${a})`)
    d.setProperty('--tab-inactive-bg', `rgba(44,44,44,${a})`)
    d.setProperty('--term-bg', `rgba(22,22,22,${a})`)
  } else {
    d.setProperty('--sidebar-bg', '#efefef')
    d.setProperty('--sidebar-shadow', '#d9d9d9')
    d.setProperty('--body-bg', '#f7f7f7')
    d.setProperty('--text-color', '#1a1a1a')
    d.setProperty('--icon-color', '#999999')
    d.setProperty('--icon-hover', '#555555')
    d.setProperty('--toolbar-bg', '#e1e1e1')
    d.setProperty('--card-bg', '#ffffff')
    d.setProperty('--border-color', '#e0e0e0')
    d.setProperty('--active-color', '#000000')
    d.setProperty('--hover-bg', 'rgba(0,0,0,0.03)')
    d.setProperty('--close-hover-bg', 'rgba(0,0,0,0.06)')
    d.setProperty('--tab-active-bg', '#ffffff')
    d.setProperty('--tab-inactive-bg', '#d9d9d9')
    d.setProperty('--term-bg', `rgba(245,245,245,${a})`)
  }
  document.body.style.backgroundColor = isDark.value ? `rgba(38,38,38,${a})` : '#f7f7f7'
})
</script>

<template>
  <n-config-provider :theme="theme" :theme-overrides="themeOverrides">
    <n-message-provider>
      <div class="app-root">
        <ShellPanel @open-settings="handleOpenSettings" />
        <SettingsDialog :show="showSettings" @close="handleCloseSettings" />
      </div>
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
  background: var(--body-bg, #1e1e1e);
  color: var(--text-color, #d4d4d4);
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
  border-bottom: 1px solid var(--sidebar-shadow, #3c3c3c);
  transition: background 0.15s;
}
.section-header:hover { background: rgba(255,255,255,0.06); }
.section-arrow { color: #888; transition: transform 0.15s ease; flex-shrink: 0; }
.section-arrow.rotated { transform: rotate(90deg); }
.section-icon { color: #888; }
.section-label { font-size: 12px; font-weight: 600; color: var(--text-color, #d4d4d4); text-transform: uppercase; letter-spacing: 0.5px; }
.section-actions { margin-left: auto; display: flex; gap: 2px; }
.sm-field {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.sm-label {
  font-size: 11px;
  color: var(--text-color, #d4d4d4);
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
