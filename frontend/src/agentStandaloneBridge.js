// 独立 Agent 服务（agentd）的前端桥接层。
//
// 独立服务自带的页面与桌面端「AI Agent」完全同一套组件（agent.html 入口），
// 但后端不是 Wails 也不是主服务端的 /rpc，而是 agentd 的 REST + SSE：
//   - window.go.main.App.X(...)      → 对应的 /api/agent/* 接口
//   - window.runtime.EventsOn(...)   → /api/agent/events 的 SSE
//
// 判定：服务端在 index 注入 window.__AGENT_STANDALONE__=true。
// 桌面端与主服务端不会设置该标记，因此桥接互不干扰。

const BASE = '' // 同源

// 与 core/agent 的公开方法一一对应的 REST 映射。
// call(args) 返回 fetch 的 (path, method, body) 三元组。
const REST = {
  LoadAgentData: { path: '/api/agent/data', method: 'GET' },
  GetAgentBootstrap: { path: '/api/agent/bootstrap', method: 'GET' },

  SaveAgentConfig: { path: '/api/agent/config', method: 'PUT', body: a => a[0] },
  SaveAgentSkills: { path: '/api/agent/skills', method: 'PUT', body: a => a[0] },
  SaveMCPServers: { path: '/api/agent/mcpservers', method: 'PUT', body: a => a[0] },
  SaveAgentUsers: { path: '/api/agent/users', method: 'PUT', body: a => a[0] },

  CreateAgentSession: { path: '/api/agent/sessions', method: 'POST', body: a => ({ title: a[0] || '' }) },
  SwitchAgentSession: { path: a => '/api/agent/sessions/' + enc(a[0]) + '/switch', method: 'POST' },
  RenameAgentSession: { path: a => '/api/agent/sessions/' + enc(a[0]), method: 'PATCH', body: a => ({ title: a[1] || '' }) },
  DeleteAgentSession: { path: a => '/api/agent/sessions/' + enc(a[0]), method: 'DELETE' },
  ClearAgentMessages: { path: '/api/agent/messages/clear', method: 'POST' },

  RunAgent: { path: '/api/agent/run', method: 'POST', body: a => a[0] },
  PolishText: { path: '/api/agent/polish', method: 'POST', body: a => a[0] },

  GetBuiltinTools: { path: '/api/agent/tools', method: 'GET' },
  ListAllMCPTools: { path: '/api/agent/mcp/tools', method: 'GET' },
  TestMCPServer: { path: '/api/agent/mcp/test', method: 'POST', body: a => a[0] },

  QueryAgentLogs: {
    path: a => {
      const q = a[0] || {}
      const p = new URLSearchParams()
      if (q.keyword) p.set('keyword', q.keyword)
      if (q.level) p.set('level', q.level)
      if (q.category) p.set('category', q.category)
      if (q.limit) p.set('limit', String(q.limit))
      if (q.withDetail) p.set('withDetail', '1')
      const s = p.toString()
      return '/api/agent/logs' + (s ? '?' + s : '')
    },
    method: 'GET',
  },
  GetAgentLogFacets: { path: '/api/agent/logs/facets', method: 'GET' },
  GetAgentLog: { path: a => '/api/agent/logs/' + enc(a[0]), method: 'GET' },
  ClearAgentLogs: { path: '/api/agent/logs', method: 'DELETE' },

  BuildSessionHTML: { path: a => '/api/agent/sessions/' + enc(a[0] || '') + '/html', method: 'GET' },
}

function enc(s) { return encodeURIComponent(String(s || '')) }

// SPECIAL 用浏览器能力替代、不走 JSON RPC 的方法。
// 代理对任意属性都返回函数，无法用 defineProperty 覆盖，因此在这里分流。
const SPECIAL = {
  // 导出会话：改为浏览器附件下载，返回提示文案（前端会 toast）
  ExportAgentSession: (sessionID, format) => {
    const url = BASE + '/api/agent/sessions/' + enc(sessionID) + '/export?format=' + enc(format || 'html')
    const a = document.createElement('a')
    a.href = url
    a.rel = 'noopener'
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    return Promise.resolve('会话记录（浏览器下载中）')
  },
  // 桌面端导出后自动打开文件：独立服务已在浏览器下载，无需再打开
  OpenExportFile: () => Promise.resolve(null),
  // 局域网访问开关是桌面端概念：独立服务本身就是网页
  WebChatInfo: () => Promise.resolve({ running: true, addr: location.host, port: '', url: location.origin, public: location.origin, host: location.hostname, token: '', link: location.origin }),
  StartWebChat: () => Promise.resolve({ running: true }),
  StopWebChat: () => Promise.resolve(null),
  ResetWebChatToken: () => Promise.resolve({ running: true }),
}

