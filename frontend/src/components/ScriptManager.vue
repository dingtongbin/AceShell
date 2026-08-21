<script setup lang="ts">
import { ref, computed, onMounted, nextTick, h } from 'vue'
import { NIcon, NEmpty, NDropdown, NModal, NInput, useMessage } from 'naive-ui'
import type { DropdownOption } from 'naive-ui'
import {
  ChevronForwardOutline,
  FolderOutline,
  DocumentOutline,
  DocumentTextOutline,
  AddOutline,
  CreateOutline,
  TrashOutline,
  EllipsisVerticalOutline,
  SearchOutline,
} from '@vicons/ionicons5'
import { GetScriptTree, CreateScriptFolder, DeleteScriptItem, MoveScriptItem } from '../../bindings/changeme/internal/services/filetreeservice.js'
import { CreateFile, RenameFile } from '../../bindings/changeme/internal/services/scriptfileservice.js'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

interface TreeNode {
  name: string
  path: string
  isDir: boolean
  children?: TreeNode[]
  expanded?: boolean
  depth?: number
  last?: boolean
  ancestors?: boolean[]
  _ref?: TreeNode
}

const emit = defineEmits<{
  (e: 'open-file', path: string): void
}>()

const tree = ref<TreeNode[]>([])
const flatList = ref<TreeNode[]>([])
const searchQuery = ref('')
const message = useMessage()

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

const renamePath = ref<string | null>(null)
const renameValue = ref('')

