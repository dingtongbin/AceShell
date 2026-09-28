// MCP 前端桥接器(模块级单例)。
// 职责:
//   1. 订阅后端事件(mcp-command / mcp-audit / mcp-status / mcp-critical-blocked)
//   2. 把 MCP 工具命令路由到与用户手动操作完全相同的前端路径(同一 UI、同一弹窗)
//   3. 命令严格串行(promise 队列 FIFO): 后端仲裁器已串行,前端兜底防并发竞态
//   4. activateTab=false 的命令(批量执行)不切换标签页;opDelayMs 为可控操作延迟
//   5. 用户键盘抢占检测: 终端/编辑器收到用户手动输入时立即通知后端挂起 MCP
import { ref } from 'vue'
import { Events } from '@wailsio/runtime'
import {
  GetMcpStatus,
  GetMcpAuditLog,
  McpResolveCommand,
  McpNotifyPreemption,
  McpPoll,
  SetMcpExecTuning,
} from '../../bindings/changeme/internal/services/mcpservice.js'

// ==================== 类型 ====================

/** 智能体独占锁持有者(GIL 语义: 同一时刻仅一个智能体可操作 MCP;null=空闲)。 */
export interface McpLockInfo {
  owner: string
  /** 展示名: "内嵌智能体" 或 客户端名+会话短码(如 "opencode#a1b2") */
  label: string
  kind: 'embedded' | 'external'
  heldForSec: number
  idleSec: number
}

export interface McpStatus {
  enabled: boolean
  state: 'stopped' | 'running' | 'paused'
  /** 仲裁执行槽占用中(工具调用执行期),驱动"MCP 执行中"按钮与标签页遮罩 */
  busy: boolean
  /** 智能体独占锁持有者(工具调用之间仍持续持有,驱动持锁者指示) */
  lock: McpLockInfo | null
  url: string
  token: string
  port: number
  ballX: number
  ballY: number
  opDelayMs: number
  batchIntervalMs: number
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
  enabled: false, state: 'stopped', busy: false, lock: null, url: '', token: '',
  port: 8940, ballX: -1, ballY: -1,
  opDelayMs: 1000, batchIntervalMs: 300,
  auditRetentionDays: 30, terminalReadMax: 32768,
})
const auditLog = ref<McpAuditEntry[]>([])
const criticalBlock = ref<McpCriticalBlock | null>(null)

let tabManagerApi: McpTabManagerApi | null = null
let openScriptHandler: ((filePath: string) => Promise<string | null>) | null = null
const editorRegistry = new Map<string, { isDirty: () => boolean; save: () => Promise<boolean>; setContent: (text: string) => void }>()

let started = false
let preemptLock = false

// ==================== 轮询通道(binding 主通道) ====================

// 事件推送(mcp-command 等)在部分 WebView 环境不可达(前后端 runtime 版本
// 错配),binding 轮询为命令下发与状态刷新的主通道,事件仅作冗余。
const MCP_POLL_MS = 600
let pollTimer: ReturnType<typeof setInterval> | null = null
let lastCriticalID = 0
// 双通道命令去重: 事件与轮询可能先后送达同一 requestId(领取即删,取先到者)
const seenCmdIDs = new Set<string>()

/** handleCommand 命令入口(事件/轮询共用),按 requestId 幂等。 */
function handleCommand(cmd: any) {
  if (!cmd?.requestId || seenCmdIDs.has(cmd.requestId)) return
  seenCmdIDs.add(cmd.requestId)
  if (seenCmdIDs.size > 256) {
    let n = 128
    for (const id of seenCmdIDs) { seenCmdIDs.delete(id); if (--n <= 0) break }
  }
  enqueueDispatch(async () => {
    try {
      await dispatchCommand(cmd.requestId, cmd.type, cmd.payload || {})
    } catch (e: any) {
      resolveCmd(cmd.requestId, '', String(e?.message || e)).catch(() => {})
    }
  })
}

