<script setup lang="ts">
// 智能体设置弹窗: 左 Tab 右内容。
//   AI 服务  — 多档案管理(提供商/端点/密钥/模型/API模式),测试连接,拉取模型列表
//   对话行为 — 启用开关/权限模式/步数上限/渲染窗口/上下文上限
//   技能库   — 内置运维技能(只读)说明
// API Key 留空 = 保留已存密文;填写则重新加密落盘。
import { ref, computed, watch } from 'vue'
import {
  NModal, NButton, NInput, NInputNumber, NSelect, NRadioGroup, NRadioButton,
  NTag, NIcon, NSwitch, NScrollbar, NDynamicTags, useMessage, useDialog,
} from 'naive-ui'
import {
  ServerOutline, SettingsOutline, ExtensionPuzzleOutline, AddOutline, TrashOutline,
  CheckmarkCircleOutline, RefreshOutline, KeyOutline, CreateOutline, PulseOutline,
  CubeOutline,
} from '@vicons/ionicons5'
import { useI18n } from 'vue-i18n'
import { GetAgentPresets, AgentTestConnection, AgentListModels, GetAgentSkills, AgentDiagnose, AgentGetMcpConfig, AgentSetMcpConfig, AgentTestMcpServer } from '../../bindings/changeme/internal/services/agentservice.js'
import { AgentCfg, AgentProfilesGet, AgentProfilesSet, SetAgentBehavior, SetAgentEnabled } from '../../bindings/changeme/internal/services/configservice.js'
import { useAgentBridge, agentDensity, setAgentDensity, type AgentDensity } from '../composables/useAgentBridge'

const { t } = useI18n()
const message = useMessage()
const dialog = useDialog()
const { refreshStatus } = useAgentBridge()

// 对话流密度(即时生效,localStorage 持久化)
const densityVal = computed<AgentDensity>({
  get: () => agentDensity.value,
  set: v => setAgentDensity(v),
})

const show = defineModel<boolean>('show', { default: false })

interface Preset { name: string; label: string; baseURL: string; models: string[]; note: string }
interface ProfileForm {
  id: string
  name: string
  provider: string
  baseURL: string
  model: string
  apiMode: string
  hasKey: boolean
  apiKeyInput: string // 留空 = 保留原密文
  contextWindow: number // 模型上下文窗口(token;0=默认128K)
  customModels: string[] // 自定义模型(接口不可用时手动补充)
}

// ==================== Tab ====================

const activeTab = ref<'service' | 'behavior' | 'skills' | 'mcp'>('service')

const tabs = computed(() => [
  { key: 'service', label: t('agentSettings.tabService'), icon: ServerOutline },
  { key: 'behavior', label: t('agentSettings.tabBehavior'), icon: SettingsOutline },
  { key: 'mcp', label: t('agentSettings.tabMcp'), icon: CubeOutline },
  { key: 'skills', label: t('agentSettings.tabSkills'), icon: ExtensionPuzzleOutline },
])

// ==================== 模型配置(多档案) ====================

const presets = ref<Preset[]>([])
const profiles = ref<ProfileForm[]>([])
const activeProfileId = ref('')
const saving = ref(false)

// 编辑弹窗(新建/编辑共用): 草稿模式,确认后才写回列表
const editorShow = ref(false)
const editorIsNew = ref(false)
const editorDraft = ref<ProfileForm | null>(null)
const testing = ref(false)
const fetchingModels = ref(false)
const remoteModels = ref<string[]>([])

const providerOptions = computed(() => presets.value.map(p => ({
  label: p.label, value: p.name,
})))

const modelOptions = computed(() => {
  const p = presets.value.find(x => x.name === editorDraft.value?.provider)
  const presetModels = (p?.models || []).map(m => ({ label: m, value: m }))
  const extra = remoteModels.value
    .filter(m => !presetModels.some(o => o.value === m))
    .map(m => ({ label: m, value: m }))
  const cur = editorDraft.value?.model
  if (cur && !presetModels.some(o => o.value === cur) && !extra.some(o => o.value === cur)) {
    presetModels.unshift({ label: cur, value: cur })
  }
  return [...presetModels, ...extra]
})

const currentPreset = computed(() => presets.value.find(x => x.name === editorDraft.value?.provider) || null)

function genId(): string {
  return 'p-' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2, 8)
}

function openEditorNew() {
  editorIsNew.value = true
  editorDraft.value = {
    id: genId(), name: '', provider: 'custom',
    baseURL: '', model: '', apiMode: 'chat',
    hasKey: false, apiKeyInput: '', contextWindow: 0, customModels: [],
  }
  remoteModels.value = []
  editorShow.value = true
}

function openEditorEdit(p: ProfileForm) {
  editorIsNew.value = false
  editorDraft.value = { ...p, apiKeyInput: '' } // 密钥留空=保留原密文
  remoteModels.value = []
  editorShow.value = true
}

