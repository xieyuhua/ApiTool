<script setup>
import { ref, computed, reactive, onMounted, onBeforeUnmount } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  CaptureInfo, StartCaptureServer, StopCaptureServer, GetCapturedRequests,
  ClearCapturedRequests, GenerateApiFromCaptured, ExportCapturedOpenAPI,
  BuildCapturedOpenAPI, CopyToClipboard, ImportCapturedAsTestCases,
} from '../../wailsjs/go/main/App'
import { store, saveNow, reloadStore } from '../store'
import { FORMATS } from '../cli'

const info = reactive({ running: false, addr: '', port: '', url: '', token: '', count: 0 })
const list = ref([])
const selected = ref([]) // 选中的捕获记录 id
const loading = ref(false)
const showToken = ref(false)
const detail = ref(null) // 当前查看详情的记录
const timer = ref(null)
const tableRef = ref(null)

// 导入目标：项目 + 目录
const importProjectId = ref(store.data.currentProjectId)
const importDirId = ref('')
const importing = ref(false)

// 当前选中项目的目录树（用于导入目录选择）
const projectDirOptions = computed(() => {
  const p = store.data.projects.find(x => x.id === importProjectId.value)
  if (!p) return [{ value: '', label: '全部接口（根目录）' }]
  const nodes = (parentId) => p.dirs
    .filter(d => d.parentId === parentId)
    .map(d => ({ value: d.id, label: d.name, children: nodes(d.id) }))
  return [{ value: '', label: '全部接口（根目录）', children: nodes('') }]
})

async function refresh() {
  loading.value = true
  try {
    const [i, l] = await Promise.all([CaptureInfo(), GetCapturedRequests()])
    Object.assign(info, i)
    list.value = l
    // 清理已不存在的选中项
    const ids = new Set(list.value.map(x => x.id))
    selected.value = selected.value.filter(id => ids.has(id))
  } catch (e) {
    // 忽略轮询错误
  } finally {
    loading.value = false
  }
}