async function callREST(method, args) {
  if (Object.prototype.hasOwnProperty.call(SPECIAL, method)) return SPECIAL[method](...args)
  const def = REST[method]
  if (!def) throw new Error('独立服务不支持该操作：' + method)
  const path = typeof def.path === 'function' ? def.path(args) : def.path
  const init = { method: def.method, credentials: 'same-origin', headers: {} }
  if (def.body) {
    init.headers['Content-Type'] = 'application/json'
    init.body = JSON.stringify(def.body(args) ?? {})
  }
  const res = await fetch(BASE + path, init)
  let data = null
  try { data = await res.json() } catch (e) { /* 非 JSON */ }
  if (res.status === 401) throw new Error((data && data.error) || '访问令牌无效，请用带 token 的链接重新打开')
  if (!res.ok || (data && data.ok === false)) {
    throw new Error((data && data.error) || ('请求失败（HTTP ' + res.status + '）'))
  }
  return data ? data.result : null
}

// installStandaloneBridge 安装 window.go / window.runtime。
export function installStandaloneBridge() {
  if (typeof window === 'undefined') return false
  if (!window.__AGENT_STANDALONE__) return false
  if (window.__AGENT_STANDALONE_BRIDGE__) return true

  const cache = new Map()
  const make = (method) => (...args) => callREST(method, args)
  const app = new Proxy({}, {
    get(_, prop) {
      if (typeof prop !== 'string') return undefined
      if (!cache.has(prop)) cache.set(prop, make(prop))
      return cache.get(prop)
    },
  })

  window.go = { main: { App: app } }

  // ---- 事件：SSE ----
  const handlers = new Map()
  let es = null
  let closed = false

  function dispatch(event, data) {
    const set = handlers.get(event)
    if (!set) return
    for (const cb of Array.from(set)) {
      try { cb(data) } catch (e) { console.error('[agentd] 事件处理失败', event, e) }
    }
  }

  function connect() {
    if (closed) return
    es = new EventSource(BASE + '/api/agent/events', { withCredentials: true })
    es.onmessage = (e) => {
      let msg
      try { msg = JSON.parse(e.data) } catch (err) { return }
      dispatch(msg.event, msg.data)
    }
    es.onerror = () => {
      // EventSource 自带重连；连接彻底断开时兜底重建
      if (es) { es.close(); es = null }
      if (!closed) setTimeout(connect, 3000)
    }
  }
  connect()

  const log = (level) => (...a) => console[level]('[agentd]', ...a)
  window.runtime = {
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
    EventsOn(eventName, callback) { return window.runtime.EventsOnMultiple(eventName, callback, -1) },
    EventsOnce(eventName, callback) { return window.runtime.EventsOnMultiple(eventName, callback, 1) },
    EventsOff(eventName) { if (eventName) handlers.delete(eventName) },
    EventsOffAll() { handlers.clear() },
    EventsEmit() { /* 独立服务无需反向发事件 */ },
    LogPrint: log('log'), LogTrace: log('debug'), LogDebug: log('debug'),
    LogInfo: log('info'), LogWarning: log('warn'), LogError: log('error'), LogFatal: log('error'),
    ClipboardGetText() { return Promise.reject(new Error('请使用浏览器剪贴板')) },
    ClipboardSetText(text) { return writeClipboard(text) },
    BrowserOpenURL(url) { window.open(url, '_blank', 'noopener') },
  }

  window.__AGENT_STANDALONE_BRIDGE__ = true
  return true
}

function writeClipboard(text) {
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

// isStandalone 当前是否运行在独立 Agent 服务中。
export function isStandalone() {
  return typeof window !== 'undefined' && !!window.__AGENT_STANDALONE__
}
