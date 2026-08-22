<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { NIcon, NButton, NTag, NSwitch, NScrollbar, NRadioGroup, NRadioButton, NTooltip, NInputNumber, NSelect, NInput, NCheckbox, NModal, useMessage } from 'naive-ui'
import { CopyOutline, RefreshOutline, PauseOutline, PlayOutline, StopOutline, ShieldCheckmarkOutline, ListOutline, SettingsOutline, KeyOutline, AddOutline, TrashOutline, SaveOutline, DocumentTextOutline, ChevronDownOutline } from '@vicons/ionicons5'
import { useI18n } from 'vue-i18n'
import { useMcpBridge } from '../composables/useMcpBridge'
import { SetMcpEnabled, SetMcpMode, McpPause, McpResume, ResetMcpToken, ExportAuditPdf } from '../../bindings/changeme/internal/services/mcpservice.js'
import { McpCustomRules } from '../../bindings/changeme/internal/services/configservice.js'

const { t, locale } = useI18n()
const message = useMessage()
const { status, auditLog, pendingApprovals, approveApproval, denyApproval, refreshGrants, removeGrant, clearGrants, saveExecTuning, saveCustomRules } = useMcpBridge()

// 弹窗显隐(v-model:show,与智能体设置弹窗相互独立)
const show = defineModel<boolean>('show', { default: false })

const activeTab = ref<'settings' | 'approvals' | 'grants' | 'logs'>('settings')
const showToken = ref(false)
const switching = ref(false)

const stateText = computed(() => {
  switch (status.value.state) {
    case 'running': return t('mcp.stateRunning')
    case 'paused': return t('mcp.statePaused')
    default: return t('mcp.stateStopped')
  }
})
const stateType = computed(() => (status.value.state === 'running' ? 'success' : status.value.state === 'paused' ? 'warning' : 'default') as any)

// 倒序:最新日志在最上方
const reversedLogs = computed(() => [...auditLog.value].reverse())

// ==================== 日志过滤与导出 ====================

const riskFilter = ref<'all' | 'blocked' | 'confirm' | 'auto'>('all')
const sourceFilter = ref<'all' | 'external' | 'embedded' | 'system'>('all')
const logExpanded = ref<Record<string, boolean>>({})
const exportingPdf = ref(false)

const riskFilterOptions = computed(() => [
  { key: 'all', label: t('mcp.filterAll') },
  { key: 'blocked', label: t('mcp.riskBlocked') },
  { key: 'confirm', label: t('mcp.riskConfirm') },
  { key: 'auto', label: t('mcp.riskAuto') },
])
const sourceFilterOptions = computed(() => [
  { key: 'all', label: t('mcp.filterAll') },
  { key: 'external', label: t('mcp.sourceExternal') },
  { key: 'embedded', label: t('mcp.sourceEmbedded') },
  { key: 'system', label: t('mcp.sourceSystem') },
])

const filteredLogs = computed(() => reversedLogs.value.filter(log => {
  if (riskFilter.value !== 'all' && log.risk !== riskFilter.value) return false
  if (sourceFilter.value !== 'all' && log.source !== sourceFilter.value) return false
  return true
}))

function sourceText(source: string): string {
  switch (source) {
    case 'external': return t('mcp.sourceExternal')
    case 'embedded': return t('mcp.sourceEmbedded')
    case 'system': return t('mcp.sourceSystem')
    default: return source
  }
}

function toggleLogDetail(id: string) {
  logExpanded.value[id] = !logExpanded.value[id]
}

async function handleExportPdf() {
  exportingPdf.value = true
  try {
    const raw = JSON.parse(await ExportAuditPdf(locale.value))
    if (raw?.error) message.error(String(raw.error))
    else if (raw?.path) message.success(t('mcp.exportPdfOk', { path: raw.path }))
    // path 为空 = 用户取消,不提示
  } catch (e: any) {
    message.error(String(e?.message || e))
  } finally { exportingPdf.value = false }
}

function riskType(risk: string): any {
  switch (risk) {
    case 'blocked': return 'error'
    case 'confirm': return 'warning'
    default: return 'success'
  }
}
function riskText(risk: string): string {
  switch (risk) {
    case 'blocked': return t('mcp.riskBlocked')
    case 'confirm': return t('mcp.riskConfirm')
    default: return t('mcp.riskAuto')
  }
}
function decisionType(decision: string): any {
  switch (decision) {
    case 'approved': case 'auto': return 'success'
    case 'denied': return 'error'
    case 'pending': return 'warning'
    default: return 'default'
  }
}
function decisionText(decision: string): string {
  const key = 'mcp.decision_' + decision
  const val = t(key)
  return val === key ? decision : val
}

