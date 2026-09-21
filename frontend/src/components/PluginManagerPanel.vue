<script setup lang="ts">
// 插件管理面板 —— 内部布局对齐 VSCode 扩展视图:
//   头部(标题 + 安装/目录图标钮) → 搜索框 → 可折叠安装行
//   → 分组列表(sticky 分组头带折叠箭头与计数, 条目两行: 名称+版本 / 状态, hover 出操作)
//   → 底部统计行。
// 数据直读注册表 ref(随 plugin-registry-changed 与失效协议自动刷新)。
import { ref, computed } from 'vue'
import { NIcon, NTag, NSwitch, NInput, NButton, NPopconfirm, NTooltip, useMessage } from 'naive-ui'
import {
  ReloadOutline,
  TrashOutline,
  RefreshCircleOutline,
  FolderOpenOutline,
  SearchOutline,
  CaretDownOutline,
  CaretForwardOutline,
  CloudDownloadOutline,
  CloudUploadOutline,
} from '@vicons/ionicons5'
import { useI18n } from 'vue-i18n'
import { usePlugins, type PluginSummary } from '../composables/usePluginBridge'
import { PluginList, PluginSetEnabled, PluginReload, PluginUninstall, PluginRestoreBundled, PluginInstallFromGitHub, PluginInstallZip, PluginOpenDir } from '../../bindings/changeme/internal/services/pluginservice.js'
import { OpenFileDialog } from '../../bindings/changeme/internal/services/windowservice.js'
import { debounceClick } from '../utils/debounce'

const { t } = useI18n()
const message = useMessage()
const { plugins } = usePlugins()

const emit = defineEmits<{
  (e: 'open-detail', pluginID: string): void
}>()

const keyword = ref('')
const ghInput = ref('')
const installing = ref(false)
const installOpen = ref(true)
// 操作防抖: id → 操作名, 期间控件 loading 且忽略重复点击
const busy = ref<Record<string, string>>({})
// 选中条目(点击详情后高亮, VSCode 焦点风格)
const selectedId = ref('')
// 分组折叠态
const collapsed = ref<Record<string, boolean>>({})
// 按钮统一 300ms 防抖(键 = 插件 id)
const dToggle = debounceClick((id: string, v: boolean) => toggleEnabled(id, v))
const dReload = debounceClick((id: string) => reload(id))
const dUninstall = debounceClick((id: string) => uninstall(id))
const dRestore = debounceClick((id: string) => restore(id))
const dOpenDetail = debounceClick((id: string) => openDetailById(id))

const filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  if (!kw) return plugins.value
  return plugins.value.filter(p =>
    p.id.toLowerCase().includes(kw) || p.displayName.toLowerCase().includes(kw))
})

const installedList = computed(() => filtered.value.filter(p => p.status !== 'uninstalled'))
const uninstalledList = computed(() => filtered.value.filter(p => p.status === 'uninstalled'))
const runningCount = computed(() => plugins.value.filter(p => p.status === 'running').length)

function toggleSection(key: string) {
  collapsed.value[key] = !collapsed.value[key]
}

function openDetail(p: PluginSummary) {
  selectedId.value = p.id
  emit('open-detail', p.id)
}

function openDetailById(id: string) {
  selectedId.value = id
  emit('open-detail', id)
}

function statusText(p: PluginSummary): string {
  const key = { running: 'plugins.statusRunning', starting: 'plugins.statusStarting', stopped: 'plugins.statusStopped', error: 'plugins.statusError', disabled: 'plugins.statusDisabled', uninstalled: 'plugins.statusUninstalled' }[p.status] || 'plugins.statusStopped'
  const text = t(key)
  return p.error ? `${text} · ${p.error}` : text
}

function toggleEnabled(id: string, v: boolean) {
  if (busy.value[id]) return
  busy.value[id] = 'toggle'
  PluginSetEnabled(id, v)
    .catch((e: any) => message.error(String(e?.message || e)))
    .finally(() => setTimeout(() => { delete busy.value[id] }, 1000))
}

