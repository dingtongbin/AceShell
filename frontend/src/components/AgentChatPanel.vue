<script setup lang="ts">
// 智能体聊天面板(右侧停靠,独立于左侧面板,可最大化)。
// - 双排头: 标题/状态 + 会话选择
// - 会话管理: 切换/新建/重命名/归档/删除(离开执行中会话需确认,无记住选项)
// - 事件窗口: 分页懒加载(顶部"加载更多"),流式增量实时显示
// - 待办清单: 悬浮卡片(可折叠)
// - 挂起队列: 执行中同会话新消息挂起(容量1),支持立即发送(打断式)/丢弃
// - 底部工具栏: 技能选择 / 权限模式 / 模型切换 / AI 服务切换 / 设置
import { ref, reactive, computed, watch, nextTick, h, onBeforeUnmount, onMounted } from 'vue'
import {
  NIcon, NButton, NTag, NScrollbar, NInput, NTooltip, NDropdown, NModal, NPopselect, NPopover,
  useMessage, useDialog,
} from 'naive-ui'
import {
  CloseOutline, AddOutline, SettingsOutline, ChatbubbleEllipsesOutline,
  ChevronUpOutline, ListOutline, TrashOutline,
  ArchiveOutline, ArchiveOutline as UnarchiveOutline, CreateOutline,
  DocumentTextOutline, RefreshOutline, FlashOutline, CloseCircleOutline, ExtensionPuzzleOutline,
  ShieldHalfOutline, ServerOutline, ExpandOutline, ContractOutline,
  RocketOutline, TimeOutline, ShieldCheckmarkOutline, ChevronDownOutline, SparklesOutline,
  ChevronForwardOutline, ChevronBackOutline, CopyOutline,
  ArrowUp, StopSharp,
} from '@vicons/ionicons5'
import { useI18n } from 'vue-i18n'
import { useAgentBridge, agentDensity } from '../composables/useAgentBridge'
import { useMcpBridge } from '../composables/useMcpBridge'
import { renderMd } from '../utils/markdown'
import { ExportSessionPdf, AgentListModels } from '../../bindings/changeme/internal/services/agentservice.js'
import { AgentCfg, AgentProfilesGet, AgentProfilesSet, AgentSetActiveProfile, SetAgentBehavior } from '../../bindings/changeme/internal/services/configservice.js'

defineProps<{ width: number; maximized?: boolean }>()
const emit = defineEmits<{
  (e: 'close'): void
  (e: 'open-settings'): void
  (e: 'open-mcp-settings'): void
  (e: 'toggle-maximize'): void
}>()

const { t, locale } = useI18n()
const message = useMessage()
const dialog = useDialog()

const {
  status, sessions, activeId, activeSession, events, headOffset, totalEvents,
  todos, streaming, agentError, skills, sessionSkills, pendingMsg,
  switchSession, newSession, deleteSession, renameSession, archiveSession,
  send, interrupt, regenerate, pendingFlush, pendingDiscard,
  setSessionSkills, loadMoreEvents, dismissError, refreshStatus,
} = useAgentBridge()

const sending = ref(false)
const autoScroll = ref(true)
const todoExpanded = ref(true)
const listRef = ref<any>(null)

// ==================== 底部工具栏: 供应商+模型(合并二级选择器) ====================

interface ProfileItem { id: string; name: string; model: string; hasKey: boolean; customModels?: string[] }
const profiles = ref<ProfileItem[]>([])
const modelList = ref<string[]>([])
const loadingModels = ref(false)
const fetchError = ref('') // 自动获取失败原因(面板内联展示,不弹 toast)
// 两级选择器: 一级=供应商列表(底部固定"管理模型"),二级=该供应商模型列表(下钻)
const showProfileModel = ref(false)
const pmDrillId = ref('') // 已下钻的供应商 id;空=停留在一级列表
const pmPanelRef = ref<HTMLElement | null>(null)
const pmBtnRef = ref<HTMLElement | null>(null)
// fixed 定位坐标+高度上限: 弹层脱离 composer(overflow:hidden)裁剪,且不越出可视区
const pmStyle = ref<{ left: string; bottom: string; maxHeight: string }>({ left: '0px', bottom: '0px', maxHeight: 'none' })

async function refreshProfiles() {
  try {
    const raw = JSON.parse(await AgentProfilesGet())
    profiles.value = Array.isArray(raw?.profiles) ? raw.profiles : []
  } catch { /* ignore */ }
}

async function fetchModels(profileID = '') {
  if (loadingModels.value) return
  loadingModels.value = true
  fetchError.value = ''
  try {
    const raw = JSON.parse(await AgentListModels('', '', profileID))
    if (raw?.error) { fetchError.value = String(raw.error); modelList.value = [] }
    else modelList.value = Array.isArray(raw) ? raw : []
  } catch (e: any) {
    fetchError.value = String(e?.message || e)
    modelList.value = []
  } finally { loadingModels.value = false }
}

// 按钮文案: 供应商:模型
const pmBtnLabel = computed(() => {
  const p = status.value.activeProfile || t('agent.profilePick')
  const m = status.value.model || t('agent.modelPick')
  return `${p}:${m}`
})

// 自适应弹层定位: 向上弹出,水平/垂直均钳制在可视区内(左侧不越界,高度不超按钮上方可用空间)
function popupStyleAbove(rect: DOMRect, panelWidth: number): { left: string; bottom: string; maxHeight: string } {
  const vw = window.innerWidth
  const left = Math.min(Math.max(8, rect.left), Math.max(8, vw - panelWidth - 8))
  const bottom = window.innerHeight - rect.top + 6
  const maxHeight = Math.max(80, rect.top - 14)
  return { left: `${left}px`, bottom: `${bottom}px`, maxHeight: `${maxHeight}px` }
}

function toggleProfileModel() {
  if (showProfileModel.value) { closeProfileModel(); return }
  const rect = pmBtnRef.value?.getBoundingClientRect()
  if (rect) pmStyle.value = popupStyleAbove(rect, 260)
  showProfileModel.value = true
  pmDrillId.value = '' // 始终从一级供应商列表开始
}

function closeProfileModel() {
  showProfileModel.value = false
}

// 下钻的供应商对象(二级标题用)
const drilledProfile = computed(() => profiles.value.find(p => p.id === pmDrillId.value))

// 点击供应商条目: 仅下钻展示其模型列表(不切换活动档案,点外部关闭=零变更)
async function onPickProfile(p: ProfileItem) {
  pmDrillId.value = p.id
  await fetchModels(p.id)
}

// 点击模型: 切换档案(若下钻的不是当前档案)+模型,然后关闭
async function onPickModel(model: string) {
  const pid = pmDrillId.value
  const target = profiles.value.find(p => p.id === pid)
  closeProfileModel()
  if (pid && pid !== status.value.activeProfileId) await handleProfileChange(pid)
  if (!target || target.model !== model) await handleModelChange(model)
}

// 底部固定项: 管理模型 → 打开智能体设置
function onManageModels() {
  closeProfileModel()
  emit('open-settings')
}

// 点击面板/按钮外部: 关闭且不切换(同时服务供应商/权限模式/加号/上下文四个面板)
function onPmDocClick(e: MouseEvent) {
  const path = e.composedPath?.() ?? []
  if (showProfileModel.value) {
    const inPanel = pmPanelRef.value && path.includes(pmPanelRef.value)
    const inBtn = pmBtnRef.value && path.includes(pmBtnRef.value)
    if (!inPanel && !inBtn) closeProfileModel()
  }
  if (showPermPanel.value) {
    const inPanel = permPanelRef.value && path.includes(permPanelRef.value)
    const inBtn = permBtnRef.value && path.includes(permBtnRef.value)
    if (!inPanel && !inBtn) showPermPanel.value = false
  }
  if (showPlusMenu.value) {
    const inPanel = plusPanelRef.value && path.includes(plusPanelRef.value)
    const inBtn = plusBtnRef.value && path.includes(plusBtnRef.value)
    if (!inPanel && !inBtn) showPlusMenu.value = false
  }
  if (showCtxPanel.value) {
    const inPanel = ctxPanelRef.value && path.includes(ctxPanelRef.value)
    const inBtn = ctxBtnRef.value && path.includes(ctxBtnRef.value)
    if (!inPanel && !inBtn) showCtxPanel.value = false
  }
}

onMounted(() => {
  document.addEventListener('click', onPmDocClick, true)
  window.addEventListener('pointerup', onWindowPointerUp)
  nextTick(() => setupFollowObserver()) // 滚动容器就绪后绑定吸底观察器
})
onBeforeUnmount(() => {
  document.removeEventListener('click', onPmDocClick, true)
  window.removeEventListener('pointerup', onWindowPointerUp)
  contentObserver?.disconnect()
  cancelAnimationFrame(followRaf)
  if (sendAtTimer !== undefined) clearTimeout(sendAtTimer)
  stopTick()
})

/** 切换模型 = 更新活动档案的 model(apiKey 留空保留原密文)。 */
async function handleModelChange(model: string) {
  try {
    const raw = JSON.parse(await AgentProfilesGet())
    const list: any[] = Array.isArray(raw?.profiles) ? raw.profiles : []
    for (const p of list) {
      if (p.id === raw.activeProfileId) p.model = model
    }
    await AgentProfilesSet(JSON.stringify({ activeProfileId: raw.activeProfileId, profiles: list }))
    await refreshStatus()
    message.success(t('agent.modelSwitched', { model }))
  } catch (e: any) {
    message.error(String(e?.message || e))
  }
}

async function handleProfileChange(id: string) {
  try {
    const raw = JSON.parse(await AgentSetActiveProfile(id))
    if (raw?.error) { message.error(String(raw.error)); return }
    await refreshStatus()
  } catch (e: any) {
    message.error(String(e?.message || e))
  }
}

// ==================== 底部工具栏: 权限模式(自绘点击面板,与供应商选择器同款) ====================

const showPermPanel = ref(false)
const permBtnRef = ref<HTMLElement | null>(null)
const permPanelRef = ref<HTMLElement | null>(null)
const permStyle = ref<{ left: string; bottom: string; maxHeight: string }>({ left: '0px', bottom: '0px', maxHeight: 'none' })

function togglePermPanel() {
  if (showPermPanel.value) { showPermPanel.value = false; return }
  const rect = permBtnRef.value?.getBoundingClientRect()
  if (rect) permStyle.value = popupStyleAbove(rect, 240)
  showPermPanel.value = true
}

async function onPickPerm(mode: string) {
  showPermPanel.value = false
  if (mode !== status.value.permMode) await handlePermChange(mode)
}

const permOptions = computed(() => [
  { key: 'plan', label: t('agent.permPlan'), icon: DocumentTextOutline, desc: t('agent.permPlanDesc') },
  { key: 'manual', label: t('agent.permManual'), icon: ShieldCheckmarkOutline, desc: t('agent.permManualDesc') },
  { key: 'auto', label: t('agent.permAuto'), icon: FlashOutline, desc: t('agent.permAutoDesc'), warn: true },
])

// 按钮图标随当前模式切换(与下拉选项图标一致)
const permBtnIcon = computed(() => {
  const opt = permOptions.value.find(o => o.key === status.value.permMode)
  return opt?.icon ?? ShieldHalfOutline
})

/** 快切权限模式: 先读当前行为参数再整体保存,避免覆盖其它字段。 */
async function handlePermChange(mode: string) {
  try {
    const cfg = await AgentCfg()
    const raw = await SetAgentBehavior(mode, cfg.maxSteps, cfg.historyWindow, cfg.contextMaxEvents)
    const err = raw ? JSON.parse(raw) : null
    if (err?.error) { message.error(String(err.error)); return }
    await refreshStatus()
  } catch (e: any) {
    message.error(String(e?.message || e))
  }
}

// ==================== 底部工具栏: 加号菜单(占位) ====================

const showPlusMenu = ref(false)
const plusBtnRef = ref<HTMLElement | null>(null)
const plusPanelRef = ref<HTMLElement | null>(null)
const plusStyle = ref<{ left: string; bottom: string; maxHeight: string }>({ left: '0px', bottom: '0px', maxHeight: 'none' })

function togglePlusMenu() {
  if (showPlusMenu.value) { showPlusMenu.value = false; return }
  const rect = plusBtnRef.value?.getBoundingClientRect()
  if (rect) plusStyle.value = popupStyleAbove(rect, 220)
  showPlusMenu.value = true
}

// ==================== 底部工具栏: 上下文用量圆圈(真实 AI 上下文) ====================

const showCtxPanel = ref(false)
const ctxBtnRef = ref<HTMLElement | null>(null)
const ctxPanelRef = ref<HTMLElement | null>(null)
const ctxStyle = ref<{ left: string; bottom: string; maxHeight: string }>({ left: '0px', bottom: '0px', maxHeight: 'none' })