// 编辑弹窗确认: 校验后写回列表并立即持久化(避免用户忘记点主弹窗保存导致密钥丢失)
async function editorConfirm() {
  const d = editorDraft.value
  if (!d) return
  if (!d.name.trim()) { message.warning(t('agentSettings.needName')); return }
  if (!d.baseURL.trim()) { message.warning(t('agentSettings.needBaseUrl')); return }
  if (!d.model.trim()) { message.warning(t('agentSettings.needModel')); return }
  // 无密钥防线: 档案未存密钥且本次未填写 → 二次确认(密钥按档案 ID 绑定,新档案不继承旧档案密钥)
  if (!d.hasKey && !d.apiKeyInput.trim()) {
    const go = await new Promise<boolean>(resolve => {
      dialog.warning({
        title: t('agentSettings.noKeyTitle'),
        content: t('agentSettings.noKeyWarning'),
        positiveText: t('common.confirm'),
        negativeText: t('common.cancel'),
        onPositiveClick: () => resolve(true),
        onNegativeClick: () => resolve(false),
        onClose: () => resolve(false),
        onMaskClick: () => resolve(false),
      })
    })
    if (!go) return
  }
  if (editorIsNew.value) {
    profiles.value.push({ ...d, name: d.name.trim(), baseURL: d.baseURL.trim(), model: d.model.trim() })
    if (!activeProfileId.value) activeProfileId.value = d.id
  } else {
    const idx = profiles.value.findIndex(p => p.id === d.id)
    if (idx >= 0) {
      profiles.value[idx] = { ...d, name: d.name.trim(), baseURL: d.baseURL.trim(), model: d.model.trim() }
    }
  }
  // 立即落盘:apiKey 非空→加密保存;空→保留原密文
  const err = await saveProfiles()
  if (err) return
  editorShow.value = false
}

function removeProfile(id: string) {
  const idx = profiles.value.findIndex(p => p.id === id)
  if (idx < 0) return
  profiles.value.splice(idx, 1)
  if (activeProfileId.value === id) activeProfileId.value = profiles.value[0]?.id || ''
  void saveProfiles()
}

async function activateProfile(id: string) {
  if (!id || activeProfileId.value === id) return
  activeProfileId.value = id
  await saveProfiles()
  await refreshStatus()
}

// 档案列表持久化到后端(apiKey 非空→重新加密;空→保留原密文)。返回错误信息,空=成功。
async function saveProfiles(): Promise<string> {
  if (profiles.value.length && !activeProfileId.value) {
    activeProfileId.value = profiles.value[0].id
  }
  const payload = {
    activeProfileId: activeProfileId.value,
    profiles: profiles.value.map(p => ({
      id: p.id, name: p.name.trim(), provider: p.provider,
      baseURL: p.baseURL.trim(), model: p.model.trim(), apiMode: p.apiMode,
      apiKey: p.apiKeyInput.trim(),
      contextWindow: Math.max(0, Math.floor(Number(p.contextWindow) || 0)),
      customModels: (p.customModels || []).map(m => m.trim()).filter(Boolean),
    })),
  }
  const raw = await AgentProfilesSet(JSON.stringify(payload))
  const parsed = raw ? JSON.parse(raw) : null
  if (parsed?.error) {
    message.error(String(parsed.error))
    return String(parsed.error)
  }
  apiKeyInputsClear()
  return ''
}

function handleProviderChange(name: string) {
  const d = editorDraft.value
  if (!d || typeof name !== 'string' || !name) return
  d.provider = name
  const preset = presets.value.find(x => x.name === name)
  if (preset) {
    if (preset.baseURL) d.baseURL = preset.baseURL
    if (preset.models?.length && !preset.models.includes(d.model)) d.model = preset.models[0]
  }
}

async function fetchRemoteModels() {
  const d = editorDraft.value
  if (!d?.baseURL) { message.warning(t('agentSettings.needBaseUrl')); return }
  fetchingModels.value = true
  try {
    const raw = JSON.parse(await AgentListModels(d.baseURL.trim(), d.apiKeyInput.trim(), ''))
    if (raw?.error) message.error(String(raw.error))
    else remoteModels.value = Array.isArray(raw) ? raw : []
  } catch (e: any) {
    message.error(String(e?.message || e))
  } finally { fetchingModels.value = false }
}

async function handleTest() {
  const d = editorDraft.value
  if (!d?.baseURL) { message.warning(t('agentSettings.needBaseUrl')); return }
  testing.value = true
  try {
    // 优先用表单值(密钥留空则后端用已存密钥)
    const raw = JSON.parse(await AgentTestConnection(d.baseURL, d.apiKeyInput.trim()))
    if (raw?.ok) message.success(t('agentSettings.testOk'))
    else message.error(String(raw?.error || 'Connection failed'))
  } catch (e: any) {
    message.error(String(e?.message || e))
  } finally { testing.value = false }
}

// ==================== 对话行为 ====================

const behavior = ref({
  enabled: false,
  permMode: 'manual',
  maxSteps: 24,
  historyWindow: 200,
  contextMaxEvents: 400,
})

// ==================== 技能库 ====================

const skills = ref<{ id: string; name: string; desc: string; prompt: string }[]>([])

// ==================== 外接 MCP(设置页 MCP tab) ====================

