// 按钮防抖: 首次点击立即执行, wait 毫秒内的重复点击(同键)忽略。
// 键取首个参数(通常是记录/插件 id), 不同对象互不抑制。
export function debounceClick<A extends unknown[]>(fn: (...args: A) => void, wait = 300): (...args: A) => void {
  const last = new Map<string, number>()
  return (...args: A) => {
    const key = args.length > 0 ? String(args[0]) : "*"
    const now = Date.now()
    if (now - (last.get(key) ?? 0) < wait) return
    last.set(key, now)
    fn(...args)
  }
}
