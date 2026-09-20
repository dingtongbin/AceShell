// 插件前端桥接器(模块级单例, 仿 useMcpBridge)。
// 职责:
//   1. 订阅后端插件事件(registry/open-tab/tab-updated/tab-closed/toast)
//   2. 按注册表动态 import 插件前端模块(/plugins/<id>/dist/entry.js, 同源)并缓存组件
//   3. 为插件组件构造注入 ctx(call/openTab/toast/主题快照)
//   4. 标签页类事件路由到 TabManager 绑定的处理器(bindPluginTabManager)
import { ref, markRaw, type Component } from 'vue'
import { Events } from '@wailsio/runtime'
import { PluginList } from '../../bindings/changeme/internal/services/pluginservice.js'
import { useTheme } from '../stores/theme'
import { diagLog } from './useDiag'

// ==================== 类型 ====================

export interface PluginViewInfo {
  id: string
  title: string
  icon: string
  componentId: string
}

export interface PluginSummary {
  id: string
  displayName: string
  version: string
  icon?: string
  accentColor?: string
  status: 'starting' | 'running' | 'stopped' | 'error' | 'disabled' | 'uninstalled'
  error?: string
  /** 是否随主程序捆绑内置 */
  bundled?: boolean
  views?: PluginViewInfo[]
}

/** 插件开签页参数(componentId 必须是 entry.js 导出 components 的键)。 */
export interface PluginTabSpec {
  tabKey: string
  title: string
  componentId: string
  props?: Record<string, any>
  icon?: string
  color?: string
}

/** 注入给插件组件的宿主上下文。 */
export interface PluginCtx {
  pluginID: string
  /** 调用插件 Go 侧业务方法(经宿主转发), JSON 进出。 */
  call: (method: string, args?: Record<string, any>) => Promise<any>
  /** 打开标签页(插件前端直达; tabKey 幂等, 重复打开即激活)。 */
  openTab: (spec: PluginTabSpec) => Promise<string>
  /** 宿主内 Toast。 */
  toast: (message: string, level?: 'info' | 'warning' | 'error' | 'success') => void
  /** 订阅插件 Go 侧推送的流式事件(EmitUIEvent 通道), 返回退订函数。 */
  onEvent: (cb: (payload: any) => void) => () => void
  /** 主题快照(响应式引用)。 */
  theme: { isDark: Readonly<import('vue').Ref<boolean>>; accent: Readonly<import('vue').Ref<string>> }
}

/** 插件注册表 → 左侧工具栏扁平视图项。 */
export interface PluginToolbarView {
  pluginID: string
  viewID: string
  title: string
  icon: string
  componentId: string
  accentColor?: string
}

// ==================== 状态 ====================

const started = ref(false)
const plugins = ref<PluginSummary[]>([])

// 组件缓存: `${pluginID}:${componentId}` → Component | null(加载失败)
const componentCache = new Map<string, Component | null>()
// 失败时间戳: 失败不永久缓存, 5s 后允许重试(面板重开/注册表刷新时)
const failedAt = new Map<string, number>()
// 加载失败原因: 同键 → 错误消息(面板展示, 便于定位)
const loadErrors = new Map<string, string>()

const LOAD_RETRY_MS = 5000
// 插件 ctx 缓存(同一插件保持稳定引用)
const ctxCache = new Map<string, PluginCtx>()
// 插件流式事件监听器: pluginID → 回调集合(EmitUIEvent 通道)
const eventListeners = new Map<string, Set<(payload: any) => void>>()

// TabManager 绑定的处理器(ShellPanel onMounted 时注入)
let tabApi: {
  openPluginTab: (payload: PluginOpenTabPayload) => Promise<void>
  closePluginTab: (pluginID: string, tabKey: string) => void
  setPluginTabTitle: (pluginID: string, tabKey: string, title: string) => void
} | null = null

// Toast 落点(ShellPanel 注入 useMessage)
let toastSink: ((message: string, level: string) => void) | null = null

export interface PluginOpenTabPayload {
  pluginID: string
  tabId: string
  tabKey: string
  title: string
  componentId: string
  props?: Record<string, any>
  icon?: string
  color?: string
}

