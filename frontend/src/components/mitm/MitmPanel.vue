<template>
  <div class="mitm-panel">
    <!-- 顶部控制栏 -->
    <div class="mitm-top">
      <div class="mitm-title">
        <h2>网络抓包 (MITM)</h2>
        <span class="sub">轻量级流量解密 / API 文档自动生成（Fiddler / Charles 替代方案）</span>
      </div>
      <div class="mitm-actions">
        <template v-if="!status.running">
          <el-input v-model="proxyAddr" size="small" style="width: 190px"
            placeholder="监听地址 如 127.0.0.1:8888" title="代理监听地址，0 表示随机端口" />
          <el-switch v-model="sysProxy" size="small" active-text="切换系统代理" @change="setSysProxy" />
          <el-button type="success" @click="startSniff" :loading="starting">开始抓包</el-button>
        </template>
        <template v-else>
          <el-tag type="success" effect="dark">抓包中 · {{ status.proxyAddr }}</el-tag>
          <el-tag v-if="status.systemProxy" type="info">系统代理已开</el-tag>
          <el-tag v-else type="warning">仅监听端口</el-tag>
          <el-button type="primary" plain size="small" @click="copyProxyAddr" title="复制代理地址到剪贴板">复制代理地址</el-button>
          <el-button type="danger" @click="stopSniff">停止并保存</el-button>
        </template>
        <el-button @click="installCA" :disabled="status.caInstalled" :loading="installing">
          {{ status.caInstalled ? 'CA 已安装' : '安装根证书' }}
        </el-button>
        <el-button @click="openCADialog">查看证书</el-button>
      </div>
    </div>

    <el-alert
      v-if="!status.caInstalled && !status.running"
      type="warning"
      :closable="false"
      show-icon
      title="HTTPS 解密需安装根证书"
      description="点击右上角「安装根证书」将 ApiTool 自签 CA 加入系统信任库（需以管理员身份运行）。未安装时仅能抓取明文 HTTP，HTTPS 会被降级透传（不解密）。" />

    <el-alert
      v-if="status.error"
      type="error"
      :closable="false"
      show-icon
      :title="status.error" />

    <div class="mitm-body" :class="{ resizing }">
      <!-- 左：流量列表 + 过滤 -->
      <div class="mitm-left" :style="{ width: leftWidth + '%' }">
        <!-- 过滤条件 -->
        <div class="filter-box">
          <div class="filter-row">
            <span class="fl">Host 过滤（逗号分隔，留空=全部）</span>
            <el-input v-model="filterHosts" size="small" placeholder="example.com, api.test.cn" @change="applyFilter" />
          </div>
          <div class="filter-row">
            <span class="fl">排除 Host</span>
            <el-input v-model="filterExclude" size="small" placeholder="localhost, 127.0.0.1" @change="applyFilter" />
          </div>
          <div class="filter-row">
            <span class="fl">协议勾选</span>
            <el-checkbox-group v-model="filterProtocols" size="small" @change="applyFilter">
              <el-checkbox-button value="http">HTTP</el-checkbox-button>
              <el-checkbox-button value="https">HTTPS</el-checkbox-button>
              <el-checkbox-button value="websocket">WebSocket</el-checkbox-button>
              <el-checkbox-button value="sse">SSE</el-checkbox-button>
            </el-checkbox-group>
            <el-tooltip content="勾选哪些协议就解析哪些；一个都不选则全部抓取解析" placement="top">
              <span style="color:#86909c;margin-left:6px;cursor:help;font-size:13px">?</span>
            </el-tooltip>
          </div>
          <div class="filter-row">
            <el-checkbox v-model="filterOnlyHTTP" @change="applyFilter">仅抓取 HTTP/HTTPS</el-checkbox>
            <el-checkbox v-model="autoDoc" border size="small" title="抓取时自动为每个请求生成文档草稿">自动生成文档</el-checkbox>
          </div>
        </div>

        <!-- 实时流量 -->
        <div class="traffic-head">
          <span>实时流量（{{ liveRecords.length }}）</span>
          <div class="th-actions">
            <template v-if="selectedIds.length">
              <el-button link type="primary" size="small" @click="openBatchImport">批量导入（{{ selectedIds.length }}）</el-button>
              <el-button link size="small" @click="selectedIds = []">取消选择</el-button>
            </template>
            <el-button link type="primary" size="small" @click="clearLive">清空</el-button>
          </div>
        </div>
        <div class="traffic-list">
          <div v-if="!liveRecords.length" class="empty">暂无流量，开始抓包后系统流量将实时显示在这里</div>
          <div
            v-for="r in liveRecords"
            :key="r.id"
            class="traffic-item"
            :class="{ active: selected && selected.id === r.id, checked: selectedIds.includes(r.id) }"
            @click="selectRecord(r)">
            <el-checkbox size="small" :model-value="selectedIds.includes(r.id)" @click.stop
              @change="(v) => toggleSelect(r.id, v)" />
            <span class="proto" :class="'p-' + r.protocol.toLowerCase()">{{ r.protocol }}</span>
            <span class="method" v-if="r.method">{{ r.method }}</span>
            <span class="url" :title="r.url">{{ r.url || r.host }}</span>
            <span class="status" v-if="r.statusCode">{{ r.statusCode }}</span>
          </div>
        </div>
      </div>

      <!-- 可拖拽分隔条 -->
      <div class="resize-bar" title="拖动调整宽度" @mousedown="startResize"></div>

      <!-- 右：详情 -->
      <div class="mitm-right">
        <template v-if="selected">
          <div class="detail-head">
            <el-tag size="small">{{ selected.method || selected.protocol }}</el-tag>
            <span class="du">{{ selected.url || selected.host }}</span>
            <el-button link type="primary" size="small" @click="copyText(selected.reqBody)">复制请求体</el-button>
          </div>
          <el-tabs v-model="detailTab">
            <el-tab-pane label="概览" name="overview">
              <div class="kv"><b>协议</b><span>{{ selected.protocol }} <i v-if="!selected.decrypted">（未解密）</i></span></div>
              <div class="kv"><b>状态</b><span>{{ selected.statusCode || '—' }} {{ selected.statusText }}</span></div>
              <div class="kv"><b>耗时</b><span>{{ selected.durationMs }} ms</span></div>
              <div class="kv"><b>Host</b><span>{{ selected.host }}</span></div>
              <div class="kv"><b>说明</b><span>{{ selected.note || '—' }}</span></div>
            </el-tab-pane>
            <el-tab-pane label="请求头" name="reqh">
              <pre class="code">{{ kvToText(selected.reqHeaders) }}</pre>
            </el-tab-pane>
            <el-tab-pane label="请求体" name="reqb">
              <pre class="code">{{ selected.reqBody || '（无）' }}</pre>
            </el-tab-pane>
            <el-tab-pane label="响应头" name="resh">
              <pre class="code">{{ kvToText(selected.respHeaders) }}</pre>
            </el-tab-pane>
            <el-tab-pane label="响应体" name="resb">
              <pre class="code">{{ selected.respBody || '（无）' }}</pre>
            </el-tab-pane>
          </el-tabs>

          <div class="detail-actions">
            <el-button size="small" type="primary" @click="openImportDialog">生成接口并导入接口树</el-button>
          </div>
        </template>
        <div v-else class="detail-empty">点击左侧流量查看详情</div>
      </div>
    </div>

    <!-- 会话与导出 -->
    <div class="mitm-sessions">
      <div class="sess-head">
        <span>抓包会话（已保存 {{ sessions.length }}）</span>
        <el-button size="small" type="primary" @click="refreshSessions">刷新</el-button>
      </div>
      <el-table :data="sessions" size="small" empty-text="暂无会话">
        <el-table-column prop="name" label="会话名" min-width="180" />
        <el-table-column prop="startedAt" label="开始时间" width="180" />
        <el-table-column label="记录数" width="90">
          <template #default="{ row }">{{ (row.records || []).length }}</template>
        </el-table-column>
        <el-table-column label="操作" width="220">
          <template #default="{ row }">
            <el-button link type="primary" @click="exportSession(row.id, row.name)">导出 OpenAPI</el-button>
            <el-button link type="danger" @click="deleteSession(row.id)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- 导入接口树弹窗 -->
    <el-dialog v-model="importDialog" title="生成接口并导入接口树" width="480px">
      <p style="color:#86909c; font-size:13px; margin:0 0 12px">
        将当前选中的流量记录「{{ selected ? (selected.method + ' ' + (selected.path || selected.host)) : '' }}」转换为接口定义，写入所选项目/目录。
      </p>
      <div class="import-row">
        <span class="ir-label">目标项目</span>
        <el-select v-model="importProjectId" size="small" style="width: 200px">
          <el-option v-for="p in store.data.projects" :key="p.id" :label="p.name" :value="p.id" />
        </el-select>
      </div>
      <div class="import-row">
        <span class="ir-label">目标目录</span>
        <el-tree-select v-model="importDirId" :data="projectDirOptions" check-strictly
          :render-after-expand="false" size="small" style="width: 200px"
          placeholder="选择目录（默认根目录）" default-expand-all />
      </div>
      <template #footer>
        <el-button size="small" @click="importDialog = false">取消</el-button>
        <el-button size="small" type="primary" :loading="importing" @click="doImportApi">生成并导入</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="caDialog" title="根证书 (CA) 信息" width="640px">
      <p>将以下根证书安装到系统「受信任的根证书颁发机构」即可解密 HTTPS。也可点击「安装根证书」由程序自动安装（需管理员）。</p>
      <div class="kv"><b>指纹(SHA1)</b><span>{{ status.caFingerprint }}</span></div>
      <pre class="code ca">{{ caPem }}</pre>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onBeforeUnmount } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  SniffStatus, SniffStart, SniffStop, SniffSetFilter, SniffListSessions,
  SniffGetSession, SniffDeleteSession, SniffExportOpenAPI, SniffInstallCA, SniffCAPEM,
  SniffSetSystemProxy, SniffGenerateApiFromSession, SniffGenerateApiFromRecords, CopyToClipboard
} from '../../../wailsjs/go/main/App'
import { EventsOn, EventsOff } from '../../../wailsjs/runtime/runtime'
import { store, reloadStore } from '../../store'

