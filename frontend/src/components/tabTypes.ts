import type { Component } from 'vue'
import type { Terminal } from '@xterm/xterm'
import type { FitAddon } from '@xterm/addon-fit'

export interface Tab {
  id: string
  title: string
  kind: 'terminal' | 'component'
  sessionPath: string
  protocol: string
  host: string
  port: number
  username?: string
  status: 'idle' | 'connecting' | 'connected' | 'error'
  terminal: Terminal | null
  fitAddon: FitAddon | null
  logBuffer: string
  dataBits?: number
  stopBits?: string
  parity?: string
  component?: Component
  componentProps?: Record<string, any>
  onClose?: () => boolean | Promise<boolean>
  dirty?: boolean
  icon?: Component
  color?: string
  terminalRebuild?: boolean
  terminalCleanup?: () => void
}

export interface Pane {
  id: string
  isMain: boolean
  focused: boolean
  tabs: Tab[]
  activeTabId: string | null
}

// 活动标签页状态快照(供顶级菜单按活动标签页启用/禁用工具)
export interface ActiveTabState {
  hasTab: boolean
  isTerminal: boolean
  protocol: string
  connected: boolean
}

export interface ComponentTabOptions {
  title: string
  component: Component
  props?: Record<string, any>
  icon?: Component
  color?: string
  status?: 'idle' | 'connecting' | 'connected' | 'error'
  dirty?: boolean
  onClose?: () => boolean | Promise<boolean>
}

export interface ComponentTabPatch {
  title?: string
  props?: Record<string, any>
  status?: 'idle' | 'connecting' | 'connected' | 'error'
  dirty?: boolean
}

export interface TabPaneApi {
  openSession: (sessionPath: string) => Promise<string>
  openSerial: (portName: string, baudRate: number, dataBits: number, stopBits: string, parity: string) => void
  openSftp: (tab?: Tab) => void
  openScriptDialog: (tab?: Tab) => void
  exportLog: (tab?: Tab) => void
  clearScrollback: (tab?: Tab) => void
  clearScreen: (tab?: Tab) => void
  getActiveSessionPath: () => string | null
  openComponentTab: (opts: ComponentTabOptions) => string
  updateComponentTab: (tabId: string, patch: ComponentTabPatch) => void
  closeTabById: (tabId: string) => void
  activateTab: (tabId: string) => void
  reportCursor: (row: number, col: number) => void
  copySelection: () => Promise<void>
  pasteClipboard: () => Promise<void>
  // MCP 桥接扩展: 与用户操作完全相同的路径执行 MCP 命令
  // activateTab=false 时(批量执行等后台化操作)不切换标签页
  mcpTerminalSend: (tabId: string, text: string, needPasteConfirm: boolean, activateTab?: boolean) => Promise<{ ok: boolean; note?: string }>
  mcpCloseTab: (tabId: string, activateTab?: boolean) => Promise<{ ok: boolean; note?: string }>
}

export type SplitDir = 'h' | 'v'

// 跨 pane 共享的拖拽状态(模块单例):dataTransfer.getData 在 dragover 期间不可靠,
// 拖拽源信息统一走该状态,dragend 时由源 pane 清空
export const dragState = {
  tabId: '',
  srcPaneId: '',
}

export interface PaneNode {
  type: 'pane'
  paneId: string
}

export interface SplitNode {
  type: 'split'
  dir: SplitDir
  ratio: number
  a: LayoutNode
  b: LayoutNode
}

export type LayoutNode = PaneNode | SplitNode

export interface PaneActions {
  onSplit: (paneId: string, tabId: string, dir: SplitDir) => void
  onMoveTab: (tabId: string, targetPaneId: string, index?: number) => void
  onSplitAt: (tabId: string, targetPaneId: string, dir: SplitDir) => void
  onFocus: (paneId: string) => void
  openRdp: (meta: { sessionPath: string; name: string; host: string; port: number }) => void
  onStatus: (paneId: string, text: string, row: number, col: number, encoding: string, hasTab: boolean) => void
  onActiveTabState: (paneId: string, state: ActiveTabState) => void
  registerPane: (paneId: string, api: TabPaneApi) => () => void
  paneExists: (paneId: string) => boolean
}

export interface PaneCtx {
  actions: PaneActions
}

export interface PassProps {
  showToolbar: boolean
  isVertical: boolean
  verticalWidth: number
  termCfg: any
  showWelcomePaneId: string | null
}