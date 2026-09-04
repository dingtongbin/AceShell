<script setup lang="ts">
import { ref, reactive, computed, onMounted, onBeforeUnmount } from 'vue'
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
  CheckmarkOutline,
  CopyOutline,
  ClipboardOutline,
  CloseOutline,
  HardwareChipOutline,
} from '@vicons/ionicons5'
import { Window, Events } from '@wailsio/runtime'
import { useTheme } from '../stores/theme'
import { GetConfig, SetTheme, SetShowSerial, SetShowHelp, SetCustomTitlebar, SetCloseConfirm, SetFileEditingAutoSave, SetShowToolbar, SetTerminalConfig, SetShowAssistant } from '../../bindings/changeme/internal/services/configservice.js'
import { SetMcpEnabled, McpResume, McpNotifyPreemption } from '../../bindings/changeme/internal/services/mcpservice.js'
import { useMcpBridge } from '../composables/useMcpBridge'
import { useI18n } from 'vue-i18n'
import type { ActiveTabState } from './tabTypes'

const { t } = useI18n()
const message = useMessage()
const { toggleTheme, isDark } = useTheme()
const { status: mcpStatus } = useMcpBridge()

const props = defineProps<{
  showSession: boolean
  showAgent: boolean
  /** 智能助手总开关(视图菜单): 关闭时隐藏 MCP/资源管理器/AI 面板三个顶栏按钮 */
  showAssistant: boolean
  /** 活动标签页状态快照:用于按活动标签页启用/禁用工具菜单项 */
  activeTabState: ActiveTabState
  /** 自绘标题栏开关(Frameless 模式):关闭时不渲染 ─□✕ 与拖拽区 */
  framelessEnabled: boolean
}>()

const emit = defineEmits<{
  (e: 'toggle-session'): void
  (e: 'toggle-agent'): void
  (e: 'new-session'): void
  (e: 'new-folder'): void
  (e: 'import-sessions'): void
  (e: 'export-sessions'): void
  (e: 'exit'): void
  (e: 'edit-active-session'): void
  (e: 'rename-selected'): void
  (e: 'delete-selected'): void
  (e: 'tool-action', key: string): void
  (e: 'about'): void
  (e: 'view-docs'): void
}>()

// 浏览器 dev 预览(无 Wails runtime)回退:不渲染窗口控制与拖拽区
const isWails = typeof window !== 'undefined' && !!(window as any)._wails
const showWinControls = computed(() => isWails && props.framelessEnabled)

// ==================== 视图菜单:设置弹窗内的勾选项快捷开关 ====================

const viewCfg = reactive({
  showSerial: true,
  showHelp: true,
  customTitlebar: true,
  showToolbar: true,
  personalize: false,
  copyOnSelect: true,
  cursorBlink: true,
  autoSave: true,
  closeNoConfirm: false,
  showAssistant: true,
})

async function loadViewCfg() {
  try {
    const cfg = JSON.parse(await GetConfig())
    viewCfg.showSerial = cfg.view?.showSerial ?? true
    viewCfg.showHelp = cfg.view?.showHelp ?? true
    viewCfg.customTitlebar = cfg.view?.customTitlebar ?? true
    viewCfg.showToolbar = cfg.view?.showToolbar ?? true
    viewCfg.personalize = cfg.terminal?.personalize ?? false
    viewCfg.copyOnSelect = cfg.terminal?.copyOnSelect ?? true
    viewCfg.cursorBlink = cfg.terminal?.cursorBlink ?? true
    viewCfg.autoSave = cfg.fileEditing?.autoSave ?? true
    viewCfg.closeNoConfirm = cfg.view?.closeConfirm === false
    viewCfg.showAssistant = cfg.view?.showAssistant ?? true
  } catch {}
}

// 终端表单类勾选项通过 SetTerminalConfig 整包保存(与设置弹窗同一口径)
async function setTermField(field: 'personalize' | 'copyOnSelect' | 'cursorBlink', value: boolean) {
  try {
    const cfg = JSON.parse(await GetConfig())
    const t = cfg.terminal ?? {}
    await SetTerminalConfig(JSON.stringify({
      showToolbar: cfg.view?.showToolbar ?? true,
      personalize: field === 'personalize' ? value : (t.personalize ?? false),
      fontColor: t.fontColor || '#FFFFFF',
      bgColor: t.bgColor || '#0C0C0C',
      bgOpacity: t.bgOpacity ?? 100,
      bgImage: t.bgImage || '',
      fontFamily: t.fontFamily || '"Cascadia Code", Consolas, "Courier New", monospace',
      fontSize: t.fontSize ?? 16,
      lineHeight: t.lineHeight ?? 1,
      copyOnSelect: field === 'copyOnSelect' ? value : (t.copyOnSelect ?? true),
      cursorBlink: field === 'cursorBlink' ? value : (t.cursorBlink ?? true),
      cursorStyle: t.cursorStyle || 'bar',
      scrollback: t.scrollback ?? 1000,
    }))
  } catch {}
}

