// 智能体前端桥接器(模块级单例)。
// 职责:
//   1. 订阅后端事件(agent-event / agent-stream / agent-status-changed / agent-error)
//   2. 会话列表与当前会话事件窗口管理(分页懒加载,前端仅保留尾部窗口)
//   3. 全局串行约束由后端 AgentService 保证;前端仅展示状态与发送入口
import { ref, computed } from 'vue'
import { Events } from '@wailsio/runtime'
import {
  GetAgentStatus,
  GetAgentSkills,
  AgentSessionList,
  AgentNewSession,
  AgentSend,
  AgentInterrupt,
  AgentPendingFlush,
  AgentPendingDiscard,
  AgentRegenerate,
  AgentSessionEvents,
  AgentSessionTodos,
  AgentSessionSkills,
  AgentSetSessionSkills,
  AgentRenameSession,
  AgentArchiveSession,
  AgentDeleteSession,
} from '../../bindings/changeme/internal/services/agentservice.js'

// ==================== 类型 ====================

// ==================== 对话流密度(全局共享,localStorage 持久化;设置弹窗与聊天面板共用) ====================
export type AgentDensity = 'compact' | 'standard' | 'detailed'
export const agentDensity = ref<AgentDensity>(
  (localStorage.getItem('agentDensity') as AgentDensity) || 'standard',
)
export function setAgentDensity(d: AgentDensity) {
  agentDensity.value = d
  localStorage.setItem('agentDensity', d)
}

export interface AgentTodoItem {
  content: string
  status: 'pending' | 'in_progress' | 'done'
}

export interface AgentSkill {
  id: string
  name: string
  desc: string
  prompt: string
}

export interface AgentPendingMsg {
  sessionId: string
  text: string
}

export interface AgentToolCall {
  id: string
  name: string
  arguments: string
}

export interface AgentEvent {
  id: string
  ts: string
  role: 'user' | 'assistant' | 'tool' | 'system'
  kind: 'message' | 'tool_call' | 'tool_result' | 'error'
  content?: string
  toolName?: string
  toolArgs?: string
  toolCallId?: string
  toolCalls?: AgentToolCall[]
  todos?: AgentTodoItem[]
  tokensIn?: number // 回合输入 token(缓存未命中,最终回答事件携带)
  tokensCached?: number // 回合输入 token(缓存命中)
  tokensOut?: number // 回合输出 token
  ok?: boolean
}

export interface AgentSessionMeta {
  id: string
  title: string
  createdAt: string
  updatedAt: string
  archived: boolean
  todos: AgentTodoItem[]
}

export interface AgentStatus {
  enabled: boolean
  model: string
  permMode: 'plan' | 'manual' | 'auto'
  running: boolean
  sessionId: string
  step: number
  maxSteps: number
  pending: AgentPendingMsg | null
  activeProfileId: string
  activeProfile: string
  language: string
  ctxUsed?: number // 当前上下文占用(最近一次 LLM 调用 prompt+completion)
  ctxWindow?: number // 模型上下文窗口上限
}

// ==================== 状态 ====================

// 事件窗口: 前端只保留当前会话的尾部窗口(分页懒加载,向上翻页回源后端)
const PAGE_SIZE = 50
const MEM_CAP = 500 // 前端内存事件上限(超限裁剪头部,更早的靠"加载更多"回源)

const status = ref<AgentStatus>({
  enabled: false, model: '', permMode: 'manual',
  running: false, sessionId: '', step: 0, maxSteps: 30,
  pending: null, activeProfileId: '', activeProfile: '', language: 'zh-CN',
})
const sessions = ref<AgentSessionMeta[]>([])
const activeId = ref('')
const events = ref<AgentEvent[]>([])
const headOffset = ref(0) // 窗口首条事件的全量下标(0 = 已到最早)
const totalEvents = ref(0)
const todos = ref<AgentTodoItem[]>([])
const skills = ref<AgentSkill[]>([]) // 内置技能库(只读)
const sessionSkills = ref<string[]>([]) // 当前会话已选技能 ID
const pendingMsg = ref<AgentPendingMsg | null>(null) // 挂起消息(容量1)
const streaming = ref('') // 流式增量缓冲(最终事件落盘后清空替换)
const agentError = ref('')

