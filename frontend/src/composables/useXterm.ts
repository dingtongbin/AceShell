import { Terminal } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import { FitAddon } from '@xterm/addon-fit'
import { useTheme } from '../stores/theme'
import { Copy as ClipboardCopy, Paste as ClipboardPaste } from '../../bindings/changeme/internal/services/clipboardservice.js'

interface XtermHandlers {
  isConnected: () => boolean
  onData: (d: string) => void
  onPaste: (text: string) => void
  onMultiLinePaste: (text: string) => void
  // 剪贴板操作结果反馈(copy-ok / copy-fail / paste-fail / paste-empty / paste-ok),由调用方弹提示
  onClipboardFeedback?: (type: 'copy-ok' | 'copy-fail' | 'paste-fail' | 'paste-empty' | 'paste-ok', msg: string) => void
}

// TermConfig 终端显示配置(与后端 TerminalConfig 对应)。
export interface TermConfig {
  personalize: boolean
  fontColor: string
  bgColor: string
  bgOpacity: number
  bgImage: string
  fontFamily: string
  fontSize: number
  lineHeight: number
  copyOnSelect: boolean
  cursorBlink: boolean
  cursorStyle: 'bar' | 'block' | 'underline'
  scrollback: number
}

// defaultTermConfig 终端设置初始默认值(与后端 Init 默认一致)。
function defaultTermConfig(): TermConfig {
  return {
    personalize: false,
    fontColor: '#FFFFFF',
    bgColor: '#0C0C0C',
    bgOpacity: 100,
    bgImage: '',
    fontFamily: '"Cascadia Code", Consolas, "Courier New", monospace',
    fontSize: 16,
    lineHeight: 1,
    copyOnSelect: true,
    cursorBlink: true,
    cursorStyle: 'bar',
    scrollback: 1000,
  }
}

// normalizeTermConfig 将后端 JSON 解析结果规范化为完整 TermConfig(缺失字段回退默认)。
export function normalizeTermConfig(raw: any): TermConfig {
  const d = defaultTermConfig()
  return {
    personalize: raw?.personalize ?? d.personalize,
    fontColor: raw?.fontColor ?? d.fontColor,
    bgColor: raw?.bgColor ?? d.bgColor,
    bgOpacity: raw?.bgOpacity ?? d.bgOpacity,
    bgImage: raw?.bgImage ?? d.bgImage,
    fontFamily: raw?.fontFamily ?? d.fontFamily,
    fontSize: raw?.fontSize ?? d.fontSize,
    lineHeight: raw?.lineHeight ?? d.lineHeight,
    copyOnSelect: raw?.copyOnSelect ?? d.copyOnSelect,
    cursorBlink: raw?.cursorBlink ?? d.cursorBlink,
    cursorStyle: ['bar', 'block', 'underline'].includes(raw?.cursorStyle) ? raw.cursorStyle : d.cursorStyle,
    scrollback: Math.min(Math.max(raw?.scrollback ?? 1000, 0), 100000),
  }
}

// getTermBg 读取全局 --term-bg 变量（随透明度设置变化），返回带 alpha 的背景色。
function getTermBg(): string {
  const v = getComputedStyle(document.documentElement).getPropertyValue('--term-bg').trim()
  return v || 'rgba(0,0,0,0.7)'
}

// hexToRgba 将 #RRGGBB 转为 rgba 字符串。
function hexToRgba(hex: string, alpha: number): string {
  const m = /^#([0-9a-fA-F]{6})$/.exec(hex.trim())
  if (!m) return `rgba(0,0,0,${alpha})`
  const n = parseInt(m[1], 16)
  const r = (n >> 16) & 0xff
  const g = (n >> 8) & 0xff
  const b = n & 0xff
  return `rgba(${r},${g},${b},${Math.max(0, Math.min(1, alpha))})`
}

// resolveTermTheme 计算终端主题色:
// 个性化开启时使用用户设置的字体色/背景色(背景按不透明度);
// 关闭时跟随主题:暗色白字(背景沿用面板透明度联动),亮色反转黑字白底。
function resolveTermTheme(termCfg: TermConfig | null, isDark: boolean): {
  background: string
  foreground: string
  cursor: string
} {
  if (termCfg?.personalize) {
    const fg = termCfg.fontColor || '#FFFFFF'
    const bg = hexToRgba(termCfg.bgColor || '#0C0C0C', (termCfg.bgOpacity ?? 100) / 100)
    return { background: bg, foreground: fg, cursor: fg }
  }
  if (isDark) {
    return { background: getTermBg(), foreground: '#FFFFFF', cursor: '#FFFFFF' }
  }
  return { background: '#FFFFFF', foreground: '#1E1E1E', cursor: '#1E1E1E' }
}

