import { reactive, watch } from 'vue'
import { LoadData, SaveData, GetVersion, CheckUpdate } from '../wailsjs/go/main/App'

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
  appVersion: '', // 客户端版本号（来自 Go 端）
  data: {
    projects: [
      { id: 'default', name: '默认项目', dirs: [], apis: [], environments: [], activeEnvId: '', updatedAt: '', common: { headers: [], query: [] } },
    ],
    currentProjectId: 'default',
    settings: { aiBaseUrl: '', aiKey: '', aiModel: '', timeoutSec: 30, cloudURL: '', cloudToken: '', cloudUser: '', autoSync: false, version: '1.0.0', updateURL: 'http://127.0.0.1:8080' },
  },
})

// 当前项目（含独立的目录/接口/环境）
export function currentProject() {
  const p = store.data.projects.find(x => x.id === store.data.currentProjectId)
  return p || store.data.projects[0]
}

let saveTimer = null
function scheduleSave() {
  clearTimeout(saveTimer)
  saveTimer = setTimeout(saveNow, 500)
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
  } catch (e) {
    console.error('保存失败', e)
  }
}

async function loadInto() {
  if (!hasGoBridge()) {
    // 非 Wails 环境（如浏览器预览），保持内存中的默认数据
    return
  }
  const d = await LoadData()
  d.projects ||= []
  if (!d.projects.length) {
    d.projects = [{ id: 'default', name: '默认项目', dirs: [], apis: [], environments: [], activeEnvId: '', updatedAt: '', common: { headers: [], query: [] } }]
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
    p.apis.forEach(normalizeApi)
  }
  d.settings ||= { aiBaseUrl: '', aiKey: '', aiModel: '', timeoutSec: 30, cloudURL: '', cloudToken: '', cloudUser: '', autoSync: false, version: '1.0.0', updateURL: 'http://127.0.0.1:8080' }
  d.settings.version ||= '1.0.0'
  d.settings.updateURL ||= 'http://127.0.0.1:8080'
  store.data = d
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
  watch(() => store.data, () => { scheduleSave(); scheduleAutoSync() }, { deep: true })
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
    store.currentApiId = ''
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
  if (store.currentApiId && !currentProject().apis.find(a => a.id === store.currentApiId)) {
    store.currentApiId = ''
  }
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
  store.currentApiId = api.id
  store.activeTab = 'debug'
  return api
}

export function removeApi(id) {
  const p = currentProject()
  const i = p.apis.findIndex(a => a.id === id)
  if (i >= 0) p.apis.splice(i, 1)
  if (store.currentApiId === id) store.currentApiId = ''
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
  p.apis = p.apis.filter(a => {
    const del = ids.has(a.dirId)
    if (del && store.currentApiId === a.id) store.currentApiId = ''
    return !del
  })
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
