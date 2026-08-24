// MCP 前端桥接器(模块级单例)。
// 职责:
//   1. 订阅后端事件(mcp-command / mcp-approval / mcp-audit / mcp-status / mcp-critical-blocked)
//   2. 把 MCP 工具命令路由到与用户手动操作完全相同的前端路径(同一 UI、同一弹窗)
//   3. 命令严格串行(promise 队列 FIFO): 后端仲裁器已串行,前端兜底防并发竞态
//   4. activateTab=false 的命令(批量执行)不切换标签页;opDelayMs 为可视时延
//   5. 用户键盘抢占检测: 终端/编辑器收到用户手动输入时立即通知后端挂起 MCP
import { ref } from 'vue'
import { Events } from '@wailsio/runtime'
import {
  GetMcpStatus,
  GetMcpAuditLog,
  McpResolveCommand,
  McpResolveApproval,
  McpNotifyPreemption,
  GetMcpGrants,
  RemoveMcpGrant,
  ClearMcpGrants,
  SetMcpExecTuning,
  SetMcpCustomRules,
} from '../../bindings/changeme/internal/services/mcpservice.js'

// ==================== 类型 ====================

export interface McpStatus {
  enabled: boolean
  state: 'stopped' | 'running' | 'paused'
  /** 仲裁执行槽占用中(工具调用执行期),驱动"MCP 执行中"按钮与标签页遮罩 */
  busy: boolean
  mode: 'manual' | 'auto'
  url: string
  token: string
  port: number
  pendingApprovals: number
  ballX: number
  ballY: number
  opDelayMs: number
  batchIntervalMs: number
  grantsEnabled: boolean
  auditRetentionDays: number
  terminalReadMax: number
}

export interface McpAuditEntry {
  id: string
  ts: string
  level: string
  action: string
  subject: string
  detail: string
  risk: string
  decision: string
  byUser: boolean
  source: string
  batchId: string
}

export interface McpApproval {
  id: string
  action: string
  summary: string
  detail: string
  risk: string
  command: string
  paths: string[]
  expiresAt: string
}

export interface McpGrant {
  id: string
  command: string
  paths: string[]
  createdAt: string
}

export interface McpCriticalBlock {
  command: string
  reason: string
}

// TabManager 提供的命令执行接口(与用户操作同路径)
export interface McpTabManagerApi {
  listTabs: () => any[]
  openSession: (sessionPath: string) => string | null | Promise<string | null>
  mcpTerminalSend: (tabId: string, text: string, needPasteConfirm: boolean, activateTab: boolean) => Promise<{ ok: boolean; note?: string }>
  mcpCloseTab: (tabId: string, activateTab: boolean) => Promise<{ ok: boolean; note?: string }>
}

// ==================== 状态 ====================

const status = ref<McpStatus>({
  enabled: false, state: 'stopped', busy: false, mode: 'manual', url: '', token: '',
  port: 8940, pendingApprovals: 0, ballX: -1, ballY: -1,
  opDelayMs: 1000, batchIntervalMs: 300, grantsEnabled: true,
  auditRetentionDays: 30, terminalReadMax: 32768,
})
const auditLog = ref<McpAuditEntry[]>([])
const pendingApprovals = ref<McpApproval[]>([])
const criticalBlock = ref<McpCriticalBlock | null>(null)

let tabManagerApi: McpTabManagerApi | null = null
let openScriptHandler: ((filePath: string) => Promise<string | null>) | null = null
const editorRegistry = new Map<string, { isDirty: () => boolean; save: () => Promise<boolean>; setContent: (text: string) => void }>()

let started = false
let preemptLock = false

// ==================== 事件订阅 ====================

function parseEvt(evt: any): any {
  try { return JSON.parse(evt.data) } catch { return null }
}

/** init 初始化桥接器(应用启动时调用一次;重复调用幂等)。 */
async function initMcpBridge() {
  if (started) return
  started = true

  // 初始状态与历史日志
  try {
    const s = JSON.parse(await GetMcpStatus())
    Object.assign(status.value, s)
  } catch {}
  try {
    const list = JSON.parse(await GetMcpAuditLog(-200, 200))
    if (Array.isArray(list)) auditLog.value = list
  } catch {}

  // MCP 工具命令 → 串行队列 → 路由到与用户完全相同的 UI 路径
  Events.On('mcp-command', async (evt: any) => {
    const cmd = parseEvt(evt)
    if (!cmd?.requestId) return
    enqueueDispatch(async () => {
      try {
        await dispatchCommand(cmd.requestId, cmd.type, cmd.payload || {})
      } catch (e: any) {
        McpResolveCommand(cmd.requestId, '', String(e?.message || e)).catch(() => {})
      }
    })
  })

  // 审批请求: 弹窗等用户决策
  Events.On('mcp-approval-requested', (evt: any) => {
    const ap = parseEvt(evt)
    if (!ap?.id) return
    pendingApprovals.value.push(ap)
  })

  // 审批被移除(超时/挂起/抢占): 清理待审批列表
  Events.On('mcp-approval-removed', (evt: any) => {
    const info = parseEvt(evt)
    if (!info?.id) return
    pendingApprovals.value = pendingApprovals.value.filter(a => a.id !== info.id)
  })

  // 审计日志实时追加(有界: 前端只保留最近 500 条,历史查后端)
  Events.On('mcp-audit-appended', (evt: any) => {
    const entry = parseEvt(evt)
    if (!entry?.id) return
    auditLog.value.push(entry)
    if (auditLog.value.length > 500) auditLog.value = auditLog.value.slice(-500)
  })

  // 状态变更
  Events.On('mcp-status-changed', (evt: any) => {
    const s = parseEvt(evt)
    if (s) Object.assign(status.value, s)
  })

  // 绝对危险指令被拦截: 弹窗提示(MCP 已自动挂起)
  Events.On('mcp-critical-blocked', (evt: any) => {
    const info = parseEvt(evt)
    if (info) criticalBlock.value = info
  })
}

