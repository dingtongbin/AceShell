import type RFB from '@novnc/novnc'

// VNC 连接信息(由后端 GetVncConnection 下发,含明文密码与一次性桥接令牌)。
// 桥接走 wsbridge 普通透传路径(RFB 为裸 TCP 流),令牌绑定目标防 SSRF。
export interface VncConnectionInfo {
  host: string
  port: number
  username: string
  password: string
  authToken: string
  bridgeWsUrl: string
}

// 在目标容器上建立 noVNC 会话。noVNC 为纯 JS 客户端,无独立 WASM 运行时,无需预热。
export async function connectVnc(target: HTMLElement, conn: VncConnectionInfo): Promise<RFB> {
  const { default: RFB } = await import('@novnc/novnc')
  const rfb = new RFB(target, conn.bridgeWsUrl, {
    shared: true,
    credentials: { username: conn.username || '', password: conn.password || '' },
  })
  // scaleViewport:画布等比缩放适配容器;resizeSession:服务端支持 DesktopResize
  // 伪编码时把远程分辨率协商为容器大小,不支持时由 scaleViewport 兜底缩放。
  rfb.scaleViewport = true
  rfb.resizeSession = true
  rfb.background = 'var(--bg-color, #1e1e1e)'
  return rfb
}