function reload(id: string) {
  if (busy.value[id]) return
  busy.value[id] = 'reload'
  PluginReload(id)
    .then(() => message.success(t('plugins.reloaded', { id })))
    .catch((e: any) => message.error(String(e?.message || e)))
    .finally(() => setTimeout(() => { delete busy.value[id] }, 1000))
}

function uninstall(id: string) {
  if (busy.value[id]) return
  busy.value[id] = 'uninstall'
  PluginUninstall(id)
    .then(raw => {
      const res = JSON.parse(raw)
      if (res?.error) { message.error(String(res.error)); return }
      message.success(t('plugins.uninstalledOk', { id }))
    })
    .catch((e: any) => message.error(String(e?.message || e)))
    .finally(() => setTimeout(() => { delete busy.value[id] }, 1000))
}

function restore(id: string) {
  if (busy.value[id]) return
  busy.value[id] = 'restore'
  PluginRestoreBundled(id)
    .then(raw => {
      const res = JSON.parse(raw)
      if (res?.error) { message.error(String(res.error)); return }
      message.success(t('plugins.restoredOk', { id }))
    })
    .catch((e: any) => message.error(String(e?.message || e)))
    .finally(() => setTimeout(() => { delete busy.value[id] }, 1000))
}

async function install() {
  const input = ghInput.value.trim()
  if (!input || installing.value) return
  installing.value = true
  try {
    const res = JSON.parse(await PluginInstallFromGitHub(input))
    if (res?.error) {
      message.error(String(res.error))
    } else {
      message.success(t('plugins.installOk', { id: res.id, version: res.version }))
      ghInput.value = ''
    }
  } catch (e: any) {
    message.error(String(e?.message || e))
  } finally {
    installing.value = false
  }
}

// 本地 zip 安装: 文件选择对话框 → 安装管线
const installingZip = ref(false)
const dPickZip = debounceClick(() => { void pickZip() })
async function pickZip() {
  if (installingZip.value) return
  const path = await OpenFileDialog(t('plugins.zipPickTitle'), 'Zip', '*.zip')
  if (!path) return
  installingZip.value = true
  try {
    const res = JSON.parse(await PluginInstallZip(path))
    if (res?.error) {
      message.error(String(res.error))
    } else {
      message.success(t('plugins.installOk', { id: res.id, version: res.version }))
    }
  } catch (e: any) {
    message.error(String(e?.message || e))
  } finally {
    installingZip.value = false
  }
}

function openDir() {
  PluginOpenDir().catch(() => {})
}
</script>

