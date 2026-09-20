// 设计令牌单一来源: 全应用唯一允许定义颜色的地方。
// theme.ts 由此派生 naive-ui GlobalThemeOverrides 与全局 CSS 变量;
// 组件样式只允许引用令牌(CSS 变量),禁止新增硬编码色值。
//
// 强调色参数化: 默认 #0078d4 时,浅色模式精确复现历史值
// (#005a9e/#0078d4/#00487f),深色模式补齐 naive-ui 品牌色覆盖
// (修复深色下组件库回落绿色默认主色导致的蓝绿割裂)。
import type { GlobalThemeOverrides } from 'naive-ui'

/** 默认强调色(Fluent 蓝)。 */
export const DEFAULT_ACCENT = '#0078d4'

/** 预设强调色(外观页选择器用)。 */
export const ACCENT_PRESETS: Array<{ label: string; value: string }> = [
  { label: '蓝', value: '#0078d4' },
  { label: '靛', value: '#4f6bed' },
  { label: '紫', value: '#7c3aed' },
  { label: '青', value: '#00897b' },
  { label: '绿', value: '#0f9d58' },
  { label: '橙', value: '#d97a1a' },
  { label: '赭', value: '#c94f4f' },
  { label: '灰', value: '#5a6570' },
]

// ==================== 颜色工具 ====================

/** hex → [r,g,b]。 */
function hexToRgb(hex: string): [number, number, number] {
  const h = hex.replace('#', '')
  const v = h.length === 3 ? h.split('').map(c => c + c).join('') : h
  return [parseInt(v.slice(0, 2), 16), parseInt(v.slice(2, 4), 16), parseInt(v.slice(4, 6), 16)]
}

function clamp(n: number): number {
  return Math.max(0, Math.min(255, Math.round(n)))
}

function rgbToHex(r: number, g: number, b: number): string {
  return '#' + [r, g, b].map(n => clamp(n).toString(16).padStart(2, '0')).join('')
}

/** 向黑色收缩(factor=0.75 即保留 75% 亮度)。 */
function shade(hex: string, factor: number): string {
  const [r, g, b] = hexToRgb(hex)
  return rgbToHex(r * factor, g * factor, b * factor)
}

/** 向白色提亮(ratio 为白色的混合比例)。 */
function tint(hex: string, ratio: number): string {
  const [r, g, b] = hexToRgb(hex)
  return rgbToHex(r + (255 - r) * ratio, g + (255 - g) * ratio, b + (255 - b) * ratio)
}

// ==================== 语义色 ====================

/** 语义状态色(深/浅各一套,收编散落的红橙绿硬编码)。 */
export const SEMANTIC = {
  dark: { success: '#3fb950', warning: '#e2a03f', danger: '#e45858', info: '#4fc3ff' },
  light: { success: '#1a7f37', warning: '#b45309', danger: '#d13438', info: '#0b6aaf' },
} as const

// ==================== naive-ui overrides ====================

