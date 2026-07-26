<script setup>
import { ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { SendRequest, FormatJSON, ParseFields } from '../../wailsjs/go/main/App'
import { store, saveNow, activeEnvVars, currentProject, uid } from '../store'
import { runScript } from '../script'
import KVEditor from './KVEditor.vue'
import EnvManager from './EnvManager.vue'
import CommonParams from './CommonParams.vue'

const props = defineProps({ api: { type: Object, required: true } })

const methods = ['GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'HEAD', 'OPTIONS']
const reqTab = ref('query')
const respTab = ref('body')
const sending = ref(false)
const envVisible = ref(false)
const commonVisible = ref(false)

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

const resp = computed(() => props.api.lastResponse || null)
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
        ElMessage.error('前置脚本执行出错：' + String(e))
        return
      }
      // 应用脚本中设置的环境变量
      spec.env = Object.entries(envMap).map(([key, value]) => ({ key, value, enabled: true }))
    }

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
        ElMessage.warning('后置脚本执行出错：' + String(e))
      }
    }

    props.api.lastResponse = r
    props.api.updatedAt = new Date().toISOString()
    if (r.error) {
      ElMessage.error(r.error)
    } else {
      respTab.value = 'body'
      ElMessage.success(`请求完成 ${r.status} · ${r.durationMs}ms`)
    }
    saveNow()
  } finally {
    sending.value = false
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
    store.activeTab = 'params'
    ElMessage.success('已生成请求参数，可在参数设置中补充描述')
  } catch (e) {
    ElMessage.error(String(e))
  }
}

// 用响应 JSON 生成响应参数文档
async function respToRespFields() {
  if (!resp.value || !resp.value.isJson) { ElMessage.warning('当前响应不是 JSON'); return }
  try {
    const fields = await ParseFields(resp.value.body, JSON.parse(JSON.stringify(props.api.respFields)))
    props.api.respFields = fields || []
    store.activeTab = 'params'
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
</script>

<template>
  <div class="panel-page">
    <!-- 请求行 -->
    <div class="card">
      <div class="url-row">
        <el-select v-model="api.method" style="width:110px" size="large">
          <el-option v-for="m in methods" :key="m" :label="m" :value="m" />
        </el-select>
        <el-input v-model="api.url" size="large" placeholder="请输入接口地址，如 https://api.example.com/user/list（支持 {{变量}}）"
          @keyup.enter="send" />
        <el-select v-model="currentProject().activeEnvId" size="large" style="width:140px" placeholder="环境" title="选择环境变量">
          <el-option label="无环境" value="" />
          <el-option v-for="e in currentProject().environments" :key="e.id" :label="e.name" :value="e.id" />
        </el-select>
        <el-button size="large" title="管理环境" @click="envVisible = true">⚙ 环境</el-button>
        <el-button size="large" title="公共参数（对所有接口自动附加）" @click="commonVisible = true">☰ 公共参数</el-button>
        <el-button type="primary" size="large" :loading="sending" style="width:100px" @click="send">
          发 送
        </el-button>
      </div>

      <el-tabs v-model="reqTab" style="margin-top:10px">
        <el-tab-pane :label="`Query 参数 (${api.query.length})`" name="query">
          <KVEditor :items="api.query" key-placeholder="参数名" />
        </el-tab-pane>
        <el-tab-pane :label="`请求头 (${api.headers.length})`" name="headers">
          <KVEditor :items="api.headers" key-placeholder="参数名" />
        </el-tab-pane>
        <el-tab-pane label="请求体 Body" name="body">
          <div style="display:flex; gap:12px; align-items:center; margin-bottom:10px; flex-wrap:wrap">
            <el-radio-group v-model="api.bodyType" size="small">
              <el-radio-button value="none">none</el-radio-button>
              <el-radio-button value="json">JSON</el-radio-button>
              <el-radio-button value="form">Form 表单</el-radio-button>
              <el-radio-button value="text">Raw 文本</el-radio-button>
            </el-radio-group>
            <span style="color:#86909c; font-size:12px">Content-Type</span>
            <el-select v-model="api.contentType" size="small" filterable allow-create default-first-option
              placeholder="自动（按类型）" style="width:280px"
              @change="onContentTypeChange">
              <el-option v-for="ct in contentTypeOptions" :key="ct" :label="ct" :value="ct" />
            </el-select>
          </div>
          <template v-if="api.bodyType === 'json' || api.bodyType === 'text'">
            <el-input v-model="api.body" type="textarea" :rows="10" class="mono"
              :placeholder="bodyPlaceholder" />
            <div style="margin-top:8px; display:flex; gap:8px" v-if="api.bodyType === 'json'">
              <el-button size="small" @click="formatBody">格式化 JSON</el-button>
              <el-button size="small" @click="bodyToReqFields">生成请求参数文档</el-button>
            </div>
          </template>
          <KVEditor v-else-if="api.bodyType === 'form'" :items="api.formItems" key-placeholder="字段名" />
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
          <el-input v-model="api.preScript" type="textarea" :rows="9" class="mono" placeholder="// 例如：request.setHeader('X-Token', env('token'))" />
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
          <el-input v-model="api.postScript" type="textarea" :rows="9" class="mono" placeholder="// 例如：setEnv('lastId', response.json().id)" />
        </el-tab-pane>
      </el-tabs>
    </div>

    <EnvManager v-model:visible="envVisible" />
    <CommonParams v-model:visible="commonVisible" />

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
        <span v-if="resp && resp.isJson">
          <el-button size="small" type="primary" plain @click="respToRespFields">导入为响应参数文档</el-button>
        </span>
      </div>

      <div v-if="!resp" style="color:#86909c; font-size:13px; padding:20px 0; text-align:center">
        点击「发送」后，响应结果将显示在这里（自动保存，下次打开仍可查看）
      </div>
      <div v-else-if="resp.error" class="resp-body" style="color:#ff8181">{{ resp.error }}</div>
      <template v-else>
        <el-tabs v-model="respTab">
          <el-tab-pane label="响应体" name="body">
            <pre class="resp-body">{{ resp.body }}</pre>
          </el-tab-pane>
        </el-tabs>
      </template>
    </div>
  </div>
</template>

<style scoped>
.url-row { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
.url-row .el-input { flex: 1; min-width: 240px; }
.mono :deep(textarea) { font-family: Consolas, "Courier New", monospace; font-size: 12.5px; }
.resp-meta { color: #86909c; font-size: 12px; font-weight: 400; margin-left: 10px; }
.script-tip { font-size: 12px; color: #4e5969; background: #f2f3f5; border-radius: 6px; padding: 8px 10px; margin-bottom: 10px; line-height: 1.7; }
.script-tip code { background: #e5e6eb; padding: 1px 5px; border-radius: 4px; font-family: Consolas, monospace; color: #165dff; }
</style>
