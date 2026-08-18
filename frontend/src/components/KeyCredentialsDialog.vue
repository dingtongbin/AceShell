<script setup lang="ts">
import { ref, watch } from 'vue'
import { NModal, NButton, NInput, NCheckbox } from 'naive-ui'

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
  <n-modal :show="show" @update:show="emit('update:show', $event)" preset="dialog" title="SSH 密钥登录" :show-icon="false" style="width: 420px" :closable="false" :mask-closable="false">
    <div style="margin-bottom: 12px; font-size: 13px; color: #999;">连接到 <b>{{ host }}</b></div>
    <div style="margin-bottom: 8px;">
      <label style="display: block; font-size: 12px; color: #999; margin-bottom: 4px;">用户名</label>
      <n-input v-model:value="inputUser" placeholder="输入登录账户" size="small" @keyup.enter="handleSubmit" />
    </div>
    <div style="display: flex; gap: 16px; margin-bottom: 8px;">
      <n-checkbox v-model:checked="rememberUser">记住用户名</n-checkbox>
    </div>
    <template #action>
      <n-button @click="handleCancel">取消</n-button>
      <n-button type="primary" @click="handleSubmit">连接</n-button>
    </template>
  </n-modal>
</template>