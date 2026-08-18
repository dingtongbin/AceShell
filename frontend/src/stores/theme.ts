import { ref, computed, watch } from 'vue'
import { darkTheme, lightTheme } from 'naive-ui'
import type { GlobalThemeOverrides } from 'naive-ui'

type ThemeMode = 'dark' | 'light' | 'auto'

const themeMode = ref<ThemeMode>('dark')

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

const lightThemeOverrides: GlobalThemeOverrides = {
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
  Modal: {
        color: '#ffffff',
        textColor: '#1a1a1a',
      },
  Dialog: {
        color: '#ffffff',
        textColor: '#1a1a1a',
      },
  Card: {
        color: '#ffffff',
        borderColor: '#e8e8e8',
      },
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
  Empty: {
        textColor: '#888',
      },
  Message: {
        color: '#ffffff',
        textColor: '#1a1a1a',
      },
  Notification: {
        color: '#ffffff',
        textColor: '#1a1a1a',
      },
  Tooltip: {
        color: '#555',
        textColor: '#fff',
      },
  Progress: {
        railColor: '#e8e8e8',
      },
  Slider: {
        railColor: '#e8e8e8',
      },
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

  const theme = computed(() => isDark.value ? darkTheme : lightTheme)
  const themeOverrides = computed(() => isDark.value ? undefined : lightThemeOverrides)

  return {
    isDark,
    themeMode,
    theme,
    themeOverrides,
    toggleTheme,
    setTheme,
    setThemeMode,
    initTheme,
  }
}