async function loadTree() {
  try {
    const raw = await GetScriptTree()
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

function handleDblClick(node: TreeNode) {
  if (!node.isDir) {
    emit('open-file', node.path)
  }
}

function startRename(node: TreeNode) {
  renamePath.value = node.path; renameValue.value = node.name
  nextTick(() => { const inp = document.querySelector('.script-rename-input input') as HTMLInputElement; if (inp) { inp.focus(); inp.select() } })
}

async function finishRename() {
  if (!renamePath.value || !renameValue.value.trim()) { renamePath.value = null; return }
  const newName = renameValue.value.trim()
  if (newName === renamePath.value.split('/').pop()) { renamePath.value = null; return }
  try {
    await RenameFile(renamePath.value, newName)
    renamePath.value = null
    loadTree()
  } catch (err: any) {
    message.error(t('scriptManager.renameFailed', { err: (err && err.message) || t('scriptManager.unknownError') }))
    renamePath.value = null
  }
}

async function confirmDelete(n: TreeNode) {
  deleteTarget.value = { path: n.path, name: n.name, isDir: n.isDir }; showDeleteConfirm.value = true
}

async function executeDelete() {
  if (!deleteTarget.value) return
  const d = deleteTarget.value
  try {
    await DeleteScriptItem(d.path)
    showDeleteConfirm.value = false
    deleteTarget.value = null
    loadTree()
  } catch (err: any) {
    message.error(t('scriptManager.deleteFailed', { err: (err && err.message) || t('scriptManager.unknownError') }))
  }
}

function cancelDelete() { showDeleteConfirm.value = false; deleteTarget.value = null }

function getContextMenuOptions(node: TreeNode): DropdownOption[] {
  if (node.isDir) {
    return [
      { label: t('scriptManager.newScript'), key: 'new-script', icon: () => h(NIcon, { size: 14 }, { default: () => h(DocumentTextOutline) }) },
      { label: t('scriptManager.newFolder'), key: 'new-folder', icon: () => h(NIcon, { size: 14 }, { default: () => h(AddOutline) }) },
      { type: 'divider', key: 'd1' },
      { label: t('scriptManager.rename'), key: 'rename', icon: () => h(NIcon, { size: 14 }, { default: () => h(CreateOutline) }) },
      { label: t('scriptManager.deleteFolder'), key: 'delete', icon: () => h(NIcon, { size: 14 }, { default: () => h(TrashOutline) }) },
    ]
  }
  return [
    { label: t('scriptManager.open'), key: 'open', icon: () => h(NIcon, { size: 14 }, { default: () => h(DocumentTextOutline) }) },
    { type: 'divider', key: 'd1' },
    { label: t('scriptManager.rename'), key: 'rename', icon: () => h(NIcon, { size: 14 }, { default: () => h(CreateOutline) }) },
    { label: t('scriptManager.delete'), key: 'delete', icon: () => h(NIcon, { size: 14 }, { default: () => h(TrashOutline) }) },
  ]
}

function openContextMenu(e: MouseEvent, node: TreeNode) {
  e.preventDefault(); e.stopPropagation()
  ctxNode.value = node; ctxOptions.value = getContextMenuOptions(node)
  ctxX.value = e.clientX; ctxY.value = e.clientY; ctxShow.value = true
}

const rootMenuOptions = computed<DropdownOption[]>(() => [
  { label: t('scriptManager.newScript'), key: 'root-new-script', icon: () => h(NIcon, { size: 14 }, { default: () => h(DocumentTextOutline) }) },
  { label: t('scriptManager.newFolder'), key: 'root-new-folder', icon: () => h(NIcon, { size: 14 }, { default: () => h(FolderOutline) }) },
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
    if (key === 'root-new-script') openNewScript('')
    if (key === 'root-new-folder') createFolder('')
  }
}

async function handleContextAction(key: string, node: TreeNode) {
  switch (key) {
    case 'open': emit('open-file', node.path); break
    case 'rename': startRename(node); break
    case 'new-script': openNewScript(node.isDir ? node.path : ''); break
    case 'new-folder': createFolder(node.isDir ? node.path : ''); break
    case 'delete': confirmDelete(node); break
  }
}

async function createFolder(parent: string) {
  const name = await uniqueName(parent, t('scriptManager.newFolder'), true)
  if (!name) return
  const path = parent ? parent + '/' + name : name
  try { await CreateScriptFolder(path); loadTree() } catch (err: any) { message.error(t('scriptManager.createFolderFailed', { err: (err && err.message) || t('scriptManager.unknownError') })) }
}

// ====== 新建脚本弹窗:预填"新建脚本.sh",以用户输入为准,不自动追加后缀 ======

const showNewScript = ref(false)
const newScriptName = ref('')
const newScriptParent = ref('')
const newScriptError = ref('')

function openNewScript(parent: string) {
  newScriptParent.value = parent
  newScriptName.value = t('scriptManager.newScriptPlaceholder')
  newScriptError.value = ''
  showNewScript.value = true
  nextTick(() => {
    const inp = document.querySelector('.new-script-input input') as HTMLInputElement | null
    if (inp) { inp.focus(); inp.select() }
  })
}

async function confirmNewScript() {
  let name = newScriptName.value.trim()
  if (!name) {
    name = await uniqueName(newScriptParent.value, t('scriptManager.newScriptPlaceholder'), false)
  } else {
    const exists = await checkNameConflict(newScriptParent.value, name, false)
    if (exists) { newScriptError.value = t('scriptManager.nameConflict'); return }
  }
  const path = newScriptParent.value ? newScriptParent.value + '/' + name : name
  try {
    await CreateFile(path)
    showNewScript.value = false
    loadTree()
  } catch (err: any) {
    newScriptError.value = (err && err.message) || t('scriptManager.createFailed')
  }
}

function cancelNewScript() { showNewScript.value = false }

async function uniqueName(parent: string, base: string, isDir: boolean): Promise<string> {
  try {
    const raw = await GetScriptTree()
    const root = JSON.parse(raw) || []
    let siblings: any[] = root
    if (parent) {
      const parts = parent.split('/')
      let current: any[] = root
      for (const part of parts) {
        const node = current.find((n: any) => n.name === part && n.isDir)
        if (node?.children) current = node.children; else break
      }
      siblings = current
    }
    let name = base
    let i = 2
    while (siblings.some((n: any) => n.name === name)) {
      const dot = base.lastIndexOf('.')
      name = (dot > 0 ? base.substring(0, dot) + ' (' + i + ')' + base.substring(dot) : base + ' (' + i + ')')
      i++
    }
    return name
  } catch { return base }
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

  const name = dragNode.path.split('/').pop() || dragNode.name
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
    const raw = await GetScriptTree()
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
    await MoveScriptItem(path, destFolder || '.')
    await loadTree()
  } catch (err: any) { message.error(t('scriptManager.moveFailed', { err: (err && err.message) || t('scriptManager.unknownError') })) }
}

async function handleRenameAndMove() {
  if (!pendingDragPath.value || !conflictName.value.trim()) return
  const newName = conflictName.value.trim()
  const dest = pendingDestFolder.value
  const renamePath = pendingDragPath.value

  const stillConflict = await checkNameConflict(dest === '.' ? '' : dest, newName, pendingIsDir.value, renamePath)
  if (stillConflict) { message.warning(t('scriptManager.stillConflict')); return }

  try {
    const oldBase = renamePath.split('/').pop() || ''
    await MoveScriptItem(renamePath, dest || '.')
    const movedPath = (dest && dest !== '.' && dest !== '') ? dest + '/' + oldBase : oldBase
    await RenameFile(movedPath, newName)
    await loadTree()
  } catch (err: any) { message.error(t('scriptManager.moveFailed', { err: (err && err.message) || t('scriptManager.unknownError') })) }

  showConflict.value = false
  pendingDragPath.value = null; pendingDestFolder.value = ''
}

function cancelMove() {
  showConflict.value = false
  pendingDragPath.value = null; pendingDestFolder.value = ''
}

const canMove = computed(() => {
  if (!conflictName.value.trim() || !pendingDragPath.value) return false
  const origName = (pendingDragPath.value || '').split('/').pop() || ''
  if (conflictName.value.trim() === origName) return false
  return true
})

function getDropClass(node: TreeNode): string {
  if (dropTargetPath.value !== node.path) return ''
  if (node.isDir) return 'drop-target drop-folder'
  return 'drop-target'
}

onMounted(loadTree)
</script>

<template>
  <div class="script-manager">
    <n-input v-model:value="searchQuery" size="tiny" :placeholder="t('scriptManager.searchPlaceholder')" clearable class="script-search-input">
      <template #prefix><n-icon :size="14" :component="SearchOutline" /></template>
    </n-input>
    <div
      class="script-tree"
      :class="{ 'drop-root': dropRoot }"
      @dragover="onTreeDragOver"
      @dragleave="onTreeDragLeave"
      @drop="onTreeDrop"
      @contextmenu="(e: MouseEvent) => openRootContextMenu(e)"
    >
      <div v-if="filteredList.length === 0" class="script-empty">
        <n-empty :description="t('scriptManager.noFiles')" size="small" />
      </div>

      <div
        v-for="node in filteredList"
        :key="node.path"
        class="tree-node"
        :class="[getDropClass(node)]"
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
          <n-icon v-else :size="14" :component="DocumentOutline" class="tree-icon" style="color: #6e9fc7" />
          <span class="node-name">{{ node.name }}</span>
        </div>
        <div v-else style="flex:1;min-width:0">
          <n-input v-model:value="renameValue" size="tiny" class="script-rename-input" @keyup.enter="finishRename" @blur="finishRename" @click.stop />
        </div>
        <n-dropdown :options="getContextMenuOptions(node)" trigger="click" @select="(key: string) => handleContextAction(key, node)" placement="bottom-end">
          <n-icon :size="14" :component="EllipsisVerticalOutline" class="node-more" @click.stop />
        </n-dropdown>
      </div>
    </div>

    <n-dropdown :show="ctxShow" :options="ctxOptions" :x="ctxX" :y="ctxY" placement="bottom-start" @select="handleCtxSelect" @clickoutside="ctxShow = false" />

    <n-modal v-model:show="showConflict" :title="t('scriptManager.conflictTitle')" preset="dialog" :show-icon="false" :mask-closable="false" style="width: 400px">
      <n-form label-placement="left">
        <n-form-item :label="t('scriptManager.conflictLabel')">
          <n-input v-model:value="conflictName" :placeholder="t('scriptManager.newNamePlaceholder')" @keyup.enter="handleRenameAndMove" />
        </n-form-item>
      </n-form>
      <template #action>
        <n-button @click="cancelMove">{{ t('common.cancel') }}</n-button>
        <n-button type="primary" :disabled="!canMove" @click="handleRenameAndMove">{{ t('scriptManager.move') }}</n-button>
      </template>
    </n-modal>
    <n-modal v-model:show="showDeleteConfirm" :title="t('scriptManager.deleteConfirmTitle')" preset="dialog" :show-icon="false" style="width: 420px" :closable="false" :mask-closable="false">
      <div style="font-size:14px">
        <p>{{ t('scriptManager.deleteConfirmMsg', { name: deleteTarget?.name }) }}</p>
        <p v-if="deleteTarget?.isDir" style="margin-top:8px;color:#e45858;font-size:12px">{{ t('scriptManager.deleteFolderWarn') }}</p>
      </div>
      <template #action>
        <n-button @click="cancelDelete">{{ t('common.cancel') }}</n-button>
        <n-button type="error" @click="executeDelete">{{ t('scriptManager.confirmDelete') }}</n-button>
      </template>
    </n-modal>
    <n-modal v-model:show="showNewScript" :title="t('scriptManager.newScriptTitle')" preset="dialog" :show-icon="false" style="width: 380px" :closable="false" :mask-closable="false">
      <div class="form-group">
        <label class="form-label">{{ t('scriptManager.nameLabel') }}</label>
        <n-input v-model:value="newScriptName" class="new-script-input" :placeholder="t('scriptManager.newScriptPlaceholder')" @keyup.enter="confirmNewScript" @keyup.esc="cancelNewScript" />
        <p v-if="newScriptError" style="margin-top: 8px; color: #e45858; font-size: 12px;">{{ newScriptError }}</p>
        <p style="margin-top: 8px; color: var(--icon-color); font-size: 12px;">{{ t('scriptManager.newScriptHint') }}</p>
      </div>
      <template #action>
        <n-button @click="cancelNewScript">{{ t('common.cancel') }}</n-button>
        <n-button type="primary" @click="confirmNewScript">{{ t('common.confirm') }}</n-button>
      </template>
    </n-modal>
  </div>
</template>

<style scoped>
.script-manager { width: 100%; height: 100%; display: flex; flex-direction: column; overflow: hidden; }
.script-search-input { flex-shrink: 0; width: 98%; align-self: center; }
.script-search-input :deep(.n-input) { border-radius: 0; }
.script-tree { flex: 1; overflow-y: auto; overflow-x: hidden; padding: 2px 0; }
.script-empty { height: 100%; display: flex; align-items: center; justify-content: center; }
.tree-node { display: flex; align-items: center; gap: 4px; height: 24px; cursor: pointer; transition: background 0.1s; user-select: none; position: relative; }
.tree-node:hover { background: rgba(255, 255, 255, 0.05); }
.tree-node.dragging { opacity: 0.4; }
.tree-node.drop-target { background: rgba(0, 120, 212, 0.25); }
.tree-node.drop-folder { background: rgba(0, 120, 212, 0.35); box-shadow: inset 0 0 0 1px rgba(0, 120, 212, 0.6); }
.script-tree.drop-root { background: rgba(0, 120, 212, 0.1); box-shadow: inset 0 0 0 1px rgba(0, 120, 212, 0.4); }
.indent-guide { position: absolute; top: 0; bottom: 0; width: 1px; border-left: 1px solid #444; pointer-events: none; }
.indent-guide.guide-hide { border-color: transparent; }
.indent-guide.guide-last { bottom: 50%; }
.tree-icon { flex-shrink: 0; width: 16px; text-align: center; }
.rotated { transform: rotate(90deg); }
.node-name { flex: 1; min-width: 0; font-size: 13px; color: var(--text-color, #d4d4d4); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; text-align: left; }
.node-more { flex-shrink: 0; opacity: 0; color: #888; transition: opacity 0.15s; cursor: pointer; margin-right: 4px; }
.tree-node:hover .node-more { opacity: 0.6; }
.node-more:hover { opacity: 1 !important; }
.script-rename-input :deep(.n-input__input) { height: 20px; font-size: 12px; text-align: left; }
</style>