<script setup lang="ts">
import { ref, watch, onMounted } from 'vue'
import { NButton, NSelect, NScrollbar } from 'naive-ui'
import { ListPorts } from '../../bindings/changeme/internal/services/serialservice.js'
import { GetConfig, SetSerialConfig } from '../../bindings/changeme/internal/services/configservice.js'

const emit = defineEmits<{
  (e: 'connect', portName: string, baudRate: number, dataBits: number, stopBits: string, parity: string): void
}>()

const ports = ref<string[]>([])
const portName = ref('')
const baudRate = ref(115200)
const dataBits = ref(8)
const stopBits = ref('1')
const parity = ref('none')

const baudOptions = [300, 1200, 2400, 4800, 9600, 19200, 38400, 57600, 115200, 230400, 460800, 921600].map(v => ({ label: String(v), value: v }))
const dataBitsOptions = [5, 6, 7, 8].map(v => ({ label: String(v), value: v }))
const stopBitsOptions = [
  { label: '1', value: '1' },
  { label: '1.5', value: '1.5' },
  { label: '2', value: '2' },
]
const parityOptions = [
  { label: '无', value: 'none' },
  { label: '奇校验 (Odd)', value: 'odd' },
  { label: '偶校验 (Even)', value: 'even' },
  { label: '标记 (Mark)', value: 'mark' },
  { label: '空 (Space)', value: 'space' },
]

async function loadConfig() {
  try {
    const cfg = JSON.parse(await GetConfig())
    if (cfg.serial) {
      portName.value = cfg.serial.port ?? ''
      baudRate.value = cfg.serial.baudRate ?? 115200
      dataBits.value = cfg.serial.dataBits ?? 8
      stopBits.value = cfg.serial.stopBits ?? '1'
      parity.value = cfg.serial.parity ?? 'none'
    }
  } catch {}
}

function saveConfig() {
  SetSerialConfig(JSON.stringify({
    port: portName.value,
    baudRate: baudRate.value,
    dataBits: dataBits.value,
    stopBits: stopBits.value,
    parity: parity.value,
  })).catch(() => {})
}

async function refreshPorts() {
  try {
    const raw = await ListPorts()
    ports.value = JSON.parse(raw)
    if (ports.value.length > 0 && !ports.value.includes(portName.value)) {
      portName.value = ports.value[0]
    }
  } catch {
    ports.value = []
  }
}

function handleConnect() {
  if (!portName.value) return
  emit('connect', portName.value, baudRate.value, dataBits.value, stopBits.value, parity.value)
}

watch([portName, baudRate, dataBits, stopBits, parity], saveConfig)

onMounted(async () => {
  await loadConfig()
  refreshPorts()
})
</script>

<template>
  <n-scrollbar style="max-height: 100%">
    <div class="serial-fields">
      <div class="sm-row">
        <label class="sm-label">端口</label>
        <n-select size="tiny" v-model:value="portName" :options="ports.map(p => ({ label: p, value: p }))" placeholder="检测中..." @focus="refreshPorts" style="flex: 1" />
        <n-button size="tiny" @click="refreshPorts">刷新</n-button>
      </div>
      <div class="sm-row">
        <label class="sm-label">波特率</label>
        <n-select size="tiny" v-model:value="baudRate" :options="baudOptions" style="flex: 1" />
      </div>
      <div class="sm-row">
        <label class="sm-label">数据位</label>
        <n-select size="tiny" v-model:value="dataBits" :options="dataBitsOptions" style="flex: 1" />
      </div>
      <div class="sm-row">
        <label class="sm-label">停止位</label>
        <n-select size="tiny" v-model:value="stopBits" :options="stopBitsOptions" style="flex: 1" />
      </div>
      <div class="sm-row">
        <label class="sm-label">校验</label>
        <n-select size="tiny" v-model:value="parity" :options="parityOptions" style="flex: 1" />
      </div>
      <n-button size="tiny" type="primary" :disabled="!portName" @click="handleConnect" class="sm-connect-btn">连接</n-button>
    </div>
  </n-scrollbar>
</template>

<style scoped>
.serial-fields { display: flex; flex-direction: column; gap: 6px; padding: 6px 8px 8px; }
.sm-row { display: flex; align-items: center; gap: 6px; }
.sm-label { white-space: nowrap; font-size: 12px; min-width: 42px; text-align: right; }
.sm-connect-btn { align-self: center; }
</style>
