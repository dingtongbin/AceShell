<script setup lang="ts">
import { ref, onMounted, onUnmounted, nextTick, h } from 'vue'
import { NIcon, NButton, NInput, NModal, NProgress, NDropdown, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import type { DropdownOption } from 'naive-ui'
import {
  FolderOutline, DocumentOutline, ArrowUpOutline, RefreshOutline, EyeOutline,
  CreateOutline, TrashOutline, CloudUploadOutline, CloudDownloadOutline, HomeOutline,
  PencilOutline, CopyOutline, OpenOutline, CodeSlashOutline,
  AddCircleOutline, FolderOpenOutline
} from '@vicons/ionicons5'
import { List, Mkdir, Remove, RemoveAll, Rename, Download, ReadFile, WriteFile, UploadProgress, DownloadProgress, CancelTransfer } from '../../bindings/changeme/internal/services/sftpservice.js'
import { DownloadFile, ReadFileBase64, GetUserHomeDir, LocalCreateFile, LocalCreateDir, LocalRename, LocalReadText, LocalWriteText, OpenWithDefault, OpenWithEditor, MoveToRecycleBin } from '../../bindings/changeme/internal/services/windowservice.js'
import { Copy } from '../../bindings/changeme/internal/services/clipboardservice.js'
import { Events } from '@wailsio/runtime'
import hljs from 'highlight.js'
import 'highlight.js/styles/vs2015.css'

const message = useMessage()
const { t } = useI18n()

const props = defineProps<{ sessionID: string; tabId: string }>()

interface FileInfo { name: string; size: number; mode: string; modTime: string; isDir: boolean }

// 远程
const remotePath = ref('/')
const remoteFiles = ref<FileInfo[]>([])
const remoteLoading = ref(false)
const pathInput = ref('/')

// 本地
const localPath = ref('.')
const localPathInput = ref('')
const localFiles = ref<FileInfo[]>([])
const localLoading = ref(false)

// 传输
const transfers = ref<{ id: string; name: string; direction: string; percent: number; status: string; transferred: number; size: number; speed: string }[]>([])
const speedMap = new Map<string, { lastBytes: number; lastTime: number }>()
let trId = 0

// 弹窗
const showMkdir = ref(false); const mkdirName = ref('')
const showDelete = ref(false); const delTarget = ref<FileInfo | null>(null)
const delSide = ref('')
const showEditor = ref(false); const editFile = ref(''); const editContent = ref(''); const editHtml = ref(''); const editPath = ref('')
const editIsLocal = ref(false)

// 名称输入弹窗(新建文件/新建文件夹/重命名)
const showNameDlg = ref(false)
const nameDlgTitle = ref('')
const nameDlgValue = ref('')
let nameAction: (() => void) | null = null
function openNameDlg(title: string, value: string, onOk: () => void) {
  nameDlgTitle.value = title; nameDlgValue.value = value; nameAction = onOk; showNameDlg.value = true
}
async function confirmNameDlg() {
  const name = nameDlgValue.value.trim()
  if (!name || !nameAction) return
  showNameDlg.value = false
  const action = nameAction; nameAction = null
  try { await action() } catch (err: any) { message.error((err && err.message) || t('sftpPanel.opFail')) }
}

// 右键菜单
const ctxShow = ref(false); const ctxX = ref(0); const ctxY = ref(0)
const ctxOptions = ref<DropdownOption[]>([])
const ctxSide = ref('')
const ctxTarget = ref<FileInfo | null>(null)
const showPreview = ref(false); const previewFile = ref(''); const previewUrl = ref(''); const previewType = ref<'image' | 'video' | ''>('')
const editorHlRef = ref<HTMLElement | null>(null)

// 分割比例
const splitLeft = ref(50)
const transferHeight = ref(120)

// 拖动状态
interface DragState { dragging: boolean; startX: number; startVal: number }
const hDrag: DragState = { dragging: false, startX: 0, startVal: 50 }
const vDrag = { dragging: false, startY: 0, startVal: 120 }

function startHDrag(e: MouseEvent) {
  hDrag.dragging = true; hDrag.startX = e.clientX; hDrag.startVal = splitLeft.value
  document.addEventListener('mousemove', onHDrag); document.addEventListener('mouseup', stopHDrag)
}
function onHDrag(e: MouseEvent) {
  if (!hDrag.dragging) return
  const container = (e.target as HTMLElement).closest('.sftp-panes') as HTMLElement
  const w = container?.offsetWidth || 800
  const dx = e.clientX - hDrag.startX
  splitLeft.value = Math.min(80, Math.max(20, hDrag.startVal + (dx / w) * 100))
}
function stopHDrag() {
  hDrag.dragging = false; document.removeEventListener('mousemove', onHDrag); document.removeEventListener('mouseup', stopHDrag)
}

function startVDrag(e: MouseEvent) {
  vDrag.dragging = true; vDrag.startY = e.clientY; vDrag.startVal = transferHeight.value
  document.addEventListener('mousemove', onVDrag); document.addEventListener('mouseup', stopVDrag)
}
function onVDrag(e: MouseEvent) {
  if (!vDrag.dragging) return
  const container = (e.target as HTMLElement).closest('.sftp-layout') as HTMLElement
  const h = container?.offsetHeight || 600
  const dy = vDrag.startY - e.clientY
  transferHeight.value = Math.min(h - 120, Math.max(60, vDrag.startVal + dy))
}
function stopVDrag() {
  vDrag.dragging = false; document.removeEventListener('mousemove', onVDrag); document.removeEventListener('mouseup', stopVDrag)
}

onUnmounted(() => { stopHDrag(); stopVDrag(); offProgress?.(); offFileDrop?.() })

function joinPath(base: string, name: string): string {
  if (base.endsWith('/') || base.endsWith('\\')) return base + name
  return base + '/' + name
}

function getLocalFullPath(f: { name: string }): string { return joinPath(localPath.value, f.name) }

// 加载远程
async function loadRemote(path?: string) {
  remoteLoading.value = true
  try {
    const target = path ?? remotePath.value
    const r = JSON.parse(await List(props.sessionID, target))
    if (r.error) { remoteFiles.value = []; message.error(t('sftpPanel.loadRemoteFail') + ': ' + r.error) }
    else { remotePath.value = r.path || target; pathInput.value = remotePath.value; remoteFiles.value = r.files || [] }
  } catch { remoteFiles.value = []; message.error(t('sftpPanel.loadRemoteFail')) }
  remoteLoading.value = false
}

// 加载本地
async function loadLocal(path?: string) {
  localLoading.value = true
  try {
    const target = path ?? localPath.value
    const r = JSON.parse(await List('__local__', target))
    if (r.error) { localFiles.value = []; message.error(t('sftpPanel.loadLocalFail') + ': ' + r.error) }
    else { localPath.value = r.path || target; localPathInput.value = localPath.value; localFiles.value = r.files || [] }
  } catch { localFiles.value = []; message.error(t('sftpPanel.loadLocalFail')) }
  localLoading.value = false
}

function openLocal(f: FileInfo) { if (f.isDir) loadLocal(joinPath(localPath.value, f.name)) }
function goUpLocal() {
  const p = localPath.value.replace(/\\/g, '/').replace(/\/$/, '')
  if (p === '' || p === '/') return
  if (/^[A-Za-z]:$/.test(p)) return
  const idx = p.lastIndexOf('/')
  let up = idx <= 0 ? '/' : p.substring(0, idx)
  if (/^[A-Za-z]:$/.test(up)) up += '/'
  loadLocal(up)
}

function openRemote(f: FileInfo) { if (f.isDir) loadRemote(remotePath.value === '/' ? '/' + f.name : remotePath.value + '/' + f.name) }
function goUp() { const p = remotePath.value.split('/').filter(Boolean); p.pop(); loadRemote('/' + p.join('/') || '/') }

function formatSize(b: number): string {
  if (!b) return '-'; const u = ['B', 'KB', 'MB', 'GB']; let i = 0, n = b
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++ }
  return n.toFixed(i ? 1 : 0) + ' ' + u[i]
}

