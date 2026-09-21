<script setup lang="ts">
// 插件侧栏面板: 与资源管理器共享侧栏容器, 内容为插件前端组件(同文档直挂)。
// 显隐上报 OnViewVisible/OnViewHidden; ErrorBoundary 兜底防插件崩溃拖垮宿主。
import { ref, watch, onErrorCaptured, computed, type Component } from 'vue'
import { useI18n } from 'vue-i18n'
import { NIcon } from 'naive-ui'
import { CloseOutline } from '@vicons/ionicons5'
import { loadPluginComponent, getPluginCtx, getPluginLoadError, pluginEpoch, isPluginAlive, type PluginToolbarView } from '../composables/usePluginBridge'

const props = defineProps<{
  view: PluginToolbarView
  active: boolean
}>()

const emit = defineEmits<{
  (e: 'close'): void
}>()

const { t } = useI18n()

const comp = ref<Component | null>(null)
const loadFailed = ref(false)
const failDetail = ref('')
// 组件实例内的错误捕获(ErrorBoundary)
const crashed = ref(false)
const crashMsg = ref('')

// 插件组件在面板隐藏时保持挂载(保活), active 仅用于可见性
const renderKey = computed(() => `${props.view.pluginID}:${props.view.viewID}`)

// 已加载到的代次(替代布尔守卫): 插件重载/更新后代次递增, 面板自动换新模块
const loadedEpoch = ref(-1)
async function ensureLoaded() {
  const ep = pluginEpoch(props.view.pluginID)
  if (loadedEpoch.value === ep && comp.value) return
  loadedEpoch.value = ep
  comp.value = await loadPluginComponent(props.view.pluginID, props.view.componentId)
  loadFailed.value = comp.value === null
  failDetail.value = getPluginLoadError(props.view.pluginID, props.view.componentId)
}

watch(() => props.active, () => {
  void ensureLoaded()
}, { immediate: true })

// 插件失效(重载/更新): 复位崩溃态并按新代次重新加载。
// 卸载/禁用不重新加载 —— 文件已不在, 加载只会得到 404 失败闪屏;
// 面板本身随即被注册表驱动的卸载流程拆掉, 这里只把内容清空。
watch(() => pluginEpoch(props.view.pluginID), () => {
  crashed.value = false
  crashMsg.value = ''
  loadFailed.value = false
  failDetail.value = ''
  if (!isPluginAlive(props.view.pluginID)) {
    loadedEpoch.value = -1
    comp.value = null
    return
  }
  void ensureLoaded()
})

// 显隐上报(仅向运行中插件; 首次激活上报 visible)
watch(() => props.active, async v => {
  const { PluginSetViewVisible } = await import('../../bindings/changeme/internal/services/pluginservice.js')
  PluginSetViewVisible(props.view.pluginID, props.view.viewID, v).catch(() => {})
})

onErrorCaptured((err) => {
  crashed.value = true
  crashMsg.value = String(err)
  return false
})

const ctx = computed(() => getPluginCtx(props.view.pluginID))
</script>

<template>
  <div class="plugin-panel">
    <div class="plugin-panel-header">
      <img v-if="view.icon" class="plugin-panel-icon" :src="view.icon" alt="" />
      <span class="plugin-panel-title">{{ view.title }}</span>
      <button class="plugin-panel-close" :title="t('resourceManager.hidePanel')" @click="emit('close')">
        <n-icon :size="16" :component="CloseOutline" />
      </button>
    </div>
    <div class="plugin-panel-body">
      <template v-if="crashed">
        <div class="plugin-panel-state">
          <div class="plugin-state-title">{{ t('plugins.crashed') }}</div>
          <div class="plugin-state-detail">{{ crashMsg }}</div>
        </div>
      </template>
      <template v-else-if="loadFailed">
        <div class="plugin-panel-state">
          <div class="plugin-state-title">{{ t('plugins.loadFailed') }}</div>
          <div v-if="failDetail" class="plugin-state-detail">{{ failDetail }}</div>
          <div class="plugin-state-detail">{{ t('plugins.loadFailedHint') }}</div>
        </div>
      </template>
      <template v-else-if="comp">
        <component :is="comp" :key="renderKey" :ctx="ctx" :view="view" :active="active" />
      </template>
      <template v-else>
        <div class="plugin-panel-state">
          <div class="plugin-state-title">{{ t('plugins.loading') }}</div>
        </div>
      </template>
    </div>
  </div>
</template>

<style scoped>
.plugin-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  overflow: hidden;
}

.plugin-panel-header {
  height: 38px;
  min-height: 38px;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 8px 0 12px;
  font-size: 13px;
  color: var(--text-color);
}

.plugin-panel-icon {
  width: 16px;
  height: 16px;
  flex-shrink: 0;
}

.plugin-panel-title {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-weight: 500;
}

.plugin-panel-close {
  width: 24px;
  height: 24px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: none;
  border-radius: 4px;
  background: transparent;
  color: var(--icon-color);
  cursor: pointer;
  transition: background 0.15s;
}
.plugin-panel-close:hover {
  background: rgba(255, 255, 255, 0.08);
  color: var(--text-color);
}

.plugin-panel-body {
  flex: 1;
  min-height: 0;
  overflow: auto;
}

.plugin-panel-state {
  padding: 32px 16px;
  text-align: center;
  color: var(--icon-color);
  font-size: 12px;
}
.plugin-state-title {
  font-size: 13px;
  color: var(--text-color);
  margin-bottom: 6px;
}
.plugin-state-detail {
  word-break: break-all;
  opacity: 0.7;
}
</style>