let started = false
let loadingEvents = false

// ==================== 工具函数 ====================

function parseEvt(evt: any): any {
  try { return JSON.parse(evt.data) } catch { return null }
}

const activeSession = computed(() => sessions.value.find(s => s.id === activeId.value) || null)
const runningElsewhere = computed(() => status.value.running && status.value.sessionId && status.value.sessionId !== activeId.value)

// ==================== 事件订阅 ====================

/** initAgentBridge 初始化(应用启动时调用一次;重复调用幂等)。 */
async function initAgentBridge() {
  if (started) return
  started = true

  try {
    const s = JSON.parse(await GetAgentStatus())
    Object.assign(status.value, s)
    pendingMsg.value = s?.pending ?? null
  } catch {}
  await refreshSessions()
  await refreshSkills()
  // 启动自动进入空会话(防抖: 最新会话为空则复用,否则新建);失败兜底打开最近会话
  const res = await newSession('', true)
  if (!res.ok) {
    const first = sessions.value.find(s => !s.archived)
    if (first) await switchSession(first.id, false)
  }

  // 持久化事件落盘推送: 当前会话 → 追加窗口;其他会话 → 仅刷新列表时间戳
  Events.On('agent-event', async (evt: any) => {
    const info = parseEvt(evt)
    if (!info?.sessionId || !info?.event) return
    const ev: AgentEvent = info.event
    if (info.sessionId === activeId.value) {
      events.value.push(ev)
      totalEvents.value++
      if (events.value.length > MEM_CAP) {
        const cut = events.value.length - MEM_CAP
        events.value = events.value.slice(cut)
        headOffset.value += cut
      }
      // 流式缓冲已被最终事件取代
      if (ev.role === 'assistant') streaming.value = ''
      // todo 更新 → 刷新清单
      if (ev.kind === 'tool_result' && ev.toolName === 'update_todo' && ev.ok) {
        await refreshTodos()
      }
    }
    refreshSessions().catch(() => {})
  })

  // 流式增量: 仅当前运行会话实时显示(不持久化)
  Events.On('agent-stream', (evt: any) => {
    const info = parseEvt(evt)
    if (!info?.sessionId || info.sessionId !== activeId.value) return
    streaming.value += String(info.delta || '')
  })

  // 状态变更(运行/空闲/步数/挂起)
  Events.On('agent-status-changed', (evt: any) => {
    const s = parseEvt(evt)
    if (s) {
      const wasRunning = status.value.running
      Object.assign(status.value, s)
      pendingMsg.value = s.pending ?? null
      // 运行结束 → 清空流式缓冲(最终事件已落盘)
      if (wasRunning && !s.running) streaming.value = ''
    }
  })

  // 挂起消息变更(挂起/消费/丢弃)
  Events.On('agent-pending-changed', (evt: any) => {
    const p = parseEvt(evt)
    pendingMsg.value = p ?? null
  })

  // 事件持久化失败等错误
  Events.On('agent-error', (evt: any) => {
    const info = parseEvt(evt)
    if (info?.message) agentError.value = String(info.message)
  })
}

// ==================== 会话管理 ====================

async function refreshSessions() {
  try {
    const list = JSON.parse(await AgentSessionList())
    if (Array.isArray(list)) sessions.value = list
  } catch {}
}

async function refreshTodos() {
  if (!activeId.value) { todos.value = []; return }
  try {
    const list = JSON.parse(await AgentSessionTodos(activeId.value))
    todos.value = Array.isArray(list) ? list : []
  } catch { todos.value = [] }
}

/** refreshSkills 加载内置技能库(只读,缓存一次)。 */
async function refreshSkills() {
  if (skills.value.length) return
  try {
    const list = JSON.parse(await GetAgentSkills())
    if (Array.isArray(list)) skills.value = list
  } catch { /* ignore */ }
}

