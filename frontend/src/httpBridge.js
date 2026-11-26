// 局域网 Web 模式的 Wails 兼容桥接层。
//
// 桌面端运行时由 Wails 注入 window.go（后端方法）与 window.runtime（事件、日志）。
// 在局域网网页里这两个对象不存在，导致 wailsjs 封装与各组件无法调用后端。
// 本文件用 HTTP(SSE) 重新实现这两个对象，接口与 Wails 完全一致，
// 因此前端所有组件（Agent、接口调试、抓包、设置……）无需任何改动即可在网页中运行，
// 界面与功能与桌面端完全一致。
//
// 判定：window.runtime 缺失即视为 Web 模式（浏览器预览 / 局域网网页）。

// 仅在浏览器（无 Wails 运行时）环境下安装。
//
// 判定顺序很重要：Wails v3 的 runtime.js 是异步注入的，进入 main.js 时
// window.wails / window.runtime 可能都还不存在，此时若只检查 window.runtime
// 会误判为「浏览器预览」，把 HTTP/SSE 桥装上去并覆盖真正的 v3 绑定
// （表现为所有后端调用 404）。因此：
//   1) 已经由 v3 兼容桥接管 -> 不安装；
//   2) v3 的 window.wails 已在 -> 不安装；
//   3) v2 桌面端（window.runtime + window.go 且无 __WEBUI__）-> 不安装。
export function installWebBridge() {
  if (window.__WAILS_V3__) return false
  if (window.wails && window.wails.Call) return false
  if (window.runtime && window.go && !window.__WEBUI__) return false
  window.__WEBUI__ = true
  installGoBridge()
  installRuntimeBridge()
  return true
}

// ---------------------------------------------------------------- window.go
// wailsjs/go/main/App.js 里的每个方法最终都调用 window.go.main.App.X(args...)
// 用 Proxy 懒生成任意方法，方法名即 RPC 路径 /rpc/{Method}
// 浏览器本地可实现的 Wails 方法：同名覆写，不走 HTTP RPC。
// 桌面端这些方法依赖系统剪贴板/文件对话框；网页端浏览器都有等价能力。
const localMethods = {
  // 剪贴板：用 navigator.clipboard（非安全上下文回退 execCommand）
  CopyToClipboard: (text) => writeClipboard(String(text ?? '')),
  SetClipboardText: (text) => writeClipboard(String(text ?? '')),
  ClipboardSetText: (text) => writeClipboard(String(text ?? '')),
  GetClipboardText: () => readClipboard(),
  // 文档 Markdown：服务端渲染后由浏览器写入剪贴板
  CopyDocMarkdown: async (dirID, apiID) => {
    const md = await callRPC('DocMarkdown', [dirID || '', apiID || ''])
    return writeClipboard(md || '')
  },
  // 分享网页源码/标题：服务端渲染（结构跟随文档模板）
  BuildSharedHTML: (dirID, apiID) => callRPC('ShareDocHTML', [dirID || '', apiID || '']),
  BuildSharedTitle: (dirID, apiID) => callRPC('ShareDocTitle', [dirID || '', apiID || '']),
  // 文档导入：浏览器选文件 → 内容上传服务端解析
  ImportDoc: () => importDocViaBrowser(),
  // 分享链接：网页端走服务端落库（桌面端是进程内内存态）
  CreateShareLink: (dirID, apiID, password, expireMinutes) => createShare(dirID, apiID, password, expireMinutes),
  ListShares: () => listShares(),
  StopShare: (token) => stopShare(token),
  // 预览分享文档：服务端渲染 HTML，新标签页以 blob 打开（桌面端写临时文件）
  ShareDoc: async (dirID, apiID) => {
    const html = await callRPC('ShareDocHTML', [dirID || '', apiID || ''])
    const url = URL.createObjectURL(new Blob([html || ''], { type: 'text/html;charset=utf-8' }))
    window.open(url, '_blank', 'noopener')
    setTimeout(() => URL.revokeObjectURL(url), 60000)
    return '分享文档预览'
  },
  // 内置同步服务作为分享后端：网页端没有该进程，返回未运行让对话框走服务端分享
  SyncShareBackend: () => Promise.resolve({ url: '', token: '', running: false, publicUrl: '' }),
  // 「按最新模板刷新本地链接」是桌面端内存分享的概念；网页端链接落库为发布时快照
  RefreshShares: () => Promise.resolve(0),
  // 打开链接 / 预览：浏览器新标签页
  OpenInBrowser: (url) => { window.open(url, '_blank', 'noopener'); return Promise.resolve(null) },
  // 导出水印预览：服务端渲染 HTML，前端在新标签页以 blob 打开
  PreviewDocWatermark: async (dirID, apiID) => {
    const html = await callRPC('PreviewWatermarkHTML', [dirID || '', apiID || ''])
    const url = URL.createObjectURL(new Blob([html || ''], { type: 'text/html;charset=utf-8' }))
    window.open(url, '_blank', 'noopener')
    setTimeout(() => URL.revokeObjectURL(url), 60000)
    return '水印预览'
  },
  // 桌面端独占但网页端可安全 no-op 的能力
  NotifyUpdated: () => Promise.resolve(null),
}

