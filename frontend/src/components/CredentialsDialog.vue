<script setup lang="ts">
import { ref, watch } from 'vue'
import { NModal, NButton, NInput, NCheckbox } from 'naive-ui'

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
  <n-modal :show="show" @update:show="emit('update:show', $event)" preset="dialog" :title="title || 'SSH 登录凭证'" :show-icon="false" style="width: 420px" :closable="false" :mask-closable="false">
    <div style="margin-bottom: 12px; font-size: 13px; color: #999;">连接到 <b>{{ host }}</b></div>
    <div style="margin-bottom: 12px;">
      <label style="display: block; font-size: 12px; color: #999; margin-bottom: 4px;">用户名</label>
      <n-input v-model:value="inputUser" placeholder="留空则使用默认用户" size="small" />
    </div>
    <div style="margin-bottom: 16px;">
      <label style="display: block; font-size: 12px; color: #999; margin-bottom: 4px;">密码</label>
      <n-input v-model:value="inputPass" type="password" show-password-on="click" :placeholder="hasPassword ? '已有密码，需要修改请编辑内容' : '输入密码'" size="small" />
    </div>
    <div style="display: flex; gap: 16px; margin-bottom: 8px;">
      <n-checkbox v-model:checked="rememberUser">记住用户名</n-checkbox>
      <n-checkbox v-model:checked="rememberPass">记住密码</n-checkbox>
    </div>
    <template #action>
      <n-button @click="handleCancel">取消</n-button>
      <n-button type="primary" @click="handleSubmit">连接</n-button>
    </template>
  </n-modal>
</template>
