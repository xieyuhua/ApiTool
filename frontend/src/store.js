import { reactive, watch, ref } from 'vue'
import * as runtime from '../wailsjs/runtime/runtime'
import { LoadData, SaveData, GetVersion, CheckUpdate, GetClipboardText, CallAI, ClearTestData, GetClipItems, CopyClipItem, DeleteClipItem, ClearClipHistory, GetClipImageData } from '../wailsjs/go/main/App'

export function uid() {
  return (crypto.randomUUID ? crypto.randomUUID() : Date.now() + '-' + Math.random().toString(16).slice(2))
}

export function newKV() {
  return { key: '', value: '', description: '', enabled: true }
}

export function newEnvVar() {
  return { key: '', value: '', enabled: true }
}

export function newField() {
  return { name: '', type: 'string', required: false, example: '', description: '', children: [] }
}

export function normalizeApi(api) {
  api.headers ||= []
  api.query ||= []
  api.formItems ||= []
  api.reqFields ||= []
  api.respFields ||= []
  api.bodyType ||= 'json'
  api.method ||= 'GET'
  api.contentType ||= ''
  api.preScript ||= ''
  api.postScript ||= ''
  normalizeFields(api.reqFields)
  normalizeFields(api.respFields)
  return api
}

function normalizeFields(fields) {
  for (const f of fields) {
    f.children ||= []
    normalizeFields(f.children)
  }
}

export const store = reactive({
  loaded: false,
  loading: false, // 初始化数据加载中（全屏遮罩）
  treeLoading: false, // 切换项目 / 重建目录时的局部加载
  view: 'workspace', // workspace | docs | settings
  currentApiId: '',
  activeTab: 'debug', // debug | params | doc
  // 接口调试多标签：每个标签绑定一个接口，支持同时打开多个接口窗口（类似浏览器标签）
  openTabs: [], // [{ id, apiId, sub }]
  activeTabId: '', // 当前激活的标签 id
  appVersion: '', // 客户端版本号（来自 Go 端）
  // 轻量修订计数器：任何需要持久化的修改 +1，替代对整个 store.data 的 deep watch，
  // 避免每次按键都深度遍历全量数据（性能关键）。
  saveRev: 0,
  data: {
    projects: [
      { id: 'default', name: '默认项目', dirs: [], apis: [], environments: [], activeEnvId: '', updatedAt: '', common: { headers: [], query: [] }, testCases: [], testPlans: [], testReports: [] },
    ],
    currentProjectId: 'default',
    settings: {
      aiBaseUrl: '', aiKey: '', aiModel: '', timeoutSec: 30, cloudURL: '', cloudToken: '', cloudUser: '', autoSync: false, version: '1.0.0', updateURL: 'http://127.0.0.1:8080',
      theme: 'light',            // light | dark | auto
      scheme: '',                // 主题方案 id（空=自定义，使用 theme+accent）
      accent: '#165dff',         // 主题色（主色）
      clipboard: { monitor: true, maxItems: 200 },
    },
    plugins: { connections: [] },
    clipboard: { history: [] },
  },
})

// ---------------- 主题 ----------------
export const THEMES = [
  { value: 'light', label: '浅色' },
  { value: 'dark', label: '深色' },
  { value: 'auto', label: '跟随系统' },
]

