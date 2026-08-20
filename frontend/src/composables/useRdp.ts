import { ref } from 'vue'
import type { UserInteraction } from '@devolutions/iron-remote-desktop'

// 协议模块对象:iron-remote-desktop-rdp 的 Backend 导出
// (含 SessionBuilder/DeviceEvent/DesktopSize/ClipboardData/InputTransaction)。
// 该包 JS 以 base64 内嵌 WASM(约 6MB),无需额外资源;iron-remote-desktop 包在导入时注册
// <iron-remote-desktop> 自定义元素。
type RdpBackend = typeof import('@devolutions/iron-remote-desktop-rdp').Backend

async function loadRdpRuntime(): Promise<RdpBackend> {
  const [{ Backend, init }, mod] = await Promise.all([
    import('@devolutions/iron-remote-desktop-rdp'),
    import('@devolutions/iron-remote-desktop'),
  ])
  void mod
  await init('ERROR')
  return Backend
}

// 单例预加载:所有 RDP 标签页共享同一份 WASM 实例,避免每次打开标签页重复解码/编译
// 6MB 内嵌 WASM。失败后清空缓存以便重试。
let runtimePromise: Promise<RdpBackend> | null = null
export function ensureRdpRuntime(): Promise<RdpBackend> {
  if (!runtimePromise) {
    runtimePromise = loadRdpRuntime().catch((err) => {
      runtimePromise = null
      throw err
    })
  }
  return runtimePromise
}

// 应用启动后后台预热,首次打开 RDP 标签页时无需等待 WASM 加载。
export function warmupRdpRuntime(): void {
  ensureRdpRuntime().catch(() => {})
}

// 全局就绪的 UserInteraction(ready 事件回调中注入)
export const rdpUI = ref<UserInteraction | null>(null)

export interface RdpConnectionInfo {
  host: string
  port: number
  username: string
  password: string
  authToken: string
  bridgeWsUrl: string
}

// 构建 IronRDP 连接配置并建立会话,返回 SessionTerminationInfo 的 run 回调。
export async function connectRdp(
  ui: UserInteraction,
  conn: RdpConnectionInfo,
  desktopSize: { width: number; height: number },
) {
  const { enableCredssp } = await import('@devolutions/iron-remote-desktop-rdp')
  const config = ui
    .configBuilder()
    .withUsername(conn.username)
    .withPassword(conn.password)
    .withDestination(`${conn.host}:${conn.port}`)
    .withProxyAddress(conn.bridgeWsUrl)
    .withAuthToken(conn.authToken)
    .withDesktopSize(desktopSize)
    .withExtension(enableCredssp(false))
    .build()
  const info = await ui.connect(config)
  return info
}

// 解析 IronRDP 错误:跨 wasm 边界的 IronError 通过 backtrace()/kind() 暴露详情,
// 普通 Error 走 message,最后兜底 String。
export function formatIronError(e: unknown): string {
  const err = e as any
  if (err && typeof err.backtrace === 'function') {
    try {
      const bt = err.backtrace()
      if (bt) return String(bt)
    } catch {}
  }
  if (err && typeof err.kind === 'function') {
    try {
      const kind = err.kind()
      if (kind !== undefined) return `IronRDP 错误(kind=${kind})`
    } catch {}
  }
  return err?.message ? String(err.message) : String(e)
}