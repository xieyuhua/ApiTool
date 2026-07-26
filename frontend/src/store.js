import { reactive, watch } from 'vue'
import { LoadData, SaveData } from '../wailsjs/go/main/App'

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
  view: 'workspace', // workspace | docs | settings
  currentApiId: '',
  activeTab: 'debug', // debug | params | doc
  data: {
    projects: [
      { id: 'default', name: '默认项目', dirs: [], apis: [], environments: [], activeEnvId: '', updatedAt: '' },
    ],
    currentProjectId: 'default',
    settings: { aiBaseUrl: '', aiKey: '', aiModel: '', timeoutSec: 30, cloudURL: '', cloudToken: '', cloudUser: '' },
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

export async function saveNow() {
  clearTimeout(saveTimer)
  if (!store.loaded) return
  try {
    await SaveData(JSON.parse(JSON.stringify(store.data)))
  } catch (e) {
    console.error('保存失败', e)
  }
}

async function loadInto() {
  const d = await LoadData()
  d.projects ||= []
  if (!d.projects.length) {
    d.projects = [{ id: 'default', name: '默认项目', dirs: [], apis: [], environments: [], activeEnvId: '', updatedAt: '' }]
  }
  if (!d.projects.find(p => p.id === d.currentProjectId)) {
    d.currentProjectId = d.projects[0].id
  }
  for (const p of d.projects) {
    p.dirs ||= []
    p.apis ||= []
    p.environments ||= []
    p.activeEnvId ||= ''
    p.apis.forEach(normalizeApi)
  }
  d.settings ||= { aiBaseUrl: '', aiKey: '', aiModel: '', timeoutSec: 30, cloudURL: '', cloudToken: '', cloudUser: '' }
  store.data = d
}

export async function initStore() {
  try {
    await loadInto()
  } catch (e) {
    console.error('加载数据失败', e)
  }
  store.loaded = true
  watch(() => store.data, scheduleSave, { deep: true })
}

export async function reloadStore() {
  try {
    await loadInto()
  } catch (e) {
    console.error('重新加载数据失败', e)
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
  store.data.currentProjectId = id
  store.currentApiId = ''
  saveNow()
}

export function addProject() {
  const p = {
    id: uid(), name: '新项目', dirs: [], apis: [], environments: [], activeEnvId: '',
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
      out.push({ id: a.id, label: a.name, type: 'api', method: a.method, children: [] })
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
