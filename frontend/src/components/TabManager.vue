<script setup lang="ts">
import { ref, computed, nextTick, onMounted, onBeforeUnmount, watch, provide, markRaw } from 'vue'
import { useMessage } from 'naive-ui'
import { Events } from '@wailsio/runtime'
import { DesktopOutline } from '@vicons/ionicons5'
import { GetConfig } from '../../bindings/changeme/internal/services/configservice.js'
import { GetRdpConnection } from '../../bindings/changeme/internal/services/rdpservice.js'
import { ReleaseRdpConnection } from '../../bindings/changeme/internal/services/rdpservice.js'
import { GetVncConnection } from '../../bindings/changeme/internal/services/vncservice.js'
import { ReleaseVncConnection } from '../../bindings/changeme/internal/services/vncservice.js'
import { applyTermCfg, normalizeTermConfig, resetTermComposition, type TermConfig } from '../composables/useXterm'
import { loadPluginComponent, getPluginCtx, usePlugins } from '../composables/usePluginBridge'
import { PluginNotifyTabEvent } from '../../bindings/changeme/internal/services/pluginservice.js'
import TabPane from './TabPane.vue'
import SplitPane from './SplitPane.vue'
import RemoteDesktopTab from './RemoteDesktopTab.vue'
import VncTab from './VncTab.vue'
import PluginDetailView from './PluginDetailView.vue'
import type { LayoutNode, SplitNode } from './tabTypes'
import type { Pane, PaneCtx, TabPaneApi, SplitDir, Tab, ActiveTabState } from './tabTypes'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const props = defineProps<{ showToolbar: boolean; tabOrientation: string; verticalTabWidth: number }>()

const emit = defineEmits<{
  (e: 'new-ssh'): void
  (e: 'new-telnet'): void
  (e: 'new-serial'): void
  (e: 'status', info: { text: string; row: number; col: number; encoding: string; hasTab: boolean }): void
  (e: 'active-tab-state', state: ActiveTabState): void
}>()

const isVertical = computed(() => props.tabOrientation === 'vertical')

const panes = ref<Pane[]>([])
const layout = ref<LayoutNode>({ type: 'pane', paneId: '' })
const paneApis = new Map<string, TabPaneApi>()
// 必须在 setup 期(首渲染前)创建主 pane: 模板经 findPane(layout.paneId) 取 pane 传给
// TabPane, 若拖到 onMounted 才创建, 首帧会以 pane=undefined 挂载 TabPane —— 其 setup
// 作用域的 watcher/computed 立即解引用崩溃; 生产构建中 Vue 把错误吞进 console.error,
// 调度器被毒化, 后续任意 flush(如打开设置弹窗)再次扫到即整轮中止, 表现为
// "弹窗内容消失、遮罩残留"。
ensureMainPane()

const termCfg = ref<TermConfig | null>(null)

const message = useMessage()

function newPaneId(): string {
  return 'pane-' + Date.now() + '-' + Math.random().toString(36).slice(2, 8)
}

function ensureMainPane(): Pane {
  const main = panes.value.find(p => p.isMain)
  if (main) return main
  const created: Pane = { id: newPaneId(), isMain: true, focused: true, tabs: [], activeTabId: null }
  panes.value.push(created)
  layout.value = { type: 'pane', paneId: created.id }
  return created
}

function findPane(paneId: string): Pane | undefined {
  return panes.value.find(p => p.id === paneId)
}

function findTabPane(tabId: string): Pane | null {
  return panes.value.find(p => p.tabs.some(t => t.id === tabId)) ?? null
}

// 查找已打开的文件编辑标签页(按 componentProps.filePath 匹配),
// 命中则激活其所在 pane 与该标签页,返回标签页 ID;未命中返回 null
function activateFileTab(filePath: string): string | null {
  for (const p of panes.value) {
    const t = p.tabs.find(tab => tab.kind === 'component' && (tab.componentProps as any)?.filePath === filePath)
    if (t) {
      setFocus(p.id)
      paneApis.get(p.id)?.activateTab(t.id)
      return t.id
    }
  }
  return null
}

