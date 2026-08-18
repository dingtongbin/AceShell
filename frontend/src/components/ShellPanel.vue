<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, inject, computed, watch } from 'vue'
import { NModal, NInput, NInputNumber, NSelect, NButton, NDescriptions, NDescriptionsItem, NCheckboxGroup, NCheckbox, NRadioGroup, NRadioButton, NIcon, NScrollbar, useMessage } from 'naive-ui'
import { DocumentTextOutline, LogoGithub, GlobeOutline } from '@vicons/ionicons5'
import { Window } from '@wailsio/runtime'
import LeftToolBar from './LeftToolBar.vue'
import MainMenu from './MainMenu.vue'
import ResourceManager from './ResourceManager.vue'
import TabManager from './TabManager.vue'
import ExportDialog from './ExportDialog.vue'
import ImportDialog from './ImportDialog.vue'

import { SaveSession, CreateFolder, LoadSession, GetTree, UpdateSession } from '../../bindings/changeme/internal/services/sessionfileservice.js'
import { GetVersion } from '../../bindings/changeme/internal/services/versionservice.js'
import { GetConfig, SetShowSession, SetShowSerial, SetShowToolbar } from '../../bindings/changeme/internal/services/configservice.js'
import { ListPorts } from '../../bindings/changeme/internal/services/serialservice.js'
import { OpenUrl as BrowserOpenUrl } from '../../bindings/changeme/internal/services/browserservice.js'
import { ListKeys, DeleteKey } from '../../bindings/changeme/internal/services/globalkeyservice.js'
import { ScanBrowsers } from '../../bindings/changeme/internal/services/browserservice.js'
import KeyCreateDialog from './KeyCreateDialog.vue'
import SshCopyDialog from './SshCopyDialog.vue'
import FileEditor from './FileEditor.vue'
import type { FileEditorApi } from './FileEditor.vue'

const message = useMessage()

const emit = defineEmits<{
  (e: 'open-settings'): void
}>()

const showSessionManager = ref(true)
const mainMenuShow = ref(false)
const showAbout = ref(false)
const globalStatus = ref<{ text: string; row: number; col: number; encoding: string; hasTab: boolean }>({ text: '未连接', row: 0, col: 0, encoding: '', hasTab: false })

function onTabStatus(info: { text: string; row: number; col: number; encoding: string; hasTab: boolean }) {
  globalStatus.value = info
}
const showSerial = ref(true)
const showToolbar = ref(true)
const showHelp = ref(true)
const showGithub = ref(true)
const appVersion = ref('0.1.0')
const showExport = ref(false)
const showImport = ref(false)
const sessionWidth = ref(220)
const isResizing = ref(false)
const tabManagerRef = ref<InstanceType<typeof TabManager> | null>(null)
const sessionManagerRef = ref<InstanceType<typeof ResourceManager> | null>(null)
const tabOrientation = ref('horizontal')
const verticalTabWidth = ref(180)

// 活动会话路径（资源管理器高亮 + 编辑菜单）
const activeSessionPath = ref<string | null>(null)

// Folder
const showNewFolder = ref(false)
const newFolderName = ref('')
const newFolderParent = ref('')

// Session dialog
const showSessionDialog = ref(false)
const isEditMode = ref(false)
const selectedProtocol = ref<'ssh' | 'sftp' | 'telnet' | 'serial' | 'http'>('ssh')

// Common fields
const sessName = ref('')
const sessCreated = ref('')
const sessUpdated = ref('')
const sessFolder = ref('')
const sessPath = ref('')

// SSH
const sshHost = ref('')
const sshPort = ref(22)
const sshUser = ref('')
const sshPassword = ref('')
const sshAuthMode = ref<'password' | 'key'>('password')
const sshKeyRef = ref('')
const keyList = ref<{ id: string; name: string; type: string; fingerprint: string }[]>([])
const showKeyCreate = ref(false)
const showKeyCopy = ref(false)

// Telnet
const telnetHost = ref('')
const telnetPort = ref(23)
const telnetAccount = ref('')
const telnetPassword = ref('')

// Serial
const serialDevice = ref('')
const serialBaud = ref(9600)
const serialDataBits = ref(8)
const serialStopBits = ref('1')
const serialParity = ref('none')

// HTTP
const httpUrl = ref('')
const httpBrowser = ref('')
const browserList = ref<{ id: string; name: string; execPath: string; isDefault: boolean }[]>([])

async function loadBrowsers() {
  try {
    const raw = JSON.parse(await ScanBrowsers())
    browserList.value = Array.isArray(raw) ? raw : []
    if (!httpBrowser.value) {
      const def = browserList.value.find(b => b.id === 'default')
      if (def) httpBrowser.value = def.id
    }
  } catch {
    browserList.value = []
  }
}

const browserOptions = computed(() =>
  browserList.value.map(b => ({
    label: b.id === 'default' ? `${b.name}（系统默认）` : b.name + (b.isDefault ? '（系统默认）' : ''),
    value: b.id,
  })),
)

// 会话弹窗内 Tab：表单 / 元数据 / 高级
const sessionSide = ref<'settings' | 'meta' | 'advanced'>('settings')

// 现代安全加密算法（默认勾选）
const modernCiphers = [
  'aes256-gcm@openssh.com',
  'aes128-gcm@openssh.com',
  'chacha20-poly1305@openssh.com',
  'aes256-ctr',
  'aes192-ctr',
  'aes128-ctr',
]
// 兼容性加密算法
const legacyCiphers = [
  'aes256-cbc',
  'aes128-cbc',
  '3des-cbc',
  'blowfish-cbc',
  'twofish-cbc',
  'twofish256-cbc',
  'twofish128-cbc',
  'cast128-cbc',
  'arcfour',
  'arcfour256',
  'arcfour128',
]
const cipherGroups = [
  { label: '现代安全算法', children: modernCiphers, cls: 'cipher-group-modern' },
  { label: '兼容性算法', children: legacyCiphers, cls: 'cipher-group-legacy' },
]
const sessAllowedCiphers = ref<string[]>([])
function setModernCiphers() { sessAllowedCiphers.value = [...modernCiphers] }
function setAllCiphers() { sessAllowedCiphers.value = [...modernCiphers, ...legacyCiphers] }

