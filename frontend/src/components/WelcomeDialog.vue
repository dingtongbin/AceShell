<script setup lang="ts">
// 首次使用引导向导: 欢迎 → 选择语言(原生文字) → 是否开启智能助手(默认关)。
// 完成后 SetOnboarded(true) 持久化,不再弹出。
// 布局契约: 面板固定宽高,内容区滚动包裹;语言选项使用虚拟滚动列表(NVirtualList)。
import { ref, computed } from 'vue'
import { NModal, NButton, NIcon, NScrollbar, NVirtualList, useMessage } from 'naive-ui'
import { CheckmarkCircleOutline, LanguageOutline, HardwareChipOutline } from '@vicons/ionicons5'
import { useI18n } from 'vue-i18n'
import { SetOnboarded, SetLanguage, SetShowAssistant } from '../../bindings/changeme/internal/services/configservice.js'

const emit = defineEmits<{ (e: 'done', assistant: boolean): void }>()

const show = defineModel<boolean>('show', { default: false })

const { t, locale } = useI18n()
const message = useMessage()

const step = ref(1)
const totalSteps = 2

// 语言选项: 用各语言的原生描述展示(不翻译);经虚拟滚动列表渲染
const langs = [
  { code: 'zh-CN', label: '中文（简体）' },
  { code: 'en-US', label: 'English' },
]
const selectedLang = ref(locale.value || 'zh-CN')

const enableAssistant = ref(false) // 默认不开启
const saving = ref(false)

function pickLang(code: string) {
  selectedLang.value = code
  // 即时生效: 向导内文案跟随所选语言切换
  locale.value = code as any
}

const stepTitle = computed(() => (step.value === 1 ? t('welcome.langTitle') : t('welcome.assistantTitle')))

async function finish() {
  saving.value = true
  try {
    await SetLanguage(selectedLang.value)
    const res = JSON.parse(await SetShowAssistant(enableAssistant.value))
    if (res?.error) throw new Error(String(res.error))
    await SetOnboarded(true)
    emit('done', enableAssistant.value)
    show.value = false
  } catch (e: any) {
    message.error(String(e?.message || e))
    // 失败时仍标记完成,避免坏状态导致每次启动都卡在向导
    await SetOnboarded(true).catch(() => {})
    show.value = false
  } finally { saving.value = false }
}
</script>

<template>
  <n-modal
    v-model:show="show" preset="card" :show-icon="false" :closable="false" :mask-closable="false"
    style="width: 560px; max-width: 94vw"
    content-style="height: 470px; box-sizing: border-box;"
  >
    <div class="wl-wrap">
      <div class="wl-head">
        <div class="wl-app-name">AceShell</div>
        <div class="wl-sub">{{ t('welcome.subtitle') }}</div>
      </div>

      <div class="wl-step-ind">{{ t('welcome.step', { n: step, total: totalSteps }) }} · {{ stepTitle }}</div>

      <!-- 内容区: 固定高度内滚动 -->
      <div class="wl-body">
        <!-- 第一步: 语言(虚拟滚动列表) -->
        <n-virtual-list v-if="step === 1" :items="langs" :item-size="48" key-field="code" style="height: 100%">
          <template #default="{ item }">
            <button
              class="wl-lang-item" :class="{ active: selectedLang === item.code }"
              @click="pickLang(item.code)"
            >
              <n-icon v-if="selectedLang === item.code" :size="15" :component="CheckmarkCircleOutline" />
              <span>{{ item.label }}</span>
            </button>
          </template>
        </n-virtual-list>

        <!-- 第二步: 智能助手(滚动条包裹) -->
        <n-scrollbar v-else style="height: 100%">
          <div class="wl-section-head">
            <n-icon :size="15" :component="HardwareChipOutline" />
            <span>{{ t('welcome.assistantTitle') }}</span>
          </div>
          <button class="wl-assist-card" :class="{ active: enableAssistant }" @click="enableAssistant = !enableAssistant">
            <div class="wl-assist-check"><n-icon v-if="enableAssistant" :size="16" :component="CheckmarkCircleOutline" /></div>
            <div>
              <div class="wl-assist-name">{{ enableAssistant ? '✓ ' + t('settings.assistant') : t('settings.assistant') }}</div>
              <div class="wl-assist-desc">{{ t('welcome.assistantDesc') }}</div>
            </div>
          </button>
          <div class="cfg-desc" style="margin-top:6px">{{ t('agentSettings.enableDesc') }}</div>
        </n-scrollbar>
      </div>

      <div class="wl-actions">
        <n-button v-if="step > 1" size="small" @click="step--">{{ t('welcome.back') }}</n-button>
        <div style="flex:1" />
        <n-button v-if="step < totalSteps" size="small" type="primary" @click="step++">{{ t('welcome.next') }}</n-button>
        <n-button v-else size="small" type="primary" :loading="saving" @click="finish">{{ t('welcome.done') }}</n-button>
      </div>
    </div>
  </n-modal>
</template>

<style scoped>
.wl-wrap { height: 100%; display: flex; flex-direction: column; gap: 10px; }
.wl-head { display: flex; flex-direction: column; align-items: center; gap: 6px; text-align: center; }
.wl-app-name { font-size: 24px; font-weight: 700; letter-spacing: 1px; color: var(--text-color, #d4d4d4); }
.wl-sub { font-size: 12px; line-height: 1.55; color: var(--text-secondary, #999); }
.wl-step-ind { font-size: 11px; color: var(--icon-color, #888); letter-spacing: .3px; }
.wl-body { flex: 1; min-height: 0; }

.wl-section-head { display: flex; align-items: center; gap: 6px; font-size: 13px; font-weight: 600; color: var(--text-color, #d4d4d4); margin-bottom: 8px; }

/* 虚拟滚动列表项 */
.wl-lang-item { display: flex; align-items: center; justify-content: center; gap: 7px; width: 100%; height: 40px; margin-bottom: 8px; border-radius: 8px; border: 1px solid var(--border-color, #454545); background: transparent; color: var(--text-color, #d4d4d4); font-size: 14px; cursor: pointer; transition: all .15s; }
.wl-lang-item:hover { border-color: var(--primary-color, #0078d4); }
.wl-lang-item.active { border-color: var(--primary-color, #0078d4); background: rgba(0, 120, 212, 0.1); color: var(--primary-color, #4aa3ff); font-weight: 600; }
.wl-lang-item .n-icon { color: var(--primary-color, #0078d4); }

.wl-assist-card { display: flex; align-items: flex-start; gap: 10px; width: 100%; text-align: left; padding: 12px; border-radius: 8px; border: 1px solid var(--border-color, #454545); background: transparent; cursor: pointer; transition: all .15s; }
.wl-assist-card:hover { border-color: var(--primary-color, #0078d4); }
.wl-assist-card.active { border-color: var(--primary-color, #0078d4); background: rgba(0, 120, 212, 0.08); }
.wl-assist-check { width: 18px; flex-shrink: 0; color: var(--primary-color, #0078d4); padding-top: 2px; }
.wl-assist-name { font-size: 13.5px; font-weight: 600; color: var(--text-color, #d4d4d4); }
.wl-assist-desc { font-size: 12px; line-height: 1.6; color: var(--text-secondary, #999); margin-top: 4px; }
.cfg-desc { font-size: 11.5px; color: var(--text-secondary, #999); }

.wl-actions { display: flex; align-items: center; gap: 8px; padding-top: 10px; border-top: 1px solid var(--border-color, #3c3c3c); }
</style>