// 真实上下文占用 = 最近一次 LLM 调用的 prompt+completion token(后端 statusMap 提供)
const ctxUsed = computed(() => status.value.ctxUsed || 0)
const ctxWindow = computed(() => status.value.ctxWindow || 128000)
const ctxPercent = computed(() => Math.min(100, Math.round((ctxUsed.value / ctxWindow.value) * 100)))
// 圆环参数: 半径 7 → 周长 ≈ 43.98
const CTX_RING_LEN = 2 * Math.PI * 7
const ctxRingColor = computed(() => {
  const p = ctxPercent.value
  if (p >= 85) return '#e45858'
  if (p >= 60) return '#e2a03f'
  return '#4ec9b0'
})
// 用量文案: 千分位 + K/M 缩写
function fmtTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 10_000) return `${Math.round(n / 1000)}K`
  return n.toLocaleString()
}

function toggleCtxPanel() {
  if (showCtxPanel.value) { showCtxPanel.value = false; return }
  const rect = ctxBtnRef.value?.getBoundingClientRect()
  if (rect) ctxStyle.value = popupStyleAbove(rect, 220)
  showCtxPanel.value = true
}

// ==================== 底部工具栏: 技能 ====================

const skillOptions = computed(() => skills.value.map(s => ({
  key: s.id,
  label: s.name,
  desc: s.desc,
  checked: sessionSkills.value.includes(s.id),
})))

// 组合输入框聚焦态(边框高亮)
const composerFocused = ref(false)

// contenteditable 行内 chip 编辑器: chip 嵌入文本流,光标处插入
const editorRef = ref<HTMLDivElement | null>(null)

// 编辑器纯文本(chip 节点跳过,用于发送按钮禁用判断)
const editorText = ref('')

// 遍历编辑器提取纯文本: chip 占位一个空格,<br> 转换行
function extractEditorText(): string {
  const el = editorRef.value
  if (!el) return ''
  let out = ''
  const walk = (node: Node) => {
    node.childNodes.forEach(ch => {
      const he = ch as HTMLElement
      if (he.classList?.contains('composer-chip')) {
        out += ' '
      } else if (ch.nodeType === Node.TEXT_NODE) {
        out += ch.textContent || ''
      } else if (he.tagName === 'BR') {
        out += '\n'
      } else {
        walk(ch)
      }
    })
  }
  walk(el)
  return out.replace(/\u00a0/g, ' ').trim()
}

function onEditorInput() {
  editorText.value = extractEditorText()
}

// 点击 chip 上的 ×: 事件委托删除
function onEditorClick(e: MouseEvent) {
  const target = e.target as HTMLElement
  if (!target.classList?.contains('chip-x')) return
  e.preventDefault()
  e.stopPropagation()
  const chip = target.closest('.composer-chip') as HTMLElement | null
  const id = chip?.dataset?.skillId
  if (chip && id) {
    chip.remove()
    onEditorInput()
    void removeSkill(id)
  }
}

// 在光标处插入行内 chip(未聚焦则追加末尾)
function insertChipAtCursor(id: string, name: string) {
  const el = editorRef.value
  if (!el) return
  el.focus()
  const chip = document.createElement('span')
  chip.contentEditable = 'false'
  chip.className = 'composer-chip'
  chip.dataset.skillId = id
  const ic = document.createElement('span')
  ic.className = 'chip-ic'
  ic.textContent = '🧩'
  const label = document.createElement('span')
  label.className = 'chip-name'
  label.textContent = name
  const x = document.createElement('span')
  x.className = 'chip-x'
  x.textContent = '×'
  chip.append(ic, label, x)

  const sel = window.getSelection()
  if (sel && sel.rangeCount && el.contains(sel.anchorNode)) {
    const range = sel.getRangeAt(0)
    range.deleteContents()
    range.insertNode(chip)
    const sp = document.createTextNode('\u00a0')
    chip.after(sp)
    range.setStartAfter(sp)
    range.collapse(true)
    sel.removeAllRanges()
    sel.addRange(range)
  } else {
    el.appendChild(chip)
    el.appendChild(document.createTextNode('\u00a0'))
    // 光标移到末尾
    const range = document.createRange()
    range.selectNodeContents(el)
    range.collapse(false)
    sel?.removeAllRanges()
    sel?.addRange(range)
  }
  onEditorInput()
}

// 移除编辑器中指定技能的 chip
function removeChipDom(id: string) {
  editorRef.value?.querySelector(`.composer-chip[data-skill-id="${id}"]`)?.remove()
  onEditorInput()
}

async function addSkill(id: string) {
  const cur = [...sessionSkills.value]
  if (cur.length >= 5) { message.warning(t('agent.skillsMax')); return }
  cur.push(id)
  const sk = skills.value.find(s => s.id === id)
  if (sk) insertChipAtCursor(id, sk.name)
  const err = await setSessionSkills(cur)
  if (err) { removeChipDom(id); message.error(err) }
}

async function removeSkill(id: string) {
  const cur = sessionSkills.value.filter(s => s !== id)
  removeChipDom(id)
  const err = await setSessionSkills(cur)
  if (err) message.error(err)
}

async function handleSkillToggle(id: string) {
  if (sessionSkills.value.includes(id)) await removeSkill(id)
  else await addSkill(id)
}

// ==================== 派生状态 ====================

const activeTitle = computed(() => activeSession.value?.title || t('agent.selectSession'))

// AI 消息头部标签: 服务名 · 模型名
const aiLabel = computed(() => {
  const parts: string[] = []
  if (status.value.activeProfile) parts.push(status.value.activeProfile)
  if (status.value.model) parts.push(status.value.model)
  return parts.length ? parts.join(' · ') : 'AI'
})

const hasMore = computed(() => headOffset.value > 0)
const runningHere = computed(() => status.value.running && status.value.sessionId === activeId.value)
const pendingHere = computed(() => !!pendingMsg.value && pendingMsg.value.sessionId === activeId.value)

// ==================== 回合实时计时 ====================
// 发送即进入任务: 乐观占位回合 + 耗时实时跳动(每 500ms 重算,仅执行中运行)
const nowTick = ref(Date.now())
const sendAt = ref(0) // 最近一次成功发送(非挂起)时刻;0=无
let tickTimer: number | undefined
function startTick() {
  nowTick.value = Date.now()
  if (tickTimer === undefined) {
    tickTimer = window.setInterval(() => { nowTick.value = Date.now() }, 500)
  }
}
function stopTick() {
  if (tickTimer !== undefined) {
    clearInterval(tickTimer)
    tickTimer = undefined
  }
}
let sendAtTimer: number | undefined
/** 发送/重跑/挂起立即发送成功 → 记录时刻并开表(乐观回合立即出现并计时)。 */
function markSendAt() {
  sendAt.value = Date.now()
  startTick()
  // 21s 兜底: 若始终未进入运行态(如瞬时出错且无事件),清乐观标记并停表防空转
  if (sendAtTimer !== undefined) clearTimeout(sendAtTimer)
  sendAtTimer = window.setTimeout(() => {
    sendAtTimer = undefined
    if (!runningHere.value) { sendAt.value = 0; stopTick() }
  }, 21_000)
}
// 执行结束/会话切换 → 停表并清乐观标记(回合最终耗时改由事件时间戳计算)
watch(runningHere, v => {
  if (v) startTick()
  else {
    // 挂起打断立即发送时 running 会瞬时翻 false 再翻 true:
    // 3s 内刚设置的发送时刻不清除(新轮即将开始)
    if (!(sendAt.value > 0 && Date.now() - sendAt.value < 3000)) {
      stopTick()
      sendAt.value = 0
    }
  }
})
watch(activeId, () => { stopTick(); sendAt.value = 0 })

// ==================== MCP 审批条(输入框上方,与 MCP 设置弹窗同源同步) ====================

const { pendingApprovals: mcpApprovals, approveApproval: mcpApprove, denyApproval: mcpDeny } = useMcpBridge()
const approvalExpanded = ref<Record<string, boolean>>({})
const approvalCountdown = ref<Record<string, number>>({})
let approvalTimer: number | undefined

// 每秒刷新倒计时(30s 超时自动拒绝,后端兜底;前端仅展示)
watch(() => mcpApprovals.value.length, () => {
  if (approvalTimer !== undefined) return
  approvalTimer = window.setInterval(() => {
    const next: Record<string, number> = {}
    const now = Date.now()
    for (const a of mcpApprovals.value) {
      const left = Math.max(0, Math.ceil((new Date(a.expiresAt).getTime() - now) / 1000))
      next[a.id] = left
    }
    approvalCountdown.value = next
    if (mcpApprovals.value.length === 0 && approvalTimer !== undefined) {
      window.clearInterval(approvalTimer)
      approvalTimer = undefined
    }
  }, 1000)
}, { immediate: true })

function toggleApprovalDetail(id: string) {
  approvalExpanded.value[id] = !approvalExpanded.value[id]
}

function handleMcpApprove(id: string, permanent: boolean) {
  mcpApprove(id, permanent)
  message.success(permanent ? t('agent.approvedPermanent') : t('agent.approvedOnce'))
}

function handleMcpDeny(id: string) {
  mcpDeny(id)
  message.info(t('agent.denied'))
}

onBeforeUnmount(() => {
  if (approvalTimer !== undefined) {
    window.clearInterval(approvalTimer)
    approvalTimer = undefined
  }
})

// ==================== 滚动(吸底跟随) ====================

const listContentRef = ref<HTMLElement | null>(null) // .agent-list(高度随内容增长)

// 滚动容器与内容元素(n-scrollbar 结构: wrapper > container > content)
function scrollEls(): { box: HTMLElement | null; content: HTMLElement | null } {
  const root = listRef.value?.$el as HTMLElement | undefined
  const box = root?.querySelector('.n-scrollbar-container') as HTMLElement | null
  return { box, content: (box?.querySelector('.n-scrollbar-content') as HTMLElement | null) || box }
}

// 吸底滚动: nextTick(DOM 更新) + rAF(下一帧,覆盖渲染后高度变化) 双重保障
async function scrollToBottom() {
  if (!autoScroll.value) return
  await nextTick()
  await new Promise(r => requestAnimationFrame(() => r(null)))
  const { box } = scrollEls()
  if (box) box.scrollTop = box.scrollHeight
}

// 内容高度变化(流式追加/事件落盘/图片代码块撑高)且在底部 → 自动跟随
// ResizeObserver 覆盖所有高度增长场景,包括异步渲染的高度突变
let contentObserver: ResizeObserver | null = null
let followRaf = 0
let suspendFollow = false // 懒加载头部插入时暂停吸底
function setupFollowObserver() {
  if (contentObserver) contentObserver.disconnect()
  const content = listContentRef.value
  if (!content || typeof ResizeObserver === 'undefined') return
  contentObserver = new ResizeObserver(() => {
    if (!autoScroll.value || suspendFollow) return
    cancelAnimationFrame(followRaf)
    followRaf = requestAnimationFrame(() => {
      const { box } = scrollEls()
      if (box && autoScroll.value && !suspendFollow) box.scrollTop = box.scrollHeight
    })
  })
  contentObserver.observe(content)
}

watch(() => events.value.length, () => { if (autoScroll.value) scrollToBottom() })
watch(streaming, () => { if (autoScroll.value) scrollToBottom() })
// 会话切换后容器重建,重新绑定观察器并强制吸底
watch(activeId, async () => {
  autoScroll.value = true
  await scrollToBottom()
  setupFollowObserver()
})

// 吸底判定采用"显式上翻"策略,杜绝竞态误判:
// 程序吸底设置的 scrollTop 也会异步触发 scroll 事件,若在事件派发前内容又增长,
// 按距底距离判定会把程序滚动误判为用户上翻 → autoScroll 永久关闭(吸底失效根因)。
// 因此: scroll 事件只在"已在底部"时恢复跟随;上翻停止跟随由 wheel(向上)与滚动条拖拽显式判定。
let railDrag = false // 用户拖拽自定义滚动条中
function onScroll(e: Event) {
  const el = e.target as HTMLElement
  // 内容不足一屏时恒为跟随态(无滚动可言)
  if (el.scrollHeight <= el.clientHeight) { autoScroll.value = true; return }
  const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40
  if (railDrag) autoScroll.value = atBottom
  else if (atBottom) autoScroll.value = true
}
// 滚轮向上 = 用户上翻,立即停止跟随(向下滚回底部由 onScroll 恢复)
function onAreaWheel(e: WheelEvent) {
  if (e.deltaY < 0) autoScroll.value = false
}
// 拖拽自定义滚动条(rail)期间按位置实时判定
function onAreaPointerDown(e: PointerEvent) {
  const target = e.target as HTMLElement
  if (target?.closest?.('.n-scrollbar-rail')) railDrag = true
}
function onWindowPointerUp() { railDrag = false }

// ==================== 加载更多(懒加载) ====================

const loadingMore = ref(false)
async function handleLoadMore() {
  if (loadingMore.value || !hasMore.value) return
  loadingMore.value = true
  suspendFollow = true // 头部插入历史事件,暂停吸底(保持视口位置)
  const { box } = scrollEls()
  const prevHeight = box?.scrollHeight || 0
  const ok = await loadMoreEvents()
  if (ok) {
    await nextTick()
    if (box) box.scrollTop = box.scrollHeight - prevHeight // 保持视口位置
  }
  loadingMore.value = false
  requestAnimationFrame(() => { suspendFollow = false })
}