function setFocus(paneId: string) {
  for (const p of panes.value) p.focused = p.id === paneId
}

// 打开 RDP 图形会话:同一 (host:port) 只允许一个标签页,已存在则定位激活到其所在 pane。
// 未打开时获取桥接连接信息(生成一次性 token),在焦点 pane 新建 RemoteDesktopTab 组件标签页。
async function openRdp(meta: { sessionPath: string; name: string; host: string; port: number }) {
  const key = `${meta.host}:${meta.port}`
  for (const p of panes.value) {
    const t = p.tabs.find(tab => tab.kind === 'component' && (tab.componentProps as any)?.rdpKey === key)
    if (t) {
      setFocus(p.id)
      paneApis.get(p.id)?.activateTab(t.id)
      return
    }
  }
  let conn: any
  try {
    conn = JSON.parse(await GetRdpConnection(meta.sessionPath))
  } catch (e: any) {
    message.error(t('tabManager.getRdpConnFailed', { err: e?.message || e }))
    return
  }
  focusApi()?.openComponentTab({
    title: `RDP - ${meta.name || conn.host}`,
    component: RemoteDesktopTab,
    props: { conn, rdpKey: key },
    icon: DesktopOutline,
    color: 'var(--primary-color)',
    status: 'connecting',
    // 标签页真正关闭时释放桥接 token(组件销毁时释放会导致跨 pane 拖动重建后
    // 无法用同一 token 重连 WS)。
    onClose: async () => {
      if (conn.authToken) ReleaseRdpConnection(conn.authToken).catch(() => {})
      return true
    },
  })
}

// 打开 VNC 图形会话:同一 (host:port) 只允许一个标签页,已存在则定位激活到其所在 pane。
// 未打开时获取桥接连接信息(生成一次性 token),在焦点 pane 新建 kind='vnc' 组件标签页。
async function openVnc(meta: { sessionPath: string; name: string; host: string; port: number }) {
  const key = `${meta.host}:${meta.port}`
  for (const p of panes.value) {
    const t = p.tabs.find(tab => tab.kind === 'vnc' && (tab.componentProps as any)?.vncKey === key)
    if (t) {
      setFocus(p.id)
      paneApis.get(p.id)?.activateTab(t.id)
      return
    }
  }
  let conn: any
  try {
    conn = JSON.parse(await GetVncConnection(meta.sessionPath))
  } catch (e: any) {
    message.error(t('tabManager.getVncConnFailed', { err: e?.message || e }))
    return
  }
  focusApi()?.openComponentTab({
    title: `VNC - ${meta.name || conn.host}`,
    kind: 'vnc',
    component: VncTab,
    props: { conn, vncKey: key },
    icon: DesktopOutline,
    color: 'var(--primary-color)',
    status: 'connecting',
    sessionPath: meta.sessionPath,
    protocol: 'vnc',
    host: meta.host,
    port: meta.port,
    onClose: async () => {
      if (conn.authToken) ReleaseVncConnection(conn.authToken).catch(() => {})
      return true
    },
  })
}

// 在目标 pane 上向右/向下拆出新 pane（新 pane 在右侧/下方）
function splitPane(targetPaneId: string, dir: SplitDir): Pane {
  const newPane: Pane = { id: newPaneId(), isMain: false, focused: true, tabs: [], activeTabId: null }
  panes.value.push(newPane)
  layout.value = splitAt(layout.value, targetPaneId, dir, newPane.id)
  return newPane
}

