<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { NSpin, NEmpty } from 'naive-ui'
import { ensureRdpRuntime, rdpUI, connectRdp, formatIronError, type RdpConnectionInfo } from '../composables/useRdp'
import type { UserInteraction } from '@devolutions/iron-remote-desktop'

const props = defineProps<{
  conn: RdpConnectionInfo
  active: boolean
}>()

const rdpModule = ref<typeof import('@devolutions/iron-remote-desktop-rdp').Backend | null>(null)
const status = ref<'loading' | 'ready' | 'connecting' | 'connected' | 'error'>('loading')
const errorMsg = ref('')
const hostEl = ref<HTMLElement | null>(null)
let ui: UserInteraction | null = null
let running = false
let resizeObserver: ResizeObserver | null = null
let runPromise: Promise<unknown> | null = null
let resizeTimer: ReturnType<typeof setTimeout> | null = null
let sessionSize = { width: 1280, height: 800 }

// 计算远程桌面分辨率:容器 CSS 尺寸(逻辑像素)。
// 历史:曾乘以 devicePixelRatio 做物理像素超采样以缓解细线弯折,但弯折根因是
// xrdp 0.9 的 32bpp 压缩格式与 IronRDP 不兼容(已通过 max_bpp=24 解决),超采样
// 不再必要;且 xrdp 0.9 对超过容器尺寸的分辨率建立会话不可靠(位图铺不满 canvas
// 导致右下黑边),故按 1:1 请求。
function computeSessionSize(): { width: number; height: number } {
  const el = hostEl.value
  return {
    width: Math.max(320, Math.round(el?.clientWidth || 1280)),
    height: Math.max(240, Math.round(el?.clientHeight || 800)),
  }
}

// 应用物理 1:1 缩放:fit 显示已把位图铺满容器(逻辑像素),再按 1/DPR 反向缩放后,
// 画面以服务器原生像素大小呈现(等价 RDP 会话 100% 缩放),不随本地系统缩放放大。
// 容器多余区域为黑边,可接受。
function applyDprScale() {
  const dpr = window.devicePixelRatio || 1
  hostEl.value?.style.setProperty('--rdp-dpr', String(dpr))
}

async function connect() {
  const mod = rdpModule.value
  if (!mod || !ui || running) return
  running = true
  status.value = 'connecting'
  sessionSize = computeSessionSize()
  try {
    const info = await connectRdp(ui, props.conn, sessionSize)
    status.value = 'connected'
    applyDprScale()
    ui.setVisibility(true)
    runPromise = info.run().catch(() => {})
    runPromise.finally(() => { if (status.value === 'connected') status.value = 'ready' })
  } catch (e: any) {
    status.value = 'error'
    errorMsg.value = formatIronError(e)
    running = false
  }
}

// 容器尺寸变化只更新显示缩放(DPR),不重新协商服务器分辨率。
// xrdp 0.9 的动态分辨率切换有已知 bug(deactivate-reactivate 不可靠),
// 重新协商会导致画面拉伸/黑屏;固定会话分辨率后,窗口变化仅改变黑边大小。
function scheduleResize() {
  if (resizeTimer) clearTimeout(resizeTimer)
  resizeTimer = setTimeout(() => {
    resizeTimer = null
    applyDprScale()
  }, 150)
}

function onReady(e: Event) {
  const detail = (e as CustomEvent).detail
  ui = detail?.irgUserInteraction ?? null
  if (!ui) {
    status.value = 'error'
    errorMsg.value = 'IronRDP 组件就绪事件缺少 UserInteraction'
    return
  }
  rdpUI.value = ui
  ui.onWarningCallback((msg) => { console.warn('[RDP]', msg) })
  status.value = 'ready'
  nextTick(connect)
}

onMounted(async () => {
  try {
    rdpModule.value = await ensureRdpRuntime()
  } catch (e: any) {
    status.value = 'error'
    errorMsg.value = 'IronRDP 运行时加载失败: ' + formatIronError(e)
    return
  }
  applyDprScale()
  if (hostEl.value) {
    resizeObserver = new ResizeObserver(scheduleResize)
    resizeObserver.observe(hostEl.value)
  }
})

onBeforeUnmount(() => {
  if (resizeTimer) clearTimeout(resizeTimer)
  resizeObserver?.disconnect()
  ui?.shutdown()
  // 注意:不在此释放桥接 token。跨 pane 拖动标签页会销毁重建本组件,
  // 重建后需用同一 token 重连 WS;token 由 TabManager 在标签页真正关闭时释放。
})

function retry() {
  errorMsg.value = ''
  running = false
  connect()
}
</script>

<template>
  <div class="rdp-tab">
    <div ref="hostEl" class="rdp-host">
      <iron-remote-desktop
        v-if="rdpModule"
        :module="rdpModule"
        scale="fit"
        flexcenter="true"
        @ready="onReady"
      />
    </div>
    <div v-if="status === 'loading'" class="rdp-overlay">
      <n-spin size="small" />
      <span class="rdp-overlay-text">正在加载 IronRDP 运行时…</span>
    </div>
    <div v-else-if="status === 'ready' || status === 'connecting'" class="rdp-overlay">
      <n-spin size="small" />
      <span class="rdp-overlay-text">{{ status === 'ready' ? '准备连接…' : '正在连接 RDP 服务器…' }}</span>
    </div>
    <div v-else-if="status === 'error'" class="rdp-overlay">
      <n-empty description="连接失败" size="small" />
      <div class="rdp-error">{{ errorMsg }}</div>
      <button class="rdp-retry" @click="retry">重试</button>
    </div>
  </div>
</template>

<style scoped>
.rdp-tab { position: relative; width: 100%; height: 100%; overflow: hidden; }
.rdp-host { position: absolute; inset: 0; }
/* 物理 1:1 显示:fit 铺满容器后按 1/DPR 反向缩放,画面以服务器原生像素大小呈现,
   不随本地系统缩放(125%/150%)放大;容器多余区域为黑边。 */
.rdp-host :deep(iron-remote-desktop) {
  transform: scale(calc(1 / var(--rdp-dpr, 1)));
  transform-origin: center;
}
.rdp-overlay {
  position: absolute; inset: 0; display: flex; flex-direction: column;
  align-items: center; justify-content: center; gap: 12px;
  background: var(--bg-color, #1e1e1e); color: var(--text-color, #d4d4d4);
  font-size: 13px; z-index: 2; pointer-events: auto;
}
.rdp-error { max-width: 90%; word-break: break-all; text-align: center; color: #e45858; }
.rdp-retry { padding: 4px 16px; cursor: pointer; border-radius: 4px; border: 1px solid #555; background: transparent; color: inherit; }
.rdp-retry:hover { border-color: #888; }
</style>