// ==================== 离开确认(无记住选项) ====================

/** confirmLeave 若当前会话执行中 → 弹窗确认强中断;返回 true 表示可以离开。 */
function confirmLeave(actionText: string): Promise<boolean> {
  return new Promise(resolve => {
    if (!runningHere.value) { resolve(true); return }
    dialog.warning({
      title: t('agent.leaveTitle'),
      content: t('agent.leaveConfirm', { action: actionText }),
      positiveText: t('agent.leaveInterrupt'),
      negativeText: t('common.cancel'),
      onPositiveClick: async () => {
        await interrupt()
        resolve(true)
      },
      onNegativeClick: () => resolve(false),
      onClose: () => resolve(false),
      onMaskClick: () => resolve(false),
    })
  })
}

// ==================== 会话操作 ====================

// 历史会话弹窗
const showHistory = ref(false)

async function handleHistoryPick(id: string) {
  showHistory.value = false
  if (id === activeId.value) return
  await handleSwitch(id)
}

async function handleSwitch(id: string) {
  if (id === activeId.value) return
  const res = await switchSession(id, false)
  if (res === 'confirm-needed') {
    const ok = await confirmLeave(t('agent.switchAction'))
    if (ok) await switchSession(id, true)
    return
  }
  autoScroll.value = true
  scrollToBottom()
}

async function handleNewSession() {
  const ok = await confirmLeave(t('agent.newAction'))
  if (!ok) return
  const res = await newSession('', true)
  if (!res.ok && res.error) message.error(res.error)
  autoScroll.value = true
}

// 会话右键/更多菜单
const ctxTarget = ref<string>('')
const ctxShow = ref(false)
const ctxX = ref(0)
const ctxY = ref(0)
const renameTarget = ref('')
const renameText = ref('')
const renameShow = ref(false)

function openSessionMenu(e: MouseEvent, id: string) {
  e.preventDefault()
  ctxTarget.value = id
  ctxX.value = e.clientX
  ctxY.value = e.clientY
  ctxShow.value = true
}

async function handleArchiveToggle() {
  const id = ctxTarget.value
  const s = sessions.value.find(x => x.id === id)
  if (!s) return
  const err = await archiveSession(id, !s.archived)
  if (err) message.error(err)
}

async function handleDeleteSession() {
  const id = ctxTarget.value
  const s = sessions.value.find(x => x.id === id)
  if (!s) return
  dialog.warning({
    title: t('agent.deleteTitle'),
    content: t('agent.deleteConfirm', { title: s.title }),
    positiveText: t('common.delete'),
    negativeText: t('common.cancel'),
    onPositiveClick: async () => {
      const ok = await confirmLeave(t('agent.deleteAction'))
      if (!ok) return
      const err = await deleteSession(id)
      if (err) message.error(err)
    },
  })
}

function openRename() {
  const s = sessions.value.find(x => x.id === ctxTarget.value)
  if (!s) return
  renameTarget.value = s.id
  renameText.value = s.title
  renameShow.value = true
}

// 导出会话 PDF(按界面语言本地化)
const exportingPdf = ref(false)
async function handleExportPdf() {
  const id = ctxTarget.value || activeId.value
  if (!id) return
  exportingPdf.value = true
  try {
    const raw = JSON.parse(await ExportSessionPdf(id, locale.value))
    if (raw?.error) message.error(String(raw.error))
    else if (raw?.path) message.success(t('agent.exportPdfOk', { path: raw.path }))
    // path 为空 = 用户取消,不提示
  } catch (e: any) {
    message.error(String(e?.message || e))
  } finally { exportingPdf.value = false }
}

async function handleRename() {
  if (!renameText.value.trim()) return
  await renameSession(renameTarget.value, renameText.value.trim())
  renameShow.value = false
}

// 刷新对话(重跑末轮;执行中 = 中断后重跑)
async function handleRegenerate() {
  if (!activeId.value) return
  const res = await regenerate()
  if (!res.ok && res.error) message.error(res.error)
  else {
    // 重跑也立即进入任务并开表(占位回合计时起点=此刻,不用旧用户消息时间戳)
    markSendAt()
    autoScroll.value = true
  }
}

// ==================== 发送/中断/挂起 ====================

async function handleSend() {
  const text = extractEditorText()
  if (!text) return
  // 同会话执行中 → 后端挂起(容量1);跨会话执行中 → 后端拒绝
  sending.value = true
  autoScroll.value = true
  const res = await send(text)
  sending.value = false
  if (res.ok) {
    if (editorRef.value) editorRef.value.innerHTML = ''
    editorText.value = ''
    if (res.pending) message.info(t('agent.queuedHint'))
    else markSendAt() // 发送成功即进入任务: 立即开表(不等后端 status 事件),乐观回合立即出现
  } else if (res.error) {
    message.error(res.error)
  }
}

function onInputKeydown(e: KeyboardEvent) {
  // Enter 发送;Shift+Enter 换行(多行输入)
  if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
    e.preventDefault()
    handleSend()
  }
}

// 立即发送挂起消息(打断当前轮,新轮以新用户事件开跑) → 同样立即开表
async function handlePendingFlush() {
  await pendingFlush()
  markSendAt()
}

// ==================== 事件流合并(回合=工单: 执行过程可折叠+结论常显) ====================

// AI 组片段: 文本 / 工具调用(结果按 toolCallId 配对回填) / 错误
interface ToolSeg { kind: 'tool'; id: string; name: string; args?: string; result?: string; ok?: boolean; done: boolean; ts?: number; dur?: number }
interface TextSeg { kind: 'text'; text: string; streaming?: boolean }
interface ErrSeg { kind: 'error'; text: string }
type AiSeg = ToolSeg | TextSeg | ErrSeg

// 回合 token 用量: 缓存未命中输入 / 缓存命中输入 / 输出
interface TurnTokens { in: number; cached: number; out: number }

interface FlowBlock {
  key: string
  type: 'user' | 'ai' | 'system'
  md: string // user/system 原文
  segs: AiSeg[] // ai 组片段(按时间顺序,user/system 恒为空)
  ts?: number // user/system 事件时间戳(乐观回合计时起点)
  firstTs?: number // 回合起始时间(摘要耗时)
  lastTs?: number // 回合最后事件时间
  tokens?: TurnTokens // 回合累计 token 用量(后端最终回答事件携带)
  optimistic?: boolean // 乐观占位回合: 已发送但首个 AI 事件未落盘
}

// 密度三档(共享状态,设置弹窗中配置): 紧凑(历史全收/成功工具仅一行) / 标准 / 详细(不截断)
const density = agentDensity

// 回合折叠状态(用户手动操作记忆,持久到 localStorage;默认策略见 isTurnOpen)
const openTurns = reactive(new Set<string>())
{
  try {
    for (const k of JSON.parse(localStorage.getItem('agentOpenTurns') || '[]')) openTurns.add(k)
  } catch { /* 忽略损坏数据 */ }
}
function toggleTurn(key: string) {
  if (openTurns.has(key)) openTurns.delete(key)
  else openTurns.add(key)
  localStorage.setItem('agentOpenTurns', JSON.stringify([...openTurns]))
}
// 最近用户操作过的回合键(新增回合不视为已操作;容量有界 64)
const touchedTurns = reactive(new Set<string>())
function touchTurn(key: string) {
  touchedTurns.add(key)
  if (touchedTurns.size > 64) {
    const first = touchedTurns.values().next().value
    if (first !== undefined) touchedTurns.delete(first)
  }
}

// tabId → 友好标签映射(open_session 结果注册;terminal 类工具显示目标主机)
const tabLabels = new Map<string, string>()

// 参数解析缓存(args JSON 字符串 → 对象;结果同理,流式重算零开销)
const argsCache = new Map<string, Record<string, unknown> | null>()
function parseJson(s?: string): Record<string, unknown> | null {
  if (!s) return null
  let v = argsCache.get(s)
  if (v === undefined) {
    try { v = JSON.parse(s) as Record<string, unknown> } catch { v = null }
    if (argsCache.size > 600) argsCache.clear()
    argsCache.set(s, v)
  }
  return v
}

function evTs(ev: { ts?: string }): number {
  const t = ev.ts ? new Date(ev.ts).getTime() : NaN
  return Number.isNaN(t) ? Date.now() : t
}

/** flowBlocks 把事件序列重组为渲染块: 用户/系统消息独立,AI 侧连续事件(文本+工具+错误)合并为一个回合。 */
const flowBlocks = computed<FlowBlock[]>(() => {
  const blocks: FlowBlock[] = []
  let ai: FlowBlock | null = null
  const ensureAi = (): FlowBlock => {
    if (!ai) {
      ai = { key: '', type: 'ai', md: '', segs: [] }
      blocks.push(ai)
    }
    return ai
  }
  // 事件携带的 token 用量累计进 AI 回合(最终回答与中断/错误/上限终止事件均携带)
  const accumTokens = (b: FlowBlock, ev: { tokensIn?: number; tokensCached?: number; tokensOut?: number }) => {
    if (ev.tokensIn || ev.tokensCached || ev.tokensOut) {
      b.tokens = {
        in: (b.tokens?.in || 0) + (ev.tokensIn || 0),
        cached: (b.tokens?.cached || 0) + (ev.tokensCached || 0),
        out: (b.tokens?.out || 0) + (ev.tokensOut || 0),
      }
    }
  }
  // 回合计时起点: 本轮发送时刻早于首事件时间戳时用发送时刻(计满首事件前的等待)
  const startTs = (ts: number) => (sendAt.value > 0 && sendAt.value < ts ? sendAt.value : ts)
  // 回合块初始化(首个事件到达时)
  const initTurn = (b: FlowBlock, ev: { id: string }, ts: number) => {
    if (!b.key) { b.key = ev.id; b.firstTs = startTs(ts) }
    else if (!b.firstTs) b.firstTs = startTs(ts)
  }
  for (const ev of events.value) {
    const ts = evTs(ev)
    if (ev.role === 'user' && ev.kind === 'message') {
      ai = null
      blocks.push({ key: ev.id, type: 'user', md: ev.content || '', segs: [], ts })
    } else if (ev.role === 'system' && ev.kind === 'message') {
      ai = null
      blocks.push({ key: ev.id, type: 'system', md: ev.content || '', segs: [], ts })
    } else if (ev.kind === 'error') {
      const b = ensureAi()
      initTurn(b, ev, ts)
      b.segs.push({ kind: 'error', text: ev.content || '' })
      accumTokens(b, ev)
      b.lastTs = ts
    } else if (ev.kind === 'tool_call') {
      const b = ensureAi()
      initTurn(b, ev, ts)
      for (const tc of ev.toolCalls || []) {
        b.segs.push({ kind: 'tool', id: tc.id, name: tc.name, args: tc.arguments, done: false, ts })
      }
      b.lastTs = ts
    } else if (ev.kind === 'tool_result') {
      const b = ensureAi()
      initTurn(b, ev, ts)
      // 配对: 组内按 toolCallId 找未完成段回填结果;找不到(窗口截断等)则补建
      let seg = b.segs.find(s => s.kind === 'tool' && s.id === ev.toolCallId && !s.done) as ToolSeg | undefined
      if (!seg) {
        seg = { kind: 'tool', id: ev.toolCallId || ev.id, name: ev.toolName || 'tool', done: true, ts }
        b.segs.push(seg)
      } else {
        if (ev.toolName) seg.name = ev.toolName
        seg.done = true
      }
      seg.ok = ev.ok
      seg.result = ev.content || ''
      if (seg.ts) seg.dur = Math.max(0, ts - seg.ts)
      // open_session 结果 → 注册 tabId 友好标签(后续 terminal 工具显示主机名)
      if (seg.name === 'open_session') registerTabLabel(seg)
      b.lastTs = ts
    } else if (ev.role === 'assistant' && ev.kind === 'message' && ev.content) {
      const b = ensureAi()
      initTurn(b, ev, ts)
      b.segs.push({ kind: 'text', text: ev.content })
      accumTokens(b, ev)
      b.lastTs = ts
    }
  }
  // 流式增量并入末尾 AI 组(无则新建),与已落盘内容同容器展示
  if (streaming.value) {
    const b = ensureAi()
    if (!b.firstTs) b.firstTs = sendAt.value || Date.now()
    b.segs.push({ kind: 'text', text: streaming.value, streaming: true })
  }
  // 乐观执行中回合: 已发送(非挂起)但首个 AI 事件未落盘 → 立即出现"进行中"回合并计时
  // runningHere 已翻真 or 发送后 20s 乐观窗口(防 status 事件延迟/丢失导致占位永驻)
  const last = blocks[blocks.length - 1]
  const turnStarting = runningHere.value || (sendAt.value > 0 && nowTick.value - sendAt.value < 20_000)
  if (last && last.type === 'user' && turnStarting) {
    // 计时起点优先用发送时刻(regenerate 复用旧用户消息,事件时间戳可能很久远)
    blocks.push({
      key: `running-${last.key}`, type: 'ai', md: '', segs: [],
      firstTs: sendAt.value || last.ts || Date.now(), optimistic: true,
    })
  }
  // 兜底 key
  for (let i = 0; i < blocks.length; i++) {
    if (!blocks[i].key) blocks[i].key = `blk-${i}`
  }
  return blocks
})

