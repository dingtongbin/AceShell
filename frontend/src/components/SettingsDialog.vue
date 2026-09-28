<script setup lang="ts">
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { NModal, NSwitch, NTag, NRadioGroup, NRadioButton, NButton, NInput, NCheckbox, NIcon, NSlider, NColorPicker, NInputNumber, NAutoComplete, NSelect, useMessage } from 'naive-ui'
import { CloseOutline, LogoGithub, GlobeOutline, CopyOutline, RefreshOutline, PauseOutline, PlayOutline, SaveOutline, DocumentTextOutline, ChevronDownOutline } from '@vicons/ionicons5'
import { useTheme } from '../stores/theme'
import { ACCENT_PRESETS } from '../stores/tokens'
import { GetConfig, SetTabOrientation, SetTheme, SetThemeAccent, SetCloseConfirm, SetPanelOpacity, SetWallpaper, SetTerminalConfig, SetShowSerial, SetShowHelp, SetFileEditingAutoSave, SetLanguage, SetCustomTitlebar, SetShowToolbar, SetShowAssistant, McpDangerousPatterns } from '../../bindings/changeme/internal/services/configservice.js'
import { SetMcpEnabled, McpPause, McpResume, ResetMcpToken, ExportAuditPdf, SetMcpDangerousPatterns } from '../../bindings/changeme/internal/services/mcpservice.js'
import { OpenFileDialog } from '../../bindings/changeme/internal/services/windowservice.js'
import { OpenUrl as BrowserOpenUrl } from '../../bindings/changeme/internal/services/browserservice.js'
import { GetVersion } from '../../bindings/changeme/internal/services/versionservice.js'
import { setLocale, languageOptions } from '../i18n'
import { useI18n } from 'vue-i18n'
import { useMcpBridge } from '../composables/useMcpBridge'

const message = useMessage()
const { t, locale } = useI18n()

const { themeMode, setThemeMode, accent, setAccent } = useTheme()

// 自定义强调色: 即时预览(setAccent) + 持久化(SetThemeAccent) + 跨组件同步(config-changed)
async function handleAccentChange(color: string) {
  const hex = color.slice(0, 7)
  if (!/^#[0-9a-fA-F]{6}$/.test(hex)) return
  setAccent(hex)
  try {
    await SetThemeAccent(hex)
    window.dispatchEvent(new Event('config-changed'))
  } catch (e) {
    console.warn('设置强调色失败:', e)
  }
}

// 用系统默认浏览器打开外部链接
async function openExternal(url: string) {
  const err = await BrowserOpenUrl('', url)
  if (err) message.error(t('settings.openUrlFailed', { err }))
}

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ (e: 'close'): void }>()

const activeNav = ref('general')
const tabOrientation = ref('horizontal')

const closeNoConfirm = ref(false)
const panelOpacity = ref(100)
const wallpaperPath = ref('')
const showSerial = ref(true)
const showHelp = ref(true)
// 自绘标题栏(Frameless 窗口):即时切换,config-changed → ShellPanel watcher 调 Window.SetFrameless
const customTitlebar = ref(true)
const autoSave = ref(true)
const language = ref('zh-CN')
const appVersion = ref('0.1.0')
// 视图 tab 与顶级菜单视图菜单同步的项(顺序/变量/持久化口径一致)
const showToolbar = ref(true)
const personalize = ref(false)
const copyOnSelect = ref(true)
const cursorBlink = ref(true)
const showAssistant = ref(false)

const navItems = computed(() => [
  { key: 'general', label: t('settings.nav.general') },
  { key: 'view', label: t('settings.nav.view') },
  { key: 'terminal', label: t('settings.nav.terminal') },
  { key: 'fileEditing', label: t('settings.nav.fileEditing') },
  { key: 'tabs', label: t('settings.nav.tabs') },
  { key: 'mcp', label: t('settings.nav.mcp') },
  { key: 'about', label: t('settings.nav.about') },
])

// ==================== MCP 服务设置(与设置弹窗布局同语言) ====================

const { status: mcpStatus, auditLog: mcpAuditLog, saveExecTuning } = useMcpBridge()

const mcpSwitching = ref(false)
const mcpShowToken = ref(false)

const mcpStateText = computed(() => {
  switch (mcpStatus.value.state) {
    case 'running': return t('mcp.stateRunning')
    case 'paused': return t('mcp.statePaused')
    default: return t('mcp.stateStopped')
  }
})
const mcpStateType = computed(() => (mcpStatus.value.state === 'running' ? 'success' : mcpStatus.value.state === 'paused' ? 'warning' : 'default') as any)

function maskedToken(token: string): string {
  if (!token) return ''
  if (mcpShowToken.value) return token
  if (token.length <= 8) return '****'
  return token.slice(0, 4) + '****' + token.slice(-4)
}

async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    message.success(t('mcp.copied'))
  } catch { message.error(t('mcp.copyFailed')) }
}

// 解析控制接口返回: statusMap 刷新本地状态; {"error":...} 弹错并返回 false
function applyMcpControlResult(res: any, successTip?: string): boolean {
  if (!res || typeof res !== 'object') return false
  if (res.error) { message.error(String(res.error)); return false }
  Object.assign(mcpStatus.value, res)
  if (successTip) message.success(successTip)
  return true
}

async function handleMcpToggle(enabled: boolean) {
  mcpSwitching.value = true
  try {
    const res = JSON.parse(await SetMcpEnabled(enabled) || '{}')
    applyMcpControlResult(res)
  } catch (e: any) {
    message.error(String(e?.message || e))
  } finally { mcpSwitching.value = false }
}

async function handleMcpPause() {
  try {
    const res = JSON.parse(await McpPause() || '{}')
    applyMcpControlResult(res)
  } catch (e: any) { message.error(String(e?.message || e)) }
}
async function handleMcpResume() {
  try {
    const res = JSON.parse(await McpResume() || '{}')
    applyMcpControlResult(res)
  } catch (e: any) { message.error(String(e?.message || e)) }
}

async function handleMcpResetToken() {
  try {
    const res = JSON.parse(await ResetMcpToken() || '{}')
    applyMcpControlResult(res, t('mcp.tokenResetOk'))
  } catch (e: any) { message.error(String(e?.message || e)) }
}

// 执行参数(本地编辑副本,保存时整体提交)
const mcpTuning = ref({ opDelayMs: 1000, batchIntervalMs: 300, auditRetentionDays: 30, terminalReadMaxKB: 32 })
const mcpTuningSaving = ref(false)