function isImage(name: string) { return /\.(png|jpg|jpeg|gif|bmp|webp|svg|ico)$/i.test(name) }
function isVideo(name: string) { return /\.(mp4|webm|ogg|mov|avi|mkv|wmv|flv)$/i.test(name) }

// 允许软件内编辑的文本扩展名白名单;无扩展名文件(Dockerfile/Makefile 等)放行,由后端做 UTF-8 校验兜底
const TEXT_EXTS = new Set([
  'txt', 'log', 'md', 'markdown', 'json', 'yml', 'yaml', 'toml', 'ini', 'conf', 'cfg', 'env', 'properties',
  'xml', 'html', 'htm', 'css', 'js', 'mjs', 'cjs', 'ts', 'tsx', 'jsx', 'vue', 'svelte',
  'py', 'go', 'rs', 'java', 'c', 'h', 'cpp', 'cc', 'cxx', 'hpp', 'hh', 'cs',
  'sh', 'bash', 'zsh', 'bat', 'cmd', 'ps1', 'sql', 'csv', 'gitignore', 'plist', 'dockerfile'
])
function isEditable(name: string) {
  if (isImage(name) || isVideo(name)) return false
  const parts = name.split('.')
  if (parts.length === 1) return true
  return TEXT_EXTS.has(parts.pop()?.toLowerCase() || '')
}

function getRemotePath(f: { name: string }) { return remotePath.value === '/' ? '/' + f.name : remotePath.value + '/' + f.name }

// 上传
function doUploadFile(f: { name: string; path?: string }) {
  const localFilePath = f.path || getLocalFullPath(f)
  const rp = getRemotePath(f)
  const id = 'up_' + (++trId)
  transfers.value.push({ id, name: f.name, direction: '↑', percent: 0, status: 'transferring', transferred: 0, size: 0, speed: '0 B/s' })
  speedMap.set(id, { lastBytes: 0, lastTime: Date.now() })
  UploadProgress(props.sessionID, localFilePath, rp, id).then((result: string) => {
    const data = JSON.parse(result)
    if (data.cancelled) {
      const t = transfers.value.find(x => x.id === id); if (t) { t.status = 'cancelled'; t.percent = 100 }
    } else if (data.error) {
      const t = transfers.value.find(x => x.id === id); if (t) t.status = 'error'
    } else {
      const t = transfers.value.find(x => x.id === id); if (t) { t.percent = 100; t.status = 'done'; t.transferred = data.written || t.size }
    }
    loadRemote()
  }).catch(() => {
    const t = transfers.value.find(x => x.id === id); if (t) t.status = 'error'
  })
}

function doUploadPicker() {
  const inp = document.createElement('input'); inp.type = 'file'
  inp.onchange = () => { const f = inp.files?.[0]; if (f) doUploadFile({ name: f.name, path: (f as any).path || f.name }) }
  inp.click()
}

