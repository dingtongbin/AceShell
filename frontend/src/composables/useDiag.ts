// 前端诊断探针: 全局错误与关键异步事件 → 宿主 plugin-runs.log(带宿主时间戳)。
// 用途: "面板白屏/页面卡死"类问题在无 devtools 的生产包里拿不到现场,
// 统一经此落盘, 与资产请求日志做时序对账。

let seq = 0

/** diagLog 落一行诊断; 失败静默(诊断通道本身不得影响业务)。 */
export function diagLog(msg: string): void {
  const line = `${new Date().toISOString()} #${++seq} ${msg}`
  try {
    import('../../bindings/changeme/internal/services/pluginservice.js')
      .then(({ FrontendDiagLog }) => FrontendDiagLog(line))
      .catch(() => {})
  } catch {}
}

/** installDiagProbes 应用启动时调用一次(幂等)。 */
export function installDiagProbes(): void {
  if ((globalThis as any).__ACESHELL_DIAG_INSTALLED__) return
  ;(globalThis as any).__ACESHELL_DIAG_INSTALLED__ = true

  window.addEventListener('error', (e) => {
    // 捕获阶段区分两类: 资源加载失败(target=元素)与 JS 异常(target=window)
    const t = e.target
    if (t instanceof HTMLElement && (t.tagName === 'IMG' || t.tagName === 'SCRIPT' || t.tagName === 'LINK')) {
      const el = t as HTMLImageElement
      diagLog(`resource-error <${el.tagName.toLowerCase()}> ${String(el.src).slice(0, 300)}`)
      return
    }
    diagLog(`js-error ${e.message} @ ${e.filename}:${e.lineno}:${e.colno}`)
  }, true)

  window.addEventListener('unhandledrejection', (e) => {
    const r = e.reason
    const stack = r instanceof Error ? (r.stack || r.message) : String(r)
    diagLog(`unhandled-rejection ${stack.slice(0, 2000)}`)
  })
}
