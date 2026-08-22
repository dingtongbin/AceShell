<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { NIcon, NTooltip, useMessage } from 'naive-ui'
import {
  AddOutline,
  FolderOutline,
  ArrowUpOutline,
  DownloadOutline,
  LogOutOutline,
  CreateOutline,
  TrashOutline,
  CodeOutline,
  MoonOutline,
  SunnyOutline,
  FolderOpenOutline,
  InformationCircleOutline,
  BookOutline,
  CheckmarkOutline,
} from '@vicons/ionicons5'
import { Window, Events } from '@wailsio/runtime'
import { useTheme } from '../stores/theme'
import { SetTheme } from '../../bindings/changeme/internal/services/configservice.js'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const message = useMessage()
const { toggleTheme, isDark } = useTheme()

const props = defineProps<{
  showSession: boolean
  showAgent: boolean
  showToolbar: boolean
  /** 自绘标题栏开关(Frameless 模式):关闭时不渲染 ─□✕ 与拖拽区 */
  framelessEnabled: boolean
}>()

const emit = defineEmits<{
  (e: 'toggle-session'): void
  (e: 'toggle-agent'): void
  (e: 'toggle-toolbar'): void
  (e: 'new-session'): void
  (e: 'new-folder'): void
  (e: 'import-sessions'): void
  (e: 'export-sessions'): void
  (e: 'exit'): void
  (e: 'edit-active-session'): void
  (e: 'rename-selected'): void
  (e: 'delete-selected'): void
  (e: 'exec-script'): void
  (e: 'sftp'): void
  (e: 'about'): void
  (e: 'view-docs'): void
}>()

// 浏览器 dev 预览(无 Wails runtime)回退:不渲染窗口控制与拖拽区
const isWails = typeof window !== 'undefined' && !!(window as any)._wails
const showWinControls = computed(() => isWails && props.framelessEnabled)

// ==================== 菜单(迁移自 MainMenu,编辑项并入「文件」) ====================

interface MenuItem {
  key: string
  label?: string
  icon?: any
  checked?: boolean
  divider?: boolean
}

interface MenuEntry {
  key: string
  label: string
  items: MenuItem[]
}

const menus = computed<MenuEntry[]>(() => [
  {
    key: 'file',
    label: t('mainMenu.file'),
    items: [
      { key: 'new-session', label: t('mainMenu.newSession'), icon: AddOutline },
      { key: 'new-folder', label: t('mainMenu.newFolder'), icon: FolderOutline },
      { key: 'd1', divider: true },
      { key: 'import-sessions', label: t('mainMenu.importSessions'), icon: ArrowUpOutline },
      { key: 'export-sessions', label: t('mainMenu.exportSessions'), icon: DownloadOutline },
      { key: 'd2', divider: true },
      { key: 'edit-active-session', label: t('mainMenu.editActiveSession'), icon: CreateOutline },
      { key: 'rename-selected', label: t('mainMenu.renameSelected'), icon: CreateOutline },
      { key: 'delete-selected', label: t('mainMenu.deleteSelected'), icon: TrashOutline },
      { key: 'd3', divider: true },
      { key: 'exit', label: t('mainMenu.exit'), icon: LogOutOutline },
    ],
  },
  {
    key: 'tool',
    label: t('mainMenu.tool'),
    items: [
      { key: 'exec-script', label: t('mainMenu.execScript'), icon: CodeOutline },
      { key: 'sftp', label: 'SFTP', icon: FolderOpenOutline },
      { key: 'toggle-theme', label: t('mainMenu.toggleTheme'), icon: isDark.value ? SunnyOutline : MoonOutline },
      { key: 'd1', divider: true },
      { key: 'toggle-toolbar', label: t('mainMenu.toolbar'), checked: props.showToolbar },
    ],
  },
  {
    key: 'help',
    label: t('mainMenu.help'),
    items: [
      { key: 'about', label: t('mainMenu.about'), icon: InformationCircleOutline },
      { key: 'view-docs', label: t('mainMenu.viewDocs'), icon: BookOutline },
    ],
  },
])

// ==================== 菜单交互:点击展开 → 悬停切换,Esc/外部点击关闭 ====================

