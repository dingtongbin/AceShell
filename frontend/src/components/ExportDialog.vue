<script setup lang="ts">
import { ref, watch, onMounted, defineComponent, h, computed } from 'vue'
import { NModal, NButton, NInput, NCheckbox, NIcon, NEmpty, NScrollbar } from 'naive-ui'
import { ChevronForwardOutline } from '@vicons/ionicons5'
import { GetExportTree, ExportSessions } from '../../bindings/changeme/internal/services/sessionfileservice.js'

interface TreeNode {
  name: string; path: string; isDir: boolean
  children?: TreeNode[]; expanded?: boolean; checked?: boolean; indeterminate?: boolean
}

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ (e: 'update:show', v: boolean): void; (e: 'done'): void }>()

const tree = ref<TreeNode[]>([])
const password = ref('')
const pwError = ref('')
const exporting = ref(false)

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
  if (n < 8 || n > 64) return '长度须为 8~64 个字符'
  if (cats < 3) return '需包含大写/小写/数字/符号中至少三类'
  return 'ok'
})

function validatePassword(pw: string): string | null {
  if (!pw) return '导出口令不能为空'
  const n = [...pw].length
  if (n < 8 || n > 64) return '口令长度须为 8~64 个字符'
  if (passwordCategories(pw) < 3) return '口令需包含大写字母、小写字母、数字、符号中的至少三类'
  return null
}

// 递归排序：文件夹排在前，会话文件排在后；同类型内按名称排序
function sortTree(nodes: TreeNode[]) {
  nodes.sort((a, b) => {
    if (a.isDir !== b.isDir) return a.isDir ? -1 : 1
    return a.name.localeCompare(b.name)
  })
  nodes.forEach(n => { if (n.children?.length) sortTree(n.children) })
}

async function loadTree() {
  try {
    const parsed = JSON.parse(await GetExportTree()) || []
    tree.value = parsed
    sortTree(tree.value)
  } catch { tree.value = [] }
}

function toggleExpand(n: TreeNode) { n.expanded = !n.expanded }

// 勾选状态由事件参数驱动,避免受控组件回弹;同时联动子级与父级
function setChecked(n: TreeNode, checked: boolean) {
  n.checked = checked
  n.indeterminate = false
  propagateCheck(n, checked)
  updateParentState(n)
}

function propagateCheck(n: TreeNode, checked: boolean) {
  if (n.children) n.children.forEach(c => {
    c.checked = checked; c.indeterminate = false
    propagateCheck(c, checked)
  })
}

function updateParentState(node: TreeNode) {
  function findParent(nodes: TreeNode[], target: TreeNode): TreeNode | null {
    for (const n of nodes) {
      if (n.children && n.children.includes(target)) return n
      if (n.children) {
        const found = findParent(n.children, target)
        if (found) return found
      }
    }
    return null
  }
  const p = findParent(tree.value, node)
  if (!p) return
  const allChecked = p.children?.every(c => c.checked && !c.indeterminate)
  const someChecked = p.children?.some(c => c.checked || c.indeterminate)
  p.checked = allChecked || false
  p.indeterminate = !allChecked && (someChecked || false)
  updateParentState(p)
}

function getCheckedPaths(): string[] {
  const paths: string[] = []
  function walk(nodes: TreeNode[]) {
    for (const n of nodes) {
      if (n.checked && !n.indeterminate) {
        paths.push(n.path)
        if (n.isDir) continue
      }
      if (n.children) walk(n.children)
    }
  }
  walk(tree.value)
  return paths
}

const TreeRow = defineComponent({
  props: { node: { type: Object, required: true }, depth: { type: Number, default: 0 } },
  setup(props) {
    // setup 仅执行一次且组件实例可能复用:节点引用必须每次渲染从 props 获取
    return () => {
      const n = props.node as TreeNode
      return [
        h('div', {
          class: 'tree-row',
          style: { paddingLeft: `${8 + props.depth * 20}px` },
          onClick: () => {
            // 整行点击:文件夹展开/收纳,文件勾选;checkbox 已由 span @click.stop 保护不会误折叠
            if (n.isDir) toggleExpand(n)
            else setChecked(n, !n.checked)
          }
        }, [
          n.isDir
            ? h('span', { class: 'chevron', style: { width: '14px', display: 'inline-flex' } }, [
                h(NIcon, {
                  size: 12,
                  component: ChevronForwardOutline,
                  class: ['chevron-arrow', { rotated: n.expanded }],
                  style: 'color:#888'
                })
              ])
            : h('span', { style: { width: '14px', display: 'inline-block' } }),
          h('span', { onClick: (e: MouseEvent) => e.stopPropagation() }, [
            h(NCheckbox, {
              checked: !!n.checked,
              indeterminate: n.indeterminate,
              onUpdateChecked: (v: boolean) => setChecked(n, v)
            })
          ]),
          h('span', { class: 'tree-name' }, n.name)
        ]),
        ...(n.expanded && n.children && n.children.length
          ? n.children.map(c => h(TreeRow, { key: c.path, node: c, depth: props.depth + 1 }))
          : [])
      ]
    }
  }
})