function splitAt(tree: LayoutNode, paneId: string, dir: SplitDir, newPaneId: string): LayoutNode {
  if (tree.type === 'pane') {
    if (tree.paneId !== paneId) return tree
    return { type: 'split', dir, ratio: 0.5, a: { ...tree }, b: { type: 'pane', paneId: newPaneId } }
  }
  return { ...tree, a: splitAt(tree.a, paneId, dir, newPaneId), b: splitAt(tree.b, paneId, dir, newPaneId) }
}

function removePaneFromTree(tree: LayoutNode, paneId: string): LayoutNode | null {
  if (tree.type === 'pane') return tree.paneId === paneId ? null : tree
  const a = removePaneFromTree(tree.a, paneId)
  const b = removePaneFromTree(tree.b, paneId)
  if (a && b) return { ...tree, a, b }
  return a ?? b
}

// 销毁子 pane：从布局树移除并折叠父分割节点，释放引用
function destroyPane(paneId: string) {
  const idx = panes.value.findIndex(p => p.id === paneId)
  if (idx === -1) return
  panes.value.splice(idx, 1)
  layout.value = removePaneFromTree(layout.value, paneId) ?? layout.value
  paneApis.delete(paneId)
}

// 跨 pane 移动标签页:摘除 xterm 渲染 DOM(不销毁实例、不丢内容),
// 由目标 pane 挂载时重建终端实例(logBuffer 回放),避免迁移实例键盘失效
function detachTabTerminals(tab: Tab) {
  tab.terminalRebuild = true
  resetTermComposition(tab.terminal)
  tab.terminal?.element?.remove()
}

// 移动标签页到目标 pane 的指定位置(默认追加末尾)。源 pane 失去最后一个标签时按销毁规则处理(由 watch 触发)
function moveTabTo(tabId: string, target: Pane, index?: number) {
  const src = findTabPane(tabId)
  if (!src || src === target) return
  const idx = src.tabs.findIndex(t => t.id === tabId)
  if (idx === -1) return
  const [tab] = src.tabs.splice(idx, 1)
  detachTabTerminals(tab)
  if (src.activeTabId === tabId) {
    src.activeTabId = src.tabs.length > 0 ? src.tabs[Math.min(idx, src.tabs.length - 1)].id : null
  }
  if (index === undefined || index >= target.tabs.length) target.tabs.push(tab)
  else target.tabs.splice(index, 0, tab)
  target.activeTabId = tabId
  setFocus(target.id)
}

// 子 pane 失去最后一个标签页 → 立即销毁；主 pane 永不被销毁（空态由 TabPane 呈现）
watch(
  () => panes.value.map(p => p.tabs.length),
  () => {
    for (const p of [...panes.value]) {
      if (!p.isMain && p.tabs.length === 0) destroyPane(p.id)
    }
  },
)

// 切换为垂直标签模式时，合并所有分屏（分屏仅支持水平模式）
watch(isVertical, (v) => {
  if (!v) return
  const main = ensureMainPane()
  for (const p of [...panes.value]) {
    if (p.isMain) continue
    for (const t of [...p.tabs]) moveTabTo(t.id, main)
    destroyPane(p.id)
  }
  layout.value = { type: 'pane', paneId: main.id }
})

// 无子 pane 且主 pane 空时显示欢迎页（与单标签页模式行为一致）
const showWelcomePaneId = computed(() => {
  if (panes.value.length !== 1) return null
  const p = panes.value[0]
  return p.tabs.length === 0 ? p.id : null
})

// 布局叶子对应的 pane; 不存在时不渲染 TabPane(防御: 禁止以 undefined 挂载)
const activeLayoutPane = computed<Pane | null>(() =>
  layout.value.type === 'pane' ? findPane(layout.value.paneId) ?? null : null)

function focusPane(): Pane {
  const f = panes.value.find(p => p.focused)
  if (f) return f
  return ensureMainPane()
}

function focusApi(): TabPaneApi | null {
  return paneApis.get(focusPane().id) ?? null
}

