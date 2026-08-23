<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, inject, computed, watch } from 'vue'
import { NModal, NInput, NInputNumber, NSelect, NButton, NDescriptions, NDescriptionsItem, NCheckboxGroup, NCheckbox, NRadioGroup, NRadioButton, NIcon, NScrollbar, useMessage } from 'naive-ui'
import { DocumentTextOutline } from '@vicons/ionicons5'
import { Window } from '@wailsio/runtime'
import LeftToolBar from './LeftToolBar.vue'
import TopMenuBar from './TopMenuBar.vue'
import ResourceManager from './ResourceManager.vue'
import TabManager from './TabManager.vue'
import ExportDialog from './ExportDialog.vue'
import ImportDialog from './ImportDialog.vue'

import { SaveSession, CreateFolder, LoadSession, GetTree, UpdateSession } from '../../bindings/changeme/internal/services/sessionfileservice.js'
import { GetVersion } from '../../bindings/changeme/internal/services/versionservice.js'
import { GetConfig, SetShowSession, SetShowSerial, SetShowToolbar, SetCustomTitlebar, SetPanelLayout } from '../../bindings/changeme/internal/services/configservice.js'
import { ListPorts } from '../../bindings/changeme/internal/services/serialservice.js'
import { OpenUrl as BrowserOpenUrl } from '../../bindings/changeme/internal/services/browserservice.js'
import { useI18n } from 'vue-i18n'
import { ListKeys, DeleteKey } from '../../bindings/changeme/internal/services/globalkeyservice.js'
import { ScanBrowsers } from '../../bindings/changeme/internal/services/browserservice.js'
import { GetRdpTestServers } from '../../bindings/changeme/internal/services/rdpservice.js'
import KeyCreateDialog from './KeyCreateDialog.vue'
import SshCopyDialog from './SshCopyDialog.vue'
import FileEditor from './FileEditor.vue'
import type { FileEditorApi } from './FileEditor.vue'
import McpSettingsPanel from './McpSettingsPanel.vue'
import AgentChatPanel from './AgentChatPanel.vue'
import AgentSettingsDialog from './AgentSettingsDialog.vue'
import { useMcpBridge } from '../composables/useMcpBridge'
import { useAgentBridge } from '../composables/useAgentBridge'

const message = useMessage()
const { t } = useI18n()
const { initMcpBridge, bindTabManager, bindOpenScriptHandler, criticalBlock } = useMcpBridge()
const { initAgentBridge } = useAgentBridge()

const emit = defineEmits<{
  (e: 'open-settings'): void
}>()

// 左侧面板两态:资源管理器 / 关闭(MCP 设置已弹窗化,不再占用侧栏)
const leftPanel = ref<'resource' | 'none'>('resource')
// 右侧智能体聊天面板(独立于左侧面板,独立开关,可最大化铺满)
const showAgentPanel = ref(false)
const agentPanelWidth = ref(300)
const agentMaximized = ref(false)
// MCP 设置弹窗(独立于智能体设置弹窗)
const showMcpSettings = ref(false)
// 智能体设置弹窗(独立于主设置弹窗)
const showAgentSettings = ref(false)
// 兼容原 showSessionManager 语义:资源面板是否开启(配置持久化)
const showSessionManager = computed(() => leftPanel.value === 'resource')
// 自绘标题栏开关(Frameless):决定 TopMenuBar 是否渲染窗口控制与拖拽区
const customTitlebar = ref(true)
const showAbout = ref(false)
const globalStatus = ref<{ text: string; row: number; col: number; encoding: string; hasTab: boolean }>({ text: t('shellPanel.notConnected'), row: 0, col: 0, encoding: '', hasTab: false })

function onTabStatus(info: { text: string; row: number; col: number; encoding: string; hasTab: boolean }) {
  globalStatus.value = info
}
const showSerial = ref(true)
const showToolbar = ref(true)
const showHelp = ref(true)
const appVersion = ref('0.1.2')
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
const selectedProtocol = ref<'ssh' | 'sftp' | 'telnet' | 'serial' | 'http' | 'rdp'>('ssh')

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

// RDP
const rdpHost = ref('')
const rdpPort = ref(3389)
const rdpUser = ref('')
const rdpPassword = ref('')
const rdpTestServers = ref<{ label: string; value: string }[]>([])
const rdpTestSel = ref('')

async function loadRdpTestServers() {
  try {
    const raw = JSON.parse(await GetRdpTestServers())
    const list = Array.isArray(raw) ? raw : []
    rdpTestServers.value = list.map((s: any) => ({
      label: `${s.name} (${s.host}:${s.port})`,
      value: JSON.stringify(s),
    }))
  } catch {
    rdpTestServers.value = []
  }
}

