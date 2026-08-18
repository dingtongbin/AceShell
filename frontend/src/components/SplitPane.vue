<script setup lang="ts">
import { computed } from 'vue'
import TabPane from './TabPane.vue'
import type { Pane, PaneNode, SplitNode, PassProps } from './tabTypes'

const props = defineProps<{
  node: SplitNode
  panes: Pane[]
  pass: PassProps
}>()

function paneOf(paneId: string): Pane | undefined {
  return props.panes.find(p => p.id === paneId)
}

const aIsPane = computed(() => props.node.a.type === 'pane')
const bIsPane = computed(() => props.node.b.type === 'pane')

function startResize(e: MouseEvent) {
  e.preventDefault()
  const divider = e.currentTarget as HTMLElement
  const el = divider.parentElement as HTMLElement
  if (!el) return
  const dir = props.node.dir
  const startX = e.clientX
  const startY = e.clientY
  const startRatio = props.node.ratio
  const rect = el.getBoundingClientRect()
  const move = (ev: MouseEvent) => {
    const delta = dir === 'h' ? ev.clientX - startX : ev.clientY - startY
    const total = dir === 'h' ? rect.width : rect.height
    if (total <= 0) return
    props.node.ratio = Math.min(0.85, Math.max(0.15, startRatio + delta / total))
  }
  const up = () => {
    document.removeEventListener('mousemove', move)
    document.removeEventListener('mouseup', up)
  }
  document.addEventListener('mousemove', move)
  document.addEventListener('mouseup', up)
}
</script>

<template>
  <div class="split-pane" :class="node.dir">
    <TabPane v-if="aIsPane" :key="(node.a as PaneNode).paneId" :pane="paneOf((node.a as PaneNode).paneId)!" :show-toolbar="pass.showToolbar"
      :is-vertical="pass.isVertical" :vertical-width="pass.verticalWidth" :term-cfg="pass.termCfg" :show-welcome-pane-id="pass.showWelcomePaneId"
      @new-ssh="$emit('new-ssh')" @new-telnet="$emit('new-telnet')" @new-serial="$emit('new-serial')" />
    <SplitPane v-else :node="node.a as SplitNode" :panes="panes" :pass="pass" @new-ssh="$emit('new-ssh')" @new-telnet="$emit('new-telnet')" @new-serial="$emit('new-serial')" />

    <div class="split-divider" :class="node.dir" @mousedown="startResize"></div>

    <TabPane v-if="bIsPane" :key="(node.b as PaneNode).paneId" :pane="paneOf((node.b as PaneNode).paneId)!" :show-toolbar="pass.showToolbar"
      :is-vertical="pass.isVertical" :vertical-width="pass.verticalWidth" :term-cfg="pass.termCfg" :show-welcome-pane-id="pass.showWelcomePaneId"
      @new-ssh="$emit('new-ssh')" @new-telnet="$emit('new-telnet')" @new-serial="$emit('new-serial')" />
    <SplitPane v-else :node="node.b as SplitNode" :panes="panes" :pass="pass" @new-ssh="$emit('new-ssh')" @new-telnet="$emit('new-telnet')" @new-serial="$emit('new-serial')" />
  </div>
</template>

<style scoped>
.split-pane { flex: 1; min-width: 0; min-height: 0; display: flex; overflow: hidden; }
.split-pane.h { flex-direction: row; }
.split-pane.v { flex-direction: column; }
.split-divider { flex-shrink: 0; background: var(--border-color); position: relative; z-index: 5; transition: background 0.15s; }
.split-divider.h { width: 4px; cursor: col-resize; }
.split-divider.v { height: 4px; cursor: row-resize; }
.split-divider:hover, .split-divider:active { background: #0078d4; }
</style>