async function handleToggle(enabled: boolean) {
  switching.value = true
  try {
    const err = JSON.parse(await SetMcpEnabled(enabled) || 'null')
    if (err) message.error(String(err))
  } catch (e: any) {
    message.error(String(e?.message || e))
  } finally { switching.value = false }
}

async function handleMode(mode: string) {
  try {
    const err = JSON.parse(await SetMcpMode(mode) || 'null')
    if (err) message.error(String(err))
  } catch (e: any) {
    message.error(String(e?.message || e))
  }
}

async function handlePause() {
  try { JSON.parse(await McpPause() || 'null') } catch {}
}
async function handleResume() {
  try { JSON.parse(await McpResume() || 'null') } catch {}
}

async function handleResetToken() {
  try {
    const raw = JSON.parse(await ResetMcpToken() || 'null')
    if (raw) message.error(String(raw))
    else message.success(t('mcp.tokenResetOk'))
  } catch (e: any) { message.error(String(e?.message || e)) }
}

async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    message.success(t('mcp.copied'))
  } catch { message.error(t('mcp.copyFailed')) }
}

function maskedToken(token: string): string {
  if (!token) return ''
  if (showToken.value) return token
  if (token.length <= 8) return '****'
  return token.slice(0, 4) + '****' + token.slice(-4)
}

// ==================== 执行参数 ====================

// 本地编辑副本(从 status 同步,保存时整体提交)
const tuning = ref({ opDelayMs: 1000, batchIntervalMs: 300, grantsEnabled: true, auditRetentionDays: 30, terminalReadMaxKB: 32 })
const savingTuning = ref(false)

watch(() => [
  status.value.opDelayMs, status.value.batchIntervalMs, status.value.grantsEnabled,
  status.value.auditRetentionDays, status.value.terminalReadMax,
], () => {
  tuning.value = {
    opDelayMs: status.value.opDelayMs,
    batchIntervalMs: status.value.batchIntervalMs,
    grantsEnabled: status.value.grantsEnabled,
    auditRetentionDays: status.value.auditRetentionDays,
    terminalReadMaxKB: Math.round(status.value.terminalReadMax / 1024),
  }
}, { immediate: true })

async function handleSaveTuning() {
  savingTuning.value = true
  try {
    const raw = await saveExecTuning(
      tuning.value.opDelayMs, tuning.value.batchIntervalMs, tuning.value.grantsEnabled,
      tuning.value.auditRetentionDays, Math.round(tuning.value.terminalReadMaxKB * 1024),
    )
    const err = raw ? JSON.parse(raw) : null
    if (err) message.error(String(err))
    else message.success(t('mcp.tuningSaved'))
  } catch (e: any) { message.error(String(e?.message || e)) } finally { savingTuning.value = false }
}

// ==================== 自定义分级规则 ====================

interface RuleRow { pattern: string; risk: string; note: string }
const ruleRows = ref<RuleRow[]>([])
const rulesLoading = ref(false)
const savingRules = ref(false)

const riskOptions = [
  { label: t('mcp.riskBlocked'), value: 'blocked' },
  { label: t('mcp.riskConfirm'), value: 'confirm' },
  { label: t('mcp.riskAuto'), value: 'auto' },
]

async function loadRules() {
  rulesLoading.value = true
  try {
    const rules = await McpCustomRules()
    ruleRows.value = (Array.isArray(rules) ? rules : []).map((r: any) => ({
      pattern: String(r.pattern || ''), risk: String(r.risk || 'confirm'), note: String(r.note || ''),
    }))
  } catch { ruleRows.value = [] } finally { rulesLoading.value = false }
}

function addRuleRow() {
  if (ruleRows.value.length >= 100) return // 与后端上限一致
  ruleRows.value.push({ pattern: '', risk: 'confirm', note: '' })
}
function removeRuleRow(i: number) { ruleRows.value.splice(i, 1) }

