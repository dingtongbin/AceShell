<script setup lang="ts">
// Ping 工具标签页(componentId = "tool"): 探测工作台 + 可视化。
// 探测由后端子协程执行, 结果经 ctx.onEvent 流式推送; 历史记录持久化在插件私有数据目录。
// 通过 PropsJson.recordId 打开历史记录(记录模式), 点开始则回到实时模式。
import { ref, computed, watch, onBeforeUnmount } from 'vue'
import { debounceClick } from './debounce'

interface PingPacket {
  seq: number
  ok: boolean
  ms: number
  line: string
}

interface PingStats {
  sent: number
  lost: number
  minMs: number
  maxMs: number
  avgMs: number
  jitterMs: number
}

interface PingRecord {
  id: string
  target: string
  startedAt: string
  count: number
  intervalMs: number
  timeoutMs: number
  sent: number
  lost: number
  minMs: number
  maxMs: number
  avgMs: number
  jitterMs: number
  packets?: PingPacket[]
}

const props = defineProps<{
  ctx: {
    pluginID: string
    call: (method: string, args?: Record<string, any>) => Promise<any>
    onEvent: (cb: (payload: any) => void) => () => void
    toast: (message: string, level?: 'info' | 'warning' | 'error' | 'success') => void
    theme: { isDark: { value: boolean }; accent: { value: string } }
  }
  recordId?: string
  active?: boolean
}>()

// ==================== 状态 ====================
// mode: live = 实时探测会话; record = 查看历史记录
const mode = ref<'idle' | 'live' | 'record'>('idle')
const running = ref(false)
const livePackets = ref<PingPacket[]>([])
const viewRecord = ref<PingRecord | null>(null)
const historyList = ref<PingRecord[]>([])

// 表单
const host = ref('')
const count = ref(0)
const intervalMs = ref(1000)
const timeoutMs = ref(2000)

// ==================== 事件流(实时模式) ====================
let offEvent: (() => void) | null = null
function onEvent(payload: any) {
  if (mode.value !== 'live') return
  if (payload?.type === 'result') {
    livePackets.value = [...livePackets.value, { seq: payload.seq, ok: !!payload.ok, ms: payload.ms || 0, line: String(payload.line || '') }]
  } else if (payload?.type === 'done') {
    running.value = false
    props.ctx.toast('探测完成, 已存入历史', 'success')
    void refreshHistory()
  } else if (payload?.type === 'stopped') {
    running.value = false
    void refreshHistory()
  }
}
offEvent = props.ctx.onEvent(onEvent)

// ==================== 当前展示的包序列与统计 ====================
const packets = computed<PingPacket[]>(() =>
  mode.value === 'record' ? (viewRecord.value?.packets ?? []) : livePackets.value)

const stats = computed<PingStats>(() => {
  if (mode.value === 'record' && viewRecord.value) {
    const r = viewRecord.value
    return { sent: r.sent, lost: r.lost, minMs: r.minMs, maxMs: r.maxMs, avgMs: r.avgMs, jitterMs: r.jitterMs }
  }
  const ok = packets.value.filter(p => p.ok)
  const ms = ok.map(p => p.ms)
  const minMs = ms.length ? Math.min(...ms) : 0
  const maxMs = ms.length ? Math.max(...ms) : 0
  const avgMs = ms.length ? ms.reduce((a, b) => a + b, 0) / ms.length : 0
  let jit = 0
  for (let i = 1; i < ms.length; i++) jit += Math.abs(ms[i] - ms[i - 1])
  const jitterMs = ms.length > 1 ? jit / (ms.length - 1) : 0
  return { sent: packets.value.length, lost: packets.value.length - ok.length, minMs, maxMs, avgMs, jitterMs }
})

const lossPct = computed(() =>
  stats.value.sent > 0 ? (stats.value.lost * 100) / stats.value.sent : 0)

// ==================== SVG 图表几何 ====================
const CW = 1000
const CH = 200
const yMax = computed(() => {
  const ms = packets.value.filter(p => p.ok).map(p => p.ms)
  const raw = ms.length ? Math.max(...ms) : 50
  return Math.max(50, Math.ceil(raw * 1.2))
})
const xFor = (i: number): number =>
  packets.value.length <= 1 ? CW / 2 : 4 + (i / (packets.value.length - 1)) * (CW - 8)