const status = reactive({ running: false, proxyAddr: '', caInstalled: false, caFingerprint: '', systemProxy: false, error: '' })
const proxyAddr = ref('127.0.0.1:8888')
const sysProxy = ref(false)
const starting = ref(false)
const installing = ref(false)
const liveRecords = ref([])
const selected = ref(null)
const selectedIds = ref([])
const detailTab = ref('overview')
const sessions = ref([])
const caDialog = ref(false)
const caPem = ref('')

const filterHosts = ref('')
const filterExclude = ref('localhost, 127.0.0.1')
const filterOnlyHTTP = ref(false)
const filterProtocols = ref([]) // http/https/websocket/sse，空=全部解析
const autoDoc = ref(false)

// 导入接口树
const importDialog = ref(false)
const importProjectId = ref(store.data.currentProjectId || '')
const importDirId = ref('')
const importing = ref(false)
const projectDirOptions = computed(() => {
  const p = store.data.projects.find(x => x.id === importProjectId.value)
  if (!p) return [{ value: '', label: '根目录' }]
  const nodes = (parentId) => p.dirs
    .filter(d => d.parentId === parentId)
    .sort((a, b) => (a.sort || 0) - (b.sort || 0))
    .map(d => ({ value: d.id, label: d.name, children: nodes(d.id) }))
  return [{ value: '', label: '根目录', children: nodes('') }]
})

