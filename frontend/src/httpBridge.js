// 局域网 Web 模式的 Wails 兼容桥接层。
//
// 桌面端运行时由 Wails 注入 window.go（后端方法）与 window.runtime（事件、日志）。
// 在局域网网页里这两个对象不存在，导致 wailsjs 封装与各组件无法调用后端。
// 本文件用 HTTP(SSE) 重新实现这两个对象，接口与 Wails 完全一致，
// 因此前端所有组件（Agent、接口调试、抓包、设置……）无需任何改动即可在网页中运行，
// 界面与功能与桌面端完全一致。
//
// 判定：window.runtime 缺失即视为 Web 模式（浏览器预览 / 局域网网页）。

// 仅在浏览器（无 Wails 运行时）环境下安装
export function installWebBridge() {
  if (window.runtime && window.go) return false
  window.__WEBUI__ = true
  installGoBridge()
  installRuntimeBridge()
  return true
}

// ---------------------------------------------------------------- window.go
// wailsjs/go/main/App.js 里的每个方法最终都调用 window.go.main.App.X(args...)
// 用 Proxy 懒生成任意方法，方法名即 RPC 路径 /rpc/{Method}
function installGoBridge() {
  const cache = new Map()
  const make = (method) => (...args) => callRPC(method, args)

  const app = new Proxy({}, {
    get(_, prop) {
      if (typeof prop !== 'string') return undefined
      if (!cache.has(prop)) cache.set(prop, make(prop))
      return cache.get(prop)
    },
  })
  window.go = { main: { App: app } }
}

async function callRPC(method, args) {
  const res = await fetch('/rpc/' + encodeURIComponent(method), {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(args || []),
  })
  let data = null
  try { data = await res.json() } catch (e) { /* 非 JSON 响应 */ }
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

  function connect() {
    if (closed) return
    es = new EventSource('/events', { withCredentials: true })
    es.onmessage = (e) => {
      let msg
      try { msg = JSON.parse(e.data) } catch (err) { return }
      dispatch(msg.event, msg.data)
    }
    es.onerror = () => {
      // EventSource 自带重连；连接彻底失败时兜底重建
      if (es) { es.close(); es = null }
      if (!closed) setTimeout(connect, 3000)
    }
  }
  connect()

  const log = (level) => (...a) => console[level]('[webui]', ...a)

  window.runtime = {
    // ---- 事件 ----
    EventsOnMultiple(eventName, callback, maxCallbacks) {
      if (!handlers.has(eventName)) handlers.set(eventName, new Set())
      const set = handlers.get(eventName)
      set.add(callback)
      return () => set.delete(callback)
    },
    EventsOn(eventName, callback) {
      return window.runtime.EventsOnMultiple(eventName, callback, -1)
    },
    EventsOnce(eventName, callback) {
      const off = window.runtime.EventsOnMultiple(eventName, callback, 1)
      setTimeout(() => { if (off) off() }, 0)
      return off
    },
    EventsOff(eventName, ...rest) {
      if (rest.length) {
        rest.forEach(n => handlers.delete(n))
        return
      }
      handlers.delete(eventName)
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
  if (navigator.clipboard && window.isSecureContext) return navigator.clipboard.writeText(text)
  return new Promise((resolve, reject) => {
    const ta = document.createElement('textarea')
    ta.value = text
    document.body.appendChild(ta)
    ta.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    ok ? resolve() : reject(new Error('复制失败'))
  })
}

function readClipboard() {
  if (navigator.clipboard && navigator.clipboard.readText) return navigator.clipboard.readText()
  return Promise.reject(new Error('浏览器不允许读取剪贴板'))
}