// 浅色组件覆盖(历史值,视觉零变化);主色族由 injectAccent 注入。
const lightOverridesBase: GlobalThemeOverrides = {
  common: {
    primaryColor: '#005a9e',
    primaryColorHover: '#0078d4',
    primaryColorPressed: '#004078',
    primaryColorSuppl: '#005a9e',
    bodyColor: '#f7f7f7',
    cardColor: '#ffffff',
    modalColor: '#ffffff',
    tableColor: '#ffffff',
    inputColor: '#ffffff',
    inputColorDisabled: '#f5f5f5',
    actionColor: '#f7f7f7',
    hoverColor: 'rgba(0,0,0,0.03)',
    borderColor: '#e0e0e0',
    dividerColor: '#e0e0e0',
    textColor1: '#1a1a1a',
    textColor2: '#333333',
    textColor3: '#888888',
    fontSize: '13px',
    fontSizeSmall: '12px',
    fontSizeTiny: '11px',
    fontSizeMedium: '13px',
    fontSizeLarge: '15px',
  },
  Button: {
    textColor: '#333',
    textColorHover: '#333',
    textColorPrimary: '#fff',
    border: '1px solid #d0d0d0',
    borderHover: '1px solid #005a9e',
    color: '#ffffff',
    colorHover: '#f5f5f5',
    colorPrimary: '#005a9e',
    colorPrimaryHover: '#0078d4',
    colorPrimaryPressed: '#004078',
    rippleColor: '#005a9e',
    borderRadius: '3px',
  },
  Input: {
    color: '#ffffff',
    colorFocus: '#ffffff',
    border: '1px solid #d0d0d0',
    borderFocus: '1px solid #005a9e',
    textColor: '#1a1a1a',
    placeholderColor: '#aaa',
    borderRadius: '3px',
  },
  Select: {
    menuColor: '#ffffff',
    color: '#ffffff',
    border: '1px solid #d0d0d0',
    borderFocus: '1px solid #005a9e',
  },
  Switch: {
    railColor: '#d0d0d0',
    railColorActive: '#005a9e',
  },
  Checkbox: {
    color: '#ffffff',
    checkMarkColor: '#fff',
    border: '1px solid #d0d0d0',
  },
  Tag: {
    color: '#f0f0f0',
    textColor: '#333',
    border: '1px solid #d0d0d0',
  },
  Modal: { color: '#ffffff', textColor: '#1a1a1a' },
  Dialog: { color: '#ffffff', textColor: '#1a1a1a' },
  Card: { color: '#ffffff', borderColor: '#e8e8e8' },
  Table: {
    tdColor: '#ffffff',
    thColor: '#fafafa',
    tdColorStriped: '#f7f7f7',
    borderColor: '#e8e8e8',
    thTextColor: '#1a1a1a',
    tdTextColor: '#1a1a1a',
  },
  Dropdown: {
    menuColor: '#ffffff',
    optionTextColor: '#1a1a1a',
    optionTextColorHover: '#1a1a1a',
    optionColorHover: 'rgba(0,0,0,0.03)',
  },
  Empty: { textColor: '#888' },
  Message: { color: '#ffffff', textColor: '#1a1a1a' },
  Notification: { color: '#ffffff', textColor: '#1a1a1a' },
  Tooltip: { color: '#555', textColor: '#fff' },
  Progress: { railColor: '#e8e8e8' },
  Slider: { railColor: '#e8e8e8' },
  DataTable: {
    tdColor: '#ffffff',
    thColor: '#fafafa',
    borderColor: '#e8e8e8',
    thTextColor: '#1a1a1a',
    tdTextColor: '#1a1a1a',
  },
  Tree: {
    nodeTextColor: '#1a1a1a',
    nodeTextColorActive: '#1a1a1a',
    arrowColor: '#888',
  },
}

