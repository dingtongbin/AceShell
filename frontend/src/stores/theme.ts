import { ref, computed } from 'vue'
import { darkTheme, lightTheme } from 'naive-ui'
import type { GlobalThemeOverrides } from 'naive-ui'
import { buildCssVars, darkOverrides, lightOverrides, DEFAULT_ACCENT } from './tokens'

type ThemeMode = 'dark' | 'light' | 'auto'

const themeMode = ref<ThemeMode>('dark')

/** 自定义强调色(外观页可改;所有主色族由此派生)。 */
const accent = ref<string>(DEFAULT_ACCENT)

const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
const systemPrefersDark = ref(mediaQuery.matches)

mediaQuery.addEventListener('change', (e: MediaQueryListEvent) => {
  systemPrefersDark.value = e.matches
})

const isDark = computed(() => {
  if (themeMode.value === 'auto') {
    return systemPrefersDark.value
  }
  return themeMode.value === 'dark'
})

export function useTheme() {
  function initTheme(mode: string) {
    if (mode === 'dark' || mode === 'light' || mode === 'auto') {
      themeMode.value = mode as ThemeMode
    }
  }

  function setThemeMode(mode: ThemeMode) {
    themeMode.value = mode
  }

  function toggleTheme() {
    if (themeMode.value === 'dark') {
      themeMode.value = 'light'
    } else if (themeMode.value === 'light') {
      themeMode.value = 'dark'
    } else {
      themeMode.value = isDark.value ? 'light' : 'dark'
    }
  }

  function setTheme(dark: boolean) {
    themeMode.value = dark ? 'dark' : 'light'
  }

  /** 设置自定义强调色(外观页调用;overrides 与 CSS 变量随之刷新)。 */
  function setAccent(color: string) {
    if (/^#[0-9a-fA-F]{6}$/.test(color)) {
      accent.value = color.toLowerCase()
    }
  }

  const theme = computed(() => isDark.value ? darkTheme : lightTheme)
  // 深浅两套均有品牌色覆盖(此前深色无覆盖 → 组件库回落绿色默认主色,蓝绿割裂根源)
  const themeOverrides = computed<GlobalThemeOverrides>(() =>
    isDark.value ? darkOverrides(accent.value) : lightOverrides(accent.value))

  return {
    isDark,
    themeMode,
    theme,
    themeOverrides,
    accent,
    toggleTheme,
    setTheme,
    setThemeMode,
    initTheme,
    setAccent,
  }
}

/**
 * 应用全局 CSS 变量到 documentElement(App.vue watchEffect 调用)。
 * a 为面板不透明度(0.3~1)。
 */
export function applyThemeVars(isDark: boolean, opacity: number, accentColor: string) {
  const d = document.documentElement.style
  const a = Math.min(100, Math.max(30, opacity)) / 100
  const vars = buildCssVars(isDark, a, accentColor)
  for (const [k, v] of Object.entries(vars)) {
    d.setProperty(k, v)
  }
  document.body.style.backgroundColor = isDark ? `rgba(38,38,38,${a})` : '#f7f7f7'
}