// 对外 API：转发到当前焦点 pane
async function openSession(sessionPath: string): Promise<string | null> { return (await focusApi()?.openSession(sessionPath)) || null }
function openSerial(portName: string, baudRate: number, dataBits: number, stopBits: string, parity: string) { focusApi()?.openSerial(portName, baudRate, dataBits, stopBits, parity) }
function openSftp() { focusApi()?.openSftp() }
function openScriptDialog() { focusApi()?.openScriptDialog() }
function exportLog() { focusApi()?.exportLog() }
function clearScrollback() { focusApi()?.clearScrollback() }
function clearScreen() { focusApi()?.clearScreen() }
function getActiveSessionPath(): string | null { return focusApi()?.getActiveSessionPath() ?? null }
function openComponentTab(opts: Parameters<TabPaneApi['openComponentTab']>[0]): string | null { return focusApi()?.openComponentTab(opts) ?? null }
function updateComponentTab(tabId: string, patch: Parameters<TabPaneApi['updateComponentTab']>[1]) { focusApi()?.updateComponentTab(tabId, patch) }
function closeTabById(tabId: string) { focusApi()?.closeTabById(tabId) }
function copySelection() { return focusApi()?.copySelection() }
function pasteClipboard() { return focusApi()?.pasteClipboard() }

// ==================== 插件标签页 ====================

// 打开插件标签页: tabKey 幂等(已存在则激活), 组件经插件桥缓存动态加载。
// 标签页类型 = 插件名(protocol); 生命周期经 PluginNotifyTabEvent 回传插件。
async function openPluginTab(payload: {
  pluginID: string; tabId: string; tabKey: string; title: string;
  componentId: string; props?: Record<string, any>; icon?: string; color?: string;
}) {
  for (const p of panes.value) {
    const t = p.tabs.find(tab => (tab.componentProps as any)?.tabKey === payload.tabKey && (tab.componentProps as any)?.pluginID === payload.pluginID)
    if (t) {
      setFocus(p.id)
      // 幂等重开时合并最新 props(工具类标签页靠 recordId 等字段切换内容), 组件实例不重建
      if (payload.props && Object.keys(payload.props).length > 0) {
        const old = (t.componentProps ?? {}) as Record<string, any>
        paneApis.get(p.id)?.updateComponentTab(t.id, {
          props: { ...old, ...payload.props, pluginID: payload.pluginID, tabKey: payload.tabKey, componentId: payload.componentId, ctx: getPluginCtx(payload.pluginID) },
        })
      }
      paneApis.get(p.id)?.activateTab(t.id)
      PluginNotifyTabEvent(payload.pluginID, payload.tabKey, 'activated').catch(() => {})
      return
    }
  }
  const comp = await loadPluginComponent(payload.pluginID, payload.componentId)
  if (!comp) {
    message.error(t('plugins.loadFailed'))
    return
  }
  const tabId = focusApi()?.openComponentTab({
    title: payload.title,
    kind: 'component',
    component: comp,
    props: { ...(payload.props || {}), pluginID: payload.pluginID, tabKey: payload.tabKey, componentId: payload.componentId, ctx: getPluginCtx(payload.pluginID) },
    iconUrl: payload.icon,
    color: payload.color,
    protocol: payload.pluginID,
    onClose: async () => {
      PluginNotifyTabEvent(payload.pluginID, payload.tabKey, 'closed').catch(() => {})
      return true
    },
  })
  if (tabId) PluginNotifyTabEvent(payload.pluginID, payload.tabKey, 'opened').catch(() => {})
}