// 深色组件覆盖(此前为 undefined → 组件库回落绿色默认主色,是蓝绿割裂的根源)。
// 表面层对齐应用灰阶(#252526/#3c3c3c),主色族由 injectAccent 注入。
const darkOverridesBase: GlobalThemeOverrides = {
  common: {
    primaryColor: '#0078d4',
    primaryColorHover: '#1f88d9',
    primaryColorPressed: '#0066b4',
    primaryColorSuppl: '#0078d4',
    bodyColor: '#252526',
    cardColor: '#252526',
    modalColor: '#252526',
    popoverColor: '#2b2b2b',
    tableColor: '#252526',
    inputColor: '#303030',
    inputColorDisabled: '#3a3a3a',
    actionColor: '#2d2d2d',
    hoverColor: 'rgba(255,255,255,0.06)',
    borderColor: '#333333',
    dividerColor: '#333333',
    textColor1: '#d4d4d4',
    textColor2: '#b8b8b8',
    textColor3: '#8a8a8a',
    fontSize: '13px',
    fontSizeSmall: '12px',
    fontSizeTiny: '11px',
    fontSizeMedium: '13px',
    fontSizeLarge: '15px',
  },
  Button: {
    textColor: '#d4d4d4',
    textColorHover: '#ffffff',
    textColorPrimary: '#fff',
    border: '1px solid #333333',
    borderHover: '1px solid #1f88d9',
    color: '#3c3c3c',
    colorHover: '#454545',
    colorPrimary: '#0078d4',
    colorPrimaryHover: '#1f88d9',
    colorPrimaryPressed: '#0066b4',
    rippleColor: '#0078d4',
    borderRadius: '3px',
  },
  Input: {
    color: '#303030',
    colorFocus: '#303030',
    border: '1px solid #333333',
    borderFocus: '1px solid #1f88d9',
    textColor: '#d4d4d4',
    placeholderColor: '#6e6e6e',
    borderRadius: '3px',
  },
  Select: {
    menuColor: '#2b2b2b',
    color: '#303030',
    border: '1px solid #333333',
    borderFocus: '1px solid #1f88d9',
  },
  Switch: {
    railColor: '#3c3c3c',
    railColorActive: '#0078d4',
  },
  Checkbox: {
    color: '#303030',
    checkMarkColor: '#fff',
    border: '1px solid #3d3d3d',
  },
  Tag: {
    color: '#333333',
    textColor: '#d4d4d4',
    border: '1px solid #333333',
  },
  Modal: { color: '#252526', textColor: '#d4d4d4' },
  Dialog: { color: '#252526', textColor: '#d4d4d4' },
  Card: { color: '#2b2b2b', borderColor: '#333333' },
  Table: {
    tdColor: '#252526',
    thColor: '#2d2d2d',
    tdColorStriped: '#2b2b2b',
    borderColor: '#333333',
    thTextColor: '#d4d4d4',
    tdTextColor: '#d4d4d4',
  },
  Dropdown: {
    menuColor: '#2b2b2b',
    optionTextColor: '#d4d4d4',
    optionTextColorHover: '#ffffff',
    optionColorHover: 'rgba(255,255,255,0.06)',
  },
  Empty: { textColor: '#8a8a8a' },
  Message: { color: '#2b2b2b', textColor: '#d4d4d4' },
  Notification: { color: '#2b2b2b', textColor: '#d4d4d4' },
  Tooltip: { color: '#3a3a3a', textColor: '#d4d4d4' },
  Progress: { railColor: '#3c3c3c' },
  Slider: { railColor: '#3c3c3c' },
  DataTable: {
    tdColor: '#252526',
    thColor: '#2d2d2d',
    borderColor: '#333333',
    thTextColor: '#d4d4d4',
    tdTextColor: '#d4d4d4',
  },
  Tree: {
    nodeTextColor: '#d4d4d4',
    nodeTextColorActive: '#ffffff',
    arrowColor: '#8a8a8a',
  },
}

/** 把强调色注入覆盖表(替换主色族的全部出现点)。 */
function injectAccent(base: GlobalThemeOverrides, accent: string): GlobalThemeOverrides {
  const hover = tint(accent, 0.12)
  const pressed = shade(accent, 0.85)
  const o: GlobalThemeOverrides = JSON.parse(JSON.stringify(base))
  const common = o.common as Record<string, string>
  common.primaryColor = accent
  common.primaryColorHover = hover
  common.primaryColorPressed = pressed
  common.primaryColorSuppl = accent
  const btn = o.Button as Record<string, string>
  btn.colorPrimary = accent
  btn.colorPrimaryHover = hover
  btn.colorPrimaryPressed = pressed
  btn.borderHover = `1px solid ${hover}`
  btn.rippleColor = accent
  const input = o.Input as Record<string, string>
  input.borderFocus = `1px solid ${hover}`
  const select = o.Select as Record<string, string>
  select.borderFocus = `1px solid ${hover}`
  const sw = o.Switch as Record<string, string>
  sw.railColorActive = accent
  return o
}

/** 浅色模式 naive-ui 覆盖(强调色参数化)。 */
export function lightOverrides(accent: string): GlobalThemeOverrides {
  // 浅色历史值: primary=shade(accent,0.75)=#005a9e、hover=accent、pressed=shade(accent,0.6)≈#004078。
  const o = injectAccent(lightOverridesBase, accent)
  const common = o.common as Record<string, string>
  common.primaryColor = shade(accent, 0.75)
  common.primaryColorHover = accent
  common.primaryColorPressed = shade(accent, 0.6)
  common.primaryColorSuppl = shade(accent, 0.75)
  const btn = o.Button as Record<string, string>
  btn.colorPrimary = shade(accent, 0.75)
  btn.colorPrimaryHover = accent
  btn.colorPrimaryPressed = shade(accent, 0.6)
  btn.borderHover = `1px solid ${accent}`
  btn.rippleColor = shade(accent, 0.75)
  const input = o.Input as Record<string, string>
  input.borderFocus = `1px solid ${accent}`
  const select = o.Select as Record<string, string>
  select.borderFocus = `1px solid ${accent}`
  const sw = o.Switch as Record<string, string>
  sw.railColorActive = shade(accent, 0.75)
  return o
}

