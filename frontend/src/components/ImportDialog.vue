<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { NModal, NButton, NInput, NIcon, NSelect, NCheckbox, NScrollbar, useMessage } from 'naive-ui'
import { ChevronForwardOutline, DocumentLockOutline } from '@vicons/ionicons5'
import { GetImportTree, ImportSessions, PickImportFile, GetImportPackageTree, GetImportPackageKeys } from '../../bindings/changeme/internal/services/sessionfileservice.js'
import { useI18n } from 'vue-i18n'

interface TreeNode {
  name: string; path: string; isDir: boolean
  children?: TreeNode[]; expanded?: boolean
  checked?: boolean; indeterminate?: boolean
  depth?: number
  _ref?: TreeNode
}

interface PkgKey { name: string; type: string; fingerprint: string }

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ (e: 'update:show', v: boolean): void; (e: 'done'): void }>()
const message = useMessage()
const { t } = useI18n()

const localTree = ref<TreeNode[]>([])
const localLoaded = ref(false)
const pkgTree = ref<TreeNode[]>([])
const pkgTreeError = ref('')
const pkgKeys = ref<PkgKey[]>([])
const password = ref('')
const pwError = ref('')
const selectedPath = ref('')
const filePath = ref('')
const fileName = ref('')
const overwrite = ref('skip')
const importing = ref(false)

const showConfirm = ref(false)
const confirmTree = ref<TreeNode[]>([])
const showCloseAsk = ref(false)

// 口令规则:必填、8~64 位、大写/小写/数字/符号至少三类
function passwordCategories(pw: string): number {
  const flags = [false, false, false, false]
  for (const ch of pw) {
    if (ch >= 'A' && ch <= 'Z') flags[0] = true
    else if (ch >= 'a' && ch <= 'z') flags[1] = true
    else if (ch >= '0' && ch <= '9') flags[2] = true
    else flags[3] = true
  }
  return flags.filter(Boolean).length
}

const pwStrength = computed(() => {
  if (!password.value) return ''
  const n = [...password.value].length
  const cats = passwordCategories(password.value)
  if (n < 8 || n > 64) return t('importDialog.pwLengthError')
  if (cats < 3) return t('importDialog.pwCategoryError')
  return 'ok'
})

function validatePassword(pw: string): string | null {
  if (!pw) return t('importDialog.pwEmptyError')
  return null
}

const hasFile = computed(() => !!filePath.value)
const overwriteOptions = computed(() => [
  { label: t('importDialog.overwriteSkip'), value: 'skip' },
  { label: t('importDialog.overwriteReplace'), value: 'overwrite' },
])
const pwValid = computed(() => validatePassword(password.value) === null)

function resetAll() {
  password.value = ''
  pwError.value = ''
  selectedPath.value = ''
  filePath.value = ''
  fileName.value = ''
  overwrite.value = 'skip'
  pkgTree.value = []
  pkgTreeError.value = ''
  pkgKeys.value = []
  showConfirm.value = false
}

watch(() => props.show, (val) => {
  if (val) {
    loadLocalTree()
    resetAll()
  }
})

async function loadLocalTree() {
  localLoaded.value = false
  try { localTree.value = JSON.parse(await GetImportTree()) || [] } catch { localTree.value = [] }
  localLoaded.value = true
}

// ====== 左侧:包内文件夹树(多选) ======

// 注意:flatList 渲染的是 { ...n } 拷贝,修改操作必须通过 _ref 定位原树节点
function togglePkgExpand(n: TreeNode) {
  const ref = (n as any)._ref || n
  ref.expanded = !ref.expanded
}

// 勾选状态由事件参数驱动,避免受控组件回弹;同时联动子级与父级
function setPkgCheck(n: TreeNode, checked: boolean) {
  const ref = (n as any)._ref || n
  ref.checked = checked
  ref.indeterminate = false
  propagatePkgCheck(ref, checked)
  updatePkgParent(ref)
}

function propagatePkgCheck(n: TreeNode, checked: boolean) {
  if (n.children) n.children.forEach(c => {
    c.checked = checked; c.indeterminate = false
    propagatePkgCheck(c, checked)
  })
}

function updatePkgParent(node: TreeNode) {
  function findParent(nodes: TreeNode[], target: TreeNode): TreeNode | null {
    for (const n of nodes) {
      if (n.children) {
        if (n.children.includes(target)) return n
        const found = findParent(n.children, target)
        if (found) return found
      }
    }
    return null
  }
  const p = findParent(pkgTree.value, node)
  if (!p) return
  const allChecked = p.children?.every(c => c.checked && !c.indeterminate)
  const someChecked = p.children?.some(c => c.checked || c.indeterminate)
  p.checked = allChecked || false
  p.indeterminate = !allChecked && (someChecked || false)
  updatePkgParent(p)
}

