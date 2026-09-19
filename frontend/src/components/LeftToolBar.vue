<script setup lang="ts">
import { NIcon, NTooltip } from 'naive-ui'
import {
  FolderOutline,
  FolderOpenOutline,
  HelpCircleOutline,
  SettingsOutline,
} from '@vicons/ionicons5'
import { useI18n } from 'vue-i18n'
import type { PluginToolbarView } from '../composables/usePluginBridge'

const { t } = useI18n()

defineProps<{
  showSession: boolean
  showHelp: boolean
  /** 已启用插件注册的侧栏视图(运行中插件) */
  pluginViews: PluginToolbarView[]
  /** 当前激活的插件视图标识 pluginID:viewID(null=未激活) */
  activePluginView: string | null
}>()

const emit = defineEmits<{
  (e: 'toggle-session'): void
  (e: 'open-help'): void
  (e: 'open-settings'): void
  (e: 'toggle-plugin-view', view: PluginToolbarView): void
}>()
</script>

<template>
  <div class="left-tool-bar">
    <div class="ltb-top">
      <n-tooltip placement="right" trigger="hover" :delay="300">
        <template #trigger>
          <div class="ltb-item" :class="{ active: showSession }" @click="emit('toggle-session')">
            <n-icon :size="24" :component="showSession ? FolderOpenOutline : FolderOutline" />
          </div>
        </template>
        {{ t('common.explorer') }}
      </n-tooltip>
      <!-- 插件注册的侧栏视图图标(像 VSCode 活动栏) -->
      <n-tooltip v-for="view in pluginViews" :key="view.pluginID + ':' + view.viewID" placement="right" trigger="hover" :delay="300">
        <template #trigger>
          <div class="ltb-item" :class="{ active: activePluginView === view.pluginID + ':' + view.viewID }"
            @click="emit('toggle-plugin-view', view)">
            <img class="ltb-plugin-icon" :src="view.icon" alt="" />
          </div>
        </template>
        {{ view.title }}
      </n-tooltip>
    </div>
    <div class="ltb-bottom">
      <n-tooltip v-if="showHelp" placement="right" trigger="hover" :delay="300">
        <template #trigger>
          <div class="ltb-item" @click="emit('open-help')">
            <n-icon :size="24" :component="HelpCircleOutline" />
          </div>
        </template>
        {{ t('common.help') }}
      </n-tooltip>
      <n-tooltip placement="right" trigger="hover" :delay="300">
        <template #trigger>
          <div class="ltb-item" @click="emit('open-settings')">
            <n-icon :size="24" :component="SettingsOutline" />
          </div>
        </template>
        {{ t('common.settings') }}
      </n-tooltip>
    </div>
  </div>
</template>

<style scoped>
.left-tool-bar {
  width: 44px;
  height: 100%;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  align-items: center;
  padding: 8px 0;
  background: var(--toolbar-bg);
  user-select: none;
}

.ltb-top,
.ltb-bottom {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  min-height: 0;
}

/* 插件图标多时纵向滚动 */
.ltb-top {
  flex: 1;
  overflow-y: auto;
  overflow-x: hidden;
  scrollbar-width: none;
}
.ltb-top::-webkit-scrollbar {
  display: none;
}

.ltb-item {
  width: 36px;
  height: 36px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 6px;
  color: var(--text-color);
  opacity: 0.75;
  cursor: pointer;
  transition: background 0.15s, opacity 0.15s, color 0.2s;
}

.ltb-item:hover {
  background: rgba(255, 255, 255, 0.06);
  opacity: 1;
}

.ltb-item.active {
  color: var(--primary-color);
  opacity: 1;
}

.ltb-plugin-icon {
  width: 22px;
  height: 22px;
  object-fit: contain;
  pointer-events: none;
}
</style>