/** 深色模式 naive-ui 覆盖(强调色参数化)。 */
export function darkOverrides(accent: string): GlobalThemeOverrides {
  return injectAccent(darkOverridesBase, accent)
}

// ==================== CSS 变量 ====================

/**
 * 协议身份色: 各连接协议的固定标识色(会话树/标签页/日志),深浅同值、
 * 不随强调色变化——统一为强调色会丢失协议可辨识度,故保留多彩但收敛到这里。
 */
const PROTO_COLORS: Record<string, string> = {
  '--proto-ssh': '#4ec9b0',
  '--proto-telnet': '#569cd6',
  '--proto-shell': '#dcdcaa',
  '--proto-default': '#6e9fc7',
}

/**
 * 计算 global CSS 变量(挂到 documentElement)。
 * a 为面板不透明度(0.3~1,来自外观设置),仅影响历史上有透明度的表面。
 */
export function buildCssVars(isDark: boolean, a: number, accent: string): Record<string, string> {
  const base: Record<string, string> = isDark
    ? {
        '--sidebar-bg': `rgba(32,32,32,${a})`,
        '--sidebar-shadow': '#3c3c3c',
        '--body-bg': `rgba(38,38,38,${a})`,
        '--text-color': '#d4d4d4',
        '--icon-color': '#6e6e6e',
        '--icon-hover': '#c5c5c5',
        '--toolbar-bg': `rgba(52,52,52,${a})`,
        '--card-bg': `rgba(44,44,44,${a})`,
        '--panel-bg': '#252526',
        '--border-color': '#3c3c3c',
        '--active-color': '#ffffff',
        '--hover-bg': 'rgba(255,255,255,0.05)',
        '--close-hover-bg': 'rgba(255,255,255,0.1)',
        '--tab-active-bg': `rgba(22,22,22,${a})`,
        '--tab-inactive-bg': `rgba(44,44,44,${a})`,
        '--term-bg': `rgba(22,22,22,${a})`,
        '--primary-color': accent,
        '--primary-hover': tint(accent, 0.12),
        '--primary-pressed': shade(accent, 0.85),
        '--on-primary': '#ffffff',
        '--success-color': SEMANTIC.dark.success,
        '--warning-color': SEMANTIC.dark.warning,
        '--danger-color': SEMANTIC.dark.danger,
        '--info-color': SEMANTIC.dark.info,
      }
    : {
        '--sidebar-bg': '#efefef',
        '--sidebar-shadow': '#d9d9d9',
        '--body-bg': '#f7f7f7',
        '--text-color': '#1a1a1a',
        '--icon-color': '#999999',
        '--icon-hover': '#555555',
        '--toolbar-bg': '#e1e1e1',
        '--card-bg': '#ffffff',
        '--panel-bg': '#ffffff',
        '--border-color': '#e0e0e0',
        '--active-color': '#000000',
        '--hover-bg': 'rgba(0,0,0,0.03)',
        '--close-hover-bg': 'rgba(0,0,0,0.06)',
        '--tab-active-bg': '#ffffff',
        '--tab-inactive-bg': '#d9d9d9',
        '--term-bg': `rgba(245,245,245,${a})`,
        '--primary-color': shade(accent, 0.75),
        '--primary-hover': accent,
        '--primary-pressed': shade(accent, 0.6),
        '--on-primary': '#ffffff',
        '--success-color': SEMANTIC.light.success,
        '--warning-color': SEMANTIC.light.warning,
        '--danger-color': SEMANTIC.light.danger,
        '--info-color': SEMANTIC.light.info,
      }
  return { ...base, ...PROTO_COLORS }
}