async function handleSaveRules() {
  savingRules.value = true
  try {
    const payload = ruleRows.value.filter(r => r.pattern.trim())
    const raw = await saveCustomRules(JSON.stringify(payload))
    const err = raw ? JSON.parse(raw) : null
    if (err) message.error(String(err))
    else { message.success(t('mcp.rulesSaved')); loadRules() }
  } catch (e: any) { message.error(String(e?.message || e)) } finally { savingRules.value = false }
}

// ==================== 永久授权管理 ====================

const grants = ref<any[]>([])
const grantsLoading = ref(false)

async function loadGrants() {
  grantsLoading.value = true
  try { grants.value = await refreshGrants() } finally { grantsLoading.value = false }
}

async function handleRemoveGrant(id: string) {
  await removeGrant(id)
  message.success(t('mcp.grantRemoved'))
  loadGrants()
}
async function handleClearGrants() {
  await clearGrants()
  message.success(t('mcp.grantsCleared'))
  loadGrants()
}

// 打开授权/设置页时按需加载
watch(activeTab, tab => {
  if (tab === 'grants') loadGrants()
  if (tab === 'settings' && ruleRows.value.length === 0 && !rulesLoading.value) loadRules()
})

// 弹窗打开:回到设置页并按需加载(有待审批则直接定位审批页)
watch(show, open => {
  if (!open) return
  activeTab.value = pendingApprovals.value.length ? 'approvals' : 'settings'
  if (activeTab.value === 'settings' && ruleRows.value.length === 0 && !rulesLoading.value) loadRules()
})

// ==================== 审批(含永久授权勾选) ====================

// 每条待审批的永久授权勾选状态(id → checked)
const permanentChecks = ref<Record<string, boolean>>({})

function canPermanent(ap: any): boolean {
  return !!ap?.command && ap.risk === 'confirm'
}

function handleApprove(ap: any) {
  approveApproval(ap.id, !!permanentChecks.value[ap.id] && canPermanent(ap))
}

function handleDeny(id: string) {
  denyApproval(id)
}
</script>