/** refreshSessionSkills 加载当前会话已选技能。 */
async function refreshSessionSkills() {
  if (!activeId.value) { sessionSkills.value = []; return }
  try {
    const list = JSON.parse(await AgentSessionSkills(activeId.value))
    sessionSkills.value = Array.isArray(list) ? list : []
  } catch { sessionSkills.value = [] }
}

/** setSessionSkills 设置会话技能(后端校验:仅内置 ID,上限 5)。 */
async function setSessionSkills(ids: string[]): Promise<string> {
  if (!activeId.value) return ''
  try {
    const raw = JSON.parse(await AgentSetSessionSkills(activeId.value, JSON.stringify(ids)))
    if (raw?.error) return String(raw.error)
    sessionSkills.value = ids
    return ''
  } catch (e: any) { return String(e?.message || e) }
}

/**
 * switchSession 切换会话。
 * force=false 时若目标会话正在执行 → 返回 'confirm-needed',由 UI 弹窗确认(强中断)后带 force 重试。
 */
async function switchSession(id: string, force: boolean): Promise<'ok' | 'confirm-needed' | 'error'> {
  if (id === activeId.value) return 'ok'
  // 离开执行中的会话必须显式确认(无"记住选择")
  if (!force && status.value.running && status.value.sessionId === activeId.value && activeId.value !== '') {
    return 'confirm-needed'
  }
  if (!force && status.value.running && status.value.sessionId === id) {
    // 目标会话在跑(后台接收事件): 直接切换无需中断
  }
  // 离开旧会话若它在执行 → 先强中断
  if (force && status.value.running && status.value.sessionId === activeId.value && activeId.value !== '' && activeId.value !== id) {
    try { await AgentInterrupt() } catch {}
  }
  activeId.value = id
  streaming.value = ''
  await loadEventsTail(id)
  await refreshTodos()
  await refreshSessionSkills()
  return 'ok'
}

/** loadEventsTail 加载当前会话尾部窗口(最近 PAGE_SIZE 条)。 */
async function loadEventsTail(id: string) {
  loadingEvents = true
  try {
    const raw = JSON.parse(await AgentSessionEvents(id, -PAGE_SIZE, PAGE_SIZE))
    events.value = Array.isArray(raw.events) ? raw.events : []
    totalEvents.value = Number(raw.total) || events.value.length
    // offset = 窗口首条的全量下标
    headOffset.value = Number(raw.offset) || 0
  } catch {
    events.value = []
    totalEvents.value = 0
    headOffset.value = 0
  } finally { loadingEvents = false }
}

/** loadMoreEvents 向上翻页(懒加载更早的事件);到顶返回 false。 */
async function loadMoreEvents(): Promise<boolean> {
  if (!activeId.value || loadingEvents || headOffset.value <= 0) return false
  loadingEvents = true
  try {
    const raw = JSON.parse(await AgentSessionEvents(activeId.value, Math.max(0, headOffset.value - PAGE_SIZE), PAGE_SIZE))
    const older: AgentEvent[] = Array.isArray(raw.events) ? raw.events : []
    if (older.length === 0) return false
    events.value = [...older, ...events.value]
    headOffset.value = Number(raw.offset) || 0
    return true
  } catch { return false } finally { loadingEvents = false }
}

/** newSession 新建会话;若当前会话执行中 → 'confirm-needed'。 */
async function newSession(title: string, force: boolean): Promise<{ ok: boolean; id?: string; confirmNeeded?: boolean; error?: string }> {
  if (!force && status.value.running && status.value.sessionId === activeId.value) {
    return { ok: false, confirmNeeded: true }
  }
  if (force && status.value.running && status.value.sessionId === activeId.value) {
    try { await AgentInterrupt() } catch {}
  }
  try {
    const meta = JSON.parse(await AgentNewSession(title || ''))
    if (meta?.error) return { ok: false, error: String(meta.error) }
    await refreshSessions()
    if (meta?.id) {
      activeId.value = meta.id
      events.value = []
      totalEvents.value = 0
      headOffset.value = 0
      todos.value = []
      streaming.value = ''
    }
    return { ok: true, id: meta?.id }
  } catch (e: any) {
    return { ok: false, error: String(e?.message || e) }
  }
}