// ---- 分享链接（服务端落库版）----
async function createShare(dirID, apiID, password, expireMinutes) {
  const base = apiBaseURL()
  const res = await fetch(base + '/api/share', {
    method: 'POST',
    credentials: 'same-origin',
    headers: await authHeaders(),
    body: JSON.stringify({ dirId: dirID || '', apiId: apiID || '', password: password || '', expireMinutes: Number(expireMinutes) || 0 }),
  })
  const d = await res.json().catch(() => null)
  if (!res.ok || (d && d.ok === false)) throw new Error((d && d.error) || ('分享失败 HTTP ' + res.status))
  const token = (d && d.result && d.result.token) || ''
  // 拼绝对地址，便于复制给他人
  return (window.location.origin || base) + '/s/' + token
}

async function listShares() {
  const base = apiBaseURL()
  const res = await fetch(base + '/api/share', { credentials: 'same-origin', headers: await authHeaders() })
  if (!res.ok) return []
  const d = await res.json().catch(() => null)
  return (d && d.result) || []
}

async function stopShare(token) {
  const base = apiBaseURL()
  const res = await fetch(base + '/api/share/' + encodeURIComponent(token), {
    method: 'DELETE', credentials: 'same-origin', headers: await authHeaders(),
  })
  const d = await res.json().catch(() => null)
  if (!res.ok || (d && d.ok === false)) throw new Error((d && d.error) || ('停止分享失败 HTTP ' + res.status))
  return null
}

function installGoBridge() {
  const cache = new Map()
  const make = (method) => (...args) => callRPC(method, args)

  const app = new Proxy({}, {
    get(_, prop) {
      if (typeof prop !== 'string') return undefined
      if (Object.prototype.hasOwnProperty.call(localMethods, prop)) return localMethods[prop]
      if (!cache.has(prop)) cache.set(prop, make(prop))
      return cache.get(prop)
    },
  })
  window.go = { main: { App: app } }
}

// importDocViaBrowser 弹出浏览器文件选择框，把内容交给服务端解析导入。
// 返回导入结果描述（与桌面端 ImportDoc 的返回语义一致）。
function importDocViaBrowser() {
  return new Promise((resolve, reject) => {
    const input = document.createElement('input')
    input.type = 'file'
    input.accept = '.json,.yaml,.yml'
    input.style.display = 'none'
    input.onchange = async () => {
      const f = input.files && input.files[0]
      document.body.removeChild(input)
      if (!f) { reject(new Error('User cancelled')); return }
      try {
        const text = await f.text()
        resolve(await callRPC('ImportDocContent', [f.name, text]))
      } catch (e) { reject(e) }
    }
    document.body.appendChild(input)
    input.click()
  })
}

