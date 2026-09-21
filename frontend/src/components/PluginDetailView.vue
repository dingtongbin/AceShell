<script setup lang="ts">
// 插件详情标签页(宿主自有组件, 非插件 ESM)。
// 数据直读注册表 ref: 随 plugin-registry-changed / 失效协议自动刷新,
// 卸载/禁用/重载后详情内容自动跟随, 无需手动刷新。
// 每个插件至多一个详情标签页(TabManager.openPluginDetailTab 去重定位)。
import { ref, computed, watch } from 'vue'
import { NIcon, NTag, NSwitch, NButton, NPopconfirm, useMessage } from 'naive-ui'
import {
  ReloadOutline,
  TrashOutline,
  RefreshCircleOutline,
  PulseOutline,
  DocumentTextOutline,
} from '@vicons/ionicons5'
import { useI18n } from 'vue-i18n'
import { usePlugins, type PluginSummary } from '../composables/usePluginBridge'
import { PluginSetEnabled, PluginReload, PluginUninstall, PluginRestoreBundled } from '../../bindings/changeme/internal/services/pluginservice.js'
import { renderMd } from '../utils/markdown'
import { debounceClick } from '../utils/debounce'

const props = defineProps<{ pluginID: string }>()

const { t, locale } = useI18n()
const message = useMessage()
const { plugins } = usePlugins()

// 操作防抖: id → 操作名, 期间控件 loading 且忽略重复点击
const busy = ref<string>('')
// 按钮统一 300ms 防抖
const dReload = debounceClick(() => doReload())
const dUninstall = debounceClick(() => doUninstall())
const dRestore = debounceClick(() => doRestore())

const summary = computed<PluginSummary | undefined>(() =>
  plugins.value.find(p => p.id === props.pluginID))

const views = computed(() => summary.value?.views ?? [])

function statusText(p: PluginSummary): string {
  const key = { running: 'plugins.statusRunning', starting: 'plugins.statusStarting', stopped: 'plugins.statusStopped', error: 'plugins.statusError', disabled: 'plugins.statusDisabled', uninstalled: 'plugins.statusUninstalled' }[p.status] || 'plugins.statusStopped'
  const text = t(key)
  return p.error ? `${text} · ${p.error}` : text
}

async function withBusy(op: string, fn: () => Promise<void>) {
  if (busy.value) return
  busy.value = op
  try {
    await fn()
  } catch (e: any) {
    message.error(String(e?.message || e))
  } finally {
    setTimeout(() => { if (busy.value === op) busy.value = '' }, 800)
  }
}

function toggleEnabled(v: boolean) {
  withBusy('toggle', async () => {
    await PluginSetEnabled(props.pluginID, v)
  })
}

function doReload() {
  withBusy('reload', async () => {
    await PluginReload(props.pluginID)
    message.success(t('plugins.reloaded', { id: props.pluginID }))
  })
}

function doUninstall() {
  withBusy('uninstall', async () => {
    const res = JSON.parse(await PluginUninstall(props.pluginID))
    if (res?.error) { message.error(String(res.error)); return }
    message.success(t('plugins.uninstalledOk', { id: props.pluginID }))
  })
}

function doRestore() {
  withBusy('restore', async () => {
    const res = JSON.parse(await PluginRestoreBundled(props.pluginID))
    if (res?.error) { message.error(String(res.error)); return }
    message.success(t('plugins.restoredOk', { id: props.pluginID }))
  })
}

// ==================== 文档(docs 钩子) ====================
// plugin.json 的 docs: { "<locale>": "相对路径.md", "default": "..." }。
// 解析顺序: 当前语言 → default → 任一可用项; md 经插件资产服务同源拉取,
// renderMd(marked + DOMPurify)渲染。语言切换或注册表刷新后自动重取。

const docPath = computed(() => {
  const docs = summary.value?.docs
  if (!docs) return ''
  return docs[locale.value] || docs['default'] || Object.values(docs).find(v => !!v) || ''
})

const docHtml = ref('')
const docLoading = ref(false)
const docFailed = ref(false)
let docSeq = 0

async function loadDoc() {
  const rel = docPath.value
  if (!rel) {
    docHtml.value = ''
    docLoading.value = false
    docFailed.value = false
    return
  }
  const seq = ++docSeq
  docLoading.value = true
  docFailed.value = false
  try {
    const url = `/plugins/${encodeURIComponent(props.pluginID)}/${rel.split('/').map(encodeURIComponent).join('/')}`
    const res = await fetch(url)
    if (!res.ok) throw new Error(String(res.status))
    const text = await res.text()
    if (seq !== docSeq) return
    docHtml.value = renderMd(text)
    docLoading.value = false
  } catch {
    if (seq !== docSeq) return
    docHtml.value = ''
    docLoading.value = false
    docFailed.value = true
  }
}

