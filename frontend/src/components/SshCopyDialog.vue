<script setup lang="ts">
import { ref, watch } from 'vue'
import { NModal, NInput, NInputNumber, NButton, useMessage } from 'naive-ui'
import { SshCopyKey } from '../../bindings/changeme/internal/services/globalkeyservice.js'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const props = defineProps<{
  show: boolean
  selectedKey: string
  host: string
  port: number
  user: string
}>()
const emit = defineEmits<{ (e: 'update:show', v: boolean): void; (e: 'done'): void }>()

const message = useMessage()

const keyRef = ref('')
const targetHost = ref('')
const targetPort = ref(22)
const targetUser = ref('')
const targetPassword = ref('')
const copying = ref(false)

watch(() => props.show, (v) => {
  if (v) {
    keyRef.value = props.selectedKey || ''
    targetHost.value = props.host || ''
    targetPort.value = props.port || 22
    targetUser.value = props.user || ''
    targetPassword.value = ''
  }
})

async function doCopy() {
  if (!keyRef.value) { message.error(t('sshCopyDialog.selectKeyFirst')); return }
  if (!targetHost.value.trim()) { message.error(t('sshCopyDialog.hostRequired')); return }
  if (!targetUser.value.trim()) { message.error(t('sshCopyDialog.usernameRequired')); return }
  if (!targetPassword.value) { message.error(t('sshCopyDialog.passwordRequired')); return }
  copying.value = true
  try {
    const result = await SshCopyKey(keyRef.value, targetHost.value.trim(), targetPort.value, targetUser.value.trim(), targetPassword.value)
    message.success(result)
    emit('done')
  } catch (e: any) {
    message.error((e.message || e).toString())
  } finally {
    copying.value = false
  }
}
</script>

<template>
  <n-modal :show="show" @update:show="emit('update:show', $event)" preset="dialog" :title="t('sshCopyDialog.title')" :show-icon="false" style="width: 460px" :mask-closable="false">
    <div class="copy-form">
      <div class="form-group">
        <label class="form-label">{{ t('sshCopyDialog.key') }}</label>
        <n-input v-model:value="keyRef" disabled :placeholder="t('sshCopyDialog.currentKeyPlaceholder')" />
      </div>
      <div class="form-group">
        <label class="form-label">{{ t('sshCopyDialog.targetHost') }} <span style="color: #e88070">*</span></label>
        <n-input v-model:value="targetHost" :placeholder="t('sshCopyDialog.hostPlaceholder')" />
      </div>
      <div class="form-group">
        <label class="form-label">{{ t('common.port') }}</label>
        <n-input-number v-model:value="targetPort" :min="1" :max="65535" style="width: 100%" />
      </div>
      <div class="form-group">
        <label class="form-label">{{ t('common.username') }} <span style="color: #e88070">*</span></label>
        <n-input v-model:value="targetUser" :placeholder="t('sshCopyDialog.usernamePlaceholder')" />
      </div>
      <div class="form-group">
        <label class="form-label">{{ t('common.password') }} <span style="color: #e88070">*</span></label>
        <n-input v-model:value="targetPassword" type="password" show-password-on="click" :placeholder="t('sshCopyDialog.passwordPlaceholder')" />
      </div>
      <div class="form-hint">{{ t('sshCopyDialog.hint') }}</div>
    </div>
    <template #action>
      <n-button @click="emit('update:show', false)">{{ t('common.cancel') }}</n-button>
      <n-button type="primary" :loading="copying" @click="doCopy">{{ t('sshCopyDialog.deploy') }}</n-button>
    </template>
  </n-modal>
</template>

<style scoped>
.copy-form { display: flex; flex-direction: column; gap: 12px; }
.form-group { display: flex; flex-direction: column; gap: 4px; }
.form-label { font-size: 12px; color: var(--text-color-2, #999); }
.form-hint { font-size: 11px; color: var(--text-color-3, #777); line-height: 1.5; }
</style>