<script setup lang="ts">
import { ref, onMounted, onUnmounted, nextTick, watch } from 'vue'
import { useMessage } from 'naive-ui'
import { ReadFile, WriteFile } from '../../bindings/changeme/internal/services/scriptfileservice.js'
import { GetConfig } from '../../bindings/changeme/internal/services/configservice.js'
import hljs from 'highlight.js'
import 'highlight.js/styles/vs2015.css'

export interface FileEditorApi {
  isDirty: () => boolean
  save: () => Promise<boolean>
}

const props = defineProps<{
  filePath: string
  fileName: string
  active?: boolean
  onApiReady?: (api: FileEditorApi) => void
  onDirtyChange?: (dirty: boolean) => void
  onCursorChange?: (row: number, col: number) => void
}>()

const message = useMessage()
const content = ref('')
const contentHtml = ref('')
const loading = ref(true)
const dirty = ref(false)
const saved = ref(false)
const autoSave = ref(true)

const editorEl = ref<HTMLElement | null>(null)
const taRef = ref<HTMLTextAreaElement | null>(null)
const lineNumRef = ref<HTMLElement | null>(null)
const lineNumbers = ref('1')
let lastLineCount = 1
let autoSaveTimer: ReturnType<typeof setTimeout> | null = null

const AUTO_SAVE_DELAY = 5000

function getLang(name: string): string {
  const m: any = { js: 'javascript', ts: 'typescript', py: 'python', go: 'go', html: 'xml', css: 'css', json: 'json', xml: 'xml', yaml: 'yaml', yml: 'yaml', toml: 'ini', md: 'markdown', sh: 'bash', sql: 'sql', txt: 'plaintext', log: 'plaintext', ini: 'ini', conf: 'ini', cfg: 'ini' }
  return m[name.split('.').pop()?.toLowerCase() || ''] || 'plaintext'
}

function updateLineNumbers() {
  const count = content.value.split('\n').length
  if (count === lastLineCount) return
  lastLineCount = count
  const parts = new Array(count)
  for (let i = 0; i < count; i++) parts[i] = String(i + 1)
  lineNumbers.value = parts.join('\n')
}

function updateHl() {
  contentHtml.value = hljs.highlight(content.value, { language: getLang(props.fileName) }).value
}

function markDirty() {
  dirty.value = true
  saved.value = false
  props.onDirtyChange?.(true)
  scheduleAutoSave()
}

function scheduleAutoSave() {
  // 固定间隔:变脏后计时,持续输入不重置计时器,到点即保存
  if (autoSaveTimer) return
  if (!autoSave.value) return
  autoSaveTimer = setTimeout(async () => {
    autoSaveTimer = null
    if (!dirty.value) return
    const ok = await doSave(false)
    if (ok) props.onDirtyChange?.(false)
  }, AUTO_SAVE_DELAY)
}

function onInput() {
  const el = editorEl.value
  const st = el?.scrollTop || 0
  const sl = el?.scrollLeft || 0
  updateLineNumbers()
  updateHl()
  markDirty()
  reportCursorPos()
  nextTick(() => {
    if (editorEl.value) { editorEl.value.scrollTop = st; editorEl.value.scrollLeft = sl }
    if (lineNumRef.value) lineNumRef.value.scrollTop = st
  })
}

function syncScroll(e: Event) {
  const ta = e.target as HTMLTextAreaElement
  if (editorEl.value) { editorEl.value.scrollTop = ta.scrollTop; editorEl.value.scrollLeft = ta.scrollLeft }
  if (lineNumRef.value) lineNumRef.value.scrollTop = ta.scrollTop
}

// 上报光标所在行列(状态栏显示)
function reportCursorPos() {
  const ta = taRef.value
  if (!ta) return
  const sel = ta.selectionStart
  const text = ta.value
  let line = 1
  let col = 1
  for (let i = 0; i < sel; i++) {
    if (text.charCodeAt(i) === 10) { line++; col = 1 } else { col++ }
  }
  props.onCursorChange?.(line, col)
}

function onTab(e: KeyboardEvent) {
  const ta = e.target as HTMLTextAreaElement
  const p = ta.selectionStart
  const v = ta.value
  ta.value = v.substring(0, p) + '\t' + v.substring(ta.selectionEnd)
  ta.selectionStart = ta.selectionEnd = p + 1
  content.value = ta.value
  updateLineNumbers()
  updateHl()
  markDirty()
  reportCursorPos()
}