const serialBaudOptions = [9600, 19200, 38400, 57600, 115200, 230400, 460800, 921600].map(v => ({ label: String(v), value: v }))
const stopBitsOptions = [
  { label: '1', value: '1' },
  { label: '1.5', value: '1.5' },
  { label: '2', value: '2' },
]
const parityOptions = [
  { label: '无', value: 'none' },
  { label: '奇校验 (Odd)', value: 'odd' },
  { label: '偶校验 (Even)', value: 'even' },
  { label: '标记 (Mark)', value: 'mark' },
  { label: '空格 (Space)', value: 'space' },
]

// ==================== Config ====================

async function loadConfig() {
  try {
    const cfg = JSON.parse(await GetConfig())
    showSessionManager.value = cfg.view?.showSession ?? true
    showSerial.value = cfg.view?.showSerial ?? true
    showToolbar.value = cfg.view?.showToolbar ?? true
    showHelp.value = cfg.view?.showHelp ?? true
    showGithub.value = cfg.view?.showGithub ?? true
    tabOrientation.value = cfg.view?.tabOrientation ?? 'horizontal'
    verticalTabWidth.value = cfg.view?.verticalTabWidth ?? 180
  } catch {}
}

function toggleSessionManager() {
  showSessionManager.value = !showSessionManager.value
  SetShowSession(showSessionManager.value).catch(() => message.error('保存配置失败'))
}

function toggleToolbar() {
  showToolbar.value = !showToolbar.value
  SetShowToolbar(showToolbar.value).catch(() => message.error('保存配置失败'))
}

function toggleSerial() {
  showSerial.value = !showSerial.value
  SetShowSerial(showSerial.value).catch(() => message.error('保存配置失败'))
}

// ==================== Resize ====================

function startResize(e: PointerEvent) {
  isResizing.value = true
  ;(e.target as HTMLElement).setPointerCapture(e.pointerId)
}
function onResize(e: PointerEvent) {
  if (!isResizing.value) return
  const w = e.clientX
  if (w < 60) {
    showSessionManager.value = false
    sessionWidth.value = 0
  } else {
    showSessionManager.value = true
    sessionWidth.value = Math.max(0, Math.min(w, 600))
  }
}
function stopResize() {
  if (isResizing.value) {
    isResizing.value = false
    sessionWidth.value = showSessionManager.value ? Math.max(60, sessionWidth.value) : 220
  }
}

// ==================== Validation ====================