/** startMcpPolling 启动轮询(幂等): 每次响应直刷状态、领取命令、消费拦截告警。 */
function startMcpPolling() {
  if (pollTimer !== null) return
  pollTimer = setInterval(async () => {
    try {
      const res = JSON.parse(await McpPoll(lastCriticalID) || '{}')
      if (!res || typeof res !== 'object') return
      // 状态兜底刷新(等价 mcp-status-changed)
      Object.assign(status.value, {
        enabled: !!res.enabled, state: res.state, busy: !!res.busy, lock: res.lock ?? null,
        url: res.url ?? '', token: res.token ?? '', port: res.port ?? 8940,
        ballX: res.ballX ?? -1, ballY: res.ballY ?? -1,
        opDelayMs: res.opDelayMs ?? 1000, batchIntervalMs: res.batchIntervalMs ?? 300,
        auditRetentionDays: res.auditRetentionDays ?? 30, terminalReadMax: res.terminalReadMax ?? 32768,
      })
      // 绝对危险拦截告警(等价 mcp-critical-blocked 兜底)
      for (const c of res.criticals || []) {
        if (c?.id > lastCriticalID) {
          lastCriticalID = c.id
          criticalBlock.value = { command: c.command, reason: c.reason }
        }
      }
      // 待执行命令(领取即删)
      for (const cmd of res.commands || []) handleCommand(cmd)
    } catch { /* 后端未就绪等瞬时错误,静默重试 */ }
  }, MCP_POLL_MS)
}

// ==================== 事件订阅 ====================

