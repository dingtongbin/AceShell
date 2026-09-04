<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { NSpin, NEmpty } from 'naive-ui'
import { connectVnc, type VncConnectionInfo } from '../composables/useVnc'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const props = defineProps<{
  conn: VncConnectionInfo
  active: boolean
}>()

const status = ref<'connecting' | 'connected' | 'ready' | 'error'>('connecting')
const errorMsg = ref('')
const hostEl = ref<HTMLElement | null>(null)
let rfb: any = null
let disposed = false
let resizeObserver: ResizeObserver | null = null
let resizeTimer: ReturnType<typeof setTimeout> | null = null

// 事件回调以实例身份校验:重连后旧实例的异步 disconnect 事件不得污染新会话状态
async function open() {
  if (!hostEl.value) return
  status.value = 'connecting'
  try {
    const inst = await connectVnc(hostEl.value, props.conn)
    if (disposed) {
      try { inst.disconnect() } catch {}
      return
    }
    rfb = inst
    const mine = () => rfb === inst && !disposed
    inst.addEventListener('connect', () => {
      if (!mine()) return
      status.value = 'connected'
      errorMsg.value = ''
    })
    inst.addEventListener('disconnect', (e: Event) => {
      if (!mine()) return
      const detail = (e as CustomEvent).detail || {}
      if (detail.clean) {
        status.value = 'ready'
        errorMsg.value = ''
      } else {
        status.value = 'error'
        errorMsg.value = detail.reason ? String(detail.reason) : t('vncTab.disconnected')
      }
    })
    inst.addEventListener('securityfailure', (e: Event) => {
      if (!mine()) return
      const detail = (e as CustomEvent).detail || {}
      status.value = 'error'
      errorMsg.value = detail.reason ? String(detail.reason) : t('vncTab.securityFailure')
    })
    inst.addEventListener('credentialsrequired', () => {
      if (!mine()) return
      status.value = 'error'
      errorMsg.value = t('vncTab.needPassword')
    })
    // 远端 → 本地剪贴板单向同步
    inst.addEventListener('clipboard', (e: Event) => {
      const text = (e as CustomEvent).detail?.text
      if (typeof text === 'string' && text) navigator.clipboard?.writeText(text).catch(() => {})
    })
  } catch (e: any) {
    status.value = 'error'
    errorMsg.value = e?.message ? String(e.message) : String(e)
  }
}

function closeRfb() {
  disposed = true
  try { rfb?.disconnect() } catch {}
  rfb = null
}

function retry() {
  closeRfb()
  disposed = false
  open()
}

function sendCtrlAltDel() {
  try { rfb?.sendCtrlAltDel() } catch {}
}

// 容器尺寸变化(拆分窗格/窗口调整)触发重协商:noVNC 仅监听 window resize,
// 重设 scaleViewport/resizeSession 促使其重新计算缩放并请求服务端调整分辨率。
function scheduleResize() {
  if (status.value !== 'connected' || !rfb) return
  if (resizeTimer) clearTimeout(resizeTimer)
  resizeTimer = setTimeout(() => {
    resizeTimer = null
    try {
      rfb.scaleViewport = false
      rfb.scaleViewport = true
      rfb.resizeSession = true
    } catch {}
  }, 150)
}

onMounted(() => {
  open()
  if (hostEl.value) {
    resizeObserver = new ResizeObserver(scheduleResize)
    resizeObserver.observe(hostEl.value)
  }
})

onBeforeUnmount(() => {
  if (resizeTimer) clearTimeout(resizeTimer)
  resizeObserver?.disconnect()
  closeRfb()
  // 注意:不在此释放桥接 token。跨 pane 拖动标签页会销毁重建本组件,
  // 重建后需用同一 token 重连 WS;token 由 TabManager 在标签页真正关闭时释放。
})
</script>

<template>
  <div class="vnc-tab">
    <div ref="hostEl" class="vnc-host"></div>
    <div v-if="status === 'connected'" class="vnc-tools">
      <button class="vnc-tool-btn" @click="sendCtrlAltDel">{{ t('vncTab.sendCtrlAltDel') }}</button>
    </div>
    <div v-if="status === 'connecting'" class="vnc-overlay">
      <n-spin size="small" />
      <span class="vnc-overlay-text">{{ t('vncTab.connecting') }}</span>
    </div>
    <div v-else-if="status === 'ready' || status === 'error'" class="vnc-overlay">
      <n-empty :description="status === 'ready' ? t('vncTab.disconnected') : t('vncTab.connectFailed')" size="small" />
      <div v-if="errorMsg" class="vnc-error">{{ errorMsg }}</div>
      <button class="vnc-retry" @click="retry">{{ t('common.retry') }}</button>
    </div>
  </div>
</template>

<style scoped>
.vnc-tab { position: relative; width: 100%; height: 100%; overflow: hidden; }
.vnc-host { position: absolute; inset: 0; }
.vnc-tools {
  position: absolute; top: 8px; right: 12px; z-index: 2;
  display: flex; gap: 6px;
}
.vnc-tool-btn {
  padding: 3px 10px; cursor: pointer; border-radius: 4px;
  border: 1px solid #555; background: rgba(30, 30, 30, 0.75); color: inherit; font-size: 12px;
}
.vnc-tool-btn:hover { border-color: #888; }
.vnc-overlay {
  position: absolute; inset: 0; display: flex; flex-direction: column;
  align-items: center; justify-content: center; gap: 12px;
  background: var(--bg-color, #1e1e1e); color: var(--text-color, #d4d4d4);
  font-size: 13px; z-index: 3; pointer-events: auto;
}
.vnc-error { max-width: 90%; word-break: break-all; text-align: center; color: #e45858; }
.vnc-retry { padding: 4px 16px; cursor: pointer; border-radius: 4px; border: 1px solid #555; background: transparent; color: inherit; }
.vnc-retry:hover { border-color: #888; }
</style>
