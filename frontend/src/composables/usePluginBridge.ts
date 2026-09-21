// 插件前端桥接器(模块级单例, 仿 useMcpBridge)。
// 职责:
//   1. 订阅后端插件事件(registry/open-tab/tab-updated/tab-closed/toast)
//   2. 按注册表动态 import 插件前端模块(/plugins/<id>/dist/entry.js, 同源)并缓存组件
//   3. 为插件组件构造注入 ctx(call/openTab/toast/主题快照)
//   4. 标签页类事件路由到 TabManager 绑定的处理器(bindPluginTabManager)
import { ref, reactive, markRaw, nextTick, defineComponent, h, onMounted, onBeforeUnmount, type Component } from 'vue'
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
  /** 文档钩子: locale → 插件目录内相对路径(md); 'default' 为兜底 */
  docs?: Record<string, string>
  /** 视图点击钩子: 声明后点活动栏图标 = 调此 RPC(打开/定位工具标签页), 不展开侧栏 */
  viewClickRpc?: string
  /** 能力标签声明 (插件 Info 上报) */
  capabilities?: string[]
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

// ==================== 代次(epoch): 插件失效协议的核心 ====================
// 每次重载/更新/禁用/卸载都递增该插件的代次。代次进入两处:
//   1. 组件缓存 key —— 保证失效后 loadPluginComponent 必然 miss;
//   2. ESM 模块 URL 的 query(?v=) —— 硬约束: 浏览器模块表按 specifier 缓存,
//      Cache-Control: no-store 只挡 HTTP 缓存, 挡不住模块表复用已求值的模块,
//      specifier 变化才会重新求值并产生新的组件对象。
// 用 reactive 包装: PluginPanel 的 watch/computed 需要跟踪代次变化。
const epochs = reactive(new Map<string, number>())

/** pluginEpoch 取插件当前代次(未失效过为 0)。 */
export function pluginEpoch(pluginID: string): number {
  return epochs.get(pluginID) ?? 0
}

/** isPluginAlive 插件是否仍在册且可用(运行中或启动中)。 */
export function isPluginAlive(pluginID: string): boolean {
  const p = plugins.value.find(x => x.id === pluginID)
  return !!p && (p.status === 'running' || p.status === 'starting')
}

function bumpEpoch(pluginID: string): number {
  const n = pluginEpoch(pluginID) + 1
  epochs.set(pluginID, n)
  return n
}

function cacheKey(pluginID: string, componentId: string): string {
  return `${pluginID}:${componentId}:${pluginEpoch(pluginID)}`
}

/** pluginModuleURL 插件前端模块地址, query 携带版本与代次作 cache-bust。 */
function pluginModuleURL(pluginID: string): string {
  const v = plugins.value.find(p => p.id === pluginID)?.version ?? ''
  return `/plugins/${encodeURIComponent(pluginID)}/dist/entry.js?v=${encodeURIComponent(v)}-${pluginEpoch(pluginID)}`
}