<template>
  <div class="pmgr">
    <div class="pmgr-header">
      <span class="pmgr-title">{{ t('plugins.managerTitle') }}</span>
      <div class="pmgr-header-actions">
        <n-tooltip placement="bottom" trigger="hover" :delay="300">
          <template #trigger>
            <button class="pmgr-icon-btn" :class="{ active: installOpen }" @click="installOpen = !installOpen">
              <n-icon :size="15" :component="CloudDownloadOutline" />
            </button>
          </template>
          {{ t('plugins.installTitle') }}
        </n-tooltip>
        <n-tooltip placement="bottom" trigger="hover" :delay="300">
          <template #trigger>
            <button class="pmgr-icon-btn" :disabled="installingZip" @click="dPickZip()">
              <n-icon :size="15" :component="CloudUploadOutline" />
            </button>
          </template>
          {{ t('plugins.zipPickTitle') }}
        </n-tooltip>
        <n-tooltip placement="bottom" trigger="hover" :delay="300">
          <template #trigger>
            <button class="pmgr-icon-btn" @click="openDir">
              <n-icon :size="15" :component="FolderOpenOutline" />
            </button>
          </template>
          {{ t('plugins.openDir') }}
        </n-tooltip>
      </div>
    </div>

    <div class="pmgr-search">
      <n-input v-model:value="keyword" size="small" :placeholder="t('plugins.searchPlaceholder')" clearable>
        <template #prefix>
          <n-icon :size="13" :component="SearchOutline" />
        </template>
      </n-input>
    </div>

    <div v-if="installOpen" class="pmgr-install">
      <n-input v-model:value="ghInput" size="small" :placeholder="t('plugins.installPlaceholder')" :disabled="installing" @keyup.enter="install" />
      <n-button size="small" type="primary" ghost :loading="installing" @click="install">{{ t('plugins.installBtn') }}</n-button>
    </div>

    <div class="pmgr-list">
      <template v-if="installedList.length === 0 && uninstalledList.length === 0">
        <div class="pmgr-empty">{{ t('plugins.empty') }}</div>
      </template>
      <template v-else>
        <template v-if="installedList.length > 0">
          <button class="pmgr-section" @click="toggleSection('installed')">
            <n-icon :size="12" :component="collapsed['installed'] ? CaretForwardOutline : CaretDownOutline" />
            <span>{{ t('plugins.sectionInstalled') }}</span>
            <span class="pmgr-section-count">{{ installedList.length }}</span>
          </button>
          <template v-if="!collapsed['installed']">
            <div v-for="p in installedList" :key="p.id"
              class="pmgr-item" :class="{ selected: selectedId === p.id }"
              @click="dOpenDetail(p.id)">
              <img v-if="p.icon" class="pmgr-icon" :src="p.icon" alt="" />
              <div v-else class="pmgr-icon pmgr-icon-ph">{{ p.displayName.slice(0, 1) }}</div>
              <div class="pmgr-main">
                <div class="pmgr-name">
                  <span class="pmgr-name-text">{{ p.displayName }}</span>
                  <span class="pmgr-ver">v{{ p.version || '-' }}</span>
                  <n-tag v-if="p.bundled" size="tiny" :bordered="false" type="info">{{ t('plugins.bundled') }}</n-tag>
                </div>
                <div class="pmgr-status" :class="{ 'pmgr-status-error': p.status === 'error' }">{{ statusText(p) }}</div>
              </div>
              <div class="pmgr-actions" @click.stop>
                <n-switch size="small" :loading="busy[p.id] === 'toggle'" :value="p.status !== 'disabled'" @update:value="(v: boolean) => dToggle(p.id, v)" />
                <n-tooltip placement="bottom" trigger="hover" :delay="300">
                  <template #trigger>
                    <button class="pmgr-icon-btn" :disabled="busy[p.id] === 'reload' || p.status === 'disabled'" @click="dReload(p.id)">
                      <n-icon :size="14" :component="ReloadOutline" />
                    </button>
                  </template>
                  {{ t('plugins.reload') }}
                </n-tooltip>
                <n-popconfirm placement="bottom" :width="260" :show-icon="false" @positive-click="dUninstall(p.id)">
                  <template #trigger>
                    <button class="pmgr-icon-btn pmgr-danger" :disabled="busy[p.id] === 'uninstall'">
                      <n-icon :size="14" :component="TrashOutline" />
                    </button>
                  </template>
                  {{ t('plugins.uninstallConfirm', { id: p.id }) }}
                </n-popconfirm>
              </div>
            </div>
          </template>
        </template>

        <template v-if="uninstalledList.length > 0">
          <button class="pmgr-section" @click="toggleSection('uninstalled')">
            <n-icon :size="12" :component="collapsed['uninstalled'] ? CaretForwardOutline : CaretDownOutline" />
            <span>{{ t('plugins.sectionUninstalled') }}</span>
            <span class="pmgr-section-count">{{ uninstalledList.length }}</span>
          </button>
          <template v-if="!collapsed['uninstalled']">
            <div v-for="p in uninstalledList" :key="p.id"
              class="pmgr-item pmgr-item-uninstalled" :class="{ selected: selectedId === p.id }"
              @click="dOpenDetail(p.id)">
              <img v-if="p.icon" class="pmgr-icon" :src="p.icon" alt="" />
              <div v-else class="pmgr-icon pmgr-icon-ph">{{ p.displayName.slice(0, 1) }}</div>
              <div class="pmgr-main">
                <div class="pmgr-name">
                  <span class="pmgr-name-text">{{ p.displayName }}</span>
                  <span class="pmgr-ver">v{{ p.version || '-' }}</span>
                </div>
                <div class="pmgr-status">{{ statusText(p) }}</div>
              </div>
              <div class="pmgr-actions pmgr-actions-visible" @click.stop>
                <n-button size="tiny" type="primary" quaternary :loading="busy[p.id] === 'restore'" @click="dRestore(p.id)">
                  <template #icon><n-icon :component="RefreshCircleOutline" /></template>
                  {{ t('plugins.restore') }}
                </n-button>
              </div>
            </div>
          </template>
        </template>
      </template>
    </div>

    <div class="pmgr-footer">
      {{ t('plugins.footerSummary', { n: plugins.length, m: runningCount }) }}
    </div>
  </div>