function applyRdpTestServer(value: string) {
  try {
    const s = JSON.parse(value)
    rdpHost.value = s.host || ''
    rdpPort.value = s.port || 3389
    rdpUser.value = s.username || ''
    rdpPassword.value = s.password || ''
  } catch {}
}

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
    label: b.id === 'default' ? `${b.name}${t('shellPanel.defaultBrowser')}` : b.name + (b.isDefault ? t('shellPanel.defaultBrowser') : ''),
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
  { label: t('shellPanel.cipherModern'), children: modernCiphers, cls: 'cipher-group-modern' },
  { label: t('shellPanel.cipherLegacy'), children: legacyCiphers, cls: 'cipher-group-legacy' },
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
const parityOptions = computed(() => [
  { label: t('shellPanel.parityNone'), value: 'none' },
  { label: 'Odd', value: 'odd' },
  { label: 'Even', value: 'even' },
  { label: 'Mark', value: 'mark' },
  { label: 'Space', value: 'space' },
])

// ==================== Config ====================

async function loadConfig() {
  try {
    const cfg = JSON.parse(await GetConfig())
    leftPanel.value = (cfg.view?.showSession ?? true) ? 'resource' : 'none'
    showSerial.value = cfg.view?.showSerial ?? true
    showToolbar.value = cfg.view?.showToolbar ?? true
    showHelp.value = cfg.view?.showHelp ?? true
    tabOrientation.value = cfg.view?.tabOrientation ?? 'horizontal'
    verticalTabWidth.value = cfg.view?.verticalTabWidth ?? 180
    customTitlebar.value = cfg.view?.customTitlebar ?? true
    showAgentPanel.value = cfg.view?.showAgentPanel ?? false
    agentPanelWidth.value = cfg.view?.agentPanelWidth ?? 300
    sessionWidth.value = cfg.view?.sessionWidth ?? 220
  } catch {}
}

// 面板布局持久化: 前端只上报最新值,Go 侧周期写盘(1s)+窗口关闭必写,保护磁盘
function pushPanelLayout() {
  SetPanelLayout(showAgentPanel.value, agentPanelWidth.value, sessionWidth.value).catch(() => {})
}

function toggleSessionManager() {
  leftPanel.value = leftPanel.value === 'resource' ? 'none' : 'resource'
  SetShowSession(leftPanel.value === 'resource').catch(() => message.error(t('shellPanel.saveConfigFailed')))
}

function toggleAgentPanel() {
  showAgentPanel.value = !showAgentPanel.value
  if (!showAgentPanel.value) agentMaximized.value = false
  pushPanelLayout()
}

function closeAgentPanel() {
  showAgentPanel.value = false
  agentMaximized.value = false
  pushPanelLayout()
}

// 自绘标题栏即时切换:设置弹窗开关 → config-changed → loadConfig 刷新本值 → Frameless 往返
watch(customTitlebar, v => {
  Window.SetFrameless(v).catch(() => {})
})

function toggleToolbar() {
  showToolbar.value = !showToolbar.value
  SetShowToolbar(showToolbar.value).catch(() => message.error(t('shellPanel.saveConfigFailed')))
}

function toggleSerial() {
  showSerial.value = !showSerial.value
  SetShowSerial(showSerial.value).catch(() => message.error(t('shellPanel.saveConfigFailed')))
}

// ==================== Resize ====================

// 资源管理器面板(右侧边框拖宽)
function startResize(e: PointerEvent) {
  isResizing.value = true
  ;(e.target as HTMLElement).setPointerCapture(e.pointerId)
}
function onResize(e: PointerEvent) {
  if (!isResizing.value) return
  const w = e.clientX
  if (w < 60) {
    leftPanel.value = 'none'
    sessionWidth.value = 0
  } else {
    leftPanel.value = 'resource'
    sessionWidth.value = Math.max(0, Math.min(w, 600))
  }
}
function stopResize() {
  if (isResizing.value) {
    isResizing.value = false
    sessionWidth.value = showSessionManager.value ? Math.max(60, sessionWidth.value) : 220
    pushPanelLayout()
  }
}

// AI 聊天面板(左侧边框拖宽,反向: 鼠标左移变宽)
const AGENT_W_MIN = 240
const AGENT_W_MAX = 720
const isAgentResizing = ref(false)
let agentResizeX = 0
let agentResizeW = 0
let agentPushAt = 0
function startAgentResize(e: PointerEvent) {
  isAgentResizing.value = true
  agentResizeX = e.clientX
  agentResizeW = agentPanelWidth.value
  ;(e.target as HTMLElement).setPointerCapture(e.pointerId)
}
function onAgentResize(e: PointerEvent) {
  if (!isAgentResizing.value) return
  agentPanelWidth.value = Math.max(AGENT_W_MIN, Math.min(AGENT_W_MAX, agentResizeW + (agentResizeX - e.clientX)))
  // 拖拽中节流上报(200ms): Go 内存即时最新,落盘由 Go 周期写+关闭必写兜底
  const now = Date.now()
  if (now - agentPushAt > 200) {
    agentPushAt = now
    pushPanelLayout()
  }
}
function stopAgentResize() {
  if (!isAgentResizing.value) return
  isAgentResizing.value = false
  pushPanelLayout()
}

// 合并分发: shell-body 上的 pointermove/up 同时服务两个拖拽源
function onBodyPointerMove(e: PointerEvent) {
  onResize(e)
  onAgentResize(e)
}
function onBodyPointerUp() {
  stopResize()
  stopAgentResize()
}

// ==================== 窄窗自适应 ====================