watch(() => [
  mcpStatus.value.opDelayMs, mcpStatus.value.batchIntervalMs,
  mcpStatus.value.auditRetentionDays, mcpStatus.value.terminalReadMax,
], () => {
  mcpTuning.value = {
    opDelayMs: mcpStatus.value.opDelayMs,
    batchIntervalMs: mcpStatus.value.batchIntervalMs,
    auditRetentionDays: mcpStatus.value.auditRetentionDays,
    terminalReadMaxKB: Math.round(mcpStatus.value.terminalReadMax / 1024),
  }
}, { immediate: true })

async function handleMcpSaveTuning() {
  mcpTuningSaving.value = true
  try {
    const raw = await saveExecTuning(
      mcpTuning.value.opDelayMs, mcpTuning.value.batchIntervalMs,
      mcpTuning.value.auditRetentionDays, Math.round(mcpTuning.value.terminalReadMaxKB * 1024),
    )
    const res = raw ? JSON.parse(raw) : null
    if (res?.error) message.error(String(res.error))
    else message.success(t('mcp.tuningSaved'))
  } catch (e: any) { message.error(String(e?.message || e)) } finally { mcpTuningSaving.value = false }
}

// 绝对危险指令字典(每行一条正则;清空全部并保存 = 恢复内置默认)
const mcpDangerousText = ref('')
const mcpDangerousLoading = ref(false)
const mcpDangerousSaving = ref(false)
const mcpDangerousLoaded = ref(false)

async function loadMcpDangerous() {
  mcpDangerousLoading.value = true
  try {
    const list = JSON.parse(await McpDangerousPatterns() || '[]')
    mcpDangerousText.value = (Array.isArray(list) ? list : []).join('\n')
  } catch { mcpDangerousText.value = '' } finally { mcpDangerousLoading.value = false }
}

function mcpDangerousCount(): number {
  return mcpDangerousText.value.split('\n').map(s => s.trim()).filter(Boolean).length
}

async function handleMcpSaveDangerous(restoreDefault = false) {
  mcpDangerousSaving.value = true
  try {
    const patterns = restoreDefault ? [] : mcpDangerousText.value.split('\n').map(s => s.trim()).filter(Boolean)
    const raw = await SetMcpDangerousPatterns(JSON.stringify(patterns))
    const res = raw ? JSON.parse(raw) : null
    if (res && !Array.isArray(res) && res.error) message.error(String(res.error))
    else { message.success(t('mcp.dangerousSaved')); loadMcpDangerous() }
  } catch (e: any) { message.error(String(e?.message || e)) } finally { mcpDangerousSaving.value = false }
}

// 打开弹窗时懒加载危险字典
watch(() => props.show && activeNav.value === 'mcp', (v) => {
  if (v && !mcpDangerousLoaded.value) {
    mcpDangerousLoaded.value = true
    loadMcpDangerous()
  }
})

// 审计日志(倒序 + 过滤 + 展开详情)
const mcpReversedLogs = computed(() => [...mcpAuditLog.value].reverse())
const mcpRiskFilter = ref<'all' | 'blocked' | 'allowed'>('all')
const mcpLogExpanded = ref<Record<string, boolean>>({})
const mcpPdfExporting = ref(false)

const mcpRiskFilterOptions = computed(() => [
  { key: 'all', label: t('mcp.filterAll') },
  { key: 'blocked', label: t('mcp.riskBlocked') },
  { key: 'allowed', label: t('mcp.riskAllowed') },
])

const mcpFilteredLogs = computed(() => mcpReversedLogs.value.filter(log => {
  if (mcpRiskFilter.value === 'blocked' && log.risk !== 'blocked') return false
  if (mcpRiskFilter.value === 'allowed' && log.risk === 'blocked') return false
  return true
}))

function mcpSourceText(source: string): string {
  switch (source) {
    case 'external': return t('mcp.sourceExternal')
    case 'embedded': return t('mcp.sourceEmbedded')
    default: return source
  }
}

function toggleMcpLogDetail(id: string) {
  mcpLogExpanded.value[id] = !mcpLogExpanded.value[id]
}

function mcpRiskType(risk: string): any {
  return risk === 'blocked' ? 'error' : 'success'
}
function mcpRiskText(risk: string): string {
  return risk === 'blocked' ? t('mcp.riskBlocked') : t('mcp.riskAllowed')
}
function mcpDecisionType(decision: string): any {
  switch (decision) {
    case 'approved': case 'auto': case 'executed': return 'success'
    case 'denied': case 'rejected': case 'blocked': return 'error'
    case 'pending': case 'timeout': return 'warning'
    default: return 'default'
  }
}
function mcpDecisionText(decision: string): string {
  const key = 'mcp.decision_' + decision
  const val = t(key)
  return val === key ? decision : val
}

async function handleMcpExportPdf() {
  mcpPdfExporting.value = true
  try {
    const raw = JSON.parse(await ExportAuditPdf(locale.value))
    if (raw?.error) message.error(String(raw.error))
    else if (raw?.path) message.success(t('mcp.exportPdfOk', { path: raw.path }))
    // path 为空 = 用户取消,不提示
  } catch (e: any) {
    message.error(String(e?.message || e))
  } finally { mcpPdfExporting.value = false }
}

// ==================== 终端设置(表单模式:确定才保存生效) ====================

const DEFAULT_TERM = {
  showToolbar: true,
  personalize: false,
  fontColor: '#FFFFFF',
  bgColor: '#0C0C0C',
  bgOpacity: 100,
  bgImage: '',
  fontFamily: '"Cascadia Code", Consolas, "Courier New", monospace',
  fontSize: 16,
  lineHeight: 1,
  copyOnSelect: true,
  cursorBlink: true,
  cursorStyle: 'bar',
  scrollback: 1000,
}

const termForm = reactive({ ...DEFAULT_TERM })
let termSaved = { ...DEFAULT_TERM }
const termError = ref('')

const fontOptions = [
  { label: 'Consolas', value: 'Consolas' },
  { label: 'Courier New', value: '"Courier New"' },
  { label: 'Menlo', value: 'Menlo' },
  { label: 'Monaco', value: 'Monaco' },
  { label: 'DejaVu Sans Mono', value: '"DejaVu Sans Mono"' },
  { label: 'Fira Code', value: '"Fira Code"' },
  { label: 'JetBrains Mono', value: '"JetBrains Mono"' },
]

const cursorStyleOptions = computed(() => [
  { label: t('settings.cursorBar'), value: 'bar' },
  { label: t('settings.cursorBlock'), value: 'block' },
  { label: t('settings.cursorUnderline'), value: 'underline' },
])