function parseEvt(evt: any): any {
  // 防御: 运行时若已反序列化为对象则直通, 字符串再 parse(双编码/包装均兼容)
  const d = evt?.data
  if (d == null) return null
  if (typeof d === 'object') return d
  try { return JSON.parse(d) } catch { return null }
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
  // (事件通道,与轮询通道共用 handleCommand,按 requestId 幂等)
  Events.On('mcp-command', async (evt: any) => {
    const cmd = parseEvt(evt)
    handleCommand(cmd)
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

  // 启动 binding 轮询主通道(命令下发/状态刷新/拦截告警)
  startMcpPolling()
}

// ==================== 命令路由(严格串行) ====================

// dispatch 队列: 同一时刻仅一条命令在执行(后端仲裁器已串行,前端兜底)
let dispatchTail: Promise<void> = Promise.resolve()

/** enqueueDispatch 串行入队;异常在内部消化并回执后端,绝不中断队列。 */
function enqueueDispatch(run: () => Promise<void>) {
  dispatchTail = dispatchTail.then(run, run)
}

/** resolveCmd 回执(带重试): binding 偶发失败会使命令"已执行但无回执",
 * 服务端只能判超时,MCP 客户端将重发命令 —— 非幂等命令(terminal_send)
 * 有重复执行风险,故回执失败时短暂重试 3 次。 */
async function resolveCmd(requestId: string, result: string, errMsg: string) {
  for (let i = 0; i < 3; i++) {
    try {
      await McpResolveCommand(requestId, result, errMsg)
      return
    } catch { /* 瞬时失败,重试 */ }
    await new Promise(r => setTimeout(r, 300))
  }
}

/** sleep 可中断延时(挂起/抢占时命令会被后端取消,延时只是尽力而为)。 */
function sleep(ms: number): Promise<void> {
  return new Promise(resolve => setTimeout(resolve, ms))
}

/** ensureRunning 命令执行前/延时后复查 MCP 状态。
 * dispatch 排队与 opDelayMs 等待期间状态可能翻转(用户挂起/停止/抢占),
 * 不复查会出现"已挂起仍向终端发送输入"的竞态窗口 —— 后端取消只对
 * 等待中的请求生效, 已进入前端执行体的命令只能在这里拦截。 */
function ensureRunning(requestId: string): boolean {
  if (status.value.state === 'running') return true
  const why = status.value.state === 'paused' ? 'MCP 已挂起, 拒绝执行' : 'MCP 已停止, 拒绝执行'
  resolveCmd(requestId, '', why).catch(() => {})
  return false
}

async function dispatchCommand(requestId: string, type: string, payload: any) {
  const activateTab = payload?.activateTab !== false
  // 可视时延: 激活标签页后给用户留出观察时间(0 = 关闭)
  const opDelayMs = Math.max(0, Number(payload?.opDelayMs) || 0)
  // 执行前复查(排队期间状态可能已翻转)
  if (!ensureRunning(requestId)) return

  switch (type) {
    case 'list_tabs': {
      // keyword 非空时按名称/ID 模糊过滤(不区分大小写),避免动辄返回全部标签页
      const kw = String(payload?.keyword || '').trim().toLowerCase()
      const all = tabManagerApi ? tabManagerApi.listTabs() : []
      const tabs = kw
        ? all.filter(tb =>
            String(tb?.title || '').toLowerCase().includes(kw) ||
            String(tb?.id || '').toLowerCase().includes(kw))
        : all
      resolveCmd(requestId, JSON.stringify(tabs), '').catch(() => {})
      break
    }
    case 'open_session': {
      if (!tabManagerApi) { resolveCmd(requestId, '', '前端未就绪').catch(() => {}); return }
      const tabId = await tabManagerApi.openSession(payload.sessionPath)
      if (!tabId) {
        resolveCmd(requestId, '', '打开会话失败(会话不存在或协议不支持)').catch(() => {})
      } else {
        resolveCmd(requestId, JSON.stringify({ tab_id: tabId, status: 'opened' }), '').catch(() => {})
      }
      break
    }
    case 'terminal_send': {
      if (!tabManagerApi) { resolveCmd(requestId, '', '前端未就绪').catch(() => {}); return }
      if (opDelayMs > 0 && activateTab) {
        await sleep(opDelayMs)
        // 延时窗口内用户可能已挂起/停止, 发送前再查一次
        if (!ensureRunning(requestId)) return
      }
      const res = await tabManagerApi.mcpTerminalSend(payload.tabId, payload.text, false, activateTab)
      if (!res.ok) {
        resolveCmd(requestId, '', res.note || '发送失败').catch(() => {})
      } else {
        resolveCmd(requestId, JSON.stringify({ ok: true, note: res.note || '' }), '').catch(() => {})
      }
      break
    }
    case 'batch_execute': {
      // 批量执行: 不切换标签页(activateTab=false),逐条串行发送,间隔 intervalMs
      if (!tabManagerApi) { resolveCmd(requestId, '', '前端未就绪').catch(() => {}); return }
      const commands: string[] = Array.isArray(payload.commands) ? payload.commands : []
      const intervalMs = Math.max(50, Number(payload.intervalMs) || 200)
      const results: any[] = []
      for (let i = 0; i < commands.length; i++) {
        if (i > 0) await sleep(intervalMs)
        const res = await tabManagerApi.mcpTerminalSend(payload.tabId, commands[i], false, false)
        results.push({ index: i + 1, ok: res.ok, note: res.note || '' })
        if (!res.ok) {
          // 单条失败即停止后续(连接断开等场景继续无意义)
          resolveCmd(requestId, '', `第 ${i + 1} 条执行失败: ${res.note || '发送失败'}`).catch(() => {})
          return
        }
      }
      resolveCmd(requestId, JSON.stringify({ ok: true, executed: results.length, results }), '').catch(() => {})
      break
    }
    case 'open_script': {
      if (!openScriptHandler) { resolveCmd(requestId, '', '前端未就绪').catch(() => {}); return }
      const tabId = await openScriptHandler(payload.filePath)
      if (!tabId) {
        resolveCmd(requestId, '', '打开脚本失败').catch(() => {})
      } else {
        resolveCmd(requestId, JSON.stringify({ tab_id: tabId }), '').catch(() => {})
      }
      break
    }
    case 'script_write': {
      // 先确保文件在编辑器标签页中打开,再通过编辑器 API 写入(与用户编辑完全一致)
      if (!openScriptHandler) { resolveCmd(requestId, '', '前端未就绪').catch(() => {}); return }
      const tabId = await openScriptHandler(payload.filePath)
      if (!tabId) { resolveCmd(requestId, '', '打开脚本失败').catch(() => {}); return }
      const api = await waitForEditor(payload.filePath)
      if (!api) { resolveCmd(requestId, '', '编辑器未就绪').catch(() => {}); return }
      api.setContent(payload.content)
      resolveCmd(requestId, JSON.stringify({ ok: true, note: '内容已写入编辑器' }), '').catch(() => {})
      break
    }
    case 'close_tab': {
      if (!tabManagerApi) { resolveCmd(requestId, '', '前端未就绪').catch(() => {}); return }
      if (opDelayMs > 0 && activateTab) {
        await sleep(opDelayMs)
        if (!ensureRunning(requestId)) return
      }
      const res = await tabManagerApi.mcpCloseTab(payload.tabId, activateTab)
      if (!res.ok) {
        resolveCmd(requestId, '', res.note || '关闭失败').catch(() => {})
      } else {
        resolveCmd(requestId, JSON.stringify({ ok: true }), '').catch(() => {})
      }
      break
    }
    default:
      resolveCmd(requestId, '', '未知命令: ' + type).catch(() => {})
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

// ==================== 执行参数(设置面板调用) ====================

function saveExecTuning(opDelayMs: number, batchIntervalMs: number, auditRetentionDays: number, terminalReadMax: number) {
  return SetMcpExecTuning(opDelayMs, batchIntervalMs, auditRetentionDays, terminalReadMax).catch(() => '')
}

export function useMcpBridge() {
  return {
    status, auditLog, criticalBlock,
    initMcpBridge,
    bindTabManager, bindOpenScriptHandler, registerEditor, unregisterEditor,
    notifyUserInput,
    saveExecTuning,
  }
}
