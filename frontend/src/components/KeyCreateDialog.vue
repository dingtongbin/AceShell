<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { NModal, NInput, NSelect, NButton, useMessage } from 'naive-ui'
import { CreateKey } from '../../bindings/changeme/internal/services/globalkeyservice.js'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ (e: 'update:show', v: boolean): void; (e: 'created'): void }>()

const message = useMessage()

const keyName = ref('')
const keyType = ref('ed25519')
const passphrase = ref('')
const creating = ref(false)

const typeOptions = computed(() => [
  { label: t('keyCreateDialog.ed25519Recommended'), value: 'ed25519' },
  { label: 'RSA 2048', value: 'rsa2048' },
  { label: 'RSA 4096', value: 'rsa4096' },
])

watch(() => props.show, (v) => {
  if (v) { keyName.value = ''; keyType.value = 'ed25519'; passphrase.value = '' }
})

function validate(): string | null {
  if (!keyName.value.trim()) return t('keyCreateDialog.nameRequired')
  if (passphrase.value && passphrase.value.length < 4) return t('keyCreateDialog.passphraseTooShort')
  return null
}

async function doCreate() {
  const err = validate()
  if (err) { message.error(err); return }
  creating.value = true
  try {
    await CreateKey(keyName.value.trim(), keyType.value, passphrase.value)
    message.success(t('keyCreateDialog.created'))
    emit('created')
  } catch (e: any) {
    message.error(t('keyCreateDialog.createFailed', { err: e.message || e }))
  } finally {
    creating.value = false
  }
}
</script>

<template>
  <n-modal :show="show" @update:show="emit('update:show', $event)" preset="dialog" :title="t('keyCreateDialog.title')" :show-icon="false" style="width: 440px" :mask-closable="false">
    <div class="key-form">
      <div class="form-group">
        <label class="form-label">{{ t('keyCreateDialog.keyName') }} <span style="color: #e88070">*</span></label>
        <n-input v-model:value="keyName" :placeholder="t('keyCreateDialog.keyNamePlaceholder')" />
      </div>
      <div class="form-group">
        <label class="form-label">{{ t('keyCreateDialog.keyType') }}</label>
        <n-select v-model:value="keyType" :options="typeOptions" />
      </div>
      <div class="form-group">
        <label class="form-label">{{ t('keyCreateDialog.passphrase') }}</label>
        <n-input v-model:value="passphrase" type="password" show-password-on="click" :placeholder="t('keyCreateDialog.passphrasePlaceholder')" />
        <div class="form-hint">{{ t('keyCreateDialog.passphraseHint') }}</div>
      </div>
    </div>
    <template #action>
      <n-button @click="emit('update:show', false)">{{ t('common.cancel') }}</n-button>
      <n-button type="primary" :loading="creating" @click="doCreate">{{ t('common.create') }}</n-button>
    </template>
  </n-modal>
</template>

<style scoped>
.key-form { display: flex; flex-direction: column; gap: 12px; }
.form-group { display: flex; flex-direction: column; gap: 4px; }
.form-label { font-size: 12px; color: var(--text-color-2, #999); }
.form-hint { font-size: 11px; color: var(--text-color-3, #777); line-height: 1.5; }
</style>