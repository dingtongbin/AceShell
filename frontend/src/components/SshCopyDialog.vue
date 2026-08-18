<script setup lang="ts">
import { ref, watch } from 'vue'
import { NModal, NInput, NInputNumber, NButton, useMessage } from 'naive-ui'
import { SshCopyKey } from '../../bindings/changeme/internal/services/globalkeyservice.js'

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
  if (!keyRef.value) { message.error('请先选择密钥'); return }
  if (!targetHost.value.trim()) { message.error('主机地址不能为空'); return }
  if (!targetUser.value.trim()) { message.error('用户名不能为空'); return }
  if (!targetPassword.value) { message.error('请输入目标主机密码'); return }
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
  <n-modal :show="show" @update:show="emit('update:show', $event)" preset="dialog" title="部署公钥到主机" :show-icon="false" style="width: 460px" :mask-closable="false">
    <div class="copy-form">
      <div class="form-group">
        <label class="form-label">密钥</label>
        <n-input v-model:value="keyRef" disabled placeholder="当前密钥" />
      </div>
      <div class="form-group">
        <label class="form-label">目标主机 <span style="color: #e88070">*</span></label>
        <n-input v-model:value="targetHost" placeholder="IP 或域名" />
      </div>
      <div class="form-group">
        <label class="form-label">端口</label>
        <n-input-number v-model:value="targetPort" :min="1" :max="65535" style="width: 100%" />
      </div>
      <div class="form-group">
        <label class="form-label">用户名 <span style="color: #e88070">*</span></label>
        <n-input v-model:value="targetUser" placeholder="登录用户名" />
      </div>
      <div class="form-group">
        <label class="form-label">密码 <span style="color: #e88070">*</span></label>
        <n-input v-model:value="targetPassword" type="password" show-password-on="click" placeholder="用于本次部署的登录密码" />
      </div>
      <div class="form-hint">将当前密钥的公钥追加到目标主机 ~/.ssh/authorized_keys，之后即可用该密钥免密登录。</div>
    </div>
    <template #action>
      <n-button @click="emit('update:show', false)">取消</n-button>
      <n-button type="primary" :loading="copying" @click="doCopy">部署</n-button>
    </template>
  </n-modal>
</template>

<style scoped>
.copy-form { display: flex; flex-direction: column; gap: 12px; }
.form-group { display: flex; flex-direction: column; gap: 4px; }
.form-label { font-size: 12px; color: var(--text-color-2, #999); }
.form-hint { font-size: 11px; color: var(--text-color-3, #777); line-height: 1.5; }
</style>