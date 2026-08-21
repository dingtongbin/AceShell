<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, h, nextTick } from 'vue'
import { NIcon, NButton, NEmpty, NDropdown, NModal, NForm, NFormItem, NInput } from 'naive-ui'
import type { DropdownOption } from 'naive-ui'
import {
  ChevronForwardOutline,
  FolderOutline,
  AddOutline,
  CreateOutline,
  TrashOutline,
  EllipsisVerticalOutline,
  TerminalOutline,
  RadioOutline,
  LogInOutline,
  SearchOutline,
  DesktopOutline,
} from '@vicons/ionicons5'
import { Events } from '@wailsio/runtime'
import { useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { GetTree, DeleteSession, DeleteFolder, MoveFile, MoveFolder, RenameItem, LoadSession } from '../../bindings/changeme/internal/services/sessionfileservice.js'

interface TreeNode {
  name: string
  path: string
  isDir: boolean
  protocol?: string
  children?: TreeNode[]
  expanded?: boolean
  depth?: number
  last?: boolean
  ancestors?: boolean[]
  _ref?: TreeNode
}

const props = defineProps<{
  activeSessionPath: string | null
  width: number
}>()

const emit = defineEmits<{
  (e: 'select', path: string): void
  (e: 'new-folder', parentPath: string): void
  (e: 'new-session', folderPath: string): void
  (e: 'edit-session', path: string): void
  (e: 'import-sessions'): void
  (e: 'export-sessions'): void
  (e: 'refresh'): void
}>()

const tree = ref<TreeNode[]>([])
const flatList = ref<TreeNode[]>([])
const searchQuery = ref('')
const message = useMessage()
const { t } = useI18n()
let offTree: (() => void) | null = null

const filteredList = computed(() => {
  if (!searchQuery.value) return flatList.value
  const q = searchQuery.value.toLowerCase()
  return flatList.value.filter(n => n.name.toLowerCase().includes(q))
})

const ctxShow = ref(false)
const ctxX = ref(0)
const ctxY = ref(0)
const ctxOptions = ref<DropdownOption[]>([])
const ctxNode = ref<TreeNode | null>(null)

let dragNodePath: string | null = null
const dropTargetPath = ref<string | null>(null)
const dropRoot = ref(false)
let dragHoverTimer: ReturnType<typeof setTimeout> | null = null

const showConflict = ref(false)
const conflictName = ref('')
const pendingDragPath = ref<string | null>(null)
const pendingDestFolder = ref('')
const pendingIsDir = ref(false)

const showDeleteConfirm = ref(false)
const deleteTarget = ref<{ path: string; name: string; isDir: boolean } | null>(null)

const showMeta = ref(false)
const metaInfo = ref<{ name: string; protocol: string; host: string; port: number; created: string; updated: string; username: string } | null>(null)

async function viewMeta(node: TreeNode) {
  try {
    const raw = await LoadSession(node.path)
    const m = JSON.parse(raw)
    metaInfo.value = {
      name: m.name || '',
      protocol: m.protocol || '',
      host: m.host || '',
      port: m.port || 0,
      created: m.created || '',
      updated: m.updated || '',
      username: m.username || '',
    }
    showMeta.value = true
  } catch {
    message.error(t('sessionManager.cannotReadMeta'))
  }
}

const renamePath = ref<string | null>(null)
const renameValue = ref('')

async function loadTree() {
  try {
    const raw = await GetTree()
    const parsed = JSON.parse(raw)
    tree.value = mergeExpandedState(tree.value, parsed || [])
    flattenTree()
  } catch {
    tree.value = []
    flatList.value = []
  }
}

function mergeExpandedState(oldTree: TreeNode[], newTree: TreeNode[]): TreeNode[] {
  const expandedMap = new Map<string, boolean>()
  function collect(nodes: TreeNode[]) { for (const n of nodes) { if (n.isDir && n.expanded) expandedMap.set(n.path, true); if (n.children) collect(n.children) } }
  collect(oldTree)
  function apply(nodes: TreeNode[]) { for (const n of nodes) { if (n.isDir) n.expanded = expandedMap.get(n.path) || false; if (n.children) apply(n.children) } }
  apply(newTree)
  return newTree
}

function flattenTree() {
  flatList.value = []
  function walk(nodes: TreeNode[], depth: number, ancestors: boolean[]) {
    nodes.forEach((node, idx) => {
      const isLast = idx === nodes.length - 1
      flatList.value.push({ ...node, depth, last: isLast, ancestors: [...ancestors], _ref: node })
      if (node.isDir && node.expanded && node.children?.length) {
        walk(node.children, depth + 1, [...ancestors, !isLast])
      }
    })
  }
  walk(tree.value, 0, [])
}

function collapseRecursive(node: TreeNode) {
  if (!node.isDir) return
  node.expanded = false
  if (node.children) node.children.forEach(collapseRecursive)
}

function toggleFolder(ref: TreeNode) {
  if (ref.expanded) {
    collapseRecursive(ref)
  } else {
    ref.expanded = true
  }
  flattenTree()
}

function handleClick(node: TreeNode) {
  renamePath.value = null
  if (node.isDir) {
    const ref = (node as any)._ref || node; toggleFolder(ref)
  }
}

function startRename(node: TreeNode) {
  renamePath.value = node.path; renameValue.value = node.name
  nextTick(() => { const inp = document.querySelector('.rename-input input') as HTMLInputElement; if (inp) { inp.focus(); inp.select() } })
}

async function finishRename() {
  if (!renamePath.value || !renameValue.value.trim()) { renamePath.value = null; return }
  const newName = renameValue.value.trim()
  if (newName === renamePath.value.split('/').pop()?.replace('.toml', '')) { renamePath.value = null; return }
  try { await RenameItem(renamePath.value, newName); renamePath.value = null; loadTree() } catch (err: any) { message.error(t('sessionManager.renameFailed', { err: (err && err.message) || t('sessionManager.unknownError') })) }
}

async function confirmDelete(n: TreeNode) {
  deleteTarget.value = { path: n.path, name: n.name, isDir: n.isDir }; showDeleteConfirm.value = true
}

async function executeDelete() {
  if (!deleteTarget.value) return
  const d = deleteTarget.value
  try { if (d.isDir) await DeleteFolder(d.path); else await DeleteSession(d.path); showDeleteConfirm.value = false; deleteTarget.value = null; loadTree(); emit('refresh') } catch (err: any) { message.error(t('sessionManager.deleteFailed', { err: (err && err.message) || t('sessionManager.unknownError') })) }
}

function cancelDelete() { showDeleteConfirm.value = false; deleteTarget.value = null }

function handleDblClick(node: TreeNode) {
  if (!node.isDir) {
    emit('select', node.path)
  }
}

function getProtocolIcon(protocol?: string) {
  switch (protocol) { case 'ssh': return TerminalOutline; case 'telnet': return TerminalOutline; case 'serial': return RadioOutline; case 'rdp': return DesktopOutline; default: return TerminalOutline }
}
function getProtocolColor(protocol?: string) {
  switch (protocol) { case 'ssh': return '#4ec9b0'; case 'telnet': return '#569cd6'; case 'serial': return '#c586c0'; case 'rdp': return '#c586c0'; default: return '#6e9fc7' }
}

function getContextMenuOptions(node: TreeNode): DropdownOption[] {
  if (node.isDir) {
    return [
      { label: t('sessionManager.newSession'), key: 'new-session', icon: () => h(NIcon, { size: 14 }, { default: () => h(AddOutline) }) },
      { label: t('sessionManager.newFolder'), key: 'new-folder', icon: () => h(NIcon, { size: 14 }, { default: () => h(AddOutline) }) },
      { type: 'divider', key: 'd1' },
      { label: t('sessionManager.importSessions'), key: 'import', icon: () => h(NIcon, { size: 14 }, { default: () => h(AddOutline) }) },
      { label: t('sessionManager.exportSessions'), key: 'export', icon: () => h(NIcon, { size: 14 }, { default: () => h(AddOutline) }) },
      { type: 'divider', key: 'd2' },
      { label: t('sessionManager.rename'), key: 'rename', icon: () => h(NIcon, { size: 14 }, { default: () => h(CreateOutline) }) },
      { label: t('sessionManager.deleteFolder'), key: 'delete-folder', icon: () => h(NIcon, { size: 14 }, { default: () => h(TrashOutline) }) },
    ]
  }
  return [
    { label: t('sessionManager.connectSession'), key: 'connect', icon: () => h(NIcon, { size: 14 }, { default: () => h(LogInOutline) }) },
    { label: t('sessionManager.editSession'), key: 'edit', icon: () => h(NIcon, { size: 14 }, { default: () => h(CreateOutline) }) },
    { label: t('sessionManager.viewMeta'), key: 'meta', icon: () => h(NIcon, { size: 14 }, { default: () => h(SearchOutline) }) },
    { type: 'divider', key: 'd1' },
    { label: t('sessionManager.rename'), key: 'rename', icon: () => h(NIcon, { size: 14 }, { default: () => h(CreateOutline) }) },
    { label: t('sessionManager.deleteSession'), key: 'delete', icon: () => h(NIcon, { size: 14 }, { default: () => h(TrashOutline) }) },
  ]
}

function openContextMenu(e: MouseEvent, node: TreeNode) {
  e.preventDefault(); e.stopPropagation()
  ctxNode.value = node; ctxOptions.value = getContextMenuOptions(node)
  ctxX.value = e.clientX; ctxY.value = e.clientY; ctxShow.value = true
}

const rootMenuOptions = computed<DropdownOption[]>(() => [
  { label: t('sessionManager.newSession'), key: 'root-new-session', icon: () => h(NIcon, { size: 14 }, { default: () => h(AddOutline) }) },
  { label: t('sessionManager.newFolder'), key: 'root-new-folder', icon: () => h(NIcon, { size: 14 }, { default: () => h(FolderOutline) }) },
  { type: 'divider', key: 'd1' },
  { label: t('sessionManager.importSessions'), key: 'root-import', icon: () => h(NIcon, { size: 14 }, { default: () => h(AddOutline) }) },
  { label: t('sessionManager.exportSessions'), key: 'root-export', icon: () => h(NIcon, { size: 14 }, { default: () => h(AddOutline) }) },
])

function openRootContextMenu(e: MouseEvent) {
  ctxNode.value = null
  ctxOptions.value = rootMenuOptions.value
  ctxX.value = e.clientX
  ctxY.value = e.clientY
  ctxShow.value = true
}

function handleCtxSelect(key: string) {
  ctxShow.value = false
  if (ctxNode.value) {
    handleContextAction(key, ctxNode.value)
  } else {
    if (key === 'root-new-folder') emit('new-folder', '')
    if (key === 'root-new-session') emit('new-session', '')
    if (key === 'root-import') emit('import-sessions')
    if (key === 'root-export') emit('export-sessions')
  }
}

async function handleContextAction(key: string, node: TreeNode) {
  switch (key) {
    case 'rename': startRename(node); break
    case 'connect': emit('select', node.path); break
    case 'meta': viewMeta(node); break
    case 'new-session': emit('new-session', node.isDir ? node.path : ''); break
    case 'new-folder': emit('new-folder', node.isDir ? node.path : ''); break
    case 'edit': emit('edit-session', node.path); break
    case 'import': emit('import-sessions'); break
    case 'export': emit('export-sessions'); break
    case 'delete-folder': case 'delete': confirmDelete(node); break
  }
}

// ====== Drag & Drop ======

function onDragStart(e: DragEvent, node: TreeNode) {
  dragNodePath = node.path
  e.dataTransfer!.effectAllowed = 'move'
  e.dataTransfer!.setData('text/plain', node.path)
  ;(e.target as HTMLElement).classList.add('dragging')
}

function onDragOver(e: DragEvent, node: TreeNode) {
  e.preventDefault()
  e.stopPropagation()
  if (!dragNodePath || dragNodePath === node.path) return
  if (dragNodePath.startsWith(node.path + '/')) return
  e.dataTransfer!.dropEffect = 'move'
  if (dragHoverTimer) { clearTimeout(dragHoverTimer); dragHoverTimer = null }
  const highlightPath = node.isDir ? node.path : (node.path.includes('/') ? node.path.substring(0, node.path.lastIndexOf('/')) : '')
  dropTargetPath.value = highlightPath
  if (node.isDir && !node.expanded) {
    dragHoverTimer = setTimeout(() => {
      const ref = (node as any)._ref || node
      if (ref && !ref.expanded) { ref.expanded = true; flattenTree() }
      dragHoverTimer = null
    }, 1000)
  }
}

function onDragLeave(e: DragEvent, node: TreeNode) {
  const related = e.relatedTarget as HTMLElement | null
  if (related && (e.currentTarget as HTMLElement).contains(related)) return
  if (dropTargetPath.value === node.path) {
    dropTargetPath.value = null
    if (dragHoverTimer) { clearTimeout(dragHoverTimer); dragHoverTimer = null }
  }
}

function onTreeDragOver(e: DragEvent) {
  e.preventDefault()
  if (dragNodePath) {
    e.dataTransfer!.dropEffect = 'move'
    dropRoot.value = true
  }
}

function onTreeDragLeave(e: DragEvent) {
  const related = e.relatedTarget as HTMLElement | null
  if (related && (e.currentTarget as HTMLElement).contains(related)) return
  dropRoot.value = false
}

function onTreeDrop(e: DragEvent) {
  dropRoot.value = false
  onDrop(e)
}

async function onDrop(e: DragEvent, targetNode?: TreeNode) {
  e.preventDefault()
  e.stopPropagation()
  if (!dragNodePath || dragNodePath === targetNode?.path) return
  if (targetNode && dragNodePath.startsWith(targetNode.path + '/')) return

  let destFolder = ''
  if (targetNode?.isDir) {
    destFolder = targetNode.path
  } else if (targetNode) {
    const parts = targetNode.path.split('/'); parts.pop(); destFolder = parts.join('/')
  } else {
    destFolder = '.'
  }

  const dragNode = flatList.value.find(n => n.path === dragNodePath)
  if (!dragNode) return

  const dragParent = dragNode.path.includes('/') ? dragNode.path.substring(0, dragNode.path.lastIndexOf('/')) : ''
  const normalizedDest = destFolder === '.' ? '' : destFolder
  if (dragParent === normalizedDest) {
    dragNodePath = null; dropTargetPath.value = null; dropRoot.value = false; return
  }

  const name = dragNode.path.split('/').pop()?.replace('.toml', '') || dragNode.name
  const hasConflict = await checkNameConflict(normalizedDest, name, dragNode.isDir, dragNodePath)

  if (hasConflict) {
    pendingDragPath.value = dragNodePath
    pendingDestFolder.value = destFolder
    pendingIsDir.value = dragNode.isDir
    conflictName.value = name
    showConflict.value = true
  } else {
    await doMove(dragNodePath, destFolder, dragNode.isDir)
  }

  dragNodePath = null; dropTargetPath.value = null; dropRoot.value = false
}

function onDragEnd(e: DragEvent) {
  ;(e.target as HTMLElement).classList.remove('dragging')
  dropTargetPath.value = null
  dropRoot.value = false
  if (dragHoverTimer) { clearTimeout(dragHoverTimer); dragHoverTimer = null }
}

async function checkNameConflict(folderPath: string, name: string, isDir: boolean, excludePath?: string): Promise<boolean> {
  try {
    const raw = await GetTree()
    const tree = JSON.parse(raw) || []
    let siblings = tree
    if (folderPath) {
      const parts = folderPath.split('/')
      let current = tree
      for (const part of parts) {
        const node = current.find((n: any) => n.name === part && n.isDir)
        if (node?.children) current = node.children; else break
      }
      siblings = current
    }
    return siblings.some((n: any) => n.name === name && n.isDir === isDir && n.path !== excludePath)
  } catch { return false }
}

async function doMove(path: string, destFolder: string, isDir: boolean) {
  try {
    if (isDir) await MoveFolder(path, destFolder || '.')
    else await MoveFile(path, destFolder || '.')
    await loadTree()
  } catch (err) { console.error('Move error:', err) }
}

async function handleRenameAndMove() {
  if (!pendingDragPath.value || !conflictName.value.trim()) return
  const newName = conflictName.value.trim()
  const dest = pendingDestFolder.value
  const renamePath = pendingDragPath.value
  const isDir = pendingIsDir.value

  const stillConflict = await checkNameConflict(dest === '.' ? '' : dest, newName, isDir, renamePath)
  if (stillConflict) return

  try {
    const oldBase = renamePath.split('/').pop() || ''
    if (isDir) {
      await MoveFolder(renamePath, dest || '.')
    } else {
      await MoveFile(renamePath, dest || '.')
    }
    const movedPath = (dest && dest !== '.' && dest !== '') ? dest + '/' + oldBase : oldBase
    await RenameItem(movedPath, newName)
    await loadTree()
  } catch (err: any) { message.error(t('sessionManager.renameFailed', { err: (err && err.message) || t('sessionManager.unknownError') })) }

  showConflict.value = false
  pendingDragPath.value = null; pendingDestFolder.value = ''
}

function cancelMove() {
  showConflict.value = false
  pendingDragPath.value = null; pendingDestFolder.value = ''
}

const canMove = computed(() => {
  if (!conflictName.value.trim() || !pendingDragPath.value) return false
  const origName = (pendingDragPath.value || '').split('/').pop()?.replace('.toml', '') || ''
  if (conflictName.value.trim() === origName) return false
  return true
})

function getDropClass(node: TreeNode): string {
  if (dropTargetPath.value !== node.path) return ''
  if (node.isDir) return 'drop-target drop-folder'
  return 'drop-target'
}

onMounted(() => { loadTree(); offTree = Events.On('session-tree-changed', () => loadTree()) })
onBeforeUnmount(() => { offTree?.() })

function renameSelected() {
  const node = flatList.value.find(n => n.path === props.activeSessionPath)
  if (node) startRename(node)
}

function deleteSelected() {
  const node = flatList.value.find(n => n.path === props.activeSessionPath)
  if (node) confirmDelete(node)
}

defineExpose({ renameSelected, deleteSelected })
</script>

<template>
  <div class="session-manager">
    <n-input v-model:value="searchQuery" size="tiny" :placeholder="t('sessionManager.searchPlaceholder')" clearable class="session-search-input">
      <template #prefix><n-icon :size="14" :component="SearchOutline" /></template>
    </n-input>
    <div
      class="session-tree"
      :class="{ 'drop-root': dropRoot }"
      @dragover="onTreeDragOver"
      @dragleave="onTreeDragLeave"
      @drop="onTreeDrop"
      @contextmenu="(e: MouseEvent) => openRootContextMenu(e)"
    >
      <div v-if="filteredList.length === 0" class="session-empty">
        <n-empty :description="t('sessionManager.noSessions')" size="small" />
      </div>

      <div
        v-for="node in filteredList"
        :key="node.path"
        class="tree-node"
        :class="[getDropClass(node), { active: props.activeSessionPath === node.path }]"
        :style="{ paddingLeft: (8 + (node.depth || 0) * 16) + 'px' }"
        draggable="true"
        @click="handleClick(node)"
        @dblclick="handleDblClick(node)"
        @contextmenu="(e: MouseEvent) => openContextMenu(e, node)"
        @dragstart="(e: DragEvent) => onDragStart(e, node)"
        @dragover="(e: DragEvent) => onDragOver(e, node)"
        @dragleave="(e: DragEvent) => onDragLeave(e, node)"
        @drop="(e: DragEvent) => onDrop(e, node)"
        @dragend="onDragEnd"
      >
        <div v-if="renamePath !== node.path" style="display:flex;align-items:center;gap:4px;flex:1;min-width:0">
          <div v-for="d in (node.depth || 0)" :key="d" class="indent-guide" :style="{ left: (d * 16 - 3) + 'px' }" :class="{ 'guide-hide': !node.ancestors?.[d - 1] }" />
          <div v-if="node.depth" class="indent-guide" :class="{ 'guide-last': node.last }" :style="{ left: ((node.depth || 0) * 16 - 3) + 'px' }" />
          <n-icon v-if="node.isDir" :size="12" :component="ChevronForwardOutline" class="tree-icon" :class="{ rotated: node.expanded }" />
          <n-icon v-else :size="14" :component="getProtocolIcon(node.protocol)" class="tree-icon" :style="{ color: getProtocolColor(node.protocol) }" />
          <span class="node-name">{{ node.name }}</span>
        </div>
        <div v-else style="flex:1;min-width:0">
          <n-input v-model:value="renameValue" size="tiny" class="rename-input" @keyup.enter="finishRename" @blur="finishRename" @click.stop />
        </div>
        <n-dropdown :options="getContextMenuOptions(node)" trigger="click" @select="(key: string) => handleContextAction(key, node)" placement="bottom-end">
          <n-icon :size="14" :component="EllipsisVerticalOutline" class="node-more" @click.stop />
        </n-dropdown>
      </div>
    </div>

    <n-dropdown :show="ctxShow" :options="ctxOptions" :x="ctxX" :y="ctxY" placement="bottom-start" @select="handleCtxSelect" @clickoutside="ctxShow = false" />

    <n-modal v-model:show="showConflict" :title="t('sessionManager.conflictTitle')" preset="dialog" :show-icon="false" :mask-closable="false" style="width: 400px">
      <n-form label-placement="left">
        <n-form-item :label="t('sessionManager.conflictLabel')">
          <n-input v-model:value="conflictName" :placeholder="t('sessionManager.newNamePlaceholder')" @keyup.enter="handleRenameAndMove" />
        </n-form-item>
      </n-form>
      <template #action>
        <n-button @click="cancelMove">{{ t('common.cancel') }}</n-button>
        <n-button type="primary" :disabled="!canMove" @click="handleRenameAndMove">{{ t('sessionManager.move') }}</n-button>
      </template>
    </n-modal>
    <n-modal v-model:show="showMeta" :title="t('sessionManager.metaTitle')" preset="dialog" :show-icon="false" style="width: 420px" :mask-closable="false">
      <n-descriptions v-if="metaInfo" bordered :column="1" size="small" label-style="width:100px">
        <n-descriptions-item :label="t('sessionManager.sessionName')">{{ metaInfo.name || '--' }}</n-descriptions-item>
        <n-descriptions-item :label="t('sessionManager.type')">{{ metaInfo.protocol || '--' }}</n-descriptions-item>
        <n-descriptions-item :label="metaInfo.protocol === 'serial' ? t('sessionManager.devicePath') : t('sessionManager.ipAddress')">{{ metaInfo.host || '--' }}</n-descriptions-item>
        <n-descriptions-item :label="t('common.port')">{{ metaInfo.port || '--' }}</n-descriptions-item>
        <n-descriptions-item v-if="metaInfo.username" :label="t('common.username')">{{ metaInfo.username }}</n-descriptions-item>
        <n-descriptions-item :label="t('sessionManager.createdTime')">{{ metaInfo.created || '--' }}</n-descriptions-item>
        <n-descriptions-item :label="t('sessionManager.updatedTime')">{{ metaInfo.updated || '--' }}</n-descriptions-item>
      </n-descriptions>
      <template #action><n-button @click="showMeta = false">{{ t('common.close') }}</n-button></template>
    </n-modal>
    <n-modal v-model:show="showDeleteConfirm" :title="t('sessionManager.deleteConfirmTitle')" preset="dialog" :show-icon="false" style="width: 420px" :closable="false" :mask-closable="false">
      <div style="font-size:14px">
        <p>{{ t('sessionManager.deleteConfirmMsg', { name: deleteTarget?.name }) }}</p>
        <p v-if="deleteTarget?.isDir" style="margin-top:8px;color:#e45858;font-size:12px">{{ t('sessionManager.deleteFolderWarn') }}</p>
      </div>
      <template #action>
        <n-button @click="cancelDelete">{{ t('common.cancel') }}</n-button>
        <n-button type="error" @click="executeDelete">{{ t('sessionManager.confirmDelete') }}</n-button>
      </template>
    </n-modal>
  </div>
</template>

<style scoped>
.session-manager { width: 100%; height: 100%; display: flex; flex-direction: column; overflow: hidden; }
.session-title { font-size: 11px; font-weight: 600; color: var(--text-color, #d4d4d4); text-transform: uppercase; letter-spacing: 0.8px; }
.header-actions { display: flex; gap: 2px; }
.session-search-input { flex-shrink: 0; width: 98%; align-self: center; }
.session-search-input :deep(.n-input) { border-radius: 0; }
.session-tree { flex: 1; overflow-y: auto; overflow-x: hidden; padding: 2px 0; }
.session-empty { height: 100%; display: flex; align-items: center; justify-content: center; }
.tree-node { display: flex; align-items: center; gap: 4px; height: 24px; cursor: pointer; transition: background 0.1s; user-select: none; position: relative; }
.tree-node:hover { background: rgba(255, 255, 255, 0.05); }
.tree-node.active { background: rgba(0, 120, 212, 0.2); }
.tree-node.dragging { opacity: 0.4; }
.tree-node.drop-target { background: rgba(0, 120, 212, 0.25); }
.tree-node.drop-folder { background: rgba(0, 120, 212, 0.35); box-shadow: inset 0 0 0 1px rgba(0, 120, 212, 0.6); }
.session-tree.drop-root { background: rgba(0, 120, 212, 0.1); box-shadow: inset 0 0 0 1px rgba(0, 120, 212, 0.4); }
.indent-guide { position: absolute; top: 0; bottom: 0; width: 1px; border-left: 1px solid #444; pointer-events: none; }
.indent-guide.guide-hide { border-color: transparent; }
.indent-guide.guide-last { bottom: 50%; }
.tree-icon { flex-shrink: 0; width: 16px; text-align: center; }
.arrow-icon { color: #888; transition: transform 0.15s ease; }
.rotated { transform: rotate(90deg); }
.node-name { flex: 1; min-width: 0; font-size: 13px; color: var(--text-color, #d4d4d4); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; text-align: left; }
.node-more { flex-shrink: 0; opacity: 0; color: #888; transition: opacity 0.15s; cursor: pointer; margin-right: 4px; }
.tree-node:hover .node-more { opacity: 0.6; }
.node-more:hover { opacity: 1 !important; }
.rename-input :deep(.n-input__input) { height: 20px; font-size: 12px; text-align: left; }
</style>