/** 插件开签页的确定性 tabId(与后端 pluginTabID 一致)。 */
export function pluginTabID(pluginID: string, tabKey: string): string {
  return `plugin://${pluginID}/${tabKey}`
}

// ==================== 初始化 ====================

function parseEvt(evt: any): any {
  try { return JSON.parse(evt.data) } catch { return null }
}

/** initPluginBridge 应用启动时调用一次(幂等)。 */
export async function initPluginBridge() {
  if (started.value) return
  started.value = true

  try {
    const list = JSON.parse(await PluginList())
    plugins.value = Array.isArray(list?.plugins) ? list.plugins : []
  } catch {}

  Events.On('plugin-registry-changed', (evt: any) => {
    const data = parseEvt(evt)
    if (Array.isArray(data?.plugins)) {
      plugins.value = data.plugins
      void prefetchComponents()
    }
  })

  Events.On('plugin-open-tab', async (evt: any) => {
    const payload = parseEvt(evt) as PluginOpenTabPayload
    if (!payload?.pluginID || !payload.tabKey) return
    try {
      await tabApi?.openPluginTab(payload)
    } catch (e) {
      console.error('[plugin] open-tab failed', e)
      toastSink?.(String(payload.title || payload.pluginID), 'error')
    }
  })

  Events.On('plugin-tab-closed', (evt: any) => {
    const info = parseEvt(evt)
    if (!info?.pluginID || !info.tabKey) return
    tabApi?.closePluginTab(info.pluginID, info.tabKey)
  })

  Events.On('plugin-tab-updated', (evt: any) => {
    const info = parseEvt(evt)
    if (!info?.pluginID || !info.tabKey) return
    tabApi?.setPluginTabTitle(info.pluginID, info.tabKey, String(info.title ?? ''))
  })

  Events.On('plugin-toast', (evt: any) => {
    const info = parseEvt(evt)
    if (info?.message && toastSink) toastSink(String(info.message), String(info.level || 'info'))
  })

  // 插件流式事件(EmitUIEvent → 按插件分发)
  Events.On('plugin-event', (evt: any) => {
    const info = parseEvt(evt)
    if (!info?.pluginID) return
    const set = eventListeners.get(info.pluginID)
    if (!set) return
    for (const cb of set) {
      try { cb(info.payload) } catch (e) { console.error('[plugin] event handler error', e) }
    }
  })

  void prefetchComponents()
}

/** bindPluginTabManager ShellPanel onMounted 时注入 TabManager 能力。 */
export function bindPluginTabManager(api: NonNullable<typeof tabApi>) {
  tabApi = api
}

/** bindPluginToast 注入消息落点(useMessage 所在组件)。 */
export function bindPluginToast(sink: (message: string, level: string) => void) {
  toastSink = sink
}

// ==================== 组件加载 ====================

/** loadPluginComponent 动态加载并缓存插件组件; 失败缓存 null(5s 后可重试)。 */
export async function loadPluginComponent(pluginID: string, componentId: string): Promise<Component | null> {
  const key = `${pluginID}:${componentId}`
  const cached = componentCache.get(key)
  if (cached) return cached
  if (cached === null) {
    const at = failedAt.get(key) ?? 0
    if (Date.now() - at < LOAD_RETRY_MS) return null
  }
  componentCache.set(key, null) // 先占位防并发重复加载
  let errMsg = ''
  // 看门狗: import 迟迟不 settle(模块评估挂起/网络停滞)时落盘标记
  let settled = false
  const watchdog = setTimeout(() => {
    if (!settled) diagLog(`import ${key} HUNG >10s (module evaluation or fetch stalled)`)
  }, 10000)
  diagLog(`import ${key} start`)
  try {
    const mod: any = await import(/* @vite-ignore */ `/plugins/${encodeURIComponent(pluginID)}/dist/entry.js`)
    const comp = mod?.default?.components?.[componentId] ?? mod?.components?.[componentId]
    if (!comp) throw new Error(`组件 ${componentId} 未在 entry.js 导出`)
    componentCache.set(key, markRaw(comp))
    loadErrors.delete(key)
    failedAt.delete(key)
    void reportLoad(pluginID, componentId, true, '')
    return componentCache.get(key) ?? null
  } catch (e) {
    errMsg = e instanceof Error ? `${e.name}: ${e.message}` : String(e)
    console.error(`[plugin] 加载 ${pluginID}/${componentId} 失败`, e)
    loadErrors.set(key, errMsg)
    failedAt.set(key, Date.now())
    void reportLoad(pluginID, componentId, false, errMsg)
  } finally {
    settled = true
    clearTimeout(watchdog)
    diagLog(`import ${key} settled${errMsg ? ` failed: ${errMsg}` : ' ok'}`)
  }
  return componentCache.get(key) ?? null
}