async function toggleViewItem(key: string) {
  switch (key) {
    case 'v-serial':
      viewCfg.showSerial = !viewCfg.showSerial
      await SetShowSerial(viewCfg.showSerial).catch(() => {})
      break
    case 'v-help':
      viewCfg.showHelp = !viewCfg.showHelp
      await SetShowHelp(viewCfg.showHelp).catch(() => {})
      break
    case 'v-titlebar':
      viewCfg.customTitlebar = !viewCfg.customTitlebar
      await SetCustomTitlebar(viewCfg.customTitlebar).catch(() => {})
      break
    case 'v-toolbar':
      viewCfg.showToolbar = !viewCfg.showToolbar
      await SetShowToolbar(viewCfg.showToolbar).catch(() => {})
      break
    case 'v-personalize':
      viewCfg.personalize = !viewCfg.personalize
      await setTermField('personalize', viewCfg.personalize)
      break
    case 'v-copy-select':
      viewCfg.copyOnSelect = !viewCfg.copyOnSelect
      await setTermField('copyOnSelect', viewCfg.copyOnSelect)
      break
    case 'v-cursor-blink':
      viewCfg.cursorBlink = !viewCfg.cursorBlink
      await setTermField('cursorBlink', viewCfg.cursorBlink)
      break
    case 'v-autosave':
      viewCfg.autoSave = !viewCfg.autoSave
      await SetFileEditingAutoSave(viewCfg.autoSave).catch(() => {})
      break
    case 'v-close-confirm':
      viewCfg.closeNoConfirm = !viewCfg.closeNoConfirm
      await SetCloseConfirm(!viewCfg.closeNoConfirm).catch(() => {})
      break
    case 'v-assistant':
      viewCfg.showAssistant = !viewCfg.showAssistant
      await SetShowAssistant(viewCfg.showAssistant).catch(() => {})
      break
    default:
      return
  }
  window.dispatchEvent(new Event('config-changed'))
}

// ==================== 菜单(迁移自 MainMenu,编辑项并入「文件」) ====================

interface MenuItem {
  key: string
  label?: string
  icon?: any
  checked?: boolean
  disabled?: boolean
  divider?: boolean
}

interface MenuEntry {
  key: string
  label: string
  items: MenuItem[]
}

// ==================== 工具菜单:镜像终端工具栏,按活动标签页启用/禁用 ====================

// 活动标签页为终端标签页(复制/粘贴/脚本/日志/清屏等工具的前提)
const hasActiveTerminal = computed(() => !!props.activeTabState?.hasTab && !!props.activeTabState?.isTerminal)
// SFTP 仅对已连接的 SSH 会话可用
const canSftp = computed(() => !!props.activeTabState?.hasTab && props.activeTabState?.protocol === 'ssh' && !!props.activeTabState?.connected)

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
      { key: 'exit', label: t('mainMenu.exit'), icon: LogOutOutline },
    ],
  },
  {
    key: 'view',
    label: t('mainMenu.view'),
    items: [
      { key: 'v-serial', label: t('settings.serialManager'), checked: viewCfg.showSerial },
      { key: 'v-help', label: t('settings.help'), checked: viewCfg.showHelp },
      { key: 'v-titlebar', label: t('settings.customTitlebar'), checked: viewCfg.customTitlebar },
      { key: 'dv1', divider: true },
      { key: 'v-toolbar', label: t('settings.toolbarSwitch'), checked: viewCfg.showToolbar },
      { key: 'v-personalize', label: t('settings.personalize'), checked: viewCfg.personalize },
      { key: 'v-copy-select', label: t('settings.copyOnSelect'), checked: viewCfg.copyOnSelect },
      { key: 'v-cursor-blink', label: t('settings.cursorBlink'), checked: viewCfg.cursorBlink },
      { key: 'dv2', divider: true },
      { key: 'v-autosave', label: t('settings.autoSave'), checked: viewCfg.autoSave },
      { key: 'v-close-confirm', label: t('settings.noCloseConfirm'), checked: viewCfg.closeNoConfirm },
      { key: 'dv3', divider: true },
      { key: 'v-assistant', label: t('settings.assistant'), checked: viewCfg.showAssistant },
    ],
  },
  {
    key: 'tool',
    label: t('mainMenu.tool'),
    items: [
      { key: 'copy-selection', label: t('tabPane.copySelection'), icon: CopyOutline, disabled: !hasActiveTerminal.value },
      { key: 'paste-terminal', label: t('tabPane.pasteToTerminal'), icon: ClipboardOutline, disabled: !hasActiveTerminal.value },
      { key: 'sftp', label: 'SFTP', icon: FolderOpenOutline, disabled: !canSftp.value },
      { key: 'd1', divider: true },
      { key: 'exec-script', label: t('mainMenu.execScript'), icon: CodeOutline, disabled: !hasActiveTerminal.value },
      { key: 'export-log', label: t('tabPane.exportLog'), icon: DownloadOutline, disabled: !hasActiveTerminal.value },
      { key: 'clear-scrollback', label: t('tabPane.clearScrollback'), icon: TrashOutline, disabled: !hasActiveTerminal.value },
      { key: 'clear-screen', label: t('tabPane.clearScreen'), icon: CloseOutline, disabled: !hasActiveTerminal.value },
      { key: 'd2', divider: true },
      { key: 'toggle-theme', label: t('mainMenu.toggleTheme'), icon: isDark.value ? SunnyOutline : MoonOutline },
    ],
  },
  {
    // 帮助: 顶级按钮直接打开帮助窗口,无下拉项(items 置空,模板按 key 特判)
    key: 'help',
    label: t('mainMenu.help'),
    items: [],
  },
])