// 组件缓存: cacheKey → Component | null(失败占位)
const componentCache = new Map<string, Component | null>()
// 失败时间戳: 失败不永久缓存, 5s 后允许重试(面板重开/注册表刷新时)
const failedAt = new Map<string, number>()
// 加载失败原因: 同键 → 错误消息(面板展示, 便于定位)
const loadErrors = new Map<string, string>()
// 在途加载: 同 key 并发调用共享同一 Promise(避免"在途"被误判为"失败占位")
const inflight = new Map<string, Promise<Component | null>>()

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
  /** 插件代次变化时用新模块就地重建该插件的标签页(可选: 未绑定则跳过)。 */
  reloadPluginTabs?: (pluginID: string) => Promise<void>
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
  // 防御: 运行时若已反序列化为对象则直通, 字符串再 parse(双编码/包装均兼容)
  const d = evt?.data
  if (d == null) return null
  if (typeof d === 'object') return d
  try { return JSON.parse(d) } catch { return null }
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
      diagLog(`registry: ${data.plugins.length} plugins [${data.plugins.map((p: any) => `${p.id}:${p.status}`).join(', ')}]`)
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

  // 插件失效事件(重载/更新/禁用/卸载): 触发前端失效协议
  Events.On('plugin-invalidated', (evt: any) => {
    const info = parseEvt(evt)
    if (!info?.id) return
    void invalidatePlugin(String(info.id), String(info.reason || 'unknown')).catch(e => {
      console.error('[plugin] invalidate failed', e)
    })
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

/** loadPluginComponent 动态加载并缓存插件组件; 失败冷却 5s, 并发共享同一在途 Promise。 */
export async function loadPluginComponent(pluginID: string, componentId: string): Promise<Component | null> {
  const key = cacheKey(pluginID, componentId)
  const cached = componentCache.get(key)
  if (cached) return cached
  if (cached === null) {
    const at = failedAt.get(key)
    if (at !== undefined && Date.now() - at < LOAD_RETRY_MS) return null
    const pending = inflight.get(key)
    if (pending) return pending
  }
  const task = doLoadPluginComponent(pluginID, componentId, key)
  inflight.set(key, task)
  try {
    return await task
  } finally {
    inflight.delete(key)
  }
}

// wrapCustomRenderer 自定义渲染逃逸舱: 插件组件导出 { __aceshellCustom: true, mount(el, props) → dispose? }
// 时(React/Svelte 等自渲染框架), 以薄壳 Vue 组件承载 —— 宿主只管生命周期与容器,
// 渲染完全交给插件 (dispose 可选, 卸载时回调清理)。
function wrapCustomRenderer(mount: (el: HTMLElement, props: Record<string, any>) => void | (() => void)): Component {
  return markRaw(defineComponent({
    name: 'PluginCustomRenderer',
    inheritAttrs: false,
    setup(_, { attrs }) {
      const host = ref<HTMLElement | null>(null)
      let dispose: (() => void) | void
      onMounted(() => {
        if (host.value) dispose = mount(host.value, attrs as Record<string, any>)
      })
      onBeforeUnmount(() => {
        if (typeof dispose === 'function') dispose()
      })
      return () => h('div', { ref: host, style: 'width:100%;height:100%;overflow:hidden;' })
    },
  }))
}

async function doLoadPluginComponent(pluginID: string, componentId: string, key: string): Promise<Component | null> {
  let errMsg = ''
  // 看门狗: import 迟迟不 settle(模块评估挂起/网络停滞)时落盘标记
  let settled = false
  const watchdog = setTimeout(() => {
    if (!settled) diagLog(`import ${key} HUNG >10s (module evaluation or fetch stalled)`)
  }, 10000)
  diagLog(`import ${key} start`)
  try {
    const mod: any = await import(/* @vite-ignore */ pluginModuleURL(pluginID))
    const raw = mod?.default?.components?.[componentId] ?? mod?.components?.[componentId]
    if (!raw) throw new Error(`组件 ${componentId} 未在 entry.js 导出`)
    // 自定义渲染逃逸舱: { __aceshellCustom: true, mount(el, props) } 形态包装为薄壳组件
    const comp: Component = (raw && typeof raw === 'object' && (raw as any).__aceshellCustom === true && typeof (raw as any).mount === 'function')
      ? wrapCustomRenderer((raw as any).mount)
      : raw
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
  return componentCache.get(cacheKey(pluginID, componentId)) ?? null
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

// ==================== 失效协议 ====================

/** removePluginStyles 移除某插件注入的全部样式(含早期无标记版本的兼容清理)。 */
function removePluginStyles(pluginID: string) {
  // 契约: 插件注入的 <style> 须带 data-aceshell-plugin="<pluginID>"(见 ping/entry.ts)
  document.querySelectorAll(`style[data-aceshell-plugin="${pluginID}"]`).forEach(el => el.remove())
  const legacy = document.getElementById(`aceshell-plugin-${pluginID}-style`)
  if (legacy) legacy.remove()
}

/**
 * invalidatePlugin 插件失效: 换代 → 清缓存 → 断订阅 → (重载/更新时)就地重建标签页
 * → (组件树 settles 后)移除注入样式。
 * 由后端 plugin-invalidated 事件驱动。
 * 卸载/禁用不触发任何重新加载 —— 文件已不在, 加载只会得到 404 与失败闪屏;
 * 面板/标签页的拆除由注册表事件驱动(状态不再是 running), 这里只负责清干净状态。
 */
export async function invalidatePlugin(pluginID: string, reason: string) {
  bumpEpoch(pluginID)
  const prefix = pluginID + ':'
  for (const m of [componentCache, failedAt, loadErrors]) {
    for (const k of [...m.keys()]) {
      if (k.startsWith(prefix)) m.delete(k)
    }
  }
  ctxCache.delete(pluginID)
  const listeners = eventListeners.get(pluginID)
  if (listeners) {
    listeners.clear()
    eventListeners.delete(pluginID)
  }
  if (reason === 'reload' || reason === 'update') {
    try {
      await tabApi?.reloadPluginTabs?.(pluginID)
    } catch (e) {
      console.warn('[plugin] reload tabs failed', e)
    }
  }
  // 等面板与标签页的组件树重建完成再动 DOM 级资源, 避免旧树短暂失去样式
  await nextTick()
  removePluginStyles(pluginID)
  diagLog(`invalidate ${pluginID} (${reason}) epoch=${pluginEpoch(pluginID)}`)
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
  return { plugins, initPluginBridge, bindPluginTabManager, bindPluginToast, loadPluginComponent, getCachedComponent, getPluginCtx, pluginToolbarViews, pluginEpoch, invalidatePlugin }
}
