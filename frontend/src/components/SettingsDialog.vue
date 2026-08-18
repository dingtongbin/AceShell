<script setup lang="ts">
import { ref, reactive, onMounted, watch } from 'vue'
import { NModal, NSwitch, NTag, NRadioGroup, NRadioButton, NButton, NInput, NCheckbox, NIcon, NSlider, NColorPicker, NInputNumber, NAutoComplete, NSelect, useMessage } from 'naive-ui'
import { CloseOutline, LogoGithub, GlobeOutline } from '@vicons/ionicons5'
import { useTheme } from '../stores/theme'
import { GetConfig, SetTabOrientation, SetTheme, SetCloseConfirm, SetPanelOpacity, SetWallpaper, SetTerminalConfig, SetShowSerial, SetShowHelp, SetShowGithub, SetFileEditingAutoSave } from '../../bindings/changeme/internal/services/configservice.js'
import { OpenFileDialog } from '../../bindings/changeme/internal/services/windowservice.js'
import { OpenUrl as BrowserOpenUrl } from '../../bindings/changeme/internal/services/browserservice.js'
import { GetVersion } from '../../bindings/changeme/internal/services/versionservice.js'

const message = useMessage()

const { themeMode, setThemeMode } = useTheme()

// 用系统默认浏览器打开外部链接
async function openExternal(url: string) {
  const err = await BrowserOpenUrl('', url)
  if (err) message.error('打开链接失败: ' + err)
}

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ (e: 'close'): void }>()

const activeNav = ref('general')
const tabOrientation = ref('horizontal')

const closeNoConfirm = ref(false)
const panelOpacity = ref(70)
const wallpaperPath = ref('')
const showSerial = ref(true)
const showHelp = ref(true)
const showGithub = ref(true)
const autoSave = ref(true)
const appVersion = ref('0.1.0')

const navItems = [
  { key: 'general', label: '通用' },
  { key: 'view', label: '视图' },
  { key: 'terminal', label: '终端' },
  { key: 'fileEditing', label: '文件编辑' },
  { key: 'tabs', label: '标签' },
  { key: 'about', label: '关于' },
]

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