const pkgFlat = computed(() => {
  const out: TreeNode[] = []
  function walk(nodes: TreeNode[], depth: number) {
    for (const n of nodes) {
      out.push({ ...n, depth, _ref: n })
      if (n.isDir && n.expanded && n.children?.length) walk(n.children, depth + 1)
    }
  }
  walk(pkgTree.value, 0)
  return out
})

const hasPkgSelection = computed(() => {
  function walk(nodes: TreeNode[]): boolean {
    for (const n of nodes) {
      if (n.checked && !n.indeterminate) return true
      if (n.children && walk(n.children)) return true
    }
    return false
  }
  return walk(pkgTree.value)
})

// ====== 右侧:本地文件夹树(单选,顶层为 sessions 根) ======

function toggleLocalExpand(n: TreeNode) {
  const ref = (n as any)._ref || n
  ref.expanded = !ref.expanded
}

function selectLocal(n: TreeNode) { selectedPath.value = n.path }

// 行点击:选中目标 + 文件夹同时展开/收纳(chevron 点击仅展开,不改变选中)
function handleLocalRowClick(n: TreeNode) {
  selectedPath.value = n.path
  if (n.isDir) toggleLocalExpand(n)
}

const localFlat = computed(() => {
  const out: TreeNode[] = []
  // sessions 根节点恒为最顶层(展开状态),其下才是各子文件夹
  out.push({ name: t('importDialog.sessionRoot'), path: '', isDir: true, depth: 0, expanded: true })
  function walk(nodes: TreeNode[], depth: number) {
    for (const n of nodes) {
      out.push({ ...n, depth, _ref: n })
      if (n.isDir && n.expanded && n.children?.length) walk(n.children, depth + 1)
    }
  }
  walk(localTree.value, 1)
  return out
})

// ====== 文件选择与包解析 ======

async function pickFile() {
  try {
    const path = await PickImportFile()
    if (path) {
      filePath.value = path
      fileName.value = path.split(/[/\\]/).pop() || path
      resetPackageState()
    }
  } catch {}
}

function onDrop(e: DragEvent) {
  e.preventDefault()
  const file = e.dataTransfer?.files?.[0]
  if (!file) return
  const p = (file as any).path
  if (!p) { message.error(t('importDialog.noFilePath')); return }
  filePath.value = p
  fileName.value = file.name
  resetPackageState()
}

function onDragOver(e: DragEvent) {
  e.preventDefault()
  e.dataTransfer!.dropEffect = 'copy'
}

function resetPackageState() {
  pkgTree.value = []
  pkgTreeError.value = ''
  pkgKeys.value = []
  parsedOk.value = false
}

// 手动触发解析:仅点击"确定"时才读取包,避免自动识别浪费 CPU
const parsing = ref(false)
const parsedOk = ref(false)

async function manualParse() {
  if (!filePath.value) return
  const err = validatePassword(password.value)
  if (err) { pwError.value = err; return }
  pwError.value = ''
  parsing.value = true
  try {
    await loadPackageTree()
  } finally {
    parsing.value = false
  }
}

// 密码变化后旧解析结果失效,清空等待重新解析
watch(password, () => {
  if (filePath.value) resetPackageState()
})

async function loadPackageTree() {
  if (!filePath.value) return
  const err = validatePassword(password.value)
  if (err) { pkgTreeError.value = err; pkgKeys.value = []; return }
  pkgTree.value = []
  pkgTreeError.value = ''
  pkgKeys.value = []
  try {
    const json = await GetImportPackageTree(filePath.value, password.value)
    if (!json) {
      pkgTreeError.value = t('importDialog.cannotReadPkg')
      return
    }
    pkgTree.value = JSON.parse(json) || []
    if (pkgTree.value.length === 0) {
      pkgTreeError.value = t('importDialog.pkgEmpty')
      return
    }
    parsedOk.value = true
    const keysJSON = await GetImportPackageKeys(filePath.value, password.value)
    if (keysJSON) {
      const parsed = JSON.parse(keysJSON)
      if (Array.isArray(parsed)) pkgKeys.value = parsed
    }
  } catch (e: any) {
    pkgTreeError.value = e.message || t('importDialog.cannotReadPkg2')
  }
}