// 下载
async function doDownload(file: FileInfo) {
  const rp = getRemotePath(file)
  const id = 'dn_' + (++trId)
  const tmpPath = 'tmp/.sftp_dl_' + Date.now() + '_' + file.name
  transfers.value.push({ id, name: file.name, direction: '↓', percent: 0, status: 'transferring', transferred: 0, size: 0, speed: '0 B/s' })
  speedMap.set(id, { lastBytes: 0, lastTime: Date.now() })
  try {
    const result = await DownloadProgress(props.sessionID, rp, tmpPath, id)
    const data = JSON.parse(result)
    if (data.cancelled) {
      const t = transfers.value.find(x => x.id === id); if (t) { t.status = 'cancelled'; t.percent = 100 }
    } else if (!data.error) {
      const t = transfers.value.find(x => x.id === id); if (t) { t.percent = 100; t.status = 'done'; t.transferred = data.written || t.size }
      try { await DownloadFile(tmpPath) } catch { /* dialog cancelled or error */ }
    } else {
      const t = transfers.value.find(x => x.id === id); if (t) t.status = 'error'
    }
  } catch { const t = transfers.value.find(x => x.id === id); if (t) t.status = 'error' }
}

// 拖拽下载：直接写入当前本地目录，不弹系统对话框
async function doDownloadToLocal(file: FileInfo) {
  const rp = getRemotePath(file)
  const localDst = joinPath(localPath.value, file.name)
  const id = 'dn_' + (++trId)
  transfers.value.push({ id, name: file.name, direction: '↓', percent: 0, status: 'transferring', transferred: 0, size: 0, speed: '0 B/s' })
  speedMap.set(id, { lastBytes: 0, lastTime: Date.now() })
  try {
    const result = await DownloadProgress(props.sessionID, rp, localDst, id)
    const data = JSON.parse(result)
    if (data.cancelled) {
      const t = transfers.value.find(x => x.id === id); if (t) { t.status = 'cancelled'; t.percent = 100 }
    } else if (!data.error) {
      const t = transfers.value.find(x => x.id === id); if (t) { t.percent = 100; t.status = 'done'; t.transferred = data.written || t.size }
      loadLocal()
    } else {
      const t = transfers.value.find(x => x.id === id); if (t) t.status = 'error'
    }
  } catch { const t = transfers.value.find(x => x.id === id); if (t) t.status = 'error' }
}

// 预览
async function openPreview(f: FileInfo, side: string) {
  previewFile.value = f.name
  if (side === 'local') {
    previewUrl.value = await ReadFileBase64(getLocalFullPath(f))
  } else {
    const rp = getRemotePath(f)
    const tmpPath = 'tmp/.sftp_preview_' + f.name
    const result = await Download(props.sessionID, rp, tmpPath)
    const data = JSON.parse(result)
    if (data.error) { previewUrl.value = ''; return }
    previewUrl.value = await ReadFileBase64(tmpPath)
  }
  previewType.value = isImage(f.name) ? 'image' : isVideo(f.name) ? 'video' : ''
  showPreview.value = true
}

// 拖拽传输
let dragFile: any = null; let dragSide = ''
function onDragStart(e: DragEvent, side: string, file: any) { dragFile = file; dragSide = side; e.dataTransfer!.effectAllowed = 'copy' }
async function onDrop(e: DragEvent, side: string) {
  e.preventDefault()
  if (!dragFile) return
  if (dragSide === 'local' && side === 'remote') doUploadFile(dragFile as { name: string; path?: string })
  else if (dragSide === 'remote' && side === 'local') await doDownloadToLocal(dragFile as FileInfo)
  dragFile = null
}

// 右键菜单
function ctxIcon(icon: any) {
  return () => h(NIcon, { size: 14 }, { default: () => h(icon) })
}

function buildMenuOptions(f: FileInfo, side: string): DropdownOption[] {
  const items: DropdownOption[] = []
  if (!f.isDir) {
    if (side === 'remote') items.push({ label: t('sftpPanel.menuDownload'), key: 'download', icon: ctxIcon(CloudDownloadOutline) })
    items.push({ label: t('sftpPanel.menuView'), key: 'view', icon: ctxIcon(EyeOutline) })
    items.push({ label: t('sftpPanel.menuRename'), key: 'rename', icon: ctxIcon(PencilOutline) })
    if (isEditable(f.name)) {
      items.push({ label: t('sftpPanel.menuEdit'), key: 'edit', icon: ctxIcon(CodeSlashOutline) })
      items.push({ label: t('sftpPanel.menuOpen'), key: 'open', icon: ctxIcon(OpenOutline) })
      items.push({ label: t('sftpPanel.menuOpenEditor'), key: 'open-editor', icon: ctxIcon(CreateOutline) })
    }
    items.push({ label: t('sftpPanel.menuCopyPath'), key: 'copy-path', icon: ctxIcon(CopyOutline) })
  } else {
    items.push({ label: t('sftpPanel.menuNewFile'), key: 'new-file', icon: ctxIcon(DocumentOutline) })
    items.push({ label: t('sftpPanel.menuNewDir'), key: 'new-dir', icon: ctxIcon(AddCircleOutline) })
    items.push({ label: t('sftpPanel.menuEnter'), key: 'enter', icon: ctxIcon(FolderOpenOutline) })
    items.push({ label: t('sftpPanel.menuRename'), key: 'rename', icon: ctxIcon(PencilOutline) })
    items.push({ label: t('sftpPanel.menuCopyPath'), key: 'copy-path', icon: ctxIcon(CopyOutline) })
  }
  items.push({ type: 'divider', key: 'd' })
  items.push({ label: t('sftpPanel.menuDelete'), key: 'delete', icon: ctxIcon(TrashOutline) })
  return items
}