// reloadPluginTabs 插件代次变化时用新模块就地重建该插件的全部标签页。
// 保留标题/props/用户状态, 不关标签页 —— 重载路径不拆现场, 这正是"热"的含义。
// (卸载/禁用仍走 ShellPanel 的 closePluginTabsOf。)
async function reloadPluginTabs(pluginID: string) {
  for (const p of panes.value) {
    for (const t of p.tabs) {
      if (t.protocol !== pluginID) continue
      const cp = (t.componentProps ?? {}) as Record<string, any>
      if (!cp.componentId) continue
      const comp = await loadPluginComponent(pluginID, String(cp.componentId))
      if (!comp) continue
      paneApis.get(p.id)?.updateComponentTab(t.id, {
        component: comp,
        props: { ...cp, ctx: getPluginCtx(pluginID) },
      })
    }
  }
}

const { plugins: pluginRegistry } = usePlugins()

// openPluginDetailTab 打开插件详情标签页: 每个插件至多一个, 已存在则激活定位。
// 详情是宿主自有组件(非插件 ESM), 数据直读注册表 ref(随失效协议自动刷新)。
// 去重键用 componentProps.detailFor(协议字段会与插件 ID 空间冲突, 不用 protocol 判重)。
function openPluginDetailTab(pluginID: string) {
  for (const p of panes.value) {
    const t = p.tabs.find(tab => (tab.componentProps as any)?.detailFor === pluginID)
    if (t) {
      setFocus(p.id)
      paneApis.get(p.id)?.activateTab(t.id)
      return
    }
  }
  const summary = pluginRegistry.value.find(p => p.id === pluginID)
  focusApi()?.openComponentTab({
    title: summary?.displayName || pluginID,
    kind: 'component',
    component: markRaw(PluginDetailView),
    props: { pluginID, detailFor: pluginID },
    iconUrl: summary?.icon,
    color: summary?.accentColor,
    protocol: 'plugin-detail',
  })
}

// closePluginTab 插件侧请求关闭(插件经 HostService.CloseTab)。
function closePluginTab(pluginID: string, tabKey: string) {
  closeTabById(`plugin://${pluginID}/${tabKey}`)
}

// closePluginTabsOf 关闭某插件的全部标签页(插件停用/卸载后主界面不再保留其内容)。
function closePluginTabsOf(pluginID: string) {
  for (const p of panes.value) {
    for (const t of [...p.tabs]) {
      if (t.protocol === pluginID) paneApis.get(p.id)?.closeTabById(t.id)
    }
  }
}

// setPluginTabTitle 插件侧请求改标题(HostService.SetTabTitle)。
function setPluginTabTitle(pluginID: string, tabKey: string, title: string) {
  if (!title) return
  updateComponentTab(`plugin://${pluginID}/${tabKey}`, { title })
}

// ==================== MCP 桥接 API ====================

// listTabs 列出全部 pane 的全部标签页(MCP list_tabs 工具)
function listTabs() {
  const out: any[] = []
  for (const p of panes.value) {
    for (const tab of p.tabs) {
      out.push({
        id: tab.id,
        title: tab.title,
        kind: tab.kind,
        protocol: tab.protocol,
        status: tab.status,
        sessionPath: tab.sessionPath || (tab.componentProps as any)?.filePath || '',
      })
    }
  }
  return out
}

// 按 tabId 定位所属 pane 的 API
function apiForTab(tabId: string): TabPaneApi | null {
  const p = findTabPane(tabId)
  if (!p) return null
  return paneApis.get(p.id) ?? null
}

// mcpTerminalSend 转发到目标标签页所在 pane(activateTab=false 时不跳转)
function mcpTerminalSend(tabId: string, text: string, needPasteConfirm: boolean, activateTab = true): Promise<{ ok: boolean; note?: string }> {
  const api = apiForTab(tabId)
  if (!api) return Promise.resolve({ ok: false, note: 'tab not found: ' + tabId })
  return api.mcpTerminalSend(tabId, text, needPasteConfirm, activateTab)
}

// mcpCloseTab 转发到目标标签页所在 pane(activateTab=false 时不跳转)
function mcpCloseTab(tabId: string, activateTab = true): Promise<{ ok: boolean; note?: string }> {
  const api = apiForTab(tabId)
  if (!api) return Promise.resolve({ ok: false, note: 'tab not found: ' + tabId })
  return api.mcpCloseTab(tabId, activateTab)
}