async function doExport() {
  pwError.value = ''
  const paths = getCheckedPaths()
  if (paths.length === 0) { pwError.value = '请勾选要导出的内容（单击左侧文件或勾选框）'; return }
  const err = validatePassword(password.value)
  if (err) { pwError.value = err; return }
  exporting.value = true
  try {
    await ExportSessions(paths, password.value, '', [])
    emit('update:show', false)
    emit('done')
  } catch (e: any) { pwError.value = e.message || '导出失败' }
  exporting.value = false
}

onMounted(() => { loadTree() })
watch(() => props.show, (val) => { if (val) { loadTree() } })
</script>

<template>
  <n-modal :show="props.show" title="导出会话" preset="dialog" :show-icon="false" :closable="false" :mask-closable="false" style="width: 720px; max-width: 94vw">
    <div class="export-body">
      <div class="export-left">
        <div class="export-left-title">选择要导出的内容（单击勾选，密钥将从密钥库导出）</div>
        <n-scrollbar style="height: 320px" class="export-scroller">
          <div class="tree">
            <div v-if="tree.length===0" style="padding:20px;text-align:center"><n-empty description="无会话" size="small" /></div>
            <TreeRow v-for="n in tree" :key="n.path" :node="n" :depth="0" />
          </div>
        </n-scrollbar>
      </div>
      <div class="export-right">
        <div class="form-group">
          <label class="form-label">导出密码 <span style="color:#e88070">*</span>（8~64 字符，需包含大写/小写/数字/符号中至少三类）</label>
          <n-input v-model:value="password" type="password" show-password-on="click" placeholder="设置导出口令（必填）" />
        </div>
        <div class="right-hint">
          <span v-if="pwError" style="color:#e45858;font-size:12px">{{ pwError }}</span>
          <span v-else-if="pwStrength && pwStrength !== 'ok'" style="color:#dca54c;font-size:12px">{{ pwStrength }}</span>
          <span v-else-if="pwStrength === 'ok'" style="color:#4ec9b0;font-size:12px">口令强度符合要求</span>
          <span v-else style="color:var(--icon-color,#888);font-size:12px">口令需 8~64 位，包含大写/小写/数字/符号中至少三类</span>
        </div>
        <div class="right-actions">
          <n-button @click="emit('update:show',false)">取消</n-button>
          <n-button type="primary" :loading="exporting" @click="doExport">确定导出</n-button>
        </div>
      </div>
    </div>
  </n-modal>
</template>

<style scoped>
.export-body { display: flex; gap: 16px; min-height: 340px; }
.export-left {
  flex: 1; min-width: 0; border: 1px solid var(--border-color, #3c3c3c);
  border-radius: 6px; background: var(--card-bg, #1e1e1e); overflow: hidden;
}
.export-left-title { font-size: 12px; color: var(--icon-color, #888); padding: 6px 10px; border-bottom: 1px solid var(--border-color, #3c3c3c); }
.tree { padding: 4px 0; }
.tree-row { display:flex;align-items:center;gap:4px;height:26px;cursor:pointer;padding:0 4px;user-select:none }
.tree-row:hover { background:rgba(255,255,255,0.03) }
.chevron { width: 14px; display: inline-flex; flex-shrink: 0; }
.tree-name { font-size:13px;color:var(--text-color,#d4d4d4);overflow:hidden;text-overflow:ellipsis;white-space:nowrap }
.rotated { transform:rotate(90deg) }
/* h() 渲染的 NIcon 不注入 scopeId,需用 :deep 匹配其内部 svg */
:deep(.rotated) { transform: rotate(90deg); }
:deep(.chevron-arrow) { transition: transform 0.15s ease; }
.export-right { width: 260px; flex-shrink: 0; display: flex; flex-direction: column; }
.form-group { margin-top: 0; }
.form-label { display:block;font-size:13px;margin-bottom:6px;color:var(--text-color,#d4d4d4) }
.right-hint { flex: 1; min-height: 18px; margin-top: 10px; }
.right-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 10px; }
</style>