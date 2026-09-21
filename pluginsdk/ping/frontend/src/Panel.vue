<script setup lang="ts">
// Ping 插件侧栏面板(componentId = "panel") —— 记录型: 展示持久化的探测历史。
// 点击记录 → 经 history.open 打开(或定位)Ping 工具标签页并载入该记录。
// 探测本体在工作台标签页执行(后端子协程), 面板只负责历史浏览。
import { ref, watch } from 'vue'
import { debounceClick } from './debounce'

interface HistoryItem {
  id: string
  target: string
  startedAt: string
  sent: number
  lost: number
  avgMs: number
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

const records = ref<HistoryItem[]>([])
const armedClear = ref(false)
let armedTimer: ReturnType<typeof setTimeout> | null = null

async function refresh() {
  try {
    const res = await props.ctx.call('history.list')
    records.value = Array.isArray(res?.records) ? res.records : []
  } catch {}
}

let offEvent: (() => void) | null = null
offEvent = props.ctx.onEvent((payload: any) => {
  // 探测完成 → 新记录落盘 → 刷新列表
  if (payload?.type === 'done') void refresh()
})

const dOpen = debounceClick((id: string) => { void open(id) })
const dNewTab = debounceClick(() => { void open('') })
const dClear = debounceClick(() => { void clear() })

async function open(id: string) {
  try {
    await props.ctx.call('history.open', id ? { id } : {})
  } catch (e: any) {
    props.ctx.toast(String(e?.message || e), 'error')
  }
}

async function clear() {
  if (!armedClear.value) {
    armedClear.value = true
    armedTimer = setTimeout(() => { armedClear.value = false }, 3000)
    return
  }
  armedClear.value = false
  if (armedTimer) clearTimeout(armedTimer)
  try {
    await props.ctx.call('history.clear')
    await refresh()
    props.ctx.toast('历史已清空', 'success')
  } catch (e: any) {
    props.ctx.toast(String(e?.message || e), 'error')
  }
}

function fmtTime(rfc: string): string {
  const d = new Date(rfc)
  if (isNaN(d.getTime())) return rfc
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

function lossPct(r: HistoryItem): string {
  return r.sent > 0 ? ((r.lost * 100) / r.sent).toFixed(0) : '0'
}

watch(() => props.active, (v) => { if (v) void refresh() }, { immediate: true })

// 组件保活(v-show), 挂载即拉一次
void refresh()
</script>

<template>
  <div class="aceshell-plugin-ping pph">
    <div class="pph-head">
      <span class="pph-title">探测历史</span>
      <button class="pm-btn" @click="dNewTab">新建标签页</button>
    </div>
    <div class="pph-list">
      <div v-for="r in records" :key="r.id" class="pph-item" @click="dOpen(r.id)">
        <div class="pph-target">{{ r.target }}</div>
        <div class="pph-meta">
          <span>{{ fmtTime(r.startedAt) }}</span>
          <span>{{ r.sent }} 包</span>
          <span :class="{ 'pp-loss': r.lost > 0 }">丢 {{ lossPct(r) }}%</span>
          <span v-if="r.avgMs > 0">均 {{ r.avgMs.toFixed(0) }}ms</span>
        </div>
      </div>
      <div v-if="records.length === 0" class="pp-hint">
        暂无历史记录。点击「新建标签页」发起探测, 会话完成后自动存入此处(持久化于插件数据目录)。
      </div>
    </div>
    <button class="pm-btn danger pph-clear" :class="{ armed: armedClear }" @click="dClear">
      {{ armedClear ? '再点一次确认清空' : '清空历史' }}
    </button>
  </div>
</template>