// 状态栏信息：仅转发焦点 pane
function onStatus(paneId: string, text: string, row: number, col: number, encoding: string, hasTab: boolean) {
  const p = findPane(paneId)
  if (p?.focused) emit('status', { text, row, col, encoding, hasTab })
}

// 活动标签页状态：仅转发焦点 pane（供顶级菜单按活动标签页启用/禁用工具）
function onActiveTabState(paneId: string, state: ActiveTabState) {
  const p = findPane(paneId)
  if (p?.focused) emit('active-tab-state', state)
}

// 文件编辑器光标位置上报:转发到焦点 pane
function reportCursor(row: number, col: number) { focusApi()?.reportCursor(row, col) }

// 配置变化：终端个性化配置，应用到全部 pane 的终端
async function reloadTermConfig() {
  try {
    const cfg = JSON.parse(await GetConfig())
    termCfg.value = normalizeTermConfig(cfg.terminal)
  } catch {}
  for (const p of panes.value) {
    for (const tab of p.tabs) {
      applyTermCfg(tab.terminal, termCfg.value)
    }
  }
}

function closeSftpPanelsOf(connID: string) {
  for (const p of panes.value) {
    for (const t of [...p.tabs]) {
      if (t.kind === 'component' && t.componentProps?.sessionID === connID) {
        const idx = p.tabs.findIndex(x => x.id === t.id)
        if (idx === -1) continue
        t.onClose?.()
        p.tabs.splice(idx, 1)
        if (p.activeTabId === t.id) {
          p.activeTabId = p.tabs.length > 0 ? p.tabs[Math.min(idx, p.tabs.length - 1)].id : null
        }
      }
    }
  }
}

provide<PaneCtx>('pane-ctx', {
  actions: {
    onSplit: (paneId, tabId, dir) => {
      const src = findPane(paneId)
      if (!src) return
      // 主 pane 最后一个标签页不可拆分
      if (src.isMain && src.tabs.length === 1) return
      const newPane = splitPane(paneId, dir)
      const idx = src.tabs.findIndex(t => t.id === tabId)
      if (idx === -1) return
      const [tab] = src.tabs.splice(idx, 1)
      detachTabTerminals(tab)
      if (src.activeTabId === tabId) {
        src.activeTabId = src.tabs.length > 0 ? src.tabs[Math.min(idx, src.tabs.length - 1)].id : null
      }
      newPane.tabs.push(tab)
      newPane.activeTabId = tabId
      setFocus(newPane.id)
    },
    onMoveTab: (tabId, targetPaneId, index) => {
      const target = findPane(targetPaneId)
      if (!target) return
      // 主 pane 最后一个标签页不可移动到其他 pane
      const src = findTabPane(tabId)
      if (src && src.isMain && src.tabs.length === 1) return
      moveTabTo(tabId, target, index)
    },
    onSplitAt: (tabId, targetPaneId, dir) => {
      const target = findPane(targetPaneId)
      if (!target) return
      // 主 pane 最后一个标签页不可拆出到新分屏
      const src = findTabPane(tabId)
      if (!src) return
      if (src.isMain && src.tabs.length === 1) return
      const newPane = splitPane(targetPaneId, dir)
      const idx = src.tabs.findIndex(t => t.id === tabId)
      if (idx === -1) return
      const [tab] = src.tabs.splice(idx, 1)
      detachTabTerminals(tab)
      if (src.activeTabId === tabId) {
        src.activeTabId = src.tabs.length > 0 ? src.tabs[Math.min(idx, src.tabs.length - 1)].id : null
      }
      newPane.tabs.push(tab)
      newPane.activeTabId = tabId
      setFocus(newPane.id)
    },
    onFocus: (paneId) => setFocus(paneId),
    openRdp,
    openVnc,
    onStatus,
    onActiveTabState,
    registerPane: (paneId, api) => {
      paneApis.set(paneId, api)
      return () => { paneApis.delete(paneId) }
    },
    paneExists: (paneId) => panes.value.some(p => p.id === paneId),
  },
})