// open_session 结果解析 tabId+标签(字段防御式提取,不依赖后端格式细节)
function registerTabLabel(seg: ToolSeg) {
  const r = parseJson(seg.result)
  if (!r) return
  const tabId = str(r.tabId) || str(r.tab_id)
  if (!tabId) return
  const label = str(r.label) || str(r.name) || str(r.title) || str(r.session) || str(r.sessionPath)
  const args = parseJson(seg.args)
  const fallback = args ? (str(args.session_path) || '').split(/[\\/]/).pop() || '' : ''
  tabLabels.set(tabId, label || fallback || tabId.slice(0, 8))
}
function str(v: unknown): string | null {
  return typeof v === 'string' && v ? v : null
}

// ==================== 类型化工具渲染(终端/清单/会话事件/计划/写操作) ====================

type ToolKind = 'terminal' | 'list' | 'event' | 'todo' | 'write' | 'generic'
function classifyTool(name: string): ToolKind {
  switch (name) {
    case 'terminal_send': case 'batch_execute': case 'terminal_read': return 'terminal'
    case 'list_sessions': case 'list_tabs': return 'list'
    case 'open_session': case 'close_tab': case 'open_script': case 'switch_tab': return 'event'
    case 'update_todo': return 'todo'
    case 'script_write': return 'write'
    default: return 'generic'
  }
}

// 工具结果 JSON → 终端输出文本(output 字段或 results 数组)
function outText(seg: ToolSeg): string | null {
  const r = parseJson(seg.result)
  if (!r) return null
  const o = r.output
  if (typeof o === 'string' && o) return o
  if (Array.isArray(r.results)) {
    const parts: string[] = []
    for (const it of r.results) {
      if (typeof it === 'string') parts.push(it)
      else if (it && typeof it === 'object') {
        const m = it as Record<string, unknown>
        const c = str(m.command) || ''
        const out = str(m.output) || ''
        const code = m.exitCode != null ? ` [${m.exitCode}]` : ''
        parts.push(`$ ${c}${code}\n${out}`)
      }
    }
    if (parts.length) return parts.join('\n')
  }
  return null
}

// 友好命令(运维看命令而非工具名): terminal_send→命令首行,batch→N 条·首条,事件→目标
function friendlyCmd(seg: ToolSeg): { cmd: string; host?: string } {
  const a = parseJson(seg.args) || {}
  switch (seg.name) {
    case 'terminal_send': {
      const text = str(a.text) || ''
      const first = text.split('\n')[0].trim()
      const host = hostChip(str(a.tab_id))
      return { cmd: first || 'terminal', host }
    }
    case 'batch_execute': {
      const cmds = Array.isArray(a.commands) ? a.commands.filter(c => typeof c === 'string') as string[] : []
      const head = cmds[0] || ''
      const more = cmds.length > 1 ? ` 等 ${cmds.length} 条` : ''
      return { cmd: head + more, host: hostChip(str(a.tab_id)) }
    }
    case 'terminal_read':
      return { cmd: t('agent.readOutput'), host: hostChip(str(a.tab_id)) }
    case 'open_session': {
      const p = str(a.session_path) || ''
      return { cmd: `${t('agent.openSession')} ${p.split(/[\\/]/).pop() || p}` }
    }
    case 'close_tab':
      return { cmd: `${t('agent.closeTab')} ${tabLabels.get(str(a.tab_id) || '') || shortId(str(a.tab_id))}` }
    case 'open_script':
    case 'script_write': {
      const p = str(a.file_path) || ''
      const base = p.split(/[\\/]/).pop() || p
      if (seg.name === 'open_script') return { cmd: `${t('agent.openScript')} ${base}` }
      const size = parseJson(seg.args)?.content
      const kb = typeof size === 'string' ? ` · ${ceilKb(size.length)}` : ''
      return { cmd: `${t('agent.writeScript')} ${base}${kb}` }
    }
    case 'switch_tab':
      return { cmd: `${t('agent.switchTab')} ${tabLabels.get(str(a.tab_id) || '') || shortId(str(a.tab_id))}` }
    case 'update_todo':
      return { cmd: t('agent.planUpdated') }
    case 'list_sessions': return { cmd: t('agent.listSessions') }
    case 'list_tabs': return { cmd: t('agent.listTabs') }
    default: return { cmd: seg.name }
  }
}
function hostChip(tabId?: string | null): string | undefined {
  if (!tabId) return undefined
  return tabLabels.get(tabId) || shortId(tabId)
}
function shortId(id?: string | null): string {
  if (!id) return ''
  return id.length > 8 ? id.slice(0, 8) : id
}
function ceilKb(n: number): string {
  return n < 1024 ? `${n}B` : `${Math.ceil(n / 1024)}KB`
}

// md 渲染缓存(内容不可变段复用,流式重算不重复解析整段历史)
const mdCache = new Map<string, string>()
function cachedMd(text: string): string {
  let v = mdCache.get(text)
  if (v === undefined) {
    v = renderMd(text)
    if (mdCache.size > 400) mdCache.clear()
    mdCache.set(text, v)
  }
  return v
}

function fmtArgs(args?: string): string {
  if (!args) return ''
  try {
    return JSON.stringify(JSON.parse(args), null, 2)
  } catch { return args }
}

function fmtDur(ms?: number): string {
  if (ms == null || !Number.isFinite(ms)) return ''
  if (ms < 1000) return `${ms}ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`
  const m = Math.floor(ms / 60_000)
  const s = Math.floor((ms % 60_000) / 1000)
  return `${m}m${String(s).padStart(2, '0')}s`
}

// ==================== 工具段 → 纯 markdown(与正文同一渲染管线,样式天然统一) ====================

