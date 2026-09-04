<script setup lang="ts">
import { ref, computed, nextTick, onMounted, onBeforeUnmount, watch, h, inject } from 'vue'
import type { Terminal } from '@xterm/xterm'
import type { FitAddon } from '@xterm/addon-fit'
import { NIcon, NEmpty, NButton, NDropdown, NModal, NCheckbox, NSelect, useMessage } from 'naive-ui'
import type { DropdownOption } from 'naive-ui'
import { CloseOutline, TerminalOutline, RadioOutline, ChevronDownOutline, ChevronBackOutline, ChevronForwardOutline, FolderOpenOutline, RefreshOutline, OpenOutline, ServerOutline, GlobeOutline, HardwareChipOutline, AddOutline, CopyOutline, ClipboardOutline } from '@vicons/ionicons5'
import { ConnectToSession, ConnectToSessionWithCreds, LoadSession, SetNoConfirmClose } from '../../bindings/changeme/internal/services/sessionfileservice.js'
import { GetConfig } from '../../bindings/changeme/internal/services/configservice.js'
import { Send as TelnetSend, Disconnect as TelnetDisconnect } from '../../bindings/changeme/internal/services/directtelnetservice.js'
import { Send as SSHSend, Disconnect as SSHDisconnect, Resize as SSHResize } from '../../bindings/changeme/internal/services/sshservice.js'
import { Send as SerialSend, Disconnect as SerialDisconnect, Connect as SerialConnect } from '../../bindings/changeme/internal/services/serialservice.js'
import { Send as LocalSend, Disconnect as LocalDisconnect, Resize as LocalResize } from '../../bindings/changeme/internal/services/localservice.js'
import { SaveLogFile } from '../../bindings/changeme/internal/services/windowservice.js'
import { createXterm, resetTermComposition, type TermConfig } from '../composables/useXterm'
import type { EditorView } from '@codemirror/view'
import { createShellEditor } from '../composables/useShellEditor'
import { useSessionAuth } from '../composables/useSessionAuth'
import FingerprintDialog from './FingerprintDialog.vue'
import CredentialsDialog from './CredentialsDialog.vue'
import KeyCredentialsDialog from './KeyCredentialsDialog.vue'
import SftpPanel from './SftpPanel.vue'
import { Disconnect as SftpDisconnect } from '../../bindings/changeme/internal/services/sftpservice.js'
import { Copy as ClipboardCopy, Paste as ClipboardPaste } from '../../bindings/changeme/internal/services/clipboardservice.js'
import type { Pane, Tab, TabPaneApi, ComponentTabOptions, ComponentTabPatch, PaneActions, PaneCtx, ActiveTabState } from './tabTypes'
import { dragState } from './tabTypes'
import { useI18n } from 'vue-i18n'
import { useMcpBridge } from '../composables/useMcpBridge'

const { notifyUserInput: mcpNotifyUserInput } = useMcpBridge()

const props = defineProps<{
  pane: Pane
  showToolbar: boolean
  isVertical: boolean
  verticalWidth: number
  termCfg: TermConfig | null
  showWelcomePaneId: string | null
}>()

const emit = defineEmits<{
  (e: 'new-ssh'): void
  (e: 'new-telnet'): void
  (e: 'new-serial'): void
}>()

// 模块级终端查找注册表:布局拆分/合并会导致 TabPane 组件销毁重建,
// 旧终端实例的输入回调(闭包)不依赖组件作用域,改经注册表定位标签页,组件重建后依然可用。
// 每条目记录组件实例身份:组件重建可能"先挂新、后卸旧",旧实例卸载时不得删除新实例的注册
const paneTabRegistry = new Map<string, { tabs: Tab[]; instanceId: number }>()
let paneInstanceSeq = 0

const pane = props.pane
const paneInstanceId = ++paneInstanceSeq
const ctx = inject<PaneCtx>('pane-ctx')
const actions: PaneActions = ctx?.actions ?? {
  onSplit: () => {}, onMoveTab: () => {}, onSplitAt: () => {}, onFocus: () => {}, onStatus: () => {}, onActiveTabState: () => {}, registerPane: () => () => {}, paneExists: () => true, openRdp: () => {}, openVnc: () => {},
}

const message = useMessage()
const { t } = useI18n()

function findTabById(id: string): Tab | null {
  for (const { tabs } of paneTabRegistry.values()) {
    const t = tabs.find(x => x.id === id)
    if (t) return t
  }
  return null
}

function isConnectedFor(id: string): boolean {
  for (const { tabs } of paneTabRegistry.values()) {
    const t = tabs.find(x => x.id === id)
    if (t) return t.status === 'connected'
  }
  return false
}

async function sendToTab(id: string, p: string, d: string) {
  try {
    if (p === 'ssh') await SSHSend(id, d)
    else if (p === 'serial') await SerialSend(id, d)
    else if (p === 'shell') await LocalSend(id, d)
    else await TelnetSend(id, d)
  } catch {
    const tab = findTabById(id)
    if (tab?.terminal) {
      tab.terminal.write('\r\n\x1b[31m' + t('tabPane.sendFailed') + '\x1b[0m\r\n')
    }
  }
}