function loadTermForm(cfg: any) {
  const t = cfg?.terminal ?? {}
  termForm.showToolbar = cfg?.view?.showToolbar ?? DEFAULT_TERM.showToolbar
  termForm.personalize = t.personalize ?? DEFAULT_TERM.personalize
  termForm.fontColor = t.fontColor || DEFAULT_TERM.fontColor
  termForm.bgColor = t.bgColor || DEFAULT_TERM.bgColor
  termForm.bgOpacity = t.bgOpacity ?? DEFAULT_TERM.bgOpacity
  termForm.bgImage = t.bgImage || DEFAULT_TERM.bgImage
  termForm.fontFamily = t.fontFamily || DEFAULT_TERM.fontFamily
  termForm.fontSize = t.fontSize ?? DEFAULT_TERM.fontSize
  termForm.lineHeight = t.lineHeight ?? DEFAULT_TERM.lineHeight
  termForm.copyOnSelect = t.copyOnSelect ?? DEFAULT_TERM.copyOnSelect
  termForm.cursorBlink = t.cursorBlink ?? DEFAULT_TERM.cursorBlink
  termForm.cursorStyle = ['bar', 'block', 'underline'].includes(t.cursorStyle) ? t.cursorStyle : DEFAULT_TERM.cursorStyle
  termForm.scrollback = t.scrollback ?? DEFAULT_TERM.scrollback
  termSaved = { ...termForm }
  termError.value = ''
}

function resetTermForm() {
  Object.assign(termForm, DEFAULT_TERM)
  termError.value = ''
}

function cancelTermForm() {
  Object.assign(termForm, termSaved)
  termError.value = ''
  emit('close')
}

