// useSessionAuth.ts — 会话连接认证流程(SSH 指纹/凭证、SFTP、HTTP、串口)。
// 从 TabPane.vue 抽离:连接流程与指纹/凭证/浏览器弹窗状态集中管理,
// 终端相关操作(termWrite/initTerminal)与 SFTP 面板通过参数注入,不直接依赖组件实现。
import { ref, nextTick } from 'vue'
import { useMessage } from 'naive-ui'
import type { Pane, Tab } from '../components/tabTypes'
import { ConnectToSession, ConnectToSessionWithCreds, SetSessionBrowser } from '../../bindings/changeme/internal/services/sessionfileservice.js'
import { CheckFingerprint, HasPassword, GetSessionUsername, SaveCredentials, SavePermanentHostKey, SaveTempHostKey, SkipFingerprint } from '../../bindings/changeme/internal/services/sshservice.js'
import { Connect as SerialConnect } from '../../bindings/changeme/internal/services/serialservice.js'
import { OpenUrl as BrowserOpenUrl, ScanBrowsers as BrowserScanBrowsers } from '../../bindings/changeme/internal/services/browserservice.js'

export interface AuthDeps {
  pane: Pane
  termWrite: (tab: Tab, text: string) => void
  initTerminal: (tab: Tab) => void
  openSftpPanel: (connID: string, title: string, disconnectSshOnClose?: boolean) => void
}

export type Creds = { username: string; password: string; temporary: boolean }