// ====== 二次确认:重排选中包内树 ======

function buildSelectedTree(nodes: TreeNode[]): TreeNode[] {
  const result: TreeNode[] = []
  for (const n of nodes) {
    const children = n.children ? buildSelectedTree(n.children) : []
    if (n.checked && !n.indeterminate) {
      // 文件夹整体选中:下钻展示其下所有选中的会话文件
      const node: TreeNode = { name: n.name, path: n.path, isDir: n.isDir }
      if (children.length) node.children = children
      result.push(node)
      continue
    }
    if (children.length) result.push({ name: n.name, path: n.path, isDir: true, children })
  }
  return result
}

// 收集勾选路径:文件夹=整体递归,文件=单文件
function getSelectedPaths(): string[] {
  const paths: string[] = []
  function walk(nodes: TreeNode[]) {
    for (const n of nodes) {
      if (n.checked && !n.indeterminate) {
        paths.push(n.path)
        continue
      }
      if (n.children) walk(n.children)
    }
  }
  walk(pkgTree.value)
  return paths
}

function openConfirm() {
  pwError.value = ''
  confirmTree.value = buildSelectedTree(pkgTree.value)
  showConfirm.value = true
}

// 确认弹窗中重排树的扁平展开(全展开)
const confirmFlat = computed(() => {
  const out: TreeNode[] = []
  function walk(nodes: TreeNode[], depth: number) {
    for (const n of nodes) {
      out.push({ ...n, depth })
      if (n.children?.length) walk(n.children, depth + 1)
    }
  }
  walk(confirmTree.value, 0)
  return out
})

async function doImport() {
  if (!filePath.value) { pwError.value = t('importDialog.noFileError'); return }
  const err = validatePassword(password.value)
  if (err) { pwError.value = err; return }
  if (!hasPkgSelection.value) { pwError.value = t('importDialog.noPkgSelection'); return }
  importing.value = true
  try {
    const summary = await ImportSessions(password.value, selectedPath.value || '.', filePath.value, overwrite.value === 'overwrite', getSelectedPaths())
    message.success(summary || t('importDialog.importDone'))
    showConfirm.value = false
    emit('done')
  } catch (e: any) {
    pwError.value = e.message || t('importDialog.importFailed')
    showConfirm.value = false
  }
  importing.value = false
}

// ====== 右上角关闭:询问并清除解密内容 ======

function handleCloseAsk() {
  if (filePath.value) {
    showCloseAsk.value = true
  } else {
    emit('update:show', false)
  }
}

function confirmClose() {
  showCloseAsk.value = false
  resetAll()
  emit('update:show', false)
}
</script>

