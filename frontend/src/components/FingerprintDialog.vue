<script setup lang="ts">
import { ref } from 'vue'
import { NModal, NButton, NRadioGroup, NRadioButton, NInput } from 'naive-ui'

const props = defineProps<{
  show: boolean
  host: string
  fingerprint: string
  status: 'not_found' | 'mismatch'
}>()

const emit = defineEmits<{
  (e: 'update:show', val: boolean): void
  (e: 'confirm', saveType: 'once' | 'permanent'): void
  (e: 'skip'): void
  (e: 'cancel'): void
}>()

const saveType = ref<'once' | 'permanent'>('once')

function handleConfirm() {
  emit('confirm', saveType.value)
  emit('update:show', false)
}

function handleSkip() {
  emit('skip')
  emit('update:show', false)
}

function handleCancel() {
  emit('cancel')
  emit('update:show', false)
}
</script>

<template>
  <n-modal :show="show" @update:show="emit('update:show', $event)" preset="dialog" title="SSH 指纹验证" :show-icon="false" style="width: 500px" :closable="false" :mask-closable="false">
    <div v-if="status === 'not_found'" style="margin-bottom: 16px;">
      <p style="margin-bottom: 12px;">首次连接到 <b>{{ host }}</b>，请确认服务器指纹：</p>
      <n-input type="textarea" :value="fingerprint" readonly :rows="3" style="font-family: Consolas, monospace; font-size: 12px;" />
      <div style="margin-top: 12px;">
        <n-radio-group v-model:value="saveType" size="small">
          <n-radio-button value="permanent">永久保存</n-radio-button>
          <n-radio-button value="once">仅本次</n-radio-button>
        </n-radio-group>
      </div>
    </div>
    <div v-else-if="status === 'mismatch'" style="margin-bottom: 16px;">
      <p style="color: #e45858; margin-bottom: 12px;">⚠ 服务器指纹与已保存的不匹配！可能意味着服务器已更换，或存在中间人攻击风险。</p>
      <p style="margin-bottom: 8px;">服务器 <b>{{ host }}</b> 的指纹：</p>
      <n-input type="textarea" :value="fingerprint" readonly :rows="3" style="font-family: Consolas, monospace; font-size: 12px;" />
    </div>
    <template #action>
      <n-button v-if="status === 'not_found'" type="primary" @click="handleCancel">取消</n-button>
      <n-button v-if="status === 'mismatch'" type="primary" @click="handleCancel">取消</n-button>
      <n-button v-if="status === 'mismatch'" @click="handleSkip">跳过</n-button>
      <n-button v-if="status === 'not_found'" @click="handleConfirm">确认</n-button>
    </template>
  </n-modal>
</template>