async function toggleServer() {
  try {
    if (info.running) {
      await StopCaptureServer()
      ElMessage.success('已停止请求捕获服务')
    } else {
      await StartCaptureServer('', '')
      ElMessage.success('已启动请求捕获服务')
    }
    await refresh()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

function selectAll() { tableRef.value?.toggleAllSelection() }
function clearSel() { tableRef.value?.clearSelection() }

function fmtTime(t) {
  if (!t) return ''
  const d = new Date(t)
  if (isNaN(d.getTime())) return t
  const p = n => String(n).padStart(2, '0')
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

function maskToken(t) {
  if (!t) return ''
  if (showToken.value) return t
  if (t.length <= 8) return '••••••'
  return t.slice(0, 4) + '••••••••' + t.slice(-4)
}
async function copyToken() {
  try { await CopyToClipboard(info.token); ElMessage.success('Token 已复制') } catch (e) { ElMessage.error(String(e)) }
}
async function copyAddr() {
  try { await CopyToClipboard(info.url); ElMessage.success('捕获地址已复制') } catch (e) { ElMessage.error(String(e)) }
}

async function clearAll() {
  try {
    await ElMessageBox.confirm('确定清空所有已捕获的请求记录？', '清空确认', { type: 'warning' })
  } catch { return }
  try {
    await ClearCapturedRequests()
    selected.value = []
    await refresh()
    ElMessage.success('已清空')
  } catch (e) { ElMessage.error(String(e)) }
}

async function doImport() {
  if (!selected.value.length) { ElMessage.warning('请先勾选要导入的捕获记录'); return }
  importing.value = true
  try {
    await saveNow()
    const n = await GenerateApiFromCaptured(selected.value, importProjectId.value, importDirId.value)
    await reloadStore()
    ElMessage.success(`已生成并导入 ${n} 个接口到项目「${store.data.projects.find(p => p.id === importProjectId.value)?.name || ''}」`)
    selected.value = []
  } catch (e) { ElMessage.error(String(e)) }
  finally { importing.value = false }
}

async function doImportAsCases() {
  if (!selected.value.length) { ElMessage.warning('请先勾选要导入的捕获记录'); return }
  importing.value = true
  try {
    const n = await ImportCapturedAsTestCases(selected.value)
    await reloadStore()
    selected.value = []
    ElMessage.success(`已导入 ${n} 条测试用例，前往「自动化测试」运行或压测`)
    store.view = 'autotest'
  } catch (e) { ElMessage.error(String(e)) }
  finally { importing.value = false }
}

async function doExport() {
  if (!selected.value.length) { ElMessage.warning('请先勾选要导出的捕获记录'); return }
  try {
    const path = await ExportCapturedOpenAPI(selected.value, '浏览器捕获接口')
    if (path) ElMessage.success('已导出：' + path)
  } catch (e) { ElMessage.error(String(e)) }
}

async function copyOpenAPI() {
  if (!selected.value.length) { ElMessage.warning('请先勾选要导出的捕获记录'); return }
  try {
    const text = await BuildCapturedOpenAPI(selected.value, '浏览器捕获接口')
    await CopyToClipboard(text)
    ElMessage.success('OpenAPI JSON 已复制到剪贴板')
  } catch (e) { ElMessage.error(String(e)) }
}

// 将捕获记录转换为命令行生成所需的请求对象
function capturedToReq(rec) {
  const headersArr = (rec.headers || []).map(h => ({ key: h.key, value: h.value }))
  const queryArr = (rec.query || []).map(q => ({ key: q.key, value: q.value }))
  return {
    method: rec.method || 'GET',
    url: rec.url || '',
    headersArr,
    queryArr,
    body: rec.body || '',
    bodyType: rec.bodyType || (rec.body ? 'text' : 'none'),
  }
}

async function copyAsCommand(key) {
  if (!detail.value) return
  const fmt = FORMATS.find(f => f.key === key)
  if (!fmt) return
  try {
    const text = fmt.gen(capturedToReq(detail.value))
    await CopyToClipboard(text)
    ElMessage.success(`已复制为 ${fmt.label} 命令`)
  } catch (e) { ElMessage.error(String(e)) }
}

onMounted(() => {
  refresh()
  timer.value = setInterval(refresh, 2500)
})
onBeforeUnmount(() => { if (timer.value) clearInterval(timer.value) })
</script>

<template>
  <div class="main-area">
    <div class="panel-page" style="max-width:1080px; margin:0 auto; width:100%">
      <h2 style="margin:6px 0 4px">🌐 请求捕获</h2>
      <div style="color:#86909c; font-size:13px; margin-bottom:16px">
        通过浏览器扩展监听指定网页的请求，自动回传到本机捕获服务，生成接口文档并导出 OpenAPI。
        扩展源码位于项目 <code>browser-extension/</code> 目录，按 README 加载后即可使用。
      </div>

      <!-- 服务状态 -->
      <div class="card">
        <div class="card-title">捕获服务</div>
        <div class="svc-row">
          <div class="svc-item">
            <div class="svc-label">状态</div>
            <el-tag :type="info.running ? 'success' : 'info'" effect="dark">
              {{ info.running ? '运行中' : '已停止' }}
            </el-tag>
          </div>
          <div class="svc-item">
            <div class="svc-label">捕获地址</div>
            <div class="svc-val">
              <code>{{ info.url || '—' }}</code>
              <el-button v-if="info.running" size="small" text type="primary" @click="copyAddr">复制</el-button>
            </div>
          </div>
          <div class="svc-item svc-token">
            <div class="svc-label">鉴权 Token（填入浏览器扩展）</div>
            <div class="svc-val">
              <code class="token">{{ maskToken(info.token) }}</code>
              <el-button size="small" text type="primary" @click="showToken = !showToken">{{ showToken ? '隐藏' : '显示' }}</el-button>
              <el-button v-if="info.running" size="small" text type="primary" @click="copyToken">复制</el-button>
            </div>
          </div>
          <div class="svc-item">
            <div class="svc-label">已捕获</div>
            <div class="svc-val"><b style="color:#165dff">{{ info.count }}</b> 条</div>
          </div>
        </div>
        <div style="margin-top:12px; display:flex; gap:10px; flex-wrap:wrap">
          <el-button :type="info.running ? 'danger' : 'success'" :loading="loading" @click="toggleServer">
            {{ info.running ? '停止服务' : '启动服务' }}
          </el-button>
          <el-button @click="refresh">刷新列表</el-button>
        </div>
        <div class="hint">
          说明：捕获服务<b>不会随应用自动启动</b>，需在此手动点击「启动服务」；关闭应用或点击「停止服务」即可释放端口。<br />
          提示：浏览器扩展需配置与上方一致的「捕获地址」和「Token」。扩展仅在命中所配置的监控网址（支持 * 通配）时才上报请求，
          包括方法、URL、请求头、Query、请求体以及响应状态、响应头与响应体（用于自动生成请求/响应字段文档）。
        </div>
      </div>

      <!-- 操作工具栏 -->
      <div class="card">
        <div class="toolbar">
          <span class="sel-count">已选 <b>{{ selected.length }}</b> / {{ list.length }} 条</span>
          <el-button size="small" @click="selectAll">全选</el-button>
          <el-button size="small" @click="clearSel">取消</el-button>
          <el-divider direction="vertical" />
          <span class="imp-label">导入到</span>
          <el-select v-model="importProjectId" size="small" style="width:160px">
            <el-option v-for="p in store.data.projects" :key="p.id" :label="p.name" :value="p.id" />
          </el-select>
          <el-tree-select v-model="importDirId" :data="projectDirOptions" check-strictly :render-after-expand="false"
            size="small" style="width:200px" placeholder="选择目录（根目录）" default-expand-all />
          <el-button size="small" type="primary" :loading="importing" @click="doImport">生成接口并导入</el-button>
          <el-button size="small" type="success" :loading="importing" @click="doImportAsCases">导入为测试用例</el-button>
          <el-divider direction="vertical" />
          <el-button size="small" @click="doExport">导出 OpenAPI</el-button>
          <el-button size="small" @click="copyOpenAPI">复制 OpenAPI</el-button>
        </div>
      </div>

      <!-- 捕获列表 -->
      <div class="card">
        <div class="card-title">
          <span>已捕获请求（{{ list.length }}）</span>
          <el-button size="small" type="danger" plain :disabled="!list.length" @click="clearAll">一键清空</el-button>
        </div>
        <el-table ref="tableRef" v-loading="loading" :data="list" row-key="id" style="width:100%" @selection-change="rows => selected = rows.map(r => r.id)">
          <el-table-column type="selection" width="46" reserve-selection />
          <el-table-column label="方法" width="84">
            <template #default="{ row }">
              <span class="method-tag" :class="'m-' + (row.method || 'GET')">{{ row.method || 'GET' }}</span>
            </template>
          </el-table-column>
          <el-table-column label="URL" min-width="280" show-overflow-tooltip>
            <template #default="{ row }">
              <div class="u">{{ row.url }}</div>
              <div v-if="row.matchedUrl" class="matched">命中规则：{{ row.matchedUrl }}</div>
            </template>
          </el-table-column>
          <el-table-column label="状态" width="90">
            <template #default="{ row }">
              <span :class="['st', (row.statusCode >= 200 && row.statusCode < 400) ? 'ok' : 'err']">
                {{ row.statusCode || '—' }}
              </span>
              <span v-if="row.error" class="st err" title="请求错误">⚠</span>
            </template>
          </el-table-column>
          <el-table-column label="耗时" width="90">
            <template #default="{ row }">{{ row.durationMs ? row.durationMs + 'ms' : '—' }}</template>
          </el-table-column>
          <el-table-column label="时间" width="130">
            <template #default="{ row }">{{ fmtTime(row.capturedAt) }}</template>
          </el-table-column>
          <el-table-column label="操作" width="70">
            <template #default="{ row }">
              <el-button size="small" text type="primary" @click="detail = row">详情</el-button>
            </template>
          </el-table-column>
        </el-table>
        <div v-if="!list.length" class="empty">暂无捕获记录。请在浏览器扩展中配置监控网址并操作目标网页，请求将自动出现在此处。</div>
      </div>
    </div>

    <!-- 详情弹窗 -->
    <el-dialog v-model="detail" title="请求详情" width="760px" @close="detail = null">
      <template v-if="detail">
        <div class="dt-line"><span>方法</span><b>{{ detail.method }}</b></div>
        <div class="dt-line">
          <span>URL</span>
          <b style="word-break:break-all">{{ detail.url }}</b>
          <el-dropdown trigger="click" size="small" @command="copyAsCommand" style="margin-left:auto">
            <el-button size="small" type="primary" plain>复制为 ▾</el-button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item v-for="f in FORMATS" :key="f.key" :command="f.key">{{ f.label }}</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
        <div class="dt-line"><span>页面</span><b style="word-break:break-all">{{ detail.pageUrl }}</b></div>
        <div class="dt-line"><span>状态</span><b>{{ detail.statusCode }} {{ detail.statusText }}</b></div>
        <div v-if="detail.error" class="dt-line"><span>错误</span><b style="color:#f53f3f">{{ detail.error }}</b></div>

        <h4>请求头</h4>
        <pre class="kv">{{ detail.headers && detail.headers.length ? detail.headers.map(h => h.key + ': ' + h.value).join('\n') : '（无）' }}</pre>
        <h4>Query 参数</h4>
        <pre class="kv">{{ detail.query && detail.query.length ? detail.query.map(q => q.key + ' = ' + q.value).join('\n') : '（无）' }}</pre>
        <h4>请求体 ({{ detail.bodyType }})</h4>
        <pre class="body">{{ detail.body || '（无）' }}</pre>
        <h4>响应头</h4>
        <pre class="kv">{{ detail.respHeaders ? Object.entries(detail.respHeaders).map(([k, v]) => k + ': ' + v).join('\n') : '（无）' }}</pre>
        <h4>响应体{{ detail.respIsJson ? ' (JSON)' : '' }}</h4>
        <pre class="body">{{ detail.respBody || '（无）' }}</pre>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.card { background:#fff; border:1px solid #e5e6eb; border-radius:10px; padding:16px 18px; margin-bottom:16px; }
.card-title { font-weight:600; font-size:14px; margin-bottom:12px; display:flex; align-items:center; justify-content:space-between; gap:12px; }
.svc-row { display:flex; gap:24px; flex-wrap:wrap; align-items:flex-start; }
.svc-item { display:flex; flex-direction:column; gap:6px; }
.svc-label { font-size:12px; color:#86909c; }
.svc-val { display:flex; align-items:center; gap:8px; }
.svc-val code { background:#f7f8fa; border:1px solid #e5e6eb; border-radius:6px; padding:4px 8px; font-size:12px; }
.svc-token { min-width:320px; }
.svc-token .token { max-width:300px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.hint { margin-top:12px; font-size:12px; color:#86909c; background:#f7f8fa; border-radius:8px; padding:10px 12px; line-height:1.6; }
.toolbar { display:flex; align-items:center; gap:8px; flex-wrap:wrap; }
.sel-count { font-size:13px; color:#4e5969; }
.imp-label { font-size:13px; color:#86909c; }
.u { font-size:13px; word-break:break-all; }
.matched { font-size:11px; color:#165dff; margin-top:2px; }
.st { font-weight:600; }
.st.ok { color:#00b42a; }
.st.err { color:#f53f3f; }
.empty { color:#c9cdd4; text-align:center; padding:30px 0; font-size:13px; }
:deep(.el-table) { font-size:13px; }
.dt-line { display:flex; gap:10px; padding:5px 0; font-size:13px; border-bottom:1px dashed #f2f3f5; }
.dt-line span { width:60px; color:#86909c; flex-shrink:0; }
.dt-line b { font-weight:500; }
h4 { margin:14px 0 6px; font-size:13px; color:#4e5969; }
pre.kv, pre.body { background:#1d2129; color:#e5e6eb; border-radius:8px; padding:10px 12px; font-size:12px; max-height:240px; overflow:auto; white-space:pre-wrap; word-break:break-all; margin:0; }
</style>