// resetTermComposition 重置 xterm 的 IME 组合状态:
// 拖拽/拆分导致 textarea 脱离文档时,WebView 可能不配对触发 compositionend,
// 使 xterm 内部 _isComposing 卡死,keydown 被永久忽略(表现为终端无法输入)。
// 通过派发 compositionend 事件走 xterm 自身监听器恢复正常状态。
export function resetTermComposition(terminal: Terminal | null | undefined) {
  if (!terminal?.element) return
  const ta = terminal.element.querySelector('.xterm-helper-textarea') as HTMLTextAreaElement | null
  if (ta) {
    ta.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true, data: '' }))
  }
}

// applyBgImage 将背景图以 cover 方式(等比缩放铺满、不拉伸变形)应用到终端容器。
// 终端背景色按不透明度叠加在图之上,降低不透明度即可透出背景图。
export function applyBgImage(container: HTMLElement | null | undefined, imagePath: string) {
  if (!container) return
  if (!imagePath) {
    container.style.backgroundImage = ''
    container.style.backgroundSize = ''
    container.style.backgroundPosition = ''
    container.style.backgroundRepeat = ''
    return
  }
  container.style.backgroundImage = `url("${imagePath.replace(/"/g, '\\"')}")`
  container.style.backgroundSize = 'cover'
  container.style.backgroundPosition = 'center'
  container.style.backgroundRepeat = 'no-repeat'
}

// applyTermCfg 将终端配置即时应用到已存在的终端实例(滚动缓冲仅创建时生效,此处不处理)。
// 个性化关闭时,外观参数一律回退默认值,不受下方参数影响。
export function applyTermCfg(terminal: Terminal | null, termCfg: TermConfig | null) {
  if (!terminal) return
  const { isDark } = useTheme()
  const theme = resolveTermTheme(termCfg, isDark.value)
  terminal.options.theme = {
    ...terminal.options.theme,
    background: theme.background,
    foreground: theme.foreground,
    cursor: theme.cursor,
    selectionBackground: theme.foreground,
    selectionForeground: theme.background,
  }
  if (termCfg) {
    const p = termCfg.personalize
    const d = defaultTermConfig()
    terminal.options.fontSize = p ? termCfg.fontSize : d.fontSize
    terminal.options.fontFamily = p ? termCfg.fontFamily : d.fontFamily
    terminal.options.lineHeight = p ? termCfg.lineHeight : d.lineHeight
    terminal.options.cursorBlink = p ? termCfg.cursorBlink : d.cursorBlink
    terminal.options.cursorStyle = p ? termCfg.cursorStyle : d.cursorStyle
    applyBgImage(terminal.element?.parentElement, p ? termCfg.bgImage : '')
  }
}