async function confirmTermForm() {
  termError.value = ''
  if (!/^#[0-9a-fA-F]{6}$/.test(termForm.fontColor)) { termError.value = t('settings.fontColorFormat'); return }
  if (!/^#[0-9a-fA-F]{6}$/.test(termForm.bgColor)) { termError.value = t('settings.bgColorFormat'); return }
  if (termForm.bgOpacity < 0 || termForm.bgOpacity > 100) { termError.value = t('settings.bgOpacityRange'); return }
  if (!termForm.fontFamily.trim()) { termError.value = t('settings.fontEmpty'); return }
  if (termForm.fontSize < 10 || termForm.fontSize > 32) { termError.value = t('settings.fontSizeRange'); return }
  if (termForm.lineHeight < 0.8 || termForm.lineHeight > 2) { termError.value = t('settings.lineHeightRange'); return }
  if (termForm.scrollback < 100 || termForm.scrollback > 100000) { termError.value = t('settings.scrollbackRange'); return }
  try {
    await SetTerminalConfig(JSON.stringify({ ...termForm }))
    termSaved = { ...termForm }
    window.dispatchEvent(new Event('config-changed'))
    emit('close')
  } catch (e: any) { termError.value = e.message || t('settings.saveFailed') }
}

// 切换离开终端 Tab 时放弃未保存修改(等同取消)
watch(activeNav, (val, old) => {
  if (old === 'terminal' && val !== 'terminal') {
    Object.assign(termForm, termSaved)
    termError.value = ''
  }
})

async function handleThemeChange(value: string) {
  setThemeMode(value as 'dark' | 'light' | 'auto')
  try { await SetTheme(value); window.dispatchEvent(new Event('config-changed')) } catch (e) { console.warn('设置主题失败:', e) }
}

async function handleLanguageChange(value: string) {
  language.value = value
  setLocale(value)
  try { await SetLanguage(value); window.dispatchEvent(new Event('config-changed')) } catch (e) { console.warn('设置语言失败:', e) }
}

async function handleTabOrientationChange(value: string) {
  tabOrientation.value = value
  try { await SetTabOrientation(value); window.dispatchEvent(new Event('config-changed')) } catch {}
}

async function handleCloseNoConfirmChange(value: boolean) {
  closeNoConfirm.value = value
  try { await SetCloseConfirm(!value); window.dispatchEvent(new Event('config-changed')) } catch {}
}

let opacityTimer: ReturnType<typeof setTimeout> | null = null
async function handlePanelOpacityChange(value: number) {
  panelOpacity.value = value
  if (opacityTimer) { clearTimeout(opacityTimer); opacityTimer = null }
  opacityTimer = setTimeout(async () => {
    opacityTimer = null
    try { await SetPanelOpacity(value); window.dispatchEvent(new Event('config-changed')) } catch (e) { console.warn('设置透明度失败:', e) }
  }, 150)
}

async function handleShowSerialChange(value: boolean) {
  showSerial.value = value
  try { await SetShowSerial(value); window.dispatchEvent(new Event('config-changed')) } catch (e) { console.warn('切换串口管理器失败:', e) }
}

async function handleShowHelpChange(value: boolean) {
  showHelp.value = value
  try { await SetShowHelp(value); window.dispatchEvent(new Event('config-changed')) } catch (e) { console.warn('切换帮助失败:', e) }
}

async function handleCustomTitlebarChange(value: boolean) {
  customTitlebar.value = value
  try { await SetCustomTitlebar(value); window.dispatchEvent(new Event('config-changed')) } catch (e) { console.warn('切换自绘标题栏失败:', e) }
}

async function handleAutoSaveChange(value: boolean) {
  autoSave.value = value
  try { await SetFileEditingAutoSave(value); window.dispatchEvent(new Event('config-changed')) } catch (e) { console.warn('设置自动保存失败:', e) }
}

async function handleShowToolbarChange(value: boolean) {
  showToolbar.value = value
  try { await SetShowToolbar(value); window.dispatchEvent(new Event('config-changed')) } catch (e) { console.warn('切换工具栏失败:', e) }
}

// personalize/copyOnSelect/cursorBlink 走 SetTerminalConfig 整包(与顶级菜单 setTermField 同一口径)
async function setTermFieldSync(field: 'personalize' | 'copyOnSelect' | 'cursorBlink', value: boolean) {
  try {
    const cfg = JSON.parse(await GetConfig())
    const t = cfg?.terminal ?? {}
    await SetTerminalConfig(JSON.stringify({
      showToolbar: cfg?.view?.showToolbar ?? true,
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
    window.dispatchEvent(new Event('config-changed'))
  } catch (e) { console.warn('切换终端选项失败:', e) }
}

async function handleShowAssistantChange(value: boolean) {
  showAssistant.value = value
  try { await SetShowAssistant(value); window.dispatchEvent(new Event('config-changed')) } catch (e) { console.warn('切换智能助手失败:', e) }
}

async function handlePickWallpaper() {
  const path = await OpenFileDialog(t('settings.wallPickerTitle'), t('settings.imageType'), '*.png;*.jpg;*.jpeg;*.gif;*.bmp;*.webp')
  if (!path) return
  wallpaperPath.value = path
  try { await SetWallpaper(path); window.dispatchEvent(new Event('config-changed')) } catch (e) { console.warn('设置壁纸失败:', e) }
}

async function handlePickTermBgImage() {
  const path = await OpenFileDialog(t('settings.bgPickerTitle'), t('settings.imageType'), '*.png;*.jpg;*.jpeg;*.gif;*.bmp;*.webp')
  if (!path) return
  termForm.bgImage = path
}

async function handleRemoveWallpaper() {
  wallpaperPath.value = ''
  try { await SetWallpaper(''); window.dispatchEvent(new Event('config-changed')) } catch (e) { console.warn('移除壁纸失败:', e) }
}

async function loadConfig() {
  try {
    const cfg = JSON.parse(await GetConfig())
    tabOrientation.value = cfg.view?.tabOrientation ?? 'horizontal'
    closeNoConfirm.value = cfg.view?.closeConfirm === false
    panelOpacity.value = cfg.view?.panelOpacity ?? 100
    wallpaperPath.value = cfg.view?.wallpaper || ''
    showSerial.value = cfg.view?.showSerial ?? true
    showHelp.value = cfg.view?.showHelp ?? true
    customTitlebar.value = cfg.view?.customTitlebar ?? true
    language.value = cfg.language ?? 'zh-CN'
    autoSave.value = cfg.fileEditing?.autoSave ?? true
    // 视图 tab 同步项(与顶级菜单视图菜单一致)
    showToolbar.value = cfg.view?.showToolbar ?? true
    personalize.value = cfg.terminal?.personalize ?? false
    copyOnSelect.value = cfg.terminal?.copyOnSelect ?? true
    cursorBlink.value = cfg.terminal?.cursorBlink ?? true
    showAssistant.value = cfg.view?.showAssistant ?? false
    loadTermForm(cfg)
  } catch {}
}

onMounted(() => {
  loadConfig()
  GetVersion().then(v => { appVersion.value = v }).catch(() => {})
})
watch(() => props.show, (val) => { if (val) { loadConfig() } })
</script>

<template>
  <n-modal :show="show" @update:show="(v) => { if (!v) emit('close') }" :mask-closable="false" :auto-focus="false" content-style="padding:0" style="width: 680px">
    <div class="settings-dialog">
      <button class="settings-close" @click="emit('close')">
        <n-icon :size="16" :component="CloseOutline" />
      </button>
      <div class="settings-nav">
        <div class="settings-title">{{ t('settings.title') }}</div>
        <div v-for="item in navItems" :key="item.key"
          class="settings-nav-item"
          :class="{ active: activeNav === item.key }"
          @click="activeNav = item.key">
          {{ item.label }}
        </div>
      </div>
      <div class="settings-content">
          <div v-if="activeNav === 'general'">
            <div class="setting-item">
              <div class="setting-label">{{ t('settings.language') }}</div>
              <n-select :value="language" :options="languageOptions" size="small" style="width: 160px" @update:value="handleLanguageChange" />
            </div>
            <div class="setting-item" style="margin-top: 12px;">
              <div class="setting-label">{{ t('settings.themeMode') }}</div>
              <n-radio-group :value="themeMode" size="small" @update:value="handleThemeChange">
                <n-radio-button value="dark">{{ t('settings.themeDark') }}</n-radio-button>
                <n-radio-button value="light">{{ t('settings.themeLight') }}</n-radio-button>
                <n-radio-button value="auto">{{ t('settings.themeAuto') }}</n-radio-button>
              </n-radio-group>
            </div>
            <div class="setting-item" style="margin-top: 12px;">
              <div class="setting-label">{{ t('settings.accentColor') }}</div>
              <div style="display: flex; align-items: center; gap: 8px; flex-wrap: wrap;">
                <div v-for="p in ACCENT_PRESETS" :key="p.value" class="accent-swatch"
                     :class="{ active: accent === p.value }" :style="{ background: p.value }"
                     :title="p.label" @click="handleAccentChange(p.value)" />
                <n-color-picker :value="accent" :show-alpha="false" size="small" style="width: 88px"
                                :modes="['hex']" :on-complete="handleAccentChange" />
              </div>
            </div>
            <div class="setting-item" style="margin-top: 12px;">
              <div class="setting-label">{{ t('settings.panelOpacity') }}</div>
              <div style="display: flex; align-items: center; gap: 8px;">
                <n-slider :value="panelOpacity" :min="30" :max="100" style="width: 220px" @update:value="handlePanelOpacityChange" />
                <span style="font-size: 12px; color: var(--icon-color); width: 36px;">{{ panelOpacity }}%</span>
              </div>
            </div>
            <div class="setting-item" style="margin-top: 12px;">
              <div class="setting-label">{{ t('settings.wallpaper') }}</div>
              <div style="display: flex; align-items: center; gap: 8px;">
                <n-button size="small" @click="handlePickWallpaper">{{ t('settings.pickImage') }}</n-button>
                <n-button v-if="wallpaperPath" size="small" @click="handleRemoveWallpaper">{{ t('settings.removeWallpaper') }}</n-button>
                <span v-if="wallpaperPath" style="font-size: 12px; color: var(--icon-color); max-width: 220px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">{{ wallpaperPath }}</span>
                <span v-else style="font-size: 12px; color: var(--icon-color);">{{ t('settings.defaultBg') }}</span>
              </div>
            </div>
          </div>
          <!-- 视图: 与顶级菜单「视图」下拉完全同步(顺序/功能/变量) -->
          <div v-if="activeNav === 'view'">
            <div class="setting-item">
              <div class="setting-label">{{ t('settings.serialManager') }}</div>
              <n-switch :value="showSerial" size="small" @update:value="handleShowSerialChange" />
            </div>
            <div class="setting-item">
              <div class="setting-label">{{ t('settings.help') }}</div>
              <n-switch :value="showHelp" size="small" @update:value="handleShowHelpChange" />
            </div>
            <div class="setting-item">
              <div class="setting-label">{{ t('settings.customTitlebar') }}<span class="setting-desc">{{ t('settings.customTitlebarDesc') }}</span></div>
              <n-switch :value="customTitlebar" size="small" @update:value="handleCustomTitlebarChange" />
            </div>
            <div class="settings-divider"></div>
            <div class="setting-item">
              <div class="setting-label">{{ t('settings.toolbarSwitch') }}</div>
              <n-switch :value="showToolbar" size="small" @update:value="handleShowToolbarChange" />
            </div>
            <div class="setting-item">
              <div class="setting-label">{{ t('settings.personalize') }}</div>
              <n-switch :value="personalize" size="small" @update:value="(v: boolean) => setTermFieldSync('personalize', v)" />
            </div>
            <div class="setting-item">
              <div class="setting-label">{{ t('settings.copyOnSelect') }}</div>
              <n-switch :value="copyOnSelect" size="small" @update:value="(v: boolean) => setTermFieldSync('copyOnSelect', v)" />
            </div>
            <div class="setting-item">
              <div class="setting-label">{{ t('settings.cursorBlink') }}</div>
              <n-switch :value="cursorBlink" size="small" @update:value="(v: boolean) => setTermFieldSync('cursorBlink', v)" />
            </div>
            <div class="settings-divider"></div>
            <div class="setting-item">
              <div class="setting-label">{{ t('settings.autoSave') }}</div>
              <n-switch :value="autoSave" size="small" @update:value="handleAutoSaveChange" />
            </div>
            <div class="setting-item">
              <div class="setting-label">{{ t('settings.noCloseConfirm') }}</div>
              <n-switch :value="closeNoConfirm" size="small" @update:value="handleCloseNoConfirmChange" />
            </div>
            <div class="settings-divider"></div>
            <div class="setting-item">
              <div class="setting-label">{{ t('settings.assistant') }}</div>
              <n-switch :value="showAssistant" size="small" @update:value="handleShowAssistantChange" />
            </div>
          </div>
          <div v-if="activeNav === 'terminal'" class="term-settings">
            <div class="term-scroller">
              <div class="setting-item">
                <div class="setting-label">{{ t('settings.toolbarSwitch') }}</div>
                <n-switch :value="termForm.showToolbar" size="small" @update:value="(v: boolean) => { termForm.showToolbar = v }" />
              </div>
              <div class="setting-item" style="margin-top: 4px;">
                <div class="setting-label">{{ t('settings.personalize') }}</div>
                <n-switch :value="termForm.personalize" size="small" @update:value="(v: boolean) => { termForm.personalize = v }" />
              </div>
              <div class="term-personal" :class="{ disabled: !termForm.personalize }">
                <div class="setting-item">
                  <div class="setting-label">{{ t('settings.fontColor') }}</div>
                  <n-color-picker :value="termForm.fontColor" :show-alpha="false" :disabled="!termForm.personalize" size="small" style="width: 88px" @update:value="(v: string) => { termForm.fontColor = v }" />
                </div>
                <div class="setting-item">
                  <div class="setting-label">{{ t('settings.termBgColor') }}</div>
                  <n-color-picker :value="termForm.bgColor" :show-alpha="false" :disabled="!termForm.personalize" size="small" style="width: 88px" @update:value="(v: string) => { termForm.bgColor = v }" />
                </div>
                <div class="setting-item">
                  <div class="setting-label">{{ t('settings.termBgOpacity') }}</div>
                  <div style="display: flex; align-items: center; gap: 8px;">
                    <n-slider :value="termForm.bgOpacity" :min="0" :max="100" :disabled="!termForm.personalize" style="width: 160px" @update:value="(v: number) => { termForm.bgOpacity = v }" />
                    <span style="font-size: 12px; color: var(--icon-color); width: 36px;">{{ termForm.bgOpacity }}%</span>
                  </div>
                </div>
                <div class="setting-item">
                  <div class="setting-label">{{ t('settings.termBgImage') }}<span class="setting-desc">{{ t('settings.bgImageDesc') }}</span></div>
                  <div style="display: flex; align-items: center; gap: 8px; max-width: 300px;">
                    <n-button size="small" :disabled="!termForm.personalize" @click="handlePickTermBgImage">{{ t('settings.pickImage') }}</n-button>
                    <n-button v-if="termForm.bgImage" size="small" :disabled="!termForm.personalize" @click="termForm.bgImage = ''">{{ t('common.clear') }}</n-button>
                    <span v-if="termForm.bgImage" style="font-size: 12px; color: var(--icon-color); max-width: 150px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;" :title="termForm.bgImage">{{ termForm.bgImage }}</span>
                    <span v-else style="font-size: 12px; color: var(--icon-color);">{{ t('common.none') }}</span>
                  </div>
                </div>
                <div class="setting-item">
                  <div class="setting-label">{{ t('settings.font') }}</div>
                  <n-auto-complete :value="termForm.fontFamily" :options="fontOptions" :disabled="!termForm.personalize" size="small" placeholder="Cascadia Code" style="width: 200px" @update:value="(v: string) => { termForm.fontFamily = v }" />
                </div>
                <div class="setting-item">
                  <div class="setting-label">{{ t('settings.fontSize') }}</div>
                  <div style="display: flex; align-items: center; gap: 6px;">
                    <n-input-number :value="termForm.fontSize" :min="10" :max="32" :disabled="!termForm.personalize" size="small" style="width: 90px" @update:value="(v: number | null) => { if (v !== null) termForm.fontSize = v }" />
                    <span style="font-size: 12px; color: var(--icon-color);">px</span>
                  </div>
                </div>
                <div class="setting-item">
                  <div class="setting-label">{{ t('settings.lineHeight') }}</div>
                  <div style="display: flex; align-items: center; gap: 8px;">
                    <n-slider :value="termForm.lineHeight" :min="0.8" :max="2" :step="0.05" :disabled="!termForm.personalize" style="width: 160px" @update:value="(v: number) => { termForm.lineHeight = v }" />
                    <span style="font-size: 12px; color: var(--icon-color); width: 36px;">{{ termForm.lineHeight.toFixed(2) }}</span>
                  </div>
                </div>
                <div class="setting-item">
                  <div class="setting-label">{{ t('settings.copyOnSelect') }}</div>
                  <n-switch :value="termForm.copyOnSelect" size="small" @update:value="(v: boolean) => { termForm.copyOnSelect = v }" />
                </div>
                <div class="setting-item">
                  <div class="setting-label">{{ t('settings.cursorBlink') }}</div>
                  <n-switch :value="termForm.cursorBlink" size="small" :disabled="!termForm.personalize" @update:value="(v: boolean) => { termForm.cursorBlink = v }" />
                </div>
                <div class="setting-item">
                  <div class="setting-label">{{ t('settings.cursorStyle') }}</div>
                  <n-select :value="termForm.cursorStyle" :options="cursorStyleOptions" :disabled="!termForm.personalize" size="small" style="width: 140px" @update:value="(v: string) => { termForm.cursorStyle = v }" />
                </div>
              </div>
              <div class="setting-item" style="margin-top: 4px;">
                <div class="setting-label">{{ t('settings.scrollback') }}<span class="setting-desc">{{ t('settings.scrollbackDesc') }}</span></div>
                <div style="display: flex; align-items: center; gap: 6px;">
                  <n-input-number :value="termForm.scrollback" :min="100" :max="100000" size="small" style="width: 110px" @update:value="(v: number | null) => { if (v !== null) termForm.scrollback = v }" />
                  <span style="font-size: 12px; color: var(--icon-color);">{{ t('settings.rows') }}</span>
                </div>
              </div>
            </div>
            <div class="term-actions">
              <span v-if="termError" class="term-error">{{ termError }}</span>
              <n-button size="small" @click="resetTermForm">{{ t('settings.reset') }}</n-button>
              <n-button size="small" @click="cancelTermForm">{{ t('common.cancel') }}</n-button>
              <n-button size="small" type="primary" @click="confirmTermForm">{{ t('common.confirm') }}</n-button>
            </div>
          </div>
          <div v-if="activeNav === 'fileEditing'">
            <div class="setting-item">
              <div class="setting-label">{{ t('settings.autoSave') }}</div>
              <n-switch :value="autoSave" size="small" @update:value="handleAutoSaveChange" />
            </div>
            <div class="setting-item" style="margin-top: 4px;">
              <div class="setting-label">{{ t('settings.autoSaveInterval') }}</div>
              <span style="font-size: 12px; color: var(--icon-color);">{{ t('settings.autoSaveHint') }}</span>
            </div>
            <div class="setting-item" style="margin-top: 4px;">
              <span style="font-size: 12px; color: var(--icon-color);">{{ t('settings.manualSaveHint') }}</span>
            </div>
          </div>
          <div v-if="activeNav === 'tabs'">
            <div class="setting-item">
              <div class="setting-label">{{ t('settings.tabDirection') }}</div>
              <n-radio-group :value="tabOrientation" size="small" @update:value="handleTabOrientationChange">
                <n-radio-button value="horizontal">{{ t('settings.horizontal') }}</n-radio-button>
                <n-radio-button value="vertical">{{ t('settings.vertical') }}</n-radio-button>
              </n-radio-group>
            </div>
            <div class="setting-item" style="margin-top: 12px;">
              <div class="setting-label">{{ t('settings.noCloseConfirm') }}</div>
              <n-checkbox :checked="closeNoConfirm" @update:checked="handleCloseNoConfirmChange" />
            </div>
          </div>
          <!-- MCP 服务: 开关/状态 + 连接 + 危险拦截 + 执行参数 + 危险字典 + 审计日志 -->
          <div v-if="activeNav === 'mcp'" class="mcp-settings">
            <div class="setting-item">
              <div class="setting-label">{{ t('mcp.enable') }}<span class="setting-desc">{{ t('mcp.enableDesc') }}</span></div>
              <div style="display: flex; align-items: center; gap: 10px; flex-shrink: 0;">
                <n-tag :type="mcpStateType" size="small" round>{{ mcpStateText }}</n-tag>
                <n-switch :value="mcpStatus.enabled" :loading="mcpSwitching" size="small" @update:value="handleMcpToggle" />
                <n-button size="small" :type="mcpStatus.state === 'paused' ? 'primary' : 'default'" :disabled="mcpStatus.state === 'stopped'"
                          :title="mcpStatus.state === 'paused' ? t('mcp.resume') : t('mcp.pause')"
                          @click="mcpStatus.state === 'paused' ? handleMcpResume() : handleMcpPause()">
                  <template #icon><n-icon :size="13" :component="mcpStatus.state === 'paused' ? PlayOutline : PauseOutline" /></template>
                </n-button>
              </div>
            </div>

            <div class="settings-divider"></div>
            <div class="mcp-block">
              <div class="mcp-block-title">{{ t('mcp.connection') }}</div>
              <div class="mcp-conn-row">
                <span class="mcp-conn-label">URL</span>
                <span class="mcp-conn-value mono">{{ mcpStatus.url || '--' }}</span>
                <n-button text size="small" @click="copyText(mcpStatus.url)"><n-icon :size="14" :component="CopyOutline" /></n-button>
              </div>
              <div class="mcp-conn-row">
                <span class="mcp-conn-label">{{ t('mcp.tokenLabel') }}</span>
                <span class="mcp-conn-value mono">{{ maskedToken(mcpStatus.token) || '--' }}</span>
                <n-button text size="small" @click="mcpShowToken = !mcpShowToken">{{ mcpShowToken ? t('mcp.hide') : t('mcp.show') }}</n-button>
                <n-button text size="small" @click="copyText(mcpStatus.token)"><n-icon :size="14" :component="CopyOutline" /></n-button>
                <n-button text size="small" :title="t('mcp.resetToken')" @click="handleMcpResetToken"><n-icon :size="14" :component="RefreshOutline" /></n-button>
              </div>
              <div class="mcp-hint">{{ t('mcp.connDesc') }}</div>
            </div>

            <div class="settings-divider"></div>
            <div class="mcp-block">
              <div class="mcp-block-title">{{ t('mcp.riskTitle') }}</div>
              <div class="mcp-risk-item"><span class="dot dot-red" /><span>{{ t('mcp.riskBlockedDesc') }}</span></div>
              <div class="mcp-risk-item"><span class="dot dot-green" /><span>{{ t('mcp.riskAutoDesc') }}</span></div>
            </div>

            <div class="settings-divider"></div>
            <div class="mcp-block">
              <div class="mcp-block-title">{{ t('mcp.execTuning') }}</div>
              <div class="setting-item">
                <div class="setting-label">{{ t('mcp.opDelay') }}<span class="setting-desc">{{ t('mcp.opDelayDesc') }}</span></div>
                <n-input-number v-model:value="mcpTuning.opDelayMs" size="small" :min="0" :max="10000" :step="100" style="width: 120px; flex-shrink: 0" />
              </div>
              <div class="setting-item">
                <div class="setting-label">{{ t('mcp.batchInterval') }}<span class="setting-desc">{{ t('mcp.batchIntervalDesc') }}</span></div>
                <n-input-number v-model:value="mcpTuning.batchIntervalMs" size="small" :min="50" :max="10000" :step="50" style="width: 120px; flex-shrink: 0" />
              </div>
              <div class="setting-item">
                <div class="setting-label">{{ t('mcp.auditRetention') }}<span class="setting-desc">{{ t('mcp.auditRetentionDesc') }}</span></div>
                <n-input-number v-model:value="mcpTuning.auditRetentionDays" size="small" :min="1" :max="365" style="width: 120px; flex-shrink: 0" />
              </div>
              <div class="setting-item">
                <div class="setting-label">{{ t('mcp.terminalReadMax') }}<span class="setting-desc">{{ t('mcp.terminalReadMaxDesc') }}</span></div>
                <n-input-number v-model:value="mcpTuning.terminalReadMaxKB" size="small" :min="1" :max="256" style="width: 120px; flex-shrink: 0" />
              </div>
              <div class="mcp-actions">
                <n-button size="small" type="primary" :loading="mcpTuningSaving" @click="handleMcpSaveTuning">
                  <template #icon><n-icon :size="13" :component="SaveOutline" /></template>
                  {{ t('mcp.saveTuning') }}
                </n-button>
              </div>
            </div>

            <div class="settings-divider"></div>
            <div class="mcp-block">
              <div class="mcp-block-title">{{ t('mcp.dangerousDict') }}</div>
              <div class="mcp-hint" style="margin-bottom: 8px">{{ t('mcp.dangerousDictDesc') }}</div>
              <n-input
                v-model:value="mcpDangerousText"
                type="textarea"
                size="small"
                :rows="7"
                :placeholder="t('mcp.dangerousPlaceholder')"
                class="mcp-pattern-editor mono"
              />
              <div class="mcp-actions">
                <span class="mcp-hint">{{ t('mcp.dangerousCount', { count: mcpDangerousCount() }) }}</span>
                <div class="mcp-btn-group">
                  <n-button size="small" :disabled="mcpDangerousSaving" @click="handleMcpSaveDangerous(true)">
                    <template #icon><n-icon :size="13" :component="RefreshOutline" /></template>
                    {{ t('mcp.restoreDefault') }}
                  </n-button>
                  <n-button size="small" type="primary" :loading="mcpDangerousSaving" @click="handleMcpSaveDangerous()">
                    <template #icon><n-icon :size="13" :component="SaveOutline" /></template>
                    {{ t('mcp.saveTuning') }}
                  </n-button>
                </div>
              </div>
            </div>

            <div class="settings-divider"></div>
            <div class="mcp-block">
              <div class="mcp-block-title">{{ t('mcp.tabLogs') }}</div>
              <div class="mcp-log-toolbar">
                <div class="mcp-log-filter-group">
                  <button
                    v-for="opt in mcpRiskFilterOptions" :key="'r-' + opt.key"
                    class="mcp-filter-btn"
                    :class="{ active: mcpRiskFilter === opt.key, [`risk-${opt.key}`]: opt.key !== 'all' }"
                    @click="mcpRiskFilter = opt.key as any"
                  >{{ opt.label }}</button>
                </div>
                <n-button size="small" quaternary :loading="mcpPdfExporting" @click="handleMcpExportPdf">
                  <template #icon><n-icon :size="13" :component="DocumentTextOutline" /></template>
                  PDF
                </n-button>
              </div>
              <div class="mcp-log-list">
                <div v-if="mcpFilteredLogs.length === 0" class="mcp-hint" style="padding: 12px 0; text-align: center">
                  {{ mcpReversedLogs.length === 0 ? t('mcp.noLogs') : t('mcp.noFilterMatch') }}
                </div>
                <div
                  v-for="log in mcpFilteredLogs" :key="log.id"
                  class="mcp-log-item"
                  :class="{ clickable: !!log.detail }"
                  @click="log.detail && toggleMcpLogDetail(log.id)"
                >
                  <div class="mcp-log-line1">
                    <span class="mcp-log-ts">{{ log.ts }}</span>
                    <span class="mcp-log-source">{{ mcpSourceText(log.source) }}</span>
                    <n-tag :type="mcpRiskType(log.risk)" size="small">{{ mcpRiskText(log.risk) }}</n-tag>
                    <n-tag :type="mcpDecisionType(log.decision)" size="small">{{ mcpDecisionText(log.decision) }}</n-tag>
                    <span class="mcp-log-action">{{ log.action }}</span>
                    <n-icon v-if="log.detail" :size="12" :component="ChevronDownOutline" class="mcp-log-chev" :class="{ expanded: mcpLogExpanded[log.id] }" />
                  </div>
                  <div class="mcp-log-subject">{{ log.subject }}</div>
                  <pre v-if="log.detail && mcpLogExpanded[log.id]" class="mcp-log-detail">{{ log.detail }}</pre>
                </div>
              </div>
            </div>
          </div>
          <div v-if="activeNav === 'about'">
            <div class="about-item"><span class="about-label">{{ t('settings.aboutName') }}</span><span class="about-value">AceShell</span></div>
            <div class="about-item"><span class="about-label">{{ t('settings.aboutVersion') }}</span><span class="about-value"><n-tag size="tiny" type="info">v{{ appVersion }}</n-tag></span></div>
            <div class="about-item"><span class="about-label">{{ t('settings.aboutDesc') }}</span><span class="about-value">{{ t('settings.descValue') }}</span></div>
            <div class="about-item"><span class="about-label">{{ t('settings.aboutTech') }}</span><span class="about-value">Go + Wails v3 + Vue 3 + TypeScript</span></div>
            <div class="about-item"><span class="about-label">{{ t('settings.aboutFeatures') }}</span><span class="about-value">{{ t('settings.featuresValue') }}</span></div>
            <div class="about-item"><span class="about-label">{{ t('settings.aboutHomepage') }}</span>
              <span class="about-link" @click="openExternal('https://github.com/dingtongbin/AceShell')">
                <n-icon :size="13" :component="LogoGithub" /> github.com/dingtongbin/AceShell
              </span>
            </div>
            <div class="about-item"><span class="about-label">{{ t('settings.aboutBlog') }}</span>
              <span class="about-link" @click="openExternal('https://dingtongbin.cn/')">
                <n-icon :size="13" :component="GlobeOutline" /> dingtongbin.cn
              </span>
            </div>
            <div class="about-item"><span class="about-label">{{ t('settings.aboutCopyright') }}</span>
              <span class="about-value">{{ t('settings.copyrightValue') }}</span>
            </div>
            <div class="about-item"><span class="about-label">{{ t('settings.aboutLicense') }}</span><span class="about-value"><n-tag size="tiny" type="warning">GPL-3.0</n-tag></span></div>
          </div>
      </div>
    </div>
  </n-modal>
</template>

<style scoped>
.accent-swatch {
  width: 22px;
  height: 22px;
  border-radius: 50%;
  cursor: pointer;
  border: 2px solid transparent;
  box-shadow: inset 0 0 0 1px rgba(0, 0, 0, 0.18);
  transition: transform 0.12s, border-color 0.12s;
}
.accent-swatch:hover { transform: scale(1.12); }
.accent-swatch.active {
  border-color: var(--text-color);
  box-shadow: 0 0 0 2px var(--primary-color);
}

.settings-dialog {
  display: flex;
  height: 420px;
  background: var(--body-bg);
  border-radius: 8px;
  overflow: hidden;
  position: relative;
}
.settings-nav {
  width: 110px;
  flex-shrink: 0;
  border-right: 1px solid var(--border-color);
  padding: 16px 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.settings-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-color);
  padding: 0 16px 12px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}
.settings-nav-item {
  padding: 6px 16px;
  font-size: 12px;
  color: var(--text-color);
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
  user-select: none;
  text-align: left;
}
.settings-nav-item:hover {
  background: rgba(255,255,255,0.06);
}
.settings-nav-item.active {
  background: rgba(0, 120, 212, 0.2);
  color: var(--primary-color);
  font-weight: 500;
}
.settings-content {
  flex: 1;
  padding: 20px 24px;
  min-width: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
}
.settings-close {
  position: absolute;
  top: 8px;
  right: 8px;
  z-index: 10;
  width: 24px;
  height: 24px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: none;
  background: transparent;
  color: var(--text-color);
  cursor: pointer;
  border-radius: 4px;
  font-size: 16px;
  line-height: 1;
  transition: background 0.15s;
}
.settings-close:hover {
  background: rgba(255,255,255,0.1);
  color: var(--text-color);
}
.setting-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 0;
}
.setting-label {
  font-size: 13px;
  color: var(--text-color);
}
.about-item {
  display: flex;
  align-items: center;
  padding: 8px 0;
  gap: 12px;
}
.about-label {
  font-size: 12px;
  color: #999;
  width: 64px;
  flex-shrink: 0;
}
.about-value {
  font-size: 13px;
  color: var(--text-color);
}
.about-link {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: var(--primary-color);
  cursor: pointer;
  user-select: none;
}
.about-link:hover {
  text-decoration: underline;
  color: var(--primary-color);
}
.plugin-card {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 10px;
  padding: 8px 10px;
  border-radius: 6px;
  background: var(--panel-bg, rgba(128, 128, 128, 0.06));
}
/* 操作按钮禁止收缩: 空间不足时整行换行, 杜绝溢出弹窗导致点不到 */
.plugin-card .n-button,
.plugin-card .n-switch {
  flex-shrink: 0;
}
.plugin-card-icon {
  width: 20px;
  height: 20px;
  flex-shrink: 0;
  object-fit: contain;
}
.plugin-card-main {
  flex: 1;
  min-width: 0;
}
.plugin-card-name {
  font-size: 12px;
  color: var(--text-color);
  display: flex;
  align-items: center;
  gap: 6px;
}
.plugin-card-status {
  font-size: 11px;
  color: var(--icon-color);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.plugin-status-error {
  color: var(--danger-color);
}
.term-settings {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
}
.term-scroller {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding-right: 4px;
}
.term-personal.disabled {
  opacity: 0.45;
  pointer-events: none;
}
.term-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  flex-shrink: 0;
  padding-top: 12px;
  margin-top: 12px;
  border-top: 1px solid var(--border-color);
}
.term-error {
  font-size: 12px;
  color: var(--danger-color);
  margin-right: auto;
}
.setting-desc {
  font-size: 11px;
  color: var(--icon-color);
  font-weight: normal;
}
.settings-divider {
  height: 1px;
  background: var(--border-color);
  margin: 10px 0;
}

/* ==================== MCP 设置页 ==================== */
.mcp-block { padding: 2px 0; }
.mcp-block-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--icon-color);
  text-transform: uppercase;
  letter-spacing: 0.5px;
  margin-bottom: 8px;
}
.mcp-hint { font-size: 11px; color: var(--icon-color); line-height: 1.6; margin-top: 2px; }
.mcp-conn-row { display: flex; align-items: center; gap: 8px; padding: 5px 0; }
.mcp-conn-label { font-size: 12px; color: var(--icon-color); flex-shrink: 0; width: 44px; }
.mcp-conn-value { font-size: 13px; color: var(--text-color); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex: 1; min-width: 0; }
.mono { font-family: Consolas, 'Courier New', monospace; }
.mcp-risk-item { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--text-color); line-height: 2; }
.dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
.dot-red { background: var(--danger-color); }
.dot-green { background: var(--primary-color); }
.mcp-btn-group { display: flex; gap: 8px; flex-shrink: 0; }
.mcp-actions { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin-top: 10px; }
.mcp-pattern-editor { font-size: 12px; }