<template>
  <n-modal :show="props.show" :title="t('importDialog.title')" preset="dialog" :show-icon="false" :mask-closable="false" style="width: 720px; max-width: 94vw" @update:show="(v) => { if (!v) handleCloseAsk() }">
    <div class="import-head">
      <div class="head-col">
        <div class="head-col-title">{{ t('importDialog.fileTitle') }}</div>
        <div class="file-drop-zone" :class="{ 'has-file': !!filePath }" @click="pickFile" @drop="onDrop" @dragover="onDragOver">
          <template v-if="!filePath">
            <n-icon :size="22" :component="DocumentLockOutline" style="color:#888" />
            <span class="drop-text">{{ t('importDialog.pickHint') }}</span>
          </template>
          <template v-else>
            <n-icon :size="18" :component="DocumentLockOutline" style="color:#4ec9b0" />
            <span class="file-name">{{ fileName }}</span>
          </template>
        </div>
      </div>
      <div class="head-col head-right">
        <div class="head-col-title-row">
          <span class="head-col-title">{{ t('importDialog.passwordTitle') }}</span>
          <n-button size="small" type="primary" ghost :disabled="!hasFile || !pwValid" :loading="parsing" @click="manualParse">{{ t('importDialog.confirmParse') }}</n-button>
        </div>
        <n-input v-model:value="password" type="password" show-password-on="click" size="small" :placeholder="t('importDialog.passwordPlaceholder')" :disabled="!hasFile" @keyup.enter="manualParse" />
      </div>
    </div>

    <div class="tree-panes">
      <div class="tree-pane">
        <div class="tree-pane-title">{{ t('importDialog.pkgTreeTitle') }}</div>
        <n-scrollbar style="height: 220px" class="tree-scroller">
          <div v-if="pkgFlat.length > 0" class="tree">
            <div
              v-for="n in pkgFlat"
              :key="n.path"
              class="tree-row"
              :style="{ paddingLeft: (8 + (n.depth || 0) * 18) + 'px' }"
              @click="n.isDir && togglePkgExpand(n)"
            >
              <span class="chevron" :class="{ empty: !n.isDir }" @click.stop="togglePkgExpand(n)">
                <n-icon v-if="n.isDir" :size="12" :component="ChevronForwardOutline" class="chevron-arrow" :class="{ rotated: n.expanded }" style="color:#888" />
              </span>
              <span @click.stop>
                <n-checkbox :checked="!!n.checked" :indeterminate="n.indeterminate" @update:checked="(v: boolean) => setPkgCheck(n, v)" />
              </span>
              <span class="tree-name" :class="{ file: !n.isDir }">{{ n.name }}</span>
            </div>
          </div>
          <div v-else-if="pkgTreeError" class="tree-tip error">{{ pkgTreeError }}</div>
          <div v-else-if="!hasFile" class="tree-tip">{{ t('importDialog.tipSelectFile') }}</div>
          <div v-else-if="parsing" class="tree-tip">{{ t('importDialog.tipParsing') }}</div>
          <div v-else class="tree-tip">{{ t('importDialog.tipClickConfirm') }}</div>
        </n-scrollbar>
      </div>
      <div class="tree-pane">
        <div class="tree-pane-title">{{ t('importDialog.localTreeTitle') }}</div>
        <n-scrollbar style="height: 220px" class="tree-scroller">
          <div class="tree">
            <div
              v-for="n in localFlat"
              :key="n.path || '/'"
              class="tree-row"
              :class="{ selected: selectedPath === n.path }"
              :style="{ paddingLeft: (8 + (n.depth || 0) * 18) + 'px' }"
              @click="handleLocalRowClick(n)"
            >
              <span class="chevron" @click.stop="toggleLocalExpand(n)">
                <n-icon :size="12" :component="ChevronForwardOutline" class="chevron-arrow" :class="{ rotated: n.expanded }" style="color:#888" />
              </span>
              <span class="tree-name">{{ n.name }}</span>
            </div>
          </div>
          <div v-if="!localLoaded" class="tree-tip">{{ t('importDialog.loading') }}</div>
          <div v-else-if="localFlat.length <= 1" class="tree-tip">{{ t('importDialog.tipNoLocal') }}</div>
        </n-scrollbar>
      </div>
    </div>

    <div v-if="pkgKeys.length > 0" class="key-tip">{{ t('importDialog.keyTip', { count: pkgKeys.length, names: pkgKeys.map(k => k.name).join('、') }) }}</div>

    <div class="bottom-hint">
      <span v-if="pwError" class="hint-error">{{ pwError }}</span>
      <span v-else-if="parsedOk" class="hint-ok">{{ t('importDialog.parseOk') }}</span>
      <span v-else-if="pwStrength && pwStrength !== 'ok'" class="hint-warn">{{ pwStrength }}</span>
      <span v-else class="hint-muted">{{ t('importDialog.strengthHint') }}</span>
    </div>

    <div class="bottom-row">
      <n-select v-model:value="overwrite" :disabled="!hasFile" size="small" style="width: 260px"
        :options="overwriteOptions" />
      <div style="flex:1" />
      <n-button type="primary" size="small" :loading="importing"
        :disabled="!hasFile || !pwValid || !hasPkgSelection"
        @click="openConfirm">{{ t('importDialog.import') }}</n-button>
    </div>
  </n-modal>

  <!-- 二次确认:重排后的选中包内树 + 导入目标 -->
  <n-modal v-model:show="showConfirm" :title="t('importDialog.confirmTitle')" preset="dialog" :show-icon="false" style="width: 460px" :closable="false" :mask-closable="false">
    <div class="confirm-target">{{ t('importDialog.confirmTarget') }}<b>{{ selectedPath ? '/' + selectedPath : t('importDialog.sessionRoot') }}</b></div>
    <div class="confirm-tree-title">{{ t('importDialog.confirmTreeTitle') }}</div>
    <n-scrollbar style="height: 220px" class="tree-scroller">
      <div v-if="confirmFlat.length > 0" class="tree">
        <div
          v-for="n in confirmFlat"
          :key="n.path"
          class="tree-row"
          :style="{ paddingLeft: (8 + (n.depth || 0) * 18) + 'px' }"
        >
          <span class="chevron" :class="{ empty: !n.isDir || !n.children?.length }">
            <n-icon v-if="n.isDir" :size="12" :component="ChevronForwardOutline" :class="{ rotated: n.expanded }" style="color:#888" />
          </span>
          <span class="tree-name" :class="{ file: !n.isDir }">{{ n.name }}</span>
          <span v-if="n.path.startsWith('keys/')" class="key-badge">{{ t('importDialog.keyBadge') }}</span>
        </div>
      </div>
      <div v-else class="tree-tip">{{ t('importDialog.tipNoSelection') }}</div>
    </n-scrollbar>
    <template #action>
      <n-button @click="showConfirm = false">{{ t('common.cancel') }}</n-button>
      <n-button type="primary" :loading="importing" @click="doImport">{{ t('importDialog.confirmImport') }}</n-button>
    </template>
  </n-modal>

  <!-- 关闭询问 -->
  <n-modal v-model:show="showCloseAsk" :title="t('importDialog.closeAskTitle')" preset="dialog" :show-icon="false" style="width: 380px" :closable="false" :mask-closable="false">
    <div>{{ t('importDialog.closeAskMsg') }}</div>
    <template #action>
      <n-button @click="showCloseAsk = false">{{ t('common.cancel') }}</n-button>
      <n-button type="primary" @click="confirmClose">{{ t('importDialog.confirmClose') }}</n-button>
    </template>
  </n-modal>