interface McpServerForm {
  name: string
  type: 'http' | 'sse' | 'stdio' | 'builtin'
  url: string
  command: string
  argsText: string // JSON 数组文本,如 ["-y","pkg"]
  envJson: string // JSON 对象文本
  headersJson: string // JSON 对象文本
  enabled: boolean
}

const mcpServers = ref<McpServerForm[]>([])
const mcpSaving = ref(false)
const mcpTestResults = ref<Record<string, string>>({})

async function loadMcpConfig() {
  try {
    const raw = JSON.parse(await AgentGetMcpConfig())
    console.log('[agent-mcp] config loaded:', raw)
    if (raw?.error) { message.error(String(raw.error)); return }
    const map = raw?.mcpServers || {}
    mcpServers.value = Object.entries(map).map(([name, s]: [string, any]) => ({
      name,
      type: (['http', 'sse', 'stdio', 'builtin'].includes(s?.type) ? s.type : 'http') as McpServerForm['type'],
      url: String(s?.url || ''),
      command: String(s?.command || ''),
      argsText: Array.isArray(s?.args) ? JSON.stringify(s.args) : '',
      envJson: s?.env && Object.keys(s.env).length ? JSON.stringify(s.env, null, 2) : '',
      headersJson: s?.headers && Object.keys(s.headers).length ? JSON.stringify(s.headers, null, 2) : '',
      enabled: !!s?.enabled,
    }))
  } catch (e: any) {
    message.error(String(e?.message || e))
  }
}

function addMcpServer() {
  mcpServers.value.push({ name: '', type: 'http', url: '', command: '', argsText: '', envJson: '', headersJson: '', enabled: true })
}

function removeMcpServer(i: number) {
  const name = mcpServers.value[i]?.name
  mcpServers.value.splice(i, 1)
  if (name) delete mcpTestResults.value[name]
}

function parseJsonField(text: string, field: string): Record<string, string> | any[] | null {
  if (!text.trim()) return null
  try {
    return JSON.parse(text)
  } catch {
    throw new Error(t('agentSettings.mcpBadJson', { field }))
  }
}

async function saveMcpConfig() {
  const map: Record<string, any> = {}
  for (const s of mcpServers.value) {
    const name = s.name.trim()
    if (!name) { message.error(t('agentSettings.mcpNameRequired')); return }
    if (map[name]) { message.error(t('agentSettings.mcpDupName', { name })); return }
    let entry: Record<string, any> = { type: s.type, enabled: s.enabled }
    try {
      if (s.type === 'http' || s.type === 'sse') {
        entry.url = s.url.trim()
        entry.headers = parseJsonField(s.headersJson, 'headers') ?? {}
        if (!entry.url) throw new Error(t('agentSettings.mcpUrlRequired'))
      } else if (s.type === 'stdio') {
        entry.command = s.command.trim()
        entry.args = parseJsonField(s.argsText, 'args') ?? []
        entry.env = parseJsonField(s.envJson, 'env') ?? {}
        if (!entry.command) throw new Error(t('agentSettings.mcpCmdRequired'))
      }
      // builtin 无附加字段
    } catch (e: any) {
      message.error(`[${name}] ` + String(e?.message || e))
      return
    }
    map[name] = entry
  }
  mcpSaving.value = true
  try {
    const res = JSON.parse(await AgentSetMcpConfig(JSON.stringify({ mcpServers: map })))
    if (res?.error) { message.error(String(res.error)); return }
    message.success(t('agentSettings.saved'))
    await loadMcpConfig()
  } catch (e: any) {
    message.error(String(e?.message || e))
  } finally { mcpSaving.value = false }
}

async function testMcpServer(name: string) {
  mcpTestResults.value[name] = t('agentSettings.mcpTesting')
  try {
    const res = JSON.parse(await AgentTestMcpServer(name))
    if (res?.error) {
      mcpTestResults.value[name] = '✗ ' + String(res.error)
    } else {
      mcpTestResults.value[name] = '✓ ' + t('agentSettings.mcpToolsFound', { n: (res.tools || []).length }) + ((res.tools || []).length ? ': ' + res.tools.join(', ') : '')
    }
  } catch (e: any) {
    mcpTestResults.value[name] = '✗ ' + String(e?.message || e)
  }
}

// ==================== 加载/保存 ====================

watch(show, async open => {
  if (!open) return
  activeTab.value = 'service'
  apiKeyInputsClear()
  try {
    const list = JSON.parse(await GetAgentPresets())
    if (Array.isArray(list)) presets.value = list
  } catch { /* ignore */ }
  try {
    const raw = JSON.parse(await AgentProfilesGet())
    const list: any[] = Array.isArray(raw?.profiles) ? raw.profiles : []
    profiles.value = list.map(p => ({
      id: String(p.id || genId()),
      name: String(p.name || ''),
      provider: String(p.provider || 'custom'),
      baseURL: String(p.baseURL || ''),
      model: String(p.model || ''),
      apiMode: String(p.apiMode || 'chat'),
      hasKey: !!p.hasKey,
      apiKeyInput: '',
      contextWindow: Number(p.contextWindow) || 0,
      customModels: Array.isArray(p.customModels) ? p.customModels.map(String) : [],
    }))
    activeProfileId.value = String(raw?.activeProfileId || profiles.value[0]?.id || '')
  } catch { /* ignore */ }
  try {
    const cfg = await AgentCfg()
    if (cfg) {
      behavior.value = {
        enabled: !!cfg.enabled,
        permMode: String(cfg.permMode || 'manual'),
        maxSteps: Number(cfg.maxSteps) || 24,
        historyWindow: Number(cfg.historyWindow) || 200,
        contextMaxEvents: Number(cfg.contextMaxEvents) || 400,
      }
    }
  } catch { /* ignore */ }
  try {
    const raw = JSON.parse(await GetAgentSkills())
    if (Array.isArray(raw)) skills.value = raw
  } catch { /* ignore */ }
  await loadMcpConfig()
})

