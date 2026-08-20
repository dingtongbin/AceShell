<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { NIcon, useMessage } from 'naive-ui'
import {
  AddOutline,
  FolderOutline,
  ArrowUpOutline,
  DownloadOutline,
  LogOutOutline,
  CreateOutline,
  TrashOutline,
  CodeOutline,
  MoonOutline,
  SunnyOutline,
  FolderOpenOutline,
  InformationCircleOutline,
  BookOutline,
  ChevronForwardOutline,
  CheckmarkOutline,
} from '@vicons/ionicons5'
import { useTheme } from '../stores/theme'
import { SetTheme } from '../../bindings/changeme/internal/services/configservice.js'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const props = defineProps<{
  showToolbar: boolean
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'new-session'): void
  (e: 'new-folder'): void
  (e: 'import-sessions'): void
  (e: 'export-sessions'): void
  (e: 'exit'): void
  (e: 'edit-active-session'): void
  (e: 'rename-selected'): void
  (e: 'delete-selected'): void
  (e: 'exec-script'): void
  (e: 'sftp'): void
  (e: 'toggle-theme'): void
  (e: 'toggle-toolbar'): void
  (e: 'about'): void
}>()

const message = useMessage()
const { toggleTheme, isDark } = useTheme()
const activeTop = ref('file')

interface MenuItem {
  key: string
  label?: string
  icon?: any
  checked?: boolean
  divider?: boolean
}

interface MenuEntry {
  key: string
  label: string
  items: MenuItem[]
}

const menus = computed<MenuEntry[]>(() => [
  {
    key: 'file',
    label: t('mainMenu.file'),
    items: [
      { key: 'new-session', label: t('mainMenu.newSession'), icon: AddOutline },
      { key: 'new-folder', label: t('mainMenu.newFolder'), icon: FolderOutline },
      { key: 'd1', divider: true },
      { key: 'import-sessions', label: t('mainMenu.importSessions'), icon: ArrowUpOutline },
      { key: 'export-sessions', label: t('mainMenu.exportSessions'), icon: DownloadOutline },
      { key: 'd2', divider: true },
      { key: 'exit', label: t('mainMenu.exit'), icon: LogOutOutline },
    ],
  },
  {
    key: 'edit',
    label: t('mainMenu.edit'),
    items: [
      { key: 'edit-active-session', label: t('mainMenu.editActiveSession'), icon: CreateOutline },
      { key: 'd1', divider: true },
      { key: 'rename-selected', label: t('mainMenu.renameSelected'), icon: CreateOutline },
      { key: 'delete-selected', label: t('mainMenu.deleteSelected'), icon: TrashOutline },
    ],
  },
  {
    key: 'tool',
    label: t('mainMenu.tool'),
    items: [
      { key: 'exec-script', label: t('mainMenu.execScript'), icon: CodeOutline },
      { key: 'sftp', label: 'SFTP', icon: FolderOpenOutline },
      { key: 'toggle-theme', label: t('mainMenu.toggleTheme'), icon: isDark.value ? SunnyOutline : MoonOutline },
      { key: 'd1', divider: true },
      { key: 'toggle-toolbar', label: t('mainMenu.toolbar'), checked: props.showToolbar },
    ],
  },
  {
    key: 'help',
    label: t('mainMenu.help'),
    items: [
      { key: 'about', label: t('mainMenu.about'), icon: InformationCircleOutline },
      { key: 'view-docs', label: t('mainMenu.viewDocs'), icon: BookOutline },
    ],
  },
])

function handleSelect(key: string) {
  switch (key) {
    case 'new-session': emit('new-session'); break
    case 'new-folder': emit('new-folder'); break
    case 'import-sessions': emit('import-sessions'); break
    case 'export-sessions': emit('export-sessions'); break
    case 'exit': emit('exit'); break
    case 'edit-active-session': emit('edit-active-session'); break
    case 'rename-selected': emit('rename-selected'); break
    case 'delete-selected': emit('delete-selected'); break
    case 'exec-script': emit('exec-script'); break
    case 'sftp': emit('sftp'); break
    case 'toggle-theme':
      toggleTheme()
      SetTheme(isDark.value ? 'dark' : 'light').catch(() => message.error(t('mainMenu.saveThemeFailed')))
      emit('toggle-theme')
      break
    case 'toggle-toolbar': emit('toggle-toolbar'); break
    case 'about': emit('about'); break
    case 'view-docs': message.info(t('mainMenu.viewDocsHint')); break
    default: return
  }
  emit('close')
}

const menuRoot = ref<HTMLElement | null>(null)

function onDocClick(e: MouseEvent) {
  const el = menuRoot.value
  if (el && !el.contains(e.target as Node)) emit('close')
}

function onEsc(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close')
}

onMounted(() => {
  document.addEventListener('click', onDocClick)
  document.addEventListener('keydown', onEsc)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', onDocClick)
  document.removeEventListener('keydown', onEsc)
})
</script>

<template>
  <div ref="menuRoot" class="main-menu">
    <div
      v-for="m in menus"
      :key="m.key"
      class="main-menu-top"
      :class="{ active: activeTop === m.key }"
      @mouseenter="activeTop = m.key"
      @click="activeTop = m.key"
    >
      <span class="main-menu-label">{{ m.label }}</span>
      <n-icon :size="12" :component="ChevronForwardOutline" class="main-menu-arrow" />
    </div>
    <div v-for="m in menus" :key="m.key" class="main-menu-panel" v-show="activeTop === m.key">
      <div
        v-for="it in m.items"
        :key="it.key"
        class="main-menu-item"
        :class="{ divider: it.divider }"
        @click="it.divider ? null : handleSelect(it.key)"
      >
        <template v-if="!it.divider">
          <span class="mmi-icon"><n-icon v-if="it.icon" :size="15" :component="it.icon" /></span>
          <span class="mmi-label">{{ it.label }}</span>
          <span class="mmi-check"><n-icon v-if="it.checked" :size="14" :component="CheckmarkOutline" /></span>
        </template>
      </div>
    </div>
  </div>
</template>

<style scoped>
.main-menu {
  position: relative;
  width: 140px;
  background: var(--panel-bg, #252526);
  border-radius: 6px;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.5);
  padding: 4px;
  z-index: 1000;
  user-select: none;
}

.main-menu-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 6px 10px;
  font-size: 13px;
  color: var(--text-color, #d4d4d4);
  border-radius: 4px;
  cursor: pointer;
  transition: background 0.15s;
}

.main-menu-top:hover,
.main-menu-top.active {
  background: rgba(255, 255, 255, 0.08);
}

.main-menu-arrow {
  opacity: 0.55;
}

.main-menu-panel {
  position: absolute;
  left: 100%;
  top: -4px;
  min-width: 210px;
  background: var(--panel-bg, #252526);
  border-radius: 6px;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.5);
  padding: 4px;
  z-index: 1001;
}

.main-menu-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 10px;
  font-size: 13px;
  color: var(--text-color, #d4d4d4);
  border-radius: 4px;
  cursor: pointer;
  transition: background 0.15s;
  white-space: nowrap;
}

.main-menu-item:hover {
  background: rgba(255, 255, 255, 0.08);
}

.main-menu-item.divider {
  height: 1px;
  padding: 0;
  margin: 4px 6px;
  background: var(--sidebar-shadow, #3c3c3c);
  cursor: default;
}

.mmi-icon {
  width: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.mmi-label {
  flex: 1;
}

.mmi-check {
  width: 14px;
  display: flex;
  align-items: center;
  justify-content: center;
}
</style>