const cursorStyleOptions = [
  { label: '竖线（默认）', value: 'bar' },
  { label: '方块', value: 'block' },
  { label: '横线', value: 'underline' },
]

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
  if (!/^#[0-9a-fA-F]{6}$/.test(termForm.fontColor)) { termError.value = '字体颜色格式不正确'; return }
  if (!/^#[0-9a-fA-F]{6}$/.test(termForm.bgColor)) { termError.value = '终端背景色格式不正确'; return }
  if (termForm.bgOpacity < 0 || termForm.bgOpacity > 100) { termError.value = '背景不透明度须在 0~100 之间'; return }
  if (!termForm.fontFamily.trim()) { termError.value = '字体不能为空'; return }
  if (termForm.fontSize < 10 || termForm.fontSize > 32) { termError.value = '字号须在 10~32 之间'; return }
  if (termForm.lineHeight < 0.8 || termForm.lineHeight > 2) { termError.value = '行高须在 0.8~2.0 之间'; return }
  if (termForm.scrollback < 100 || termForm.scrollback > 100000) { termError.value = '滚动缓冲须在 100~100000 行之间'; return }
  try {
    await SetTerminalConfig(JSON.stringify({ ...termForm }))
    termSaved = { ...termForm }
    window.dispatchEvent(new Event('config-changed'))
    emit('close')
  } catch (e: any) { termError.value = e.message || '保存失败' }
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

async function handleTabOrientationChange(value: string) {
  tabOrientation.value = value
  try { await SetTabOrientation(value); window.dispatchEvent(new Event('config-changed')) } catch {}
}

async function handleCloseNoConfirmChange(value: boolean) {
  closeNoConfirm.value = value
  try { await SetCloseConfirm(!value); window.dispatchEvent(new Event('config-changed')) } catch {}
}

async function handlePanelOpacityChange(value: number) {
  panelOpacity.value = value
  try { await SetPanelOpacity(value); window.dispatchEvent(new Event('config-changed')) } catch (e) { console.warn('设置透明度失败:', e) }
}

async function handleShowSerialChange(value: boolean) {
  showSerial.value = value
  try { await SetShowSerial(value); window.dispatchEvent(new Event('config-changed')) } catch (e) { console.warn('切换串口管理器失败:', e) }
}

async function handleShowHelpChange(value: boolean) {
  showHelp.value = value
  try { await SetShowHelp(value); window.dispatchEvent(new Event('config-changed')) } catch (e) { console.warn('切换帮助失败:', e) }
}

async function handleShowGithubChange(value: boolean) {
  showGithub.value = value
  try { await SetShowGithub(value); window.dispatchEvent(new Event('config-changed')) } catch (e) { console.warn('切换 GitHub 按钮失败:', e) }
}

async function handleAutoSaveChange(value: boolean) {
  autoSave.value = value
  try { await SetFileEditingAutoSave(value); window.dispatchEvent(new Event('config-changed')) } catch (e) { console.warn('设置自动保存失败:', e) }
}

async function handlePickWallpaper() {
  const path = await OpenFileDialog('选择壁纸图片', '图片', '*.png;*.jpg;*.jpeg;*.gif;*.bmp;*.webp')
  if (!path) return
  wallpaperPath.value = path
  try { await SetWallpaper(path); window.dispatchEvent(new Event('config-changed')) } catch (e) { console.warn('设置壁纸失败:', e) }
}

async function handlePickTermBgImage() {
  const path = await OpenFileDialog('选择终端背景图', '图片', '*.png;*.jpg;*.jpeg;*.gif;*.bmp;*.webp')
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
    panelOpacity.value = cfg.view?.panelOpacity || 70
    wallpaperPath.value = cfg.view?.wallpaper || ''
    showSerial.value = cfg.view?.showSerial ?? true
    showHelp.value = cfg.view?.showHelp ?? true
    showGithub.value = cfg.view?.showGithub ?? true
    autoSave.value = cfg.fileEditing?.autoSave ?? true
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
        <div class="settings-title">设置</div>
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
              <div class="setting-label">主题模式</div>
              <n-radio-group :value="themeMode" size="small" @update:value="handleThemeChange">
                <n-radio-button value="dark">暗色</n-radio-button>
                <n-radio-button value="light">亮色</n-radio-button>
                <n-radio-button value="auto">跟随系统</n-radio-button>
              </n-radio-group>
            </div>
            <div class="setting-item" style="margin-top: 12px;">
              <div class="setting-label">面板透明度</div>
              <div style="display: flex; align-items: center; gap: 8px;">
                <n-slider :value="panelOpacity" :min="30" :max="100" style="width: 220px" @update:value="handlePanelOpacityChange" />
                <span style="font-size: 12px; color: var(--icon-color); width: 36px;">{{ panelOpacity }}%</span>
              </div>
            </div>
            <div class="setting-item" style="margin-top: 12px;">
              <div class="setting-label">壁纸</div>
              <div style="display: flex; align-items: center; gap: 8px;">
                <n-button size="small" @click="handlePickWallpaper">选择图片</n-button>
                <n-button v-if="wallpaperPath" size="small" @click="handleRemoveWallpaper">移除壁纸</n-button>
                <span v-if="wallpaperPath" style="font-size: 12px; color: var(--icon-color); max-width: 220px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">{{ wallpaperPath }}</span>
                <span v-else style="font-size: 12px; color: var(--icon-color);">默认黑灰背景</span>
              </div>
            </div>
          </div>
          <div v-if="activeNav === 'view'">
            <div class="setting-item">
              <div class="setting-label">串口管理器</div>
              <n-switch :value="showSerial" size="small" @update:value="handleShowSerialChange" />
            </div>
            <div class="setting-item" style="margin-top: 12px;">
              <div class="setting-label">帮助</div>
              <n-switch :value="showHelp" size="small" @update:value="handleShowHelpChange" />
            </div>
            <div class="setting-item" style="margin-top: 12px;">
              <div class="setting-label">GitHub 按钮</div>
              <n-switch :value="showGithub" size="small" @update:value="handleShowGithubChange" />
            </div>
          </div>
          <div v-if="activeNav === 'terminal'" class="term-settings">
            <div class="term-scroller">
              <div class="setting-item">
                <div class="setting-label">工具栏开关</div>
                <n-switch :value="termForm.showToolbar" size="small" @update:value="(v: boolean) => { termForm.showToolbar = v }" />
              </div>
              <div class="setting-item" style="margin-top: 4px;">
                <div class="setting-label">终端个性化</div>
                <n-switch :value="termForm.personalize" size="small" @update:value="(v: boolean) => { termForm.personalize = v }" />
              </div>
              <div class="term-personal" :class="{ disabled: !termForm.personalize }">
                <div class="setting-item">
                  <div class="setting-label">字体颜色</div>
                  <n-color-picker :value="termForm.fontColor" :show-alpha="false" :disabled="!termForm.personalize" size="small" style="width: 88px" @update:value="(v: string) => { termForm.fontColor = v }" />
                </div>
                <div class="setting-item">
                  <div class="setting-label">终端背景色</div>
                  <n-color-picker :value="termForm.bgColor" :show-alpha="false" :disabled="!termForm.personalize" size="small" style="width: 88px" @update:value="(v: string) => { termForm.bgColor = v }" />
                </div>
                <div class="setting-item">
                  <div class="setting-label">终端背景不透明</div>
                  <div style="display: flex; align-items: center; gap: 8px;">
                    <n-slider :value="termForm.bgOpacity" :min="0" :max="100" :disabled="!termForm.personalize" style="width: 160px" @update:value="(v: number) => { termForm.bgOpacity = v }" />
                    <span style="font-size: 12px; color: var(--icon-color); width: 36px;">{{ termForm.bgOpacity }}%</span>
                  </div>
                </div>
                <div class="setting-item">
                  <div class="setting-label">终端背景图 <span class="setting-desc">（cover 等比铺满，默认无图）</span></div>
                  <div style="display: flex; align-items: center; gap: 8px; max-width: 300px;">
                    <n-button size="small" :disabled="!termForm.personalize" @click="handlePickTermBgImage">选择图片</n-button>
                    <n-button v-if="termForm.bgImage" size="small" :disabled="!termForm.personalize" @click="termForm.bgImage = ''">清除</n-button>
                    <span v-if="termForm.bgImage" style="font-size: 12px; color: var(--icon-color); max-width: 150px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;" :title="termForm.bgImage">{{ termForm.bgImage }}</span>
                    <span v-else style="font-size: 12px; color: var(--icon-color);">无</span>
                  </div>
                </div>
                <div class="setting-item">
                  <div class="setting-label">字体</div>
                  <n-auto-complete :value="termForm.fontFamily" :options="fontOptions" :disabled="!termForm.personalize" size="small" placeholder="Cascadia Code" style="width: 200px" @update:value="(v: string) => { termForm.fontFamily = v }" />
                </div>
                <div class="setting-item">
                  <div class="setting-label">字号</div>
                  <div style="display: flex; align-items: center; gap: 6px;">
                    <n-input-number :value="termForm.fontSize" :min="10" :max="32" :disabled="!termForm.personalize" size="small" style="width: 90px" @update:value="(v: number | null) => { if (v !== null) termForm.fontSize = v }" />
                    <span style="font-size: 12px; color: var(--icon-color);">px</span>
                  </div>
                </div>
                <div class="setting-item">
                  <div class="setting-label">行高</div>
                  <div style="display: flex; align-items: center; gap: 8px;">
                    <n-slider :value="termForm.lineHeight" :min="0.8" :max="2" :step="0.05" :disabled="!termForm.personalize" style="width: 160px" @update:value="(v: number) => { termForm.lineHeight = v }" />
                    <span style="font-size: 12px; color: var(--icon-color); width: 36px;">{{ termForm.lineHeight.toFixed(2) }}</span>
                  </div>
                </div>
                <div class="setting-item">
                  <div class="setting-label">选择时复制</div>
                  <n-switch :value="termForm.copyOnSelect" size="small" @update:value="(v: boolean) => { termForm.copyOnSelect = v }" />
                </div>
                <div class="setting-item">
                  <div class="setting-label">光标闪烁</div>
                  <n-switch :value="termForm.cursorBlink" size="small" :disabled="!termForm.personalize" @update:value="(v: boolean) => { termForm.cursorBlink = v }" />
                </div>
                <div class="setting-item">
                  <div class="setting-label">光标样式</div>
                  <n-select :value="termForm.cursorStyle" :options="cursorStyleOptions" :disabled="!termForm.personalize" size="small" style="width: 140px" @update:value="(v: string) => { termForm.cursorStyle = v }" />
                </div>
              </div>
              <div class="setting-item" style="margin-top: 4px;">
                <div class="setting-label">终端滚动缓冲 <span class="setting-desc">（新标签页生效）</span></div>
                <div style="display: flex; align-items: center; gap: 6px;">
                  <n-input-number :value="termForm.scrollback" :min="100" :max="100000" size="small" style="width: 110px" @update:value="(v: number | null) => { if (v !== null) termForm.scrollback = v }" />
                  <span style="font-size: 12px; color: var(--icon-color);">行</span>
                </div>
              </div>
            </div>
            <div class="term-actions">
              <span v-if="termError" class="term-error">{{ termError }}</span>
              <n-button size="small" @click="resetTermForm">重置</n-button>
              <n-button size="small" @click="cancelTermForm">取消</n-button>
              <n-button size="small" type="primary" @click="confirmTermForm">确定</n-button>
            </div>
          </div>
          <div v-if="activeNav === 'fileEditing'">
            <div class="setting-item">
              <div class="setting-label">自动保存</div>
              <n-switch :value="autoSave" size="small" @update:value="handleAutoSaveChange" />
            </div>
            <div class="setting-item" style="margin-top: 4px;">
              <div class="setting-label">自动保存间隔</div>
              <span style="font-size: 12px; color: var(--icon-color);">编辑停顿约 5 秒后自动保存</span>
            </div>
            <div class="setting-item" style="margin-top: 4px;">
              <span style="font-size: 12px; color: var(--icon-color);">Ctrl+S 可随时手动保存</span>
            </div>
          </div>
          <div v-if="activeNav === 'tabs'">
            <div class="setting-item">
              <div class="setting-label">标签方向</div>
              <n-radio-group :value="tabOrientation" size="small" @update:value="handleTabOrientationChange">
                <n-radio-button value="horizontal">水平</n-radio-button>
                <n-radio-button value="vertical">垂直</n-radio-button>
              </n-radio-group>
            </div>
            <div class="setting-item" style="margin-top: 12px;">
              <div class="setting-label">关闭标签页不弹确认</div>
              <n-checkbox :checked="closeNoConfirm" @update:checked="handleCloseNoConfirmChange" />
            </div>
          </div>
          <div v-if="activeNav === 'about'">
            <div class="about-item"><span class="about-label">应用名称</span><span class="about-value">AceShell</span></div>
            <div class="about-item"><span class="about-label">版本</span><span class="about-value"><n-tag size="tiny" type="info">v{{ appVersion }}</n-tag></span></div>
            <div class="about-item"><span class="about-label">描述</span><span class="about-value">一款永久免费的集成式 Shell 终端</span></div>
            <div class="about-item"><span class="about-label">技术栈</span><span class="about-value">Go + Wails v3 + Vue 3 + TypeScript</span></div>
            <div class="about-item"><span class="about-label">功能</span><span class="about-value">SSH / Telnet / 串口终端 / SFTP / 脚本管理 / 日志</span></div>
            <div class="about-item"><span class="about-label">项目主页</span>
              <span class="about-link" @click="openExternal('https://github.com/dingtongbin/AceShell')">
                <n-icon :size="13" :component="LogoGithub" /> github.com/dingtongbin/AceShell
              </span>
            </div>
            <div class="about-item"><span class="about-label">我的博客</span>
              <span class="about-link" @click="openExternal('https://dingtongbin.cn/')">
                <n-icon :size="13" :component="GlobeOutline" /> dingtongbin.cn
              </span>
            </div>
            <div class="about-item"><span class="about-label">版权</span>
              <span class="about-value">Copyright (C) 2026 AceShell。本项目基于 GPL-3.0 许可开源发布，遵循自由软件基金会发布的 GNU 通用公共许可证第 3 版（或由您选择的任何更高版本）条款。</span>
            </div>
            <div class="about-item"><span class="about-label">许可</span><span class="about-value"><n-tag size="tiny" type="warning">GPL-3.0</n-tag></span></div>
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
</style>