const activeMenu = ref<string | null>(null)
const menuOpen = computed(() => activeMenu.value !== null)

function toggleMenu(key: string) {
  activeMenu.value = activeMenu.value === key ? null : key
}

function onMenuEnter(key: string) {
  if (menuOpen.value && activeMenu.value !== key) activeMenu.value = key
}

function closeMenu() {
  activeMenu.value = null
}

function handleSelect(key: string) {
  switch (key) {
    case 'new-session': emit('new-session'); break
    case 'new-folder': emit('new-folder'); break
    case 'import-sessions': emit('import-sessions'); break
    case 'export-sessions': emit('export-sessions'); break
    case 'exit': emit('exit'); break
    case 'edit-active-session': emit('edit-active-session'); break
    case 'rename-selected': emit('rename-selected'); break
    case 'delete-selected': emit('delete-selected'); break
    case 'exec-script': emit('exec-script'); break
    case 'sftp': emit('sftp'); break
    case 'toggle-theme':
      toggleTheme()
      SetTheme(isDark.value ? 'dark' : 'light').catch(() => message.error(t('mainMenu.saveThemeFailed')))
      break
    case 'toggle-toolbar': emit('toggle-toolbar'); break
    case 'about': emit('about'); break
    case 'view-docs': emit('view-docs'); break
    default: return
  }
  closeMenu()
}

const menuRoot = ref<HTMLElement | null>(null)

function onDocClick(e: MouseEvent) {
  const el = menuRoot.value
  if (el && !el.contains(e.target as Node)) closeMenu()
}

function onEsc(e: KeyboardEvent) {
  if (e.key === 'Escape') closeMenu()
}

onMounted(() => {
  document.addEventListener('click', onDocClick)
  document.addEventListener('keydown', onEsc)
  if (showWinControls.value) {
    refreshMaxState()
  }
})

onBeforeUnmount(() => {
  document.removeEventListener('click', onDocClick)
  document.removeEventListener('keydown', onEsc)
})

// ==================== 窗口控制(─ □ ✕,仅 Frameless 模式) ====================

const isMax = ref(false)

async function refreshMaxState() {
  try {
    isMax.value = await Window.IsMaximised()
  } catch {}
}

function handleMinimise() {
  Window.Minimise().catch(() => {})
}

async function handleToggleMaximise() {
  try {
    await Window.ToggleMaximise()
    // 状态变更可能有延迟,短暂重查兜底
    setTimeout(() => { refreshMaxState() }, 80)
  } catch {}
}

function handleClose() {
  Window.Close().catch(() => {})
}

// ==================== 拖拽区 ====================

// Windows 自带双击最大化由原生处理,此处兜底(Win+Up 等场景图标状态也靠事件刷新)
function onDragDblClick() {
  if (showWinControls.value) handleToggleMaximise()
}

let offEvents: Array<() => void> = []
onMounted(() => {
  if (showWinControls.value) {
    try {
      offEvents.push(Events.On('common:WindowMaximise' as any, () => { isMax.value = true }))
      offEvents.push(Events.On('common:WindowUnMaximise' as any, () => { isMax.value = false }))
      offEvents.push(Events.On('common:WindowRestore' as any, () => { isMax.value = false }))
      offEvents.push(Events.On('windows:WindowMaximise' as any, () => { isMax.value = true }))
      offEvents.push(Events.On('windows:WindowUnMaximise' as any, () => { isMax.value = false }))
      offEvents.push(Events.On('windows:WindowRestore' as any, () => { isMax.value = false }))
    } catch {}
  }
})

onBeforeUnmount(() => {
  offEvents.forEach(off => { try { off() } catch {} })
  offEvents = []
})
</script>