async function doSave(showTip: boolean): Promise<boolean> {
  try {
    await WriteFile(props.filePath, content.value)
    dirty.value = false
    saved.value = true
    props.onDirtyChange?.(false)
    if (showTip) message.success('已保存')
    return true
  } catch (err: any) {
    if (showTip) message.error('保存失败: ' + ((err && err.message) || '未知错误'))
    return false
  }
}

async function save(): Promise<boolean> {
  if (autoSaveTimer) { clearTimeout(autoSaveTimer); autoSaveTimer = null }
  return doSave(true)
}

function onKeydown(e: KeyboardEvent) {
  if (e.ctrlKey && (e.key === 's' || e.key === 'S')) {
    e.preventDefault()
    save()
  }
}

onMounted(async () => {
  props.onApiReady?.({
    isDirty: () => dirty.value,
    save,
  })
  try {
    const cfg = JSON.parse(await GetConfig())
    autoSave.value = cfg.fileEditing?.autoSave ?? true
  } catch {}
  try {
    content.value = await ReadFile(props.filePath)
    lastLineCount = 0
    updateLineNumbers()
    updateHl()
  } catch (err: any) {
    message.error('无法打开编辑: ' + ((err && err.message) || '读取失败'))
  }
  loading.value = false
  nextTick(() => {
    if (!props.active) return
    // 首次打开:光标定位于第一行第一列并聚焦
    const ta = taRef.value
    if (ta) {
      ta.focus()
      ta.selectionStart = 0
      ta.selectionEnd = 0
      ta.scrollTop = 0
      ta.scrollLeft = 0
      if (lineNumRef.value) lineNumRef.value.scrollTop = 0
    }
    reportCursorPos()
  })
})

// 切换到活动标签页时:聚焦并恢复原光标与阅读位置(组件未销毁,位置天然保留)
watch(() => props.active, (act) => {
  if (!act || loading.value) return
  nextTick(() => {
    const ta = taRef.value
    if (!ta) return
    ta.focus()
    if (lineNumRef.value) lineNumRef.value.scrollTop = ta.scrollTop
    reportCursorPos()
  })
})

onUnmounted(() => {
  if (autoSaveTimer) { clearTimeout(autoSaveTimer); autoSaveTimer = null }
  if (dirty.value && !saved.value) {
    props.onDirtyChange?.(false)
  }
})
</script>

<template>
  <div class="file-editor">
    <div v-if="loading" class="fe-loading">加载中...</div>
    <div v-else class="fe-wrap" @keydown="onKeydown">
      <div ref="lineNumRef" class="fe-lines">{{ lineNumbers }}</div>
      <div class="fe-body">
        <textarea ref="taRef" v-model="content" class="fe-area" spellcheck="false" @input="onInput" @keyup="reportCursorPos" @click="reportCursorPos" @keydown.tab.prevent="onTab" @scroll="syncScroll" />
        <pre ref="editorEl" class="fe-hl" v-html="contentHtml" />
      </div>
    </div>
  </div>
</template>

<style scoped>
.file-editor {
  width: 100%; height: 100%; display: flex; flex-direction: column; overflow: hidden;
  background: var(--term-bg, #101010);
}
.fe-loading { flex: 1; display: flex; align-items: center; justify-content: center; color: var(--icon-color, #888); font-size: 13px }
.fe-wrap {
  flex: 1; min-height: 0; display: flex; overflow: hidden;
  background: var(--term-bg, #101010);
}
.fe-lines {
  flex-shrink: 0; width: 44px; overflow: hidden; padding: 8px 6px 8px 0; text-align: right;
  font-family: 'Cascadia Code', 'Fira Code', Consolas, 'Courier New', monospace;
  font-size: 13px; line-height: 1.55; white-space: pre;
  color: var(--icon-color, #6e6e6e); background: var(--term-bg, #101010);
  user-select: none;
}
.fe-body { position: relative; flex: 1; min-width: 0; overflow: hidden; }
.fe-area, .fe-hl {
  position: absolute; top: 0; left: 0; right: 0; bottom: 0;
  padding: 8px 8px 8px 4px; overflow: auto;
  font-family: 'Cascadia Code', 'Fira Code', Consolas, 'Courier New', monospace;
  font-size: 13px; line-height: 1.55; white-space: pre; tab-size: 2;
  text-align: left;
}
.fe-area {
  color: transparent; caret-color: var(--text-color, #d4d4d4); background: transparent;
  resize: none; border: none; outline: none; z-index: 2;
}
.fe-hl { z-index: 1; pointer-events: none; color: var(--text-color, #d4d4d4); margin: 0; overflow: hidden }
</style>