// apiBase 解析服务端地址：显式注入（window.__API_BASE__）优先，
// 其次是 App 端/用户配置写入的 localStorage('apitool_server_base')。
// 网页端两者皆空 → 返回 '' 即同源相对路径。
function apiBaseURL() {
  if (typeof window === 'undefined') return ''
  if (window.__API_BASE__) return window.__API_BASE__
  try { return localStorage.getItem('apitool_server_base') || '' } catch (e) { return '' }
}

// authHeaders 构造请求头：始终 JSON；有 accessToken 时附带 Bearer。
// 网页端（同源）主要靠 HttpOnly Cookie，Bearer 冗余无害；
// App 端（跨域）Cookie 不发送，必须走 Bearer。auth.js 为动态导入避免循环依赖。
// clientId：本端（标签页/App 实例）的稳定标识，服务端用它区分"是谁保存的"，
// 避免把自己的保存通知又推回给自己。
// 用 sessionStorage（按标签页隔离）：localStorage 在同浏览器多标签间共享，
// 会导致同一浏览器的第二个标签页把另一标签的保存误判为"自己发的"而丢失提醒。
const CLIENT_ID_KEY = 'apitool_client_id'
function clientId() {
  try {
    let v = sessionStorage.getItem(CLIENT_ID_KEY)
    if (!v) {
      v = 'c_' + Date.now().toString(36) + '_' + Math.random().toString(36).slice(2, 10)
      sessionStorage.setItem(CLIENT_ID_KEY, v)
    }
    return v
  } catch (e) { return '' }
}
export { clientId }

async function authHeaders() {
  const h = { 'Content-Type': 'application/json' }
  if (clientId()) h['X-Client-Id'] = clientId()
  try {
    const mod = await import('./auth')
    const tok = mod.getAccessToken && mod.getAccessToken()
    if (tok) h['Authorization'] = 'Bearer ' + tok
  } catch (e) { /* ignore */ }
  return h
}

async function callRPC(method, args) {
  const base = apiBaseURL()
  const res = await fetch(base + '/rpc/' + encodeURIComponent(method), {
    method: 'POST',
    credentials: 'same-origin',
    headers: await authHeaders(),
    body: JSON.stringify(args || []),
  })
  let data = null
  try { data = await res.json() } catch (e) { /* 非 JSON 响应 */ }
  // 401：accessToken 过期。httpBridge 不直接依赖 auth.js（避免循环），
  // 抛出标记错误由 store 启动流程统一刷新；这里先尝试一次 refresh 重试。
  if (res.status === 401) {
    const mod = await import('./auth')
    try {
      await mod.refreshAccessToken()
      const retry = await fetch(base + '/rpc/' + encodeURIComponent(method), {
        method: 'POST',
        credentials: 'same-origin',
        headers: await authHeaders(),
        body: JSON.stringify(args || []),
      })
      let rd = null
      try { rd = await retry.json() } catch (e) { /* ignore */ }
      if (retry.ok && rd && rd.ok !== false) return rd ? rd.result : null
      if (retry.status === 401) throw new mod.AuthRequired()
      throw new Error((rd && rd.error) || '请求失败（HTTP ' + retry.status + '）')
    } catch (e) {
      if (e && e.name === 'AuthRequired') throw e
      throw e
    }
  }
  if (!res.ok) throw new Error((data && data.error) || ('请求失败（HTTP ' + res.status + '）'))
  if (data && data.ok === false) throw new Error(data.error || '调用失败')
  return data ? data.result : null
}