function openContextMenu(e: MouseEvent, side: string, f: FileInfo) {
  e.preventDefault(); e.stopPropagation()
  ctxSide.value = side; ctxTarget.value = f
  ctxOptions.value = buildMenuOptions(f, side)
  ctxX.value = e.clientX; ctxY.value = e.clientY; ctxShow.value = true
}

// 远端文件用系统程序打开:先下载临时副本再打开
async function remoteExternal(f: FileInfo, editor: boolean) {
  const rp = getRemotePath(f)
  const tmpPath = 'tmp/.sftp_ext_' + Date.now() + '_' + f.name
  try {
    const result = await Download(props.sessionID, rp, tmpPath)
    const data = JSON.parse(result)
    if (data.error) { message.error(t('sftpPanel.downloadFail', { err: data.error })); return }
    if (editor) await OpenWithEditor(tmpPath)
    else await OpenWithDefault(tmpPath)
  } catch (err: any) { message.error((err && err.message) || t('sftpPanel.openFail')) }
}

async function handleCtxSelect(key: string) {
  ctxShow.value = false
  const f = ctxTarget.value
  const side = ctxSide.value
  ctxTarget.value = null
  if (!f) return
  const rp = side === 'remote' ? getRemotePath(f) : getLocalFullPath(f)
  if (key === 'download' && side === 'remote') { doDownload(f); return }
  if (key === 'view') { openPreview(f, side); return }
  if (key === 'enter') { if (side === 'local') openLocal(f); else openRemote(f); return }
  if (key === 'copy-path') { Copy(rp); return }
  if (key === 'edit') { if (side === 'local') openLocalEditor(f); else openEditor(f); return }
  if (key === 'open' || key === 'open-editor') {
    if (side === 'local') {
      if (key === 'open') await OpenWithDefault(rp)
      else await OpenWithEditor(rp)
    } else {
      await remoteExternal(f, key === 'open-editor')
    }
    return
  }
  if (key === 'delete') { confirmDelete(f, side); return }
  if (key === 'rename') {
    openNameDlg(t('sftpPanel.menuRename'), f.name, async () => {
      const name = nameDlgValue.value.trim()
      if (!name || name === f.name) return
      if (side === 'local') await LocalRename(rp, name)
      else await Rename(props.sessionID, rp, joinPath(remotePath.value, name))
      if (side === 'local') loadLocal(); else loadRemote()
    })
    return
  }
  if (key === 'new-file') {
    openNameDlg(t('sftpPanel.menuNewFile'), t('sftpPanel.menuNewFile'), async () => {
      const name = nameDlgValue.value.trim()
      if (!name) return
      if (side === 'local') await LocalCreateFile(joinPath(localPath.value, name))
      else await WriteFile(props.sessionID, joinPath(remotePath.value, name), '')
      if (side === 'local') loadLocal(); else loadRemote()
    })
    return
  }
  if (key === 'new-dir') {
    openNameDlg(t('sftpPanel.menuNewDir'), t('sftpPanel.menuNewDir'), async () => {
      const name = nameDlgValue.value.trim()
      if (!name) return
      if (side === 'local') await LocalCreateDir(joinPath(localPath.value, name))
      else await Mkdir(props.sessionID, joinPath(remotePath.value, name))
      if (side === 'local') loadLocal(); else loadRemote()
    })
    return
  }
}

// 删除
function confirmDelete(f: FileInfo, side: string) { delTarget.value = f; delSide.value = side; showDelete.value = true }
async function doDelete() {
  if (!delTarget.value) return
  const f = delTarget.value
  try {
    if (delSide.value === 'local') {
      await MoveToRecycleBin(getLocalFullPath(f))
      loadLocal()
    } else {
      if (f.isDir) await RemoveAll(props.sessionID, getRemotePath(f))
      else await Remove(props.sessionID, getRemotePath(f))
      loadRemote()
    }
  } catch { message.error(t('sftpPanel.deleteFail')) }
  showDelete.value = false; delTarget.value = null; delSide.value = ''
}

// 新建文件夹
async function doMkdir() {
  if (!mkdirName.value.trim()) return
  await Mkdir(props.sessionID, remotePath.value === '/' ? '/' + mkdirName.value : remotePath.value + '/' + mkdirName.value)
  showMkdir.value = false; mkdirName.value = ''; loadRemote()
}

// 编辑
const lineNumRef = ref<HTMLElement | null>(null)
const lineNumbers = ref('1')
let lastLineCount = 1
function updateLineNumbers() {
  const count = editContent.value.split('\n').length
  if (count === lastLineCount) return
  lastLineCount = count
  const parts = new Array(count)
  for (let i = 0; i < count; i++) parts[i] = String(i + 1)
  lineNumbers.value = parts.join('\n')
}

async function openEditor(file: FileInfo) {
  editFile.value = file.name; editPath.value = getRemotePath(file)
  editIsLocal.value = false
  const result = await ReadFile(props.sessionID, editPath.value)
  if (result.startsWith('{"error"')) {
    try { const d = JSON.parse(result); if (d.error) { message.error(t('sftpPanel.openEditorFail', { err: d.error })); return } } catch { /* 非错误 JSON,按内容处理 */ }
  }
  editContent.value = result
  lastLineCount = 0
  updateLineNumbers()
  editHtml.value = hljs.highlight(editContent.value, { language: getLang(file.name) }).value
  showEditor.value = true
}