let recOff = null
let statusOff = null
let errOff = null

onMounted(async () => {
  try {
    const s = await SniffStatus()
    Object.assign(status, s)
    sysProxy.value = !!s.systemProxy
  } catch (e) { /* ignore */ }
  try { sessions.value = await SniffListSessions() } catch (e) {}
  recOff = EventsOn('sniff:record', (rec) => {
    liveRecords.value.push(rec)
    // 自动生成：勾选了「自动生成文档」且为有效 HTTP 流量时自动导入当前项目
    if (autoDoc.value && rec.method && rec.url) {
      autoImport([rec])
    }
  })
  statusOff = EventsOn('sniff:status', (s) => { Object.assign(status, s); sysProxy.value = !!s.systemProxy })
  errOff = EventsOn('sniff:error', (msg) => {
    ElMessage.warning(msg || 'HTTPS 解密异常，请确认根证书已安装并信任')
  })
})

onBeforeUnmount(() => {
  if (recOff) EventsOff('sniff:record', recOff)
  if (statusOff) EventsOff('sniff:status', statusOff)
  if (errOff) EventsOff('sniff:error', errOff)
})

function setSysProxy(val) {
  SniffSetSystemProxy(!!val).catch(() => {})
}

async function startSniff() {
  starting.value = true
  try {
    applyFilter()
    const addr = proxyAddr.value.trim() || '127.0.0.1:8888'
    await SniffStart(addr)
    liveRecords.value = []
    if (!status.caInstalled) {
      ElMessage.warning('已启动（仅 HTTP 明文）。解密 HTTPS 请先安装根证书')
    } else {
      ElMessage.success('抓包已启动，监听 ' + (status.proxyAddr || addr) + '，系统流量将通过代理经过本工具')
    }
  } catch (e) {
    ElMessage.error('启动失败：' + String(e))
  } finally {
    starting.value = false
  }
}

