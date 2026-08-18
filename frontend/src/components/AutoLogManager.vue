<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { NIcon, NEmpty, NInput, NModal } from 'naive-ui'
import { CaretForwardOutline, DocumentOutline, TerminalOutline, RadioOutline, SearchOutline } from '@vicons/ionicons5'
import { GetLogTree, GetLogContent, GetLogMeta } from '../../bindings/changeme/internal/services/logservice.js'

interface LogNode {
  name: string
  path: string
  isDir: boolean
  protocol?: string
  children?: LogNode[]
  expanded?: boolean
  depth?: number
}

const tree = ref<LogNode[]>([])
const flatList = ref<LogNode[]>([])
const searchQuery = ref('')
const showContent = ref(false)
const logContent = ref('')
const logMeta = ref<any>(null)
const activeLogId = ref('')

// 日志查看器行号
const logLineRef = ref<HTMLElement | null>(null)
const logLineNumbers = ref('')
let logLineCount = 0
function buildLogLineNumbers() {
  const count = logContent.value.split('\n').length
  if (count === logLineCount) return
  logLineCount = count
  if (count > 100000) { logLineNumbers.value = ''; return }
  const parts = new Array(count)
  for (let i = 0; i < count; i++) parts[i] = String(i + 1)
  logLineNumbers.value = parts.join('\n')
}
function syncLogScroll(e: Event) {
  const pre = e.target as HTMLElement
  if (logLineRef.value) logLineRef.value.scrollTop = pre.scrollTop
}

const filteredList = computed(() => {
  if (!searchQuery.value) return flatList.value
  const q = searchQuery.value.toLowerCase()
  return flatList.value.filter(n => n.name.toLowerCase().includes(q))
})

async function loadTree() {
  try {
    const raw = await GetLogTree()
    const parsed = JSON.parse(raw) || []
    tree.value = parsed.map((n: LogNode) => ({ ...n, expanded: false }))
    flattenTree()
  } catch {
    tree.value = []
    flatList.value = []
  }
}

function flattenTree() {
  flatList.value = []
  function walk(nodes: LogNode[], depth: number) {
    nodes.forEach((node) => {
      if (depth === 0 || !node.isDir || node.expanded) {
        flatList.value.push({ ...node, depth } as any)
        if (node.isDir && node.expanded && node.children?.length) {
          walk(node.children, depth + 1)
        }
      }
    })
  }
  walk(tree.value, 0)
}

function toggleFolder(path: string) {
  const find = (nodes: LogNode[]): LogNode | undefined => {
    for (const n of nodes) {
      if (n.path === path) return n
      if (n.children) { const r = find(n.children); if (r) return r }
    }
    return undefined
  }
  const node = find(tree.value)
  if (node) {
    node.expanded = !node.expanded
    flattenTree()
  }
}

async function viewLog(node: LogNode) {
  if (node.isDir) return
  activeLogId.value = node.path
  try {
    const [content, meta] = await Promise.all([
      GetLogContent(node.path),
      GetLogMeta(node.path)
    ])
    logContent.value = content || ''
    logMeta.value = JSON.parse(meta)
    logLineCount = 0
    buildLogLineNumbers()
    showContent.value = true
  } catch {}
}

function getProtoIcon(p?: string) {
  switch (p) {
    case 'ssh': return TerminalOutline
    case 'telnet': return TerminalOutline
    case 'serial': return RadioOutline
    case 'shell': return TerminalOutline
    default: return DocumentOutline
  }
}

function getProtoColor(p?: string) {
  switch (p) {
    case 'ssh': return '#4ec9b0'
    case 'telnet': return '#569cd6'
    case 'serial': return '#c586c0'
    case 'shell': return '#6e9fc7'
    default: return 'var(--icon-color)'
  }
}

onMounted(loadTree)
</script>

