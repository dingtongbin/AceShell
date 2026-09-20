<script setup lang="ts">
// Ping 插件侧栏面板(componentId = "panel")。
// 经 ctx.call('start'/'stop') 控制探测会话, ctx.onEvent 订阅流式结果(EmitUIEvent 通道)。
import { ref, onBeforeUnmount, nextTick } from 'vue'

interface PingResult {
  seq: number
  ok: boolean
  ms: number
  line: string
}

const props = defineProps<{
  ctx: {
    pluginID: string
    call: (method: string, args?: Record<string, any>) => Promise<any>
    onEvent: (cb: (payload: any) => void) => () => void
    toast: (message: string, level?: 'info' | 'warning' | 'error' | 'success') => void
    theme: { isDark: { value: boolean }; accent: { value: string } }
  }
  view: { pluginID: string; viewID: string; title: string }
  active: boolean
}>()

const hostInput = ref('')
const running = ref(false)
const results = ref<PingResult[]>([])
const summary = ref('')
const logEl = ref<HTMLElement | null>(null)

let offEvent: (() => void) | null = null
const MAX_ROWS = 200

function onEvent(payload: any) {
  if (payload?.type === 'result') {
    results.value = [...results.value.slice(-(MAX_ROWS - 1)), { seq: payload.seq, ok: !!payload.ok, ms: payload.ms || 0, line: String(payload.line || '') }]
    nextTick(() => { logEl.value?.scrollTo({ top: logEl.value.scrollHeight }) })
  } else if (payload?.type === 'done') {
    running.value = false
    summary.value = String(payload.summary || '')
  } else if (payload?.type === 'stopped') {
    running.value = false
  }
}
offEvent = props.ctx.onEvent(onEvent)

function latencyClass(r: PingResult): string {
  if (!r.ok) return 'pp-loss'
  if (r.ms < 50) return 'pp-ok'
  if (r.ms < 150) return ''
  return 'pp-loss'
}

async function start() {
  const host = hostInput.value.trim()
  if (!host) { props.ctx.toast('请输入目标主机', 'warning'); return }
  try {
    await props.ctx.call('start', { host, count: 0, intervalMs: 1000, timeoutMs: 2000 })
    results.value = []
    summary.value = ''
    running.value = true
  } catch (e: any) {
    props.ctx.toast(String(e?.message || e), 'error')
  }
}

async function stop() {
  try { await props.ctx.call('stop') } catch {}
}

const sent = ref(0)
const lost = ref(0)
// 统计由前端按流式结果聚合(插件 done 事件也会给汇总)
function recount() {
  sent.value = results.value.length
  lost.value = results.value.filter(r => !r.ok).length
}

onBeforeUnmount(() => { offEvent?.(); stop() })
</script>

<template>
  <div class="aceshell-plugin-ping">
    <div class="pp-row">
      <input class="pp-input" v-model="hostInput" placeholder="目标主机或 IP, 如 223.5.5.5"
        :disabled="running" @keyup.enter="start" />
      <button v-if="!running" class="pp-btn" @click="start">开始</button>
      <button v-else class="pp-btn stop" @click="stop">停止</button>
    </div>
    <div class="pp-stats">
      <span>已发 <b>{{ results.length }}</b></span>
      <span>丢失 <b>{{ results.filter(r => !r.ok).length }}</b></span>
      <span v-if="summary"><b>{{ summary }}</b></span>
    </div>
    <div class="pp-log" ref="logEl">
      <div v-for="r in results" :key="r.seq" class="pp-rowline">
        <span>#{{ r.seq }} {{ r.ok ? '回复' : '超时' }}</span>
        <span :class="latencyClass(r)">{{ r.ok ? r.ms.toFixed(1) + ' ms' : r.line }}</span>
      </div>
      <div v-if="results.length === 0" class="pp-hint">输入目标后点击开始; 逐包结果实时推送(EmitUIEvent 通道演示)</div>
    </div>
    <div class="pp-hint">持续探测每秒一包; 停止或关闭面板自动结束会话</div>
  </div>
</template>