async function stopSniff() {
  try {
    await SniffStop()
    sessions.value = await SniffListSessions()
    ElMessage.success('已停止并保存会话')
  } catch (e) {
    ElMessage.error('停止失败：' + String(e))
  }
}

function applyFilter() {
  const f = {
    host: filterHosts.value,
    excludeHosts: filterExclude.value.split(',').map(x => x.trim()).filter(Boolean),
    onlyHttp: filterOnlyHTTP.value,
    protocols: filterProtocols.value,
  }
  SniffSetFilter(f).catch(() => {})
}

async function installCA() {
  installing.value = true
  try {
    await SniffInstallCA()
    status.caInstalled = true
    ElMessage.success('根证书已安装，现在可解密 HTTPS')
  } catch (e) {
    ElMessage.error('安装失败：' + String(e) + '\n请尝试以管理员身份运行本程序，或手动导入 ca.pem')
  } finally {
    installing.value = false
  }
}

async function openCADialog() {
  try { caPem.value = await SniffCAPEM() } catch (e) { caPem.value = '' }
  caDialog.value = true
}

function selectRecord(r) { selected.value = r; detailTab.value = 'overview' }
function toggleSelect(id, val) {
  if (val) {
    if (!selectedIds.value.includes(id)) selectedIds.value.push(id)
  } else {
    selectedIds.value = selectedIds.value.filter(x => x !== id)
  }
}
function clearLive() { liveRecords.value = []; selected.value = null; selectedIds.value = [] }

function selectedRecords() {
  const set = new Set(selectedIds.value)
  return liveRecords.value.filter(r => set.has(r.id))
}

function openBatchImport() {
  if (!selectedIds.value.length) {
    ElMessage.warning('请先勾选要导入的流量记录')
    return
  }
  importProjectId.value = store.data.currentProjectId || ''
  importDirId.value = ''
  importDialog.value = true
}