// ==================== 命令路由(严格串行) ====================

// dispatch 队列: 同一时刻仅一条命令在执行(后端仲裁器已串行,前端兜底)
let dispatchTail: Promise<void> = Promise.resolve()

/** enqueueDispatch 串行入队;异常在内部消化并回执后端,绝不中断队列。 */
function enqueueDispatch(run: () => Promise<void>) {
  dispatchTail = dispatchTail.then(run, run)
}

/** sleep 可中断延时(挂起/抢占时命令会被后端取消,延时只是尽力而为)。 */
function sleep(ms: number): Promise<void> {
  return new Promise(resolve => setTimeout(resolve, ms))
}

async function dispatchCommand(requestId: string, type: string, payload: any) {
  const activateTab = payload?.activateTab !== false
  // 可视时延: 激活标签页后给用户留出观察时间(0 = 关闭)
  const opDelayMs = Math.max(0, Number(payload?.opDelayMs) || 0)

  switch (type) {
    case 'list_tabs': {
      const tabs = tabManagerApi ? tabManagerApi.listTabs() : []
      McpResolveCommand(requestId, JSON.stringify(tabs), '').catch(() => {})
      break
    }
    case 'open_session': {
      if (!tabManagerApi) { McpResolveCommand(requestId, '', '前端未就绪').catch(() => {}); return }
      const tabId = await tabManagerApi.openSession(payload.sessionPath)
      if (!tabId) {
        McpResolveCommand(requestId, '', '打开会话失败(会话不存在或协议不支持)').catch(() => {})
      } else {
        McpResolveCommand(requestId, JSON.stringify({ tab_id: tabId, status: 'opened' }), '').catch(() => {})
      }
      break
    }
    case 'terminal_send': {
      if (!tabManagerApi) { McpResolveCommand(requestId, '', '前端未就绪').catch(() => {}); return }
      if (opDelayMs > 0 && activateTab) await sleep(opDelayMs)
      const res = await tabManagerApi.mcpTerminalSend(payload.tabId, payload.text, !!payload.needPasteConfirm, activateTab)
      if (!res.ok) {
        McpResolveCommand(requestId, '', res.note || '发送失败').catch(() => {})
      } else {
        McpResolveCommand(requestId, JSON.stringify({ ok: true, note: res.note || '' }), '').catch(() => {})
      }
      break
    }
    case 'batch_execute': {
      // 批量执行: 不切换标签页(activateTab=false),逐条串行发送,间隔 intervalMs
      if (!tabManagerApi) { McpResolveCommand(requestId, '', '前端未就绪').catch(() => {}); return }
      const commands: string[] = Array.isArray(payload.commands) ? payload.commands : []
      const intervalMs = Math.max(50, Number(payload.intervalMs) || 200)
      const results: any[] = []
      for (let i = 0; i < commands.length; i++) {
        if (i > 0) await sleep(intervalMs)
        const res = await tabManagerApi.mcpTerminalSend(payload.tabId, commands[i], false, false)
        results.push({ index: i + 1, ok: res.ok, note: res.note || '' })
        if (!res.ok) {
          // 单条失败即停止后续(连接断开等场景继续无意义)
          McpResolveCommand(requestId, '', `第 ${i + 1} 条执行失败: ${res.note || '发送失败'}`).catch(() => {})
          return
        }
      }
      McpResolveCommand(requestId, JSON.stringify({ ok: true, executed: results.length, results }), '').catch(() => {})
      break
    }
    case 'open_script': {
      if (!openScriptHandler) { McpResolveCommand(requestId, '', '前端未就绪').catch(() => {}); return }
      const tabId = await openScriptHandler(payload.filePath)
      if (!tabId) {
        McpResolveCommand(requestId, '', '打开脚本失败').catch(() => {})
      } else {
        McpResolveCommand(requestId, JSON.stringify({ tab_id: tabId }), '').catch(() => {})
      }
      break
    }
    case 'script_write': {
      // 先确保文件在编辑器标签页中打开,再通过编辑器 API 写入(与用户编辑完全一致)
      if (!openScriptHandler) { McpResolveCommand(requestId, '', '前端未就绪').catch(() => {}); return }
      const tabId = await openScriptHandler(payload.filePath)
      if (!tabId) { McpResolveCommand(requestId, '', '打开脚本失败').catch(() => {}); return }
      const api = await waitForEditor(payload.filePath)
      if (!api) { McpResolveCommand(requestId, '', '编辑器未就绪').catch(() => {}); return }
      api.setContent(payload.content)
      McpResolveCommand(requestId, JSON.stringify({ ok: true, note: '内容已写入编辑器' }), '').catch(() => {})
      break
    }
    case 'close_tab': {
      if (!tabManagerApi) { McpResolveCommand(requestId, '', '前端未就绪').catch(() => {}); return }
      if (opDelayMs > 0 && activateTab) await sleep(opDelayMs)
      const res = await tabManagerApi.mcpCloseTab(payload.tabId, activateTab)
      if (!res.ok) {
        McpResolveCommand(requestId, '', res.note || '关闭失败').catch(() => {})
      } else {
        McpResolveCommand(requestId, JSON.stringify({ ok: true }), '').catch(() => {})
      }
      break
    }
    default:
      McpResolveCommand(requestId, '', '未知命令: ' + type).catch(() => {})
  }
}