const yFor = (ms: number): number => CH - 6 - (ms / yMax.value) * (CH - 16)

const linePoints = computed(() =>
  packets.value
    .map((p, i) => (p.ok ? `${xFor(i).toFixed(1)},${yFor(p.ms).toFixed(1)}` : ''))
    .filter(Boolean)
    .join(' '))

const lossIdx = computed(() =>
  packets.value.map((p, i) => ({ p, i })).filter(x => !x.p.ok).map(x => x.i))

const gridRows = computed(() => [0.25, 0.5, 0.75, 1].map(f => ({
  y: (CH - 6 - f * (CH - 16)).toFixed(1),
  label: Math.round(yMax.value * f),
})))

// ==================== 操作(300ms 防抖) ====================
const dStart = debounceClick(() => { void start() })
const dStop = debounceClick(() => { void stop() })
const dClear = debounceClick(() => { void clearHistory() })
const dNewTab = debounceClick(() => { void openTab('') })
const dOpenRecord = debounceClick((id: string) => { void openTab(id) })

async function start() {
  const target = host.value.trim()
  if (!target) { props.ctx.toast('请输入目标主机', 'warning'); return }
  try {
    await props.ctx.call('start', {
      host: target, count: count.value, intervalMs: intervalMs.value, timeoutMs: timeoutMs.value,
    })
    mode.value = 'live'
    livePackets.value = []
    viewRecord.value = null
    running.value = true
  } catch (e: any) {
    props.ctx.toast(String(e?.message || e), 'error')
  }
}

async function stop() {
  try { await props.ctx.call('stop') } catch {}
}

async function openTab(recordId: string) {
  try {
    await props.ctx.call('history.open', recordId ? { id: recordId } : {})
  } catch (e: any) {
    props.ctx.toast(String(e?.message || e), 'error')
  }
}

async function clearHistory() {
  try {
    await props.ctx.call('history.clear')
    await refreshHistory()
    if (mode.value === 'record') { mode.value = 'idle'; viewRecord.value = null }
    props.ctx.toast('历史已清空', 'success')
  } catch (e: any) {
    props.ctx.toast(String(e?.message || e), 'error')
  }
}

// ==================== 历史与记录加载 ====================
async function refreshHistory() {
  try {
    const res = await props.ctx.call('history.list')
    historyList.value = Array.isArray(res?.records) ? res.records : []
  } catch {}
}

async function loadRecord(id: string) {
  try {
    const res = await props.ctx.call('history.get', { id })
    const rec = res?.record as PingRecord | undefined
    if (!rec) return
    mode.value = 'record'
    viewRecord.value = rec
    running.value = false
  } catch (e: any) {
    props.ctx.toast(String(e?.message || e), 'error')
  }
}

watch(() => props.recordId, (id) => {
  if (id) void loadRecord(id)
}, { immediate: true })

function backToLive() {
  mode.value = 'idle'
  viewRecord.value = null
  livePackets.value = []
}