// 主题方案（预设风格）：每个方案打包 明暗模式 + 主色 + 一套完整配色变量。
// 选择方案会同时写入 settings.theme 与 settings.accent，并叠加 data-scheme 专属变量。
export const SCHEMES = [
  {
    id: 'default', label: '原始亮色', mode: 'light', accent: '#165dff',
    desc: '系统默认浅色主题，干净专业', swatch: '#165dff',
    vars: {},
  },
  {
    id: 'cyber', label: '科技青', mode: 'dark', accent: '#00e0c6',
    desc: '暗夜霓虹青，年轻电竞风', swatch: '#00e0c6',
    vars: {
      '--bg': '#0b0f14', '--bg-soft': '#10161d', '--surface': '#11181f', '--surface-2': '#19222c',
      '--border': '#22303c', '--text': '#d7faf4', '--text-muted': '#6f8a94',
      '--primary': '#00e0c6', '--primary-soft': '#0c2e2b', '--nav-bg': '#06090d', '--nav-hover': '#15212b',
      '--nav-active': '#00e0c6', '--code-bg': '#070b0f', '--code-text': '#9ff3e6',
    },
  },
  {
    id: 'neon', label: '暗夜紫', mode: 'dark', accent: '#8b5cf6',
    desc: '赛博紫光，潮流炫酷', swatch: '#8b5cf6',
    vars: {
      '--bg': '#0f0a16', '--bg-soft': '#160f20', '--surface': '#16101f', '--surface-2': '#211831',
      '--border': '#322241', '--text': '#ece6f7', '--text-muted': '#8a7aa3',
      '--primary': '#8b5cf6', '--primary-soft': '#241733', '--nav-bg': '#080510', '--nav-hover': '#1d1430',
      '--nav-active': '#8b5cf6', '--code-bg': '#0a0612', '--code-text': '#d8ccff',
    },
  },
  {
    id: 'aurora', label: '极光绿', mode: 'dark', accent: '#22d39a',
    desc: '极光薄荷绿，清新自然', swatch: '#22d39a',
    vars: {
      '--bg': '#08110d', '--bg-soft': '#0e1a14', '--surface': '#0f1a14', '--surface-2': '#16271e',
      '--border': '#1f3a2c', '--text': '#dcf5ea', '--text-muted': '#6f9b86',
      '--primary': '#22d39a', '--primary-soft': '#0c2a20', '--nav-bg': '#040907', '--nav-hover': '#13241b',
      '--nav-active': '#22d39a', '--code-bg': '#050c08', '--code-text': '#a7f0d2',
    },
  },
  {
    id: 'sunset', label: '日落橙', mode: 'light', accent: '#ff7d00',
    desc: '暖阳橙调，活力明快', swatch: '#ff7d00',
    vars: {
      '--bg': '#fbf6f1', '--bg-soft': '#f3eae2', '--surface': '#ffffff', '--surface-2': '#faf4ee',
      '--border': '#ecddd0', '--text': '#2b2118', '--text-muted': '#9a8a7d',
      '--primary': '#ff7d00', '--primary-soft': '#fff0e0', '--nav-bg': '#2b1d12', '--nav-hover': '#3a2718',
      '--nav-active': '#ff7d00', '--code-bg': '#2b1d12', '--code-text': '#f3e6da',
    },
  },
  {
    id: 'sakura', label: '樱粉', mode: 'light', accent: '#f759ab',
    desc: '柔和樱粉，甜美清新', swatch: '#f759ab',
    vars: {
      '--bg': '#fdf4f9', '--bg-soft': '#f8e8f1', '--surface': '#ffffff', '--surface-2': '#fdf0f7',
      '--border': '#f3dbe9', '--text': '#3a2531', '--text-muted': '#a98aa0',
      '--primary': '#f759ab', '--primary-soft': '#fde6f3', '--nav-bg': '#2a1224', '--nav-hover': '#3a1933',
      '--nav-active': '#f759ab', '--code-bg': '#2a1224', '--code-text': '#f7d9ec',
    },
  },
  {
    id: 'graphite', label: '石墨灰', mode: 'dark', accent: '#9aa4b2',
    desc: '极简中性灰，沉稳专注', swatch: '#9aa4b2',
    vars: {
      '--bg': '#15171a', '--bg-soft': '#1b1e22', '--surface': '#1d2024', '--surface-2': '#25292e',
      '--border': '#33383f', '--text': '#e6e9ed', '--text-muted': '#8b929c',
      '--primary': '#9aa4b2', '--primary-soft': '#262b31', '--nav-bg': '#0e1012', '--nav-hover': '#23272c',
      '--nav-active': '#9aa4b2', '--code-bg': '#0e1012', '--code-text': '#d5dae0',
    },
  },
]

const SCHEME_MAP = Object.fromEntries(SCHEMES.map(s => [s.id, s]))
// 所有方案可能设置的 CSS 变量键集合；脱离方案（自定义/明暗切换）时需从根元素清除这些内联变量
const SCHEME_VAR_KEYS = [...new Set(SCHEMES.flatMap(s => Object.keys(s.vars || {})))]

// 应用主题方案：叠加 data-scheme 专属变量；无方案（自定义/明暗切换）时清除这些内联变量，
// 否则旧方案的 --bg/--surface 等会继续覆盖浅色，导致亮色无法恢复。
export function applyScheme() {
  const root = document.documentElement
  const id = store.data.settings.scheme
  const scheme = id && SCHEME_MAP[id]
  if (scheme && scheme.vars) {
    root.setAttribute('data-scheme', id)
    for (const k in scheme.vars) root.style.setProperty(k, scheme.vars[k])
  } else {
    root.removeAttribute('data-scheme')
    // 清除此前方案写入的内联变量，让浅色/自定义配色真正生效
    for (const k of SCHEME_VAR_KEYS) root.style.removeProperty(k)
  }
  applyTheme()
}

export function setScheme(id) {
  const scheme = id && SCHEME_MAP[id]
  store.data.settings.scheme = id || ''
  if (scheme) {
    store.data.settings.theme = scheme.mode
    store.data.settings.accent = scheme.accent
  }
  applyScheme()
  saveNow()
}


function hexToRgb(hex) {
  hex = (hex || '').replace('#', '')
  if (hex.length === 3) hex = hex.split('').map(c => c + c).join('')
  const n = parseInt(hex, 16)
  if (isNaN(n)) return null
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255]
}
function rgbToHex(r, g, b) {
  const h = x => Math.max(0, Math.min(255, Math.round(x))).toString(16).padStart(2, '0')
  return '#' + h(r) + h(g) + h(b)
}
function mix(rgb, target, amt) { return rgb.map((c, i) => Math.round(c + (target[i] - c) * amt)) }