// 切到 MCP tab 时若尚未加载过则补拉(防弹窗打开早期调用失败后无重试机会)
watch(activeTab, async tab => {
  if (tab === 'mcp' && mcpServers.value.length === 0) await loadMcpConfig()
})

function apiKeyInputsClear() {
  for (const p of profiles.value) p.apiKeyInput = ''
  remoteModels.value = []
}

// ==================== 运行时诊断 ====================
const diagShow = ref(false)
const diagging = ref(false)
const diagRows = ref<{ k: string; v: string; bad?: boolean }[]>([])

async function runDiagnose() {
  diagging.value = true
  try {
    const raw = JSON.parse(await AgentDiagnose(false))
    const fmt = (label: string, key: string, badWhen?: (v: any) => boolean) => {
      const v = raw?.[key]
      diagRows.value.push({ k: label, v: v === undefined ? '-' : String(v), bad: badWhen ? badWhen(v) : false })
    }
    diagRows.value = []
    fmt(t('agentSettings.diagEnabled'), 'enabled', v => v !== true)
    fmt(t('agentSettings.diagProfileCount'), 'profileCount', v => !v)
    fmt(t('agentSettings.diagActiveFound'), 'activeFound', v => v !== true)
    fmt(t('agentSettings.diagBaseUrl'), 'baseURL', v => !v)
    fmt(t('agentSettings.diagModel'), 'model', v => !v)
    fmt(t('agentSettings.diagKeyStored'), 'keyEncStored', v => v !== true)
    fmt(t('agentSettings.diagKeyDecrypt'), 'keyDecryptOk', v => v !== true)
    if (raw?.keyError) diagRows.value.push({ k: t('agentSettings.diagKeyError'), v: String(raw.keyError), bad: true })
    diagRows.value.push({
      k: t('agentSettings.diagBlocked'),
      v: raw?.sendBlocked ? String(raw.sendBlocked) : t('agentSettings.diagPass'),
      bad: !!raw?.sendBlocked,
    })
    diagShow.value = true
  } catch (e: any) {
    message.error(String(e?.message || e))
  } finally { diagging.value = false }
}

async function handleSave() {
  saving.value = true
  try {
    // 1. 档案(立即持久化过,此处幂等重存)
    const errProfiles = await saveProfiles()
    if (errProfiles) return
    // 2. 行为参数
    const rawBehavior = await SetAgentBehavior(
      behavior.value.permMode, behavior.value.maxSteps,
      behavior.value.historyWindow, behavior.value.contextMaxEvents,
    )
    const errBehavior = rawBehavior ? JSON.parse(rawBehavior) : null
    if (errBehavior?.error) { message.error(String(errBehavior.error)); return }
    // 3. 启用开关
    await SetAgentEnabled(behavior.value.enabled)
    await refreshStatus()
    message.success(t('agentSettings.saved'))
    apiKeyInputsClear()
    show.value = false
  } catch (e: any) {
    message.error(String(e?.message || e))
  } finally { saving.value = false }
}
</script>