const pass = computed(() => ({
  showToolbar: props.showToolbar,
  isVertical: isVertical.value,
  verticalWidth: props.verticalTabWidth || 180,
  termCfg: termCfg.value,
  showWelcomePaneId: showWelcomePaneId.value,
}))

let offOutput: (() => void) | null = null
let offStatus: (() => void) | null = null

onMounted(async () => {
  await reloadTermConfig()
  window.addEventListener('config-changed', reloadTermConfig)

  offOutput = Events.On('session-output', (evt: any) => {
    try {
      const info = JSON.parse(evt.data)
      for (const p of panes.value) {
        const tab = p.tabs.find(t => t.id === info.id)
        if (tab?.terminal) {
          tab.terminal.write(info.data)
          tab.logBuffer += info.data
          if (tab.logBuffer.length > 512 * 1024) tab.logBuffer = tab.logBuffer.slice(-256 * 1024)
          return
        }
      }
    } catch {}
  })

  offStatus = Events.On('session-status-changed', (evt: any) => {
    try {
      const info = JSON.parse(evt.data)
      for (const p of panes.value) {
        const tab = p.tabs.find(t => t.id === info.id)
        if (tab) {
          if (info.status === 'disconnected' || info.status === 'error') {
            tab.status = info.status === 'error' ? 'error' : 'idle'
            closeSftpPanelsOf(info.id)
            if (info.message) tab.terminal?.write(`\r\n\x1b[31m${info.message}\x1b[0m\r\n`)
            tab.terminal?.write('\x1b[90m' + t('tabPane.enterToReconnect') + '\x1b[0m\r\n')
          }
          return
        }
      }
    } catch {}
  })
})

onBeforeUnmount(() => {
  window.removeEventListener('config-changed', reloadTermConfig)
  offOutput?.()
  offStatus?.()
  for (const p of panes.value) {
    for (const t of p.tabs) {
      t.terminalCleanup?.()
      t.terminal?.dispose()
    }
  }
})

defineExpose({ openSession, openSerial, openSftp, openScriptDialog, exportLog, clearScrollback, clearScreen, getActiveSessionPath, openComponentTab, updateComponentTab, closeTabById, activateFileTab, reportCursor, copySelection, pasteClipboard, openRdp, openVnc, listTabs, mcpTerminalSend, mcpCloseTab, openPluginTab, closePluginTab, closePluginTabsOf, setPluginTabTitle, reloadPluginTabs, openPluginDetailTab })
</script>

<template>
  <div class="tab-manager" :class="{ 'vertical-tabs': isVertical }">
    <template v-if="layout.type === 'pane' && activeLayoutPane">
      <TabPane :key="layout.paneId" :pane="activeLayoutPane" :show-toolbar="pass.showToolbar"
        :is-vertical="pass.isVertical" :vertical-width="pass.verticalWidth" :term-cfg="pass.termCfg" :show-welcome-pane-id="pass.showWelcomePaneId"
        @new-ssh="emit('new-ssh')" @new-telnet="emit('new-telnet')" @new-serial="emit('new-serial')" />
    </template>
    <SplitPane v-else :node="layout as SplitNode" :panes="panes" :pass="pass"
      @new-ssh="emit('new-ssh')" @new-telnet="emit('new-telnet')" @new-serial="emit('new-serial')" />
  </div>
</template>

<style scoped>
.tab-manager { flex: 1; min-width: 0; height: 100%; display: flex; flex-direction: column; overflow: hidden; }
.tab-manager.vertical-tabs { flex-direction: row; }
</style>