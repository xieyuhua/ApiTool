// 服务端账号体系客户端：注册 / 登录 / 刷新 / 登出。
//
// 与 httpBridge 的关系：httpBridge.callRPC 走 HttpOnly Cookie（服务端登录后种下），
// 本模块在 401 时用 refreshToken 轮换后自动重试一次；App 端（Capacitor）则把
// accessToken 显式带在 Authorization Header（跨域 Cookie 受限）。
//
// token 存储：
//   - refreshToken 持久化 localStorage（apitool_refresh）
//   - accessToken 内存 + Cookie 兜底（服务端登录响应已 Set-Cookie）

const REFRESH_KEY = 'apitool_refresh'
const BASE_KEY = 'apitool_server_base'

// API 基地址：默认同源（网页端）；App 端可配置（设置页写入 localStorage）。
export function apiBase() {
  if (typeof window === 'undefined') return ''
  return window.__API_BASE__ || localStorage.getItem(BASE_KEY) || ''
}

export function setApiBase(url) {
  if (url) localStorage.setItem(BASE_KEY, url.replace(/\/+$/, ''))
  else localStorage.removeItem(BASE_KEY)
}

export function getRefreshToken() {
  try { return localStorage.getItem(REFRESH_KEY) || '' } catch (e) { return '' }
}
export function saveRefreshToken(t) {
  try { t ? localStorage.setItem(REFRESH_KEY, t) : localStorage.removeItem(REFRESH_KEY) } catch (e) { /* ignore */ }
}

// AuthRequired 登录失效信号（store 据此切换到登录页）。
export class AuthRequired extends Error {
  constructor(msg) { super(msg || '需要登录'); this.name = 'AuthRequired' }
}

let refreshing = null

// refreshAccessToken 用 refreshToken 轮换；返回 accessToken 或抛 AuthRequired。
export async function refreshAccessToken() {
  const rt = getRefreshToken()
  if (!rt) throw new AuthRequired('无刷新令牌')
  // 并发 401 只发一次刷新
  if (!refreshing) {
    refreshing = (async () => {
      const res = await fetch(apiBase() + '/api/token/refresh', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ refreshToken: rt }),
      })
      const data = await res.json().catch(() => null)
      if (!res.ok || !data || !data.accessToken || data.ok === false) {
        saveRefreshToken('')
        throw new AuthRequired((data && data.error) || '登录已过期')
      }
      return data
    })().finally(() => { refreshing = null })
  }
  return refreshing
}

// login 登录并持久化 refreshToken。
// 响应格式（服务端 issueSession 直接返回顶层结构，无 {ok,result} 包装）：
//   成功 { accessToken, refreshToken, user:{id,username}, projects:[...] }
//   失败 { ok:false, error:"..." }（writeErr）或 HTTP 4xx/5xx
export async function login(username, password) {
  const res = await fetch(apiBase() + '/api/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  })
  const data = await res.json().catch(() => null)
  if (!res.ok || !data || !data.accessToken || data.ok === false) {
    throw new Error((data && data.error) || '登录失败')
  }
  saveRefreshToken(data.refreshToken)
  return data
}

// register 注册并自动登录（响应格式同 login）。
export async function register(username, password) {
  const res = await fetch(apiBase() + '/api/register', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  })
  const data = await res.json().catch(() => null)
  if (!res.ok || !data || !data.accessToken || data.ok === false) {
    throw new Error((data && data.error) || '注册失败')
  }
  saveRefreshToken(data.refreshToken)
  return data
}

// logout 登出（吊销 refresh + 清理本地）。
export async function logout() {
  try {
    await fetch(apiBase() + '/api/logout', { method: 'POST' })
  } catch (e) { /* ignore */ }
  saveRefreshToken('')
}

// probe 探测服务端是否可达（/api/health）。
export async function probeServer(timeoutMs = 2500) {
  try {
    const ctrl = new AbortController()
    const timer = setTimeout(() => ctrl.abort(), timeoutMs)
    const res = await fetch(apiBase() + '/api/health', { signal: ctrl.signal })
    clearTimeout(timer)
    if (!res.ok) return null
    const data = await res.json()
    return (data && data.ok) ? data : null
  } catch (e) {
    return null
  }
}