<template>
  <n-modal v-model:show="show" :title="t('agentSettings.title') + ' [v2]'" preset="dialog" :show-icon="false" style="width: 740px" :mask-closable="false">
    <div class="agent-settings">
      <!-- 左侧 Tab 导航(垂直) -->
      <div class="as-tabs">
        <div
          v-for="tab in tabs"
          :key="tab.key"
          class="as-tab"
          :class="{ active: activeTab === tab.key }"
          @click="activeTab = tab.key as any"
        >
          <n-icon :size="15" :component="tab.icon" />
          <span>{{ tab.label }}</span>
        </div>
      </div>

      <!-- 右侧: 内容 + 底部操作 -->
      <div class="as-main">
      <div class="as-content">
        <n-scrollbar style="height: 100%">
          <!-- ==================== 模型配置 ==================== -->
          <div v-if="activeTab === 'service'" class="as-pane">
            <div class="as-section-title">
              {{ t('agentSettings.profiles') }}
              <div style="display:flex;gap:4px">
                <n-button size="tiny" quaternary :loading="diagging" @click="runDiagnose">
                  <template #icon><n-icon :size="12" :component="PulseOutline" /></template>
                  {{ t('agentSettings.diagnose') }}
                </n-button>
                <n-button size="tiny" quaternary type="primary" @click="openEditorNew">
                  <template #icon><n-icon :size="12" :component="AddOutline" /></template>
                  {{ t('agentSettings.addProfile') }}
                </n-button>
              </div>
            </div>
            <div v-if="!profiles.length" class="as-empty">{{ t('agentSettings.noProfiles') }}</div>
            <div
              v-for="p in profiles"
              :key="p.id"
              class="profile-item"
              :class="{ active: p.id === activeProfileId }"
            >
              <div class="profile-main" @click="openEditorEdit(p)">
                <div class="profile-name">
                  {{ p.name || t('agentSettings.unnamed') }}
                  <n-tag v-if="p.id === activeProfileId" size="tiny" type="success" round :bordered="false">{{ t('agentSettings.activeProfile') }}</n-tag>
                </div>
                <div class="profile-sub">{{ (p.model ? p.model + ' · ' : '') + p.baseURL }}</div>
              </div>
              <n-icon v-if="p.hasKey" :size="13" :component="KeyOutline" class="profile-key" />
              <n-button
                size="tiny" quaternary
                :disabled="p.id === activeProfileId || profiles.length <= 1"
                @click.stop="activateProfile(p.id)"
              >{{ t('agentSettings.setActive') }}</n-button>
              <n-button size="tiny" quaternary @click.stop="openEditorEdit(p)">
                <template #icon><n-icon :size="12" :component="CreateOutline" /></template>
              </n-button>
              <n-button size="tiny" quaternary type="error" @click.stop="removeProfile(p.id)">
                <template #icon><n-icon :size="12" :component="TrashOutline" /></template>
              </n-button>
            </div>
            <div class="as-pane-save">
              <n-button size="small" type="primary" :loading="saving" @click="handleSave">{{ t('common.save') }}</n-button>
            </div>
          </div>

          <!-- ==================== 对话行为 ==================== -->
          <div v-else-if="activeTab === 'behavior'" class="as-pane">
            <div class="cfg-row">
              <div class="cfg-main">
                <div class="cfg-label">{{ t('agentSettings.enable') }}</div>
                <div class="cfg-desc">{{ t('agentSettings.enableDesc') }}</div>
              </div>
              <n-switch v-model:value="behavior.enabled" size="small" />
            </div>
            <div class="cfg-row" style="margin-top: 12px">
              <div class="cfg-label">{{ t('agentSettings.permMode') }}</div>
            </div>
            <n-radio-group v-model:value="behavior.permMode" size="small" style="width: 100%; margin-top: 4px">
              <n-radio-button value="plan" style="flex: 1">{{ t('agent.permPlan') }}</n-radio-button>
              <n-radio-button value="manual" style="flex: 1">{{ t('agent.permManual') }}</n-radio-button>
              <n-radio-button value="auto" style="flex: 1">{{ t('agent.permAuto') }}</n-radio-button>
            </n-radio-group>
            <div class="cfg-desc" style="margin-top: 6px">
              {{ behavior.permMode === 'plan' ? t('agentSettings.permPlanDesc')
                : behavior.permMode === 'manual' ? t('agentSettings.permManualDesc')
                : t('agentSettings.permAutoDesc') }}
            </div>
            <div class="cfg-row" style="margin-top: 14px">
              <div class="cfg-label">{{ t('agentSettings.density') }}</div>
            </div>
            <n-radio-group :value="densityVal" size="small" style="width: 100%; margin-top: 4px" @update:value="densityVal = $event">
              <n-radio-button value="compact" style="flex: 1">{{ t('agentSettings.densCompact') }}</n-radio-button>
              <n-radio-button value="standard" style="flex: 1">{{ t('agentSettings.densStandard') }}</n-radio-button>
              <n-radio-button value="detailed" style="flex: 1">{{ t('agentSettings.densDetailed') }}</n-radio-button>
            </n-radio-group>
            <div class="cfg-desc" style="margin-top: 6px">
              {{ densityVal === 'compact' ? t('agentSettings.densCompactDesc')
                : densityVal === 'standard' ? t('agentSettings.densStandardDesc')
                : t('agentSettings.densDetailedDesc') }}
            </div>
            <div class="cfg-row" style="margin-top: 14px">
              <div class="cfg-main">
                <div class="cfg-label">{{ t('agentSettings.maxSteps') }}</div>
                <div class="cfg-desc">{{ t('agentSettings.maxStepsDesc') }}</div>
              </div>
              <n-input-number v-model:value="behavior.maxSteps" size="small" :min="1" :max="100" style="width: 110px; flex-shrink: 0" />
            </div>
            <div class="cfg-row">
              <div class="cfg-main">
                <div class="cfg-label">{{ t('agentSettings.historyWindow') }}</div>
                <div class="cfg-desc">{{ t('agentSettings.historyWindowDesc') }}</div>
              </div>
              <n-input-number v-model:value="behavior.historyWindow" size="small" :min="20" :max="1000" :step="20" style="width: 110px; flex-shrink: 0" />
            </div>
            <div class="cfg-row">
              <div class="cfg-main">
                <div class="cfg-label">{{ t('agentSettings.contextMax') }}</div>
                <div class="cfg-desc">{{ t('agentSettings.contextMaxDesc') }}</div>
              </div>
              <n-input-number v-model:value="behavior.contextMaxEvents" size="small" :min="20" :max="2000" :step="50" style="width: 110px; flex-shrink: 0" />
            </div>
            <div class="as-pane-save">
              <n-button size="small" type="primary" :loading="saving" @click="handleSave">{{ t('common.save') }}</n-button>
            </div>
          </div>

          <!-- ==================== 外接 MCP ==================== -->
          <!-- 独立 v-if(不挂 v-else-if 链),避免链式分支受相邻节点变化影响 -->
          <div v-if="activeTab === 'mcp'" class="as-pane">
            <div style="padding:4px 0;color:#888;font-size:11px">MCP PANEL · {{ mcpServers.length }} servers</div>
            <div class="as-section-title">
              {{ t('agentSettings.mcpServers') }}
              <div style="display:flex;gap:4px">
                <n-button size="tiny" quaternary @click="loadMcpConfig">
                  <template #icon><n-icon :size="12" :component="RefreshOutline" /></template>
                  {{ t('common.refresh') }}
                </n-button>
                <n-button size="tiny" quaternary type="primary" @click="addMcpServer">
                  <template #icon><n-icon :size="12" :component="AddOutline" /></template>
                  {{ t('agentSettings.mcpAdd') }}
                </n-button>
                <n-button size="tiny" quaternary type="success" :loading="mcpSaving" @click="saveMcpConfig">{{ t('common.save') }}</n-button>
              </div>
            </div>
            <div class="cfg-desc" style="margin-bottom: 8px">{{ t('agentSettings.mcpHint') }}</div>
            <div v-if="!mcpServers.length" class="as-empty">{{ t('agentSettings.mcpEmpty') }}</div>
            <div v-for="(s, i) in mcpServers" :key="i" class="mcp-item" :class="{ disabled: !s.enabled }">
              <div class="mcp-row">
                <n-switch v-model:value="s.enabled" size="small" />
                <n-input v-model:value="s.name" size="small" :placeholder="t('agentSettings.mcpNamePh')" style="width: 150px" />
                <n-select v-model:value="s.type" size="small" style="width: 120px" :options="[
                  { label: 'HTTP', value: 'http' },
                  { label: 'SSE', value: 'sse' },
                  { label: 'stdio', value: 'stdio' },
                  { label: t('agentSettings.mcpTypeBuiltin'), value: 'builtin' },
                ]" />
                <div style="margin-left:auto;display:flex;gap:4px;flex-shrink:0">
                  <n-button size="tiny" quaternary :disabled="!s.name.trim() || !s.enabled || s.type === 'builtin'" @click="testMcpServer(s.name)">{{ t('agentSettings.mcpTest') }}</n-button>
                  <n-button size="tiny" quaternary type="error" @click.stop="removeMcpServer(i)">
                    <template #icon><n-icon :size="12" :component="TrashOutline" /></template>
                  </n-button>
                </div>
              </div>
              <div v-if="s.type === 'http' || s.type === 'sse'" class="mcp-fields">
                <n-input v-model:value="s.url" size="small" :placeholder="t('agentSettings.mcpUrlPh')" />
                <n-input v-model:value="s.headersJson" type="textarea" size="small" :rows="2" :placeholder="t('agentSettings.mcpHeadersJson')" />
              </div>
              <div v-else-if="s.type === 'stdio'" class="mcp-fields">
                <div style="display:flex;gap:6px">
                  <n-input v-model:value="s.command" size="small" :placeholder="t('agentSettings.mcpCommandPh')" style="width: 160px; flex-shrink:0" />
                  <n-input v-model:value="s.argsText" size="small" :placeholder="t('agentSettings.mcpArgsJson')" style="flex:1" />
                </div>
                <n-input v-model:value="s.envJson" type="textarea" size="small" :rows="2" :placeholder="t('agentSettings.mcpEnvJson')" />
              </div>
              <div v-if="s.type === 'builtin'" class="cfg-desc">{{ t('agentSettings.mcpBuiltinDesc') }}</div>
              <div v-if="mcpTestResults[s.name]" class="mcp-test-result">{{ mcpTestResults[s.name] }}</div>
            </div>
          </div>

          <!-- ==================== 技能库 ==================== -->
          <div v-if="activeTab === 'skills'" class="as-pane">
            <div class="cfg-desc" style="margin-bottom: 10px">{{ t('agentSettings.skillsDesc') }}</div>
            <div v-for="s in skills" :key="s.id" class="skill-card">
              <div class="skill-card-head">
                <n-icon :size="14" :component="ExtensionPuzzleOutline" class="skill-card-icon" />
                <span class="skill-card-name">{{ s.name }}</span>
                <n-tag size="tiny" round :bordered="false">{{ t('agentSettings.skillBuiltin') }}</n-tag>
              </div>
              <div class="skill-card-desc">{{ s.desc }}</div>
            </div>
          </div>
        </n-scrollbar>
      </div>

      <!-- 底部操作: 已移除全局取消/保存,各 Tab 自治保存(MCP tab 有独立保存;技能库只读) -->
      </div>
    </div>
  </n-modal>

  <!-- 模型配置编辑弹窗(新建/编辑共用,独立于列表) -->
  <n-modal
    v-model:show="editorShow"
    :title="editorIsNew ? t('agentSettings.editorNew') : t('agentSettings.editorEdit')"
    preset="dialog"
    :show-icon="false"
    style="width: 480px"
    :mask-closable="false"
  >
    <div v-if="editorDraft" class="editor-form">
      <div class="cfg-row">
        <div class="cfg-main">
          <div class="cfg-label">{{ t('agentSettings.profileName') }}</div>
        </div>
        <n-input v-model:value="editorDraft.name" size="small" style="width: 200px" :placeholder="t('agentSettings.profileNamePh')" />
      </div>
      <div class="cfg-row" style="margin-top: 8px">
        <div class="cfg-main">
          <div class="cfg-label">{{ t('agentSettings.provider') }}</div>
          <div v-if="currentPreset?.note" class="cfg-desc note">{{ currentPreset.note }}</div>
        </div>
        <n-select :value="editorDraft.provider" :options="providerOptions" size="small" style="width: 200px" @update:value="handleProviderChange" />
      </div>
      <div class="cfg-row" style="margin-top: 8px">
        <div class="cfg-main">
          <div class="cfg-label">{{ t('agentSettings.baseUrl') }}</div>
          <div class="cfg-desc">{{ t('agentSettings.baseUrlDesc') }}</div>
        </div>
      </div>
      <n-input v-model:value="editorDraft.baseURL" size="small" placeholder="https://api.example.com/v1" class="cfg-input" />
      <div class="cfg-row" style="margin-top: 10px">
        <div class="cfg-main">
          <div class="cfg-label">{{ t('agentSettings.apiKey') }}</div>
          <div class="cfg-desc">
            {{ editorDraft.hasKey ? t('agentSettings.keyStored') : t('agentSettings.keyEmpty') }}
          </div>
        </div>
      </div>
      <n-input
        v-model:value="editorDraft.apiKeyInput"
        size="small"
        type="password"
        show-password-on="click"
        :placeholder="editorDraft.hasKey ? t('agentSettings.keyKeepHint') : 'sk-…'"
        class="cfg-input"
      />
      <div class="cfg-row" style="margin-top: 10px">
        <div class="cfg-main">
          <div class="cfg-label">{{ t('agentSettings.model') }}</div>
          <div class="cfg-desc">{{ t('agentSettings.modelDesc') }}</div>
        </div>
        <n-button size="tiny" quaternary :loading="fetchingModels" @click="fetchRemoteModels">
          <template #icon><n-icon :size="12" :component="RefreshOutline" /></template>
          {{ t('agentSettings.fetchModels') }}
        </n-button>
      </div>
      <n-select
        v-model:value="editorDraft.model"
        size="small"
        filterable
        tag
        :options="modelOptions"
        :placeholder="t('agentSettings.modelPlaceholder')"
        class="cfg-input"
      />
      <div class="cfg-row" style="margin-top: 10px">
        <div class="cfg-main">
          <div class="cfg-label">{{ t('agentSettings.customModels') }}</div>
          <div class="cfg-desc">{{ t('agentSettings.customModelsDesc') }}</div>
        </div>
      </div>
      <n-dynamic-tags v-model:value="editorDraft.customModels" size="small" class="cfg-input" />
      <div class="cfg-row" style="margin-top: 10px">
        <div class="cfg-main">
          <div class="cfg-label">{{ t('agentSettings.contextWindow') }}</div>
          <div class="cfg-desc">{{ t('agentSettings.contextWindowDesc') }}</div>
        </div>
      </div>
      <n-input-number
        v-model:value="editorDraft.contextWindow"
        size="small"
        :min="0"
        :max="10000000"
        :step="1000"
        :placeholder="t('agentSettings.contextWindowPlaceholder')"
        class="cfg-input"
      />
      <div class="cfg-row" style="margin-top: 10px">
        <div class="cfg-main">
          <div class="cfg-label">{{ t('agentSettings.apiMode') }}</div>
          <div class="cfg-desc">{{ t('agentSettings.apiModeDesc') }}</div>
        </div>
        <n-radio-group v-model:value="editorDraft.apiMode" size="small" name="api-mode">
          <n-radio-button value="chat">Chat Completions</n-radio-button>
          <n-radio-button value="responses">Responses</n-radio-button>
        </n-radio-group>
      </div>
      <div class="editor-actions">
        <n-button size="small" :loading="testing" @click="handleTest">
          <template #icon><n-icon :size="13" :component="CheckmarkCircleOutline" /></template>
          {{ t('agentSettings.testConn') }}
        </n-button>
        <div class="as-actions-right">
          <n-button size="small" @click="editorShow = false">{{ t('common.cancel') }}</n-button>
          <n-button size="small" type="primary" @click="editorConfirm">{{ t('common.confirm') }}</n-button>
        </div>
      </div>
    </div>
  </n-modal>

  <!-- 运行时诊断结果 -->
  <n-modal v-model:show="diagShow" :title="t('agentSettings.diagTitle')" preset="dialog" :show-icon="false" style="width: 460px">
    <div class="diag-list">
      <div v-for="(r, i) in diagRows" :key="i" class="diag-row" :class="{ bad: r.bad }">
        <span class="diag-k">{{ r.k }}</span>
        <span class="diag-v">{{ r.v }}</span>
      </div>
    </div>
  </n-modal>
