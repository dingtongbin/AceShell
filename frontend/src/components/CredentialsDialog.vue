<script setup lang="ts">
import { ref, watch } from 'vue'
import { NModal, NButton, NInput, NCheckbox } from 'naive-ui'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const props = defineProps<{
  show: boolean
  host: string
  username: string
  hasPassword: boolean
  title?: string
}>()

const emit = defineEmits<{
  (e: 'update:show', val: boolean): void
  (e: 'submit', data: { username: string; password: string; rememberUser: boolean; rememberPass: boolean }): void
  (e: 'cancel'): void
}>()

const inputUser = ref('')
const inputPass = ref('')
const rememberUser = ref(false)
const rememberPass = ref(false)

watch(() => props.show, (val) => {
  if (val) {
    inputUser.value = props.username || ''
    inputPass.value = ''
    rememberUser.value = false
    rememberPass.value = false
  }
})

function handleSubmit() {
  emit('submit', {
    username: inputUser.value,
    password: inputPass.value,
    rememberUser: rememberUser.value,
    rememberPass: rememberPass.value,
  })
  emit('update:show', false)
}

function handleCancel() {
  emit('cancel')
  emit('update:show', false)
}
</script>

<template>
  <n-modal :show="show" @update:show="emit('update:show', $event)" preset="dialog" :title="title || t('credentialsDialog.title')" :show-icon="false" style="width: 420px" :closable="false" :mask-closable="false">
    <div style="margin-bottom: 12px; font-size: 13px; color: #999;">{{ t('credentialsDialog.connectTo', { host }) }}</div>
    <div style="margin-bottom: 12px;">
      <label style="display: block; font-size: 12px; color: #999; margin-bottom: 4px;">{{ t('common.username') }}</label>
      <n-input v-model:value="inputUser" :placeholder="t('credentialsDialog.placeholderDefaultUser')" size="small" />
    </div>
    <div style="margin-bottom: 16px;">
      <label style="display: block; font-size: 12px; color: #999; margin-bottom: 4px;">{{ t('common.password') }}</label>
      <n-input v-model:value="inputPass" type="password" show-password-on="click" :placeholder="hasPassword ? t('credentialsDialog.hasPasswordPlaceholder') : t('credentialsDialog.inputPassword')" size="small" />
    </div>
    <div style="display: flex; gap: 16px; margin-bottom: 8px;">
      <n-checkbox v-model:checked="rememberUser">{{ t('common.rememberUsername') }}</n-checkbox>
      <n-checkbox v-model:checked="rememberPass">{{ t('common.rememberPassword') }}</n-checkbox>
    </div>
    <template #action>
      <n-button @click="handleCancel">{{ t('common.cancel') }}</n-button>
      <n-button type="primary" @click="handleSubmit">{{ t('common.connect') }}</n-button>
    </template>
  </n-modal>
</template>