<template>
  <n-modal v-model:show="show" :title="t('mcp.panelTitle')" preset="dialog" :show-icon="false" style="width: 740px" :mask-closable="false">
    <div class="mcp-panel">
      <div class="mcp-status-bar">
        <n-tag :type="stateType" size="small" round>{{ stateText }}</n-tag>
        <span class="mcp-url mono">{{ status.url || '--' }}</span>
      </div>
    <div class="mcp-layout">
      <!-- 左侧导航(左 Tab) -->
      <div class="mcp-side">
        <div class="rm-tab" :class="{ active: activeTab === 'settings' }" @click="activeTab = 'settings'">
          <n-icon :size="13" :component="SettingsOutline" />
          <span>{{ t('mcp.tabSettings') }}</span>
        </div>
        <div class="rm-tab" :class="{ active: activeTab === 'approvals' }" @click="activeTab = 'approvals'">
          <n-icon :size="13" :component="ShieldCheckmarkOutline" />
          <span>{{ t('mcp.tabApprovals') }}</span>
          <span v-if="pendingApprovals.length" class="rm-badge">{{ pendingApprovals.length }}</span>
        </div>
        <div class="rm-tab" :class="{ active: activeTab === 'grants' }" @click="activeTab = 'grants'">
          <n-icon :size="13" :component="KeyOutline" />
          <span>{{ t('mcp.tabGrants') }}</span>
        </div>
        <div class="rm-tab" :class="{ active: activeTab === 'logs' }" @click="activeTab = 'logs'">
          <n-icon :size="13" :component="ListOutline" />
          <span>{{ t('mcp.tabLogs') }}</span>
        </div>
      </div>

      <!-- 右侧内容 -->
      <div class="mcp-body">
      <!-- 设置 -->
      <div v-show="activeTab === 'settings'" class="mcp-pane">
        <n-scrollbar>
          <div class="mcp-section">
            <div class="mcp-row">
              <div class="mcp-row-main">
                <div class="mcp-label">{{ t('mcp.enable') }}</div>
                <div class="mcp-desc">{{ t('mcp.enableDesc') }}</div>
              </div>
              <n-switch :value="status.enabled" :loading="switching" size="small" @update:value="handleToggle" />
            </div>

            <div class="mcp-row">
              <div class="mcp-row-main">
                <div class="mcp-label">{{ t('mcp.serviceControl') }}</div>
                <div class="mcp-desc">{{ t('mcp.serviceControlDesc') }}</div>
              </div>
              <div class="mcp-btn-group">
                <n-tooltip trigger="hover" :delay="300">
                  <template #trigger>
                    <n-button size="tiny" :type="status.state === 'paused' ? 'primary' : 'default'" :disabled="status.state === 'stopped'" @click="status.state === 'paused' ? handleResume() : handlePause()">
                      <template #icon><n-icon :size="12" :component="status.state === 'paused' ? PlayOutline : PauseOutline" /></template>
                    </n-button>
                  </template>
                  {{ status.state === 'paused' ? t('mcp.resume') : t('mcp.pause') }}
                </n-tooltip>
                <n-tooltip trigger="hover" :delay="300">
                  <template #trigger>
                    <n-button size="tiny" type="error" quaternary :disabled="status.state === 'stopped'" @click="handleToggle(false)">
                      <template #icon><n-icon :size="12" :component="StopOutline" /></template>
                    </n-button>
                  </template>
                  {{ t('mcp.stop') }}
                </n-tooltip>
              </div>
            </div>
          </div>

          <div class="mcp-section">
            <div class="mcp-section-title">{{ t('mcp.approvalMode') }}</div>
            <n-radio-group :value="status.mode" size="small" style="width: 100%" @update:value="handleMode">
              <n-radio-button value="manual" style="flex: 1">{{ t('mcp.modeManual') }}</n-radio-button>
              <n-radio-button value="auto" style="flex: 1">{{ t('mcp.modeAuto') }}</n-radio-button>
            </n-radio-group>
            <div class="mcp-desc" style="margin-top: 6px">
              {{ status.mode === 'auto' ? t('mcp.modeAutoDesc') : t('mcp.modeManualDesc') }}
            </div>
          </div>

          <div class="mcp-section">
            <div class="mcp-section-title">{{ t('mcp.connection') }}</div>
            <div class="mcp-conn-row">
              <span class="mcp-conn-label">URL</span>
              <span class="mcp-conn-value">{{ status.url || '--' }}</span>
              <n-button text size="tiny" @click="copyText(status.url)"><n-icon :size="13" :component="CopyOutline" /></n-button>
            </div>
            <div class="mcp-conn-row">
              <span class="mcp-conn-label">{{ t('mcp.tokenLabel') }}</span>
              <span class="mcp-conn-value mono">{{ maskedToken(status.token) || '--' }}</span>
              <n-button text size="tiny" @click="showToken = !showToken">{{ showToken ? t('mcp.hide') : t('mcp.show') }}</n-button>
              <n-button text size="tiny" @click="copyText(status.token)"><n-icon :size="13" :component="CopyOutline" /></n-button>
              <n-button text size="tiny" :title="t('mcp.resetToken')" @click="handleResetToken"><n-icon :size="13" :component="RefreshOutline" /></n-button>
            </div>
            <div class="mcp-desc" style="margin-top: 6px">{{ t('mcp.connDesc') }}</div>
          </div>

          <div class="mcp-section">
            <div class="mcp-section-title">{{ t('mcp.riskTitle') }}</div>
            <div class="mcp-risk-item"><span class="dot dot-red" /><span>{{ t('mcp.riskBlockedDesc') }}</span></div>
            <div class="mcp-risk-item"><span class="dot dot-orange" /><span>{{ t('mcp.riskConfirmDesc') }}</span></div>
            <div class="mcp-risk-item"><span class="dot dot-green" /><span>{{ t('mcp.riskAutoDesc') }}</span></div>
          </div>

          <!-- 执行参数 -->
          <div class="mcp-section">
            <div class="mcp-section-title">{{ t('mcp.execTuning') }}</div>
            <div class="mcp-tune-row">
              <div class="mcp-tune-main">
                <div class="mcp-label">{{ t('mcp.opDelay') }}</div>
                <div class="mcp-desc">{{ t('mcp.opDelayDesc') }}</div>
              </div>
              <n-input-number v-model:value="tuning.opDelayMs" size="tiny" :min="0" :max="10000" :step="100" style="width: 96px; flex-shrink: 0" />
            </div>
            <div class="mcp-tune-row">
              <div class="mcp-tune-main">
                <div class="mcp-label">{{ t('mcp.batchInterval') }}</div>
                <div class="mcp-desc">{{ t('mcp.batchIntervalDesc') }}</div>
              </div>
              <n-input-number v-model:value="tuning.batchIntervalMs" size="tiny" :min="50" :max="10000" :step="50" style="width: 96px; flex-shrink: 0" />
            </div>
            <div class="mcp-tune-row">
              <div class="mcp-tune-main">
                <div class="mcp-label">{{ t('mcp.auditRetention') }}</div>
                <div class="mcp-desc">{{ t('mcp.auditRetentionDesc') }}</div>
              </div>
              <n-input-number v-model:value="tuning.auditRetentionDays" size="tiny" :min="1" :max="365" style="width: 96px; flex-shrink: 0" />
            </div>
            <div class="mcp-tune-row">
              <div class="mcp-tune-main">
                <div class="mcp-label">{{ t('mcp.terminalReadMax') }}</div>
                <div class="mcp-desc">{{ t('mcp.terminalReadMaxDesc') }}</div>
              </div>
              <n-input-number v-model:value="tuning.terminalReadMaxKB" size="tiny" :min="1" :max="256" style="width: 96px; flex-shrink: 0" />
            </div>
            <div class="mcp-row">
              <div class="mcp-row-main">
                <div class="mcp-label">{{ t('mcp.grantsSwitch') }}</div>
                <div class="mcp-desc">{{ t('mcp.grantsSwitchDesc') }}</div>
              </div>
              <n-switch v-model:value="tuning.grantsEnabled" size="small" />
            </div>
            <n-button size="tiny" type="primary" :loading="savingTuning" style="margin-top: 8px" @click="handleSaveTuning">
              <template #icon><n-icon :size="12" :component="SaveOutline" /></template>
              {{ t('mcp.saveTuning') }}
            </n-button>
          </div>

          <!-- 自定义分级规则 -->
          <div class="mcp-section">
            <div class="mcp-section-title">{{ t('mcp.customRules') }}</div>
            <div class="mcp-desc" style="margin-bottom: 8px">{{ t('mcp.customRulesDesc') }}</div>
            <div v-if="ruleRows.length === 0" class="mcp-desc">{{ t('mcp.rulesEmpty') }}</div>
            <div v-for="(row, i) in ruleRows" :key="i" class="mcp-rule-row">
              <n-select v-model:value="row.risk" size="tiny" :options="riskOptions" style="width: 86px; flex-shrink: 0" />
              <n-input v-model:value="row.pattern" size="tiny" :placeholder="t('mcp.rulePatternPlaceholder')" />
              <n-input v-model:value="row.note" size="tiny" :placeholder="t('mcp.ruleNotePlaceholder')" style="width: 72px; flex-shrink: 0" />
              <n-button text size="tiny" type="error" @click="removeRuleRow(i)"><n-icon :size="13" :component="TrashOutline" /></n-button>
            </div>
            <div class="mcp-btn-group" style="margin-top: 8px">
              <n-button size="tiny" dashed @click="addRuleRow">
                <template #icon><n-icon :size="12" :component="AddOutline" /></template>
                {{ t('mcp.addRule') }}
              </n-button>
              <n-button size="tiny" type="primary" :loading="savingRules" :disabled="ruleRows.length === 0" @click="handleSaveRules">
                <template #icon><n-icon :size="12" :component="SaveOutline" /></template>
                {{ t('mcp.saveTuning') }}
              </n-button>
            </div>
          </div>
        </n-scrollbar>
      </div>

      <!-- 待审批 -->
      <div v-show="activeTab === 'approvals'" class="mcp-pane">
        <n-scrollbar>
          <div v-if="pendingApprovals.length === 0" class="mcp-empty">{{ t('mcp.noApprovals') }}</div>
          <div v-for="ap in pendingApprovals" :key="ap.id" class="mcp-approval-card">
            <div class="mcp-approval-head">
              <n-tag :type="riskType(ap.risk)" size="small">{{ riskText(ap.risk) }}</n-tag>
              <span class="mcp-approval-action">{{ ap.action }}</span>
            </div>
            <div class="mcp-approval-summary">{{ ap.summary }}</div>
            <pre class="mcp-approval-detail">{{ ap.detail }}</pre>
            <div v-if="canPermanent(ap) && status.grantsEnabled" class="mcp-approval-perm">
              <n-checkbox v-model:checked="permanentChecks[ap.id]" size="small">
                {{ t('mcp.permanentGrant') }}
              </n-checkbox>
              <div class="mcp-desc">{{ t('mcp.grantHint') }}</div>
            </div>
            <div class="mcp-approval-foot">
              <span class="mcp-approval-exp">{{ t('mcp.expiresAt') }}: {{ ap.expiresAt }}</span>
              <div class="mcp-btn-group">
                <n-button size="tiny" type="error" @click="handleDeny(ap.id)">{{ t('mcp.deny') }}</n-button>
                <n-button size="tiny" type="primary" @click="handleApprove(ap)">{{ t('mcp.approve') }}</n-button>
              </div>
            </div>
          </div>
        </n-scrollbar>
      </div>

      <!-- 永久授权 -->
      <div v-show="activeTab === 'grants'" class="mcp-pane">
        <n-scrollbar>
          <div v-if="grants.length === 0" class="mcp-empty">{{ t('mcp.noGrants') }}</div>
          <template v-else>
            <div class="mcp-grants-toolbar">
              <span class="mcp-desc">{{ grants.length }} {{ t('mcp.tabGrants') }}</span>
              <n-button size="tiny" type="error" quaternary @click="handleClearGrants">{{ t('mcp.clearGrants') }}</n-button>
            </div>
            <div v-for="g in grants" :key="g.id" class="mcp-grant-card">
              <div class="mcp-grant-head">
                <span class="mcp-grant-cmd mono">{{ g.command }}</span>
                <n-button text size="tiny" type="error" @click="handleRemoveGrant(g.id)"><n-icon :size="13" :component="TrashOutline" /></n-button>
              </div>
              <div v-if="g.paths && g.paths.length" class="mcp-grant-paths mono">{{ g.paths.join(', ') }}</div>
              <div class="mcp-approval-exp">{{ g.createdAt }}</div>
            </div>
          </template>
        </n-scrollbar>
      </div>

      <!-- 日志 -->
      <div v-show="activeTab === 'logs'" class="mcp-pane">
        <!-- 过滤工具栏:风险等级 + 来源(按钮切换) + 导出 -->
        <div class="mcp-log-toolbar">
          <div class="mcp-log-filter-group">
            <button
              v-for="opt in riskFilterOptions" :key="'r-' + opt.key"
              class="mcp-filter-btn"
              :class="{ active: riskFilter === opt.key, [`risk-${opt.key}`]: opt.key !== 'all' }"
              @click="riskFilter = opt.key as any"
            >{{ opt.label }}</button>
          </div>
          <div class="mcp-log-filter-group">
            <button
              v-for="opt in sourceFilterOptions" :key="'s-' + opt.key"
              class="mcp-filter-btn"
              :class="{ active: sourceFilter === opt.key }"
              @click="sourceFilter = opt.key as any"
            >{{ opt.label }}</button>
          </div>
          <n-tooltip trigger="hover" :delay="300">
            <template #trigger>
              <n-button size="tiny" quaternary :loading="exportingPdf" @click="handleExportPdf">
                <template #icon><n-icon :size="12" :component="DocumentTextOutline" /></template>
                PDF
              </n-button>
            </template>
            {{ t('mcp.exportPdf') }}
          </n-tooltip>
        </div>
        <n-scrollbar>
          <div v-if="filteredLogs.length === 0" class="mcp-empty">{{ reversedLogs.length === 0 ? t('mcp.noLogs') : t('mcp.noFilterMatch') }}</div>
          <div
            v-for="log in filteredLogs" :key="log.id"
            class="mcp-log-item"
            :class="{ clickable: !!log.detail }"
            @click="log.detail && toggleLogDetail(log.id)"
          >
            <div class="mcp-log-line1">
              <span class="mcp-log-ts">{{ log.ts }}</span>
              <n-tag v-if="log.risk !== '-'" :type="riskType(log.risk)" size="tiny" :bordered="false" round>{{ riskText(log.risk) }}</n-tag>
              <n-tag :type="decisionType(log.decision)" size="tiny" :bordered="false" round>{{ decisionText(log.decision) }}</n-tag>
              <span class="mcp-log-source">{{ sourceText(log.source) }}</span>
              <n-tag v-if="log.batchId" size="tiny" type="info" :bordered="false" round>{{ t('mcp.batchTag') }}</n-tag>
              <span class="mcp-log-action mono">{{ log.action }}</span>
              <n-icon v-if="log.detail" :size="11" class="mcp-log-chev" :class="{ expanded: logExpanded[log.id] }" :component="ChevronDownOutline" />
            </div>
            <div v-if="log.subject" class="mcp-log-subject mono">{{ log.subject }}</div>
            <pre v-if="log.detail && logExpanded[log.id]" class="mcp-log-detail mono">{{ log.detail }}</pre>
          </div>
        </n-scrollbar>
      </div>
    </div>
    </div>
  </div>
    </n-modal>
