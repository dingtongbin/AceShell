<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { NIcon, NButton } from 'naive-ui'
import { ChevronForwardOutline, TerminalOutline, DocumentTextOutline, PulseOutline, CloseOutline } from '@vicons/ionicons5'
import SessionManager from './SessionManager.vue'
import ScriptManager from './ScriptManager.vue'
import AutoLogManager from './AutoLogManager.vue'
import SerialManager from './SerialManager.vue'
import { GetConfig, SetSectionsState } from '../../bindings/changeme/internal/services/configservice.js'

const props = defineProps<{
  activeSessionPath: string | null
  width: number
  showSerial: boolean
}>()
const emit = defineEmits<{
  (e: 'select', path: string): void
  (e: 'new-folder', parentPath: string): void
  (e: 'new-session', folderPath: string): void
  (e: 'edit-session', path: string): void
  (e: 'import-sessions'): void
  (e: 'export-sessions'): void
  (e: 'refresh'): void
  (e: 'connect', portName: string, baudRate: number, dataBits: number, stopBits: string, parity: string): void
  (e: 'open-file', path: string): void
  (e: 'close'): void
}>()

const sessionManagerRef = ref<InstanceType<typeof SessionManager> | null>(null)

function renameSelected() { sessionManagerRef.value?.renameSelected() }
function deleteSelected() { sessionManagerRef.value?.deleteSelected() }

defineExpose({ renameSelected, deleteSelected })

const activeTab = ref<'session' | 'script' | 'log'>('session')

interface Section { key: string; label: string; icon: any; expanded: boolean; size: number }

const sections = ref<Section[]>([
  { key: 'serial', label: '串口', icon: PulseOutline, expanded: false, size: 0 },
])

async function loadState() {
  try {
    const cfg = JSON.parse(await GetConfig())
    if (cfg.sections) {
      for (const s of sections.value) {
        const saved = cfg.sections[s.key]
        if (saved) { s.expanded = saved.expanded; s.size = saved.size || 0 }
      }
    }
  } catch {}
}

function toggleSection(section: Section) {
  section.expanded = !section.expanded
  if (!section.expanded) section.size = 0
  const state: any = {}
  for (const s of sections.value) state[s.key] = { expanded: s.expanded, size: s.size }
  SetSectionsState(JSON.stringify(state)).catch(() => {})
}

onMounted(loadState)
</script>

<template>
  <div class="resource-manager">
    <div class="resource-header">
      <span class="resource-title">资源管理器</span>
      <div class="resource-close">
        <n-button text size="tiny" title="隐藏面板" @click="emit('close')">
          <n-icon :size="14" :component="CloseOutline" />
        </n-button>
      </div>
    </div>
    <div class="resource-tabs">
      <div class="rm-tab" :class="{ active: activeTab === 'session' }" @click="activeTab = 'session'">
        <n-icon :size="12" :component="TerminalOutline" />
        <span>会话</span>
      </div>
      <div class="rm-tab" :class="{ active: activeTab === 'script' }" @click="activeTab = 'script'">
        <n-icon :size="12" :component="DocumentTextOutline" />
        <span>脚本</span>
      </div>
      <div class="rm-tab" :class="{ active: activeTab === 'log' }" @click="activeTab = 'log'">
        <n-icon :size="12" :component="DocumentTextOutline" />
        <span>日志</span>
      </div>
    </div>
    <div class="resource-body">
      <div v-show="activeTab === 'session'" class="rm-pane">
        <SessionManager ref="sessionManagerRef" :active-session-path="props.activeSessionPath" :width="props.width"
          @select="(p: string) => emit('select', p)" @new-folder="(p: string) => emit('new-folder', p)"
          @new-session="(p: string) => emit('new-session', p)" @edit-session="(p: string) => emit('edit-session', p)"
          @import-sessions="emit('import-sessions')" @export-sessions="emit('export-sessions')"
          @refresh="() => emit('refresh')" />
        <div v-if="props.showSerial" class="section-wrapper">
          <div class="section-header" @click="toggleSection(sections[0])">
            <n-icon :size="12" :component="ChevronForwardOutline" class="section-arrow" :class="{ rotated: sections[0].expanded }" />
            <n-icon :size="14" :component="sections[0].icon" class="section-icon" />
            <span class="section-label">串口</span>
          </div>
          <div v-show="sections[0].expanded" class="section-content">
            <SerialManager @connect="(pn, br, db, sb, p) => emit('connect', pn, br, db, sb, p)" />
          </div>
        </div>
      </div>
      <div v-show="activeTab === 'script'" class="rm-pane">
        <ScriptManager @open-file="(p: string) => emit('open-file', p)" />
      </div>
      <div v-show="activeTab === 'log'" class="rm-pane">
        <AutoLogManager />
      </div>
    </div>
  </div>
</template>

<style scoped>
.resource-manager { height: 100%; display: flex; flex-direction: column; background: var(--sidebar-bg, #181818); overflow: hidden; }
.resource-header { height: 35px; display: flex; align-items: center; padding: 0 8px 0 12px; border-bottom: 1px solid var(--sidebar-shadow, #3c3c3c); flex-shrink: 0; }
.resource-title { font-size: 11px; font-weight: 600; color: var(--text-color, #d4d4d4); text-transform: uppercase; letter-spacing: 0.8px; }
.resource-close { margin-left: auto; display: flex; align-items: center; }
.resource-tabs { display: flex; height: 30px; min-height: 30px; border-bottom: 1px solid var(--sidebar-shadow, #3c3c3c); flex-shrink: 0; }
.rm-tab { flex: 1; display: flex; align-items: center; justify-content: center; gap: 4px; font-size: 12px; color: var(--icon-color, #6e6e6e); cursor: pointer; transition: background 0.15s, color 0.15s; user-select: none; }
.rm-tab:hover { color: var(--icon-hover, #c5c5c5); background: var(--hover-bg); }
.rm-tab.active { color: var(--active-color, #ffffff); background: var(--tab-active-bg, #0d0d0d); border-bottom: 2px solid #0078d4; }
.resource-body { flex: 1; display: flex; flex-direction: column; overflow: hidden; }
.rm-pane { flex: 1; min-height: 0; display: flex; flex-direction: column; overflow: hidden; }
.section-wrapper { flex-shrink: 0; display: flex; flex-direction: column; border-top: 1px solid var(--sidebar-shadow, #3c3c3c); }
.section-content { max-height: 200px; overflow: hidden; }
</style>