</template>

<style scoped>
.pmgr {
  height: 100%;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  color: var(--text-color);
}

.pmgr-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 12px 8px;
  flex-shrink: 0;
}

.pmgr-title {
  font-size: 13px;
  font-weight: 500;
}

.pmgr-header-actions {
  display: flex;
  align-items: center;
  gap: 4px;
}

.pmgr-icon-btn {
  width: 24px;
  height: 24px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: none;
  border-radius: 5px;
  background: transparent;
  color: var(--icon-color);
  cursor: pointer;
  transition: background 0.12s, color 0.12s;
}
.pmgr-icon-btn:hover:not(:disabled) {
  background: rgba(255, 255, 255, 0.08);
  color: var(--text-color);
}
.pmgr-icon-btn.active {
  color: var(--primary-color);
}
.pmgr-icon-btn:disabled {
  opacity: 0.4;
  cursor: default;
}
.pmgr-danger:hover:not(:disabled) {
  color: var(--danger-color, #e24b4b);
}

.pmgr-search {
  padding: 0 10px;
  flex-shrink: 0;
}

.pmgr-install {
  margin: 8px 10px 0;
  padding: 8px;
  border: 1px solid var(--border-color);
  border-radius: 6px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  flex-shrink: 0;
}

.pmgr-list {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding-bottom: 6px;
}

.pmgr-section {
  position: sticky;
  top: 0;
  z-index: 1;
  width: 100%;
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 7px 12px 5px;
  border: none;
  background: var(--body-bg);
  color: var(--icon-color);
  font-size: 11px;
  font-weight: 500;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  text-align: left;
  cursor: pointer;
  user-select: none;
}
.pmgr-section:hover {
  color: var(--text-color);
}
.pmgr-section-count {
  color: var(--primary-color);
}

.pmgr-item {
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 0 6px;
  padding: 7px 8px;
  border-radius: 6px;
  border: 1px solid transparent;
  cursor: pointer;
  transition: background 0.12s;
}
.pmgr-item:hover {
  background: rgba(255, 255, 255, 0.05);
}
.pmgr-item:hover .pmgr-actions {
  opacity: 1;
}
.pmgr-item.selected {
  border-color: var(--primary-color);
}
.pmgr-item-uninstalled {
  opacity: 0.7;
}

.pmgr-icon {
  width: 40px;
  height: 40px;
  object-fit: contain;
  flex-shrink: 0;
}
.pmgr-icon-ph {
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 8px;
  background: var(--primary-color);
  color: #fff;
  font-size: 17px;
}

.pmgr-main {
  flex: 1;
  min-width: 0;
}

.pmgr-name {
  display: flex;
  align-items: center;
  gap: 5px;
  min-width: 0;
}
.pmgr-name-text {
  font-size: 13px;
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.pmgr-ver {
  font-size: 11px;
  color: var(--icon-color);
  flex-shrink: 0;
}

.pmgr-status {
  font-size: 11px;
  color: var(--icon-color);
  margin-top: 3px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.pmgr-status-error {
  color: var(--danger-color, #e24b4b);
}

.pmgr-actions {
  display: flex;
  align-items: center;
  gap: 6px;
  opacity: 0;
  transition: opacity 0.12s;
  flex-shrink: 0;
}
.pmgr-actions-visible {
  opacity: 1;
}

.pmgr-empty {
  padding: 32px 12px;
  text-align: center;
  font-size: 12px;
  color: var(--icon-color);
}

.pmgr-footer {
  flex-shrink: 0;
  padding: 6px 12px;
  border-top: 1px solid var(--border-color);
  font-size: 11px;
  color: var(--icon-color);
}
</style>