// createXterm 创建统一的 xterm 终端实例（外观、交互与主标签页完全一致）：
// 左键选中即复制(可关闭)、右键粘贴（多行走确认回调）、单击/双击/三击选择由 xterm 原生处理。
// 个性化关闭时,字体/字号/行高/光标/复制等外观参数一律回退默认值,仅滚动缓冲始终按配置生效。
// 返回的 cleanup 必须在终端 dispose 前调用,释放 document 级监听引用,避免实例无法回收。
export function createXterm(
  container: HTMLElement,
  handlers: XtermHandlers,
  termCfg: TermConfig | null = null,
): { terminal: Terminal; fitAddon: FitAddon; cleanup: () => void } {
  const { isDark } = useTheme()
  const cfg = normalizeTermConfig(termCfg)
  const theme = resolveTermTheme(termCfg, isDark.value)
  const p = cfg.personalize
  const d = defaultTermConfig()

  const terminal = new Terminal({
    fontSize: p ? cfg.fontSize : d.fontSize,
    fontFamily: p ? cfg.fontFamily : d.fontFamily,
    lineHeight: p ? cfg.lineHeight : d.lineHeight,
    cursorBlink: p ? cfg.cursorBlink : d.cursorBlink,
    cursorStyle: p ? cfg.cursorStyle : d.cursorStyle,
    allowTransparency: true,
    convertEol: true,
    scrollback: cfg.scrollback,
    theme: {
      background: theme.background,
      foreground: theme.foreground,
      cursor: theme.cursor,
      cursorAccent: '#0a0a0a',
      selectionBackground: theme.foreground,
      selectionForeground: theme.background,
    },
  })

  applyBgImage(container, p ? cfg.bgImage : '')

  const fitAddon = new FitAddon()
  terminal.loadAddon(fitAddon)
  terminal.open(container)

  requestAnimationFrame(() => {
    try { fitAddon.fit() } catch {}
  })

  terminal.onData((d: string) => {
    if (handlers.isConnected()) handlers.onData(d)
  })

  // 监听绑定在 terminal.element 上而非 container:跨 pane 移动/组件重建时,
  // 容器 DOM 会销毁重建,但 element 随终端实例迁移,监听不丢失
  const termEl = terminal.element

// 左键选中即复制:基础交互(§16.4),独立于个性化开关。
// 复制通道固定走后端 ClipboardService(Go 系统级剪贴板,syscall 直写,
// 不依赖 WebView2 的 clipboard API 权限),结果经 onClipboardFeedback 反馈。
// 监听在 document 级 mouseup:拖动可能结束在终端边界之外,元素级监听会漏掉;
// 事件冒泡最晚触发,此时 xterm 已完成选区更新,单击(无选区)不会误复制。
// 复制成功后保留选区(不主动清除),用户可继续观察或再次操作。
const cleanupFns: (() => void)[] = []
if (cfg.copyOnSelect) {
  const onDocumentMouseUp = (e: MouseEvent) => {
    if (e.button !== 0) return
    if (!termEl?.contains(e.target as Node)) return
    let sel = ''
    try {
      sel = terminal.getSelection()
    } catch {}
    if (!sel) return
    ClipboardCopy(sel)
      .then(err => {
        if (err) {
          console.warn('选中复制失败:', err)
          handlers.onClipboardFeedback?.('copy-fail', '复制失败: ' + err)
          return
        }
        handlers.onClipboardFeedback?.('copy-ok', '已复制选中内容')
      })
      .catch(err => {
        console.warn('选中复制失败:', err)
        handlers.onClipboardFeedback?.('copy-fail', '复制失败: ' + ((err as any)?.message || err))
      })
  }
  document.addEventListener('mouseup', onDocumentMouseUp)
  cleanupFns.push(() => document.removeEventListener('mouseup', onDocumentMouseUp))
}

// 右键粘贴:无条件粘贴系统剪贴板内容(需求 §16.4,不因残留选区走复制分支)。
// 剪贴板读取固定走后端 ClipboardService,不依赖 WebView2 的 clipboard-read 权限。
// 触发通道双保险:termEl 的 contextmenu + document 级 mouseup(button===2)兜底
// (WebView2 默认右键菜单开启时,contextmenu 事件可能被默认菜单流程吞掉,兜底确保粘贴可达);
// 800ms 内去重,避免同一右键两通道都触发导致重复粘贴。每一步失败都有可见反馈。
let lastPasteAt = 0
const doPaste = async () => {
  const now = Date.now()
  if (now - lastPasteAt < 800) return
  lastPasteAt = now
  let text = ''
  try {
    text = await ClipboardPaste()
  } catch (err) {
    console.warn('读取剪贴板失败:', err)
    handlers.onClipboardFeedback?.('paste-fail', '读取剪贴板失败: ' + ((err as any)?.message || err))
    return
  }
  if (!text) {
    handlers.onClipboardFeedback?.('paste-empty', '剪贴板为空,无内容可粘贴')
    return
  }
  if (!handlers.isConnected()) {
    handlers.onClipboardFeedback?.('paste-fail', '当前终端未连接,无法粘贴')
    return
  }
  const stripped = text.replace(/(\r?\n)+$/, '')
  if (stripped.includes('\n')) {
    handlers.onMultiLinePaste(text)
  } else {
    handlers.onPaste(text)
    handlers.onClipboardFeedback?.('paste-ok', '已粘贴到终端')
  }
}

const onContextMenu = (e: Event) => {
  e.preventDefault()
  e.stopPropagation()
  void doPaste()
}
termEl?.addEventListener('contextmenu', onContextMenu)
cleanupFns.push(() => termEl?.removeEventListener('contextmenu', onContextMenu))

const onDocumentRightMouseUp = (e: MouseEvent) => {
  if (e.button !== 2) return
  if (!termEl?.contains(e.target as Node)) return
  void doPaste()
}
document.addEventListener('mouseup', onDocumentRightMouseUp)
cleanupFns.push(() => document.removeEventListener('mouseup', onDocumentRightMouseUp))

  const cleanup = () => {
    for (const fn of cleanupFns) {
      try { fn() } catch {}
    }
    cleanupFns.length = 0
  }

  return { terminal, fitAddon, cleanup }
}