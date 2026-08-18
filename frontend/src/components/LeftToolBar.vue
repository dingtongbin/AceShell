<script setup lang="ts">
import { NIcon, NTooltip } from 'naive-ui'
import {
  FolderOutline,
  FolderOpenOutline,
  HelpCircleOutline,
  LogoGithub,
  SettingsOutline,
} from '@vicons/ionicons5'

defineProps<{
  showSession: boolean
  showHelp: boolean
  showGithub: boolean
}>()

const emit = defineEmits<{
  (e: 'toggle-session'): void
  (e: 'open-help'): void
  (e: 'open-github'): void
  (e: 'open-settings'): void
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
        资源管理器
      </n-tooltip>
    </div>
    <div class="ltb-bottom">
      <n-tooltip v-if="showGithub" placement="right" trigger="hover" :delay="300">
        <template #trigger>
          <div class="ltb-item" @click="emit('open-github')">
            <n-icon :size="24" :component="LogoGithub" />
          </div>
        </template>
        GitHub
      </n-tooltip>
      <n-tooltip v-if="showHelp" placement="right" trigger="hover" :delay="300">
        <template #trigger>
          <div class="ltb-item" @click="emit('open-help')">
            <n-icon :size="24" :component="HelpCircleOutline" />
          </div>
        </template>
        帮助
      </n-tooltip>
      <n-tooltip placement="right" trigger="hover" :delay="300">
        <template #trigger>
          <div class="ltb-item" @click="emit('open-settings')">
            <n-icon :size="24" :component="SettingsOutline" />
          </div>
        </template>
        设置
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
  background: var(--toolbar-bg, #2d2d2d);
  user-select: none;
}

.ltb-top,
.ltb-bottom {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
}

.ltb-item {
  width: 36px;
  height: 36px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 6px;
  color: var(--text-color, #d4d4d4);
  opacity: 0.75;
  cursor: pointer;
  transition: background 0.15s, opacity 0.15s, color 0.2s;
}

.ltb-item:hover {
  background: rgba(255, 255, 255, 0.06);
  opacity: 1;
}

.ltb-item.active {
  color: #4ec9b0;
  opacity: 1;
}
</style>
