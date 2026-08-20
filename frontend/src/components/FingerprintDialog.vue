<script setup lang="ts">
import { ref } from 'vue'
import { NModal, NButton, NRadioGroup, NRadioButton, NInput } from 'naive-ui'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

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
  <n-modal :show="show" @update:show="emit('update:show', $event)" preset="dialog" :title="t('fingerprintDialog.title')" :show-icon="false" style="width: 500px" :closable="false" :mask-closable="false">
    <div v-if="status === 'not_found'" style="margin-bottom: 16px;">
      <p style="margin-bottom: 12px;">{{ t('fingerprintDialog.firstConnect', { host }) }}</p>
      <n-input type="textarea" :value="fingerprint" readonly :rows="3" style="font-family: Consolas, monospace; font-size: 12px;" />
      <div style="margin-top: 12px;">
        <n-radio-group v-model:value="saveType" size="small">
          <n-radio-button value="permanent">{{ t('common.permanentSave') }}</n-radio-button>
          <n-radio-button value="once">{{ t('common.onlyOnce') }}</n-radio-button>
        </n-radio-group>
      </div>
    </div>
    <div v-else-if="status === 'mismatch'" style="margin-bottom: 16px;">
      <p style="color: #e45858; margin-bottom: 12px;">{{ t('fingerprintDialog.mismatchWarn') }}</p>
      <p style="margin-bottom: 8px;">{{ t('fingerprintDialog.serverFingerprint', { host }) }}</p>
      <n-input type="textarea" :value="fingerprint" readonly :rows="3" style="font-family: Consolas, monospace; font-size: 12px;" />
    </div>
    <template #action>
      <n-button v-if="status === 'not_found'" type="primary" @click="handleCancel">{{ t('common.cancel') }}</n-button>
      <n-button v-if="status === 'mismatch'" type="primary" @click="handleCancel">{{ t('common.cancel') }}</n-button>
      <n-button v-if="status === 'mismatch'" @click="handleSkip">{{ t('common.skip') }}</n-button>
      <n-button v-if="status === 'not_found'" @click="handleConfirm">{{ t('common.ok') }}</n-button>
    </template>
  </n-modal>
</template>