// 应用主色：同步写入 Element Plus 主色变量及其衍生明暗档位
export function applyAccent(hex) {
  const root = document.documentElement
  const rgb = hexToRgb(hex)
  if (!rgb) return
  root.style.setProperty('--primary', hex)
  root.style.setProperty('--el-color-primary', hex)
  const map = {
    '--el-color-primary-light-3': mix(rgb, [255, 255, 255], 0.3),
    '--el-color-primary-light-5': mix(rgb, [255, 255, 255], 0.5),
    '--el-color-primary-light-7': mix(rgb, [255, 255, 255], 0.7),
    '--el-color-primary-light-8': mix(rgb, [255, 255, 255], 0.8),
    '--el-color-primary-light-9': mix(rgb, [255, 255, 255], 0.9),
    '--el-color-primary-dark-2': mix(rgb, [0, 0, 0], 0.2),
  }
  for (const k in map) root.style.setProperty(k, rgbToHex(map[k][0], map[k][1], map[k][2]))
}

// 应用主题：写入 data-theme（light/dark 解析后的值）并切换 Element Plus 的 dark 类
export function applyTheme() {
  const s = store.data.settings
  const choice = s.theme || 'light'
  let resolved = choice
  if (choice === 'auto' && window.matchMedia) {
    resolved = window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  }
  const root = document.documentElement
  root.setAttribute('data-theme', resolved)
  root.classList.toggle('dark', resolved === 'dark')
  applyAccent(s.accent || '#165dff')
}

export function setTheme(t) {
  store.data.settings.theme = t
  // 手动切换明暗即视为脱离预设方案，回到「自定义」，否则方案的内联变量会覆盖明暗
  store.data.settings.scheme = ''
  applyScheme()
  saveNow()
}
export function setAccent(c) {
  if (!c) c = '#165dff'
  store.data.settings.accent = c
  // 手动改主题色同样脱离预设方案
  store.data.settings.scheme = ''
  applyScheme()
  saveNow()
}

// 当前生效的明暗（用于 UI 回显）
export function effectiveTheme() {
  return document.documentElement.getAttribute('data-theme') || 'light'
}

// 当前项目（含独立的目录/接口/环境）
export function currentProject() {
  const p = store.data.projects.find(x => x.id === store.data.currentProjectId)
  return p || store.data.projects[0]
}

// 一键清空测试数据。scope: cases|plans|reports|all；返回被清空的条数。
export async function clearTestData(scope = 'all') {
  const removed = await ClearTestData(store.data.currentProjectId || '', scope)
  await reloadStore() // 重新从后端加载并整体替换 store.data，确保 UI 立即刷新
  return removed
}

let saveTimer = null
// 调试面板脏标记：用户修改了当前请求数据但尚未显式「保存请求」时为真。
// 为真期间，store 的自动保存（deep watch）会被抑制，避免把临时调试改动
// 静默覆盖到接口定义里。仅当用户点击「保存请求」时才落盘。
export const debugDirty = ref(false)
export function markDebugDirty() { debugDirty.value = true }
export function clearDebugDirty() { debugDirty.value = false }

// 调试响应：仅内存态，不随自动保存落盘。
// 只有用户点击「保存请求」时，才会把当前响应写入接口定义（api.lastResponse）并持久化，
// 从而避免发送请求后文档/响应示例被自动更新。
export const liveResponses = reactive({}) // apiId -> 最近一次响应
export function setLiveResponse(apiId, r) { if (apiId) liveResponses[apiId] = r }
export function getLiveResponse(apiId) { return apiId ? (liveResponses[apiId] || null) : null }

// 已保存接口快照：每次显式保存时抓拍当前 store.data 中全部接口的深拷贝。
// 文档预览只读此快照，从而「保存请求之后」文档才更新；单纯的实时调试编辑
// （即使未保存）不会反映到文档里，避免把临时调试改动显示成已保存的文档。
export const savedApiSnapshots = reactive({}) // apiId -> 接口深拷贝
export function captureSnapshots() {
  for (const p of store.data.projects) {
    for (const a of p.apis) savedApiSnapshots[a.id] = JSON.parse(JSON.stringify(a))
  }
}

// 全局弹窗可见状态：环境管理与公共参数入口统一放在顶部导航栏，
// 不再占用接口请求行的空间。
export const envDialogVisible = ref(false)
export const commonDialogVisible = ref(false)
export function openEnvDialog() { envDialogVisible.value = true }
export function openCommonDialog() { commonDialogVisible.value = true }

function scheduleSave() {
  if (debugDirty.value) return // 调试中：抑制自动保存，等待显式保存
  clearTimeout(saveTimer)
  saveTimer = setTimeout(saveNow, 500)
}

// 轻量「请求保存」信号：仅递增计数器（O(1)，不触发深遍历），由修订计数器 watch 接管防抖保存。
// 用于那些通过内联编辑（环境/公共参数/重命名等）修改数据、原本依赖全局 deep watch 才落盘的场景。
export function requestSave() {
  store.saveRev++
  scheduleSave()
  scheduleAutoSync()
}

// Wails 桥接（window.go）由桌面运行时注入，可能晚于 Vue 挂载；
// 浏览器预览（npm run dev）下不存在，此时退回内存数据，避免崩溃。
function hasGoBridge() {
  return !!(window.go && window.go.main && window.go.main.App)
}