.mcp-log-toolbar { display: flex; align-items: center; gap: 10px; padding: 4px 0 10px; flex-wrap: wrap; }
.mcp-log-filter-group { display: flex; align-items: center; background: var(--hover-bg, rgba(255, 255, 255, 0.04)); border-radius: 6px; padding: 2px; gap: 1px; }
.mcp-filter-btn { border: none; background: transparent; color: var(--icon-color); font-size: 12px; padding: 3px 10px; border-radius: 5px; cursor: pointer; transition: background 0.15s, color 0.15s; white-space: nowrap; }
.mcp-filter-btn:hover { color: var(--text-color); }
.mcp-filter-btn.active { background: color-mix(in srgb, var(--primary-color) 25%, transparent); color: var(--primary-color); }
.mcp-filter-btn.active.risk-blocked { background: rgba(228, 88, 88, 0.22); color: var(--danger-color); }
.mcp-filter-btn.active.risk-allowed { background: rgba(78, 201, 176, 0.2); color: var(--primary-color); }
.mcp-log-list {
  max-height: 260px;
  overflow-y: auto;
  border: 1px solid var(--border-color);
  border-radius: 6px;
  background: var(--hover-bg, rgba(128, 128, 128, 0.05));
}
.mcp-log-item { padding: 7px 10px; border-bottom: 1px solid var(--border-color); }
.mcp-log-item:last-child { border-bottom: none; }
.mcp-log-item.clickable { cursor: pointer; }
.mcp-log-item.clickable:hover { background: var(--hover-bg, rgba(255, 255, 255, 0.03)); }
.mcp-log-line1 { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.mcp-log-ts { font-size: 11px; color: var(--icon-color); font-family: Consolas, 'Courier New', monospace; }
.mcp-log-source { font-size: 11px; color: var(--icon-color); }
.mcp-log-action { font-size: 12px; font-weight: 600; color: var(--text-color); }
.mcp-log-subject { font-size: 12px; color: var(--icon-color); margin-top: 3px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: Consolas, 'Courier New', monospace; }
.mcp-log-chev { color: var(--icon-color); transition: transform 0.15s; margin-left: auto; flex-shrink: 0; }
.mcp-log-chev.expanded { transform: rotate(180deg); }
.mcp-log-detail { margin: 4px 0 0; padding: 6px 8px; background: rgba(0, 0, 0, 0.3); border-radius: 4px; font-size: 11.5px; line-height: 1.6; color: var(--text-color); white-space: pre-wrap; word-break: break-all; max-height: 180px; overflow: auto; font-family: Consolas, 'Courier New', monospace; }
</style>
