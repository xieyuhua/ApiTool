<script setup>
import { ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { SendRequest, FormatJSON, ParseFields, OpenFileDialog } from '../../../wailsjs/go/main/App'
import { store, activeEnvVars, currentProject, uid, pushLog, markDebugDirty, debugDirty, saveDebugNow, setLiveResponse, getLiveResponse, setSubTab } from '../../store'
import { runScript } from '../../script'
import KVEditor from '../test/KVEditor.vue'

const props = defineProps({ api: { type: Object, required: true } })

const methods = ['GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'HEAD', 'OPTIONS']
const reqTab = ref('query')
const respTab = ref('body')
const sending = ref(false)

const contentTypeOptions = [
  'application/json',
  'application/x-www-form-urlencoded',
  'multipart/form-data',
  'text/plain',
  'application/xml',
  'text/xml',
  'application/octet-stream',
  'application/pdf',
  'image/png',
  'application/javascript',
]

const resp = computed(() => getLiveResponse(props.api.id) || null)

// 响应体展示：JSON 默认美化展示，可切换回原文；复制时复制当前展示的内容
const respPretty = ref(true)
const respBodyText = computed(() => {
  const r = resp.value
  if (!r || !r.body) return ''
  if (!r.isJson || !respPretty.value) return r.body
  try { return JSON.stringify(JSON.parse(r.body), null, 2) } catch (e) { return r.body }
})
function toggleRespPretty() { respPretty.value = !respPretty.value }

// 复制响应体到剪贴板（优先 Clipboard API，降级 execCommand，兼容 WebView 无权限场景）
async function copyRespBody() {
  const text = respBodyText.value
  if (!text) { ElMessage.warning('响应体为空'); return }
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success('响应体已复制')
    return
  } catch (e) { /* 降级处理 */ }
  const ta = document.createElement('textarea')
  ta.value = text
  ta.style.position = 'fixed'
  ta.style.opacity = '0'
  document.body.appendChild(ta)
  ta.select()
  try { document.execCommand('copy'); ElMessage.success('响应体已复制') }
  catch (e) { ElMessage.error('复制失败') }
  document.body.removeChild(ta)
}

// 标签计数只统计“有参数名”的行，忽略空白占位行
const queryCount = computed(() => (props.api.query || []).filter(q => (q.key || '').trim()).length)
const headerCount = computed(() => (props.api.headers || []).filter(h => (h.key || '').trim()).length)
const bodyPlaceholder = computed(() =>
  props.api.bodyType === 'json' ? '{\n  "name": "张三"\n}' : '请求体原始文本')

function statusClass(s) {
  if (s >= 200 && s < 300) return 'status-ok'
  if (s >= 300 && s < 400) return 'status-warn'
  return 'status-err'
}

// 选择 Content-Type 时，若与 Body 类型强相关则同步调整（便于 UI 一致性）
function onContentTypeChange(v) {
  if (v === 'application/json') props.api.bodyType = 'json'
  else if (v === 'application/x-www-form-urlencoded' || v === 'multipart/form-data') props.api.bodyType = 'form'
  else if (v === 'text/plain') props.api.bodyType = 'text'
}

// 将环境变量列表转为可修改的 map
function envToMap(list) {
  const m = {}
  for (const kv of list) if (kv.enabled && kv.key) m[kv.key] = kv.value
  return m
}
// 将 map 写回当前环境（后置脚本持久化用）
function persistEnv(key, value) {
  const p = currentProject()
  let env = p.environments.find(e => e.id === p.activeEnvId)
  if (!env) {
    env = { id: uid(), name: '默认环境', vars: [] }
    p.environments.push(env)
    p.activeEnvId = env.id
  }
  const i = env.vars.findIndex(x => x.key === key)
  if (i >= 0) env.vars[i].value = value
  else env.vars.push({ key, value, enabled: true })
}

// 公共参数：项目级 common.headers / common.query 自动附加到所有请求；
// 接口自身已设置的同名参数优先（接口覆盖公共）。
function mergeCommon(spec) {
  const c = currentProject().common || { headers: [], query: [] }
  const hm = new Map()
  for (const h of (c.headers || [])) if (h.enabled && h.key) hm.set(String(h.key).toLowerCase(), { ...h })
  for (const h of spec.headers) if (h.enabled && h.key) hm.set(String(h.key).toLowerCase(), { ...h })
  spec.headers = [...hm.values()]
  const qm = new Map()
  for (const q of (c.query || [])) if (q.enabled && q.key) qm.set(String(q.key).toLowerCase(), { ...q })
  for (const q of spec.query) if (q.enabled && q.key) qm.set(String(q.key).toLowerCase(), { ...q })
  spec.query = [...qm.values()]
}

// 将地址栏中携带的 ?query 解析进「Query 参数」列表，并从地址中移除，
// 避免发送时地址里的 query 与 api.query 重复叠加。
function syncUrlQuery() {
  const raw = props.api.url || ''
  const qIdx = raw.indexOf('?')
  if (qIdx < 0) return
  const base = raw.slice(0, qIdx)
  let qs = raw.slice(qIdx + 1)
  const hIdx = qs.indexOf('#')
  if (hIdx >= 0) qs = qs.slice(0, hIdx)
  const params = new URLSearchParams(qs)
  const keys = [...params.keys()]
  if (!keys.length) {
    return
  }
  const have = new Set((props.api.query || []).filter(q => q.key).map(q => q.key))
  for (const [k, v] of params.entries()) {
    if (have.has(k)) continue
    props.api.query.push({ key: k, value: v, description: '', enabled: true })
    have.add(k)
  }
  markDebugDirty()
}

async function send() {
  sending.value = true
  try {
    const envMap = envToMap(activeEnvVars())
    const spec = {
      method: props.api.method,
      url: props.api.url,
      headers: JSON.parse(JSON.stringify(props.api.headers)),
      query: JSON.parse(JSON.stringify(props.api.query)),
      bodyType: props.api.bodyType,
      body: props.api.body,
      formItems: JSON.parse(JSON.stringify(props.api.formItems)),
      timeoutSec: store.data.settings.timeoutSec || 30,
      env: activeEnvVars(),
      contentType: props.api.contentType || '',
    }
    mergeCommon(spec) // 公共参数自动附加（接口同名优先）

    // 前置脚本：可修改请求与临时环境变量
    if (props.api.preScript && props.api.preScript.trim()) {
      try {
        const reqParsed = (() => { try { return JSON.parse(spec.body) } catch { return null } })()
        const req = {
          url: spec.url,
          method: spec.method,
          // body 为解析后的对象（JSON 时），便于 request.body.data.token 这类取值；非 JSON 时为原字符串
          body: reqParsed !== null ? reqParsed : spec.body,
          json() { try { return JSON.parse(spec.body) } catch { return null } },
          setHeader(k, v) {
            const i = spec.headers.findIndex(h => h.key === k)
            if (i >= 0) spec.headers[i].value = v
            else spec.headers.push({ key: k, value: v, description: '', enabled: true })
          },
          setQuery(k, v) {
            const i = spec.query.findIndex(h => h.key === k)
            if (i >= 0) spec.query[i].value = v
            else spec.query.push({ key: k, value: v, description: '', enabled: true })
          },
        }
        runScript(props.api.preScript, {
          request: req,
          env: k => envMap[k],
          setEnv: (k, v) => { envMap[k] = v },
          console,
        })
        spec.url = req.url
        spec.method = req.method
        spec.body = (typeof req.body === 'object' && req.body !== null) ? JSON.stringify(req.body, null, 2) : req.body
    } catch (e) {
      const msg = '前置脚本执行出错：' + String(e)
      pushLog('error', msg, String(e && e.stack ? e.stack : e))
      ElMessage.error(msg)
      return
    }
      // 应用脚本中设置的环境变量
      spec.env = Object.entries(envMap).map(([key, value]) => ({ key, value, enabled: true }))
    }

    pushLog('request', `${spec.method} ${spec.url}`, buildReqDetail(spec))
    const r = await SendRequest(spec)

    // 后置脚本：可读取响应并写回环境变量（持久化）
    if (props.api.postScript && props.api.postScript.trim() && !r.error) {
      try {
        const respHeaders = {}
        for (const k in r.headers) respHeaders[k] = r.headers[k]
        const respParsed = (() => { try { return JSON.parse(r.body) } catch { return null } })()
        runScript(props.api.postScript, {
          response: {
            status: r.status,
            headers: respHeaders,
            // body 为解析后的对象（JSON 时），支持 response.body.data.token / response.body.list[0].name
            body: respParsed !== null ? respParsed : r.body,
            text: r.body,
            json() { return respParsed },
          },
          env: k => envMap[k],
          setEnv: (k, v) => { envMap[k] = v; persistEnv(k, v) },
          console,
        })
    } catch (e) {
      const msg = '后置脚本执行出错：' + String(e)
      pushLog('error', msg, String(e && e.stack ? e.stack : e))
      ElMessage.warning(msg)
    }
    }

    setLiveResponse(props.api.id, r)
    if (r.error) {
      pushLog('error', `请求失败 ${spec.method} ${spec.url}`, r.error)
      ElMessage.error(r.error)
    } else {
      pushLog('response', `${r.status} ${r.statusText || ''} · ${r.durationMs}ms · ${fmtSize(r.size)}`, buildRespDetail(r))
      respTab.value = 'body'
      ElMessage.success(`请求完成 ${r.status} · ${r.durationMs}ms`)
    }
    // 注意：发送不再自动保存。响应仅在用户点击「保存请求」时随接口定义一并持久化，
    // 避免把临时调试改动静默覆盖到已保存的接口数据。
  } catch (e) {
    const msg = '请求过程异常：' + String(e)
    pushLog('error', msg, String(e && e.stack ? e.stack : e))
    ElMessage.error(msg)
  } finally {
    sending.value = false
  }
}

// 显式保存当前请求数据（url / 方法 / 参数 / 请求体 / 脚本等，含本次响应），
// 同时根据请求体 / 响应体生成参数文档。发送不会自动落盘，只有点击「保存请求」才会：
// 1) 把内存态的本次响应写入接口定义（持久化）；
// 2) 重新生成请求/响应参数文档。
async function saveRequest() {
  try {
    await generateDocs()
    props.api.lastResponse = getLiveResponse(props.api.id)
    await saveDebugNow()
    ElMessage.success('已保存当前请求并生成文档')
  } catch (e) {
    ElMessage.error('保存失败：' + String(e))
  }
}

// 保存时同步生成文档：请求体 → 请求参数；响应体 → 响应参数。
// ParseFields 会保留已填写的描述，不会覆盖手工补充的说明。
async function generateDocs() {
  if (props.api.body && props.api.body.trim()) {
    try {
      const fields = await ParseFields(props.api.body, JSON.parse(JSON.stringify(props.api.reqFields)))
      props.api.reqFields = fields || []
    } catch (e) { console.warn('请求参数文档生成失败', e) }
  }
  const r = getLiveResponse(props.api.id)
  if (r && r.isJson) {
    try {
      const fields = await ParseFields(r.body, JSON.parse(JSON.stringify(props.api.respFields)))
      props.api.respFields = fields || []
    } catch (e) { console.warn('响应参数文档生成失败', e) }
  }
}

async function formatBody() {
  if (!props.api.body.trim()) return
  try {
    props.api.body = await FormatJSON(props.api.body)
  } catch (e) {
    ElMessage.error(String(e))
  }
}

// 用请求体 JSON 生成请求参数文档
async function bodyToReqFields() {
  if (!props.api.body.trim()) { ElMessage.warning('请求体为空'); return }
  try {
    const fields = await ParseFields(props.api.body, JSON.parse(JSON.stringify(props.api.reqFields)))
    props.api.reqFields = fields || []
    setSubTab('params')
    ElMessage.success('已生成请求参数，可在参数设置中补充描述')
  } catch (e) {
    ElMessage.error(String(e))
  }
}

// 用表单字段生成请求参数文档（formItems 同步到 reqFields）
function formToReqFields() {
  const items = (props.api.formItems || []).filter(x => x.enabled && x.key)
  if (!items.length) { ElMessage.warning('表单无有效字段'); return }
  const existing = props.api.reqFields || []
  const map = {}
  existing.forEach(f => { map[f.name] = f })
  for (const it of items) {
    if (!map[it.key]) {
      map[it.key] = {
        name: it.key,
        type: it.type === 'file' ? 'file' : 'string',
        required: true,
        example: it.type === 'file' ? '' : it.value,
        description: it.description || '',
        children: null,
      }
    } else {
      const f = map[it.key]
      if (it.type === 'file' && f.type !== 'file') f.type = 'file'
      if (!f.description && it.description) f.description = it.description
    }
  }
  props.api.reqFields = Object.values(map)
  setSubTab('params')
  ElMessage.success('已生成表单参数文档，可在参数设置中补充描述')
}

// 表单：新增一行（含类型字段）
function addFormRow() {
  if (!props.api.formItems) props.api.formItems = []
  props.api.formItems.push({ key: '', value: '', description: '', enabled: true, type: 'text' })
  markDebugDirty()
}
function removeFormRow(i) {
  props.api.formItems.splice(i, 1)
  markDebugDirty()
}
// 表单：通过 Wails 原生文件对话框选择本地文件，写入 value 为真实绝对路径。
// 注意：Wails webview 中浏览器 <input type="file"> 的 File.path 为空，无法拿到真实路径，
// 必须用原生对话框才能取到可用于后端 os.Open 的绝对路径。
async function pickFormFile(it) {
  try {
    const path = await OpenFileDialog({
      Title: '选择上传文件',
      ShowHiddenFiles: false,
    })
    // 用户取消选择时返回空字符串
    if (path) { it.value = path; markDebugDirty() }
  } catch (e) {
    ElMessage.error('打开文件对话框失败: ' + (e?.message || e))
  }
}
function formFileName(path) {
  if (!path) return ''
  return path.split(/[\\/]/).pop()
}
// 切换类型时，文件类型清空文本值，避免误带
function onFormTypeChange(kv) {
  if (kv.type === 'file') {
    if (kv.value && !/\.[\\/]/.test(kv.value) && !kv.value.includes('/')) {
      // 文本值，切换为文件时清空，等待选择
    }
    kv.value = ''
  }
}

// 用响应 JSON 生成响应参数文档
async function respToRespFields() {
  const r = getLiveResponse(props.api.id)
  if (!r || !r.isJson) { ElMessage.warning('当前响应不是 JSON'); return }
  try {
    const fields = await ParseFields(r.body, JSON.parse(JSON.stringify(props.api.respFields)))
    props.api.respFields = fields || []
    setSubTab('params')
    ElMessage.success('已从响应生成响应参数，可在参数设置中补充描述')
  } catch (e) {
    ElMessage.error(String(e))
  }
}

function fmtSize(n) {
  if (n == null) return '-'
  if (n < 1024) return n + ' B'
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB'
  return (n / 1024 / 1024).toFixed(2) + ' MB'
}

// 将一次请求规格格式化为可读日志明细
function buildReqDetail(spec) {
  const lines = []
  lines.push(`${spec.method} ${spec.url}`)
  lines.push(`Timeout: ${spec.timeoutSec}s`)
  const env = (spec.env || []).map(e => e.key + '=' + e.value).join(', ')
  lines.push(`Env: ${env || '(无)'}`)
  const hdrs = (spec.headers || []).filter(h => h.enabled && h.key)
  if (hdrs.length) { lines.push('Headers:'); for (const h of hdrs) lines.push(`  ${h.key}: ${h.value}`) }
  const qs = (spec.query || []).filter(q => q.enabled && q.key)
  if (qs.length) { lines.push('Query:'); for (const q of qs) lines.push(`  ${q.key}=${q.value}`) }
  if (spec.bodyType === 'form') {
    const f = (spec.formItems || []).filter(x => x.enabled && x.key)
    lines.push('Form:'); for (const x of f) lines.push(`  ${x.key}=${x.value}`)
  } else if (spec.body && spec.bodyType !== 'none') {
    lines.push('Body:')
    lines.push(spec.body.length > 4000 ? spec.body.slice(0, 4000) + '\n...(已截断)' : spec.body)
  }
  return lines.join('\n')
}

// 将一次响应格式化为可读日志明细
function buildRespDetail(r) {
  const lines = []
  lines.push(`Status: ${r.status} ${r.statusText || ''}`)
  lines.push(`Duration: ${r.durationMs}ms · Size: ${r.size} B`)
  lines.push('Headers:')
  for (const k in (r.headers || {})) lines.push(`  ${k}: ${r.headers[k]}`)
  if (r.body) lines.push('Body:\n' + (r.body.length > 4000 ? r.body.slice(0, 4000) + '\n...(已截断)' : r.body))
  return lines.join('\n')
}
</script>

<template>
  <div class="panel-page">
    <!-- 请求行 -->
    <div class="card">
      <div class="url-row">
        <el-select v-model="api.method" style="width:110px" size="large" @change="markDebugDirty">
          <el-option v-for="m in methods" :key="m" :label="m" :value="m" />
        </el-select>
        <el-input v-model="api.url" size="large" placeholder="请输入接口地址，如 https://api.example.com/user/list（支持 {{变量}}）"
          @keyup.enter="send" @input="markDebugDirty" @change="syncUrlQuery" />
        <el-button type="primary" size="large" :loading="sending" style="width:100px" @click="send">
          发 送
        </el-button>
        <el-button size="large" type="success" @click="saveRequest" title="保存当前请求数据并生成文档（仅在点击时保存，不会每次发送自动更新）">
          保存请求
        </el-button>
      </div>

      <el-tabs v-model="reqTab" style="margin-top:10px">
        <el-tab-pane :label="`Query 参数 (${queryCount})`" name="query">
          <KVEditor :items="api.query" key-placeholder="参数名" @change="markDebugDirty" />
        </el-tab-pane>
        <el-tab-pane :label="`请求头 (${headerCount})`" name="headers">
          <KVEditor :items="api.headers" key-placeholder="参数名" @change="markDebugDirty" />
        </el-tab-pane>
        <el-tab-pane label="请求体 Body" name="body">
          <div style="display:flex; gap:12px; align-items:center; margin-bottom:10px; flex-wrap:wrap">
            <el-radio-group v-model="api.bodyType" size="small" @change="markDebugDirty">
              <el-radio-button value="none">none</el-radio-button>
              <el-radio-button value="json">JSON</el-radio-button>
              <el-radio-button value="form">Form 表单</el-radio-button>
              <el-radio-button value="text">Raw 文本</el-radio-button>
            </el-radio-group>
            <span style="color:#86909c; font-size:12px">Content-Type</span>
            <el-select v-model="api.contentType" size="small" filterable allow-create default-first-option
              placeholder="自动（按类型）" style="width:280px"
              @change="onContentTypeChange(); markDebugDirty()">
              <el-option v-for="ct in contentTypeOptions" :key="ct" :label="ct" :value="ct" />
            </el-select>
          </div>
          <template v-if="api.bodyType === 'json' || api.bodyType === 'text'">
            <el-input v-model="api.body" type="textarea" :rows="10" class="mono"
              :placeholder="bodyPlaceholder" @input="markDebugDirty" />
            <div style="margin-top:8px; display:flex; gap:8px" v-if="api.bodyType === 'json'">
              <el-button size="small" @click="formatBody">格式化 JSON</el-button>
              <el-button size="small" @click="bodyToReqFields">生成请求参数文档</el-button>
            </div>
          </template>
          <template v-else-if="api.bodyType === 'form'">
            <div style="margin-bottom:10px; display:flex; gap:8px">
              <el-button size="small" @click="formToReqFields">生成表单参数文档</el-button>
              <span style="color:#86909c; font-size:12px; align-self:center">字段类型可选「文本」或「文件」，文件类型将作为 multipart/form-data 上传</span>
            </div>
            <div class="form-grid form-head">
              <span class="c-enable"></span>
              <span class="c-type">类型</span>
              <span class="c-key">字段名</span>
              <span class="c-val">值 / 文件</span>
              <span class="c-desc">说明</span>
              <span class="c-op"></span>
            </div>
            <div v-for="(kv, i) in api.formItems" :key="i" class="form-grid form-row">
              <span class="c-enable"><el-checkbox v-model="kv.enabled" @change="markDebugDirty" /></span>
              <span class="c-type">
                <el-select v-model="kv.type" size="small" @change="onFormTypeChange(kv); markDebugDirty()">
                  <el-option label="文本" value="text" />
                  <el-option label="文件" value="file" />
                </el-select>
              </span>
              <span class="c-key"><el-input v-model="kv.key" placeholder="字段名" @input="markDebugDirty" /></span>
              <span class="c-val">
                <template v-if="kv.type === 'file'">
                  <el-input :model-value="formFileName(kv.value)" placeholder="选择文件" readonly @click="pickFormFile(kv)">
                    <template #append>
                      <el-button @click="pickFormFile(kv)">浏览</el-button>
                    </template>
                  </el-input>
                </template>
                <el-input v-else v-model="kv.value" placeholder="值" @input="markDebugDirty" />
              </span>
              <span class="c-desc"><el-input v-model="kv.description" placeholder="说明（写入文档）" @input="markDebugDirty" /></span>
              <span class="c-op">
                <el-button link type="danger" title="删除" @click="removeFormRow(i)">✕</el-button>
              </span>
            </div>
            <div v-if="!api.formItems.length" class="kv-empty">暂无字段，点击下方「添加字段」</div>
            <el-button link type="primary" style="margin-top:10px" @click="addFormRow">＋ 添加字段</el-button>
          </template>
          <div v-else style="color:#86909c;font-size:13px">该请求不包含 Body</div>
        </el-tab-pane>
        <el-tab-pane label="前置脚本" name="pre">
          <div class="script-tip">
            发送请求<b>前</b>执行（前端 JS）。可用变量：
            <code>request.url</code> / <code>request.method</code> / <code>request.body</code>（JSON 时已是解析对象，可 <code>request.body.data.x</code>）/
            <code>request.json()</code> / <code>request.setHeader(k,v)</code> / <code>request.setQuery(k,v)</code> /
            <code>setEnv(k,v)</code> / <code>env(k)</code> / <code>console.log()</code>
            <br>示例：<code>request.setHeader("X-Token", env("token"))</code> / <code>request.body.data.page = 1</code>
          </div>
          <el-input v-model="api.preScript" type="textarea" :rows="9" class="mono" placeholder="// 例如：request.setHeader('X-Token', env('token'))" @input="markDebugDirty" />
        </el-tab-pane>
        <el-tab-pane label="后置脚本" name="post">
          <div class="script-tip">
            收到响应<b>后</b>执行（前端 JS）。可用变量：
            <code>response.status</code> / <code>response.headers</code>（对象，如 <code>response.headers["x-request-id"]</code>）/
            <code>response.body</code>（JSON 时已是解析对象）/ <code>response.text</code>（原始字符串）/ <code>response.json()</code> /
            <code>setEnv(k,v)</code> / <code>env(k)</code> / <code>console.log()</code>
            <br>示例：
            <code>setEnv("token", response.body.data.token)</code> /
            <code>setEnv("name", response.body.data.list[0].name)</code> /
            <code>if (response.status === 200) setEnv("ok", "1")</code>
          </div>
          <el-input v-model="api.postScript" type="textarea" :rows="9" class="mono" placeholder="// 例如：setEnv('lastId', response.json().id)" @input="markDebugDirty" />
        </el-tab-pane>
      </el-tabs>
    </div>

    <!-- 环境管理与公共参数入口已统一移至顶部全局导航栏 -->

    <!-- 响应区 -->
    <div class="card">
      <div class="card-title">
        <span>
          响应
          <template v-if="resp && !resp.error">
            <span :class="statusClass(resp.status)" style="margin-left:10px">{{ resp.status }}</span>
            <span class="resp-meta">{{ resp.durationMs }} ms · {{ fmtSize(resp.size) }}</span>
          </template>
        </span>
        <span v-if="resp && !resp.error" class="resp-acts">
          <el-button size="small" @click="copyRespBody">复制响应体</el-button>
          <el-button v-if="resp.isJson" size="small" @click="toggleRespPretty">
            {{ respPretty ? '显示原文' : '格式化' }}
          </el-button>
          <el-button v-if="resp.isJson" size="small" type="primary" plain @click="respToRespFields">导入为响应参数文档</el-button>
        </span>
      </div>

      <div v-if="!resp" style="color:#86909c; font-size:13px; padding:20px 0; text-align:center">
        点击「发送」后，响应结果将显示在这里；点「保存请求」才会把响应与文档一并保存
      </div>
      <div v-else-if="resp.error" class="resp-body" style="color:#ff8181">{{ resp.error }}</div>
      <template v-else>
        <el-tabs v-model="respTab">
          <el-tab-pane label="响应体" name="body">
            <pre class="resp-body" @dblclick="copyRespBody" title="双击可复制全部内容">{{ respBodyText }}</pre>
          </el-tab-pane>
        </el-tabs>
      </template>
    </div>
  </div>
</template>

<style scoped>
.dirty-tip { color: #fa8c16; font-size: 12px; font-weight: 500; }
.url-row { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
.url-row .el-input { flex: 1; min-width: 0; }
.mono :deep(textarea) { font-family: Consolas, "Courier New", monospace; font-size: 12.5px; }
.resp-meta { color: #86909c; font-size: 12px; font-weight: 400; margin-left: 10px; }
.resp-acts { display: inline-flex; gap: 8px; align-items: center; }
.resp-body { user-select: text; cursor: text; }
.script-tip { font-size: 12px; color: #4e5969; background: #f2f3f5; border-radius: 6px; padding: 8px 10px; margin-bottom: 10px; line-height: 1.7; }
.script-tip code { background: #e5e6eb; padding: 1px 5px; border-radius: 4px; font-family: Consolas, monospace; color: #165dff; }
.form-grid { display: grid; grid-template-columns: 42px 90px minmax(150px, 1fr) minmax(190px, 1.3fr) minmax(150px, 1fr) 44px; gap: 12px; align-items: center; }
.form-head { font-size: 12px; color: #86909c; padding: 0 2px; margin-bottom: 8px; }
.form-row { margin-bottom: 10px; }
.form-grid .c-enable { display: flex; justify-content: center; }
.form-grid .c-key, .form-grid .c-val, .form-grid .c-desc, .form-grid .c-type { min-width: 0; }
.kv-empty { color: #c9cdd4; font-size: 13px; text-align: center; padding: 16px 0; border: 1px dashed #e5e6eb; border-radius: 8px; margin-bottom: 10px; }
</style>