async function openLocalEditor(file: FileInfo) {
  editFile.value = file.name; editPath.value = getLocalFullPath(file)
  editIsLocal.value = true
  try {
    editContent.value = await LocalReadText(editPath.value)
  } catch (err: any) {
    message.error(t('sftpPanel.openEditorFail', { err: (err && err.message) || t('sftpPanel.readFail') }))
    return
  }
  lastLineCount = 0
  updateLineNumbers()
  editHtml.value = hljs.highlight(editContent.value, { language: getLang(file.name) }).value
  showEditor.value = true
}
function getLang(name: string): string {
  const m: any = { js: 'javascript', ts: 'typescript', py: 'python', go: 'go', html: 'xml', css: 'css', json: 'json', xml: 'xml', yaml: 'yaml', yml: 'yaml', toml: 'ini', md: 'markdown', sh: 'bash', sql: 'sql' }
  return m[name.split('.').pop()?.toLowerCase() || ''] || 'plaintext'
}
function updateHl() { editHtml.value = hljs.highlight(editContent.value, { language: getLang(editFile.value) }).value }
function onEditorInput() {
  const el = editorHlRef.value
  const st = el?.scrollTop || 0
  const sl = el?.scrollLeft || 0
  updateLineNumbers()
  updateHl()
  nextTick(() => {
    if (editorHlRef.value) { editorHlRef.value.scrollTop = st; editorHlRef.value.scrollLeft = sl }
    if (lineNumRef.value) lineNumRef.value.scrollTop = st
  })
}
function syncEditorScroll(e: Event) {
  const ta = e.target as HTMLTextAreaElement
  if (editorHlRef.value) { editorHlRef.value.scrollTop = ta.scrollTop; editorHlRef.value.scrollLeft = ta.scrollLeft }
  if (lineNumRef.value) lineNumRef.value.scrollTop = ta.scrollTop
}
function onEditorTab(e: KeyboardEvent) {
  const ta = e.target as HTMLTextAreaElement
  const p = ta.selectionStart
  const v = ta.value
  ta.value = v.substring(0, p) + '\t' + v.substring(ta.selectionEnd)
  ta.selectionStart = ta.selectionEnd = p + 1
  editContent.value = ta.value
  onEditorInput()
}
async function saveEditor() {
  try {
    if (editIsLocal.value) await LocalWriteText(editPath.value, editContent.value)
    else await WriteFile(props.sessionID, editPath.value, editContent.value)
  } catch { message.error(t('sftpPanel.saveFail')); return }
  showEditor.value = false
  if (editIsLocal.value) loadLocal(); else loadRemote()
}

function clearDoneTransfers() {
  transfers.value = transfers.value.filter(t => t.status === 'transferring')
}

async function cancelTransfer(id: string) {
  try { await CancelTransfer(id) } catch { /* ignore */ }
}

function handleProgress(data: any) {
  const t = transfers.value.find(x => x.id === data.id)
  if (!t) return
  t.transferred = data.transferred || 0
  t.size = data.size || 0
  t.status = data.status || 'transferring'
  if (t.size > 0) t.percent = Math.round((t.transferred / t.size) * 100)
  const sm = speedMap.get(data.id)
  if (sm) {
    const now = Date.now()
    const elapsed = (now - sm.lastTime) / 1000
    if (elapsed >= 0.1) {
      const byteDiff = t.transferred - sm.lastBytes
      t.speed = formatSpeed(byteDiff / Math.max(elapsed, 0.001))
      sm.lastBytes = t.transferred
      sm.lastTime = now
    }
  }
}

function formatSpeed(bytesPerSec: number): string {
  if (bytesPerSec <= 0) return '0 B/s'
  const u = ['B/s', 'KB/s', 'MB/s', 'GB/s']
  let i = 0; let n = bytesPerSec
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++ }
  return n.toFixed(i ? 1 : 0) + ' ' + u[i]
}

let offProgress: (() => void) | null = null
let offFileDrop: (() => void) | null = null

function fileNameFromPath(p: string): string {
  return p.replace(/\\/g, '/').split('/').filter(Boolean).pop() || p
}

function handleSystemFileDrop(d: any) {
  if (typeof d === 'string') d = JSON.parse(d)
  if (!d || d.panelId !== 'sftp-remote-drop-' + props.sessionID || !Array.isArray(d.files)) return
  for (const p of d.files) {
    doUploadFile({ name: fileNameFromPath(p), path: p })
  }
}

onMounted(async () => {
  try {
    const home = await GetUserHomeDir()
    if (home && home !== '.') { localPath.value = home; localPathInput.value = home }
  } catch { /* 保持默认目录 */ }
  loadRemote(); loadLocal()
  offProgress = Events.On('sftp-transfer-progress', (d: any) => handleProgress(typeof d === 'string' ? JSON.parse(d) : d))
  offFileDrop = Events.On('sftp-files-dropped', (d: any) => handleSystemFileDrop(d))
})
</script>