</template>

<style scoped>
.mcp-panel { display: flex; flex-direction: column; overflow: hidden; margin: 0 -8px -8px; }
.mcp-status-bar { display: flex; align-items: center; gap: 8px; padding: 0 4px 8px; flex-shrink: 0; }
.mcp-url { font-size: 11px; color: var(--text-secondary, #888); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.mcp-layout { display: flex; height: 60vh; min-height: 380px; border-top: 1px solid var(--border-color, #3c3c3c); }
.mcp-side { display: flex; flex-direction: column; gap: 2px; width: 106px; flex-shrink: 0; border-right: 1px solid var(--border-color, #3c3c3c); padding: 10px 6px; }
.rm-tab { display: flex; align-items: center; gap: 6px; padding: 7px 8px; font-size: 12px; color: var(--text-secondary, #888); cursor: pointer; border-radius: 4px; transition: background 0.15s, color 0.15s; user-select: none; position: relative; }
.rm-tab:hover { color: var(--text-color, #d4d4d4); background: var(--hover-bg, rgba(255, 255, 255, 0.05)); }
.rm-tab.active { color: #4ec9b0; background: rgba(78, 201, 176, 0.12); }
.rm-badge { position: absolute; top: 4px; right: 6px; min-width: 14px; height: 14px; padding: 0 3px; border-radius: 7px; background: #e45858; color: #fff; font-size: 10px; line-height: 14px; text-align: center; }
.mcp-body { flex: 1; min-width: 0; display: flex; flex-direction: column; overflow: hidden; }
.mcp-pane { flex: 1; min-height: 0; display: flex; flex-direction: column; overflow: hidden; }

.mcp-section { padding: 10px 12px; border-bottom: 1px solid var(--sidebar-shadow, #2a2a2a); }
.mcp-section-title { font-size: 11px; font-weight: 600; color: var(--text-secondary, #888); text-transform: uppercase; letter-spacing: 0.5px; margin-bottom: 8px; }
.mcp-row { display: flex; align-items: center; justify-content: space-between; gap: 8px; padding: 4px 0; }
.mcp-row-main { min-width: 0; }
.mcp-label { font-size: 13px; color: var(--text-color, #d4d4d4); }
.mcp-desc { font-size: 11px; color: var(--text-secondary, #888); line-height: 1.5; margin-top: 2px; }
.mcp-btn-group { display: flex; gap: 4px; flex-shrink: 0; }

.mcp-conn-row { display: flex; align-items: center; gap: 6px; padding: 3px 0; }
.mcp-conn-label { font-size: 11px; color: var(--text-secondary, #888); flex-shrink: 0; width: 40px; }
.mcp-conn-value { font-size: 12px; color: var(--text-color, #d4d4d4); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex: 1; min-width: 0; }
.mono { font-family: Consolas, 'Courier New', monospace; }

.mcp-risk-item { display: flex; align-items: center; gap: 8px; font-size: 11px; color: var(--text-color, #d4d4d4); line-height: 1.6; }
.dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
.dot-red { background: #e45858; }
.dot-orange { background: #e2a03f; }
.dot-green { background: #4ec9b0; }

.mcp-empty { padding: 24px 12px; text-align: center; font-size: 12px; color: var(--text-secondary, #6e6e6e); }

.mcp-approval-card { margin: 8px 10px; padding: 10px; border: 1px solid var(--border-color, #3c3c3c); border-radius: 6px; background: var(--hover-bg, rgba(255, 255, 255, 0.03)); }
.mcp-approval-head { display: flex; align-items: center; gap: 8px; }
.mcp-approval-action { font-size: 12px; font-weight: 600; color: var(--text-color, #d4d4d4); }
.mcp-approval-summary { font-size: 12px; color: var(--text-color, #d4d4d4); margin-top: 6px; }
.mcp-approval-detail { margin: 6px 0 0; padding: 6px 8px; max-height: 120px; overflow: auto; font-size: 11px; font-family: Consolas, 'Courier New', monospace; color: var(--text-color, #d4d4d4); background: rgba(0, 0, 0, 0.3); border-radius: 4px; white-space: pre-wrap; word-break: break-all; }
.mcp-approval-foot { display: flex; align-items: center; justify-content: space-between; margin-top: 8px; }
.mcp-approval-exp { font-size: 10px; color: var(--text-secondary, #6e6e6e); }

/* 日志过滤工具栏 */
.mcp-log-toolbar { display: flex; align-items: center; gap: 8px; padding: 6px 10px; border-bottom: 1px solid var(--sidebar-shadow, #2a2a2a); flex-shrink: 0; flex-wrap: wrap; }
.mcp-log-filter-group { display: flex; align-items: center; background: var(--hover-bg, rgba(255, 255, 255, 0.04)); border-radius: 6px; padding: 1px; gap: 1px; }
.mcp-filter-btn { border: none; background: transparent; color: var(--text-secondary, #888); font-size: 10.5px; padding: 2px 8px; border-radius: 5px; cursor: pointer; transition: background 0.15s, color 0.15s; white-space: nowrap; }
.mcp-filter-btn:hover { color: var(--text-color, #d4d4d4); }
.mcp-filter-btn.active { background: rgba(0, 120, 212, 0.25); color: #4da3ff; }
.mcp-filter-btn.active.risk-blocked { background: rgba(228, 88, 88, 0.22); color: #e45858; }
.mcp-filter-btn.active.risk-confirm { background: rgba(226, 160, 63, 0.22); color: #e2a03f; }
.mcp-filter-btn.active.risk-auto { background: rgba(78, 201, 176, 0.2); color: #4ec9b0; }

.mcp-log-item { padding: 6px 12px; border-bottom: 1px solid var(--sidebar-shadow, #222); }
.mcp-log-item.clickable { cursor: pointer; }
.mcp-log-item.clickable:hover { background: var(--hover-bg, rgba(255, 255, 255, 0.03)); }
.mcp-log-line1 { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.mcp-log-ts { font-size: 10px; color: var(--text-secondary, #6e6e6e); font-family: Consolas, 'Courier New', monospace; }
.mcp-log-source { font-size: 10px; color: var(--text-secondary, #888); }
.mcp-log-action { font-size: 11px; font-weight: 600; color: var(--text-color, #d4d4d4); }
.mcp-log-subject { font-size: 11px; color: var(--text-secondary, #999); margin-top: 2px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: Consolas, 'Courier New', monospace; }
.mcp-log-chev { color: var(--text-secondary, #6e6e6e); transition: transform 0.15s; margin-left: auto; flex-shrink: 0; }
.mcp-log-chev.expanded { transform: rotate(180deg); }
.mcp-log-detail { margin: 4px 0 0; padding: 6px 8px; background: rgba(0, 0, 0, 0.3); border-radius: 4px; font-size: 10.5px; line-height: 1.6; color: var(--text-color, #ccc); white-space: pre-wrap; word-break: break-all; max-height: 180px; overflow: auto; font-family: Consolas, 'Courier New', monospace; }

/* 执行参数 */
.mcp-tune-row { display: flex; align-items: center; justify-content: space-between; gap: 10px; padding: 5px 0; }
.mcp-tune-main { min-width: 0; flex: 1; }

/* 自定义规则 */
.mcp-rule-row { display: flex; align-items: center; gap: 4px; margin-bottom: 4px; }

/* 审批永久授权勾选 */
.mcp-approval-perm { margin-top: 8px; padding: 6px 8px; border: 1px dashed var(--border-color, #3c3c3c); border-radius: 4px; }
.mcp-approval-perm .mcp-desc { margin-top: 2px; }

/* 永久授权管理 */
.mcp-grants-toolbar { display: flex; align-items: center; justify-content: space-between; padding: 8px 12px 4px; }
.mcp-grant-card { margin: 6px 10px; padding: 8px 10px; border: 1px solid var(--border-color, #3c3c3c); border-radius: 6px; background: var(--hover-bg, rgba(255, 255, 255, 0.03)); }
.mcp-grant-head { display: flex; align-items: center; justify-content: space-between; gap: 6px; }
.mcp-grant-cmd { font-size: 11px; color: var(--text-color, #d4d4d4); word-break: break-all; }
.mcp-grant-paths { font-size: 10px; color: var(--text-secondary, #888); margin-top: 4px; word-break: break-all; }
</style>