// 窗口宽度低于阈值时,资源管理器/AI 面板改为浮层弹出(覆盖标签页,不挤压终端区)。
// 层级: 标签页 < 资源管理器(600) < AI 面板(700) < AI 最大化(800)。
const NARROW_THRESHOLD = 1000
const isNarrow = ref(window.innerWidth < NARROW_THRESHOLD)
function updateNarrow() {
  isNarrow.value = window.innerWidth < NARROW_THRESHOLD
}

// ==================== Validation ====================

function validateName(name: string): string | null {
  if (!name || name.trim().length === 0) return t('shellPanel.nameRequired')
  if (name.length > 255) return t('shellPanel.nameTooLong')
  if (name !== name.trim()) return t('shellPanel.nameTrimError')
  if (/[<>:"\/\\|?*]/.test(name)) return t('shellPanel.nameInvalidChars')
  if (/[.]$/.test(name) || /\s$/.test(name)) return t('shellPanel.nameTrailingDot')
  return null
}

function validateSSH(): string | null {
  if (!sshHost.value.trim()) return t('shellPanel.sshHostRequired')
  if (sshPort.value < 1 || sshPort.value > 65535) return t('shellPanel.sshPortRange')
  if (sshAuthMode.value === 'key' && !sshKeyRef.value) return t('shellPanel.sshKeyRequired')
  return null
}

function validateTelnet(): string | null {
  if (!telnetHost.value.trim()) return t('shellPanel.telnetHostRequired')
  if (telnetPort.value < 1 || telnetPort.value > 65535) return t('shellPanel.telnetPortRange')
  return null
}

function validateSerial(): string | null {
  if (!serialDevice.value.trim()) return t('shellPanel.serialDeviceRequired')
  return null
}

function validateHttp(): string | null {
  const url = httpUrl.value.trim()
  if (!url) return t('shellPanel.urlRequired')
  const lower = url.toLowerCase()
  if (!lower.startsWith('http://') && !lower.startsWith('https://')) {
    return t('shellPanel.urlScheme')
  }
  return null
}

function validateRdp(): string | null {
  if (!rdpHost.value.trim()) return t('shellPanel.rdpHostRequired')
  if (rdpPort.value < 1 || rdpPort.value > 65535) return t('shellPanel.rdpPortRange')
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
    case 'rdp': return validateRdp()
    default: return t('shellPanel.unknownProtocol')
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
    message.success(t('shellPanel.keyDeleted'))
  } catch (e: any) {
    message.error(t('shellPanel.deleteFailed', { err: e.message || e }))
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

function buildRdpToml(name: string): string {
  return `name = "${escapeToml(name)}"\nhost = "${escapeToml(rdpHost.value.trim())}"\nport = ${rdpPort.value}\nusername = "${escapeToml(rdpUser.value)}"\npassword = "${escapeToml(rdpPassword.value)}"\nprotocol = "rdp"\nnotes = ""\ncreated = "${escapeToml(sessCreated.value || nowStr())}"\nupdated = "${escapeToml(nowStr())}"\n`
}

function buildToml(name: string): string {
  switch (selectedProtocol.value) {
    case 'ssh': return buildSshToml(name, 'ssh')
    case 'sftp': return buildSshToml(name, 'sftp')
    case 'telnet': return buildTelnetToml(name)
    case 'serial': return buildSerialToml(name)
    case 'http': return buildHttpToml(name)
    case 'rdp': return buildRdpToml(name)
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
  newFolderName.value = await getDefaultName(parentPath, t('shellPanel.newFolderTitle'), true)
  newFolderParent.value = parentPath
  showNewFolder.value = true
}

async function handleCreateFolder() {
  let n = newFolderName.value.trim()
  if (!n) n = await getDefaultName(newFolderParent.value, t('shellPanel.newFolderTitle'), true)
  const err = validateName(n)
  if (err) { message.error(err); return }
  if (await checkDuplicate(newFolderParent.value, n, true)) { message.error(t('shellPanel.duplicateFolder')); return }
  try {
    await CreateFolder((newFolderParent.value ? newFolderParent.value + '/' : '') + n)
    showNewFolder.value = false
    message.success(t('shellPanel.created'))
  } catch (e: any) {
    message.error(t('shellPanel.failed', { err: e.message || e }))
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
function resetRdpFields() { rdpHost.value = ''; rdpPort.value = 3389; rdpUser.value = ''; rdpPassword.value = ''; rdpTestSel.value = '' }
function resetAllFields() {
  sessName.value = ''; sessCreated.value = ''; sessUpdated.value = ''; sessFolder.value = ''; sessPath.value = ''
  resetSshFields(); resetTelnetFields(); resetSerialFields(); resetHttpFields(); resetRdpFields()
}

function openNewSession(folderPath: string, protocol: 'ssh' | 'sftp' | 'telnet' | 'serial' | 'http' | 'rdp' = 'ssh') {
  resetAllFields()
  selectedProtocol.value = protocol
  sessFolder.value = folderPath
  sessionSide.value = 'settings'
  isEditMode.value = false
  showSessionDialog.value = true
  if (protocol === 'http') loadBrowsers()
  if (protocol === 'rdp') loadRdpTestServers()
}

async function handleCreateSession() {
  const err = validateCurrent()
  if (err) { message.error(err); return }
  const name = sessName.value.trim()
  if (await checkDuplicate(sessFolder.value || '.', name, false)) { message.error(t('shellPanel.duplicateSession')); return }
  try {
    await SaveSession(sessFolder.value || '.', buildToml(name))
    showSessionDialog.value = false
    message.success(t('shellPanel.created'))
  } catch (e: any) { message.error(t('shellPanel.failed', { err: e.message || e })) }
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
    selectedProtocol.value = meta.protocol === 'serial' ? 'serial' : (meta.protocol === 'telnet' ? 'telnet' : (meta.protocol === 'sftp' ? 'sftp' : (meta.protocol === 'http' ? 'http' : (meta.protocol === 'rdp' ? 'rdp' : 'ssh'))))
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
      case 'rdp':
        rdpHost.value = meta.host || ''
        rdpPort.value = meta.port || 3389
        rdpUser.value = meta.username || ''
        rdpPassword.value = meta.password || ''
        loadRdpTestServers()
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
    message.success(t('shellPanel.saved'))
  } catch (e: any) { message.error(t('shellPanel.failed', { err: e.message || e })) }
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
    message.info(t('shellPanel.notEditable'))
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
// 返回标签页 ID(供 MCP open_script/script_write 复用同一打开路径)
function handleOpenFile(path: string): string | null {
  const tm = tabManagerRef.value
  if (!tm) return null
  const existing = tm.activateFileTab(path)
  if (existing) return existing
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
        editorClosePending.value.push({ fileName: name, api, resolve })
      })
    },
  }) || ''
  return tabId || null
}

// 文件编辑器未保存关闭确认:保存并关闭 / 不保存关闭 / 取消(连续关闭多个脏文件时排队)
interface EditorCloseReq { fileName: string; api: FileEditorApi; resolve: (v: boolean) => void }
const editorClosePending = ref<EditorCloseReq[]>([])

async function confirmEditorCloseSave() {
  const pending = editorClosePending.value[0]
  if (!pending) return
  editorClosePending.value = editorClosePending.value.slice(1)
  const ok = await pending.api.save()
  pending.resolve(ok)
}

function confirmEditorCloseDiscard() {
  const pending = editorClosePending.value[0]
  if (!pending) return
  editorClosePending.value = editorClosePending.value.slice(1)
  pending.resolve(true)
}

function cancelEditorClose() {
  const pending = editorClosePending.value[0]
  if (!pending) return
  editorClosePending.value = editorClosePending.value.slice(1)
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
  if (err) message.error(t('shellPanel.openLinkFailed', { err }))
}

onMounted(() => {
  loadConfig()
  window.addEventListener('config-changed', onConfigChanged)
  window.addEventListener('resize', updateNarrow)
  GetVersion().then(v => { appVersion.value = v }).catch(() => {})

  // MCP 桥接初始化:订阅事件 + 注入命令路由(MCP 操作与用户操作走同一 UI 路径)
  initMcpBridge()
  bindTabManager({
    listTabs: () => tabManagerRef.value?.listTabs() ?? [],
    openSession: async (sessionPath: string) => {
      if (!tabManagerRef.value) return null
      const tabId = await tabManagerRef.value.openSession(sessionPath)
      return tabId || null
    },
    mcpTerminalSend: (tabId: string, text: string, needPasteConfirm: boolean, activateTab: boolean) =>
      tabManagerRef.value?.mcpTerminalSend(tabId, text, needPasteConfirm, activateTab) ?? Promise.resolve({ ok: false, note: 'tab manager not ready' }),
    mcpCloseTab: (tabId: string, activateTab: boolean) =>
      tabManagerRef.value?.mcpCloseTab(tabId, activateTab) ?? Promise.resolve({ ok: false, note: 'tab manager not ready' }),
  })
  bindOpenScriptHandler(async (filePath: string) => handleOpenFile(filePath))

  // 智能体桥接初始化(事件订阅 + 会话列表加载)
  initAgentBridge()
})

onBeforeUnmount(() => {
  window.removeEventListener('config-changed', onConfigChanged)
  window.removeEventListener('resize', updateNarrow)
})
</script>

<template>
  <div class="shell-panel">
    <!-- 顶部菜单栏:左菜单 + 拖拽区 + 收纳按钮 + 窗口控制(Frameless 模式) -->
    <TopMenuBar
      :show-session="showSessionManager"
      :show-agent="showAgentPanel"
      :show-toolbar="showToolbar"
      :frameless-enabled="customTitlebar"
      @toggle-session="toggleSessionManager"
      @toggle-agent="toggleAgentPanel"
      @toggle-toolbar="toggleToolbar"
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
      @about="showAbout = true"
      @view-docs="openExternal('https://github.com/dingtongbin/AceShell')"
    />
    <div class="shell-body" @pointermove="onBodyPointerMove" @pointerup="onBodyPointerUp" @pointerleave="onBodyPointerUp">
      <LeftToolBar
        :show-session="leftPanel === 'resource'"
        :show-help="showHelp"
        @toggle-session="toggleSessionManager"
        @open-help="showAbout = true"
        @open-settings="emit('open-settings')"
      />
      <div class="right-area">
        <div class="right-content">
          <div v-show="leftPanel !== 'none'" class="shell-sidebar" :class="{ 'sidebar-overlay': isNarrow }" :style="{ width: leftPanel !== 'none' ? sessionWidth + 'px' : '0px', minWidth: leftPanel !== 'none' ? '60px' : '0px' }">
        <ResourceManager
          v-show="leftPanel === 'resource'"
          ref="sessionManagerRef"
          :style="{ width: leftPanel === 'resource' ? sessionWidth + 'px' : '0px', minWidth: leftPanel === 'resource' ? '60px' : '0px' }"
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
      <div v-if="leftPanel !== 'none'" class="resize-handle" :class="{ 'handle-overlay': isNarrow }" :style="{ left: sessionWidth + 'px' }" @pointerdown="startResize" />
      <div class="tab-area">
        <TabManager ref="tabManagerRef" :show-toolbar="showToolbar" :tab-orientation="tabOrientation" :vertical-tab-width="verticalTabWidth"
          @new-ssh="openNewSession('', 'ssh')" @new-telnet="openNewSession('', 'telnet')" @new-serial="openNewSession('', 'serial')" @status="onTabStatus" />
      </div>
      <!-- 智能体聊天面板:右侧停靠,可最大化铺满内容区(悬浮覆盖);窄窗时浮层弹出 -->
      <AgentChatPanel
        v-show="showAgentPanel"
        :width="agentPanelWidth"
        :maximized="agentMaximized"
        :class="{ 'agent-maximized': agentMaximized, 'agent-overlay': isNarrow && !agentMaximized }"
        :style="agentMaximized ? undefined : { width: agentPanelWidth + 'px' }"
        @close="closeAgentPanel"
        @toggle-maximize="agentMaximized = !agentMaximized"
        @open-settings="showAgentSettings = true"
        @open-mcp-settings="showMcpSettings = true"
      />
      <!-- AI 面板左侧拖宽手柄(最大化时隐藏) -->
      <div
        v-if="showAgentPanel && !agentMaximized"
        class="agent-resize-handle"
        :class="{ 'agent-handle-overlay': isNarrow }"
        :style="{ right: agentPanelWidth - 2 + 'px' }"
        @pointerdown="startAgentResize"
      />
      <!-- 智能体设置弹窗(独立于主设置) -->
      <AgentSettingsDialog v-model:show="showAgentSettings" />
        </div>
      <div class="global-status-bar">
        <span v-if="globalStatus.text" class="gs-left">{{ globalStatus.text }}</span>
        <span v-if="globalStatus.hasTab && globalStatus.encoding" class="gs-encoding">{{ globalStatus.encoding }}</span>
        <span v-if="globalStatus.hasTab" class="gs-cursor">行 {{ globalStatus.row }}, 列 {{ globalStatus.col }}</span>
      </div>
      </div>
    </div>

    <!-- Folder dialog -->
    <n-modal v-model:show="showNewFolder" :title="t('shellPanel.newFolderTitle')" preset="dialog" :show-icon="false" style="width: 360px" :mask-closable="false">
      <div class="form-group"><label class="form-label">{{ t('common.name') }} <span class="required">*</span></label><n-input v-model:value="newFolderName" @keyup.enter="handleCreateFolder" /></div>
      <template #action><n-button @click="showNewFolder = false">{{ t('common.cancel') }}</n-button><n-button type="primary" @click="handleCreateFolder">{{ t('common.create') }}</n-button></template>
    </n-modal>

    <!-- Session dialog (new / edit) -->
    <n-modal v-model:show="showSessionDialog" :title="isEditMode ? t('shellPanel.editSession') : t('shellPanel.newSession')" preset="dialog" :show-icon="false" style="width: 720px" :mask-closable="false">
      <div class="session-dialog">
        <n-scrollbar style="width: 140px; flex-shrink: 0;">
          <div class="session-type-list">
            <div class="type-item" :class="{ active: selectedProtocol === 'ssh' }" @click="selectedProtocol = 'ssh'">
              <span class="type-name">SSH</span>
              <span class="type-desc">{{ t('shellPanel.sshDesc') }}</span>
            </div>
            <div class="type-item" :class="{ active: selectedProtocol === 'sftp' }" @click="selectedProtocol = 'sftp'">
              <span class="type-name">SFTP</span>
              <span class="type-desc">{{ t('shellPanel.sftpDesc') }}</span>
            </div>
            <div class="type-item" :class="{ active: selectedProtocol === 'telnet' }" @click="selectedProtocol = 'telnet'">
              <span class="type-name">Telnet</span>
              <span class="type-desc">{{ t('shellPanel.telnetDesc') }}</span>
            </div>
            <div class="type-item" :class="{ active: selectedProtocol === 'serial' }" @click="selectedProtocol = 'serial'">
              <span class="type-name">{{ t('shellPanel.serialType') }}</span>
              <span class="type-desc">{{ t('shellPanel.serialDesc') }}</span>
            </div>
            <div class="type-item" :class="{ active: selectedProtocol === 'http' }" @click="selectedProtocol = 'http'; loadBrowsers()">
              <span class="type-name">HTTP</span>
              <span class="type-desc">{{ t('shellPanel.httpDesc') }}</span>
            </div>
            <div class="type-item" :class="{ active: selectedProtocol === 'rdp' }" @click="selectedProtocol = 'rdp'; loadRdpTestServers()">
              <span class="type-name">RDP</span>
              <span class="type-desc">{{ t('shellPanel.rdpDesc') }}</span>
            </div>
          </div>
        </n-scrollbar>
        <n-scrollbar style="flex: 1; min-width: 0;">
          <div class="session-main">
            <div v-if="sessionSide === 'settings'" class="anim-fade">
            <div class="form-group"><label class="form-label">{{ t('common.name') }} <span class="required">*</span></label><n-input v-model:value="sessName" :placeholder="t('shellPanel.sessionName')" /></div>
            <template v-if="selectedProtocol === 'ssh' || selectedProtocol === 'sftp'">
              <div class="form-group"><label class="form-label">{{ t('shellPanel.ipAddress') }} <span class="required">*</span></label><n-input v-model:value="sshHost" :placeholder="t('shellPanel.ipOrDomain')" /></div>
              <div class="form-group"><label class="form-label">{{ t('common.port') }}</label><n-input-number v-model:value="sshPort" :min="1" :max="65535" style="width: 100%" /></div>
              <div class="form-group"><label class="form-label">{{ t('common.username') }}</label><n-input v-model:value="sshUser" :placeholder="t('shellPanel.emptyAtConnect')" /></div>
              <div class="form-group">
                <label class="form-label">{{ t('shellPanel.loginMethod') }}</label>
                <n-radio-group v-model:value="sshAuthMode" size="small">
                  <n-radio-button value="password">{{ t('shellPanel.passwordLogin') }}</n-radio-button>
                  <n-radio-button value="key">{{ t('shellPanel.keyLogin') }}</n-radio-button>
                </n-radio-group>
              </div>
              <div v-if="sshAuthMode === 'password'" class="form-group"><label class="form-label">{{ t('common.password') }}</label><n-input v-model:value="sshPassword" type="password" show-password-on="click" :placeholder="t('shellPanel.emptyAtConnect')" /></div>
              <div v-else class="form-group">
                <label class="form-label">{{ t('shellPanel.key') }}</label>
                <div style="display: flex; gap: 6px; align-items: center; width: 100%">
                  <n-select v-model:value="sshKeyRef" :options="keyOptions" :placeholder="t('shellPanel.selectKey')" filterable clearable style="flex: 1; min-width: 0" @focus="loadKeyList" />
                  <n-button size="small" @click="openKeyCreate">{{ t('common.create') }}</n-button>
                  <n-button size="small" @click="openKeyCopy">{{ t('shellPanel.deployToHost') }}</n-button>
                  <n-button size="small" :disabled="!sshKeyRef" @click="handleDeleteKey(sshKeyRef)">{{ t('common.delete') }}</n-button>
                </div>
              </div>
            </template>
            <template v-else-if="selectedProtocol === 'telnet'">
              <div class="form-group"><label class="form-label">{{ t('shellPanel.ipAddress') }} <span class="required">*</span></label><n-input v-model:value="telnetHost" :placeholder="t('shellPanel.ipOrDomain')" /></div>
              <div class="form-group"><label class="form-label">{{ t('common.port') }}</label><n-input-number v-model:value="telnetPort" :min="1" :max="65535" style="width: 100%" /></div>
              <div class="form-group"><label class="form-label">{{ t('shellPanel.account') }}</label><n-input v-model:value="telnetAccount" :placeholder="t('shellPanel.loginAccount')" /></div>
              <div class="form-group"><label class="form-label">{{ t('common.password') }}</label><n-input v-model:value="telnetPassword" type="password" show-password-on="click" :placeholder="t('shellPanel.loginPassword')" /></div>
            </template>
            <template v-else-if="selectedProtocol === 'serial'">
              <div class="form-group">
                <label class="form-label">{{ t('shellPanel.serialDevicePath') }} <span class="required">*</span></label>
                <n-select v-model:value="serialDevice" :options="serialPorts" :placeholder="t('shellPanel.scanningPorts')" filterable allow-create clearable :loading="scanningPorts" @focus="refreshSerialPorts" />
              </div>
              <div class="form-group"><label class="form-label">{{ t('shellPanel.baudRate') }}</label><n-select v-model:value="serialBaud" :options="serialBaudOptions" /></div>
              <div class="form-group"><label class="form-label">{{ t('shellPanel.dataBits') }}</label><n-input-number v-model:value="serialDataBits" :min="5" :max="8" style="width: 100%" /></div>
              <div class="form-group"><label class="form-label">{{ t('shellPanel.stopBits') }}</label><n-select v-model:value="serialStopBits" :options="stopBitsOptions" /></div>
              <div class="form-group"><label class="form-label">{{ t('shellPanel.parity') }}</label><n-select v-model:value="serialParity" :options="parityOptions" /></div>
            </template>
            <template v-else-if="selectedProtocol === 'http'">
              <div class="form-group"><label class="form-label">URL <span class="required">*</span></label><n-input v-model:value="httpUrl" placeholder="https://example.com" /></div>
              <div class="form-group">
                <label class="form-label">{{ t('shellPanel.browser') }}</label>
                <div style="display: flex; gap: 6px; align-items: center; width: 100%">
                  <n-select v-model:value="httpBrowser" :options="browserOptions" :placeholder="t('shellPanel.selectBrowser')" clearable style="flex: 1; min-width: 0" @focus="loadBrowsers" />
                  <n-button size="small" @click="loadBrowsers">{{ t('shellPanel.rescan') }}</n-button>
                </div>
              </div>
              <div class="http-hint">
                {{ t('shellPanel.httpHint') }}
                <br />{{ t('shellPanel.httpHint1') }}
                <br />2. {{ t('shellPanel.httpHint2') }}
                <br />{{ t('shellPanel.httpHint3') }}
                <br />{{ t('shellPanel.httpHint4') }}
              </div>
            </template>
            <template v-else-if="selectedProtocol === 'rdp'">
              <div class="form-group"><label class="form-label">{{ t('shellPanel.ipAddress') }} <span class="required">*</span></label><n-input v-model:value="rdpHost" :placeholder="t('shellPanel.ipOrDomain')" /></div>
              <div class="form-group"><label class="form-label">{{ t('common.port') }}</label><n-input-number v-model:value="rdpPort" :min="1" :max="65535" style="width: 100%" /></div>
              <div class="form-group"><label class="form-label">{{ t('common.username') }}</label><n-input v-model:value="rdpUser" :placeholder="t('shellPanel.loginAccount')" /></div>
              <div class="form-group"><label class="form-label">{{ t('common.password') }}</label><n-input v-model:value="rdpPassword" type="password" show-password-on="click" :placeholder="t('shellPanel.loginPassword')" /></div>
              <div class="form-group">
                <label class="form-label">{{ t('shellPanel.testServer') }}</label>
                <n-select v-model:value="rdpTestSel" :options="rdpTestServers" :placeholder="t('shellPanel.testServerPlaceholder')" filterable clearable style="width: 100%" @update:value="applyRdpTestServer" />
              </div>
            </template>
          </div>
          <div v-else-if="sessionSide === 'meta'" class="anim-fade">
            <n-descriptions bordered :column="1" size="medium" label-style="width:100px" style="max-width: 400px; margin: 0 auto">
              <n-descriptions-item :label="t('shellPanel.createdTime')">{{ sessCreated || '--' }}</n-descriptions-item>
              <n-descriptions-item :label="t('shellPanel.updatedTime')">{{ sessUpdated || '--' }}</n-descriptions-item>
              <n-descriptions-item :label="t('shellPanel.protocol')">{{ selectedProtocol }}</n-descriptions-item>
            </n-descriptions>
          </div>
          <div v-else-if="sessionSide === 'advanced'" class="anim-fade">
            <div style="font-size: 13px; color: var(--text-color, #d4d4d4); margin-bottom: 12px; line-height: 1.6">
              {{ t('shellPanel.advancedHint') }}
            </div>
            <div class="cipher-buttons">
              <n-button size="tiny" @click="setModernCiphers">{{ t('shellPanel.modernDefault') }}</n-button>
              <n-button size="tiny" @click="setAllCiphers">{{ t('shellPanel.selectAll') }}</n-button>
              <n-button size="tiny" @click="sessAllowedCiphers = []">{{ t('common.clear') }}</n-button>
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
        <n-button @click="showSessionDialog = false">{{ t('common.cancel') }}</n-button>
        <n-button v-if="isEditMode && (selectedProtocol === 'ssh' || selectedProtocol === 'sftp')" size="small" style="margin-right: 8px" @click="sessionSide = sessionSide === 'advanced' ? 'settings' : 'advanced'">{{ t('shellPanel.advanced') }}</n-button>
        <n-button v-if="isEditMode && (selectedProtocol === 'ssh' || selectedProtocol === 'sftp')" size="small" style="margin-right: 8px" @click="sessionSide = sessionSide === 'meta' ? 'settings' : 'meta'">{{ t('shellPanel.meta') }}</n-button>
        <n-button type="primary" @click="isEditMode ? handleUpdateSession() : handleCreateSession()">{{ isEditMode ? t('common.save') : t('common.create') }}</n-button>
      </template>
    </n-modal>
    <ExportDialog v-model:show="showExport" @done="()=>{}" />
    <ImportDialog v-model:show="showImport" @done="()=>{}" />
    <KeyCreateDialog v-model:show="showKeyCreate" @created="handleKeyCreated" />
    <SshCopyDialog v-model:show="showKeyCopy" :selected-key="sshKeyRef" :host="sshHost" :port="sshPort" :user="sshUser" @done="handleKeyCopied" />

    <!-- About / help dialog -->
    <n-modal v-model:show="showAbout" :title="t('shellPanel.helpTitle')" preset="dialog" :show-icon="false" style="width: 420px" :mask-closable="false">
      <div class="about-body">
        <div class="about-name">AceShell</div>
        <div class="about-desc">{{ t('shellPanel.aboutDesc') }}</div>
        <div class="about-version">{{ t('shellPanel.versionLabel', { ver: appVersion }) }}</div>
        <div class="about-links">
          <div class="about-item"><span class="about-label">{{ t('shellPanel.projectUrl') }}</span><a class="about-link" href="#" @click.prevent="openExternal('https://github.com/dingtongbin/AceShell')">https://github.com/dingtongbin/AceShell</a></div>
          <div class="about-item"><span class="about-label">{{ t('shellPanel.authorBlog') }}</span><a class="about-link" href="#" @click.prevent="openExternal('https://dingtongbin.cn/')">https://dingtongbin.cn/</a></div>
        </div>
      </div>
      <template #action><n-button type="primary" @click="showAbout = false">{{ t('common.confirm') }}</n-button></template>
    </n-modal>

    <!-- 文件编辑器未保存关闭确认 -->
    <n-modal :show="editorClosePending.length > 0" :title="t('shellPanel.unsavedTitle')" preset="dialog" :show-icon="false" style="width: 400px" :mask-closable="false">
      <div style="font-size: 14px">
        <p>{{ t('shellPanel.unsavedMsg', { file: editorClosePending[0]?.fileName }) }}</p>
        <p style="margin-top: 8px; color: #e45858; font-size: 12px">{{ t('shellPanel.unsavedAsk') }}</p>
      </div>
      <template #action>
        <n-button @click="cancelEditorClose">{{ t('common.cancel') }}</n-button>
        <n-button @click="confirmEditorCloseDiscard">{{ t('shellPanel.discard') }}</n-button>
        <n-button type="primary" @click="confirmEditorCloseSave">{{ t('shellPanel.saveAndClose') }}</n-button>
      </template>
    </n-modal>

    <!-- MCP 设置弹窗(入口: AI 聊天面板标题栏) -->
    <McpSettingsPanel v-model:show="showMcpSettings" />

    <!-- MCP 绝对危险指令拦截弹窗:命令已被拒绝执行,MCP 已自动挂起 -->
    <n-modal :show="!!criticalBlock" :title="t('mcp.criticalTitle')" preset="dialog" :show-icon="false" style="width: 480px" :mask-closable="false">
      <div style="font-size: 14px; line-height: 1.7">
        <p style="color: #e45858; font-weight: 600">{{ t('mcp.criticalMsg') }}</p>
        <pre style="margin: 10px 0; padding: 8px 10px; background: rgba(0,0,0,0.35); border-radius: 4px; font-size: 12px; font-family: Consolas, 'Courier New', monospace; white-space: pre-wrap; word-break: break-all; max-height: 160px; overflow: auto">{{ criticalBlock?.command }}</pre>
        <p style="font-size: 12px; color: var(--text-secondary, #888)">{{ t('mcp.criticalReason') }}: {{ criticalBlock?.reason }}</p>
        <p style="font-size: 12px; color: var(--text-secondary, #888)">{{ t('mcp.criticalHint') }}</p>
      </div>
      <template #action>
        <n-button type="primary" @click="criticalBlock = null">{{ t('common.confirm') }}</n-button>
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

/* AI 面板左侧拖宽手柄(悬停/拖拽时高亮) */
.agent-resize-handle {
  position: absolute;
  top: 0;
  bottom: 0;
  width: 5px;
  cursor: col-resize;
  background: transparent;
  z-index: 10;
  transition: background 0.15s;
}
.agent-resize-handle:hover,
.agent-resize-handle:active {
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

/* 智能体面板最大化:悬浮铺满内容区(不挤压终端区) */
.agent-maximized {
  position: absolute;
  inset: 0;
  z-index: 800;
}

/* 窄窗浮层模式(宽度 < NARROW_THRESHOLD): 资源管理器/AI 面板悬浮于标签页之上,
   不再挤压终端区;层级 标签页 < 资源管理器(600) < AI 面板(700) < AI 最大化(800) */
.shell-sidebar.sidebar-overlay {
  position: absolute;
  left: 0;
  top: 0;
  bottom: 0;
  z-index: 600;
  box-shadow: 2px 0 12px rgba(0, 0, 0, 0.35);
}
.agent-overlay {
  position: absolute;
  right: 0;
  top: 0;
  bottom: 0;
  z-index: 700;
  box-shadow: -2px 0 12px rgba(0, 0, 0, 0.35);
}
.resize-handle.handle-overlay { z-index: 610; }
.agent-resize-handle.agent-handle-overlay { z-index: 710; }

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
  margin-top: 2px;
}
.about-version {
  font-size: 12px;
  color: var(--text-secondary, #888);
  margin-top: 2px;
}
.about-links {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-top: 10px;
}
.about-item {
  display: flex;
  align-items: baseline;
  gap: 8px;
  font-size: 13px;
}
.about-label {
  color: var(--text-secondary, #888);
  flex-shrink: 0;
}
.about-link {
  font-size: 13px;
  color: #0078d4;
  text-decoration: none;
  word-break: break-all;
}
.about-link:hover {
  text-decoration: underline;
  color: #4ec9b0;
}
</style>