<template>
  <div class="sftp-layout">
    <!-- 双面板 -->
    <div class="sftp-panes" :style="{ height: `calc(100% - ${transferHeight + 4}px)` }">
      <!-- 左面板 -->
      <div class="sftp-pane" :style="{ width: splitLeft + '%' }" @dragover.prevent @drop="(e: DragEvent) => onDrop(e, 'local')">
        <div class="pane-header">{{ t('sftpPanel.localFiles') }}</div>
        <div class="pane-toolbar">
          <n-button size="tiny" quaternary @click="loadLocal('.')"><n-icon :size="14" :component="HomeOutline" /></n-button>
          <n-button size="tiny" quaternary @click="goUpLocal"><n-icon :size="14" :component="ArrowUpOutline" /></n-button>
          <n-button size="tiny" quaternary @click="loadLocal()"><n-icon :size="14" :component="RefreshOutline" /></n-button>
          <n-input v-model:value="localPathInput" size="tiny" style="flex:1;margin:0 4px" @keyup.enter="loadLocal(localPathInput)" />
          <n-button size="tiny" quaternary @click="doUploadPicker"><n-icon :size="14" :component="CloudUploadOutline" /></n-button>
        </div>
        <div class="pane-list">
          <div v-if="localLoading" class="list-status">{{ t('sftpPanel.loading') }}</div>
          <div v-for="f in localFiles" :key="localPath + '/' + f.name" class="file-row" :class="{ dir: f.isDir }" draggable="true"
            @dragstart="(e: DragEvent) => onDragStart(e, 'local', f)"
            @contextmenu="(e: MouseEvent) => openContextMenu(e, 'local', f)"
            @dblclick="f.isDir ? openLocal(f) : doUploadFile(f)">
            <n-icon :size="14" :component="f.isDir ? FolderOutline : DocumentOutline" :style="{ color: f.isDir ? '#4ec9b0' : '#888' }" />
            <span class="fname">{{ f.name }}</span>
            <span class="fsize">{{ f.isDir ? '' : formatSize(f.size) }}</span>
            <span class="fact">
              <n-button v-if="!f.isDir && (isImage(f.name) || isVideo(f.name))" size="tiny" quaternary @click.stop="openPreview(f, 'local')"><n-icon :size="13" :component="EyeOutline" /></n-button>
            </span>
          </div>
        </div>
      </div>
      <!-- 分割手柄 -->
      <div class="h-handle" @mousedown="startHDrag"><div class="h-handle-line" /></div>
      <!-- 右面板 -->
      <div class="sftp-pane" :id="'sftp-remote-drop-' + props.sessionID" data-file-drop-target style="flex:1" @dragover.prevent @drop="(e: DragEvent) => onDrop(e, 'remote')">
        <div class="pane-header">{{ t('sftpPanel.remoteServer') }}</div>
        <div class="pane-toolbar">
          <n-button size="tiny" quaternary @click="loadRemote('/')"><n-icon :size="14" :component="HomeOutline" /></n-button>
          <n-button size="tiny" quaternary @click="goUp"><n-icon :size="14" :component="ArrowUpOutline" /></n-button>
          <n-button size="tiny" quaternary @click="loadRemote()"><n-icon :size="14" :component="RefreshOutline" /></n-button>
          <n-input v-model:value="pathInput" size="tiny" style="flex:1;margin:0 4px" @keyup.enter="loadRemote(pathInput)" />
          <n-button size="tiny" quaternary @click="showMkdir = true"><n-icon :size="14" :component="FolderOutline" /></n-button>
          <n-button size="tiny" quaternary @click="doUploadPicker"><n-icon :size="14" :component="CloudUploadOutline" /></n-button>
        </div>
        <div class="pane-list">
          <div v-if="remoteLoading" class="list-status">{{ t('sftpPanel.loading') }}</div>
          <div v-for="f in remoteFiles" :key="remotePath + '/' + f.name" class="file-row" :class="{ dir: f.isDir }" draggable="true"
            @dragstart="(e: DragEvent) => onDragStart(e, 'remote', f)"
            @contextmenu="(e: MouseEvent) => openContextMenu(e, 'remote', f)"
            @dblclick="isEditable(f.name) ? (f.isDir ? openRemote(f) : openEditor(f)) : (f.isDir ? openRemote(f) : openPreview(f, 'remote'))">
            <n-icon :size="14" :component="f.isDir ? FolderOutline : DocumentOutline" :style="{ color: f.isDir ? '#4ec9b0' : '#888' }" />
            <span class="fname">{{ f.name }}</span>
            <span class="fsize">{{ f.isDir ? '' : formatSize(f.size) }}</span>
            <span class="fact">
              <n-button v-if="!f.isDir && (isImage(f.name) || isVideo(f.name))" size="tiny" quaternary @click.stop="openPreview(f, 'remote')"><n-icon :size="13" :component="EyeOutline" /></n-button>
              <n-button v-if="!f.isDir && isEditable(f.name)" size="tiny" quaternary @click.stop="openEditor(f)"><n-icon :size="13" :component="CreateOutline" /></n-button>
              <n-button size="tiny" quaternary @click.stop="doDownload(f)"><n-icon :size="13" :component="CloudDownloadOutline" /></n-button>
              <n-button size="tiny" quaternary @click.stop="confirmDelete(f, 'remote')"><n-icon :size="13" :component="TrashOutline" /></n-button>
            </span>
          </div>
        </div>
      </div>
    </div>

    <!-- 传输手柄 -->
    <div class="v-handle" @mousedown="startVDrag"><div class="v-handle-line" /></div>

    <!-- 传输列表 -->
    <div class="tbar" :style="{ height: transferHeight + 'px' }">
      <div class="tbar-header">
        <span>{{ t('sftpPanel.transferList', { count: transfers.length }) }}</span>
        <n-button size="tiny" quaternary @click="clearDoneTransfers">{{ t('sftpPanel.clearDone') }}</n-button>
      </div>
      <div class="tbar-body">
        <div v-if="!transfers.length" class="tbar-empty">{{ t('sftpPanel.noTransfer') }}</div>
        <div v-for="tr in transfers" :key="tr.id" class="trow">
          <span class="tname">{{ tr.direction }} {{ tr.name }}</span>
          <n-progress :percentage="tr.percent" :height="4" :show-text="false" style="flex:1;margin:0 6px" :color="tr.status === 'error' ? '#e45858' : tr.status === 'cancelled' ? '#f0a030' : '#4ec9b0'" />
          <span class="tspeed">{{ tr.speed }}</span>
          <span class="tstatus" :class="'t-' + tr.status">{{ tr.status === 'done' ? '✓' : tr.status === 'error' ? '✗' : tr.status === 'cancelled' ? '⊘' : '' }}</span>
          <n-button v-if="tr.status === 'transferring'" size="tiny" quaternary @click="cancelTransfer(tr.id)" :title="t('common.cancel')"><n-icon :size="12" :component="TrashOutline" /></n-button>
        </div>
      </div>
    </div>

    <!-- 新建文件夹弹窗 -->
    <n-modal v-model:show="showMkdir" :title="t('sftpPanel.menuNewDir')" preset="card" :show-icon="false" style="width:320px" :mask-closable="false">
      <n-input v-model:value="mkdirName" :placeholder="t('sftpPanel.folderName')" @keyup.enter="doMkdir" />
      <template #footer><n-button @click="showMkdir = false">{{ t('common.cancel') }}</n-button><n-button type="primary" @click="doMkdir" style="margin-left:8px">{{ t('common.create') }}</n-button></template>
    </n-modal>

    <!-- 名称输入弹窗(新建文件/新建文件夹/重命名) -->
    <n-modal v-model:show="showNameDlg" :title="nameDlgTitle" preset="card" :show-icon="false" style="width:320px" :mask-closable="false">
      <n-input v-model:value="nameDlgValue" :placeholder="t('sftpPanel.inputName')" @keyup.enter="confirmNameDlg" />
      <template #footer><n-button @click="showNameDlg = false">{{ t('common.cancel') }}</n-button><n-button type="primary" @click="confirmNameDlg" style="margin-left:8px">{{ t('common.confirm') }}</n-button></template>
    </n-modal>

    <!-- 右键菜单 -->
    <n-dropdown :show="ctxShow" :options="ctxOptions" :x="ctxX" :y="ctxY" placement="bottom-start" @select="handleCtxSelect" @clickoutside="ctxShow = false" />

    <!-- 删除确认弹窗 -->
    <n-modal v-model:show="showDelete" :title="t('sftpPanel.confirmDelete')" preset="card" :show-icon="false" style="width:380px" :mask-closable="false">
      <p style="margin:0">{{ t('sftpPanel.deleteMsg1') }}<b>{{ delTarget?.name }}</b>{{ t('sftpPanel.deleteMsg2') }}</p>
      <template #footer><n-button @click="showDelete = false">{{ t('common.cancel') }}</n-button><n-button type="error" @click="doDelete" style="margin-left:8px">{{ t('common.delete') }}</n-button></template>
    </n-modal>

    <!-- 编辑弹窗 -->
    <n-modal v-model:show="showEditor" :title="t('sftpPanel.editFile')" preset="card" :show-icon="false" style="width:820px;max-width:95vw" :mask-closable="false">
      <div class="editor-meta">
        <span class="editor-path">{{ editPath }}</span>
        <span class="editor-lang">{{ getLang(editFile) }}</span>
      </div>
      <div class="editor-wrap">
        <div ref="lineNumRef" class="editor-lines">{{ lineNumbers }}</div>
        <div class="editor-body">
          <textarea v-model="editContent" class="editor-area" @input="onEditorInput" @keydown.tab.prevent="onEditorTab" @scroll="syncEditorScroll" spellcheck="false" />
          <pre ref="editorHlRef" class="editor-hl" v-html="editHtml" />
        </div>
      </div>
      <template #footer><n-button @click="showEditor = false">{{ t('common.cancel') }}</n-button><n-button type="primary" @click="saveEditor" style="margin-left:8px">{{ t('common.save') }}</n-button></template>
    </n-modal>

    <!-- 预览弹窗 -->
    <n-modal v-model:show="showPreview" :title="previewFile" preset="card" :show-icon="false" style="width:820px;max-width:95vw" :mask-closable="false">
      <div class="preview-content">
        <img v-if="previewType === 'image'" :src="previewUrl" class="preview-media" @error="() => previewUrl = ''" />
        <video v-else-if="previewType === 'video'" :src="previewUrl" class="preview-media" controls autoplay />
        <div v-else class="preview-unsupported">{{ t('sftpPanel.previewUnsupported') }}</div>
      </div>
      <template #footer><n-button @click="showPreview = false">{{ t('common.close') }}</n-button></template>
    </n-modal>
  </div>
