// noVNC 客户端最小类型声明(官方包未随包发布类型)
declare module '@novnc/novnc' {
  export interface RfbOptions {
    shared?: boolean
    credentials?: { username?: string; password?: string; target?: string }
    wsProtocols?: string[]
  }

  export default class RFB extends EventTarget {
    constructor(target: HTMLElement | Document | ShadowRoot, urlOrChannel: string | WebSocket, options?: RfbOptions)
    scaleViewport: boolean
    resizeSession: boolean
    clipViewport: boolean
    viewOnly: boolean
    background: string
    focus(options?: { preventScroll?: boolean }): void
    blur(): void
    sendCtrlAltDel(): void
    sendClipboard(text: string): void
    disconnect(): void
  }
}