function validateName(name: string): string | null {
  if (!name || name.trim().length === 0) return '名称不能为空'
  if (name.length > 255) return '名称过长'
  if (name !== name.trim()) return '名称首尾不能有空格'
  if (/[<>:"\/\\|?*]/.test(name)) return '不能包含: < > : " / \\ | ? *'
  if (/[.]$/.test(name) || /\s$/.test(name)) return '不能以点号或空格结尾'
  return null
}

function validateSSH(): string | null {
  if (!sshHost.value.trim()) return 'SSH 主机不能为空'
  if (sshPort.value < 1 || sshPort.value > 65535) return 'SSH 端口范围 1-65535'
  if (sshAuthMode.value === 'key' && !sshKeyRef.value) return '请选择要使用的密钥'
  return null
}

function validateTelnet(): string | null {
  if (!telnetHost.value.trim()) return 'Telnet 主机不能为空'
  if (telnetPort.value < 1 || telnetPort.value > 65535) return 'Telnet 端口范围 1-65535'
  return null
}

function validateSerial(): string | null {
  if (!serialDevice.value.trim()) return '串口设备路径不能为空'
  return null
}

function validateHttp(): string | null {
  const url = httpUrl.value.trim()
  if (!url) return 'URL 不能为空'
  const lower = url.toLowerCase()
  if (!lower.startsWith('http://') && !lower.startsWith('https://')) {
    return 'URL 必须以 http:// 或 https:// 开头'
  }
  return null
}

function validateCurrent(): string | null {
  const nameErr = validateName(sessName.value.trim())
  if (nameErr) return nameErr
  switch (selectedProtocol.value) {
    case 'ssh':
    case 'sftp':
      return validateSSH()
    case 'telnet': return validateTelnet()
    case 'serial': return validateSerial()
    case 'http': return validateHttp()
    default: return '未知协议'
  }
}

// ==================== Key management ====================

async function loadKeyList() {
  try {
    const raw = JSON.parse(await ListKeys())
    keyList.value = Array.isArray(raw) ? raw : []
  } catch {
    keyList.value = []
  }
}

const keyOptions = computed(() => keyList.value.map(k => ({ label: `${k.name} (${k.type})`, value: `key://${k.name}` })))

function openKeyCreate() {
  showKeyCreate.value = true
}

async function handleKeyCreated() {
  showKeyCreate.value = false
  await loadKeyList()
}

async function handleDeleteKey(keyRef: string) {
  const entry = keyList.value.find(k => `key://${k.name}` === keyRef)
  if (!entry) return
  try {
    await DeleteKey(entry.id)
    await loadKeyList()
    if (sshKeyRef.value === keyRef) sshKeyRef.value = ''
    message.success('密钥已删除')
  } catch (e: any) {
    message.error('删除失败: ' + (e.message || e))
  }
}

function openKeyCopy() {
  showKeyCopy.value = true
}

async function handleKeyCopied() {
  showKeyCopy.value = false
}

// ==================== Build TOML ====================

function nowStr() {
  return new Date().toLocaleString('sv-SE').replace('T', ' ')
}

function escapeToml(s: string): string {
  return s.replace(/\\/g, '\\\\').replace(/"/g, '\\"').replace(/\n/g, '\\n').replace(/\r/g, '\\r').replace(/\t/g, '\\t')
}

function buildSshToml(name: string, protocol: 'ssh' | 'sftp' = 'ssh'): string {
  let toml = `name = "${escapeToml(name)}"\nhost = "${escapeToml(sshHost.value)}"\nport = ${sshPort.value}\nusername = "${escapeToml(sshUser.value)}"\nprotocol = "${protocol}"\n`
  if (sshAuthMode.value === 'key') {
    toml += `authMode = "key"\nkey = "${escapeToml(sshKeyRef.value)}"\n`
  } else {
    toml += `password = "${escapeToml(sshPassword.value)}"\n`
  }
  toml += `notes = ""\ncreated = "${escapeToml(sessCreated.value || nowStr())}"\nupdated = "${escapeToml(nowStr())}"\n`
  if (sessAllowedCiphers.value.length > 0) {
    toml += `allowedCiphers = [${sessAllowedCiphers.value.map(c => `"${escapeToml(c)}"`).join(', ')}]\n`
  }
  return toml
}

function buildTelnetToml(name: string): string {
  return `name = "${escapeToml(name)}"\nhost = "${escapeToml(telnetHost.value)}"\nport = ${telnetPort.value}\nusername = "${escapeToml(telnetAccount.value)}"\npassword = "${escapeToml(telnetPassword.value)}"\nprotocol = "telnet"\nnotes = ""\ncreated = "${escapeToml(sessCreated.value || nowStr())}"\nupdated = "${escapeToml(nowStr())}"\n`
}

function buildSerialToml(name: string): string {
  return `name = "${escapeToml(name)}"\nhost = "${escapeToml(serialDevice.value)}"\nport = ${serialBaud.value}\nprotocol = "serial"\ndataBits = ${serialDataBits.value}\nstopBits = "${escapeToml(serialStopBits.value)}"\nparity = "${escapeToml(serialParity.value)}"\nnotes = ""\ncreated = "${escapeToml(sessCreated.value || nowStr())}"\nupdated = "${escapeToml(nowStr())}"\n`
}

function buildHttpToml(name: string): string {
  return `name = "${escapeToml(name)}"\nurl = "${escapeToml(httpUrl.value.trim())}"\nprotocol = "http"\nbrowser = "${escapeToml(httpBrowser.value)}"\nnotes = ""\ncreated = "${escapeToml(sessCreated.value || nowStr())}"\nupdated = "${escapeToml(nowStr())}"\n`
}

function buildToml(name: string): string {
  switch (selectedProtocol.value) {
    case 'ssh': return buildSshToml(name, 'ssh')
    case 'sftp': return buildSshToml(name, 'sftp')
    case 'telnet': return buildTelnetToml(name)
    case 'serial': return buildSerialToml(name)
    case 'http': return buildHttpToml(name)
    default: return ''
  }
}

// ==================== Duplicate check ====================

async function checkDuplicate(parentPath: string, name: string, isDir: boolean): Promise<boolean> {
  try {
    const tree = JSON.parse(await GetTree()) || []
    let siblings = tree
    if (parentPath && parentPath !== '.') {
      const parts = parentPath.split('/')
      let current = tree
      for (const part of parts) {
        const node = current.find((n: any) => n.name === part && n.isDir)
        if (node?.children) current = node.children
      }
      siblings = current
    }
    return siblings.some((n: any) => n.name === name && n.isDir === isDir)
  } catch {
    return false
  }
}

async function getDefaultName(parentPath: string, base: string, isDir: boolean): Promise<string> {
  try {
    const tree = JSON.parse(await GetTree()) || []
    let sib = tree
    if (parentPath && parentPath !== '.') {
      const parts = parentPath.split('/')
      let current = tree
      for (const part of parts) {
        const node = current.find((n: any) => n.name === part && n.isDir)
        if (node?.children) current = node.children
      }
      sib = current
    }
    const existing = sib.filter((n: any) => n.isDir === isDir).map((n: any) => n.name)
    if (!existing.includes(base)) return base
    let c = 2
    while (existing.includes(`${base}(${c})`)) c++
    return `${base}(${c})`
  } catch {
    return base
  }
}

// ==================== Folder ====================

async function handleNewFolder(parentPath: string) {
  newFolderName.value = await getDefaultName(parentPath, '新建文件夹', true)
  newFolderParent.value = parentPath
  showNewFolder.value = true
}

async function handleCreateFolder() {
  let n = newFolderName.value.trim()
  if (!n) n = await getDefaultName(newFolderParent.value, '新建文件夹', true)
  const err = validateName(n)
  if (err) { message.error(err); return }
  if (await checkDuplicate(newFolderParent.value, n, true)) { message.error('已存在同名文件夹'); return }
  try {
    await CreateFolder((newFolderParent.value ? newFolderParent.value + '/' : '') + n)
    showNewFolder.value = false
    message.success('创建成功')
  } catch (e: any) {
    message.error('失败: ' + (e.message || e))
  }
}

// ==================== Session ====================

const serialPorts = ref<{ label: string; value: string }[]>([])
const scanningPorts = ref(false)

async function refreshSerialPorts() {
  scanningPorts.value = true
  try {
    const raw = await ListPorts()
    const ports = JSON.parse(raw) as unknown
    serialPorts.value = Array.isArray(ports) ? ports.map((p: string) => ({ label: p, value: p })) : []
    if (serialPorts.value.length > 0 && !serialDevice.value) {
      serialDevice.value = serialPorts.value[0].value
    }
  } catch {
    serialPorts.value = []
  } finally {
    scanningPorts.value = false
  }
}

watch(selectedProtocol, (v) => { if (v === 'serial') refreshSerialPorts() })

function resetSshFields() { sshHost.value = ''; sshPort.value = 22; sshUser.value = ''; sshPassword.value = ''; sshAuthMode.value = 'password'; sshKeyRef.value = ''; sessAllowedCiphers.value = [] }
function resetTelnetFields() { telnetHost.value = ''; telnetPort.value = 23; telnetAccount.value = ''; telnetPassword.value = '' }
function resetSerialFields() { serialDevice.value = ''; serialBaud.value = 9600; serialDataBits.value = 8; serialStopBits.value = '1'; serialParity.value = 'none' }
function resetHttpFields() { httpUrl.value = ''; httpBrowser.value = '' }
function resetAllFields() {
  sessName.value = ''; sessCreated.value = ''; sessUpdated.value = ''; sessFolder.value = ''; sessPath.value = ''
  resetSshFields(); resetTelnetFields(); resetSerialFields(); resetHttpFields()
}

function openNewSession(folderPath: string, protocol: 'ssh' | 'sftp' | 'telnet' | 'serial' | 'http' = 'ssh') {
  resetAllFields()
  selectedProtocol.value = protocol
  sessFolder.value = folderPath
  sessionSide.value = 'settings'
  isEditMode.value = false
  showSessionDialog.value = true
  if (protocol === 'http') loadBrowsers()
}

async function handleCreateSession() {
  const err = validateCurrent()
  if (err) { message.error(err); return }
  const name = sessName.value.trim()
  if (await checkDuplicate(sessFolder.value || '.', name, false)) { message.error('该目录下已存在同名会话'); return }
  try {
    await SaveSession(sessFolder.value || '.', buildToml(name))
    showSessionDialog.value = false
    message.success('创建成功')
  } catch (e: any) { message.error('失败: ' + (e.message || e)) }
}

// ==================== Edit session ====================

async function openEditSession(path: string) {
  try {
    const meta = JSON.parse(await LoadSession(path))
    sessName.value = meta.name || ''
    sessPath.value = path
    sessFolder.value = path.substring(0, path.lastIndexOf('/'))
    sessCreated.value = meta.created || ''
    sessUpdated.value = meta.updated || ''
    selectedProtocol.value = meta.protocol === 'serial' ? 'serial' : (meta.protocol === 'telnet' ? 'telnet' : (meta.protocol === 'sftp' ? 'sftp' : (meta.protocol === 'http' ? 'http' : 'ssh')))
    switch (selectedProtocol.value) {
      case 'ssh':
      case 'sftp':
        sshHost.value = meta.host || ''
        sshPort.value = meta.port || 22
        sshUser.value = meta.username || ''
        sshPassword.value = meta.password || ''
        sshAuthMode.value = meta.authMode === 'key' ? 'key' : 'password'
        sshKeyRef.value = meta.key || ''
        sessAllowedCiphers.value = meta.allowedCiphers?.length > 0 ? meta.allowedCiphers : [...modernCiphers]
        break
      case 'telnet':
        telnetHost.value = meta.host || ''
        telnetPort.value = meta.port || 23
        telnetAccount.value = meta.username || ''
        telnetPassword.value = meta.password || ''
        break
      case 'serial':
        serialDevice.value = meta.host || ''
        serialBaud.value = meta.port || 9600
        serialDataBits.value = meta.dataBits || 8
        serialStopBits.value = meta.stopBits || '1'
        serialParity.value = meta.parity || 'none'
        break
      case 'http':
        httpUrl.value = meta.url || ''
        httpBrowser.value = meta.browser || ''
        loadBrowsers()
        break
    }
    sessionSide.value = 'settings'
    isEditMode.value = true
    showSessionDialog.value = true
  } catch (e: any) { console.error(e) }
}

async function handleUpdateSession() {
  const err = validateCurrent()
  if (err) { message.error(err); return }
  const name = sessName.value.trim()
  try {
    await UpdateSession(sessPath.value, buildToml(name))
    showSessionDialog.value = false
    message.success('保存成功')
  } catch (e: any) { message.error('失败: ' + (e.message || e)) }
}

// ==================== Tab ====================

function handleSelectSession(path: string) {
  activeSessionPath.value = path
  tabManagerRef.value?.openSession(path)
}

// ==================== Menu actions ====================

function handleEditActiveSession() {
  const tm = tabManagerRef.value
  if (!tm) return
  const path = tm.getActiveSessionPath()
  if (!path) {
    message.info('当前活动标签页不是会话连接，无法编辑')
    return
  }
  openEditSession(path)
}

function handleRenameSelected() {
  sessionManagerRef.value?.renameSelected()
}

function handleDeleteSelected() {
  sessionManagerRef.value?.deleteSelected()
}

function handleExit() {
  Window.Close()
}

function handleSerialConnect(portName: string, baudRate: number, dataBits: number, stopBits: string, parity: string) {
  tabManagerRef.value?.openSerial(portName, baudRate, dataBits, stopBits, parity)
}

// 脚本管理器双击文件:在标签页中打开文件编辑器(同文件只保留一个标签页,重复打开则激活已有标签页)
function handleOpenFile(path: string) {
  const tm = tabManagerRef.value
  if (!tm) return
  if (tm.activateFileTab(path)) return
  const name = path.split('/').pop() || path
  let tabId = ''
  let editorApi: FileEditorApi | null = null
  tabId = tm.openComponentTab({
    title: name,
    component: FileEditor,
    props: {
      filePath: path,
      fileName: name,
      onApiReady: (api: FileEditorApi) => { editorApi = api },
      onDirtyChange: (dirty: boolean) => { if (tabId) tm.updateComponentTab(tabId, { dirty }) },
      onCursorChange: (row: number, col: number) => { tm.reportCursor(row, col) },
    },
    icon: DocumentTextOutline,
    color: '#6e9fc7',
    status: 'idle',
    dirty: false,
    onClose: (): boolean | Promise<boolean> => {
      if (!editorApi || !editorApi.isDirty()) return true
      const api = editorApi
      return new Promise<boolean>(resolve => {
        editorClosePending.value = { fileName: name, api, resolve }
      })
    },
  }) || ''
}

// 文件编辑器未保存关闭确认:保存并关闭 / 不保存关闭 / 取消
const editorClosePending = ref<{ fileName: string; api: FileEditorApi; resolve: (v: boolean) => void } | null>(null)

async function confirmEditorCloseSave() {
  const pending = editorClosePending.value
  if (!pending) return
  editorClosePending.value = null
  const ok = await pending.api.save()
  pending.resolve(ok)
}

function confirmEditorCloseDiscard() {
  const pending = editorClosePending.value
  if (!pending) return
  editorClosePending.value = null
  pending.resolve(true)
}

function cancelEditorClose() {
  const pending = editorClosePending.value
  if (!pending) return
  editorClosePending.value = null
  pending.resolve(false)
}

// ==================== Menu actions ====================

function handleToolAction(key: string) {
  const tm = tabManagerRef.value
  if (!tm) return
  switch (key) {
    case 'sftp': tm.openSftp(); break
    case 'exec-script': tm.openScriptDialog(); break
    case 'export-log': tm.exportLog(); break
    case 'clear-scrollback': tm.clearScrollback(); break
    case 'clear-screen': tm.clearScreen(); break
  }
}

function onConfigChanged() { loadConfig() }

// 用系统默认浏览器打开外部链接
async function openExternal(url: string) {
  const err = await BrowserOpenUrl('', url)
  if (err) message.error('打开链接失败: ' + err)
}

// 打开项目 GitHub 页面(系统默认浏览器)
function openGithub() { openExternal('https://github.com/dingtongbin/AceShell') }

onMounted(() => {
  loadConfig()
  window.addEventListener('config-changed', onConfigChanged)
  GetVersion().then(v => { appVersion.value = v }).catch(() => {})
})

onBeforeUnmount(() => {
  window.removeEventListener('config-changed', onConfigChanged)
})
</script>

<template>
  <div class="shell-panel">
    <div class="shell-body" @pointermove="onResize" @pointerup="stopResize" @pointerleave="stopResize">
      <LeftToolBar
        :show-session="showSessionManager"
        :show-help="showHelp"
        :show-github="showGithub"
        @toggle-session="toggleSessionManager"
        @open-help="showAbout = true"
        @open-github="openGithub"
        @open-settings="emit('open-settings')"
      />
      <MainMenu
        v-if="mainMenuShow"
        class="main-menu-popup"
        :show-toolbar="showToolbar"
        @close="mainMenuShow = false"
        @new-session="openNewSession('')"
        @new-folder="handleNewFolder('')"
        @import-sessions="showImport = true"
        @export-sessions="showExport = true"
        @exit="handleExit"
        @edit-active-session="handleEditActiveSession"
        @rename-selected="handleRenameSelected"
        @delete-selected="handleDeleteSelected"
        @exec-script="handleToolAction('exec-script')"
        @sftp="handleToolAction('sftp')"
        @toggle-toolbar="toggleToolbar"
        @about="message.info('AceShell 网络 Shell 终端管理工具')"
      />
      <div class="right-area">
        <div class="right-content">
          <div v-show="showSessionManager" class="shell-sidebar" :style="{ width: showSessionManager ? sessionWidth + 'px' : '0px', minWidth: showSessionManager ? '60px' : '0px' }">
        <ResourceManager
          ref="sessionManagerRef"
          :style="{ width: showSessionManager ? sessionWidth + 'px' : '0px', minWidth: showSessionManager ? '60px' : '0px' }"
          :width="sessionWidth"
          :show-serial="showSerial"
          :active-session-path="activeSessionPath"
          @select="handleSelectSession"
          @new-folder="handleNewFolder"
          @new-session="openNewSession"
          @edit-session="openEditSession"
          @import-sessions="showImport = true"
          @export-sessions="showExport = true"
          @refresh="() => {}"
          @connect="handleSerialConnect"
          @open-file="handleOpenFile"
          @close="toggleSessionManager"
        />
      </div>
      <div v-if="showSessionManager" class="resize-handle" :style="{ left: sessionWidth + 'px' }" @pointerdown="startResize" />
      <div class="tab-area">
        <TabManager ref="tabManagerRef" :show-toolbar="showToolbar" :tab-orientation="tabOrientation" :vertical-tab-width="verticalTabWidth"
          @new-ssh="openNewSession('', 'ssh')" @new-telnet="openNewSession('', 'telnet')" @new-serial="openNewSession('', 'serial')" @status="onTabStatus" />
      </div>
        </div>
      <div class="global-status-bar">
        <span v-if="globalStatus.text" class="gs-left">{{ globalStatus.text }}</span>
        <span v-if="globalStatus.hasTab && globalStatus.encoding" class="gs-encoding">{{ globalStatus.encoding }}</span>
        <span v-if="globalStatus.hasTab" class="gs-cursor">行 {{ globalStatus.row }}, 列 {{ globalStatus.col }}</span>
      </div>
      </div>
    </div>

    <!-- Folder dialog -->
    <n-modal v-model:show="showNewFolder" title="新建文件夹" preset="dialog" :show-icon="false" style="width: 360px" :mask-closable="false">
      <div class="form-group"><label class="form-label">名称 <span class="required">*</span></label><n-input v-model:value="newFolderName" @keyup.enter="handleCreateFolder" /></div>
      <template #action><n-button @click="showNewFolder = false">取消</n-button><n-button type="primary" @click="handleCreateFolder">创建</n-button></template>
    </n-modal>

    <!-- Session dialog (new / edit) -->
    <n-modal v-model:show="showSessionDialog" :title="isEditMode ? '编辑会话' : '新建会话'" preset="dialog" :show-icon="false" style="width: 720px" :mask-closable="false">
      <div class="session-dialog">
        <n-scrollbar style="width: 140px; flex-shrink: 0;">
          <div class="session-type-list">
            <div class="type-item" :class="{ active: selectedProtocol === 'ssh' }" @click="selectedProtocol = 'ssh'">
              <span class="type-name">SSH</span>
              <span class="type-desc">安全外壳协议</span>
            </div>
            <div class="type-item" :class="{ active: selectedProtocol === 'sftp' }" @click="selectedProtocol = 'sftp'">
              <span class="type-name">SFTP</span>
              <span class="type-desc">文件传输协议</span>
            </div>
            <div class="type-item" :class="{ active: selectedProtocol === 'telnet' }" @click="selectedProtocol = 'telnet'">
              <span class="type-name">Telnet</span>
              <span class="type-desc">远程登录协议</span>
            </div>
            <div class="type-item" :class="{ active: selectedProtocol === 'serial' }" @click="selectedProtocol = 'serial'">
              <span class="type-name">串口</span>
              <span class="type-desc">串行设备连接</span>
            </div>
            <div class="type-item" :class="{ active: selectedProtocol === 'http' }" @click="selectedProtocol = 'http'; loadBrowsers()">
              <span class="type-name">HTTP</span>
              <span class="type-desc">网页链接访问</span>
            </div>
          </div>
        </n-scrollbar>
        <n-scrollbar style="flex: 1; min-width: 0;">
          <div class="session-main">
            <div v-if="sessionSide === 'settings'" class="anim-fade">
            <div class="form-group"><label class="form-label">名称 <span class="required">*</span></label><n-input v-model:value="sessName" placeholder="会话名" /></div>
            <template v-if="selectedProtocol === 'ssh' || selectedProtocol === 'sftp'">
              <div class="form-group"><label class="form-label">IP 地址 <span class="required">*</span></label><n-input v-model:value="sshHost" placeholder="IP 或域名" /></div>
              <div class="form-group"><label class="form-label">端口</label><n-input-number v-model:value="sshPort" :min="1" :max="65535" style="width: 100%" /></div>
              <div class="form-group"><label class="form-label">用户名</label><n-input v-model:value="sshUser" placeholder="留空则连接时输入" /></div>
              <div class="form-group">
                <label class="form-label">登录方式</label>
                <n-radio-group v-model:value="sshAuthMode" size="small">
                  <n-radio-button value="password">密码登录</n-radio-button>
                  <n-radio-button value="key">密钥登录</n-radio-button>
                </n-radio-group>
              </div>
              <div v-if="sshAuthMode === 'password'" class="form-group"><label class="form-label">密码</label><n-input v-model:value="sshPassword" type="password" show-password-on="click" placeholder="留空则连接时输入" /></div>
              <div v-else class="form-group">
                <label class="form-label">密钥</label>
                <div style="display: flex; gap: 6px; align-items: center; width: 100%">
                  <n-select v-model:value="sshKeyRef" :options="keyOptions" placeholder="选择密钥" filterable clearable style="flex: 1; min-width: 0" @focus="loadKeyList" />
                  <n-button size="small" @click="openKeyCreate">新建</n-button>
                  <n-button size="small" @click="openKeyCopy">部署到主机</n-button>
                  <n-button size="small" :disabled="!sshKeyRef" @click="handleDeleteKey(sshKeyRef)">删除</n-button>
                </div>
              </div>
            </template>
            <template v-else-if="selectedProtocol === 'telnet'">
              <div class="form-group"><label class="form-label">IP 地址 <span class="required">*</span></label><n-input v-model:value="telnetHost" placeholder="IP 或域名" /></div>
              <div class="form-group"><label class="form-label">端口</label><n-input-number v-model:value="telnetPort" :min="1" :max="65535" style="width: 100%" /></div>
              <div class="form-group"><label class="form-label">账号</label><n-input v-model:value="telnetAccount" placeholder="登录账号" /></div>
              <div class="form-group"><label class="form-label">密码</label><n-input v-model:value="telnetPassword" type="password" show-password-on="click" placeholder="登录密码" /></div>
            </template>
            <template v-else-if="selectedProtocol === 'serial'">
              <div class="form-group">
                <label class="form-label">串口设备路径 <span class="required">*</span></label>
                <n-select v-model:value="serialDevice" :options="serialPorts" placeholder="自动扫描串口中..." filterable allow-create clearable :loading="scanningPorts" @focus="refreshSerialPorts" />
              </div>
              <div class="form-group"><label class="form-label">波特率</label><n-select v-model:value="serialBaud" :options="serialBaudOptions" /></div>
              <div class="form-group"><label class="form-label">数据位</label><n-input-number v-model:value="serialDataBits" :min="5" :max="8" style="width: 100%" /></div>
              <div class="form-group"><label class="form-label">停止位</label><n-select v-model:value="serialStopBits" :options="stopBitsOptions" /></div>
              <div class="form-group"><label class="form-label">校验位</label><n-select v-model:value="serialParity" :options="parityOptions" /></div>
            </template>
            <template v-else-if="selectedProtocol === 'http'">
              <div class="form-group"><label class="form-label">URL <span class="required">*</span></label><n-input v-model:value="httpUrl" placeholder="https://example.com" /></div>
              <div class="form-group">
                <label class="form-label">浏览器</label>
                <div style="display: flex; gap: 6px; align-items: center; width: 100%">
                  <n-select v-model:value="httpBrowser" :options="browserOptions" placeholder="选择浏览器" clearable style="flex: 1; min-width: 0" @focus="loadBrowsers" />
                  <n-button size="small" @click="loadBrowsers">重新扫描</n-button>
                </div>
              </div>
              <div class="http-hint">
                使用说明：
                <br />1. URL 必须以 http:// 或 https:// 开头（https 优先）。
                <br />2. 浏览器列表为自动扫描的本机浏览器；留空或选择"默认浏览器（系统默认）"时使用系统默认浏览器。
                <br />3. 双击会话即直接打开所选浏览器的新标签页，不占用本应用标签页。
                <br />4. 若打开时所选浏览器已不存在或不可用，将不会打开网页，并弹出提示让您重新选择浏览器。
              </div>
            </template>
          </div>
          <div v-else-if="sessionSide === 'meta'" class="anim-fade">
            <n-descriptions bordered :column="1" size="medium" label-style="width:100px" style="max-width: 400px; margin: 0 auto">
              <n-descriptions-item label="创建时间">{{ sessCreated || '--' }}</n-descriptions-item>
              <n-descriptions-item label="更新时间">{{ sessUpdated || '--' }}</n-descriptions-item>
              <n-descriptions-item label="协议">{{ selectedProtocol }}</n-descriptions-item>
            </n-descriptions>
          </div>
          <div v-else-if="sessionSide === 'advanced'" class="anim-fade">
            <div style="font-size: 13px; color: var(--text-color, #d4d4d4); margin-bottom: 12px; line-height: 1.6">
              自定义 SSH 加密算法。留空则使用服务端协商的默认算法。
            </div>
            <div class="cipher-buttons">
              <n-button size="tiny" @click="setModernCiphers">现代 (默认)</n-button>
              <n-button size="tiny" @click="setAllCiphers">全部选择</n-button>
              <n-button size="tiny" @click="sessAllowedCiphers = []">清除</n-button>
            </div>
            <n-checkbox-group v-model:value="sessAllowedCiphers">
              <div v-for="g in cipherGroups" :key="g.label" class="cipher-group" :class="g.cls">
                <div class="cipher-group-label">{{ g.label }}</div>
                <div class="cipher-grid">
                  <n-checkbox v-for="c in g.children" :key="c" :value="c" :label="c" class="cipher-checkbox" />
                </div>
              </div>
            </n-checkbox-group>
          </div>
        </div>
        </n-scrollbar>
      </div>
      <template #action>
        <n-button @click="showSessionDialog = false">取消</n-button>
        <n-button v-if="isEditMode && (selectedProtocol === 'ssh' || selectedProtocol === 'sftp')" size="small" style="margin-right: 8px" @click="sessionSide = sessionSide === 'advanced' ? 'settings' : 'advanced'">高级选项</n-button>
        <n-button v-if="isEditMode && (selectedProtocol === 'ssh' || selectedProtocol === 'sftp')" size="small" style="margin-right: 8px" @click="sessionSide = sessionSide === 'meta' ? 'settings' : 'meta'">元数据</n-button>
        <n-button type="primary" @click="isEditMode ? handleUpdateSession() : handleCreateSession()">{{ isEditMode ? '保存' : '创建' }}</n-button>
      </template>
    </n-modal>
    <ExportDialog v-model:show="showExport" @done="()=>{}" />
    <ImportDialog v-model:show="showImport" @done="()=>{}" />
    <KeyCreateDialog v-model:show="showKeyCreate" @created="handleKeyCreated" />
    <SshCopyDialog v-model:show="showKeyCopy" :selected-key="sshKeyRef" :host="sshHost" :port="sshPort" :user="sshUser" @done="handleKeyCopied" />

    <!-- About / help dialog -->
    <n-modal v-model:show="showAbout" title="帮助" preset="dialog" :show-icon="false" style="width: 480px" :mask-closable="false">
      <n-scrollbar style="height: 360px" class="about-scroller">
        <div class="about-body">
          <div class="about-name">AceShell</div>
          <div class="about-desc">跨平台网络终端管理工具</div>
          <div class="about-info">版本：{{ appVersion }}</div>
          <div class="about-links">
            <span class="about-link" @click="openExternal('https://github.com/dingtongbin/AceShell')">
              <n-icon :size="13" :component="LogoGithub" /> 项目主页
            </span>
            <span class="about-link" @click="openExternal('https://dingtongbin.cn/')">
              <n-icon :size="13" :component="GlobeOutline" /> 我的博客
            </span>
          </div>
          <div class="about-divider" />
          <div class="about-info">会话管理：SSH / SFTP / Telnet / 串口 / HTTP 五类会话，树形组织、加密存储、支持导入导出</div>
          <div class="about-info">多层标签页：外层标签 + SSH 内层会话（shell-1、shell-N），支持拖拽排序</div>
          <div class="about-info">终端渲染：真色彩、TUI 全屏程序、超链接、emoji 宽字符、回滚缓冲、选择复制与右键粘贴</div>
          <div class="about-info">SFTP：SSH / SFTP 会话内置面板，双栏浏览、上传下载、断点续传、在线编辑</div>
          <div class="about-info">HTTP 会话：自动扫描本机浏览器，双击在所选浏览器中打开链接</div>
          <div class="about-info">脚本管理：脚本内容注入活动终端执行，内置编辑器支持多标签与语法高亮</div>
          <div class="about-info">连接日志：自动记录所有连接的输入输出，按会话浏览查看</div>
          <div class="about-info">安全：主密钥加密存储敏感字段、SSH 指纹验证、SSH 密钥生成与部署</div>
          <div class="about-info">外观：深色 / 浅色主题、壁纸、面板透明度、标签方向</div>
          <div class="about-info">跨平台：Windows / macOS / Linux</div>
          <div class="about-info">详细使用文档请参考项目根目录 AceShell项目文档.md</div>
        </div>
      </n-scrollbar>
      <template #action><n-button type="primary" @click="showAbout = false">确定</n-button></template>
    </n-modal>

    <!-- 文件编辑器未保存关闭确认 -->
    <n-modal :show="!!editorClosePending" title="未保存的修改" preset="dialog" :show-icon="false" style="width: 400px" :mask-closable="false">
      <div style="font-size: 14px">
        <p>「<b>{{ editorClosePending?.fileName }}</b>」有未保存的修改。</p>
        <p style="margin-top: 8px; color: #e45858; font-size: 12px">关闭前是否保存？</p>
      </div>
      <template #action>
        <n-button @click="cancelEditorClose">取消</n-button>
        <n-button @click="confirmEditorCloseDiscard">不保存</n-button>
        <n-button type="primary" @click="confirmEditorCloseSave">保存并关闭</n-button>
      </template>
    </n-modal>
  </div>
</template>

<style scoped>
.shell-panel {
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.shell-body {
  flex: 1;
  min-height: 0;
  display: flex;
  overflow: hidden;
  position: relative;
}

.main-menu-popup {
  position: absolute;
  left: 44px;
  top: 0;
  z-index: 1000;
}

.shell-sidebar {
  display: flex;
  flex-direction: column;
  overflow: hidden;
  flex-shrink: 0;
}

.tab-area {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.resize-handle {
  position: absolute;
  top: 0;
  bottom: 0;
  width: 1px;
  cursor: col-resize;
  background: transparent;
  flex-shrink: 0;
  transition: background 0.15s;
  z-index: 10;
}
.resize-handle:hover,
.resize-handle:active {
  background: #0078d4;
}

.global-status-bar {
  height: 22px;
  min-height: 22px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 0 10px;
  background: var(--toolbar-bg);
  font-size: 11px;
  color: var(--icon-color);
}
.gs-left { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.gs-encoding { margin-left: auto; }
.gs-cursor { flex-shrink: 0; }

.right-area {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.right-content {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: row;
  position: relative;
}

.session-dialog {
  display: flex;
  height: 460px;
}
.session-type-list {
  width: 140px;
  flex-shrink: 0;
  border-right: 1px solid var(--border-color, #3c3c3c);
  padding: 8px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.type-item {
  padding: 10px 12px;
  border-radius: 6px;
  cursor: pointer;
  transition: background 0.15s;
  user-select: none;
}
.type-item:hover {
  background: rgba(255, 255, 255, 0.06);
}
.type-item.active {
  background: rgba(0, 120, 212, 0.2);
}
.type-name {
  display: block;
  font-size: 13px;
  font-weight: 500;
  color: var(--text-color, #d4d4d4);
}
.type-item.active .type-name {
  color: #0078d4;
}
.type-desc {
  display: block;
  font-size: 11px;
  color: var(--text-secondary, #888);
  margin-top: 2px;
}
.session-main {
  padding: 16px 24px;
}
.session-main :deep(.n-input),
.session-main :deep(.n-input-number),
.session-main :deep(.n-select) {
  --n-height: 32px;
}
.anim-fade {
  animation: fadeIn 0.15s ease;
}
@keyframes fadeIn {
  from { opacity: 0; transform: translateY(4px); }
  to { opacity: 1; transform: translateY(0); }
}
.form-group {
  margin-bottom: 16px;
}
.http-hint {
  font-size: 12px;
  line-height: 1.8;
  color: var(--text-color-dim, #9d9d9d);
  background: var(--sidebar-bg, rgba(255, 255, 255, 0.03));
  border: 1px solid var(--border-color, #3c3c3c);
  border-radius: 4px;
  padding: 8px 10px;
  max-width: 100%;
}
.form-row {
  display: flex;
  gap: 12px;
}
.form-row .form-group {
  flex: 1;
  min-width: 0;
}
.form-label {
  display: block;
  font-size: 13px;
  margin-bottom: 6px;
  color: var(--text-color, #d4d4d4);
}
.required {
  color: #e45858;
}
.cipher-buttons {
  display: flex;
  gap: 8px;
  margin-bottom: 14px;
  flex-wrap: wrap;
}
.cipher-group {
  margin-bottom: 16px;
}
.cipher-group-label {
  font-size: 12px;
  font-weight: 600;
  color: var(--text-secondary, #888);
  margin-bottom: 8px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}
.cipher-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 4px 12px;
}
.cipher-checkbox {
  padding: 4px 0;
}

.about-body {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 4px 0;
}
.about-name {
  font-size: 18px;
  font-weight: 600;
  color: var(--text-color, #d4d4d4);
}
.about-desc {
  font-size: 13px;
  color: var(--text-secondary, #888);
}
.about-divider {
  height: 1px;
  background: var(--border-color, #3c3c3c);
  margin: 2px 0;
}
.about-info {
  font-size: 12px;
  color: var(--text-color, #d4d4d4);
  line-height: 1.7;
}
.about-links {
  display: flex;
  gap: 16px;
  margin-top: 2px;
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
</style>
