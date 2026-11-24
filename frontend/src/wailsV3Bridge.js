// Wails v3 兼容桥。
//
// 本项目的前端（19 个文件）沿用 Wails v2 自动生成的调用形态：
//   - window.go.main.App.Method(...)       —— 调用后端方法
//   - window.runtime.EventsOn/Off/Emit(...) —— 自定义事件
//
// Wails v3 改为 `window.wails.Call.ByName("main.App.Method", ...)` 与
// `window.wails.Events.On(...)`，形态不同。为避免改动 19 个业务文件，
// 本文件在 v3 运行时之上重建 v2 的两个全局对象，让现有 wailsjs 封装继续工作。
//
// 只在真正运行于 Wails v3 时安装：v2 桌面端、浏览器预览、局域网网页端
// （httpBridge 已提供另一套 window.go / window.runtime）都不受影响。
export function installV3Bridge() {
  if (typeof window === 'undefined') return false
  const wails = window.wails
  if (!wails || !wails.Call || !wails.Events) return false

  installGoBridge(wails)
  installRuntimeBridge(wails)
  // 标记 v3 环境：让 httpBridge 不要误装 HTTP/SSE 桥
  window.__WAILS_V3__ = true
  if (window.__WEBUI__) {
    // 极端时序：httpBridge 抢先装成了「网页模式」，这里已由真实 v3 桥覆盖，
    // 清掉标记，避免 isWebUI() 继续把它当作局域网网页端。
    delete window.__WEBUI__
  }
  return true
}

// 确认当前是否运行在 Wails v3 桌面端。
//
// v3 不像 v2 那样把运行时对象直接注入全局：它只在 /wails/runtime.js 提供
// 运行时脚本，需要页面自行加载。因此不能用「window.wails 是否存在」判断环境，
// 而用「能否取到该脚本」判断：桌面端内建资源服务器会返回 JS，
// 浏览器预览 / 局域网网页则是 404/HTML。
async function detectV3Runtime(timeoutMs = 3000) {
  try {
    const ctrl = new AbortController()
    const timer = setTimeout(() => ctrl.abort(), timeoutMs)
    const res = await fetch('/wails/runtime.js', { method: 'HEAD', signal: ctrl.signal })
    clearTimeout(timer)
    if (!res.ok) return false
    const ct = (res.headers.get('content-type') || '').toLowerCase()
    return ct.includes('javascript')
  } catch (e) {
    return false
  }
}

// 加载 v3 运行时脚本（ES module）。
function loadV3Runtime() {
  if (window.wails && window.wails.Call) return Promise.resolve()
  return new Promise((resolve, reject) => {
    const s = document.createElement('script')
    s.type = 'module'
    s.src = '/wails/runtime.js'
    s.onload = () => resolve()
    s.onerror = () => reject(new Error('加载 /wails/runtime.js 失败'))
    document.head.appendChild(s)
  })
}

// 等待 v3 运行时可用并安装兼容桥。
// 桌面端：先加载 runtime.js，再把 v3 的 Call/Events 映射回 v2 形态；
// 非桌面端：返回 false，由调用方回退到 HTTP/SSE 桥。
export async function installV3BridgeWhenReady() {
  if (installV3Bridge()) return true
  if (!(await detectV3Runtime())) return false
  try {
    await loadV3Runtime()
  } catch (e) {
    console.error('[wails] 加载运行时失败', e)
    return false
  }
  // module 脚本 onload 后全局对象即已就绪，再等待一次以防异步初始化
  for (let i = 0; i < 40; i++) {
    if (installV3Bridge()) return true
    await new Promise(r => setTimeout(r, 50))
  }
  return false
}

// ---------------------------------------------------------------- window.go
// window.go.main.App.<Method>(...args) -> Call.ByName("main.App.<Method>", ...args)
function installGoBridge(wails) {
  const cache = new Map()
  const make = (method) => (...args) => wails.Call.ByName('main.App.' + method, ...args)

  const app = new Proxy({}, {
    get(_, prop) {
      if (typeof prop !== 'string') return undefined
      // 避免被 Promise 误判为 thenable
      if (prop === 'then') return undefined
      if (!cache.has(prop)) cache.set(prop, make(prop))
      return cache.get(prop)
    },
  })
  window.go = { main: { App: app } }
}

// ---------------------------------------------------------------- window.runtime
// v2 的事件 API -> v3 的 wails.Events。
function installRuntimeBridge(wails) {
  const Events = wails.Events
  const log = (level) => (...a) => console[level]('[wails]', ...a)

  window.runtime = {
    // ---- 事件 ----
    // v3 的 On/Once 都返回取消函数，与 v2 语义一致；
    // maxCallbacks 为其它正数时用计数包装实现「调用 N 次后自动注销」。
    EventsOnMultiple(eventName, callback, maxCallbacks) {
      if (maxCallbacks === 1) return Events.Once(eventName, callback)
      if (!(maxCallbacks > 0)) return Events.On(eventName, callback)
      let left = maxCallbacks
      let off = null
      const wrapped = (ev) => {
        callback(ev)
        if (--left <= 0 && off) {
          try { off() } catch (e) { /* ignore */ }
          off = null
        }
      }
      off = Events.On(eventName, wrapped)
      return () => { if (off) { try { off() } catch (e) { /* ignore */ } off = null } }
    },
    EventsOn(eventName, callback) {
      return Events.On(eventName, callback)
    },
    EventsOnce(eventName, callback) {
      return Events.Once(eventName, callback)
    },
    // 注意：v2/v3 的 EventsOff 语义都是「按事件名移除该事件的全部监听」，
    // 不接受回调函数。项目里有 `EventsOff('sniff:record', offFn)` 这种写法，
    // 若把多余参数当作事件名传给 v3，会误删同名事件（把回调当事件名自然无效）。
    // 这里只按第一个参数的事件名移除，保持与 v2 一致；精确移除应使用
    // EventsOn 返回的取消函数（项目里 AgentLogs 等已如此使用）。
    EventsOff(eventName) {
      if (eventName) Events.Off(eventName)
    },
    EventsOffAll() {
      Events.OffAll()
    },
    EventsEmit(eventName, ...data) {
      // v2 允许无数据；v3 的 Emit 接受 (name, data)
      void Events.Emit(eventName, data.length > 1 ? data : data[0])
    },

    // ---- 日志 ----
    LogPrint: log('log'), LogTrace: log('debug'), LogDebug: log('debug'),
    LogInfo: log('info'), LogWarning: log('warn'), LogError: log('error'),
    LogFatal: log('error'),

    // ---- 剪贴板 / 浏览器（v3 运行时实现）----
    ClipboardGetText() { return window.wails.Clipboard.Text() },
    ClipboardSetText(text) { return window.wails.Clipboard.SetText(text) },
    BrowserOpenURL(url) { return window.wails.Browser.OpenURL(url) },

    // ---- 窗口控制（v3 通过 Window 命名空间；本项目主要走 Go 侧绑定）----
    WindowShow() { return notifyWindow('show') },
    WindowHide() { return notifyWindow('hide') },
    WindowCenter() { return notifyWindow('center') },
    WindowUnminimise() { return notifyWindow('unminimise') },
    WindowSetAlwaysOnTop(b) { return notifyWindow('alwaysOnTop', b) },
  }
}

// 窗口控制在 v3 的 JS API 里按窗口对象操作，且本项目相关动作都由 Go 侧
// （wailsrt）完成；这里保留空实现，避免前端调用时报 undefined。
function notifyWindow() {
  return Promise.resolve()
}