function waitForGo(timeoutMs = 3000) {
  return new Promise(resolve => {
    if (hasGoBridge()) return resolve(true)
    const t0 = Date.now()
    const timer = setInterval(() => {
      if (hasGoBridge() || Date.now() - t0 > timeoutMs) {
        clearInterval(timer)
        resolve(hasGoBridge())
      }
    }, 50)
  })
}

export async function saveNow() {
  clearTimeout(saveTimer)
  if (!store.loaded) return
  if (!hasGoBridge()) return
  try {
    await SaveData(JSON.parse(JSON.stringify(store.data)))
    clearDebugDirty() // 任何显式保存后，清除调试脏标记
  } catch (e) {
    console.error('保存失败', e)
  }
}

// 仅更新单个接口的已保存快照（O(1)，取代每次保存克隆全部接口），供文档预览使用。
export function captureApiSnapshot(api) {
  if (api && api.id) savedApiSnapshots[api.id] = JSON.parse(JSON.stringify(api))
}

// 显式保存当前调试请求数据（用户点击「保存请求」），同时清除脏标记，
// 使后续自动保存恢复生效；仅刷新当前接口的文档快照，避免全量克隆。
export async function saveDebugNow() {
  clearDebugDirty()
  await saveNow()
  captureApiSnapshot(currentApi())
}

async function loadInto() {
  if (!hasGoBridge()) {
    // 非 Wails 环境（如浏览器预览），保持内存中的默认数据
    return
  }
  const d = await LoadData()
  d.projects ||= []
  if (!d.projects.length) {
    d.projects = [{ id: 'default', name: '默认项目', dirs: [], apis: [], environments: [], activeEnvId: '', updatedAt: '', common: { headers: [], query: [] }, testCases: [], testPlans: [], testReports: [] }]
  }
  if (!d.projects.find(p => p.id === d.currentProjectId)) {
    d.currentProjectId = d.projects[0].id
  }
  for (const p of d.projects) {
    p.dirs ||= []
    p.apis ||= []
    p.environments ||= []
    p.activeEnvId ||= ''
    p.common ||= { headers: [], query: [] }
    p.common.headers ||= []
    p.common.query ||= []
    p.testCases ||= []
    p.testPlans ||= []
    p.testReports ||= []
    p.apis.forEach(normalizeApi)
  }
  d.settings ||= { aiBaseUrl: '', aiKey: '', aiModel: '', timeoutSec: 30, cloudURL: '', cloudToken: '', cloudUser: '', autoSync: false, version: '1.0.0', updateURL: 'http://127.0.0.1:8080', theme: 'light', accent: '#165dff', hotkey: 'Ctrl+Shift+V', clipboard: { monitor: true, maxItems: 200 } }
  d.settings.version ||= '1.0.0'
  d.settings.updateURL ||= 'http://127.0.0.1:8080'
  d.settings.theme ||= 'light'
  d.settings.scheme ||= ''
  d.settings.accent ||= '#165dff'
  d.settings.clipboard ||= { monitor: true, maxItems: 200 }
  if (typeof d.settings.clipboard.maxItems !== 'number') d.settings.clipboard.maxItems = 200
  if (typeof d.settings.clipboard.monitor !== 'boolean') d.settings.clipboard.monitor = true
  d.plugins ||= { connections: [] }
  d.plugins.connections ||= []
  d.clipboard ||= { history: [] }
  d.clipboard.history ||= []
  store.data = d
  captureSnapshots() // 基线快照 = 已加载（=已保存）的数据，文档预览从此开始即为保存态
}

export async function initStore() {
  store.loading = true
  const ok = await waitForGo()
  if (ok) {
    try {
      await loadInto()
    } catch (e) {
      console.error('加载数据失败', e)
    }
  } else {
    console.warn('未检测到 Wails 桥接（window.go），使用内存数据（预览模式）')
  }
  store.loading = false
  store.loaded = true
  if (ok) {
    try { store.appVersion = await GetVersion() } catch {}
  }
  // 轻量持久化监听：仅观察修订计数器（O(1)，不再深度遍历整个 store.data），
  // 由 requestSave() 在需要落盘的编辑处递增，避免每次按键都触发深 diff 导致卡顿。
  watch(() => store.saveRev, () => { scheduleSave(); scheduleAutoSync() })
  applyScheme() // 应用主题方案（含明暗/主色/专属变量，兼容跟随系统）
  try {
    if (window.matchMedia) {
      window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
        if ((store.data.settings.theme || 'light') === 'auto') applyTheme()
      })
    }
  } catch {}
  if (ok) await autoPullOnStart()
}

// 检测升级：调用升级服务地址的 /version 接口，返回 { current, latest, hasNew, url, notes, error }
export async function checkUpdate() {
  if (!hasGoBridge()) return { current: store.appVersion, latest: '', hasNew: false, url: '', notes: '', error: '预览模式不支持升级检测' }
  try {
    return await CheckUpdate()
  } catch (e) {
    return { current: store.appVersion, latest: '', hasNew: false, url: '', notes: '', error: String(e) }
  }
}