// ==================== MCP 开关(顶栏右段) ====================

// 四态:off 关闭 / idle 已开启空闲 / paused 已挂起 / busy 执行中(含等待审批)
const mcpPhase = computed<'off' | 'idle' | 'paused' | 'busy'>(() => {
  const s = mcpStatus.value
  if (!s.enabled || s.state === 'stopped') return 'off'
  if (s.state === 'paused') return 'paused'
  return (s.busy || s.pendingApprovals > 0) ? 'busy' : 'idle'
})

const mcpTooltip = computed(() => ({
  off: t('topMenu.mcpOff'),
  idle: t('topMenu.mcpOn'),
  paused: t('topMenu.mcpPaused'),
  busy: t('topMenu.mcpBusy'),
}[mcpPhase.value]))

async function handleMcpToggle() {
  try {
    switch (mcpPhase.value) {
      case 'off': {
        const res = JSON.parse(await SetMcpEnabled(true))
        if (res?.error) message.error(t('topMenu.mcpStartFailed', { err: res.error }))
        break
      }
      case 'idle':
        await SetMcpEnabled(false)
        break
      case 'paused':
        await McpResume()
        break
      case 'busy':
        // 执行中点击 = 用户打断:挂起并取消在途操作(与键盘抢占同路径)
        await McpNotifyPreemption()
        break
    }
  } catch (e: any) {
    message.error(t('topMenu.mcpStartFailed', { err: (e && e.message) || e }))
  }
}

// ==================== 菜单交互:点击展开 → 悬停切换,Esc/外部点击关闭 ====================

const activeMenu = ref<string | null>(null)
const menuOpen = computed(() => activeMenu.value !== null)

function toggleMenu(key: string) {
  activeMenu.value = activeMenu.value === key ? null : key
}

function onMenuEnter(key: string) {
  if (menuOpen.value && activeMenu.value !== key) activeMenu.value = key
}

// 帮助顶级按钮: 直接打开帮助窗口,并收起可能展开的下拉
function openHelpDirect() {
  activeMenu.value = null
  emit('about')
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
    case 'exec-script': emit('tool-action', 'exec-script'); break
    case 'sftp': emit('tool-action', 'sftp'); break
    case 'copy-selection': emit('tool-action', 'copy-selection'); break
    case 'paste-terminal': emit('tool-action', 'paste-terminal'); break
    case 'export-log': emit('tool-action', 'export-log'); break
    case 'clear-scrollback': emit('tool-action', 'clear-scrollback'); break
    case 'clear-screen': emit('tool-action', 'clear-screen'); break
    case 'toggle-theme':
      toggleTheme()
      SetTheme(isDark.value ? 'dark' : 'light').catch(() => message.error(t('mainMenu.saveThemeFailed')))
      break
    case 'v-serial':
    case 'v-help':
    case 'v-titlebar':
    case 'v-toolbar':
    case 'v-personalize':
    case 'v-copy-select':
    case 'v-cursor-blink':
    case 'v-autosave':
    case 'v-close-confirm':
    case 'v-assistant':
      toggleViewItem(key)
      break
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
  loadViewCfg()
  window.addEventListener('config-changed', loadViewCfg)
  document.addEventListener('click', onDocClick)
  document.addEventListener('keydown', onEsc)
  if (showWinControls.value) {
    refreshMaxState()
  }
})

