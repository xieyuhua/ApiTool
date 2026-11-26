// 导出下载适配层。
//
// 桌面端：调用 Go 方法弹系统保存对话框，返回保存路径。
// 网页/App 端：没有文件对话框，改为请求服务端 /export/* 端点，
//   由浏览器按 Content-Disposition 附件下载。
//
// 判定：window.__WAILS_V3__ 为真 = 桌面端；否则网页/App。
import { apiBase, getAccessToken } from './auth'

export function isDesktop() {
  return typeof window !== 'undefined' && !!window.__WAILS_V3__
}

// downloadFromServer 触发服务端导出下载。
// path 形如 '/export/doc'，params 为查询参数对象。
export async function downloadFromServer(path, params = {}) {
  const qs = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== null) qs.set(k, String(v))
  }
  const url = apiBase() + path + (qs.toString() ? '?' + qs.toString() : '')
  const headers = {}
  const tok = getAccessToken && getAccessToken()
  if (tok) headers['Authorization'] = 'Bearer ' + tok

  const res = await fetch(url, { headers, credentials: 'same-origin' })
  if (!res.ok) {
    let msg = '导出失败（HTTP ' + res.status + '）'
    try {
      const d = await res.json()
      if (d && d.error) msg = d.error
    } catch (e) { /* 非 JSON */ }
    throw new Error(msg)
  }
  // 从 Content-Disposition 解析文件名（RFC 5987）
  let filename = 'export'
  const cd = res.headers.get('Content-Disposition') || ''
  const m = cd.match(/filename\*=UTF-8''([^;]+)/i)
  if (m) {
    try { filename = decodeURIComponent(m[1]) } catch (e) { filename = m[1] }
  }
  const blob = await res.blob()
  const objectURL = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = objectURL
  a.download = filename
  a.rel = 'noopener'
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  // 延迟释放，避免部分浏览器下载中断
  setTimeout(() => URL.revokeObjectURL(objectURL), 10000)
  return filename
}

// 统一导出入口：桌面走对话框（返回路径），网页返回下载的文件名。
export async function exportDoc({ dirId = '', apiId = '', format }) {
  if (isDesktop()) {
    const mod = await import('../wailsjs/go/main/App')
    const path = await mod.ExportDoc(dirId, apiId, format)
    return { kind: 'path', value: path }
  }
  const name = await downloadFromServer('/export/doc', { dirId, apiId, format })
  return { kind: 'file', value: name }
}

export async function exportTestReport({ reportJSON, format }) {
  if (isDesktop()) {
    const mod = await import('../wailsjs/go/main/App')
    const path = await mod.ExportTestReport(reportJSON, format)
    return { kind: 'path', value: path }
  }
  const name = await downloadFromServer('/export/test-report', { report: reportJSON, format })
  return { kind: 'file', value: name }
}

export async function exportStressReport({ reportJSON, format }) {
  if (isDesktop()) {
    const mod = await import('../wailsjs/go/main/App')
    const path = await mod.ExportStressReport(reportJSON, format)
    return { kind: 'path', value: path }
  }
  const name = await downloadFromServer('/export/stress-report', { report: reportJSON, format })
  return { kind: 'file', value: name }
}

export async function exportCapturedOpenAPI({ ids, title }) {
  if (isDesktop()) {
    const mod = await import('../wailsjs/go/main/App')
    const path = await mod.ExportCapturedOpenAPI(ids, title)
    return { kind: 'path', value: path }
  }
  const name = await downloadFromServer('/export/captured-openapi', { ids: ids.join(','), title })
  return { kind: 'file', value: name }
}