watch([docPath, locale], loadDoc, { immediate: true })
</script>

<template>
  <div class="pdetail">
    <template v-if="summary">
      <div class="pdetail-head">
        <img v-if="summary.icon" class="pdetail-icon" :src="summary.icon" alt="" />
        <div v-else class="pdetail-icon pdetail-icon-ph">{{ summary.displayName.slice(0, 1) }}</div>
        <div class="pdetail-title">
        <div class="pdetail-name">
          {{ summary.displayName }}
          <n-tag size="tiny" :bordered="false">v{{ summary.version || '-' }}</n-tag>
          <n-tag v-if="summary.bundled" size="tiny" :bordered="false" type="info">{{ t('plugins.bundled') }}</n-tag>
          <n-tag v-for="c in summary.capabilities || []" :key="c" size="tiny" :bordered="false" type="warning">{{ c }}</n-tag>
        </div>
          <div class="pdetail-id">{{ summary.id }}</div>
        </div>
      </div>

      <div class="pdetail-actions">
        <template v-if="summary.status === 'uninstalled'">
          <n-button size="small" type="primary" :loading="busy === 'restore'" @click="dRestore">
            <template #icon><n-icon :component="RefreshCircleOutline" /></template>
            {{ t('plugins.restore') }}
          </n-button>
        </template>
        <template v-else>
          <div class="pdetail-switch">
            <span class="pdetail-switch-label">{{ t('plugins.enabled') }}</span>
            <n-switch size="small" :loading="busy === 'toggle'" :value="summary.status !== 'disabled'" @update:value="toggleEnabled" />
          </div>
          <n-button size="small" :loading="busy === 'reload'" :disabled="summary.status === 'disabled'" @click="dReload">
            <template #icon><n-icon :component="ReloadOutline" /></template>
            {{ t('plugins.reload') }}
          </n-button>
          <n-popconfirm placement="bottom" :width="260" :show-icon="false" @positive-click="dUninstall">
            <template #trigger>
              <n-button size="small" type="error" ghost :loading="busy === 'uninstall'">
                <template #icon><n-icon :component="TrashOutline" /></template>
                {{ t('plugins.uninstall') }}
              </n-button>
            </template>
            {{ t('plugins.uninstallConfirm', { id: summary.id }) }}
          </n-popconfirm>
        </template>
      </div>

      <div class="pdetail-status" :class="{ 'pdetail-status-error': summary.status === 'error' }">
        {{ statusText(summary) }}
      </div>

      <div class="pdetail-section" v-if="docPath || docHtml || docFailed">
        <div class="pdetail-section-title">
          <n-icon :size="12" :component="DocumentTextOutline" class="pdetail-section-icon" />
          {{ t('plugins.detailDocs') }}
        </div>
        <div v-if="docLoading" class="pdetail-muted">{{ t('plugins.loading') }}</div>
        <div v-else-if="docFailed" class="pdetail-muted">{{ t('plugins.docsFailed') }}</div>
        <div v-else class="pdetail-md" v-html="docHtml"></div>
      </div>

      <div class="pdetail-section">
        <div class="pdetail-section-title">{{ t('plugins.detailViews') }}</div>
        <div v-if="views.length === 0" class="pdetail-muted">{{ t('plugins.emptyViews') }}</div>
        <div v-for="v in views" :key="v.id" class="pdetail-view">
          <img v-if="v.icon" class="pdetail-view-icon" :src="v.icon" alt="" />
          <n-icon v-else :size="14" :component="PulseOutline" class="pdetail-view-icon" />
          <div class="pdetail-view-main">
            <div class="pdetail-view-name">{{ v.title }}</div>
            <div class="pdetail-view-comp">{{ v.componentId }}</div>
          </div>
        </div>
      </div>
    </template>
    <div v-else class="pdetail-muted pdetail-empty">{{ t('plugins.notFound') }}</div>
  </div>
</template>

<style scoped>
.pdetail {
  height: 100%;
  overflow: auto;
  padding: 16px;
  box-sizing: border-box;
  color: var(--text-color);
  /* 全局脚手架残留 #app { text-align: center }, 详情页必须显式左对齐 */
  text-align: left;
}

.pdetail-head {
  display: flex;
  gap: 12px;
  align-items: center;
}

.pdetail-icon {
  width: 52px;
  height: 52px;
  object-fit: contain;
  flex-shrink: 0;
}

.pdetail-icon-ph {
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 10px;
  background: var(--primary-color);
  color: #fff;
  font-size: 22px;
  font-weight: 500;
}

.pdetail-name {
  font-size: 16px;
  font-weight: 500;
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}