/** reportLoad 加载结果回传宿主落日志(诊断"面板白屏/失败"类问题)。 */
async function reportLoad(pluginID: string, componentId: string, ok: boolean, errMsg: string) {
  try {
    const { PluginLoadReport } = await import('../../bindings/changeme/internal/services/pluginservice.js')
    await PluginLoadReport(pluginID, componentId, ok, errMsg)
  } catch {}
}

/** getPluginLoadError 取组件加载失败原因(未失败返回空)。 */
export function getPluginLoadError(pluginID: string, componentId: string): string {
  return loadErrors.get(`${pluginID}:${componentId}`) ?? ''
}

function getCachedComponent(pluginID: string, componentId: string): Component | null {
  return componentCache.get(`${pluginID}:${componentId}`) ?? null
}

async function prefetchComponents() {
  for (const p of plugins.value) {
    if (p.status !== 'running') continue
    for (const v of p.views ?? []) {
      if (!getCachedComponent(p.id, v.componentId)) {
        await loadPluginComponent(p.id, v.componentId)
      }
    }
  }
}

// ==================== ctx 与视图 ====================

/** getPluginCtx 取(或构造)插件注入上下文。 */
export function getPluginCtx(pluginID: string): PluginCtx {
  const cached = ctxCache.get(pluginID)
  if (cached) return cached
  const { isDark, accent } = useTheme()
  const ctx: PluginCtx = {
    pluginID,
    async call(method, args) {
      const { PluginCall } = await import('../../bindings/changeme/internal/services/pluginservice.js')
      const raw = await PluginCall(pluginID, method, args ? JSON.stringify(args) : '{}')
      const parsed = JSON.parse(raw)
      if (parsed && typeof parsed === 'object' && parsed.error) throw new Error(parsed.error)
      return parsed
    },
    async openTab(spec) {
      const { PluginOpenTab } = await import('../../bindings/changeme/internal/services/pluginservice.js')
      const raw = await PluginOpenTab(pluginID, JSON.stringify(spec))
      const parsed = JSON.parse(raw)
      if (parsed?.error) throw new Error(parsed.error)
      return String(parsed.tabId)
    },
    toast(message, level = 'info') {
      toastSink?.(message, level)
    },
    onEvent(cb) {
      let set = eventListeners.get(pluginID)
      if (!set) {
        set = new Set()
        eventListeners.set(pluginID, set)
      }
      set.add(cb)
      return () => {
        set!.delete(cb)
        if (set!.size === 0) eventListeners.delete(pluginID)
      }
    },
    theme: { isDark, accent },
  }
  ctxCache.set(pluginID, ctx)
  return ctx
}

/** pluginToolbarViews 注册表中运行中插件的全部视图(工具栏消费)。 */
export function pluginToolbarViews(pluginsRef: typeof plugins): PluginToolbarView[] {
  const out: PluginToolbarView[] = []
  for (const p of pluginsRef.value) {
    if (p.status !== 'running') continue
    for (const v of p.views ?? []) {
      out.push({
        pluginID: p.id,
        viewID: v.id,
        title: v.title || p.displayName,
        icon: v.icon || p.icon || '',
        componentId: v.componentId,
        accentColor: p.accentColor,
      })
    }
  }
  return out
}

export function usePlugins() {
  return { plugins, initPluginBridge, bindPluginTabManager, bindPluginToast, loadPluginComponent, getCachedComponent, getPluginCtx, pluginToolbarViews }
}