<template>
  <div class="autolog-panel">
    <n-input v-model:value="searchQuery" size="tiny" placeholder="搜索日志..." clearable class="autolog-search-input">
      <template #prefix><n-icon :size="14" :component="SearchOutline" /></template>
    </n-input>
    <div class="autolog-list">
      <div v-if="filteredList.length === 0" class="autolog-empty">
        <n-empty description="暂无日志记录" size="small" />
      </div>
      <div v-for="node in filteredList" :key="node.path" class="autolog-row" :style="{ paddingLeft: (8 + (node.depth || 0) * 16) + 'px' }" @click="node.isDir ? toggleFolder(node.path) : viewLog(node)">
        <n-icon v-if="node.isDir" :size="12" :component="CaretForwardOutline" class="autolog-arrow" :class="{ rotated: node.expanded }" />
        <n-icon :size="node.isDir ? 14 : 14" :component="getProtoIcon(node.protocol)" :style="{ color: getProtoColor(node.protocol) }" class="autolog-icon" />
        <span class="autolog-name">{{ node.name }}</span>
      </div>
    </div>

    <n-modal v-model:show="showContent" class="log-dialog" preset="dialog" :show-icon="false" :mask-closable="false" style="width: 820px; max-width: 92vw">
      <template #header><span class="log-title">日志详情</span></template>
      <div v-if="logMeta" class="log-meta">
        <span v-if="logMeta.startTime">开始: {{ logMeta.startTime }}</span>
        <span v-if="logMeta.endTime"> | 结束: {{ logMeta.endTime }}</span>
        <span v-if="logMeta.totalLines"> | 行数: {{ logMeta.totalLines }}</span>
        <span v-if="logMeta.totalBytes"> | 大小: {{ (logMeta.totalBytes / 1024).toFixed(1) }}KB</span>
      </div>
      <div class="log-viewer">
        <div ref="logLineRef" class="log-lines">{{ logLineNumbers }}</div>
        <div class="log-body">
          <pre class="log-pre" @scroll="syncLogScroll">{{ logContent }}</pre>
        </div>
      </div>
      <template #action>
        <n-button @click="showContent = false">关闭</n-button>
      </template>
    </n-modal>
  </div>
</template>

<style scoped>
.autolog-panel { width: 100%; height: 100%; display: flex; flex-direction: column; background: var(--body-bg); overflow: hidden; }
.autolog-search-input { flex-shrink: 0; width: 98%; align-self: center; }
.autolog-search-input :deep(.n-input) { border-radius: 0; }
.autolog-list { flex: 1; overflow-y: auto; padding: 4px 0; }
.autolog-empty { height: 100%; display: flex; align-items: center; justify-content: center; }
.autolog-row { display: flex; align-items: center; gap: 6px; height: 28px; padding: 0 10px; cursor: pointer; transition: background 0.1s; text-align: left; }
.autolog-row:hover { background: rgba(255,255,255,0.05); }
.autolog-arrow { flex-shrink: 0; color: var(--icon-color); transition: transform 0.15s; }
.autolog-arrow.rotated { transform: rotate(90deg); }
.autolog-icon { flex-shrink: 0; width: 14px; text-align: left; }
.autolog-name { flex: 1; min-width: 0; font-size: 13px; color: var(--text-color); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; text-align: left; }
.log-title { font-size: 14px; font-weight: 600; }
.log-meta { margin-bottom: 8px; font-size: 12px; color: var(--icon-color); }
.log-viewer {
  display: flex; height: 480px; overflow: hidden;
  background: var(--body-bg); border-radius: 4px; border: 1px solid var(--border-color);
}
.log-lines {
  flex-shrink: 0; width: 44px; overflow: hidden; padding: 6px 6px 6px 0; text-align: right;
  font-family: 'Cascadia Code', 'Fira Code', Consolas, 'Courier New', monospace;
  font-size: 13px; line-height: 1.55; white-space: pre;
  color: var(--icon-color); background: var(--sidebar-bg); border-right: 1px solid var(--border-color);
  user-select: none;
}
.log-body { position: relative; flex: 1; overflow: hidden; min-width: 0; }
.log-pre {
  position: absolute; inset: 0; margin: 0; overflow: auto; padding: 6px 10px;
  font-family: 'Cascadia Code', 'Fira Code', Consolas, 'Courier New', monospace;
  font-size: 13px; line-height: 1.55; white-space: pre-wrap; word-break: break-all;
  color: var(--text-color); background: var(--body-bg);
}
</style>

<style>
/* 弹窗渲染在 body 下,scoped 无效,用全局类名覆盖 */
.log-dialog .n-dialog__content { padding: 12px; }
.log-dialog .n-dialog__header { padding-top: 14px; }
.log-dialog .n-dialog { width: 820px; max-width: 92vw; }
</style>