// 等待编辑器 API 就绪(组件挂载需要 1~2 个 tick,上限 2 秒)
function waitForEditor(filePath: string, deadline = 2000): Promise<any> {
  return new Promise(resolve => {
    const t0 = Date.now()
    const timer = setInterval(() => {
      const api = editorRegistry.get(filePath)
      if (api) { clearInterval(timer); resolve(api); return }
      if (Date.now() - t0 > deadline) { clearInterval(timer); resolve(null) }
    }, 50)
  })
}

// ==================== 抢占检测 ====================

/**
 * notifyUserInput 用户手动输入通知(终端键盘输入、编辑器键入、手动粘贴均调用)。
 * MCP 运行中 → 立即通知后端挂起并取消全部在途操作(用户优先)。
 * preemptLock 防抖: 挂起后 state 变为 paused,后续输入不再重复通知。
 */
function notifyUserInput() {
  if (status.value.state !== 'running') return
  if (preemptLock) return
  preemptLock = true
  McpNotifyPreemption().catch(() => {}).finally(() => {
    // 状态事件回执后解锁;兜底 1 秒解锁避免竞态卡死
    setTimeout(() => { preemptLock = false }, 1000)
  })
}

// ==================== 注册接口(ShellPanel 启动时注入) ====================

function bindTabManager(api: McpTabManagerApi) { tabManagerApi = api }
function bindOpenScriptHandler(handler: (filePath: string) => Promise<string | null>) { openScriptHandler = handler }
function registerEditor(filePath: string, api: any) { editorRegistry.set(filePath, api) }
function unregisterEditor(filePath: string) { editorRegistry.delete(filePath) }

/** approveApproval / denyApproval 审批弹窗决策回执(permanent=永久授权,仅批准时有效)。 */
function approveApproval(id: string, permanent = false) {
  pendingApprovals.value = pendingApprovals.value.filter(a => a.id !== id)
  McpResolveApproval(id, true, permanent).catch(() => {})
}
function denyApproval(id: string) {
  pendingApprovals.value = pendingApprovals.value.filter(a => a.id !== id)
  McpResolveApproval(id, false, false).catch(() => {})
}

// ==================== 永久授权/执行参数(设置面板调用) ====================

function refreshGrants(): Promise<McpGrant[]> {
  return GetMcpGrants().then(raw => {
    const list = JSON.parse(raw)
    return Array.isArray(list) ? list : []
  }).catch(() => [])
}
function removeGrant(id: string) { return RemoveMcpGrant(id).catch(() => '') }
function clearGrants() { return ClearMcpGrants().catch(() => '') }
function saveExecTuning(opDelayMs: number, batchIntervalMs: number, grantsEnabled: boolean, auditRetentionDays: number, terminalReadMax: number) {
  return SetMcpExecTuning(opDelayMs, batchIntervalMs, grantsEnabled, auditRetentionDays, terminalReadMax).catch(() => '')
}
function saveCustomRules(jsonStr: string) { return SetMcpCustomRules(jsonStr).catch(() => '') }

export function useMcpBridge() {
  return {
    status, auditLog, pendingApprovals, criticalBlock,
    initMcpBridge,
    bindTabManager, bindOpenScriptHandler, registerEditor, unregisterEditor,
    notifyUserInput,
    approveApproval, denyApproval,
    refreshGrants, removeGrant, clearGrants, saveExecTuning, saveCustomRules,
  }
}
