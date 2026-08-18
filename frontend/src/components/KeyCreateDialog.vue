<script setup lang="ts">
import { ref, watch } from 'vue'
import { NModal, NInput, NSelect, NButton, useMessage } from 'naive-ui'
import { CreateKey } from '../../bindings/changeme/internal/services/globalkeyservice.js'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ (e: 'update:show', v: boolean): void; (e: 'created'): void }>()

const message = useMessage()

const keyName = ref('')
const keyType = ref('ed25519')
const passphrase = ref('')
const creating = ref(false)

const typeOptions = [
  { label: 'Ed25519（推荐）', value: 'ed25519' },
  { label: 'RSA 2048', value: 'rsa2048' },
  { label: 'RSA 4096', value: 'rsa4096' },
]

watch(() => props.show, (v) => {
  if (v) { keyName.value = ''; keyType.value = 'ed25519'; passphrase.value = '' }
})

function validate(): string | null {
  if (!keyName.value.trim()) return '密钥名称不能为空'
  if (passphrase.value && passphrase.value.length < 4) return '口令至少 4 个字符'
  return null
}

async function doCreate() {
  const err = validate()
  if (err) { message.error(err); return }
  creating.value = true
  try {
    await CreateKey(keyName.value.trim(), keyType.value, passphrase.value)
    message.success('密钥创建成功')
    emit('created')
  } catch (e: any) {
    message.error('创建失败: ' + (e.message || e))
  } finally {
    creating.value = false
  }
}
</script>

<template>
  <n-modal :show="show" @update:show="emit('update:show', $event)" preset="dialog" title="新建 SSH 密钥" :show-icon="false" style="width: 440px" :mask-closable="false">
    <div class="key-form">
      <div class="form-group">
        <label class="form-label">密钥名称 <span style="color: #e88070">*</span></label>
        <n-input v-model:value="keyName" placeholder="如：办公服务器" />
      </div>
      <div class="form-group">
        <label class="form-label">密钥类型</label>
        <n-select v-model:value="keyType" :options="typeOptions" />
      </div>
      <div class="form-group">
        <label class="form-label">口令（可选）</label>
        <n-input v-model:value="passphrase" type="password" show-password-on="click" placeholder="私钥加密口令，可留空" />
        <div class="form-hint">设置口令后，私钥将以该口令加密保存；连接时自动使用，无需重复输入。</div>
      </div>
    </div>
    <template #action>
      <n-button @click="emit('update:show', false)">取消</n-button>
      <n-button type="primary" :loading="creating" @click="doCreate">创建</n-button>
    </template>
  </n-modal>
</template>

<style scoped>
.key-form { display: flex; flex-direction: column; gap: 12px; }
.form-group { display: flex; flex-direction: column; gap: 4px; }
.form-label { font-size: 12px; color: var(--text-color-2, #999); }
.form-hint { font-size: 11px; color: var(--text-color-3, #777); line-height: 1.5; }
</style>