</template>

<style scoped>
/* 诊断结果列表 */
.diag-list { display: flex; flex-direction: column; gap: 6px; margin: 4px 0; }
.diag-row { display: flex; justify-content: space-between; gap: 12px; font-size: 12px; padding: 5px 8px; border-radius: 4px; background: rgba(128,128,128,.08); }
.diag-row.bad { background: rgba(239,68,68,.12); }
.diag-k { color: var(--n-text-color-3, #888); flex-shrink: 0; }
.diag-v { word-break: break-all; text-align: right; }
.diag-row.bad .diag-v { color: #ef4444; }

/* 左 Tab 右内容,固定高宽 */
.agent-settings { display: flex; flex-direction: row; height: 480px; margin: 0 -8px -8px; }
.as-tabs { display: flex; flex-direction: column; gap: 2px; width: 110px; flex-shrink: 0; border-right: 1px solid var(--border-color, #3c3c3c); padding: 10px 6px; }
.as-tab { display: flex; align-items: center; gap: 7px; padding: 9px 10px; font-size: 13px; color: var(--text-secondary, #888); cursor: pointer; border-radius: 4px; user-select: none; }
.as-tab:hover { color: var(--text-color, #d4d4d4); background: var(--hover-bg, rgba(255, 255, 255, 0.05)); }
.as-tab.active { color: #4ec9b0; background: rgba(78, 201, 176, 0.12); }

.as-main { flex: 1; min-width: 0; display: flex; flex-direction: column; }
.as-content { flex: 1; min-height: 0; display: flex; flex-direction: column; }
.as-pane { padding: 10px 12px 8px 14px; display: flex; flex-direction: column; }
.as-empty { padding: 24px; text-align: center; font-size: 12px; color: var(--text-secondary, #6e6e6e); }

/* 外接 MCP 服务器卡片 */
.mcp-item { border: 1px solid var(--border-color, #3c3c3c); border-radius: 6px; padding: 8px 10px; margin-bottom: 8px; display: flex; flex-direction: column; gap: 6px; }
.mcp-item.disabled { opacity: 0.55; }
.mcp-row { display: flex; align-items: center; gap: 8px; }
.mcp-fields { display: flex; flex-direction: column; gap: 5px; }
.mcp-test-result { font-size: 11px; color: var(--text-secondary, #999); word-break: break-all; line-height: 1.5; }

/* 各 Tab 就近保存按钮行 */
.as-pane-save { display: flex; justify-content: flex-end; padding-top: 10px; margin-top: 4px; border-top: 1px solid var(--border-color, #3c3c3c); }
.as-section-title { display: flex; align-items: center; justify-content: space-between; font-size: 13px; font-weight: 600; color: var(--text-color, #d4d4d4); margin-bottom: 8px; }

/* 档案列表 */
.profile-item { display: flex; align-items: center; gap: 8px; padding: 7px 10px; border: 1px solid var(--border-color, #3c3c3c); border-radius: 6px; margin-bottom: 6px; }
.profile-item:hover { background: var(--hover-bg, rgba(255, 255, 255, 0.04)); }
.profile-item.active { border-left: 3px solid #4ec9b0; }
.profile-main { flex: 1; min-width: 0; }
.profile-name { font-size: 13px; color: var(--text-color, #d4d4d4); display: flex; align-items: center; gap: 6px; }
.profile-sub { font-size: 11px; color: var(--text-secondary, #888); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; margin-top: 1px; }
.profile-key { color: #e2a03f; flex-shrink: 0; }

.cfg-row { display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-bottom: 4px; }
.cfg-main { min-width: 0; flex: 1; }
.cfg-label { font-size: 13px; color: var(--text-color, #d4d4d4); }
.cfg-desc { font-size: 11px; color: var(--text-secondary, #888); line-height: 1.5; margin-top: 2px; }
.cfg-desc.note { color: #e2a03f; }
.cfg-input { margin-top: 2px; }

/* 技能卡 */
.skill-card { border: 1px solid var(--border-color, #3c3c3c); border-radius: 6px; padding: 8px 10px; margin-bottom: 8px; }
.skill-card-head { display: flex; align-items: center; gap: 6px; }
.skill-card-icon { color: #4ec9b0; }
.skill-card-name { font-size: 13px; font-weight: 600; color: var(--text-color, #d4d4d4); flex: 1; }
.skill-card-desc { font-size: 11px; color: var(--text-secondary, #888); line-height: 1.6; margin-top: 4px; }

.as-actions { display: flex; align-items: center; justify-content: space-between; padding: 10px 12px 0 14px; border-top: 1px solid var(--border-color, #3c3c3c); flex-shrink: 0; }
.as-actions-right { display: flex; gap: 8px; }

/* 编辑弹窗表单 */
.editor-form { display: flex; flex-direction: column; }
.editor-actions { display: flex; align-items: center; justify-content: space-between; margin-top: 16px; padding-top: 10px; border-top: 1px solid var(--border-color, #3c3c3c); }
</style>