/** send 发送消息。同会话执行中 → 后端挂起(容量1,新覆盖旧);跨会话执行中被拒绝。 */
async function send(text: string): Promise<{ ok: boolean; pending?: boolean; error?: string }> {
  if (!activeId.value) return { ok: false, error: 'no active session' }
  if (!status.value.running) streaming.value = ''
  try {
    const raw = JSON.parse(await AgentSend(activeId.value, text))
    if (raw?.error) return { ok: false, error: String(raw.error) }
    return { ok: true, pending: !!raw?.pending }
  } catch (e: any) {
    return { ok: false, error: String(e?.message || e) }
  }
}

/** interrupt 强中断当前运行轮次(同时清空挂起)。 */
async function interrupt() {
  try { await AgentInterrupt() } catch {}
  streaming.value = ''
}

/** pendingFlush 打断式立即发送挂起消息(取消当前轮次,轮次收尾自动开新轮)。 */
async function pendingFlush() {
  try { await AgentPendingFlush() } catch {}
}

/** pendingDiscard 丢弃挂起消息。 */
async function pendingDiscard() {
  try { await AgentPendingDiscard() } catch {}
}

/** regenerate 刷新对话:移除末尾助手回答(含工具链)后重跑;执行中调用 = 中断后重跑。 */
async function regenerate(): Promise<{ ok: boolean; error?: string }> {
  if (!activeId.value) return { ok: false }
  streaming.value = ''
  try {
    const raw = JSON.parse(await AgentRegenerate(activeId.value))
    if (raw?.error) return { ok: false, error: String(raw.error) }
    return { ok: true }
  } catch (e: any) {
    return { ok: false, error: String(e?.message || e) }
  }
}

async function renameSession(id: string, title: string) {
  try {
    await AgentRenameSession(id, title)
    await refreshSessions()
  } catch {}
}

async function archiveSession(id: string, archived: boolean): Promise<string> {
  try {
    const raw = JSON.parse(await AgentArchiveSession(id, archived))
    if (raw?.error) return String(raw.error)
    await refreshSessions()
    return ''
  } catch (e: any) { return String(e?.message || e) }
}

async function deleteSession(id: string): Promise<string> {
  try {
    const raw = JSON.parse(await AgentDeleteSession(id))
    if (raw?.error) return String(raw.error)
    if (activeId.value === id) {
      activeId.value = ''
      events.value = []
      todos.value = []
      totalEvents.value = 0
      headOffset.value = 0
    }
    await refreshSessions()
    // 当前会话被删 → 切到最近会话
    const first = sessions.value.find(s => !s.archived)
    if (first && !activeId.value) await switchSession(first.id, true)
    return ''
  } catch (e: any) { return String(e?.message || e) }
}

/** dismissError 清除错误提示。 */
function dismissError() { agentError.value = '' }

/** refreshStatus 拉取最新智能体状态(设置保存后调用)。 */
async function refreshStatus() {
  try {
    const s = JSON.parse(await GetAgentStatus())
    Object.assign(status.value, s)
    pendingMsg.value = s.pending ?? null
  } catch {}
}

export function useAgentBridge() {
  return {
    // 状态
    status, sessions, activeId, activeSession, events, headOffset, totalEvents,
    todos, streaming, agentError, runningElsewhere,
    skills, sessionSkills, pendingMsg,
    // 生命周期
    initAgentBridge, refreshSessions, refreshStatus,
    // 会话操作
    switchSession, newSession, deleteSession, renameSession, archiveSession,
    // 对话
    send, interrupt, regenerate, pendingFlush, pendingDiscard,
    // 技能
    refreshSkills, refreshSessionSkills, setSessionSkills,
    // 分页
    loadMoreEvents,
    // 错误
    dismissError,
  }
}