// ---------------------------------------------------------------- window.runtime
// 事件通过 SSE(/events) 推送，与 Wails 的 EventsOn/EventsOff 语义保持一致。
// 另将几个可浏览器本地实现的能力（剪贴板/打开链接）直接落在前端，
// 避免为了它们去弹桌面端对话框。
function installRuntimeBridge() {
  const handlers = new Map() // eventName -> Set<callback>
  let es = null
  let closed = false

  function dispatch(event, data) {
    const set = handlers.get(event)
    if (!set) return
    for (const cb of Array.from(set)) {
      try { cb(data) } catch (e) { console.error('[webui] 事件处理失败', event, e) }
    }
  }

  // 未登录不建立 SSE：/events 需鉴权，登录前连接只会产生 401 报错噪声。
  // 登录成功（auth.js 写入 refreshToken）后由下次重试自动接上，无需显式通知。
  let authWait = null
  function hasAuthHint() {
    try { return !!localStorage.getItem('apitool_refresh') } catch (e) { return false }
  }

  function connect() {
    if (closed) return
    if (!hasAuthHint()) {
      // 未登录：低频轮询等待登录，登录后自然接上
      if (!authWait) authWait = setTimeout(() => { authWait = null; connect() }, 5000)
      return
    }
    const base = apiBaseURL()
    es = new EventSource(base + '/events', { withCredentials: true })
    es.onmessage = (e) => {
      let msg
      try { msg = JSON.parse(e.data) } catch (err) { return }
      dispatch(msg.event, msg.data)
    }
    es.onerror = () => {
      // EventSource 自带重连；连接彻底失败时兜底重建。
      // 401（登录过期）时 EventSource 不暴露状态码，重建失败同样走此分支；
      // 由 RPC 层的 401 → refresh 流程恢复 Cookie 后这里自然重连成功。
      // 已登出（无 refreshToken）时不再重连，避免持续 401。
      if (es) { es.close(); es = null }
      if (!closed) setTimeout(connect, hasAuthHint() ? 3000 : 5000)
    }
  }
  connect()

  const log = (level) => (...a) => console[level]('[webui]', ...a)

  window.runtime = {
    // ---- 事件 ----
    // maxCallbacks > 0 时，回调被调用该次数后自动注销（与 Wails 语义一致）。
    EventsOnMultiple(eventName, callback, maxCallbacks) {
      if (!handlers.has(eventName)) handlers.set(eventName, new Set())
      const set = handlers.get(eventName)
      let left = maxCallbacks > 0 ? maxCallbacks : -1
      const wrapped = (data) => {
        callback(data)
        if (left > 0 && --left === 0) set.delete(wrapped)
      }
      set.add(wrapped)
      return () => set.delete(wrapped)
    },
    EventsOn(eventName, callback) {
      return window.runtime.EventsOnMultiple(eventName, callback, -1)
    },
    EventsOnce(eventName, callback) {
      return window.runtime.EventsOnMultiple(eventName, callback, 1)
    },
    // 与 Wails 一致：EventsOff 只按事件名移除该事件的全部监听。
    EventsOff(eventName) {
      if (eventName) handlers.delete(eventName)
    },
    EventsOffAll() { handlers.clear() },
    EventsEmit() { /* 网页端无需向桌面端反向发事件 */ },

    // ---- 日志（对齐 Wails 的 runtime.Log* 接口）----
    LogPrint: log('log'), LogTrace: log('debug'), LogDebug: log('debug'),
    LogInfo: log('info'), LogWarning: log('warn'), LogError: log('error'),
    LogFatal: log('error'),

    // ---- 浏览器本地实现，避免依赖桌面窗口 ----
    ClipboardGetText() { return readClipboard() },
    ClipboardSetText(text) { return writeClipboard(text) },
    BrowserOpenURL(url) { window.open(url, '_blank', 'noopener') },
  }
}

function writeClipboard(text) {
  // 优先异步剪贴板 API；权限被拒（非用户手势触发/策略限制）时回退 execCommand，
  // 否则「复制」按钮在部分浏览器/无 HTTPS 环境下会直接报错。
  if (navigator.clipboard && navigator.clipboard.writeText) {
    return navigator.clipboard.writeText(text).catch(() => legacyCopy(text))
  }
  return legacyCopy(text)
}

function legacyCopy(text) {
  return new Promise((resolve, reject) => {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    let ok = false
    try { ok = document.execCommand('copy') } catch (e) { ok = false }
    document.body.removeChild(ta)
    ok ? resolve() : reject(new Error('复制失败：浏览器不允许写入剪贴板'))
  })
}

function readClipboard() {
  if (navigator.clipboard && navigator.clipboard.readText) return navigator.clipboard.readText()
  return Promise.reject(new Error('浏览器不允许读取剪贴板'))
}