<template>
  <div ref="menuRoot" class="top-menu-bar" :class="{ 'browser-mode': !showWinControls }">
    <!-- 应用 Logo -->
    <div class="tmb-logo">
      <img src="../assets/logo.png" alt="AceShell" draggable="false" />
    </div>
    <!-- 左段:菜单区(不可拖拽) -->
    <div class="tmb-menus">
      <div v-for="m in menus" :key="m.key" class="tmb-menu-entry">
        <div
          class="tmb-menu-btn"
          :class="{ active: activeMenu === m.key }"
          @click.stop="toggleMenu(m.key)"
          @mouseenter="onMenuEnter(m.key)"
        >{{ m.label }}</div>
        <!-- 下拉面板:锚定在按钮正下方 -->
        <div v-if="menuOpen && activeMenu === m.key" class="tmb-dropdown">
          <div
            v-for="it in m.items"
            :key="it.key"
            class="tmb-menu-item"
            :class="{ divider: it.divider }"
            @click="it.divider ? null : handleSelect(it.key)"
          >
            <template v-if="!it.divider">
              <span class="tmi-icon"><n-icon v-if="it.icon" :size="15" :component="it.icon" /></span>
              <span class="tmi-label">{{ it.label }}</span>
              <span class="tmi-check"><n-icon v-if="it.checked" :size="14" :component="CheckmarkOutline" /></span>
            </template>
          </div>
        </div>
      </div>
    </div>

    <!-- 中段:拖拽区(--wails-draggable 由 runtime 处理窗口拖动) -->
    <div class="tmb-drag" @dblclick="onDragDblClick"></div>

    <!-- 右段:收纳按钮 + 窗口控制 -->
    <div class="tmb-right">
      <n-tooltip placement="bottom" trigger="hover" :delay="300">
        <template #trigger>
          <button class="tmb-panel-btn" :class="{ active: showSession }" @click="emit('toggle-session')">
            <!-- VSCode layout-sidebar-left:圆角方框 + 左侧实心竖条(主侧边栏) -->
            <svg width="24" height="24" viewBox="0 0 16 16">
              <rect x="0.75" y="0.75" width="14.5" height="14.5" rx="2.25" fill="none" stroke="currentColor" stroke-width="1.1" />
              <rect x="2.7" y="2.7" width="3.4" height="10.6" rx="0.8" fill="currentColor" />
            </svg>
          </button>
        </template>
        {{ t('topMenu.resourceManager') }}
      </n-tooltip>
      <n-tooltip placement="bottom" trigger="hover" :delay="300">
        <template #trigger>
          <button class="tmb-panel-btn" :class="{ active: showAgent }" @click="emit('toggle-agent')">
            <!-- VSCode layout-sidebar-right:圆角方框 + 右侧实心竖条(次侧边栏,AI 聊天) -->
            <svg width="24" height="24" viewBox="0 0 16 16">
              <rect x="0.75" y="0.75" width="14.5" height="14.5" rx="2.25" fill="none" stroke="currentColor" stroke-width="1.1" />
              <rect x="9.9" y="2.7" width="3.4" height="10.6" rx="0.8" fill="currentColor" />
            </svg>
          </button>
        </template>
        {{ t('agent.panelTitle') }}
      </n-tooltip>

      <!-- 窗口控制:仅 Frameless 模式渲染 -->
      <div v-if="showWinControls" class="tmb-win-controls">
        <button class="wc-btn" :title="t('topMenu.minimise')" @click="handleMinimise">
          <svg width="10" height="10" viewBox="0 0 10 10"><line x1="0" y1="5" x2="10" y2="5" stroke="currentColor" stroke-width="1" /></svg>
        </button>
        <button class="wc-btn" :title="t('topMenu.maximise')" @click="handleToggleMaximise">
          <!-- 最大化:方框;还原:双层方框 -->
          <svg v-if="!isMax" width="10" height="10" viewBox="0 0 10 10"><rect x="0.5" y="0.5" width="9" height="9" fill="none" stroke="currentColor" stroke-width="1" /></svg>
          <svg v-else width="10" height="10" viewBox="0 0 10 10">
            <rect x="0.5" y="2.5" width="7" height="7" fill="none" stroke="currentColor" stroke-width="1" />
            <path d="M 2.5 2.5 L 2.5 0.5 L 9.5 0.5 L 9.5 7.5 L 7.5 7.5" fill="none" stroke="currentColor" stroke-width="1" />
          </svg>
        </button>
        <button class="wc-btn wc-close" :title="t('topMenu.close')" @click="handleClose">
          <svg width="10" height="10" viewBox="0 0 10 10"><path d="M0 0 L10 10 M10 0 L0 10" stroke="currentColor" stroke-width="1" /></svg>
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.top-menu-bar {
  height: 36px;
  min-height: 36px;
  flex-shrink: 0;
  display: flex;
  align-items: stretch;
  background: var(--toolbar-bg, #2d2d2d);
  border-bottom: 1px solid var(--border-color, #3c3c3c);
  user-select: none;
  position: relative;
  z-index: 900;
}

/* 应用 Logo */
.tmb-logo {
  width: 36px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.tmb-logo img {
  width: 18px;
  height: 18px;
  border-radius: 4px;
  user-select: none;
  pointer-events: none;
}

/* 菜单区 */
.tmb-menus {
  display: flex;
  align-items: stretch;
  padding-left: 4px;
}

.tmb-menu-entry {
  position: relative;
  display: flex;
  align-items: stretch;
}

.tmb-menu-btn {
  display: flex;
  align-items: center;
  padding: 0 12px;
  font-size: 13px;
  color: var(--text-color, #d4d4d4);
  cursor: pointer;
  border-radius: 0 0 6px 6px;
  transition: background 0.15s;
}

.tmb-menu-btn:hover,
.tmb-menu-btn.active {
  background: rgba(255, 255, 255, 0.08);
}

/* 下拉面板 */
.tmb-dropdown {
  position: absolute;
  top: 100%;
  left: 0;
  min-width: 210px;
  background: var(--panel-bg, #252526);
  border: 1px solid var(--border-color, #3c3c3c);
  border-radius: 6px;
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.45);
  padding: 4px;
  z-index: 1000;
  animation: tmb-drop 0.12s ease;
}

@keyframes tmb-drop {
  from { opacity: 0; transform: translateY(-4px); }
  to { opacity: 1; transform: translateY(0); }
}

.tmb-menu-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 10px;
  font-size: 13px;
  color: var(--text-color, #d4d4d4);
  border-radius: 4px;
  cursor: pointer;
  transition: background 0.12s;
  white-space: nowrap;
}

.tmb-menu-item:hover {
  background: rgba(255, 255, 255, 0.08);
}

.tmb-menu-item.divider {
  height: 1px;
  padding: 0;
  margin: 4px 6px;
  background: var(--sidebar-shadow, #3c3c3c);
  cursor: default;
}

.tmi-icon {
  width: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.tmi-label {
  flex: 1;
}

.tmi-check {
  width: 14px;
  display: flex;
  align-items: center;
  justify-content: center;
}

/* 拖拽区 */
.tmb-drag {
  flex: 1;
  min-width: 0;
  --wails-draggable: drag;
}

/* 浏览器回退模式:拖拽区无原生拖拽语义 */
.browser-mode .tmb-drag {
  --wails-draggable: no-drag;
}

/* 右段 */
.tmb-right {
  display: flex;
  align-items: center;
  gap: 1px;
  padding-right: 2px;
}

.tmb-panel-btn {
  width: 30px;
  height: 30px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: none;
  background: transparent;
  border-radius: 5px;
  color: var(--text-color, #d4d4d4);
  opacity: 0.75;
  cursor: pointer;
  transition: background 0.15s, opacity 0.15s, color 0.2s;
}

.tmb-panel-btn svg {
  display: block;
}

.tmb-panel-btn:hover {
  background: rgba(255, 255, 255, 0.08);
  opacity: 1;
}

.tmb-panel-btn.active {
  color: #4ec9b0;
  opacity: 1;
}

/* 窗口控制按钮 */
.tmb-win-controls {
  display: flex;
  align-items: stretch;
  margin-left: 6px;
  height: 100%;
}

.wc-btn {
  width: 44px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: none;
  background: transparent;
  color: var(--text-color, #d4d4d4);
  cursor: pointer;
  transition: background 0.12s, color 0.12s;
}

.wc-btn:hover {
  background: rgba(255, 255, 255, 0.1);
}

.wc-close:hover {
  background: #e45858;
  color: #fff;
}
</style>