export function useSessionAuth(deps: AuthDeps) {
  const message = useMessage()

  // 浏览器不可用提示弹窗
  const showBrowserFail = ref(false)
  const browserFailUrl = ref('')
  const browserFailPath = ref('')
  const browserFailSel = ref('')
  const browserFailOptions = ref<{ label: string; value: string }[]>([])

  // 指纹对话框
  const showFingerprint = ref(false)
  const fingerprintHost = ref('')
  const fingerprintPort = ref(0)
  const fingerprintFolder = ref('')
  const fingerprintKey = ref('')
  const fingerprintStatus = ref<'not_found' | 'mismatch'>('not_found')
  let pendingFingerprintResolve: ((value: boolean) => void) | null = null

  // 凭证对话框
  const showCredentials = ref(false)
  const credHost = ref('')
  const credUsername = ref('')
  const credHasPassword = ref(false)
  const credTitle = ref('SSH 登录凭证')
  let pendingCredentialsResolve: ((value: { username: string; password: string; rememberUser: boolean; rememberPass: boolean } | null) => void) | null = null

  // 密钥登录凭证对话框(仅用户名,无密码)
  const showKeyCredentials = ref(false)
  const keyCredHost = ref('')
  const keyCredUsername = ref('')
  let pendingKeyCredentialsResolve: ((value: { username: string; rememberUser: boolean } | null) => void) | null = null

  async function loadBrowserFailOptions() {
    try {
      const raw = JSON.parse(await BrowserScanBrowsers())
      const list: any[] = Array.isArray(raw) ? raw : []
      browserFailOptions.value = list.map(b => ({
        label: b.id === 'default' ? `${b.name}（系统默认）` : b.name + (b.isDefault ? '（系统默认）' : ''),
        value: b.id,
      }))
    } catch {
      browserFailOptions.value = []
    }
  }

  // SSH 连接流程：认证 → 连接 → 成功回调（密码错误自动重试）。SSH 会话与 SFTP 会话共用。
  async function sshConnectLoop(
    tab: Tab,
    sessionPath: string,
    meta: any,
    onConnected: (creds: Creds) => Promise<void>,
  ): Promise<void> {
    let creds = await sshAuthFlow(tab, sessionPath, meta.authMode === 'key')
    if (!creds) {
      tab.status = 'idle'
      deps.termWrite(tab, '\r\n\x1b[33m连接已取消\x1b[0m\r\n')
      return
    }

    let retryCount = 0
    const maxAuthRetries = 5

    while (true) {
      try {
        if (creds.temporary) {
          await ConnectToSessionWithCreds(sessionPath, tab.id, creds.username, creds.password)
        } else {
          await ConnectToSession(sessionPath, tab.id)
        }
        tab.status = 'connected'
        tab.username = creds.username
        await onConnected(creds)
        return
      } catch (e: any) {
        const msg = e.message || String(e)

        if (msg.includes('too many authentication failures')) {
          deps.termWrite(tab, '\r\n\x1b[31m超过本次连接密码尝试最大次数\x1b[0m\r\n')
          tab.status = 'error'
          return
        }

        // 本地保存的密码无法解密（旧版本/其他设备密钥加密）：要求重新输入并记住
        const decryptFailed = msg.includes('密码无法解密') || msg.includes('未提供 SSH 密码')
        if (decryptFailed) {
          deps.termWrite(tab, '\r\n\x1b[33m本地保存的密码无法使用，请重新输入密码并勾选记住密码\x1b[0m\r\n')
          const newCreds = await askNonEmptyPassword(tab, creds.username, '密码无法使用，请重新输入')
          if (!newCreds) {
            tab.status = 'idle'
            deps.termWrite(tab, '\r\n\x1b[33m连接已取消\x1b[0m\r\n')
            return
          }
          if (newCreds.rememberPass || newCreds.rememberUser) {
            await SaveCredentials(sessionPath, newCreds.username, newCreds.password, newCreds.rememberUser, newCreds.rememberPass)
            creds = { username: newCreds.username, password: '', temporary: false }
          } else {
            creds = { username: newCreds.username, password: newCreds.password, temporary: true }
          }
          continue
        }

        // 真正的密码认证失败：重新输入密码重试
        if (msg.includes('unable to authenticate')) {
          if (retryCount >= maxAuthRetries) {
            deps.termWrite(tab, '\r\n\x1b[31m超过本次连接密码尝试最大次数\x1b[0m\r\n')
            tab.status = 'error'
            return
          }
          retryCount++
          deps.termWrite(tab, '\r\n\x1b[33m密码错误，请重新输入\x1b[0m\r\n')
          const newCreds = await askNonEmptyPassword(tab, creds.username, '密码错误，请重新输入')
          if (!newCreds) {
            tab.status = 'idle'
            deps.termWrite(tab, '\r\n\x1b[33m连接已取消\x1b[0m\r\n')
            return
          }
          creds = { username: newCreds.username, password: newCreds.password, temporary: true }
          continue
        }

        deps.termWrite(tab, `\r\n\x1b[31m${msg}\x1b[0m\r\n`)
        tab.status = 'error'
        return
      }
    }
  }

  // SFTP 会话：复用 SSH 认证流程，连接成功后打开 SFTP 面板标签页（无终端，失败仅提示）
  async function openSftpSession(sessionPath: string, meta: any) {
    const connID = 'sftp://' + Date.now() + '-' + Math.random().toString(36).slice(2, 8)
    const pseudo: Tab = {
      id: connID, sessionPath, title: meta.name || sessionPath, protocol: 'sftp',
      host: meta.host, port: meta.port, status: 'connecting',
      terminal: null, fitAddon: null, logBuffer: '', kind: 'terminal',
    }
    await sshConnectLoop(pseudo, sessionPath, meta, async () => {
      deps.openSftpPanel(connID, pseudo.title, true)
    })
  }

  // HTTP 会话：在所选浏览器的新标签页打开 URL。浏览器不可用时不打开，弹窗让用户重新选择。
  async function openHttpSession(sessionPath: string, meta: any) {
    const url = (meta.url || '').trim()
    if (!url) {
      message.error('会话 URL 为空，请先编辑会话')
      return
    }
    const browser = meta.browser || 'default'
    const err = await BrowserOpenUrl(browser, url)
    if (!err) {
      message.success('已在浏览器新标签页打开')
      return
    }
    // 浏览器不存在或打开失败：不打开，弹窗告知并让用户切换浏览器
    browserFailUrl.value = url
    browserFailPath.value = sessionPath
    browserFailSel.value = browser
    showBrowserFail.value = true
    loadBrowserFailOptions()
  }

  // 浏览器不可用弹窗：重新选择浏览器后重开（选择结果持久化到会话文件）
  async function retryOpenUrlWithBrowser() {
    const path = browserFailPath.value
    const url = browserFailUrl.value
    const browser = browserFailSel.value || 'default'
    showBrowserFail.value = false
    if (!path || !url) return
    try {
      await SetSessionBrowser(path, browser)
    } catch (e: any) {
      message.error('保存浏览器选择失败：' + (e.message || e))
      return
    }
    const err = await BrowserOpenUrl(browser, url)
    if (err) {
      message.error(err)
      return
    }
    message.success('已在浏览器新标签页打开')
  }

  function closeBrowserFail() {
    showBrowserFail.value = false
  }

  // SSH 认证流程：指纹校验 → 凭证输入
  // 返回 { username, password, temporary } 或 null（取消）
  // isKeyAuth 为 true 时跳过密码输入（会话使用密钥登录，凭证内置于会话文件）
  async function sshAuthFlow(tab: Tab, sessionPath: string, isKeyAuth: boolean = false): Promise<Creds | null> {
    // 1. 指纹校验
    const fpResult = await CheckFingerprint(tab.host, tab.port, sessionPath.split('/').slice(0, -1).join('/') || '')
    const fp = JSON.parse(fpResult)

    if (fp.status === 'error') {
      tab.terminal?.write(`\r\n\x1b[31m${fp.message}\x1b[0m\r\n`)
      return null
    }

    if (fp.status === 'not_found' || fp.status === 'mismatch') {
      const folder = sessionPath.split('/').slice(0, -1).join('/') || ''
      const confirmed = await showFingerprintDialog(tab.host, tab.port, fp.key, fp.status, folder)
      if (!confirmed) return null
    }

    // 2. 凭证检查
    const hasPass = await HasPassword(sessionPath)
    const savedUser = await GetSessionUsername(sessionPath)

    if (isKeyAuth) {
      // 密钥登录:用户名已保存则直接使用;未保存则弹专用弹窗(仅输入账户,不涉及密码)
      if (savedUser) {
        return { username: savedUser, password: '', temporary: false }
      }
      const keyCreds = await showKeyCredentialsDialog(tab.host)
      if (!keyCreds) return null
      if (keyCreds.rememberUser && keyCreds.username) {
        await SaveCredentials(sessionPath, keyCreds.username, '', true, false)
        return { username: keyCreds.username, password: '', temporary: false }
      }
      return { username: keyCreds.username, password: '', temporary: true }
    }

    if (savedUser && hasPass) {
      return { username: savedUser, password: '', temporary: false }
    }

    const creds = await showCredentialsDialog(tab.host, savedUser, hasPass)
    if (!creds) return null

    // 勾选了记住 → 持久化到 TOML
    if (creds.rememberUser || creds.rememberPass) {
      await SaveCredentials(sessionPath, creds.username, creds.password, creds.rememberUser, creds.rememberPass)
      return { username: creds.username, password: creds.password, temporary: false }
    }

    // 未勾选记住 → 仅本次连接，不写入文件
    return { username: creds.username, password: creds.password, temporary: true }
  }

  function showFingerprintDialog(host: string, port: number, key: string, status: 'not_found' | 'mismatch', folder: string): Promise<boolean> {
    return new Promise((resolve) => {
      fingerprintHost.value = host
      fingerprintPort.value = port
      fingerprintFolder.value = folder
      fingerprintKey.value = key
      fingerprintStatus.value = status
      showFingerprint.value = true
      pendingFingerprintResolve = resolve
    })
  }

  function onFingerprintConfirm(saveType: 'once' | 'permanent') {
    if (saveType === 'permanent') {
      SavePermanentHostKey(fingerprintHost.value, fingerprintPort.value, fingerprintFolder.value, fingerprintKey.value).catch(() => {})
    } else {
      SaveTempHostKey(fingerprintHost.value, fingerprintPort.value, fingerprintKey.value).catch(() => {})
    }
    pendingFingerprintResolve?.(true)
    pendingFingerprintResolve = null
  }

  function onFingerprintSkip() {
    SkipFingerprint(fingerprintHost.value, fingerprintPort.value, fingerprintKey.value).catch(() => {})
    pendingFingerprintResolve?.(true)
    pendingFingerprintResolve = null
  }

  function onFingerprintCancel() {
    pendingFingerprintResolve?.(false)
    pendingFingerprintResolve = null
  }

  function showCredentialsDialog(host: string, username: string, hasPassword: boolean, title?: string): Promise<{ username: string; password: string; rememberUser: boolean; rememberPass: boolean } | null> {
    return new Promise((resolve) => {
      credHost.value = host
      credUsername.value = username
      credHasPassword.value = hasPassword
      credTitle.value = title || 'SSH 登录凭证'
      showCredentials.value = true
      pendingCredentialsResolve = resolve
    })
  }

  // 密钥登录专用凭证弹窗:仅要求输入账户,不涉及密码输入与密码校验
  function showKeyCredentialsDialog(host: string): Promise<{ username: string; rememberUser: boolean } | null> {
    return new Promise((resolve) => {
      keyCredHost.value = host
      keyCredUsername.value = ''
      showKeyCredentials.value = true
      pendingKeyCredentialsResolve = resolve
    })
  }

  // 循环弹出凭证框，直到用户输入非空密码或取消，防止空密码重复提交陷入死循环
  async function askNonEmptyPassword(tab: Tab, username: string, title: string): Promise<{ username: string; password: string; rememberUser: boolean; rememberPass: boolean } | null> {
    for (let attempt = 0; attempt < 5; attempt++) {
      const data = await showCredentialsDialog(tab.host, username, false, title)
      if (!data) return null
      if (data.password && data.password.length > 0) return data
      deps.termWrite(tab, '\r\n\x1b[33m密码不能为空，请重新输入\x1b[0m\r\n')
    }
    return null
  }

  function onCredentialsSubmit(data: { username: string; password: string; rememberUser: boolean; rememberPass: boolean }) {
    pendingCredentialsResolve?.(data)
    pendingCredentialsResolve = null
  }

  function onCredentialsCancel() {
    pendingCredentialsResolve?.(null)
    pendingCredentialsResolve = null
  }

  function onKeyCredentialsSubmit(data: { username: string; rememberUser: boolean }) {
    pendingKeyCredentialsResolve?.(data)
    pendingKeyCredentialsResolve = null
  }

  function onKeyCredentialsCancel() {
    pendingKeyCredentialsResolve?.(null)
    pendingKeyCredentialsResolve = null
  }

  async function openSerial(portName: string, baudRate: number, dataBits: number, stopBits: string, parity: string) {
    const tabId = 'serial://' + portName + '@' + Date.now()
    const tabData: Tab = { id: tabId, sessionPath: '', title: portName, protocol: 'serial', host: portName, port: baudRate, status: 'connecting', terminal: null, fitAddon: null, logBuffer: '', dataBits, stopBits, parity, kind: 'terminal' }
    deps.pane.tabs.push(tabData); deps.pane.activeTabId = tabId
    nextTick(async () => {
      const tab = deps.pane.tabs.find(t => t.id === tabId)
      if (!tab) return
      deps.initTerminal(tab)
      try {
        await SerialConnect(tabId, portName, baudRate, dataBits, stopBits, parity)
        tab.status = 'connected'
      } catch (e: any) {
        tab.status = 'error'
        tab.terminal?.write(`\r\n\x1b[31m连接失败: ${e.message || e}\x1b[0m\r\n`)
      }
    })
  }

  return {
    showBrowserFail, browserFailUrl, browserFailPath, browserFailSel, browserFailOptions,
    loadBrowserFailOptions, retryOpenUrlWithBrowser, closeBrowserFail,
    showFingerprint, fingerprintHost, fingerprintPort, fingerprintFolder, fingerprintKey, fingerprintStatus,
    onFingerprintConfirm, onFingerprintSkip, onFingerprintCancel,
    showCredentials, credHost, credUsername, credHasPassword, credTitle,
    onCredentialsSubmit, onCredentialsCancel,
    showKeyCredentials, keyCredHost, keyCredUsername,
    onKeyCredentialsSubmit, onKeyCredentialsCancel,
    sshAuthFlow, sshConnectLoop, openSftpSession, openHttpSession, openSerial,
  }
}