// md 行内转义(命令/标题里的 md 控制字符)
function escMd(s: string): string {
  return s.replace(/([\\`*_[\]])/g, '\\$1')
}

// 围栏代码块(内容含 ``` 时自动加长围栏,防止嵌套截断)
function fence(text: string, lang = ''): string {
  const t = text.replace(/\s+$/, '')
  const m = t.match(/`{3,}/)
  const f = m ? '`'.repeat(Math.min(m[0].length + 1, 8)) : '```'
  return `${f}${lang}\n${t}\n${f}`
}

// 状态记号: ✓ 成功 / ✗ 失败 / ⟳ 执行中
function statusMark(seg: ToolSeg): string {
  if (!seg.done) return '⟳'
  if (seg.ok === false) return '✗'
  return '✓'
}

// 超长输出截断(纯文本,头15+尾15+省略行;详细密度不截断)
function truncateOutMd(text: string): string {
  if (density.value === 'detailed') return text
  const lines = text.split('\n')
  if (lines.length <= 40) return text
  return `${lines.slice(0, 15).join('\n')}\n── ${t('agent.omittedLines', { n: lines.length - 30 })} ──\n${lines.slice(-15).join('\n')}`
}

/** toolSegMd 工具段 → markdown 文本(标题行 + 围栏输出/GFM 任务列表)。 */
function toolSegMd(seg: ToolSeg): string {
  const kind = classifyTool(seg.name)
  const f = friendlyCmd(seg)
  const st = statusMark(seg)
  const meta: string[] = []
  if (f.host) meta.push(f.host)
  if (seg.dur != null) meta.push(fmtDur(seg.dur))
  const metaStr = meta.length ? ` \`${meta.join(' · ')}\`` : ''

  // 会话事件类: 极轻量单行
  if (kind === 'event') {
    return `${st} **${escMd(f.cmd)}**${metaStr}`
  }
  // 计划更新: GFM 任务列表
  if (kind === 'todo') {
    const todos = parseTodos(seg)
    const done = todos.filter(x => x.status === 'done').length
    const cnt = todos.length ? ` \`${done}/${todos.length}\`` : ''
    const lines = todos.map(x =>
      x.status === 'done' ? `- [x] ${x.content}`
      : x.status === 'in_progress' ? `- ⟳ ${x.content}`
      : `- [ ] ${x.content}`,
    )
    return `**${t('agent.planUpdated')}${cnt}** ${st}${lines.length ? '\n\n' + lines.join('\n') : ''}`
  }
  // 紧凑密度: 已成功工具单行(无输出体)
  if (density.value === 'compact' && seg.done && seg.ok !== false) {
    return `${st} \`${f.cmd}\`${metaStr}`
  }
  const head = `**${st} ${escMd(f.cmd)}**${metaStr}`
  // 终端类: 提示符 + 输出(终端体验)
  if (kind === 'terminal' && seg.done) {
    const out = outText(seg)
    if (out) {
      const prompt = seg.name !== 'terminal_read' && f.cmd
        ? `${f.host ? `(${f.host}) ` : ''}$ ${f.cmd}\n`
        : ''
      return `${head}\n\n${fence(prompt + truncateOutMd(out))}`
    }
  }
  // 通用: 参数 + 结果(未完成仅显示参数)
  const parts = [head]
  if (seg.args) parts.push(fence(fmtArgs(seg.args) || '{}', 'json'))
  if (seg.done && seg.result) parts.push(fmtResultMd(seg.result))
  return parts.join('\n\n')
}

// update_todo 结果 → todo 列表
function parseTodos(seg: ToolSeg): { content: string; status: string }[] {
  const r = parseJson(seg.result)
  const arr = r && (Array.isArray(r.todos) ? r.todos : Array.isArray(r) ? r : null)
  if (!arr) return []
  return arr
    .filter((x): x is Record<string, unknown> => !!x && typeof x === 'object')
    .map(x => ({ content: String(x.content ?? ''), status: String(x.status ?? 'pending') }))
}

// 回合元信息: 操作数/失败数/耗时/是否执行中
// 执行中 = 本会话正在运行且该回合是最后一个块(乐观占位块或最新 AI 块)
function turnMeta(b: FlowBlock): { ops: number; fails: number; durMs: number; active: boolean } {
  let ops = 0, fails = 0
  for (const s of b.segs) {
    if (s.kind === 'tool') {
      ops++
      if (s.ok === false) fails++
    }
  }
  const blocks = flowBlocks.value
  const isLast = blocks.length > 0 && blocks[blocks.length - 1] === b
  const active = (runningHere.value || !!b.optimistic) && isLast
  // 执行中: 耗时实时跳动(nowTick 每 500ms 重算);完成: 首末事件时间差
  let durMs = 0
  if (b.firstTs) {
    durMs = active ? Math.max(0, nowTick.value - b.firstTs) : (b.lastTs ? b.lastTs - b.firstTs : 0)
  }
  return { ops, fails, durMs, active }
}

// 回合折叠默认策略: 执行中/最新回合展开;含失败回合不自动收起;紧凑档全收;用户操作过则尊重记忆
function isTurnOpen(b: FlowBlock, isLatest: boolean): boolean {
  const meta = turnMeta(b)
  if (touchedTurns.has(b.key)) return openTurns.has(b.key)
  if (density.value === 'compact') return meta.active
  if (meta.active || isLatest) return true
  if (meta.fails > 0) return true
  return false
}

// 回合分段: 最后一个工具/错误段之后的文本 = 结论(常显);其余 = 执行过程(可折叠)
function turnSplit(b: FlowBlock): { proc: AiSeg[]; concl: AiSeg[] } {
  let lastHeavy = -1
  for (let i = 0; i < b.segs.length; i++) {
    if (b.segs[i].kind !== 'text') lastHeavy = i
  }
  return { proc: b.segs.slice(0, lastHeavy + 1), concl: b.segs.slice(lastHeavy + 1) }
}

// 段列表 → markdown 文本(错误段用引用块;流式光标由外层附加)
function segsMd(segs: AiSeg[]): string {
  const parts: string[] = []
  for (const seg of segs) {
    if (seg.kind === 'text') parts.push(seg.text)
    else if (seg.kind === 'tool') parts.push(toolSegMd(seg))
    else parts.push('> ✗ ' + seg.text.split('\n').join('\n> '))
  }
  return parts.join('\n\n')
}

/** aiProcHtml / aiConclHtml: 回合分段 markdown → 统一 renderMd 渲染。 */
function aiProcHtml(b: FlowBlock): string {
  const html = cachedMd(segsMd(turnSplit(b).proc))
  return appendCursor(html, b)
}
function aiConclHtml(b: FlowBlock): string {
  const html = cachedMd(segsMd(turnSplit(b).concl))
  return appendCursor(html, b)
}

// 复制整回合 markdown 源文本(过程+结论)
function copyTurn(b: FlowBlock) {
  navigator.clipboard.writeText(segsMd(b.segs)).then(
    () => message.success(t('agent.copiedTurn')),
    () => message.error(t('common.error')),
  )
}

// token 用量文案: 缓存未命中输入 · 缓存命中输入 · 输出(网关未返回 usage 或全 0 时为空)
function tokenLabel(b: FlowBlock): string {
  const tk = b.tokens
  if (!tk) return ''
  const parts: string[] = []
  if (tk.in > 0) parts.push(`${t('agent.tokenIn')} ${tk.in.toLocaleString()}`)
  if (tk.cached > 0) parts.push(`${t('agent.tokenCached')} ${tk.cached.toLocaleString()}`)
  if (tk.out > 0) parts.push(`${t('agent.tokenOut')} ${tk.out.toLocaleString()}`)
  return parts.join(' · ')
}
// 流式光标: 仅当末段是流式文本时追加(两个分段各自判断,谁包含末段谁显示)
function appendCursor(html: string, b: FlowBlock): string {
  const last = b.segs[b.segs.length - 1]
  return last && last.kind === 'text' && last.streaming ? html + '<span class="stream-cursor"></span>' : html
}

// 最新 AI 回合 key(默认展开)
const latestAiKey = computed(() => {
  const blocks = flowBlocks.value
  for (let i = blocks.length - 1; i >= 0; i--) if (blocks[i].type === 'ai') return blocks[i].key
  return ''
})

// 工具结果 → markdown: JSON 含 output 字段时直接展示命令输出(终端体验);
// 含 markdown 结构(代码块/表格/标题/列表)则原样渲染;
// 纯文本则包裹代码块(保对齐+等宽+暗底)
function fmtResultMd(text: string): string {
  try {
    const o = JSON.parse(text)
    if (o && typeof o.output === 'string' && o.output.trim()) {
      const head = Object.entries(o)
        .filter(([k]) => k !== 'output')
        .map(([k, v]) => `${k}: ${typeof v === 'string' ? v : JSON.stringify(v)}`)
        .join('  \n')
      return (head ? head + '\n' : '') + '```\n' + o.output.trimEnd() + '\n```'
    }
  } catch { /* 非 JSON 继续 */ }
  if (/[`]{3}|^\s*\||^#{1,6}\s|^[-*]\s|^\d+\.\s/m.test(text)) return text
  return '```\n' + text + '\n```'
}

function permText(mode: string): string {
  switch (mode) {
    case 'plan': return t('agent.permPlan')
    case 'auto': return t('agent.permAuto')
    default: return t('agent.permManual')
  }
}

// 面板挂载后拉取档案列表(底部工具栏用)
refreshProfiles()
</script>

<template>
  <div class="agent-panel">
    <!-- 头部(第一排): 标题 + 状态 + 全局操作 -->
    <div class="agent-header">
      <span class="agent-title">{{ t('agent.panelTitle') }}</span>
      <n-tag v-if="status.running" size="tiny" type="info" round>{{ t('agent.stepIndicator', { step: status.step, max: status.maxSteps }) }}</n-tag>
      <div class="agent-header-actions">
        <n-tooltip trigger="hover" :delay="300">
          <template #trigger>
            <button class="ah-btn" @click="emit('toggle-maximize')">
              <n-icon :size="17" :component="maximized ? ContractOutline : ExpandOutline" />
            </button>
          </template>
          {{ maximized ? t('agent.restore') : t('agent.maximize') }}
        </n-tooltip>
        <n-tooltip trigger="hover" :delay="300">
          <template #trigger>
            <button class="ah-btn" @click="emit('open-mcp-settings')"><n-icon :size="17" :component="RocketOutline" /></button>
          </template>
          {{ t('agent.openMcpSettings') }}
        </n-tooltip>
        <n-tooltip trigger="hover" :delay="300">
          <template #trigger>
            <button class="ah-btn" @click="emit('open-settings')"><n-icon :size="17" :component="SettingsOutline" /></button>
          </template>
          {{ t('agent.openSettings') }}
        </n-tooltip>
        <button class="ah-btn" @click="emit('close')"><n-icon :size="17" :component="CloseOutline" /></button>
      </div>
    </div>

    <!-- 头部(第二排): 会话标题 + 刷新/新建/历史会话 -->
    <div class="agent-session-bar">
      <span
        class="agent-session-title"
        :title="activeTitle"
        @contextmenu.prevent="openSessionMenu($event, activeId)"
      >{{ activeTitle }}</span>
      <div class="agent-session-actions">
        <n-tooltip trigger="hover" :delay="300">
          <template #trigger>
            <button class="ah-btn" :disabled="!activeId" @click="handleRegenerate">
              <n-icon :size="17" :component="RefreshOutline" />
            </button>
          </template>
          {{ t('agent.regenerate') }}
        </n-tooltip>
        <n-tooltip trigger="hover" :delay="300">
          <template #trigger>
            <button class="ah-btn" @click="handleNewSession">
              <n-icon :size="17" :component="AddOutline" />
            </button>
          </template>
          {{ t('agent.newSession') }}
        </n-tooltip>
        <n-tooltip trigger="hover" :delay="300">
          <template #trigger>
            <button class="ah-btn" @click="showHistory = true">
              <n-icon :size="17" :component="TimeOutline" />
            </button>
          </template>
          {{ t('agent.historyTitle') }}
        </n-tooltip>
      </div>
    </div>

    <!-- 待办清单(悬浮卡) -->
    <div v-if="todos.length" class="agent-todos" :class="{ collapsed: !todoExpanded }">
      <div class="agent-todos-head" @click="todoExpanded = !todoExpanded">
        <n-icon :size="12" :component="ListOutline" />
        <span>{{ t('agent.todos') }} ({{ todos.filter(x => x.status === 'done').length }}/{{ todos.length }})</span>
        <n-icon :size="12" :component="ChevronUpOutline" :class="{ flipped: !todoExpanded }" style="margin-left: auto" />
      </div>
      <div v-show="todoExpanded" class="agent-todos-body">
        <div v-for="(todo, i) in todos" :key="i" class="agent-todo-item" :class="todo.status">
          <span class="todo-dot" />
          <span class="todo-text">{{ todo.content }}</span>
        </div>
      </div>
    </div>

    <!-- 事件列表(懒加载) -->
    <div class="agent-body" @wheel.passive="onAreaWheel" @pointerdown="onAreaPointerDown">
      <n-scrollbar ref="listRef" @scroll="onScroll">
        <div ref="listContentRef" class="agent-list">
          <div v-if="hasMore" class="agent-load-more">
            <n-button size="tiny" quaternary :loading="loadingMore" @click="handleLoadMore">
              {{ t('agent.loadMore', { n: headOffset }) }}
            </n-button>
          </div>
          <div v-if="events.length === 0 && !streaming" class="agent-empty">
            {{ t('agent.emptyHint') }}
          </div>

          <div class="agent-flow">
          <template v-for="block in flowBlocks" :key="block.key">
            <!-- 用户消息: 头像"我" + 名字 + markdown -->
            <div v-if="block.type === 'user'" class="msg user">
              <div class="msg-head">
                <div class="msg-avatar user-avatar">我</div>
                <span class="msg-name">我</span>
              </div>
              <div class="msg-body user-body md-body" v-html="cachedMd(block.md)" />
            </div>
            <!-- 系统消息(中断/上限等) -->
            <div v-else-if="block.type === 'system'" class="agent-ev system">
              <span class="agent-sys">{{ block.md }}</span>
            </div>
            <!-- AI 回合: 工单结构 = 回合头(元信息) + 执行过程(可折叠) + 结论(常显) -->
            <div v-else class="msg ai" :class="{ 'turn-open': isTurnOpen(block, block.key === latestAiKey) }">
              <div class="msg-head">
                <div class="msg-avatar ai-avatar">
                  <n-icon :size="14" :component="SparklesOutline" />
                </div>
                <span class="msg-name">{{ aiLabel }}</span>
                <span class="turn-meta">
                  <span v-if="turnMeta(block).active" class="turn-chip turn-chip-active">{{ t('agent.turnActive') }}</span>
                  <span v-if="turnMeta(block).fails" class="turn-chip turn-chip-fail">{{ t('agent.turnFailsN', { n: turnMeta(block).fails }) }}</span>
                  <span v-else-if="!turnMeta(block).active && turnMeta(block).ops" class="turn-chip">{{ t('agent.turnDone') }}</span>
                </span>
              </div>
              <!-- 执行过程折叠条(无过程段则不渲染) -->
              <div
                v-if="turnSplit(block).proc.length"
                class="turn-fold-bar"
                @click="toggleTurn(block.key); touchTurn(block.key)"
              >
                <span class="turn-fold-arrow" :class="{ open: isTurnOpen(block, block.key === latestAiKey) }">▸</span>
                <span class="turn-fold-label">{{ t('agent.turnProcess') }}</span>
                <span class="turn-fold-meta">{{ t('agent.turnOpsN', { n: turnMeta(block).ops }) }}<template v-if="turnMeta(block).fails"> · {{ t('agent.turnFailsN', { n: turnMeta(block).fails }) }}</template><template v-if="turnMeta(block).durMs > 0"> · {{ fmtDur(turnMeta(block).durMs) }}</template></span>
              </div>
              <div v-show="isTurnOpen(block, block.key === latestAiKey)" class="msg-body ai-body md-body turn-proc" v-html="aiProcHtml(block)" />
              <!-- 结论: 最后工具之后的文本,永远完整可读 -->
              <div v-if="turnSplit(block).concl.length" class="msg-body ai-body md-body turn-concl" v-html="aiConclHtml(block)" />
              <!-- 回合底部元信息: 执行中=流光渐变+加载符;完成=复制+token 消耗;右=AI 生成标识 -->
              <div class="turn-meta-bar" :class="{ running: turnMeta(block).active }">
                <div class="turn-meta-left">
                  <template v-if="turnMeta(block).active">
                    <span class="tm-spinner" />
                    <span class="tm-running">{{ t('agent.turnActive') }}<template v-if="turnMeta(block).durMs > 0"> · {{ fmtDur(turnMeta(block).durMs) }}</template></span>
                  </template>
                  <template v-else>
                    <button class="tm-copy-btn" @click="copyTurn(block)">
                      <n-icon :size="13" :component="CopyOutline" />
                      <span>{{ t('common.copy') }}</span>
                    </button>
                    <span v-if="tokenLabel(block)" class="tm-tokens">{{ tokenLabel(block) }}</span>
                  </template>
                </div>
                <span class="tm-ai-badge">{{ t('agent.aiGenerated') }}</span>
              </div>
            </div>
          </template>
          </div>
        </div>
      </n-scrollbar>
    </div>

    <!-- 错误提示 -->
    <div v-if="agentError" class="agent-error-bar">
      <span class="agent-error-text">{{ agentError }}</span>
      <n-button text size="tiny" @click="dismissError"><n-icon :size="12" :component="CloseOutline" /></n-button>
    </div>

    <!-- MCP 审批条(输入框上方,智能体触发的危险操作待审批) -->
    <div v-for="ap in mcpApprovals" :key="ap.id" class="mcp-approval-bar">
      <n-icon :size="13" :component="ShieldCheckmarkOutline" class="approval-icon" />
      <div class="approval-main">
        <div class="approval-line">
          <span class="approval-summary">{{ ap.summary }}</span>
          <n-tag size="tiny" type="warning" :bordered="false">{{ t('mcp.riskConfirm') }}</n-tag>
          <span class="approval-countdown">{{ approvalCountdown[ap.id] ?? 30 }}s</span>
        </div>
        <div class="approval-cmd" @click="toggleApprovalDetail(ap.id)">
          <span class="approval-cmd-text">{{ ap.command }}</span>
          <n-icon :size="11" :component="ChevronDownOutline" class="approval-chevron" :class="{ expanded: approvalExpanded[ap.id] }" />
        </div>
        <div v-if="approvalExpanded[ap.id]" class="approval-detail">
          <div v-if="ap.paths?.length" class="approval-paths">
            <div v-for="p in ap.paths" :key="p" class="approval-path">{{ p }}</div>
          </div>
          <div class="approval-detail-text">{{ ap.detail }}</div>
        </div>
      </div>
      <div class="approval-actions">
        <n-button size="tiny" type="primary" @click="handleMcpApprove(ap.id, false)">{{ t('mcp.approveOnce') }}</n-button>
        <n-button size="tiny" type="warning" @click="handleMcpApprove(ap.id, true)">{{ t('mcp.approvePermanent') }}</n-button>
        <n-button size="tiny" quaternary type="error" @click="handleMcpDeny(ap.id)">{{ t('mcp.deny') }}</n-button>
      </div>
    </div>

    <!-- 挂起消息条(执行中同会话新消息) -->
    <div v-if="pendingHere" class="agent-pending-bar">
      <n-icon :size="12" :component="FlashOutline" class="pending-icon" />
      <div class="agent-pending-main">
        <div class="agent-pending-text">{{ pendingMsg?.text }}</div>
        <div class="agent-pending-hint">{{ t('agent.pendingHint') }}</div>
      </div>
      <n-button size="tiny" type="primary" @click="handlePendingFlush">{{ t('agent.flushNow') }}</n-button>
      <n-button size="tiny" quaternary @click="pendingDiscard">
        <template #icon><n-icon :size="12" :component="CloseCircleOutline" /></template>
        {{ t('agent.discard') }}
      </n-button>
    </div>

    <!-- 组合输入框: 技能chip行内嵌入文本流(光标处插入) + 内嵌工具栏 -->
    <div class="agent-composer" :class="{ focused: composerFocused }">
      <div
        ref="editorRef"
        class="composer-editor"
        contenteditable="true"
        spellcheck="false"
        :data-placeholder="t('agent.inputPlaceholder')"
        :class="{ disabled: !activeId }"
        @focus="composerFocused = true"
        @blur="composerFocused = false"
        @input="onEditorInput"
        @keydown="onInputKeydown"
        @click="onEditorClick"
      />
      <div class="composer-toolbar">
        <!-- 加号(下拉占位) -->
        <div ref="plusBtnRef" class="pm-wrap">
          <n-button size="tiny" quaternary class="ct-btn ct-square" :title="t('agent.plusTitle')" @click="togglePlusMenu">
            <n-icon :size="16" :component="AddOutline" />
          </n-button>
          <div v-if="showPlusMenu" ref="plusPanelRef" class="pm-panel ph-panel" :style="plusStyle">
            <div class="ph-title">{{ t('agent.plusTitle') }}</div>
            <div class="ph-hint">{{ t('agent.plusSoon') }}</div>
          </div>
        </div>

        <!-- 权限模式(自绘点击面板) -->
        <div ref="permBtnRef" class="pm-wrap">
          <n-button size="tiny" quaternary class="ct-btn" :class="{ 'warn-mode': status.permMode === 'auto' }" @click="togglePermPanel">
            <n-icon :size="14" :component="permBtnIcon" />
            <span class="ct-label">{{ permText(status.permMode) }}</span>
            <n-icon :size="12" :component="ChevronDownOutline" style="margin-left: 2px" />
          </n-button>
          <div v-if="showPermPanel" ref="permPanelRef" class="pm-panel perm-panel" :style="permStyle">
            <div
              v-for="opt in permOptions"
              :key="opt.key"
              class="perm-opt"
              :class="{ cur: opt.key === status.permMode, warn: opt.warn }"
              @click="onPickPerm(opt.key)"
            >
              <n-icon :size="15" :component="opt.icon" class="perm-opt-ic" />
              <span class="perm-opt-name">{{ opt.label }}</span>
              <span v-if="opt.key === status.permMode" class="pm-check">✓</span>
              <div class="perm-opt-desc">{{ opt.desc }}</div>
            </div>
          </div>
        </div>

        <!-- 技能(点击添加/移除 chip) -->
        <n-popover trigger="click" placement="top-start" :width="280" :show-arrow="false">
          <template #trigger>
            <n-button size="tiny" quaternary class="ct-btn" :class="{ active: sessionSkills.length > 0 }">
              <n-icon :size="14" :component="ExtensionPuzzleOutline" />
              <span class="ct-label">{{ t('agent.skills') }}<template v-if="sessionSkills.length"> {{ sessionSkills.length }}</template></span>
            </n-button>
          </template>
          <div class="skill-pop">
            <div class="skill-pop-title">{{ t('agent.skillsPick') }}</div>
            <div class="skill-pop-hint">{{ t('agent.skillsHint') }}</div>
            <div
              v-for="opt in skillOptions"
              :key="opt.key"
              class="skill-pop-item"
              :class="{ checked: opt.checked }"
              @click="handleSkillToggle(opt.key)"
            >
              <n-icon :size="13" :component="ExtensionPuzzleOutline" class="skill-item-icon" />
              <div class="skill-info">
                <div class="skill-name">{{ opt.label }}</div>
                <div class="skill-desc">{{ opt.desc }}</div>
              </div>
              <span v-if="opt.checked" class="skill-on">{{ t('agent.skillOn') }}</span>
            </div>
          </div>
        </n-popover>

        <div class="ct-spacer" />

        <!-- 上下文用量圆圈(下拉占位) -->
        <div ref="ctxBtnRef" class="pm-wrap">
          <button class="ctx-ring-btn" :title="t('agent.ctxTitle')" @click="toggleCtxPanel">
            <svg width="18" height="18" viewBox="0 0 18 18" class="ctx-ring">
              <circle cx="9" cy="9" r="7" fill="none" stroke="currentColor" stroke-opacity="0.18" stroke-width="2" />
              <circle
                cx="9" cy="9" r="7" fill="none"
                :stroke="ctxRingColor" stroke-width="2" stroke-linecap="round"
                :stroke-dasharray="CTX_RING_LEN"
                :stroke-dashoffset="CTX_RING_LEN * (1 - ctxPercent / 100)"
                transform="rotate(-90 9 9)"
              />
            </svg>
            <span class="ctx-pct">{{ ctxPercent }}%</span>
          </button>
          <div v-if="showCtxPanel" ref="ctxPanelRef" class="pm-panel ph-panel" :style="ctxStyle">
            <div class="ph-title">{{ t('agent.ctxTitle') }}</div>
            <div class="ctx-stat-row">
              <span class="ctx-stat-label">{{ t('agent.ctxUsedLabel') }}</span>
              <span class="ctx-stat-val">{{ fmtTokens(ctxUsed) }}</span>
            </div>
            <div class="ctx-stat-row">
              <span class="ctx-stat-label">{{ t('agent.ctxWindowLabel') }}</span>
              <span class="ctx-stat-val">{{ fmtTokens(ctxWindow) }}</span>
            </div>
            <div class="ctx-stat-row">
              <span class="ctx-stat-label">{{ t('agent.ctxPercentLabel') }}</span>
              <span class="ctx-stat-val" :style="{ color: ctxRingColor }">{{ ctxPercent }}%</span>
            </div>
            <div class="ph-hint">{{ t('agent.ctxHint') }}</div>
          </div>
        </div>

        <!-- 供应商+模型(合并二级选择器,全点击无悬浮) -->
        <div ref="pmBtnRef" class="pm-wrap">
          <n-button size="tiny" quaternary class="ct-btn" @click="toggleProfileModel">
            <n-icon :size="14" :component="ServerOutline" />
            <span class="ct-label">{{ pmBtnLabel }}</span>
            <n-icon :size="12" :component="ChevronDownOutline" style="margin-left: 2px" />
          </n-button>
          <div v-if="showProfileModel" ref="pmPanelRef" class="pm-panel" :style="pmStyle">
            <!-- 一级: 供应商列表 -->
            <template v-if="!pmDrillId">
              <div class="pm-list">
                <div
                  v-for="p in profiles"
                  :key="p.id"
                  class="pm-provider"
                  :class="{ cur: p.id === status.activeProfileId }"
                  @click="onPickProfile(p)"
                >
                  <n-icon :size="13" :component="ServerOutline" class="pm-ic" />
                  <span class="pm-provider-name">{{ p.name }}</span>
                  <span v-if="p.id === status.activeProfileId" class="pm-on">{{ t('agent.pmActive') }}</span>
                  <n-icon :size="12" :component="ChevronForwardOutline" class="pm-fwd" />
                </div>
                <div v-if="!profiles.length" class="pm-loading">{{ t('agent.pmNoProfile') }}</div>
              </div>
              <!-- 底部固定: 管理模型 -->
              <div class="pm-manage" @click="onManageModels">
                <n-icon :size="13" :component="SettingsOutline" class="pm-ic" />
                <span class="pm-manage-name">{{ t('agent.pmManage') }}</span>
              </div>
            </template>
            <!-- 二级: 该供应商模型列表(下钻): 自定义模型在前,自动获取在后 -->
            <template v-else>
              <div class="pm-subhead" @click="pmDrillId = ''">
                <n-icon :size="12" :component="ChevronBackOutline" class="pm-back" />
                <span class="pm-subhead-name">{{ drilledProfile?.name }}</span>
                <n-button
                  size="tiny" quaternary class="pm-refresh"
                  :title="t('agent.pmRefresh')" :loading="loadingModels"
                  @click.stop="fetchModels(pmDrillId)"
                >
                  <n-icon :size="13" :component="RefreshOutline" />
                </n-button>
              </div>
              <div class="pm-list">
                <div v-if="loadingModels" class="pm-loading">{{ t('agent.pmLoading') }}</div>
                <template v-else>
                  <!-- 自定义模型 -->
                  <div v-if="drilledProfile?.customModels?.length" class="pm-section">
                    <div class="pm-section-title">{{ t('agent.pmCustom') }}</div>
                    <div
                      v-for="m in drilledProfile.customModels"
                      :key="'c-' + m"
                      class="pm-model"
                      :class="{ cur: pmDrillId === status.activeProfileId && m === status.model }"
                      @click="onPickModel(m)"
                    >
                      <span class="pm-model-name">{{ m }}</span>
                      <span v-if="pmDrillId === status.activeProfileId && m === status.model" class="pm-check">✓</span>
                    </div>
                  </div>
                  <!-- 自动获取模型 -->
                  <div v-if="modelList.length" class="pm-section">
                    <div class="pm-section-title">{{ t('agent.pmAuto') }}</div>
                    <div
                      v-for="m in modelList"
                      :key="m"
                      class="pm-model"
                      :class="{ cur: pmDrillId === status.activeProfileId && m === status.model }"
                      @click="onPickModel(m)"
                    >
                      <span class="pm-model-name">{{ m }}</span>
                      <span v-if="pmDrillId === status.activeProfileId && m === status.model" class="pm-check">✓</span>
                    </div>
                  </div>
                  <!-- 两者皆空: 失败详情或空态 -->
                  <div v-if="!modelList.length && !drilledProfile?.customModels?.length" class="pm-loading">
                    {{ fetchError ? `${t('agent.pmFetchFail')}: ${fetchError}` : t('agent.pmEmpty') }}
                  </div>
                  <!-- 自动获取失败但有自定义模型兜底 -->
                  <div v-else-if="fetchError" class="pm-error" :title="fetchError">
                    {{ t('agent.pmFetchFail') }} · {{ t('agent.pmUseCustom') }}
                  </div>
                </template>
              </div>
            </template>
          </div>
        </div>

        <!-- 中断(运行中): 实心方块 -->
        <n-tooltip v-if="runningHere" trigger="hover" :delay="300">
          <template #trigger>
            <button class="send-btn stop" @click="interrupt">
              <n-icon :size="16" :component="StopSharp" />
            </button>
          </template>
          {{ t('agent.interrupt') }}
        </n-tooltip>
        <!-- 发送: 上箭头 -->
        <n-tooltip v-else trigger="hover" :delay="300">
          <template #trigger>
            <button class="send-btn" :disabled="!editorText.trim() || !activeId" @click="handleSend">
              <n-icon :size="17" :component="ArrowUp" />
            </button>
          </template>
          {{ t('agent.send') }}
        </n-tooltip>
      </div>
    </div>

    <!-- 会话操作菜单 -->
    <n-dropdown
      placement="bottom-start"
      trigger="manual"
      :show="ctxShow"
      :x="ctxX"
      :y="ctxY"
      :options="[
        { label: t('agent.rename'), key: 'rename', icon: () => h(NIcon, null, () => h(CreateOutline)) },
        { label: t('agent.exportPdf'), key: 'export-pdf', icon: () => h(NIcon, null, () => h(DocumentTextOutline)) },
        { label: activeSession?.archived ? t('agent.unarchive') : t('agent.archive'), key: 'archive', icon: () => h(NIcon, null, () => h(activeSession?.archived ? UnarchiveOutline : ArchiveOutline)) },
        { label: t('common.delete'), key: 'delete', icon: () => h(NIcon, null, () => h(TrashOutline)) },
      ]"
      @clickoutside="ctxShow = false"
      @select="(key: string) => { ctxShow = false; if (key === 'rename') openRename(); else if (key === 'export-pdf') handleExportPdf(); else if (key === 'archive') handleArchiveToggle(); else if (key === 'delete') handleDeleteSession() }"
    />

    <!-- 历史会话弹窗 -->
    <n-modal v-model:show="showHistory" :title="t('agent.historyTitle')" preset="dialog" :show-icon="false" style="width: 400px">
      <n-scrollbar style="max-height: 360px">
        <div v-if="sessions.length === 0" class="history-empty">{{ t('agent.historyEmpty') }}</div>
        <div
          v-for="s in sessions"
          :key="s.id"
          class="history-item"
          :class="{ active: s.id === activeId }"
          @click="handleHistoryPick(s.id)"
        >
          <n-icon v-if="s.id === activeId" :size="12" :component="ChatbubbleEllipsesOutline" class="history-cur" />
          <span v-else class="history-dot" />
          <span class="history-name">{{ (s.archived ? `[${t('agent.archivedTag')}] ` : '') + s.title }}</span>
        </div>
      </n-scrollbar>
    </n-modal>

    <!-- 重命名弹窗 -->
    <n-modal v-model:show="renameShow" :title="t('agent.rename')" preset="dialog" :show-icon="false" style="width: 360px" :mask-closable="false">
      <n-input v-model:value="renameText" :placeholder="t('agent.titlePlaceholder')" @keyup.enter="handleRename" />
      <template #action>
        <n-button @click="renameShow = false">{{ t('common.cancel') }}</n-button>
        <n-button type="primary" @click="handleRename">{{ t('common.ok') }}</n-button>
      </template>
    </n-modal>
  </div>
</template>

<style scoped>
.agent-panel { height: 100%; flex-shrink: 0; display: flex; flex-direction: column; background: var(--sidebar-bg, #181818); overflow: hidden; border-left: 1px solid var(--sidebar-shadow, #3c3c3c); }
.agent-header { height: 35px; display: flex; align-items: center; gap: 8px; padding: 0 8px 0 12px; border-bottom: 1px solid var(--sidebar-shadow, #3c3c3c); flex-shrink: 0; }
.agent-title { font-size: 11px; font-weight: 600; color: var(--text-color, #d4d4d4); text-transform: uppercase; letter-spacing: 0.8px; }
.agent-header-actions { margin-left: auto; display: flex; align-items: center; gap: 1px; }
.ah-btn { width: auto; min-width: 21px; height: 26px; padding: 2px; display: flex; align-items: center; justify-content: center; border: none; background: transparent; border-radius: 5px; color: var(--text-color, #d4d4d4); opacity: 0.75; cursor: pointer; transition: background 0.15s, opacity 0.15s; }
.ah-btn:hover { background: rgba(255, 255, 255, 0.08); opacity: 1; }
.ah-btn:disabled { opacity: 0.35; cursor: default; }
.ah-btn:disabled:hover { background: transparent; }

.agent-session-bar { display: flex; align-items: center; justify-content: space-between; gap: 4px; padding: 3px 8px 4px; border-bottom: 1px solid var(--sidebar-shadow, #2a2a2a); flex-shrink: 0; }
.agent-session-title { flex: 1; min-width: 0; text-align: left; font-size: 12px; font-weight: 600; color: var(--text-color, #d4d4d4); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.agent-session-actions { display: flex; align-items: center; gap: 1px; flex-shrink: 0; }

/* 历史会话弹窗 */
.history-empty { padding: 24px; text-align: center; font-size: 12px; color: var(--text-secondary, #6e6e6e); }
.history-item { display: flex; align-items: center; gap: 8px; padding: 7px 10px; border-radius: 4px; cursor: pointer; margin: 0 -4px; }
.history-item:hover { background: var(--hover-bg, rgba(255, 255, 255, 0.06)); }
.history-item.active { background: rgba(78, 201, 176, 0.12); }
.history-dot { width: 6px; height: 6px; border-radius: 50%; background: var(--border-color, #555); flex-shrink: 0; }
.history-cur { color: #4ec9b0; flex-shrink: 0; }
.history-name { font-size: 12px; color: var(--text-color, #d4d4d4); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.history-item.active .history-name { color: #4ec9b0; }

/* 待办清单 */
.agent-todos { margin: 6px 8px 0; border: none; border-radius: 8px; background: var(--hover-bg, rgba(255, 255, 255, 0.045)); flex-shrink: 0; overflow: hidden; }
.agent-todos-head { display: flex; align-items: center; gap: 6px; padding: 5px 8px; font-size: 11px; font-weight: 600; color: var(--text-color, #d4d4d4); cursor: pointer; user-select: none; }
.agent-todos-body { padding: 2px 8px 6px; }
.agent-todo-item { display: flex; align-items: center; gap: 6px; font-size: 11px; padding: 2px 0; color: var(--text-color, #d4d4d4); }
.agent-todo-item.done { opacity: 0.45; text-decoration: line-through; }
.agent-todo-item.in_progress .todo-dot { background: #0078d4; }
.todo-dot { width: 6px; height: 6px; border-radius: 50%; background: #6e6e6e; flex-shrink: 0; }
.agent-todo-item.done .todo-dot { background: #4ec9b0; }
.todo-text { word-break: break-all; }

.agent-body { flex: 1; min-height: 0; display: flex; flex-direction: column; overflow: hidden; }
.agent-list { padding: 10px 12px 14px; display: flex; flex-direction: column; gap: 10px; min-height: 100%; }
.agent-flow { display: flex; flex-direction: column; gap: 12px; }
.agent-load-more { display: flex; justify-content: center; }
.agent-empty { padding: 40px 16px; text-align: center; font-size: 12px; color: var(--text-secondary, #6e6e6e); line-height: 1.9; }

/* 事件 */
.agent-ev { display: flex; flex-direction: column; }
.agent-ev.system { align-items: center; }
.agent-sys { font-size: 11px; color: var(--text-secondary, #6e6e6e); padding: 3px 10px; background: rgba(255, 255, 255, 0.03); border-radius: 999px; }

/* 消息: 用户右侧品牌色气泡,AI 左侧无底板(正文直接落在画布上,TRAE Work 风格) */
.msg { display: flex; flex-direction: column; gap: 6px; max-width: 100%; }
.msg.user { align-items: flex-end; }
.msg.ai { align-items: stretch; }
.msg-head { display: flex; align-items: center; gap: 7px; }
.msg-avatar { width: 24px; height: 24px; border-radius: 7px; flex-shrink: 0; display: flex; align-items: center; justify-content: center; }
.user-avatar { background: #2563eb; color: #fff; font-size: 11px; font-weight: 600; }
.ai-avatar { background: #7c5cff; color: #fff; }
.msg-name { font-size: 11px; color: var(--text-secondary, #999); font-weight: 500; }
.msg-body { border-radius: 12px; font-size: 13px; line-height: 1.7; overflow-wrap: break-word; }
.user-body { background: #2563eb; color: #f0f7ff; padding: 9px 14px; border-radius: 12px 12px 4px 12px; width: fit-content; max-width: 86%; }
.ai-body { background: transparent; color: var(--text-color, #d4d4d4); padding: 0; width: 100%; }

/* markdown 正文 */
/* AI/用户消息正文: 强制左对齐(自适应宽度,不居中) */
.md-body { text-align: left; }
.md-body :deep(p) { margin: 0 0 6px; }
.md-body :deep(p:last-child) { margin-bottom: 0; }
.md-body :deep(h1), .md-body :deep(h2), .md-body :deep(h3), .md-body :deep(h4), .md-body :deep(h5), .md-body :deep(h6) { margin: 8px 0 4px; font-weight: 600; line-height: 1.4; }
.md-body :deep(h1) { font-size: 15px; } .md-body :deep(h2) { font-size: 14px; } .md-body :deep(h3), .md-body :deep(h4), .md-body :deep(h5), .md-body :deep(h6) { font-size: 13px; }
.md-body :deep(ul), .md-body :deep(ol) { margin: 2px 0 6px; padding-left: 20px; }
.md-body :deep(li) { margin: 2px 0; }
.md-body :deep(code) { font-family: Consolas, 'Courier New', monospace; font-size: 11.5px; background: rgba(0, 0, 0, 0.28); padding: 1px 5px; border-radius: 3px; }
.md-body :deep(pre) { margin: 4px 0; background: rgba(0, 0, 0, 0.35); border-radius: 6px; padding: 8px 10px; overflow-x: auto; }
.md-body :deep(pre code) { background: transparent; padding: 0; font-size: 11.5px; line-height: 1.55; }
.md-body :deep(blockquote) { margin: 6px 0; padding: 6px 12px; border-radius: 8px; background: rgba(167, 139, 250, 0.08); color: var(--text-secondary, #aaa); }
.md-body :deep(table) { border-collapse: collapse; margin: 4px 0; font-size: 11.5px; }
.md-body :deep(th), .md-body :deep(td) { border: 1px solid var(--border-color, #454545); padding: 3px 8px; text-align: left; }
.md-body :deep(th) { background: rgba(255, 255, 255, 0.06); font-weight: 600; }
.md-body :deep(a) { color: #4a9eff; text-decoration: none; }
.md-body :deep(a:hover) { text-decoration: underline; }
.md-body :deep(hr) { border: none; border-top: 1px solid var(--border-color, #454545); margin: 8px 0; }
.md-body :deep(strong) { font-weight: 600; }
.user-body code { background: rgba(255, 255, 255, 0.16); }
.user-body a { color: #bfe0ff; }

/* 流式光标(v-html 内,需 :deep) */
.md-body :deep(.stream-cursor) { display: inline-block; width: 7px; height: 14px; background: #a78bfa; margin-left: 2px; vertical-align: -2px; border-radius: 1.5px; animation: blink 1s step-end infinite; }
@keyframes blink { 50% { opacity: 0; } }

/* GFM 任务列表(计划清单): 复选框弱化,状态色区分 */
.md-body :deep(input[type='checkbox']) { accent-color: #4ade80; width: 12px; height: 12px; vertical-align: -1px; margin-right: 4px; }

/* 回合结构: 头部元信息 + Worked 式折叠胶囊 + 过程/结论 */
.turn-meta { display: flex; align-items: center; gap: 5px; margin-left: 6px; flex-wrap: wrap; }
.turn-chip { font-size: 10px; padding: 1px 8px; border-radius: 999px; background: rgba(255, 255, 255, 0.06); color: var(--text-secondary, #888); font-variant-numeric: tabular-nums; }
.turn-chip-active { background: rgba(251, 191, 36, 0.14); color: #fbbf24; }
.turn-chip-fail { background: rgba(248, 113, 113, 0.12); color: #f87171; }
.turn-fold-bar { display: flex; align-items: center; gap: 8px; padding: 5px 11px; margin: 4px 0 6px; width: 100%; box-sizing: border-box; border-radius: 9px; cursor: pointer; user-select: none; font-size: 11px; color: var(--text-secondary, #999); background: rgba(255, 255, 255, 0.05); transition: background 0.18s ease, color 0.18s ease; }
.turn-fold-bar:hover { color: var(--text-color, #ddd); background: rgba(255, 255, 255, 0.09); }
.turn-fold-arrow { display: inline-block; font-size: 9px; transition: transform 0.18s ease; color: #a78bfa; }
.turn-fold-arrow.open { transform: rotate(90deg); }
.turn-fold-label { color: var(--text-secondary, #aaa); flex-shrink: 0; font-weight: 500; }
.turn-fold-meta { font-variant-numeric: tabular-nums; opacity: 0.8; }
.turn-proc { width: 100%; }
.turn-concl { width: 100%; margin-top: 4px; }

/* 回合底部元信息栏: 左=复制+token, 右=AI 生成标识(两端对齐) */
.turn-meta-bar { display: flex; align-items: center; justify-content: space-between; gap: 8px; width: 100%; margin-top: 6px; padding-top: 6px; border-top: 1px dashed var(--sidebar-shadow, #3a3a3a); }
.turn-meta-left { display: flex; align-items: center; gap: 10px; min-width: 0; }
.tm-copy-btn { display: inline-flex; align-items: center; gap: 4px; height: 22px; padding: 0 7px; border: none; border-radius: 5px; background: transparent; color: var(--text-secondary, #888); font-size: 11px; line-height: 1; cursor: pointer; transition: background 0.15s, color 0.15s; flex-shrink: 0; }
.tm-copy-btn:hover { background: rgba(255, 255, 255, 0.06); color: var(--text-color, #d4d4d4); }
.tm-tokens { font-size: 10.5px; color: var(--text-secondary, #777); font-variant-numeric: tabular-nums; white-space: nowrap; }
.tm-ai-badge { font-size: 10.5px; color: var(--text-secondary, #777); flex-shrink: 0; user-select: none; }

/* 执行中: 流光渐变横扫 + 2s/圈加载符 */
.turn-meta-bar.running { border-top-color: transparent; padding: 5px 6px 4px; border-radius: 6px; background: linear-gradient(90deg, transparent 0%, rgba(78, 201, 176, 0.10) 25%, rgba(99, 140, 255, 0.13) 50%, rgba(78, 201, 176, 0.10) 75%, transparent 100%); background-size: 200% 100%; animation: tm-shimmer 2.4s linear infinite; }
@keyframes tm-shimmer { from { background-position: 200% 0; } to { background-position: -200% 0; } }
.tm-spinner { display: inline-block; width: 11px; height: 11px; border-radius: 50%; border: 1.5px solid rgba(255, 255, 255, 0.14); border-top-color: #4ec9b0; animation: tm-rotate 2s linear infinite; flex-shrink: 0; }
@keyframes tm-rotate { to { transform: rotate(360deg); } }
.tm-running { font-size: 11px; color: var(--text-secondary, #999); white-space: nowrap; }

/* 错误栏 */
.agent-error-bar { display: flex; align-items: center; gap: 6px; padding: 4px 10px; background: rgba(228, 88, 88, 0.12); flex-shrink: 0; }
.agent-error-text { flex: 1; font-size: 11px; color: #e45858; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

/* 挂起消息条 */
.agent-pending-bar { display: flex; align-items: center; gap: 8px; padding: 6px 10px; background: rgba(226, 160, 63, 0.12); border-top: 1px solid rgba(226, 160, 63, 0.3); flex-shrink: 0; }

/* MCP 审批条 */
.mcp-approval-bar { display: flex; align-items: flex-start; gap: 8px; padding: 8px 10px; background: rgba(226, 160, 63, 0.1); border-top: 1px solid rgba(226, 160, 63, 0.3); flex-shrink: 0; }
.approval-icon { color: #e2a03f; flex-shrink: 0; margin-top: 2px; }
.approval-main { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 3px; }
.approval-line { display: flex; align-items: center; gap: 6px; }
.approval-summary { font-size: 12px; color: var(--text-primary, #e8e8e8); font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.approval-countdown { font-size: 11px; color: #e2a03f; margin-left: auto; flex-shrink: 0; font-variant-numeric: tabular-nums; }
.approval-cmd { display: flex; align-items: center; gap: 4px; cursor: pointer; user-select: none; }
.approval-cmd-text { font-size: 11px; font-family: var(--font-mono, Consolas, monospace); color: var(--text-secondary, #aaa); background: rgba(0,0,0,.25); padding: 2px 6px; border-radius: 3px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex: 1; min-width: 0; }
.approval-chevron { color: var(--text-secondary, #888); flex-shrink: 0; transition: transform .15s; }
.approval-chevron.expanded { transform: rotate(180deg); }
.approval-detail { display: flex; flex-direction: column; gap: 4px; }
.approval-paths { display: flex; flex-direction: column; gap: 2px; }
.approval-path { font-size: 11px; font-family: var(--font-mono, Consolas, monospace); color: #7ec5ff; word-break: break-all; }
.approval-path::before { content: '▸ '; color: var(--text-secondary, #888); }
.approval-detail-text { font-size: 11px; color: var(--text-secondary, #999); word-break: break-all; }
.approval-actions { display: flex; flex-direction: column; gap: 4px; flex-shrink: 0; }
.pending-icon { color: #e2a03f; flex-shrink: 0; }
.agent-pending-main { flex: 1; min-width: 0; }
.agent-pending-text { font-size: 11px; color: var(--text-color, #d4d4d4); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.agent-pending-hint { font-size: 10px; color: var(--text-secondary, #888); margin-top: 1px; }

/* 组合输入框: 行内 chip 嵌入文本流(contenteditable) */
.agent-composer { margin: 8px 10px 10px; border-radius: 10px; border: 1px solid var(--sidebar-shadow, #3c3c3c); background: var(--bg-color, #252526); flex-shrink: 0; transition: border-color .15s, box-shadow .15s; overflow: hidden; }
.agent-composer.focused { border-color: rgba(78, 201, 176, 0.55); box-shadow: 0 0 0 2px rgba(78, 201, 176, 0.12); }
.composer-editor { padding: 10px 10px 12px; min-height: 44px; max-height: 168px; overflow-y: auto; outline: none; color: var(--text-color, #d4d4d4); font-family: inherit; font-size: 12px; line-height: 1.55; text-align: left; word-break: break-word; white-space: pre-wrap; }
.composer-editor:empty::before { content: attr(data-placeholder); color: var(--text-secondary, #6e6e6e); pointer-events: none; }
.composer-editor.disabled { pointer-events: none; opacity: 0.6; }

/* 行内技能 chip */
.composer-chip { display: inline-flex; align-items: center; gap: 3px; vertical-align: middle; background: rgba(78, 201, 176, 0.14); border: 1px solid rgba(78, 201, 176, 0.4); border-radius: 10px; padding: 0 4px 0 6px; margin: 0 2px; font-size: 11px; line-height: 18px; height: 20px; user-select: none; }
.composer-chip .chip-ic { font-size: 10px; }
.composer-chip .chip-name { color: #4ec9b0; white-space: nowrap; }
.composer-chip .chip-x { cursor: pointer; color: var(--text-secondary, #888); padding: 0 2px; border-radius: 50%; }
.composer-chip .chip-x:hover { color: #e45858; background: rgba(228, 88, 88, 0.18); }
.composer-toolbar { display: flex; align-items: center; gap: 4px; padding: 6px 8px 8px; flex-wrap: wrap; min-height: 42px; }
.ct-btn { max-width: 150px; }
/* 按钮调高: naive-ui 将 --n-height 写为内联样式,类选择器覆盖变量无效,必须显式 height */
.composer-toolbar :deep(.n-button) { height: 30px; padding: 0 8px; }
.composer-toolbar :deep(.ct-square) { padding: 0; min-width: 30px; }
.ct-btn.active { color: #4ec9b0; }
/* 行高留足下行空间: 修复 g/j/p/y 等字母底部弯钩被 overflow:hidden 裁剪 */
.ct-btn .ct-label { font-size: 11px; line-height: 16px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ct-spacer { flex: 1; }

/* 上下文用量圆圈按钮(占位) */
.ctx-ring-btn { display: inline-flex; align-items: center; gap: 5px; height: 30px; padding: 0 7px; border: none; border-radius: 6px; background: transparent; color: var(--text-secondary, #999); cursor: pointer; transition: background 0.15s, color 0.15s; flex-shrink: 0; }
.ctx-ring-btn:hover { background: rgba(255, 255, 255, 0.06); color: var(--text-color, #d4d4d4); }
.ctx-ring { display: block; flex-shrink: 0; }
.ctx-pct { font-size: 11px; line-height: 16px; color: inherit; font-variant-numeric: tabular-nums; }

/* 发送/中断: 圆形图标按钮 */
.send-btn { width: 28px; height: 28px; padding: 0; display: flex; align-items: center; justify-content: center; border: none; border-radius: 7px; background: #2563eb; color: #fff; cursor: pointer; transition: background 0.15s, opacity 0.15s; flex-shrink: 0; }
.send-btn:hover { background: #1d4fd8; }
.send-btn:disabled { opacity: 0.4; cursor: default; }
.send-btn.stop { background: #e45858; }
.send-btn.stop:hover { background: #c74444; }

/* 供应商+模型二级选择器(全点击交互) */
.pm-wrap { position: relative; flex-shrink: 0; }
/* fixed 定位: 脱离 composer 的 overflow:hidden,弹层可越过输入框边界向上展开 */
.pm-panel { position: fixed; width: 260px; display: flex; flex-direction: column; background: var(--bg-color, #252526); border: 1px solid var(--sidebar-shadow, #3c3c3c); border-radius: 8px; padding: 4px; z-index: 3000; box-shadow: 0 8px 24px rgba(0, 0, 0, 0.4); }
.pm-list { flex: 1; min-height: 0; overflow-y: auto; }
.pm-ic { color: var(--text-secondary, #999); flex-shrink: 0; }
.pm-provider { display: flex; align-items: center; gap: 6px; padding: 6px 8px; border-radius: 6px; cursor: pointer; user-select: none; }
.pm-provider + .pm-provider { margin-top: 2px; }
.pm-provider:hover { background: rgba(255, 255, 255, 0.06); }
.pm-provider.cur .pm-provider-name { color: #4ec9b0; }
.pm-provider-name { font-size: 12px; color: var(--text-color, #d4d4d4); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex: 1; }
.pm-on { font-size: 10px; color: #4ec9b0; background: rgba(78, 201, 176, 0.12); padding: 1px 6px; border-radius: 8px; flex-shrink: 0; }
.pm-fwd { color: var(--text-secondary, #777); flex-shrink: 0; }
.pm-manage { display: flex; align-items: center; gap: 6px; padding: 6px 8px; margin-top: 2px; border-top: 1px solid var(--sidebar-shadow, #2f2f2f); border-radius: 0 0 6px 6px; cursor: pointer; user-select: none; }
.pm-manage:hover { background: rgba(255, 255, 255, 0.06); }
.pm-manage-name { font-size: 12px; color: var(--text-color, #d4d4d4); }
.pm-subhead { display: flex; align-items: center; gap: 4px; padding: 5px 8px; border-bottom: 1px solid var(--sidebar-shadow, #2f2f2f); margin-bottom: 2px; border-radius: 6px 6px 0 0; cursor: pointer; user-select: none; }
.pm-subhead:hover { background: rgba(255, 255, 255, 0.04); }
.pm-subhead-name { font-size: 11px; font-weight: 600; color: var(--text-secondary, #bbb); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.pm-back { color: var(--text-secondary, #999); flex-shrink: 0; }
.pm-model { display: flex; align-items: center; justify-content: space-between; gap: 8px; padding: 5px 8px; border-radius: 5px; cursor: pointer; user-select: none; }
.pm-model:hover { background: rgba(255, 255, 255, 0.06); }
.pm-model.cur { color: #4ec9b0; }
.pm-model-name { font-size: 11px; color: var(--text-color, #ccc); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.pm-model.cur .pm-model-name { color: #4ec9b0; }
.pm-check { font-size: 11px; color: #4ec9b0; flex-shrink: 0; }
.pm-loading { padding: 8px 10px; font-size: 11px; color: var(--text-secondary, #6e6e6e); text-align: left; }
.perm-panel { min-width: 230px; max-width: 260px; overflow-y: auto; }
/* 面板内小按钮不受工具栏 30px 高度影响(如二级页刷新) */
.pm-panel :deep(.n-button) { height: 22px; padding: 0 6px; }
.pm-refresh { margin-left: auto; flex-shrink: 0; }
/* 二级页模型分区: 自定义在前/自动获取在后 */
.pm-section + .pm-section { margin-top: 4px; }
.pm-section-title { font-size: 10px; color: var(--text-secondary, #777); padding: 4px 8px 2px; user-select: none; }
.pm-error { padding: 6px 8px; font-size: 10.5px; color: #e2a03f; line-height: 1.5; word-break: break-all; border-top: 1px solid var(--sidebar-shadow, #2f2f2f); margin-top: 4px; }
/* 占位面板(加号/上下文): 置于 .pm-panel 之后覆盖宽度 */
.ph-panel { width: 220px; padding: 10px; gap: 8px; }
.ph-title { font-size: 12px; font-weight: 600; color: var(--text-color, #d4d4d4); }
.ph-hint { font-size: 11px; color: var(--text-secondary, #888); line-height: 1.5; }
.ctx-stat-row { display: flex; align-items: center; justify-content: space-between; padding: 6px 8px; background: rgba(255, 255, 255, 0.04); border-radius: 6px; }
.ctx-stat-label { font-size: 11px; color: var(--text-secondary, #999); }
.ctx-stat-val { font-size: 11px; color: var(--text-color, #d4d4d4); font-variant-numeric: tabular-nums; }
/* 按钮态: 自动模式橙黄警示 */
.ct-btn.warn-mode { color: #e2a03f !important; }
/* 权限模式选项: 图标 + 标题 + 描述(两行布局,加高,文本左对齐) */
.perm-opt { display: grid; grid-template-columns: 16px 1fr auto; grid-template-rows: auto auto; column-gap: 8px; align-items: center; padding: 8px 10px; border-radius: 6px; cursor: pointer; user-select: none; text-align: left; }
.perm-opt + .perm-opt { margin-top: 2px; }
.perm-opt:hover { background: rgba(255, 255, 255, 0.06); }
.perm-opt-ic { grid-row: 1; color: var(--text-secondary, #999); }
.perm-opt-name { grid-row: 1; font-size: 12px; color: var(--text-color, #d4d4d4); font-weight: 500; }
.perm-opt-desc { grid-column: 2 / 4; grid-row: 2; font-size: 10.5px; color: var(--text-secondary, #777); line-height: 1.5; margin-top: 2px; }
.perm-opt .pm-check { grid-row: 1; }
.perm-opt.cur .perm-opt-name, .perm-opt.cur .perm-opt-ic { color: #4ec9b0; }
/* 自动审批: 橙黄色警示 */
.perm-opt.warn .perm-opt-name, .perm-opt.warn .perm-opt-ic { color: #e2a03f; }
.perm-opt.warn:hover { background: rgba(226, 160, 63, 0.1); }
.perm-opt.warn.cur .pm-check { color: #e2a03f; }
.perm-opt.cur.warn { background: rgba(226, 160, 63, 0.07); }

/* 技能选择弹层 */
.skill-pop { display: flex; flex-direction: column; gap: 2px; }
.skill-pop-title { font-size: 12px; font-weight: 600; margin-bottom: 2px; }
.skill-pop-hint { font-size: 10px; color: var(--text-secondary, #888); margin-bottom: 6px; }
.skill-pop-item { display: flex; gap: 8px; padding: 6px 8px; border-radius: 4px; cursor: pointer; align-items: center; border: 1px solid transparent; }
.skill-pop-item:hover { background: var(--hover-bg, rgba(255, 255, 255, 0.06)); }
.skill-pop-item.checked { background: rgba(78, 201, 176, 0.1); border-color: rgba(78, 201, 176, 0.35); }
.skill-item-icon { color: var(--text-secondary, #888); flex-shrink: 0; }
.skill-pop-item.checked .skill-item-icon { color: #4ec9b0; }
.skill-info { min-width: 0; flex: 1; }
.skill-name { font-size: 12px; color: var(--text-color, #d4d4d4); }
.skill-desc { font-size: 10px; color: var(--text-secondary, #888); margin-top: 1px; }
.skill-on { font-size: 10px; color: #4ec9b0; flex-shrink: 0; }
</style>