</template>

<style scoped>
.sftp-layout {
  width: 100%; height: 100%; display: flex; flex-direction: column;
  background: var(--border-color); overflow: hidden; user-select: none;
}
.sftp-panes {
  display: flex; flex-shrink: 0; overflow: hidden;
}
.sftp-pane {
  display: flex; flex-direction: column; background: var(--body-bg); overflow: hidden; min-width: 0;
}
.sftp-pane.file-drop-target-active {
  outline: 2px dashed var(--primary-color); outline-offset: -2px;
}
.pane-header {
  padding: 6px 10px; font-size: 12px; font-weight: 500; color: var(--text-color);
  background: var(--toolbar-bg); border-bottom: 1px solid var(--border-color); flex-shrink: 0;
}
.pane-toolbar {
  display: flex; align-items: center; padding: 4px 6px; gap: 2px;
  border-bottom: 1px solid var(--border-color); flex-shrink: 0;
}
.pane-list { flex: 1; overflow-y: auto; padding: 2px 0 }

.list-status { text-align: center; padding: 20px; color: var(--icon-color); font-size: 12px }

.file-row {
  display: flex; align-items: center; padding: 2px 8px; gap: 4px;
  font-size: 12px; color: var(--text-color); cursor: pointer; transition: background .1s;
}
.file-row:hover { background: var(--hover-bg) }
.file-row.dir { color: #4ec9b0 }
.fname { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex-shrink: 1 }
.fsize { margin-left: auto; color: var(--icon-color); font-size: 11px; flex-shrink: 0; margin-right: 2px }
.fact { display: flex; gap: 1px; opacity: 0; transition: opacity .15s; flex-shrink: 0 }
.file-row:hover .fact { opacity: 1 }

/* 水平分割手柄 */
.h-handle {
  width: 4px; cursor: col-resize; flex-shrink: 0;
  display: flex; align-items: center; justify-content: center;
  background: var(--border-color); transition: background .15s;
}
.h-handle:hover { background: #0078d4 }
.h-handle-line { width: 1px; height: 32px; background: rgba(128,128,128,.2); border-radius: 1px }

/* 垂直分割手柄 */
.v-handle {
  height: 4px; cursor: row-resize; flex-shrink: 0;
  display: flex; align-items: center; justify-content: center;
  background: var(--border-color); transition: background .15s;
}
.v-handle:hover { background: #0078d4 }
.v-handle-line { height: 1px; width: 32px; background: rgba(128,128,128,.2); border-radius: 1px }

/* 传输列表 */
.tbar {
  display: flex; flex-direction: column; flex-shrink: 0;
  background: var(--sidebar-bg); border-top: 1px solid var(--border-color);
}
.tbar-header {
  display: flex; align-items: center; justify-content: space-between;
  padding: 4px 8px; font-size: 11px; color: var(--icon-color);
  background: var(--toolbar-bg); border-bottom: 1px solid var(--border-color); flex-shrink: 0;
}
.tbar-body { flex: 1; overflow-y: auto; padding: 2px 0 }
.tbar-empty { text-align: center; padding: 16px; color: var(--icon-color); font-size: 12px }
.trow { display: flex; align-items: center; padding: 3px 8px; gap: 4px; font-size: 11px; color: var(--text-color) }
.tname { flex-shrink: 0; max-width: 180px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap }
.tspeed { flex-shrink: 0; width: 56px; text-align: right; font-size: 10px; color: var(--icon-color) }
.tstatus { width: 16px; text-align: center; flex-shrink: 0; font-size: 11px }
.t-done { color: #4ec9b0 }
.t-error { color: #e45858 }
.t-cancelled { color: #f0a030 }

/* 编辑器 */
.editor-meta {
  display: flex; align-items: center; justify-content: space-between;
  margin-bottom: 8px; padding: 4px 8px; background: var(--sidebar-bg); border-radius: 4px;
}
.editor-path { font-size: 11px; color: var(--icon-color); overflow: hidden; text-overflow: ellipsis; white-space: nowrap }
.editor-lang { font-size: 10px; color: #4ec9b0; flex-shrink: 0; margin-left: 8px; text-transform: uppercase }
.editor-wrap {
  position: relative; height: 460px; overflow: hidden; display: flex;
  background: var(--body-bg); border-radius: 4px; border: 1px solid var(--border-color);
}
.editor-lines {
  flex-shrink: 0; width: 44px; overflow: hidden; padding: 12px 6px 12px 0; text-align: right;
  font-family: 'Cascadia Code', 'Fira Code', Consolas, 'Courier New', monospace;
  font-size: 13px; line-height: 1.55; white-space: pre;
  color: var(--icon-color); background: var(--sidebar-bg); border-right: 1px solid var(--border-color);
  user-select: none;
}
.editor-body {
  position: relative; flex: 1; overflow: hidden; min-width: 0;
}
.editor-area, .editor-hl {
  position: absolute; inset: 0; padding: 12px; overflow: auto;
  font-family: 'Cascadia Code', 'Fira Code', Consolas, 'Courier New', monospace;
  font-size: 13px; line-height: 1.55; white-space: pre; tab-size: 2;
}
.editor-area {
  color: transparent; caret-color: var(--text-color); background: transparent;
  resize: none; border: none; outline: none; z-index: 2;
}
.editor-hl { z-index: 1; pointer-events: none; color: var(--text-color); margin: 0; overflow: hidden }

/* 预览 */
.preview-content {
  display: flex; align-items: center; justify-content: center;
  min-height: 240px; background: var(--body-bg); border-radius: 4px; overflow: hidden;
}
.preview-media { max-width: 100%; max-height: 70vh; object-fit: contain }
.preview-unsupported { color: var(--icon-color); font-size: 14px; padding: 40px }
</style>