export async function reloadStore() {
  try {
    await loadInto()
  } catch (e) {
    console.error('重新加载数据失败', e)
  }
}

// ---------------- 云同步（自动同步） ----------------

export function cloudBase() {
  let u = (store.data.settings.cloudURL || '').trim()
  if (!u) return ''
  if (!/^https?:\/\//.test(u)) u = 'http://' + u
  return u.replace(/\/+$/, '')
}

export async function cloudApi(path, opts = {}) {
  const url = cloudBase() + path
  opts.headers = opts.headers || {}
  const token = store.data.settings.cloudToken
  if (token) opts.headers['Authorization'] = 'Bearer ' + token
  opts.headers['Content-Type'] = 'application/json'
  const r = await fetch(url, opts)
  let body = null
  try { body = await r.json() } catch { /* ignore */ }
  if (!r.ok) throw new Error((body && body.error) || ('请求失败 ' + r.status))
  return body
}

// ---------------- AI（OpenAI 兼容） ----------------
// 统一的聊天补全调用，供字段描述生成、命名工具等复用。
export async function callAI(messages, opts = {}) {
  const s = store.data.settings
  const base = (s.aiBaseUrl || '').trim()
  if (!base) throw new Error('未配置 AI 接口地址（设置 → AI 配置）')
  if (!s.aiKey) throw new Error('未配置 AI API Key（设置 → AI 配置）')
  const model = s.aiModel || 'gpt-4o-mini'
  try {
    // 优先走 Go 后端代发，规避 Wails webview 的 CORS 限制
    return await CallAI({
      baseUrl: base,
      apiKey: s.aiKey,
      model,
      timeoutSec: s.timeoutSec || 30,
      maxTokens: opts.maxTokens || 0,
      messages: messages.map(m => ({ role: m.role, content: m.content })),
    })
  } catch (e) {
    // 后端不可用时的兜底提示（极少触发）
    const msg = String(e && e.message ? e.message : e)
    if (/Failed to fetch|NetworkError|CORS/i.test(msg)) {
      throw new Error('AI 请求失败（网络/CORS）：' + msg)
    }
    throw e
  }
}

// 自动同步是否启用（需开启开关且已登录且已配置地址）
function autoSyncEnabled() {
  const s = store.data.settings
  return !!s.autoSync && !!s.cloudToken && !!s.cloudURL
}

let syncTimer = null
// 编辑后防抖推送（默认 4 秒），避免频繁请求
export function scheduleAutoSync() {
  if (!autoSyncEnabled()) return
  clearTimeout(syncTimer)
  syncTimer = setTimeout(autoPushNow, 4000)
}

// 自动把所有项目推送到云端，云端有更新版本时先拉取，避免互相覆盖
async function autoPushNow() {
  if (!autoSyncEnabled()) return
  const now = new Date().toISOString()
  let conflictId = ''
  for (const p of store.data.projects) {
    p.updatedAt = now
    try {
      const b = await cloudApi('/api/projects/' + p.id, {
        method: 'PUT',
        body: JSON.stringify({ name: p.name, updatedAt: p.updatedAt, data: p }),
      })
      if (b && b.updatedAt) p.updatedAt = b.updatedAt
    } catch (e) {
      const msg = String(e)
      if (msg.includes('409') || msg.includes('请先拉取')) conflictId = p.id
      else console.warn('自动同步到云端失败', e)
    }
  }
  saveNow()
  if (conflictId) {
    try { await autoPullProject(conflictId) } catch { /* ignore */ }
  }
}

async function autoPullProject(id) {
  const remote = await cloudApi('/api/projects/' + id)
  const data = remote.data
  if (!data) return
  const i = store.data.projects.findIndex(x => x.id === data.id)
  if (i >= 0) store.data.projects[i] = data
  else store.data.projects.push(data)
  saveNow()
}

// 启动应用时自动拉取云端「更新的」或「本地没有的」项目，实现多端同步
export async function autoPullOnStart() {
  if (!autoSyncEnabled()) return
  try {
    const list = await cloudApi('/api/projects')
    if (!Array.isArray(list)) return
    for (const c of list) {
      const existing = store.data.projects.find(x => x.id === c.id)
      if (!existing || (c.updatedAt && (!existing.updatedAt || c.updatedAt > existing.updatedAt))) {
        await autoPullProject(c.id)
      }
    }
  } catch (e) {
    console.warn('启动自动拉取失败', e)
  }
}

export function currentApi() {
  const p = currentProject()
  return p.apis.find(a => a.id === store.currentApiId) || null
}

// ---------------- 接口调试多标签（类浏览器标签） ----------------
// 每个标签绑定一个接口（apiId），多个标签可绑定同一接口；sub 记录该标签内的子页（debug/params/doc）。
function syncActive() {
  const t = store.openTabs.find(x => x.id === store.activeTabId)
  if (t) {
    store.currentApiId = t.apiId
    store.activeTab = t.sub
  } else {
    store.currentApiId = ''
    store.activeTab = 'debug'
  }
}

// 打开接口到标签：newTab=true 时强制新建标签，否则命中已存在的同接口标签并切换
export function openApiInTab(apiId, opts = {}) {
  if (!apiId) return
  if (!opts.newTab) {
    const exist = store.openTabs.find(t => t.apiId === apiId)
    if (exist) { store.activeTabId = exist.id; syncActive(); return }
  }
  const tab = { id: uid(), apiId, sub: 'debug' }
  store.openTabs.push(tab)
  store.activeTabId = tab.id
  syncActive()
}

// 切换标签
export function switchTab(id) {
  if (!store.openTabs.find(t => t.id === id)) return
  store.activeTabId = id
  syncActive()
}

// 关闭标签；关闭当前标签时自动切换到相邻标签，关闭最后一个后进入空状态
export function closeTab(id) {
  const idx = store.openTabs.findIndex(t => t.id === id)
  if (idx === -1) return
  store.openTabs.splice(idx, 1)
  if (store.activeTabId === id) {
    const next = store.openTabs[idx] || store.openTabs[idx - 1] || null
    store.activeTabId = next ? next.id : ''
  }
  syncActive()
}

export function closeCurrentTab() {
  if (store.activeTabId) closeTab(store.activeTabId)
}

export function closeOtherTabs(id) {
  const target = id || store.activeTabId
  if (!target) return
  store.openTabs = store.openTabs.filter(t => t.id === target)
  store.activeTabId = target
  syncActive()
}

export function closeAllTabs() {
  store.openTabs = []
  store.activeTabId = ''
  syncActive()
}

// 切换当前标签内的子页（debug/params/doc），回写至当前标签
export function setSubTab(sub) {
  const t = store.openTabs.find(x => x.id === store.activeTabId)
  if (t) t.sub = sub
  store.activeTab = sub
}

// 关闭所有引用了指定接口（已被删除）的标签
export function closeTabsByApi(apiId) {
  store.openTabs = store.openTabs.filter(t => t.apiId !== apiId)
  if (store.openTabs.length === 0) store.activeTabId = ''
  else if (!store.openTabs.find(t => t.id === store.activeTabId)) store.activeTabId = store.openTabs[store.openTabs.length - 1].id
  syncActive()
}


// 当前激活环境的变量（仅启用项），用于请求发送时 {{var}} 替换
export function activeEnvVars() {
  const p = currentProject()
  const id = p.activeEnvId
  if (!id) return []
  const env = p.environments.find(e => e.id === id)
  if (!env) return []
  return (env.vars || []).filter(v => v.enabled && v.key)
}

// ---------------- 项目操作 ----------------

export function switchProject(id) {
  if (!store.data.projects.find(p => p.id === id)) return
  if (id === store.data.currentProjectId) return
  store.treeLoading = true
  // 先让「加载中」绘制，再切换，避免大数据量重建目录时主线程阻塞看起来像卡死
  setTimeout(() => {
    store.data.currentProjectId = id
    closeAllTabs()
    saveNow()
    store.treeLoading = false
  }, 30)
}

export function addProject() {
  const p = {
    id: uid(), name: '新项目', dirs: [], apis: [], environments: [], activeEnvId: '',
    common: { headers: [], query: [] },
    updatedAt: new Date().toISOString(),
  }
  store.data.projects.push(p)
  store.data.currentProjectId = p.id
  saveNow()
  return p
}

export function renameProject(id, name) {
  const p = store.data.projects.find(x => x.id === id)
  if (p) p.name = name
}

export function removeProject(id) {
  if (store.data.projects.length <= 1) return
  store.data.projects = store.data.projects.filter(p => p.id !== id)
  if (store.data.currentProjectId === id) {
    store.data.currentProjectId = store.data.projects[0].id
  }
  closeAllTabs()
  saveNow()
}

// ---------------- 接口 / 目录操作 ----------------

export function addDir(parentId = '') {
  const p = currentProject()
  const dir = { id: uid(), parentId, name: '新建目录', sort: p.dirs.length }
  p.dirs.push(dir)
  return dir
}

export function addApi(dirId = '') {
  const p = currentProject()
  const api = normalizeApi({
    id: uid(),
    dirId,
    name: '新建接口',
    method: 'GET',
    url: '',
    description: '',
    contentType: '',
    headers: [],
    query: [],
    bodyType: 'json',
    body: '',
    formItems: [],
    reqFields: [],
    respFields: [],
    preScript: '',
    postScript: '',
    updatedAt: new Date().toISOString(),
  })
  p.apis.push(api)
  openApiInTab(api.id, { newTab: true })
  return api
}

export function removeApi(id) {
  const p = currentProject()
  const i = p.apis.findIndex(a => a.id === id)
  if (i >= 0) p.apis.splice(i, 1)
  delete savedApiSnapshots[id]
  closeTabsByApi(id)
}

export function removeDir(id) {
  const p = currentProject()
  const ids = new Set([id])
  let changed = true
  while (changed) {
    changed = false
    for (const d of p.dirs) {
      if (ids.has(d.parentId) && !ids.has(d.id)) { ids.add(d.id); changed = true }
    }
  }
  p.dirs = p.dirs.filter(d => !ids.has(d.id))
  const removedApiIds = new Set(p.apis.filter(a => ids.has(a.dirId)).map(a => a.id))
  p.apis = p.apis.filter(a => !ids.has(a.dirId))
  // 清空被删除目录下的接口对应的标签
  if (removedApiIds.size) {
    for (const aid of removedApiIds) closeTabsByApi(aid)
  }
}

// 构建目录树（供 el-tree 使用）type: dir | api
export function buildTree() {
  const p = currentProject()
  const nodes = (parentId) => {
    const out = []
    for (const d of p.dirs.filter(x => x.parentId === parentId)) {
      out.push({ id: d.id, label: d.name, type: 'dir', children: nodes(d.id) })
    }
    for (const a of p.apis.filter(x => x.dirId === parentId)) {
      out.push({ id: a.id, label: a.name, type: 'api', method: a.method, url: a.url, children: [] })
    }
    return out
  }
  return nodes('')
}

// 仅目录树（供范围选择）
export function buildDirTree() {
  const p = currentProject()
  const nodes = (parentId) => p.dirs
    .filter(d => d.parentId === parentId)
    .map(d => ({ value: d.id, label: d.name, children: nodes(d.id) }))
  return [{ value: '', label: '全部接口', children: nodes('') }]
}

// 当前项目的接口集合（供文档中心范围计算）
export function projectApis() {
  return currentProject().apis
}
export function projectDirs() {
  return currentProject().dirs
}

// ---------------- 接口测试：数据访问辅助 ----------------

export function projectTestCases() {
  return currentProject().testCases || []
}
export function projectTestPlans() {
  return currentProject().testPlans || []
}
export function projectReports() {
  return currentProject().testReports || []
}

// 将 AI 生成的用例合并进当前项目（去重同名 + 同接口避免重复堆叠）
export function addTestCases(cases) {
  const p = currentProject()
  p.testCases ||= []
  p.testCases.push(...cases)
  saveNow()
  return p.testCases
}

export function removeTestCase(id) {
  const p = currentProject()
  p.testCases = (p.testCases || []).filter(c => c.id !== id)
  // 同步从所有计划中移除
  for (const plan of (p.testPlans || [])) {
    plan.caseIds = (plan.caseIds || []).filter(cid => cid !== id)
  }
  saveNow()
}

export function saveTestCase(c) {
  const p = currentProject()
  p.testCases ||= []
  const i = p.testCases.findIndex(x => x.id === c.id)
  if (i >= 0) p.testCases[i] = c
  else p.testCases.push(c)
  saveNow()
}

export function addTestPlan(plan) {
  const p = currentProject()
  p.testPlans ||= []
  p.testPlans.push(plan)
  saveNow()
  return plan
}

export function removeTestPlan(id) {
  const p = currentProject()
  p.testPlans = (p.testPlans || []).filter(x => x.id !== id)
  saveNow()
}

// 将报告存入历史（最多保留 50 条）
export function appendReport(r) {
  const p = currentProject()
  p.testReports ||= []
  p.testReports.unshift(r)
  if (p.testReports.length > 50) p.testReports.length = 50
  saveNow()
}

export function removeReport(id) {
  const p = currentProject()
  p.testReports = (p.testReports || []).filter(x => x.id !== id)
  saveNow()
}

// ---------------- 调试日志（内存态，不持久化） ----------------
// 调试模式开启时记录请求/响应详情；关闭时仅记录错误，便于聚焦问题。
export const logStore = reactive({
  enabled: true,     // 总开关
  debug: true,       // 调试模式：记录请求/响应详情
  panelOpen: true,   // 左侧日志列是否展开
  filter: 'all',     // all | request | error
  expanded: {},      // 各条日志详情展开状态 id -> bool
  entries: [],       // {id, time, type, title, detail}
})

let _logSeq = 0
export function pushLog(type, title, detail) {
  if (!logStore.enabled) return
  // 非错误且未开启调试模式时不记录，避免噪音
  if (type !== 'error' && !logStore.debug) return
  const now = new Date()
  const hh = String(now.getHours()).padStart(2, '0')
  const mm = String(now.getMinutes()).padStart(2, '0')
  const ss = String(now.getSeconds()).padStart(2, '0')
  const ms = String(now.getMilliseconds()).padStart(3, '0')
  logStore.entries.unshift({
    id: ++_logSeq,
    time: `${hh}:${mm}:${ss}.${ms}`,
    type,           // request | response | error | info
    title,
    detail: detail || '',
  })
  if (logStore.entries.length > 400) logStore.entries.length = 400
}

export function clearLogs() {
  logStore.entries = []
}

// ---------------- AI 生成测试任务（全局状态，跨视图保留） ----------------
// 生成任务是后台异步跑的，状态放在全局，避免切到其它视图（接口测试组件被卸载）
// 时丢失进度与事件监听。切回「接口测试」时自动恢复进度展示与完成提示。
export const genJobId = ref('')
export const genStat = ref({ total: 0, done: 0, name: '', phase: '' })

// 压测进度（全局，跨视图保留），供「自动化测试」视图读取
export const stressStat = ref({ running: false, done: 0, total: 0 })
// 任务结束 / 出错时写入，供「接口测试」视图切回时弹出提示（提示后清除）
export const genDoneInfo = ref(null)   // { count: number, time: number }
export const genErrorInfo = ref(null)  // { error: string, time: number }

export function startGenJob(jobId, total) {
  genJobId.value = jobId
  genStat.value = { total, done: 0, name: '', phase: 'queued' }
}

let _genOff = []
// 在应用启动时注册一次（App.onMounted），不随组件卸载而失效。
export function initGenListener() {
  if (_genOff.length) return
  _genOff.push(runtime.EventsOn('apitool:gen-progress', (p) => {
    if (p.jobId !== genJobId.value) return
    genStat.value = { total: p.total, done: p.done, name: p.name || '', phase: p.phase || '' }
  }))
  _genOff.push(runtime.EventsOn('apitool:gen-done', (p) => {
    if (p.jobId !== genJobId.value) return
    genJobId.value = ''
    genStat.value = { total: 0, done: 0, name: '', phase: '' }
    if (p.cases && p.cases.length) {
      addTestCases(p.cases)
      genDoneInfo.value = { count: p.cases.length, time: Date.now() }
    } else {
      genDoneInfo.value = { count: 0, time: Date.now() }
    }
  }))
  _genOff.push(runtime.EventsOn('apitool:gen-error', (p) => {
    if (p.jobId !== genJobId.value) return
    genJobId.value = ''
    genStat.value = { total: 0, done: 0, name: '', phase: '' }
    genErrorInfo.value = { error: p.error || '生成失败', time: Date.now() }
  }))
  _genOff.push(runtime.EventsOn('apitool:stress-progress', (p) => {
    if (!p) return
    stressStat.value = { running: true, done: p.done || 0, total: p.total || 0 }
    if (p.done >= p.total && p.total > 0) {
      // 完成后由调用方重置 running，这里仅更新计数
    }
  }))
}

// ---------------- 插件管理（连接配置，按分类存储） ----------------

export function pluginConnections() {
  return store.data.plugins.connections
}

export function addPluginConn(conn) {
  store.data.plugins.connections.push(conn)
  saveNow()
}

export function updatePluginConn(conn) {
  const list = store.data.plugins.connections
  const i = list.findIndex(x => x.id === conn.id)
  if (i >= 0) list[i] = conn
  else list.push(conn)
  saveNow()
}

export function removePluginConn(id) {
  store.data.plugins.connections =
    store.data.plugins.connections.filter(x => x.id !== id)
  saveNow()
}

// ---------------- 剪贴板历史 ----------------
// 采集与存储统一由 Go 端负责（见 clipboard_windows.go）：后台轮询系统剪贴板，
// 同时捕获文本与图片（CF_DIB -> PNG），写入 data.json。前端仅负责展示、搜索、
// 复制与删除，数据通过 GetClipItems 拉取。调出窗口由全局快捷键 / 托盘菜单触发，
// Go 端弹出「剪贴板历史」浮层（见 App.vue 的 ClipboardHistory 组件）。

export let clipImgBase = ''

// 图片历史项的数据 URL 缓存（避免重复向 Go 请求 base64）
const clipImageCache = {}

// clipImageURL 返回图片历史项的 data URL，按需向 Go 请求并缓存
export async function clipImageURL(item) {
  if (!item || item.type !== 'image' || !item.id) return ''
  if (clipImageCache[item.id]) return clipImageCache[item.id]
  try {
    const r = await GetClipImageData(item.id)
    if (r && r.data) {
      const url = 'data:' + (r.mime || 'image/png') + ';base64,' + r.data
      clipImageCache[item.id] = url
      return url
    }
  } catch (e) { /* ignore */ }
  return ''
}

// 从 Go 端拉取最新历史并写入 store（图片以相对路径存储，前端拼接 base 显示）
export async function loadClipItems() {
  if (!hasGoBridge()) return
  try {
    const items = await GetClipItems()
    store.data.clipboard.history = Array.isArray(items) ? items : []
  } catch (e) {
    console.error('加载剪贴板历史失败', e)
  }
}

// 兼容旧调用：采集已移到 Go 端，这里仅做一次初始加载
export function initClipboardMonitor() {
  loadClipItems()
}

export function stopClipboardMonitor() {}

// 监听开关：仅持久化到设置（Go 端采集时读取该开关）
export function setClipboardMonitor(on) {
  store.data.settings.clipboard.monitor = !!on
  saveNow()
}

// 复制某条历史到系统剪贴板（文本直接写入；图片由 Go 端读取本地 PNG 写回 CF_DIB）
export async function copyClipItem(id) {
  await CopyClipItem(id)
}

// 删除单条（Go 端落地，成功后刷新本地）
export async function removeClip(id) {
  try {
    await DeleteClipItem(id)
  } catch (e) { /* ignore */ }
  const hist = store.data.clipboard.history
  const i = hist.findIndex(x => x.id === id)
  if (i >= 0) hist.splice(i, 1)
}

// 清空（Go 端落地，成功后刷新本地）
export async function clearClipHistory() {
  try {
    await ClearClipHistory()
  } catch (e) { /* ignore */ }
  store.data.clipboard.history = []
}