onBeforeUnmount(() => {
  window.removeEventListener('config-changed', loadViewCfg)
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
        <!-- 帮助: 无下拉,点击直接弹帮助窗口 -->
        <div
          v-if="m.key === 'help'"
          class="tmb-menu-btn"
          @click.stop="openHelpDirect"
        >{{ m.label }}</div>
        <template v-else>
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
              :class="{ divider: it.divider, disabled: it.disabled }"
              @click="it.divider || it.disabled ? null : handleSelect(it.key)"
            >
              <template v-if="!it.divider">
                <span class="tmi-icon"><n-icon v-if="it.icon" :size="15" :component="it.icon" /></span>
                <span class="tmi-label">{{ it.label }}</span>
                <span class="tmi-check"><n-icon v-if="it.checked" :size="14" :component="CheckmarkOutline" /></span>
              </template>
            </div>
          </div>
        </template>
      </div>
    </div>

    <!-- 中段:拖拽区(--wails-draggable 由 runtime 处理窗口拖动) -->
    <div class="tmb-drag" @dblclick="onDragDblClick"></div>

    <!-- 右段:MCP 开关 + 收纳按钮 + 窗口控制(前三者随"智能助手"开关显隐,默认关闭) -->
    <div class="tmb-right">
      <template v-if="showAssistant">
      <n-tooltip placement="bottom" trigger="hover" :delay="300">
        <template #trigger>
          <button class="tmb-mcp-btn" :class="mcpPhase" :title="mcpTooltip" @click="handleMcpToggle">
            <n-icon :size="16" :component="HardwareChipOutline" />
          </button>
        </template>
        {{ mcpTooltip }}
      </n-tooltip>
      <n-tooltip placement="bottom" trigger="hover" :delay="300">
        <template #trigger>
          <button class="tmb-panel-btn" :class="{ active: showSession }" @click="emit('toggle-session')">
            <!-- VSCode layout-sidebar-left:圆角方框 + 左侧实心竖条(主侧边栏) -->
            <svg width="16" height="16" viewBox="0 0 16 16">
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
            <svg width="16" height="16" viewBox="0 0 16 16">
              <rect x="0.75" y="0.75" width="14.5" height="14.5" rx="2.25" fill="none" stroke="currentColor" stroke-width="1.1" />
              <rect x="9.9" y="2.7" width="3.4" height="10.6" rx="0.8" fill="currentColor" />
            </svg>
          </button>
        </template>
        {{ t('agent.panelTitle') }}
      </n-tooltip>
      </template>

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
  /* 自绘标题栏必须凌驾于一切应用内弹层(Naive UI 弹窗遮罩从 2000 起动态递增),
     保证任意弹窗打开时窗口拖拽/移动/关闭等标题栏能力不受遮罩影响 */
  z-index: 1000000;
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
  text-align: left;
}

/* 浅色模式:柔化下拉面板阴影 */
html:not(.dark) .tmb-dropdown {
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.15);
}

@keyframes tmb-drop {
  from { opacity: 0; transform: translateY(-4px); }
  to { opacity: 1; transform: translateY(0); }
}

.tmb-menu-item {
  display: flex;
  align-items: center;
  justify-content: flex-start;
  gap: 8px;
  padding: 6px 10px;
  font-size: 13px;
  color: var(--text-color, #d4d4d4);
  border-radius: 4px;
  cursor: pointer;
  transition: background 0.12s;
  white-space: nowrap;
  text-align: left;
}

.tmb-menu-item:hover {
  background: var(--hover-bg, rgba(255, 255, 255, 0.08));
}

.tmb-menu-item.disabled {
  opacity: 0.4;
  cursor: default;
}

.tmb-menu-item.disabled:hover {
  background: transparent;
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
  text-align: left;
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

/* MCP 开关:off 关闭(暗灰) / idle 已开启(绿) / paused 挂起(黄) / busy 执行中(主题色,静态指示) */
.tmb-mcp-btn {
  width: 30px;
  height: 30px;
  margin-right: 3px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: none;
  background: transparent;
  border-radius: 5px;
  cursor: pointer;
  transition: background 0.15s, color 0.2s;
}

.tmb-mcp-btn:hover {
  background: rgba(255, 255, 255, 0.08);
}

.tmb-mcp-btn.off {
  color: var(--icon-color, #6e6e6e);
}

.tmb-mcp-btn.idle {
  color: #4ec9b0;
}

.tmb-mcp-btn.paused {
  color: #f2c97d;
}

.tmb-mcp-btn.busy {
  color: var(--primary-color, #0078d4);
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
  height: 30px;
  align-self: center;
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