</template>

<style scoped>
.import-head { display: flex; gap: 16px; }
.head-col { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 6px; }
.head-col-title { font-size: 13px; color: var(--text-color, #d4d4d4); }
.head-col-title-row { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.head-right .head-col-title-row { flex: 1; }
.file-drop-zone {
  display: flex; align-items: center; justify-content: center; gap: 8px;
  height: 40px; border: 2px dashed var(--border-color, #3c3c3c);
  border-radius: 6px; cursor: pointer; transition: border-color .15s, background .15s;
  overflow: hidden; padding: 0 8px;
}
.file-drop-zone:hover { border-color: #4ec9b0; background: rgba(78,201,176,0.04); }
.file-drop-zone.has-file { border-style: solid; border-color: #4ec9b0; }
.drop-text { font-size: 12px; color: var(--icon-color, #888); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.file-name { font-size: 13px; color: var(--text-color, #d4d4d4); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.tree-panes { display: flex; gap: 12px; margin-top: 10px; }
.tree-pane { flex: 1; min-width: 0; border: 1px solid var(--border-color, #3c3c3c); border-radius: 6px; background: var(--card-bg, #1e1e1e); overflow: hidden; }
.tree-pane-title { font-size: 12px; color: var(--icon-color, #888); padding: 6px 10px; border-bottom: 1px solid var(--border-color, #3c3c3c); }
.tree { padding: 4px 0; }
.tree-row { display: flex; align-items: center; gap: 4px; height: 26px; cursor: pointer; padding: 0 4px; user-select: none; }
.tree-row:hover { background: rgba(255, 255, 255, 0.03); }
.tree-row.selected { background: rgba(0, 120, 212, 0.2); }
.chevron { width: 14px; display: inline-flex; flex-shrink: 0; }
.chevron.empty { visibility: hidden; }
.rotated { transform: rotate(90deg); }
.chevron-arrow { transition: transform 0.15s ease; }
.tree-name { font-size: 13px; color: var(--text-color, #d4d4d4); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.tree-name.file { color: var(--icon-color, #9a9a9a); }
.key-badge { margin-left: 6px; flex-shrink: 0; font-size: 11px; color: #4ec9b0; border: 1px solid rgba(78, 201, 176, 0.4); border-radius: 3px; padding: 0 4px; }
.tree-tip { padding: 10px; text-align: center; color: var(--icon-color, #888); font-size: 12px; }
.tree-tip.error { color: #e45858; }
.key-tip { margin-top: 8px; font-size: 12px; color: #4ec9b0; }
.bottom-hint { min-height: 18px; margin-top: 8px; font-size: 12px; }
.hint-error { color: #e45858; }
.hint-warn { color: #dca54c; }
.hint-ok { color: #4ec9b0; }
.hint-muted { color: var(--icon-color, #888); }
.bottom-row { display: flex; align-items: center; gap: 10px; margin-top: 8px; }
.confirm-target { font-size: 13px; color: var(--text-color, #d4d4d4); margin-bottom: 8px; }
.confirm-tree-title { font-size: 12px; color: var(--icon-color, #888); margin-bottom: 4px; }
</style>