// 写入终端提示;无终端(SFTP 会话)时改用全局消息提示
function termWrite(tab: Tab, text: string) {
  if (tab.terminal) {
    tab.terminal.write(text)
  } else {
    message.error(text.replace(/\x1b\[[0-9;]*m/g, '').trim())
  }
}

const showCloseConfirm = ref(false)
const confirmTab = ref<Tab | null>(null)
const confirmNoAsk = ref(false)

// 多行粘贴确认(CodeMirror 可编辑+行号+语法高亮)
const showPasteConfirm = ref(false)
const pasteContent = ref('')
const pasteTargetId = ref('')
const pasteEditorEl = ref<HTMLElement | null>(null)
let pasteEditorView: EditorView | null = null

function openPasteEditor(text: string) {
  pasteContent.value = text
  showPasteConfirm.value = true
  nextTick(() => {
    if (!pasteEditorEl.value) return
    pasteEditorView?.destroy()
    pasteEditorView = null
    pasteEditorView = createShellEditor(pasteEditorEl.value, pasteContent.value, doc => { pasteContent.value = doc })
    pasteEditorView.focus()
  })
}

function disposePasteEditor() {
  pasteEditorView?.destroy()
  pasteEditorView = null
}

// 标签页右键菜单
const tabCtxShow = ref(false)

// 执行脚本对话框(CodeMirror 编辑器,样式与多行粘贴确认一致)
const showScriptDialog = ref(false)
const scriptContent = ref('')
const scriptTargetTab = ref<Tab | null>(null)
const scriptFileInputRef = ref<HTMLInputElement | null>(null)
const scriptEditorEl = ref<HTMLElement | null>(null)
let scriptEditorView: EditorView | null = null
const tabCtxX = ref(0)
const tabCtxY = ref(0)
const tabCtxOptions = ref<DropdownOption[]>([])
const tabCtxTarget = ref<Tab | null>(null)

// 连接认证流程(SSH 指纹/凭证、SFTP、HTTP、串口):状态与函数集中在 useSessionAuth
const auth = useSessionAuth({ pane, termWrite, initTerminal, openSftpPanel })
const {
  showBrowserFail, browserFailUrl, browserFailPath, browserFailSel, browserFailOptions,
  loadBrowserFailOptions, retryOpenUrlWithBrowser, closeBrowserFail,
  showFingerprint, fingerprintHost, fingerprintPort, fingerprintFolder, fingerprintKey, fingerprintStatus,
  onFingerprintConfirm, onFingerprintSkip, onFingerprintCancel,
  showCredentials, credHost, credUsername, credHasPassword, credTitle,
  onCredentialsSubmit, onCredentialsCancel,
  showKeyCredentials, keyCredHost, keyCredUsername,
  onKeyCredentialsSubmit, onKeyCredentialsCancel,
  sshAuthFlow, sshConnectLoop, openSftpSession, openHttpSession, openSerial,
} = auth

let dragTabIdx: number | null = null

const tabsContainerRef = ref<HTMLElement | null>(null)
const termSheetRef = ref<HTMLElement | null>(null)
let unregisterPane: (() => void) | null = null
const canScrollLeft = ref(false)
const canScrollRight = ref(false)

const verticalWidth = ref(props.verticalWidth)
let isResizingVertical = false

function gIcon(p: string) { switch (p) { case 'serial': return RadioOutline; case 'shell': return TerminalOutline; default: return TerminalOutline } }
function gColor(p: string) { switch (p) { case 'ssh': return '#4ec9b0'; case 'telnet': return '#569cd6'; case 'serial': return '#c586c0'; case 'shell': return '#dcdcaa'; default: return '#6e9fc7' } }
function gStatus(s: string) { switch (s) { case 'connected': return '#4ec9b0'; case 'connecting': return '#f2c97d'; case 'error': return '#e45858'; default: return '#555' } }

async function openSession(sessionPath: string): Promise<string> {
  let meta: any
  try { meta = JSON.parse(await LoadSession(sessionPath)) } catch (e: any) { console.error('Load error:', e); return '' }
  // SFTP 会话：复用 SSH 认证流程，连接成功后打开 SFTP 面板标签页（不创建终端标签页）
  if ((meta.protocol || 'telnet') === 'sftp') {
    openSftpSession(sessionPath, meta)
    return ''
  }
  // HTTP 会话：直接打开所选浏览器的新标签页（不创建应用内标签页）
  if ((meta.protocol || 'telnet') === 'http') {
    openHttpSession(sessionPath, meta)
    return ''
  }
  // RDP 会话：图形标签页,按 (host:port) 去重,已存在则定位激活
  if ((meta.protocol || 'telnet') === 'rdp') {
    actions.openRdp({ sessionPath, name: meta.name || meta.host, host: meta.host, port: meta.port })
    return ''
  }
  // VNC 会话:图形标签页(kind='vnc'),按 (host:port) 去重,已存在则定位激活
  if ((meta.protocol || 'telnet') === 'vnc') {
    actions.openVnc({ sessionPath, name: meta.name || meta.host, host: meta.host, port: meta.port })
    return ''
  }
  const tabId = sessionPath + '@' + Date.now() + '-' + Math.random().toString(36).slice(2, 8)
  const tabData: Tab = { id: tabId, sessionPath, title: meta.name || sessionPath, protocol: meta.protocol || 'telnet', host: meta.host, port: meta.port, username: meta.username || '', status: 'connecting', terminal: null, fitAddon: null, logBuffer: '', kind: 'terminal' }
  pane.tabs.push(tabData); pane.activeTabId = tabId
  nextTick(async () => {
    const tab = pane.tabs.find(t => t.id === tabId)
    if (!tab) return
    initTerminal(tab)

    // SSH 连接流程：指纹校验 → 凭证 → 连接（密码错误自动重试）
    if (tab.protocol === 'ssh') {
      await sshConnectLoop(tab, sessionPath, meta, async () => {
        if (tab.terminal) SSHResize(tabId, tab.terminal.cols, tab.terminal.rows).catch(() => {})
      })
      return
    }

    // 非 SSH 协议直接连接
    try {
      await ConnectToSession(sessionPath, tabId)
      tab.status = 'connected'
    } catch (e: any) {
      tab.status = 'error'
      tab.terminal?.write(`\r\n\x1b[31m${t('tabPane.connectFailed', { err: e.message || e })}\x1b[0m\r\n`)
    }
  })
  return tabId
}


// 泛化标签页：打开任意组件内容（不占用终端渲染路径，布局不变）
function openComponentTab(opts: ComponentTabOptions): string {
  const tabId = 'component://' + Date.now() + '-' + Math.random().toString(36).slice(2, 8)
  const tabData: Tab = {
    id: tabId,
    title: opts.title,
    kind: opts.kind ?? 'component',
    sessionPath: opts.sessionPath ?? '',
    protocol: opts.protocol ?? '',
    host: opts.host ?? '',
    port: opts.port ?? 0,
    status: opts.status ?? 'idle',
    terminal: null,
    fitAddon: null,
    logBuffer: '',
    component: opts.component,
    componentProps: opts.props,
    onClose: opts.onClose,
    dirty: opts.dirty ?? false,
    icon: opts.icon,
    color: opts.color,
  }
  pane.tabs.push(tabData)
  pane.activeTabId = tabId
  return tabId
}

function updateComponentTab(tabId: string, patch: ComponentTabPatch) {
  const tab = pane.tabs.find(t => t.id === tabId)
  if (!tab || (tab.kind !== 'component' && tab.kind !== 'vnc')) return
  if (patch.title !== undefined) tab.title = patch.title
  if (patch.props !== undefined) tab.componentProps = patch.props
  if (patch.status !== undefined) tab.status = patch.status
  if (patch.dirty !== undefined) tab.dirty = patch.dirty
}

function closeTabById(tabId: string) {
  const tab = pane.tabs.find(t => t.id === tabId)
  if (tab) doCloseTab(tab)
}

let resizeObserver: ResizeObserver | null = null
let fitDebounce: ReturnType<typeof setTimeout> | null = null

function fitAndResize(terminal: Terminal | null, fitAddon: FitAddon | null, id: string, protocol?: string) {
  if (!terminal || !fitAddon) return
  try { fitAddon.fit() } catch {}
  const cols = terminal.cols
  const rows = terminal.rows
  if (protocol === 'ssh') SSHResize(id, cols, rows).catch(() => {})
  else if (protocol === 'shell') LocalResize(id, cols, rows).catch(() => {})
}

function scheduleFit(tab: Tab) {
  if (fitDebounce) clearTimeout(fitDebounce)
  fitDebounce = setTimeout(() => {
    fitAndResize(tab.terminal, tab.fitAddon, tab.id, tab.protocol)
  }, 100)
}

function initTerminal(tab: Tab): Terminal | null {
  if (tab.terminal) {
    // 实例已存在(跨 pane 移动):重新挂载 xterm DOM 到本 pane 容器,内容不丢
    const container = document.getElementById(`term-${tab.id}`)
    if (container && tab.terminal.element && !container.contains(tab.terminal.element)) {
      container.appendChild(tab.terminal.element)
      resetTermComposition(tab.terminal)
      tab.terminal.focus()
    }
    return tab.terminal
  }
  const container = document.getElementById(`term-${tab.id}`)
  if (!container) return null
  const created = createTerminal(container, tab.id, tab.protocol)
  tab.terminal = created.terminal
  tab.fitAddon = created.fitAddon
  tab.terminalCleanup = created.cleanup
  if (tab.logBuffer) {
    tab.terminal.write(tab.logBuffer)
    tab.logBuffer = ''
  }
  return created.terminal
}

// 懒重建:迁移过的 xterm 实例键盘输入会失效,用户首次交互(点击终端)时
// 重建为全新实例(与新建标签页同路径),内容由 logBuffer 完整回放
function rebuildTerminal(tab: Tab): Terminal | null {
  disposeTerminal(tab)
  tab.terminalRebuild = false
  return initTerminal(tab)
}

function disposeTerminal(tab: Tab) {
  try { tab.terminalCleanup?.() } catch {}
  tab.terminalCleanup = undefined
  try { tab.terminal?.dispose() } catch {}
  tab.terminal = null
  tab.fitAddon = null
}

function createTerminal(container: HTMLElement, targetId: string, protocol: string): { terminal: Terminal; fitAddon: FitAddon; cleanup: () => void } {
  // 防御:容器内若残留旧实例 DOM(如重复初始化/重连路径),先清空再挂载,避免多实例叠加
  container.innerHTML = ''
  return createXterm(container, {
    isConnected: () => isConnectedFor(targetId),
    onData: (d: string) => {
      mcpNotifyUserInput()
      if (isConnectedFor(targetId)) {
        sendToTab(targetId, protocol, d)
      } else if (d === '\r') {
        // 断开状态下按回车 = 重连(保留终端历史)
        const tab = findTabById(targetId)
        if (tab && (tab.status === 'idle' || tab.status === 'error')) reconnectTab(tab)
      }
    },
    onPaste: (text: string) => {
      mcpNotifyUserInput()
      if (isConnectedFor(targetId)) sendToTab(targetId, protocol, text)
    },
    onMultiLinePaste: (text: string) => {
      mcpNotifyUserInput()
      pasteTargetId.value = targetId
      openPasteEditor(text)
    },
    onClipboardFeedback: (type: string, msg: string) => {
      if (type === 'copy-ok' || type === 'paste-ok') message.success(msg)
      else message.warning(msg)
    },
  }, props.termCfg)
}

// 终端区点击:设置 pane 焦点并聚焦活动终端;若该终端为迁移过的实例(懒重建标记),
// 先重建为全新实例(内容回放)再聚焦,确保键盘输入恢复
function onTermAreaMousedown() {
  actions.onFocus(pane.id)
  const t = pane.tabs.find(x => x.id === pane.activeTabId)
  if (!t) return
  if (t.terminalRebuild) {
    rebuildTerminal(t)
  }
  t.terminal?.focus()
  scheduleFit(t)
}

function handleResize() {
  const active = pane.tabs.find(t => t.id === pane.activeTabId)
  if (active) scheduleFit(active)
}

// 多行粘贴确认
function confirmPaste() {
  const targetId = pasteTargetId.value
  const outer = findTabById(targetId)
  if (outer && outer.status === 'connected') {
    sendToTab(outer.id, outer.protocol, pasteContent.value)
    if (mcpSendResolve) { mcpSendResolve({ ok: true }); mcpSendResolve = null }
  } else if (mcpSendResolve) {
    mcpSendResolve({ ok: false, note: 'tab not connected' }); mcpSendResolve = null
  }
  showPasteConfirm.value = false
  disposePasteEditor()
  pasteContent.value = ''
  pasteTargetId.value = ''
}

function cancelPaste() {
  showPasteConfirm.value = false
  disposePasteEditor()
  pasteContent.value = ''
  pasteTargetId.value = ''
  if (mcpSendResolve) { mcpSendResolve({ ok: false, note: 'user canceled multiline input' }); mcpSendResolve = null }
}

// ==================== MCP 桥接(与用户操作完全相同的路径) ====================

// MCP 多行输入的粘贴弹窗决议回调(用户确认/取消后回执后端)
let mcpSendResolve: ((v: { ok: boolean; note?: string }) => void) | null = null

// mcpTerminalSend MCP 向终端输入文本:
//   - activateTab=true(默认) → 先激活目标标签页(MCP 操作对用户可见,界面同步跳转)
//   - needPasteConfirm=true(手动模式多行) → 走与用户粘贴完全相同的多行确认弹窗
//   - 否则直接送入终端;单行命令缺换行时补 \n(与用户敲回车一致)
function mcpTerminalSend(tabId: string, text: string, needPasteConfirm: boolean, activateTab = true): Promise<{ ok: boolean; note?: string }> {
  return new Promise(resolve => {
    const tab = pane.tabs.find(t => t.id === tabId)
    if (!tab || tab.kind !== 'terminal') {
      resolve({ ok: false, note: 'terminal tab not found: ' + tabId })
      return
    }
    if (activateTab) switchTab(tabId)
    if (needPasteConfirm) {
      mcpSendResolve = resolve
      pasteTargetId.value = tabId
      openPasteEditor(text)
      return
    }
    if (tab.status !== 'connected') {
      resolve({ ok: false, note: 'tab not connected' })
      return
    }
    let payload = text
    if (!text.includes('\n') && !text.includes('\r')) payload = text + '\n'
    sendToTab(tabId, tab.protocol, payload)
    resolve({ ok: true })
  })
}

// mcpCloseTab MCP 关闭标签页: 与用户手动关闭完全一致(未保存脚本弹确认;用户取消则回执失败)
// activateTab=false 时不跳转(后台化操作)
async function mcpCloseTab(tabId: string, activateTab = true): Promise<{ ok: boolean; note?: string }> {
  const tab = pane.tabs.find(t => t.id === tabId)
  if (!tab) return { ok: false, note: 'tab not found: ' + tabId }
  if (activateTab) switchTab(tabId)
  await doCloseTab(tab)
  const still = pane.tabs.some(t => t.id === tabId)
  return still ? { ok: false, note: 'user canceled close' } : { ok: true }
}

// 标签页右键菜单
function openTabContextMenu(e: MouseEvent, tab: Tab) {
  e.preventDefault()
  e.stopPropagation()
  tabCtxTarget.value = tab
  tabCtxOptions.value = getTabContextMenu(tab)
  tabCtxX.value = e.clientX
  tabCtxY.value = e.clientY
  tabCtxShow.value = true
}

function getTabContextMenu(tab: Tab): DropdownOption[] {
  const items: DropdownOption[] = []
  if (tab.kind === 'component' || tab.kind === 'vnc') {
    items.push({ label: t('tabPane.closeTab'), key: 'close', icon: () => h(NIcon, { size: 14 }, { default: () => h(CloseOutline) }) })
    return items
  }
  if (tab.protocol === 'ssh' && tab.status === 'connected') {
    items.push({ label: t('tabPane.openSftp'), key: 'sftp', icon: () => h(NIcon, { size: 14 }, { default: () => h(FolderOpenOutline) }) })
    items.push({ type: 'divider', key: 'd1' })
  }
  items.push({ label: t('tabPane.reconnect'), key: 'reconnect', icon: () => h(NIcon, { size: 14 }, { default: () => h(RefreshOutline) }) })
  items.push({ label: t('tabPane.reopenInNewTab'), key: 'new-tab', icon: () => h(NIcon, { size: 14 }, { default: () => h(OpenOutline) }) })
  if (!props.isVertical) {
    items.push({ type: 'divider', key: 'd2' })
    items.push({ label: t('tabPane.splitRight'), key: 'split-right', icon: () => h(NIcon, { size: 14 }, { default: () => h(ChevronForwardOutline) }) })
    items.push({ label: t('tabPane.splitDown'), key: 'split-down', icon: () => h(NIcon, { size: 14 }, { default: () => h(ChevronDownOutline) }) })
  }
  if (tab.status === 'connected' || tab.status === 'connecting') {
    items.push({ type: 'divider', key: 'd3' })
    items.push({ label: t('tabPane.disconnectConn'), key: 'disconnect', icon: () => h(NIcon, { size: 14 }, { default: () => h(CloseOutline) }) })
  }
  items.push({ type: 'divider', key: 'd4' })
  items.push({ label: t('tabPane.closeTab'), key: 'close', icon: () => h(NIcon, { size: 14 }, { default: () => h(CloseOutline) }) })
  return items
}

function handleTabCtxSelect(key: string) {
  tabCtxShow.value = false
  const tab = tabCtxTarget.value
  if (!tab) return
  switch (key) {
    case 'sftp':
      openSftp(tab)
      break
    case 'reconnect':
      reconnectTab(tab)
      break
    case 'new-tab':
      if (tab.sessionPath) openSession(tab.sessionPath)
      break
    case 'split-right':
      actions.onSplit(pane.id, tab.id, 'h')
      break
    case 'split-down':
      actions.onSplit(pane.id, tab.id, 'v')
      break
    case 'disconnect':
      disconnectSession(tab)
      break
    case 'close':
      handleCloseTab(tab)
      break
  }
}

function tryCloseTab(tab: Tab) { confirmTab.value = tab; confirmNoAsk.value = false; showCloseConfirm.value = true }
async function confirmCloseTab() {
  const tab = confirmTab.value; showCloseConfirm.value = false
  if (!tab) return
  if (confirmNoAsk.value) { try { await SetNoConfirmClose(tab.sessionPath, true) } catch {} }
  doCloseTab(tab)
}
function cancelClose() { showCloseConfirm.value = false; confirmTab.value = null }

async function doCloseTab(tab: Tab) {
  const idx = pane.tabs.findIndex(t => t.id === tab.id); if (idx === -1) return
  if (tab.kind === 'component' || tab.kind === 'vnc') {
    const ok = await tab.onClose?.()
    if (ok === false) return
    const cur = pane.tabs.findIndex(t => t.id === tab.id); if (cur === -1) return
    pane.tabs.splice(cur, 1)
    if (pane.activeTabId === tab.id) {
      pane.activeTabId = pane.tabs.length > 0 ? pane.tabs[Math.min(cur, pane.tabs.length - 1)].id : null
    }
    return
  }
  // 关闭挂在该 SSH 连接上的 SFTP 面板标签页
  if (tab.protocol === 'ssh') closeSftpPanelsOf(tab.id)
  disposeTerminal(tab)
  if (tab.status === 'connected' || tab.status === 'connecting') {
    if (tab.protocol === 'ssh') SSHDisconnect(tab.id).catch(() => {})
    else if (tab.protocol === 'serial') SerialDisconnect(tab.id).catch(() => {})
    else if (tab.protocol === 'shell') LocalDisconnect(tab.id).catch(() => {})
    else TelnetDisconnect(tab.id).catch(() => {})
  }
  pane.tabs.splice(idx, 1)
  if (pane.activeTabId === tab.id) {
    pane.activeTabId = pane.tabs.length > 0 ? pane.tabs[Math.min(idx, pane.tabs.length - 1)].id : null
  }
  nextTick(() => { const a = pane.tabs.find(t => t.id === pane.activeTabId); if (a) scheduleFit(a) })
}

async function handleCloseTab(tab: Tab) {
  if (tab.kind === 'component' || tab.kind === 'vnc') { doCloseTab(tab); return }
  // 会话级"关闭不确认"优先
  if (tab.sessionPath) {
    try {
      const meta = JSON.parse(await LoadSession(tab.sessionPath))
      if (meta.noConfirmClose) { doCloseTab(tab); return }
    } catch {}
  }
  // 全局设置:关闭标签页不弹确认 → 直接关闭(默认弹窗;串口等无会话文件标签页同样跟随)
  if (!globalCloseConfirm) { doCloseTab(tab); return }
  tryCloseTab(tab)
}

// 全局"关闭标签页是否弹确认"(设置-标签),默认弹窗
let globalCloseConfirm = true
async function loadGlobalCloseConfirm() {
  try {
    const cfg = JSON.parse(await GetConfig())
    globalCloseConfirm = cfg.view?.closeConfirm ?? true
  } catch {}
}

function switchTab(id: string) {
  pane.activeTabId = id
  actions.onFocus(pane.id)
  nextTick(() => {
    const t = pane.tabs.find(t => t.id === id)
    if (!t) return
    // 迁移过的实例键盘失效,切换时直接重建为全新实例(内容由 logBuffer 回放)
    if (t.terminalRebuild) rebuildTerminal(t)
    t.terminal?.focus()
    scheduleFit(t)
  })
}

function reconnectAll() { pane.tabs.filter(t => t.kind === 'terminal' && (t.status === 'idle' || t.status === 'error')).forEach(t => reconnectTab(t)) }
async function closeAll() {
  for (const t of [...pane.tabs]) {
    await doCloseTab(t)
  }
}
function closeDisconnected() { [...pane.tabs].filter(t => t.kind === 'terminal' && (t.status === 'idle' || t.status === 'error')).forEach(t => doCloseTab(t)) }
function disconnectTab(tab: Tab) {
  if (!tab.protocol) return
  if (tab.protocol === 'ssh') SSHDisconnect(tab.id).catch(() => {})
  else if (tab.protocol === 'serial') SerialDisconnect(tab.id).catch(() => {})
  else if (tab.protocol === 'shell') LocalDisconnect(tab.id).catch(() => {})
  else TelnetDisconnect(tab.id).catch(() => {})
}

// 关闭挂在指定 SSH 连接上的 SFTP 面板标签页（连接断开后面板失效，一并关闭）
function closeSftpPanelsOf(connID: string) {
  for (const t of [...pane.tabs]) {
    if (t.kind === 'component' && t.componentProps?.sessionID === connID) doCloseTab(t)
  }
}

function disconnectSession(tab: Tab) {
  disconnectTab(tab)
  tab.terminal?.write('\r\n\x1b[33m' + t('tabPane.disconnected') + '\x1b[0m\r\n')
  tab.terminal?.write('\x1b[90m' + t('tabPane.enterToReconnect') + '\x1b[0m\r\n')
}

async function reconnectTab(tab: Tab) {
  if (!tab.sessionPath && tab.protocol !== 'serial') return
  if (tab.status === 'connected' || tab.status === 'connecting') {
    disconnectTab(tab)
  }
  // 保留现有 xterm 实例: 屏幕历史原样保留,只重连数据通道(initTerminal 对已有实例直接复用)
  const term = initTerminal(tab)
  if (!term) tab.logBuffer = '' // 实例不存在(标签页未挂载)时才清空回放缓冲
  tab.status = 'connecting'
  term?.write('\r\n\x1b[90m── ' + t('tabPane.reconnecting') + ' ──\x1b[0m\r\n')

  if (tab.protocol === 'serial') {
    await nextTick()
    try {
      await SerialConnect(tab.id, tab.host, tab.port, tab.dataBits ?? 8, tab.stopBits ?? '1', tab.parity ?? 'none')
      tab.status = 'connected'
    } catch (e: any) {
      tab.status = 'error'
      ;(tab.terminal as any)?.write(`\r\n\x1b[31m${t('tabPane.connectFailed', { err: e.message || e })}\x1b[0m\r\n`)
    }
    return
  }

  let meta: any
  try { meta = JSON.parse(await LoadSession(tab.sessionPath)) } catch { return }

  await nextTick()

  if (tab.protocol === 'ssh') {
    try {
      const creds = await sshAuthFlow(tab, tab.sessionPath, meta?.authMode === 'key')
      if (!creds) { tab.status = 'idle'; return }
      if (creds.temporary) {
        await ConnectToSessionWithCreds(tab.sessionPath, tab.id, creds.username, creds.password)
      } else {
        await ConnectToSession(tab.sessionPath, tab.id)
      }
      tab.status = 'connected'
      if (term) SSHResize(tab.id, term.cols, term.rows).catch(() => {})
    } catch (e: any) {
      tab.status = 'error'
      ;(tab.terminal as any)?.write(`\r\n\x1b[31m${e.message || e}\x1b[0m\r\n`)
    }
    return
  }

  try {
    await ConnectToSession(tab.sessionPath, tab.id)
    tab.status = 'connected'
  } catch (e: any) {
    tab.status = 'error'
    ;(tab.terminal as any)?.write(`\r\n\x1b[31m${t('tabPane.connectFailed', { err: e.message || e })}\x1b[0m\r\n`)
  }
}

const tabMenuOptions = computed<DropdownOption[]>(() => [
  { label: t('tabPane.reconnectDisconnected'), key: 'reconnectDisconnected' },
  { label: t('tabPane.reconnectAll'), key: 'reconnectAll' },
  { type: 'divider', key: 'd1' },
  { label: t('tabPane.closeDisconnected'), key: 'closeDisconnected' },
  { label: t('tabPane.closeAll'), key: 'closeAll' },
])

function handleTabMenu(key: string) {
  switch (key) { case 'reconnectDisconnected': reconnectAll(); break; case 'reconnectAll': pane.tabs.forEach(t => reconnectTab(t)); break; case 'closeDisconnected': closeDisconnected(); break; case 'closeAll': closeAll(); break }
}

const cursorRow = ref(0)
const cursorCol = ref(0)
let cursorTimer: ReturnType<typeof setInterval> | null = null

const activeTab = computed(() => pane.tabs.find(t => t.id === pane.activeTabId))

function clearScrollback(tab?: Tab) {
  const t = tab ?? activeTab.value
  t?.terminal?.clear()
}
function clearScreen(tab?: Tab) {
  const t = tab ?? activeTab.value
  t?.terminal?.clear()
  if (t && t.status === 'connected') sendToTab(t.id, t.protocol, '\x0C')
}

let exportCooldown = false
async function exportLog(tab?: Tab) {
  const t = tab ?? activeTab.value
  const buf = t?.logBuffer
  if (!t || !buf || exportCooldown) return
  exportCooldown = true
  setTimeout(() => { exportCooldown = false }, 2000)
  const filename = `${t.title}_${new Date().toISOString().slice(0, 19).replace(/:/g, '-')}.log`
  try { await SaveLogFile(buf, filename) } catch {}
}

function trackCursor() {
  const t = pane.tabs.find(t => t.id === pane.activeTabId)
  const term = t?.terminal ?? null
  if (term) { cursorRow.value = term.buffer.active.cursorY + 1; cursorCol.value = term.buffer.active.cursorX + 1 }
}

function onTabDragStart(e: DragEvent, idx: number, tab: Tab) {
  dragTabIdx = idx
  dragState.tabId = tab.id
  dragState.srcPaneId = pane.id
  e.dataTransfer!.effectAllowed = 'move'
  e.dataTransfer!.setData('text/plain', tab.id)
}

function onTabDragEnd() {
  dragTabIdx = null
  insertIdx.value = null
  dragState.tabId = ''
  dragState.srcPaneId = ''
}

// 标签条插入位置:鼠标在目标标签前半 → 插到该标签前,后半 → 插到其后;间隙处始终有指示
const insertIdx = ref<number | null>(null)

function onTabsContainerDragOver(e: DragEvent) {
  const el = e.currentTarget as HTMLElement
  const tabId = dragState.tabId || e.dataTransfer?.getData('text/plain') || ''
  if (!tabId) return
  e.preventDefault()
  const items = el.querySelectorAll('.tab-item, .v-tab-item') as NodeListOf<HTMLElement>
  const pos = props.isVertical ? e.clientY : e.clientX
  let idx = items.length
  for (let i = 0; i < items.length; i++) {
    const r = items[i].getBoundingClientRect()
    const mid = props.isVertical ? r.top + r.height / 2 : r.left + r.width / 2
    if (pos < mid) { idx = i; break }
  }
  insertIdx.value = idx
}

function onTabsContainerDrop(e: DragEvent) {
  e.preventDefault()
  const idx = insertIdx.value
  insertIdx.value = null
  const tabId = dragState.tabId || e.dataTransfer?.getData('text/plain') || ''
  if (idx == null || !tabId) return
  if (dragTabIdx != null) {
    // 本地拖动:插入到目标位置(移除源后索引修正);位置未变则无事发生
    if (dragTabIdx === idx || dragTabIdx + 1 === idx) { dragTabIdx = null; return }
    const [item] = pane.tabs.splice(dragTabIdx, 1)
    const target = dragTabIdx < idx ? idx - 1 : idx
    pane.tabs.splice(target, 0, item)
    dragTabIdx = null
    return
  }
  // 跨 pane 拖入:插入到指定位置
  actions.onMoveTab(tabId, pane.id, idx)
}

// 拖离标签条容器(含空白区)时清除插入指示;仅当真正离开容器时才清
function onTabsDragLeave(e: DragEvent) {
  const el = e.currentTarget as HTMLElement
  const related = e.relatedTarget as Node | null
  if (related && el.contains(related)) return
  insertIdx.value = null
}

// 终端区拖拽分屏:按鼠标位置分为 左上(合并)/右上(向右拆分)/下半区(向下拆分)
const dropZone = ref<'merge' | 'split-h' | 'split-v' | null>(null)

function onTermAreaDragOver(e: DragEvent) {
  const el = e.currentTarget as HTMLElement
  if (!el) return
  const tabId = dragState.tabId || e.dataTransfer?.getData('text/plain') || ''
  if (!tabId) return
  // 主 pane 最后一个标签页不可拆分(仍可接受其他 pane 标签合并进来)
  if (pane.isMain && pane.tabs.length === 1 && pane.tabs[0].id === tabId) return
  e.preventDefault()
  const rect = el.getBoundingClientRect()
  const x = e.clientX - rect.left
  const y = e.clientY - rect.top
  if (y < rect.height / 2) {
    dropZone.value = x < rect.width / 2 ? 'merge' : 'split-h'
  } else {
    dropZone.value = 'split-v'
  }
}

function onTermAreaDragLeave(e: DragEvent) {
  // 仅当真正离开 term-area 时清除提示;移入其子元素(dragleave 目标切换)不算离开
  const el = e.currentTarget as HTMLElement
  const related = e.relatedTarget as Node | null
  if (related && el.contains(related)) return
  dropZone.value = null
}

function onTermAreaDrop(e: DragEvent) {
  e.preventDefault()
  const zone = dropZone.value
  dropZone.value = null
  const tabId = dragState.tabId || e.dataTransfer?.getData('text/plain') || ''
  if (!zone || !tabId) return
  if (zone === 'merge') actions.onMoveTab(tabId, pane.id)
  else if (zone === 'split-h') actions.onSplitAt(tabId, pane.id, 'h')
  else actions.onSplitAt(tabId, pane.id, 'v')
}

// scroll arrows
function updateScrollState() {
  const el = tabsContainerRef.value
  if (!el) return
  canScrollLeft.value = el.scrollLeft > 2
  canScrollRight.value = el.scrollLeft < el.scrollWidth - el.clientWidth - 2
}

function scrollTabs(dir: number) {
  const el = tabsContainerRef.value
  if (!el) return
  el.scrollBy({ left: dir * 120, behavior: 'smooth' })
}

function onContainerWheel(e: WheelEvent, el: HTMLElement | null) {
  if (!el || el.scrollWidth <= el.clientWidth) return
  e.preventDefault()
  el.scrollBy({ left: e.deltaY, behavior: 'auto' })
}

function onTabsWheel(e: WheelEvent) { onContainerWheel(e, tabsContainerRef.value) }

function onTabsScroll() { updateScrollState() }

// vertical resize
function startVerticalResize(e: MouseEvent) {
  isResizingVertical = true
  e.preventDefault()
  document.addEventListener('mousemove', onVerticalResize)
  document.addEventListener('mouseup', stopVerticalResize)
}

function onVerticalResize(e: MouseEvent) {
  if (!isResizingVertical) return
  const wrapper = document.querySelector('.tab-manager') as HTMLElement
  if (!wrapper) return
  const rect = wrapper.getBoundingClientRect()
  verticalWidth.value = Math.max(100, Math.min(400, e.clientX - rect.left))
}

function stopVerticalResize() {
  isResizingVertical = false
  document.removeEventListener('mousemove', onVerticalResize)
  document.removeEventListener('mouseup', stopVerticalResize)
  const active = pane.tabs.find(t => t.id === pane.activeTabId)
  if (active) scheduleFit(active)
}

// ensure active tab is visible
function ensureActiveVisible() {
  nextTick(() => {
    const el = tabsContainerRef.value
    if (!el) return
    const activeEl = el.querySelector('.tab-item.active') as HTMLElement
    if (!activeEl) return
    if (props.isVertical) {
      const top = activeEl.offsetTop - el.offsetTop
      if (top < el.scrollTop) el.scrollTop = top
      else if (top + activeEl.offsetHeight > el.scrollTop + el.clientHeight) el.scrollTop = top + activeEl.offsetHeight - el.clientHeight
    } else {
      const left = activeEl.offsetLeft
      if (left < el.scrollLeft) el.scrollLeft = left
      else if (left + activeEl.offsetWidth > el.scrollLeft + el.clientWidth) el.scrollLeft = left + activeEl.offsetWidth - el.clientWidth
    }
  })
}

watch(() => pane.activeTabId, (id) => {
  ensureActiveVisible()
  actions.onFocus(pane.id)
  nextTick(() => {
    if (!id) return
    const t = pane.tabs.find(x => x.id === id)
    if (!t) return
    // initTerminal 幂等:已有实例则挂载迁移的 DOM,无实例则创建
    if (t.kind === 'terminal') {
      initTerminal(t)
    }
    // 新打开/切换标签页均聚焦活动终端;rebuild 标记的实例由 switchTab/onTermAreaMousedown 重建后再聚焦
    if (!t.terminalRebuild) {
      t.terminal?.focus()
    }
    scheduleFit(t)
  })
}, { immediate: true })

onMounted(() => {
  paneTabRegistry.set(pane.id, { tabs: pane.tabs, instanceId: paneInstanceId })
  window.addEventListener('resize', handleResize)
  cursorTimer = setInterval(trackCursor, 200)
  loadGlobalCloseConfirm()
  window.addEventListener('config-changed', loadGlobalCloseConfirm)

  const sheet = termSheetRef.value
  if (sheet) {
    resizeObserver = new ResizeObserver(() => { handleResize() })
    resizeObserver.observe(sheet)
  }

  nextTick(() => {
    const el = tabsContainerRef.value
    if (el) {
      updateScrollState()
    }
  })

  unregisterPane = actions.registerPane(pane.id, {
    openSession,
    openSerial,
    openSftp,
    openScriptDialog,
    exportLog,
    clearScrollback,
    clearScreen,
    getActiveSessionPath,
    openComponentTab,
    updateComponentTab,
    closeTabById,
    activateTab: switchTab,
    reportCursor,
    copySelection,
    pasteClipboard,
    mcpTerminalSend,
    mcpCloseTab,
  })

  // 挂载后强制聚焦活动终端:拆分/合并导致组件重建时,键盘焦点可能落在 body 等非终端元素,
  // 不聚焦则所有终端都无法接收输入
  nextTick(() => {
    const t = pane.tabs.find(x => x.id === pane.activeTabId)
    t?.terminal?.focus()
  })
})

onBeforeUnmount(() => {
  disposePasteEditor()
  disposeScriptEditor()
  window.removeEventListener('resize', handleResize)
  window.removeEventListener('config-changed', loadGlobalCloseConfirm)
  resizeObserver?.disconnect()
  if (cursorTimer) clearInterval(cursorTimer)
  if (fitDebounce) clearTimeout(fitDebounce)
  unregisterPane?.()
  // 仅删除本实例注册的条目:组件重建时新实例已注册(先挂后卸),旧实例不得误删
  const entry = paneTabRegistry.get(pane.id)
  if (entry && entry.instanceId === paneInstanceId) {
    paneTabRegistry.delete(pane.id)
  }
  // 布局拆分/合并/切垂直时组件销毁重建,pane 仍存在:标记全部终端重建,
  // 由重建组件激活时重建实例(logBuffer 回放内容);仅当 pane 已被销毁才真正释放终端
  if (!actions.paneExists(pane.id)) {
    pane.tabs.forEach(t => {
      disposeTerminal(t)
    })
  } else {
    pane.tabs.forEach(t => {
      t.terminalRebuild = true
    })
  }
})

function getActiveSessionPath(): string | null {
  const tab = pane.tabs.find(t => t.id === pane.activeTabId)
  return tab?.sessionPath || null
}

// 文件类型标签:取文件扩展名小写;无扩展名返回空
function getFileTypeLabel(name: string): string {
  const dot = name.lastIndexOf('.')
  if (dot <= 0 || dot === name.length - 1) return ''
  return name.substring(dot + 1).toLowerCase()
}

// 状态栏左侧文本:未连接/远程连接(协议:ip:端口)/文件编辑(文件类型)/空
function getStatusLeft(): string {
  const tab = pane.tabs.find(t => t.id === pane.activeTabId)
  if (!tab) return t('tabPane.notConnected')
  if (tab.kind === 'terminal') {
    if (tab.protocol === 'ssh') {
      const u = tab.username
      return u ? `SSH:${u}@${tab.host || ''}:${tab.port || ''}` : `SSH:${tab.host || ''}:${tab.port || ''}`
    }
    if (tab.protocol === 'telnet') return `Telnet:${tab.host || ''}:${tab.port || ''}`
    if (tab.protocol === 'serial') return t('tabPane.serialStatus', { host: tab.host || '', port: tab.port || '' })
    if (tab.protocol === 'shell') {
      const ref = tab.host || ''
      const short = ref.includes('/') || ref.includes('\\') ? ref.split(/[\\/]/).pop()?.replace(/\.exe$/i, '') ?? ref : ref.replace(/^wsl:\/\//, '')
      return t('tabPane.localStatus', { shell: short })
    }
    return t('tabPane.notConnected')
  }
  if (tab.kind === 'component') {
    const fp = (tab.componentProps as any)?.fileName
    return fp ? getFileTypeLabel(fp) : ''
  }
  // VNC 图形会话:已连接显示地址,未连接显示未连接
  if (tab.kind === 'vnc') {
    if (tab.status === 'connected') return `VNC:${tab.host || ''}:${tab.port || ''}`
    return t('tabPane.notConnected')
  }
  return ''
}

// 状态栏编码:文本类标签页 utf-8;无编码为空(不显示)
function getStatusEncoding(): string {
  const tab = pane.tabs.find(t => t.id === pane.activeTabId)
  if (!tab) return ''
  if (tab.kind === 'terminal') return 'utf-8'
  if (tab.kind === 'component') {
    return (tab.componentProps as any)?.fileName ? 'utf-8' : ''
  }
  return ''
}

// 文件编辑器光标位置上报(仅组件标签页)
function reportCursor(row: number, col: number) {
  cursorRow.value = row
  cursorCol.value = col
}

watch([() => pane.activeTabId, cursorRow, cursorCol], () => {
  actions.onStatus(pane.id, getStatusLeft(), cursorRow.value, cursorCol.value, getStatusEncoding(), !!pane.activeTabId)
})

// 活动标签页状态快照上报:供顶级菜单按活动标签页启用/禁用工具栏工具
const activeTabState = computed<ActiveTabState>(() => {
  const t = activeTab.value
  if (!t) return { hasTab: false, isTerminal: false, protocol: '', connected: false }
  return { hasTab: true, isTerminal: t.kind === 'terminal', protocol: t.protocol, connected: t.status === 'connected' }
})
watch(activeTabState, (s) => { actions.onActiveTabState(pane.id, s) }, { immediate: true })

// 打开 SFTP 面板标签页：复用既有 SSH 连接。disconnectSshOnClose 为 true 时关闭标签页同时断开其 SSH 连接
function openSftpPanel(connID: string, title: string, disconnectSshOnClose = false) {
  openComponentTab({
    title: 'SFTP - ' + title,
    component: SftpPanel,
    props: { sessionID: connID, tabId: 'sftp-panel-' + Date.now() },
    icon: FolderOpenOutline,
    color: '#4ec9b0',
    status: 'connected',
    onClose: (): boolean => {
      SftpDisconnect(connID).catch(() => {})
      if (disconnectSshOnClose) SSHDisconnect(connID).catch(() => {})
      return true
    },
  })
}

function openSftp(tab?: Tab) {
  const t = tab ?? pane.tabs.find(x => x.id === pane.activeTabId)
  if (t && t.protocol === 'ssh' && t.status === 'connected') {
    openSftpPanel(t.id, t.title)
  }
}

// 执行脚本对话框
function openScriptDialog(tab?: Tab) {
  scriptContent.value = ''
  scriptTargetTab.value = tab ?? pane.tabs.find(x => x.id === pane.activeTabId) ?? null
  showScriptDialog.value = true
  nextTick(() => {
    if (!scriptEditorEl.value) return
    scriptEditorView?.destroy()
    scriptEditorView = null
    scriptEditorView = createShellEditor(scriptEditorEl.value, '', doc => { scriptContent.value = doc })
    scriptEditorView.focus()
  })
}

function disposeScriptEditor() {
  scriptEditorView?.destroy()
  scriptEditorView = null
}

function readScriptFile() { scriptFileInputRef.value?.click() }
function onScriptFileChange(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  const reader = new FileReader()
  reader.onload = () => {
    const text = reader.result as string
    scriptEditorView?.dispatch({ changes: { from: scriptEditorView.state.doc.length, insert: text } })
  }
  reader.readAsText(file, 'UTF-8')
  input.value = ''
}

async function sendScript() {
  if (!scriptContent.value.trim()) return
  const tab = scriptTargetTab.value ?? activeTab.value
  if (tab && tab.status === 'connected') {
    const lines = scriptContent.value.split('\n')
    for (const line of lines) {
      if (line.trim()) {
        await sendToTab(tab.id, tab.protocol, line.trim() + '\n')
      }
    }
  }
  showScriptDialog.value = false
  disposeScriptEditor()
}

function cancelScript() {
  showScriptDialog.value = false
  disposeScriptEditor()
  scriptContent.value = ''
  scriptTargetTab.value = null
}

// 活动终端解析:当前活动标签页的终端
function getActiveTerminal(): { tab: Tab; terminal: Terminal | null } | null {
  const tab = pane.tabs.find(x => x.id === pane.activeTabId)
  if (!tab) return null
  return { tab, terminal: tab.terminal }
}

// 工具栏/菜单"复制":复制当前活动终端选中的文本;无选区时提示。
async function copySelection() {
  const active = getActiveTerminal()
  const sel = active?.terminal?.getSelection()
  if (!sel) {
    message.info(t('tabPane.noSelection'))
    return
  }
  try {
    const err = await ClipboardCopy(sel)
    if (err) { message.error(err); return }
    message.success(t('tabPane.copied'))
  } catch (e: any) {
    message.error(t('tabPane.copyFailed', { err: e.message || e }))
  }
}

// 工具栏/菜单"粘贴":读取系统剪贴板并送入当前活动终端;多行文本走确认弹窗。
async function pasteClipboard() {
  const active = getActiveTerminal()
  if (!active?.tab) return
  let text = ''
  try {
    text = await ClipboardPaste()
  } catch (e: any) {
    message.error(t('tabPane.readClipboardFailed', { err: e.message || e }))
    return
  }
  if (!text) {
    message.info(t('tabPane.clipboardEmpty'))
    return
  }
  const targetId = active.tab.id
  const lines = text.split(/\r?\n/)
  if (lines.length > 1) {
    pasteTargetId.value = targetId
    openPasteEditor(text)
  } else if (isConnectedFor(targetId)) {
    sendToTab(targetId, active.tab.protocol, text)
    message.success(t('tabPane.pastedToTerminal'))
  } else {
    message.warning(t('tabPane.tabNotConnected'))
  }
}

defineExpose({
  openSession,
  openSerial,
  openSftp,
  openScriptDialog,
  exportLog,
  clearScrollback,
  clearScreen,
  getActiveSessionPath,
  openComponentTab,
  updateComponentTab,
  closeTabById,
  activateTab: switchTab,
  reportCursor,
  copySelection,
  pasteClipboard,
  mcpTerminalSend,
  mcpCloseTab,
})

function emitWelcome(e: 'new-ssh' | 'new-telnet' | 'new-serial') {
  if (e === 'new-ssh') emit('new-ssh')
  else if (e === 'new-telnet') emit('new-telnet')
  else emit('new-serial')
  actions.onFocus(pane.id)
}

const dropZoneLabel = computed(() => {
  // 被拖标签来自本 pane(自己):左上合并无意义,提示必须准确反映"无任何操作"
  const isSelf = dragState.srcPaneId === pane.id && dragState.tabId !== ''
  switch (dropZone.value) {
    case 'merge': return isSelf ? t('tabPane.mergeNoOp') : t('tabPane.mergeHere')
    case 'split-h': return t('tabPane.splitRight')
    case 'split-v': return t('tabPane.splitDown')
    default: return ''
  }
})
</script>

<template>
  <div class="tab-pane" :class="{ 'vertical-tabs': isVertical }">
    <div v-if="isVertical" class="v-tabs-panel" :style="{ width: verticalWidth + 'px' }">
      <div class="v-tabs-header">
        <n-dropdown trigger="click" :options="tabMenuOptions" @select="handleTabMenu" placement="bottom-start">
          <n-icon :size="14" :component="ChevronDownOutline" class="v-tabs-menu-icon" />
        </n-dropdown>
      </div>
      <div ref="tabsContainerRef" class="v-tabs-list" @wheel="onTabsWheel" @scroll="onTabsScroll" @dragover="onTabsContainerDragOver" @dragleave="onTabsDragLeave" @drop="onTabsContainerDrop">
        <template v-for="(tab, idx) in pane.tabs" :key="tab.id">
          <div class="tab-insert-mark v" :class="{ active: insertIdx === idx }">
            <span class="tab-insert-label">{{ t('tabPane.moveHere') }}</span>
          </div>
          <div class="v-tab-item" :class="{ active: pane.activeTabId === tab.id }" @click="switchTab(tab.id)" draggable="true"
            @dragstart="(e: DragEvent) => onTabDragStart(e, idx, tab)" @dragend="onTabDragEnd"
            @contextmenu="(e: MouseEvent) => openTabContextMenu(e, tab)">
            <span v-if="tab.kind === 'terminal'" class="tab-status" :style="{ background: gStatus(tab.status) }"></span>
            <span v-if="tab.kind === 'component'" class="tab-file-state" :class="{ dirty: tab.dirty }"></span>
            <n-icon :size="13" :component="tab.icon ?? gIcon(tab.protocol)" :style="{ color: tab.color ?? gColor(tab.protocol) }" />
            <span class="tab-title">{{ tab.title }}</span>
            <n-icon :size="12" :component="CloseOutline" class="tab-close" @click.stop="handleCloseTab(tab)" />
          </div>
        </template>
        <div class="tab-insert-mark v" :class="{ active: insertIdx === pane.tabs.length }">
          <span class="tab-insert-label">{{ t('tabPane.moveHere') }}</span>
        </div>
      </div>
      <div class="v-tabs-resize-handle" @mousedown="startVerticalResize"></div>
    </div>

    <div class="main-area">
      <div ref="termSheetRef" class="term-sheet">
        <div v-if="showWelcomePaneId === pane.id" class="welcome-overlay">
          <div class="welcome-content">
            <div class="welcome-logo"><n-icon :size="36" :component="TerminalOutline" /></div>
            <h2>AceShell</h2>
            <p class="welcome-desc">{{ t('tabPane.welcomeDesc') }}</p>
            <div class="welcome-shortcuts">
              <div class="shortcut-item" @click="emitWelcome('new-ssh')"><n-icon :size="16" :component="ServerOutline" class="shortcut-icon" /><span class="shortcut-key">SSH</span><span>{{ t('tabPane.newSshConn') }}</span></div>
              <div class="shortcut-item" @click="emitWelcome('new-telnet')"><n-icon :size="16" :component="GlobeOutline" class="shortcut-icon" /><span class="shortcut-key">Telnet</span><span>{{ t('tabPane.newTelnetConn') }}</span></div>
              <div class="shortcut-item" @click="emitWelcome('new-serial')"><n-icon :size="16" :component="HardwareChipOutline" class="shortcut-icon" /><span class="shortcut-key">{{ t('tabPane.serial') }}</span><span>{{ t('tabPane.newSerialConn') }}</span></div>
            </div>
          </div>
        </div>
        <div v-if="!isVertical && pane.tabs.length > 0" class="h-tabs-bar">
          <div class="scroll-arrow" :class="{ disabled: !canScrollLeft }" @click="scrollTabs(-1)">
            <n-icon :size="14" :component="ChevronBackOutline" />
          </div>
          <div ref="tabsContainerRef" class="h-tabs-container" @wheel="onTabsWheel" @scroll="onTabsScroll" @dragover="onTabsContainerDragOver" @dragleave="onTabsDragLeave" @drop="onTabsContainerDrop">
            <template v-for="(tab, idx) in pane.tabs" :key="tab.id">
              <div class="tab-insert-mark" :class="{ active: insertIdx === idx }">
                <span class="tab-insert-label">{{ t('tabPane.moveHere') }}</span>
              </div>
              <div class="tab-item" :class="{ active: pane.activeTabId === tab.id }" @click="switchTab(tab.id)" draggable="true"
                @dragstart="(e: DragEvent) => onTabDragStart(e, idx, tab)" @dragend="onTabDragEnd"
                @contextmenu="(e: MouseEvent) => openTabContextMenu(e, tab)">
                <span v-if="tab.kind === 'terminal'" class="tab-status" :style="{ background: gStatus(tab.status) }"></span>
                <span v-if="tab.kind === 'component'" class="tab-file-state" :class="{ dirty: tab.dirty }"></span>
                <n-icon :size="13" :component="tab.icon ?? gIcon(tab.protocol)" :style="{ color: tab.color ?? gColor(tab.protocol) }" />
                <span class="tab-title">{{ tab.title }}</span>
                <n-icon :size="14" :component="CloseOutline" class="tab-close" @click.stop="handleCloseTab(tab)" />
              </div>
            </template>
            <div class="tab-insert-mark" :class="{ active: insertIdx === pane.tabs.length }">
              <span class="tab-insert-label">{{ t('tabPane.moveHere') }}</span>
            </div>
          </div>
          <div class="scroll-arrow" :class="{ disabled: !canScrollRight }" @click="scrollTabs(1)">
            <n-icon :size="14" :component="ChevronForwardOutline" />
          </div>
          <div class="tab-menu-btn">
            <n-dropdown trigger="click" :options="tabMenuOptions" @select="handleTabMenu" placement="bottom-end">
              <n-icon :size="16" :component="ChevronDownOutline" class="tab-menu-icon" />
            </n-dropdown>
          </div>
        </div>
        <div class="term-wrapper">
          <div v-if="showToolbar && activeTab && activeTab.kind === 'terminal'" class="tab-toolbar">
            <div class="toolbar-btns">
              <div class="toolbar-icon-btn" :title="t('tabPane.copySelection')" @click="copySelection()">
                <n-icon :size="15" :component="CopyOutline" />
              </div>
              <div class="toolbar-icon-btn" :title="t('tabPane.pasteToTerminal')" @click="pasteClipboard()">
                <n-icon :size="15" :component="ClipboardOutline" />
              </div>
              <n-button v-if="activeTab.protocol === 'ssh'" size="tiny" :disabled="activeTab.status !== 'connected'" @click="openSftp(activeTab)" :title="t('tabPane.openSftp')">SFTP</n-button>
              <n-button size="tiny" @click="openScriptDialog(activeTab)" :title="t('tabPane.execScript')">{{ t('tabPane.execScript') }}</n-button>
              <n-button size="tiny" @click="exportLog(activeTab)" :title="t('tabPane.exportLog')">{{ t('tabPane.exportLog') }}</n-button>
              <n-button size="tiny" @click="clearScrollback(activeTab)" :title="t('tabPane.clearScrollback')">{{ t('tabPane.clearScrollback') }}</n-button>
              <n-button size="tiny" @click="clearScreen(activeTab)" :title="t('tabPane.clearScreen')">{{ t('tabPane.clearScreen') }}</n-button>
            </div>
          </div>
          <div class="term-area" @mousedown="onTermAreaMousedown" @dragover="onTermAreaDragOver" @dragleave="onTermAreaDragLeave" @drop="onTermAreaDrop">
            <div v-if="pane.tabs.length === 0 && showWelcomePaneId !== pane.id" class="term-placeholder"><n-empty :description="t('tabPane.selectSessionHint')" size="small" /></div>
            <template v-for="tab in pane.tabs" :key="tab.id">
              <div v-if="tab.kind === 'component' || tab.kind === 'vnc'" class="term-container" :class="{ visible: pane.activeTabId === tab.id }">
                <div class="tab-content-host">
                  <component :is="tab.component" v-bind="tab.componentProps || {}" :active="pane.activeTabId === tab.id" />
                </div>
              </div>
              <div v-else :id="'term-' + tab.id" class="term-container" :class="{ visible: pane.activeTabId === tab.id }" />
            </template>
            <div v-if="dropZone" class="drop-overlay" :class="dropZone">
              <span class="drop-zone-label">{{ dropZoneLabel }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>

    <n-modal v-model:show="showCloseConfirm" :title="t('tabPane.closeConfirmTitle')" preset="dialog" :show-icon="false" :mask-closable="false" style="width: 400px">
      <div>{{ t('tabPane.closeConfirmMsg') }}</div>
      <n-checkbox v-if="confirmTab?.sessionPath" v-model:checked="confirmNoAsk" style="margin-top: 16px">{{ t('tabPane.dontAskAgain') }}</n-checkbox>
      <div v-else class="close-confirm-hint">{{ t('tabPane.closeConfirmHint') }}</div>
      <template #action>
        <n-button @click="cancelClose">{{ t('common.cancel') }}</n-button>
        <n-button type="error" @click="confirmCloseTab">{{ t('common.close') }}</n-button>
      </template>
    </n-modal>

    <n-modal v-model:show="showPasteConfirm" :title="t('tabPane.pasteConfirmTitle')" preset="dialog" :show-icon="false" :mask-closable="false" class="paste-confirm-modal" style="width: 620px">
      <div class="paste-confirm-body">
        <div class="paste-confirm-editor-wrap">
          <div class="paste-confirm-hint">{{ t('tabPane.pasteHint') }}</div>
          <div ref="pasteEditorEl" class="paste-confirm-editor" />
        </div>
        <div class="paste-confirm-actions">
          <n-button type="primary" @click="confirmPaste">{{ t('tabPane.paste') }}</n-button>
          <n-button @click="cancelPaste">{{ t('common.cancel') }}</n-button>
        </div>
      </div>
    </n-modal>

    <n-modal v-model:show="showScriptDialog" :title="t('tabPane.scriptTitle')" preset="dialog" :show-icon="false" :mask-closable="false" class="script-dialog-modal" style="width: 620px">
      <div class="script-dialog-body">
        <div class="script-dialog-editor-wrap">
          <div class="script-dialog-hint">{{ t('tabPane.scriptHint') }}</div>
          <div ref="scriptEditorEl" class="script-dialog-editor" />
        </div>
        <div class="script-dialog-actions">
          <n-button size="tiny" @click="readScriptFile" :title="t('tabPane.loadScriptFile')">{{ t('tabPane.loadText') }}</n-button>
          <n-button size="tiny" :disabled="!scriptContent.trim()" @click="sendScript">{{ t('tabPane.send') }}</n-button>
          <n-button size="tiny" type="primary" :disabled="!scriptContent.trim()" @click="sendScript">{{ t('tabPane.sendAndClose') }}</n-button>
          <n-button size="tiny" @click="cancelScript">{{ t('common.cancel') }}</n-button>
        </div>
      </div>
    </n-modal>
    <input ref="scriptFileInputRef" type="file" accept=".txt,.sh,.bat,.ps1,.py" style="display:none" @change="onScriptFileChange" />

    <n-modal v-model:show="showBrowserFail" :title="t('tabPane.browserFailTitle')" preset="dialog" :show-icon="false" style="width: 520px" :mask-closable="false">
      <div style="line-height: 1.7; color: var(--text-color, #d4d4d4)">
<div>{{ t('tabPane.browserFailMsg') }}</div>
        <div style="word-break: break-all; margin: 6px 0">{{ browserFailUrl }}</div>
<div>{{ t('tabPane.browserFailHint') }}</div>
      </div>
      <div style="margin-top: 12px">
        <n-select v-model:value="browserFailSel" :options="browserFailOptions" :placeholder="t('tabPane.selectBrowser')" clearable @focus="loadBrowserFailOptions" />
      </div>
      <template #action>
        <n-button @click="closeBrowserFail">{{ t('common.cancel') }}</n-button>
        <n-button type="primary" :disabled="!browserFailSel" @click="retryOpenUrlWithBrowser">{{ t('tabPane.retryOpen') }}</n-button>
      </template>
    </n-modal>

    <n-dropdown :show="tabCtxShow" :options="tabCtxOptions" :x="tabCtxX" :y="tabCtxY" placement="bottom-start" @select="handleTabCtxSelect" @clickoutside="tabCtxShow = false" />

    <FingerprintDialog
      v-model:show="showFingerprint"
      :host="fingerprintHost"
      :fingerprint="fingerprintKey"
      :status="fingerprintStatus"
      @confirm="onFingerprintConfirm"
      @skip="onFingerprintSkip"
      @cancel="onFingerprintCancel"
    />

    <CredentialsDialog
      v-model:show="showCredentials"
      :host="credHost"
      :username="credUsername"
      :has-password="credHasPassword"
      :title="credTitle"
      @submit="onCredentialsSubmit"
      @cancel="onCredentialsCancel"
    />

    <KeyCredentialsDialog
      v-model:show="showKeyCredentials"
      :host="keyCredHost"
      :username="keyCredUsername"
      @submit="onKeyCredentialsSubmit"
      @cancel="onKeyCredentialsCancel"
    />
  </div>
</template>

<style scoped>
.tab-pane { flex: 1; min-width: 0; min-height: 0; display: flex; flex-direction: column; overflow: hidden; position: relative; }
.tab-pane.vertical-tabs { flex-direction: row; }

/* vertical tabs */
.v-tabs-panel { display: flex; flex-direction: column; background: var(--sidebar-bg); border-right: 1px solid var(--border-color); position: relative; flex-shrink: 0; }
.v-tabs-header { display: flex; align-items: center; justify-content: flex-end; padding: 4px 6px; border-bottom: 1px solid var(--border-color); }
.v-tabs-menu-icon { color: var(--icon-color); cursor: pointer; transition: color 0.15s; }
.v-tabs-menu-icon:hover { color: var(--icon-hover); }
.v-tabs-list { flex: 1; overflow-y: auto; overflow-x: hidden; }
.v-tabs-list::-webkit-scrollbar { width: 4px; }
.v-tabs-list::-webkit-scrollbar-thumb { background: var(--border-color); border-radius: 2px; }
.v-tabs-list::-webkit-scrollbar-thumb:hover { background: var(--border-color); }
.v-tab-item { display: flex; align-items: center; gap: 6px; padding: 6px 3px; font-size: 12px; color: var(--icon-color); cursor: pointer; white-space: nowrap; transition: background 0.1s, color 0.1s; position: relative; background: var(--tab-inactive-bg); }
.v-tab-item:hover { background: var(--tab-inactive-bg); filter: brightness(1.12); color: var(--icon-hover); }
.v-tab-item.active { background: var(--tab-active-bg); color: var(--text-color); border: 1px solid var(--border-color); }
:global(html.dark) .v-tab-item.active { background: #161616; border: none; border-radius: 2px; }
.v-tab-item .tab-title { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; }
.v-tab-item .tab-close { opacity: 0; flex-shrink: 0; border-radius: 3px; width: 16px; height: 16px; padding: 0; display: flex; align-items: center; justify-content: center; transition: opacity 0.1s, background 0.1s; }
.v-tab-item:hover .tab-close { opacity: 0.6; }
.v-tab-item .tab-close:hover { opacity: 1 !important; background: var(--close-hover-bg); }
.v-tabs-resize-handle { position: absolute; top: 0; right: -2px; bottom: 0; width: 4px; cursor: col-resize; z-index: 5; }
.v-tabs-resize-handle:hover, .v-tabs-resize-handle:active { background: #0078d4; }

/* main area */
.main-area { flex: 1; min-width: 0; display: flex; flex-direction: column; overflow: hidden; }
.tab-toolbar { height: 28px; min-height: 28px; display: flex; align-items: center; padding: 0 8px; background: var(--sidebar-bg); }
.toolbar-btns { display: flex; align-items: center; gap: 4px; }
.toolbar-icon-btn { width: 22px; height: 22px; display: flex; align-items: center; justify-content: center; border-radius: 4px; color: var(--icon-color); cursor: pointer; transition: background 0.15s, color 0.15s; }
.toolbar-icon-btn:hover { background: var(--hover-bg); color: var(--icon-hover); }

/* 多行粘贴确认弹窗:左侧编辑区 + 右侧按钮列,紧凑内边距,整体尺寸固定 */
.paste-confirm-body { display: flex; gap: 10px; align-items: stretch; }
.paste-confirm-actions { display: flex; flex-direction: column; gap: 8px; padding-top: 2px; }
.paste-confirm-editor-wrap { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 4px; }
.paste-confirm-hint { font-size: 11px; color: var(--text-color, #d4d4d4); opacity: 0.65; }
.paste-confirm-editor { height: 260px; overflow: hidden; border: 1px solid #3c3c3c; border-radius: 4px; }
:deep(.paste-confirm-modal .n-modal-header) { font-size: 13px; }
:deep(.paste-confirm-editor .cm-editor) { height: 100%; }
:deep(.paste-confirm-editor .cm-scroller) { overflow: auto; }

/* horizontal tabs */
.h-tabs-bar { height: 34px; min-height: 34px; display: flex; align-items: stretch; background: var(--sidebar-bg); }
.scroll-arrow { display: flex; align-items: center; justify-content: center; width: 22px; flex-shrink: 0; cursor: pointer; color: var(--icon-color); transition: color 0.15s, background 0.15s; }
.scroll-arrow:hover { color: var(--icon-hover); background: var(--hover-bg); }
.scroll-arrow.disabled { color: var(--border-color); cursor: default; }
.scroll-arrow.disabled:hover { background: transparent; }
.h-tabs-container { flex: 1; min-width: 0; display: flex; align-items: stretch; overflow-x: auto; overflow-y: hidden; scrollbar-width: thin; scrollbar-color: var(--icon-color) transparent; }
.h-tabs-container::-webkit-scrollbar { height: 6px; }
.h-tabs-container::-webkit-scrollbar-thumb { background: var(--icon-color); border-radius: 3px; }
.h-tabs-container::-webkit-scrollbar-thumb:hover { background: var(--icon-color); }
.h-tabs-container::-webkit-scrollbar-track { background: transparent; }
.tab-item { display: flex; align-items: center; gap: 5px; padding: 0 3px; font-size: 12px; color: var(--icon-color); cursor: pointer; white-space: nowrap; transition: background 0.1s, color 0.1s; flex-shrink: 0; position: relative; background: var(--tab-inactive-bg); }
.tab-item:hover { background: var(--tab-inactive-bg); filter: brightness(1.12); color: var(--icon-hover); }
.tab-item.active { background: var(--tab-active-bg); color: var(--text-color); border: 1px solid var(--border-color); border-bottom: none; }
:global(html.dark) .tab-item.active { background: #161616; border: none; border-radius: 2px 2px 0 0; }
.tab-title { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; }
.tab-status { width: 7px; height: 7px; border-radius: 50%; flex-shrink: 0; }
.tab-file-state { width: 10px; height: 10px; border-radius: 50%; flex-shrink: 0; background: #4ec9b0; }
.tab-file-state.dirty { background: #f2c97d; }
.tab-file-state::after { content: '✓'; display: flex; align-items: center; justify-content: center; width: 100%; height: 100%; font-size: 8px; font-weight: 700; color: #0d0d0d; }
.tab-file-state.dirty::after { content: ''; }
.tab-close { opacity: 0; margin-left: 2px; border-radius: 3px; width: 16px; height: 16px; padding: 0; display: flex; align-items: center; justify-content: center; transition: opacity 0.1s, background 0.1s; }
.tab-item:hover .tab-close { opacity: 0.6; }
.tab-close:hover { opacity: 1 !important; background: var(--close-hover-bg); }
.tab-menu-btn { display: flex; align-items: center; padding: 0 8px; flex-shrink: 0; cursor: pointer; }
.close-confirm-hint { margin-top: 16px; font-size: 11px; color: var(--icon-color); }
.tab-menu-icon { color: var(--icon-color); transition: color 0.15s; }
.tab-menu-btn:hover .tab-menu-icon { color: var(--icon-hover); }

/* welcome */
.welcome-overlay { position: absolute; inset: 0; z-index: 10; display: flex; align-items: center; justify-content: center; background: var(--term-bg, #0a0a0a); user-select: none; }
.welcome-logo { width: 48px; height: 48px; display: flex; align-items: center; justify-content: center; border-radius: 10px; background: rgba(0, 120, 212, 0.12); color: #0078d4; margin: 0 auto 12px auto; }
.welcome-content h2 { font-size: 22px; font-weight: 300; margin: 0 0 4px 0; text-align: center; }
.welcome-desc { font-size: 13px; color: var(--text-color, #888); opacity: 0.7; margin: 0 0 32px 0; text-align: center; }
.welcome-shortcuts { display: flex; flex-direction: column; gap: 6px; min-width: 280px; }
.shortcut-item { display: flex; align-items: center; gap: 10px; padding: 8px 14px; border-radius: 4px; cursor: pointer; transition: background 0.15s; font-size: 13px; }
.shortcut-item:hover { background: var(--hover-bg, rgba(255,255,255,0.06)); }
.shortcut-icon { color: #0078d4; flex-shrink: 0; }
.shortcut-key { display: inline-block; background: var(--hover-bg, #333); padding: 2px 8px; border-radius: 3px; font-size: 11px; font-family: Consolas, monospace; color: #0078d4; min-width: 50px; text-align: center; }

/* terminal */
.term-sheet { flex: 1; min-height: 40px; display: flex; flex-direction: column; position: relative; overflow: hidden; }

.term-wrapper { flex: 1; min-height: 0; display: flex; flex-direction: column; }
.term-area { flex: 1; min-height: 0; position: relative; background: var(--term-bg, #000); }
.term-placeholder { width: 100%; height: 100%; display: flex; align-items: center; justify-content: center; }
.term-container { position: absolute; top: 3px; left: 0; right: 0; bottom: 3px; visibility: hidden; opacity: 0; }
.term-container.visible { visibility: visible; opacity: 1; }
.tab-content-host { width: 100%; height: 100%; overflow: auto; }

/* drop overlay (split) */
.drop-overlay { position: absolute; z-index: 9; display: flex; align-items: center; justify-content: center; pointer-events: none; box-sizing: border-box; animation: drop-zone-pulse 1s ease-in-out infinite; }
.drop-overlay.merge { top: 0; left: 0; width: 50%; height: 50%; background: rgba(0, 140, 255, 0.38); border: 3px solid #4fc3ff; box-shadow: inset 0 0 28px rgba(79, 195, 255, 0.55); }
.drop-overlay.split-h { top: 0; right: 0; width: 50%; height: 50%; background: rgba(0, 140, 255, 0.38); border: 3px solid #4fc3ff; box-shadow: inset 0 0 28px rgba(79, 195, 255, 0.55); }
.drop-overlay.split-v { bottom: 0; left: 0; width: 100%; height: 50%; background: rgba(0, 140, 255, 0.38); border: 3px solid #4fc3ff; box-shadow: inset 0 0 28px rgba(79, 195, 255, 0.55); }
@keyframes drop-zone-pulse { 0%, 100% { filter: brightness(1); } 50% { filter: brightness(1.25); } }
.drop-zone-label { background: rgba(0, 60, 120, 0.92); color: #fff; padding: 4px 14px; border-radius: 6px; font-size: 13px; font-weight: 600; border: 1px solid #4fc3ff; pointer-events: none; box-shadow: 0 2px 10px rgba(0, 0, 0, 0.55); white-space: nowrap; }

/* script dialog (与多行粘贴确认同款:CodeMirror 编辑器 + 右侧按钮列,整体高度固定) */
.script-dialog-body { display: flex; gap: 10px; align-items: stretch; }
.script-dialog-actions { display: flex; flex-direction: column; gap: 8px; padding-top: 2px; }
.script-dialog-editor-wrap { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 4px; }
.script-dialog-hint { font-size: 11px; color: var(--text-color, #d4d4d4); opacity: 0.65; }
.script-dialog-editor { height: 260px; overflow: hidden; border: 1px solid #3c3c3c; border-radius: 4px; }
:deep(.script-dialog-modal .n-modal-header) { font-size: 13px; }
:deep(.script-dialog-editor .cm-editor) { height: 100%; }
:deep(.script-dialog-editor .cm-scroller) { overflow: auto; }

:deep(.xterm) { height: 100%; }
:deep(.xterm-viewport) { overflow: hidden !important; }
:deep(.xterm-screen) { width: auto !important; }
:deep(.xterm-rows) { text-align: left; }
:deep(.xterm .xterm-cursor-layer) { z-index: 4; }

/* 标签条插入指示:标签间隙处的明亮插入线 + 文字提示 */
.tab-insert-mark { position: relative; flex-shrink: 0; width: 3px; height: 100%; background: transparent; }
.tab-insert-mark.v { width: 100%; height: 3px; }
.tab-insert-mark.active { background: #4fc3ff; box-shadow: 0 0 10px 2px rgba(79, 195, 255, 0.9); }
.tab-insert-label { display: none; position: absolute; top: calc(100% + 6px); left: 50%; transform: translateX(-50%); background: rgba(0, 80, 160, 0.95); color: #fff; font-size: 12px; font-weight: 600; padding: 3px 10px; border-radius: 5px; white-space: nowrap; border: 1px solid #4fc3ff; box-shadow: 0 2px 8px rgba(0, 0, 0, 0.5); z-index: 20; }
.tab-insert-mark.v .tab-insert-label { top: 50%; left: calc(100% + 8px); transform: translateY(-50%); }
.tab-insert-mark.active .tab-insert-label { display: block; }
</style>