function fmtTime(rfc: string): string {
  const d = new Date(rfc)
  if (isNaN(d.getTime())) return rfc
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

onBeforeUnmount(() => {
  offEvent?.()
  if (running.value) { void props.ctx.call('stop').catch(() => {}) }
})
</script>

<template>
  <div class="aceshell-plugin-ping ppt">
    <div class="pp-row ppt-form">
      <input class="pp-input ppt-host" v-model="host" placeholder="目标主机或 IP, 如 223.5.5.5"
        :disabled="running" @keyup.enter="dStart" />
      <label class="ppt-field">次数
        <input class="pp-input ppt-num" v-model.number="count" type="number" min="0" :disabled="running" title="0 = 持续探测" />
      </label>
      <label class="ppt-field">间隔ms
        <input class="pp-input ppt-num" v-model.number="intervalMs" type="number" min="100" step="100" :disabled="running" />
      </label>
      <label class="ppt-field">超时ms
        <input class="pp-input ppt-num" v-model.number="timeoutMs" type="number" min="100" step="100" :disabled="running" />
      </label>
      <button v-if="!running" class="pp-btn" @click="dStart">开始</button>
      <button v-else class="pp-btn stop" @click="dStop">停止</button>
    </div>

    <div class="ppt-cards">
      <div class="ppt-card"><span>已发</span><b>{{ stats.sent }}</b></div>
      <div class="ppt-card"><span>丢失</span><b :class="{ 'pp-loss': stats.lost > 0 }">{{ stats.lost }}</b></div>
      <div class="ppt-card"><span>丢包率</span><b>{{ lossPct.toFixed(1) }}%</b></div>
      <div class="ppt-card"><span>最小</span><b>{{ stats.minMs.toFixed(1) }}<i>ms</i></b></div>
      <div class="ppt-card"><span>最大</span><b>{{ stats.maxMs.toFixed(1) }}<i>ms</i></b></div>
      <div class="ppt-card"><span>平均</span><b>{{ stats.avgMs.toFixed(1) }}<i>ms</i></b></div>
      <div class="ppt-card"><span>抖动</span><b>{{ stats.jitterMs.toFixed(1) }}<i>ms</i></b></div>
    </div>

    <div class="ppt-chartwrap">
      <div class="ppt-chart-labels">
        <span v-for="g in gridRows" :key="g.label" :style="{ bottom: `calc(${((1 - (CH - 6 - Number(g.y)) / (CH - 16)) * 100).toFixed(1)}% - 7px)` }">{{ g.label }}ms</span>
      </div>
      <svg class="ppt-chart" :viewBox="`0 0 ${CW} ${CH}`" preserveAspectRatio="none">
        <line v-for="g in gridRows" :key="'g' + g.label" x1="0" :y1="g.y" :x2="CW" :y2="g.y"
          stroke="rgba(128,128,128,0.18)" stroke-width="1" vector-effect="non-scaling-stroke" />
        <line v-for="i in lossIdx" :key="'l' + i" :x1="xFor(i)" y1="4" :x2="xFor(i)" :y2="CH - 6"
          stroke="var(--danger-color)" stroke-width="1" stroke-dasharray="3 3" opacity="0.55" vector-effect="non-scaling-stroke" />
        <polyline v-if="packets.length > 1" :points="linePoints" fill="none" stroke="var(--primary-color)"
          stroke-width="1.5" vector-effect="non-scaling-stroke" />
        <template v-if="packets.length === 1 && packets[0].ok">
          <circle :cx="xFor(0)" :cy="yFor(packets[0].ms)" r="3" fill="var(--primary-color)" />
        </template>
      </svg>
      <div v-if="packets.length === 0" class="ppt-chart-empty">输入目标后点击开始; 延迟曲线实时绘制</div>
      <div v-if="mode === 'record' && viewRecord" class="ppt-record-bar">
        <span>历史记录 · {{ viewRecord.target }} · {{ fmtTime(viewRecord.startedAt) }}</span>
        <button class="pm-btn" @click="backToLive">返回实时</button>
      </div>
      <div v-if="running" class="ppt-live-badge">LIVE</div>
    </div>

    <div class="ppt-bottom">
      <div class="ppt-log">
        <div v-for="p in packets" :key="p.seq" class="pp-rowline">
          <span>#{{ p.seq }} {{ p.ok ? '回复' : '超时' }}</span>
          <span :class="p.ok ? (p.ms < 50 ? 'pp-ok' : p.ms < 150 ? '' : 'pp-warn') : 'pp-loss'">{{ p.ok ? p.ms.toFixed(1) + ' ms' : p.line }}</span>
        </div>
        <div v-if="packets.length === 0" class="pp-hint">暂无数据</div>
      </div>
      <div class="ppt-history">
        <div class="ppt-hist-head">
          <span>历史记录</span>
          <button class="pm-btn" @click="dNewTab">新建标签页</button>
        </div>
        <select class="ppt-select" size="10">
          <option v-for="r in historyList" :key="r.id" :value="r.id" @click="loadRecord(r.id)"
            :selected="mode === 'record' && viewRecord?.id === r.id">
            {{ fmtTime(r.startedAt) }} · {{ r.target }} · {{ r.sent }}包/{{ r.lost }}丢
          </option>
        </select>
        <button class="pm-btn danger" @click="dClear">清空历史</button>
      </div>
    </div>
  </div>
</template>
