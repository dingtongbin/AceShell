<script setup lang="ts">
import { ref, watch } from 'vue'
import { NModal, NButton, NInput, NCheckbox } from 'naive-ui'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const props = defineProps<{
  show: boolean
  host: string
  username: string
}>()

const emit = defineEmits<{
  (e: 'update:show', val: boolean): void
  (e: 'submit', data: { username: string; rememberUser: boolean }): void
  (e: 'cancel'): void
}>()

const inputUser = ref('')
const rememberUser = ref(false)

watch(() => props.show, (val) => {
  if (val) {
    inputUser.value = props.username || ''
    rememberUser.value = false
  }
})

function handleSubmit() {
  emit('submit', {
    username: inputUser.value,
    rememberUser: rememberUser.value,
  })
  emit('update:show', false)
}

function handleCancel() {
  emit('cancel')
  emit('update:show', false)
}
</script>

<template>
  <n-modal :show="show" @update:show="emit('update:show', $event)" preset="dialog" :title="t('keyCredentialsDialog.title')" :show-icon="false" style="width: 420px" :closable="false" :mask-closable="false">
    <div style="margin-bottom: 12px; font-size: 13px; color: #999;">{{ t('keyCredentialsDialog.connectTo', { host }) }}</div>
    <div style="margin-bottom: 8px;">
      <label style="display: block; font-size: 12px; color: #999; margin-bottom: 4px;">{{ t('common.username') }}</label>
      <n-input v-model:value="inputUser" :placeholder="t('keyCredentialsDialog.usernamePlaceholder')" size="small" @keyup.enter="handleSubmit" />
    </div>
    <div style="display: flex; gap: 16px; margin-bottom: 8px;">
      <n-checkbox v-model:checked="rememberUser">{{ t('common.rememberUsername') }}</n-checkbox>
    </div>
    <template #action>
      <n-button @click="handleCancel">{{ t('common.cancel') }}</n-button>
      <n-button type="primary" @click="handleSubmit">{{ t('common.connect') }}</n-button>
    </template>
  </n-modal>
</template>