// 实际执行导入（批量或单条）
async function doImportApi() {
  const batch = selectedRecords()
  if (batch.length) {
    await doBatchImport(batch)
    return
  }
  // 单条：走会话导入（兼容从会话打开的记录）
  if (selected.value && selected.value.sessionId) {
    const rec = selected.value
    importing.value = true
    try {
      const n = await SniffGenerateApiFromSession(rec.sessionId, [rec.id], importProjectId.value, importDirId.value)
      await reloadStore()
      ElMessage.success(`已生成并导入 ${n} 个接口`)
      importDialog.value = false
    } catch (e) {
      ElMessage.error(String(e))
    } finally {
      importing.value = false
    }
    return
  }
  if (selected.value) {
    await doBatchImport([selected.value])
    return
  }
  ElMessage.warning('请先选择或勾选要导入的流量记录')
}

async function doBatchImport(records) {
  if (!records.length) return
  importing.value = true
  try {
    const n = await SniffGenerateApiFromRecords(records, importProjectId.value, importDirId.value)
    await reloadStore()
    const p = store.data.projects.find(x => x.id === importProjectId.value)
    ElMessage.success(`已生成并导入 ${n} 个接口到「${p?.name || ''}」`)
    importDialog.value = false
    selectedIds.value = []
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    importing.value = false
  }
}

// 自动生成（autoDoc）：静默导入，失败不打扰
async function autoImport(records) {
  try {
    const pid = store.data.currentProjectId
    if (!pid) return
    await SniffGenerateApiFromRecords(records, pid, '')
    await reloadStore()
  } catch (e) { /* 自动生成失败静默 */ }
}

// ---- 左右宽度拖拽 ----
const leftWidth = ref(48)
let resizing = false

function startResize(e) {
  resizing = true
  document.body.classList.add('resizing')
  document.addEventListener('mousemove', onResize)
  document.addEventListener('mouseup', stopResize)
  e.preventDefault()
}

function onResize(e) {
  if (!resizing) return
  const body = document.querySelector('.mitm-body')
  if (!body) return
  const rect = body.getBoundingClientRect()
  const total = rect.width
  if (!total) return
  const pct = ((e.clientX - rect.left) / total) * 100
  // 限制在 25% ~ 75% 之间
  leftWidth.value = Math.min(75, Math.max(25, pct))
}

function stopResize() {
  resizing = false
  document.body.classList.remove('resizing')
  document.removeEventListener('mousemove', onResize)
  document.removeEventListener('mouseup', stopResize)
}

function kvToText(kvs) {
  if (!kvs || !kvs.length) return '（无）'
  return kvs.filter(k => k.enabled !== false).map(k => k.key + ': ' + k.value).join('\n')
}

async function refreshSessions() {
  try { sessions.value = await SniffListSessions() } catch (e) {}
}