.pdetail-id {
  font-size: 12px;
  color: var(--icon-color);
  margin-top: 2px;
}

.pdetail-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 14px;
  flex-wrap: wrap;
}

.pdetail-switch {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--icon-color);
}

.pdetail-status {
  margin-top: 12px;
  font-size: 12px;
  color: var(--icon-color);
}

.pdetail-status-error {
  color: var(--danger-color, #e24b4b);
}

.pdetail-section {
  margin-top: 16px;
}

.pdetail-section-title {
  font-size: 12px;
  font-weight: 500;
  color: var(--icon-color);
  text-transform: uppercase;
  letter-spacing: 0.04em;
  margin-bottom: 8px;
  display: flex;
  align-items: center;
  gap: 5px;
}

.pdetail-section-icon {
  color: var(--icon-color);
}

.pdetail-view {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  border: 1px solid var(--border-color);
  border-radius: 6px;
  margin-bottom: 6px;
}

.pdetail-view-icon {
  width: 16px;
  height: 16px;
  flex-shrink: 0;
  color: var(--icon-color);
}

.pdetail-view-name {
  font-size: 13px;
}

.pdetail-view-comp {
  font-size: 11px;
  color: var(--icon-color);
  font-family: var(--font-mono, monospace);
}

.pdetail-muted {
  font-size: 12px;
  color: var(--icon-color);
}

.pdetail-empty {
  padding: 32px 0;
  text-align: center;
}
</style>

<!-- 非 scoped: v-html 注入的 markdown 内容不带 data-v 标记, scoped 样式够不到;
     以 .pdetail-md 命名空间隔离, 观感对齐 github-markdown-css, 颜色走应用主题变量。 -->
<style>
.pdetail-md {
  text-align: left;
  font-size: 14px;
  line-height: 1.6;
  word-break: break-word;
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Noto Sans", Helvetica, Arial,
    "PingFang SC", "Microsoft YaHei", sans-serif;
}
.pdetail-md > *:first-child { margin-top: 0; }
.pdetail-md > *:last-child { margin-bottom: 0; }
.pdetail-md h1, .pdetail-md h2, .pdetail-md h3,
.pdetail-md h4, .pdetail-md h5, .pdetail-md h6 {
  margin: 20px 0 12px;
  font-weight: 600;
  line-height: 1.25;
  text-align: left;
}
.pdetail-md h1 {
  font-size: 1.5em;
  padding-bottom: .3em;
  border-bottom: 1px solid var(--border-color);
}
.pdetail-md h2 {
  font-size: 1.25em;
  padding-bottom: .3em;
  border-bottom: 1px solid var(--border-color);
}
.pdetail-md h3 { font-size: 1.05em; }
.pdetail-md h4, .pdetail-md h5, .pdetail-md h6 { font-size: 1em; }
.pdetail-md p { margin: 0 0 12px; text-align: left; }
.pdetail-md a { color: var(--primary-color); text-decoration: none; }
.pdetail-md a:hover { text-decoration: underline; }
.pdetail-md ul, .pdetail-md ol { margin: 0 0 12px; padding-left: 1.8em; text-align: left; }
.pdetail-md li { margin: 3px 0; text-align: left; }
.pdetail-md li::marker { color: var(--icon-color); }
.pdetail-md strong { font-weight: 600; }
.pdetail-md code {
  font-family: ui-monospace, SFMono-Regular, Consolas, "Courier New", monospace;
  font-size: 85%;
  padding: .2em .4em;
  background: rgba(128, 128, 128, 0.16);
  border-radius: 6px;
  text-align: left;
}
.pdetail-md pre {
  padding: 12px;
  overflow-x: auto;
  background: rgba(128, 128, 128, 0.10);
  border-radius: 6px;
  line-height: 1.45;
  text-align: left;
}
.pdetail-md pre code {
  padding: 0;
  background: transparent;
  font-size: 100%;
}
.pdetail-md table {
  border-spacing: 0;
  border-collapse: collapse;
  margin: 0 0 12px;
  text-align: left;
}
.pdetail-md th, .pdetail-md td {
  padding: 5px 12px;
  border: 1px solid var(--border-color);
  text-align: left;
}
.pdetail-md th {
  font-weight: 500;
  background: rgba(128, 128, 128, 0.06);
}
.pdetail-md tr:nth-child(2n) td { background: rgba(128, 128, 128, 0.06); }
.pdetail-md blockquote {
  margin: 0 0 12px;
  padding: 0 1em;
  color: var(--icon-color);
  border-left: .25em solid var(--border-color);
  text-align: left;
}
.pdetail-md hr {
  height: .25em;
  margin: 16px 0;
  background: var(--border-color);
  border: 0;
}
.pdetail-md img { max-width: 100%; }
</style>
