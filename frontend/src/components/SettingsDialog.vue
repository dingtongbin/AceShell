<script setup lang="ts">
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { NModal, NSwitch, NTag, NRadioGroup, NRadioButton, NButton, NInput, NCheckbox, NIcon, NSlider, NColorPicker, NInputNumber, NAutoComplete, NSelect, useMessage } from 'naive-ui'
import { CloseOutline, LogoGithub, GlobeOutline } from '@vicons/ionicons5'
import { useTheme } from '../stores/theme'
import { GetConfig, SetTabOrientation, SetTheme, SetCloseConfirm, SetPanelOpacity, SetWallpaper, SetTerminalConfig, SetShowSerial, SetShowHelp, SetFileEditingAutoSave, SetLanguage, SetCustomTitlebar, SetShowToolbar, SetShowAssistant } from '../../bindings/changeme/internal/services/configservice.js'
import { OpenFileDialog } from '../../bindings/changeme/internal/services/windowservice.js'
import { OpenUrl as BrowserOpenUrl } from '../../bindings/changeme/internal/services/browserservice.js'
import { GetVersion } from '../../bindings/changeme/internal/services/versionservice.js'
import { setLocale, languageOptions } from '../i18n'
import { useI18n } from 'vue-i18n'

const message = useMessage()
const { t } = useI18n()

const { themeMode, setThemeMode } = useTheme()

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
  { key: 'about', label: t('settings.nav.about') },
])

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
watch(() => props.show, (val) => { if (val) loadConfig() })
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
.settings-dialog {
  display: flex;
  height: 420px;
  background: var(--body-bg, #1e1e1e);
  border-radius: 8px;
  overflow: hidden;
  position: relative;
}
.settings-nav {
  width: 110px;
  flex-shrink: 0;
  border-right: 1px solid var(--border-color, #3c3c3c);
  padding: 16px 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.settings-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-color, #d4d4d4);
  padding: 0 16px 12px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}
.settings-nav-item {
  padding: 6px 16px;
  font-size: 12px;
  color: var(--text-color, #d4d4d4);
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
  color: #0078d4;
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
  color: var(--text-color, #999);
  cursor: pointer;
  border-radius: 4px;
  font-size: 16px;
  line-height: 1;
  transition: background 0.15s;
}
.settings-close:hover {
  background: rgba(255,255,255,0.1);
  color: var(--text-color, #d4d4d4);
}
.setting-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 0;
}
.setting-label {
  font-size: 13px;
  color: var(--text-color, #d4d4d4);
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
  color: var(--text-color, #d4d4d4);
}
.about-link {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: #0078d4;
  cursor: pointer;
  user-select: none;
}
.about-link:hover {
  text-decoration: underline;
  color: #4ec9b0;
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
  border-top: 1px solid var(--border-color, #3c3c3c);
}
.term-error {
  font-size: 12px;
  color: #e45858;
  margin-right: auto;
}
.setting-desc {
  font-size: 11px;
  color: var(--icon-color, #888);
  font-weight: normal;
}
.settings-divider {
  height: 1px;
  background: var(--border-color, #3c3c3c);
  margin: 10px 0;
}
</style>