async function exportSession(id, name) {
  try {
    const path = await SniffExportOpenAPI(id, name || 'openapi')
    if (path) ElMessage.success('已导出：' + path)
    else ElMessage.success('已生成 OpenAPI 文档')
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function deleteSession(id) {
  try {
    await ElMessageBox.confirm('确定删除该抓包会话？', '提示', { type: 'warning' })
    await SniffDeleteSession(id)
    sessions.value = await SniffListSessions()
    ElMessage.success('已删除')
  } catch (e) { if (e !== 'cancel') ElMessage.error(String(e)) }
}

function openImportDialog() {
  if (!selected.value) {
    ElMessage.warning('请先选择一条流量记录')
    return
  }
  importProjectId.value = store.data.currentProjectId || ''
  importDirId.value = ''
  importDialog.value = true
}

function copyText(t) {
  if (!t) return
  CopyToClipboard(t).catch(() => {})
}

async function copyProxyAddr() {
  const addr = status.proxyAddr || proxyAddr.value
  if (!addr) {
    ElMessage.warning('暂无代理地址')
    return
  }
  try {
    await CopyToClipboard(addr)
    ElMessage.success('已复制代理地址：' + addr)
  } catch (e) {
    ElMessage.error('复制失败：' + String(e))
  }
}
</script>

<style scoped>
.mitm-panel { display: flex; flex-direction: column; height: 100%; padding: 16px; gap: 12px; box-sizing: border-box; }
.mitm-top { display: flex; justify-content: space-between; align-items: center; }
.mitm-title h2 { margin: 0; font-size: 18px; }
.mitm-title .sub { color: #86909c; font-size: 12px; margin-left: 8px; }
.mitm-actions { display: flex; gap: 8px; align-items: center; }
.mitm-body { display: flex; gap: 4px; flex: 1; min-height: 0; }
.mitm-left { flex-shrink: 0; display: flex; flex-direction: column; gap: 8px; min-height: 0; transition: width .08s ease; }
.mitm-right { flex: 1; min-width: 0; border-left: 1px solid #e5e6eb; padding-left: 12px; min-height: 0; overflow: auto; }
.resize-bar { width: 6px; cursor: col-resize; background: transparent; flex-shrink: 0; border-radius: 3px; transition: background .15s; }
.resize-bar:hover, .mitm-body.resizing .resize-bar { background: #165dff; }
.mitm-body.resizing { user-select: none; cursor: col-resize; }
.mitm-body.resizing .mitm-left, .mitm-body.resizing .mitm-right { pointer-events: none; transition: none; }
.filter-box { border: 1px solid #e5e6eb; border-radius: 8px; padding: 10px; display: flex; flex-direction: column; gap: 8px; }
.filter-row { display: flex; align-items: center; gap: 8px; }
.filter-row .fl { font-size: 12px; color: #4e5969; width: 180px; }
.traffic-head, .sess-head { display: flex; justify-content: space-between; align-items: center; font-weight: 600; font-size: 13px; }
.traffic-list { flex: 1; overflow: auto; border: 1px solid #e5e6eb; border-radius: 8px; min-height: 120px; }
.traffic-item { display: flex; gap: 8px; align-items: center; padding: 6px 10px; border-bottom: 1px solid #f2f3f5; cursor: pointer; font-size: 13px; }
.traffic-item:hover { background: #f7f8fa; }
.traffic-item.active { background: #e8f3ff; }
.traffic-item.checked { background: #f0f6ff; }
.traffic-head .th-actions { display: flex; gap: 4px; align-items: center; }
.traffic-item .proto { font-size: 11px; padding: 1px 6px; border-radius: 4px; color: #fff; background: #86909c; }
.traffic-item .p-https { background: #165dff; }
.traffic-item .p-http { background: #00b42a; }
.traffic-item .p-tls { background: #ff7d00; }
.traffic-item .p-ssh, .traffic-item .p-ftp, .traffic-item .p-smtp { background: #eb0aa6; }
.traffic-item .method { color: #165dff; font-weight: 600; }
.traffic-item .url { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: #1d2129; }
.traffic-item .status { color: #86909c; }
.empty, .detail-empty { color: #c9cdd4; font-size: 13px; text-align: center; padding: 24px 0; }
.detail-head { display: flex; gap: 8px; align-items: center; margin-bottom: 10px; }
.detail-head .du { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12px; color: #4e5969; }
.kv { display: flex; gap: 10px; padding: 4px 0; font-size: 13px; border-bottom: 1px dashed #f2f3f5; }
.kv b { color: #86909c; width: 80px; font-weight: 500; }
.code { background: #f7f8fa; border-radius: 8px; padding: 10px; font-size: 12px; white-space: pre-wrap; word-break: break-all; max-height: 320px; overflow: auto; }
.ca { max-height: 240px; }
.detail-actions { margin-top: 10px; }
.mitm-sessions { border-top: 1px solid #e5e6eb; padding-top: 8px; }
.import-row { display: flex; align-items: center; gap: 8px; margin-bottom: 10px; }
.import-row .ir-label { width: 70px; color: #4e5969; font-size: 13px; flex-shrink: 0; }
</style>
