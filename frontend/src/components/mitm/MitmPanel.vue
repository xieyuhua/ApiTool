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
        <el-button @click="openImportCADialog">导入证书</el-button>
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

    <!-- 连接/证书错误单独展示 -->
    <div v-if="errorList.length" class="mitm-errors">
      <el-alert v-for="(e, i) in errorList.slice(0, 3)" :key="i" type="error" :closable="false"
        :show-icon="false" class="err-item">
        <div class="err-head">
          <span class="err-time">{{ e.time }}</span>
          <el-tag size="small" :type="errTagType(e.type)" class="err-tag">{{ errLabel(e.type) }}</el-tag>
          <span v-if="e.host" class="err-host" @click="setLiveFilter(e.host)">@ {{ e.host }}</span>
          <span style="flex:1"></span>
          <el-button link size="small" type="primary" @click="copyText(e.msg)">复制</el-button>
          <el-button link size="small" @click="errorList.splice(i, 1)">忽略</el-button>
        </div>
        <div class="err-body">{{ e.msg }}</div>
      </el-alert>
      <div class="err-foot">
        <span>共 {{ errorList.length }} 条错误</span>
        <el-button link size="small" @click="errorList = []">清空</el-button>
      </div>
    </div>

    <!-- 解决引导 -->
    <div v-if="currentGuide" class="mitm-guide" :class="'g-' + currentGuide.type">
      <div class="g-head">
        <span class="g-title">解决建议：{{ errLabel(currentGuide.type) }}</span>
        <el-button link size="small" type="primary" @click="copyText(currentGuide.solution)">复制方案</el-button>
      </div>
      <div class="g-body">{{ currentGuide.solution }}</div>
      <div class="g-act" v-if="currentGuide.action">
        <el-button size="small" @click="currentGuide.action.fn()">{{ currentGuide.action.label }}</el-button>
      </div>
    </div>

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
          <span>实时流量（{{ filteredRecords.length }} / {{ liveRecords.length }}）</span>
          <div class="th-actions">
            <el-input v-model="liveFilter" size="small" placeholder="模糊过滤 host/url/方法" clearable style="width:120px" />
            <el-radio-group v-model="viewMode" size="small">
              <el-radio-button value="list">平铺</el-radio-button>
              <el-radio-button value="group">分组</el-radio-button>
            </el-radio-group>
            <el-switch v-model="onlyErr" size="small" active-text="仅异常" style="--el-switch-on-color:#f56c6c"
              title="仅显示有解密失败记录的连接" @change="onlyErr = !!onlyErr" />
            <el-select v-if="onlyErr" v-model="errTypeFilter" size="small" placeholder="错误类型" style="width:130px" clearable>
              <el-option v-for="t in errTypeOptions" :key="t.value" :label="t.label" :value="t.value" />
            </el-select>
            <el-button link size="small" @click="selectAll">全选</el-button>
            <template v-if="selectedIds.length">
              <el-button link type="primary" size="small" @click="openBatchImport">批量导入（{{ selectedIds.length }}）</el-button>
              <el-button link size="small" @click="selectedIds = []">取消选择</el-button>
            </template>
            <el-button link type="primary" size="small" @click="clearLive">清空</el-button>
          </div>
        </div>
        <div class="traffic-list">
          <div v-if="!filteredRecords.length" class="empty">
            {{ liveRecords.length ? '没有匹配的流量（可调整过滤条件）' : '暂无流量，开始抓包后系统流量将实时显示在这里' }}
          </div>

          <!-- 平铺模式 -->
          <template v-if="viewMode === 'list'">
            <div
              v-for="r in filteredRecords"
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
          </template>

          <!-- 按 host 分组模式 -->
          <template v-else>
            <div v-for="g in groupedRecords" :key="g.key" class="grp">
              <div class="grp-head" @click="toggleGroup(g.key)">
                <span class="grp-arrow" :class="{ open: !g.collapsed }">▶</span>
                <span class="grp-host">{{ g.host || '(未知)' }}</span>
                <span class="grp-count">{{ g.records.length }}</span>
                <span class="grp-check" @click.stop>
                  <el-checkbox size="small" :model-value="g.allChecked" :indeterminate="g.someChecked"
                    @change="(v) => toggleGroupSelect(g, v)" />
                </span>
              </div>
              <template v-if="!g.collapsed">
                <div
                  v-for="r in g.records"
                  :key="r.id"
                  class="traffic-item grp-item"
                  :class="{ active: selected && selected.id === r.id, checked: selectedIds.includes(r.id) }"
                  @click="selectRecord(r)">
                  <el-checkbox size="small" :model-value="selectedIds.includes(r.id)" @click.stop
                    @change="(v) => toggleSelect(r.id, v)" />
                  <span class="proto" :class="'p-' + r.protocol.toLowerCase()">{{ r.protocol }}</span>
                  <span class="method" v-if="r.method">{{ r.method }}</span>
                  <span class="url" :title="r.url">{{ r.url }}</span>
                  <span class="status" v-if="r.statusCode">{{ r.statusCode }}</span>
                </div>
              </template>
            </div>
          </template>
        </div>
      </div>

      <!-- 可拖拽分隔条 -->
      <div class="resize-bar" title="拖动调整宽度" @mousedown="startResize"></div>

      <!-- 右：详情 -->
      <div class="mitm-right">
        <template v-if="selected">
          <div class="detail-head">
            <div class="dh-meta">
              <el-tag size="small" type="primary">{{ selected.method || selected.protocol }}</el-tag>
              <span class="du" :title="selected.url">{{ selected.url || selected.host }}</span>
            </div>
            <div class="dh-actions">
              <el-button size="small" @click="copyText(selected.reqBody)">复制请求体</el-button>
              <el-button size="small" type="primary" @click="openImportDialog">生成接口并导入接口树</el-button>
            </div>
          </div>
          <el-tabs v-model="detailTab" class="detail-tabs">
            <el-tab-pane label="概览" name="overview">
              <div class="kv"><b>协议</b><span>{{ selected.protocol }} <i v-if="!selected.decrypted">（未解密）</i></span></div>
              <div class="kv"><b>状态</b><span>{{ selected.statusCode || '—' }} {{ selected.statusText }}</span></div>
              <div class="kv"><b>耗时</b><span>{{ selected.durationMs }} ms</span></div>
              <div class="kv"><b>Host</b><span>{{ selected.host }}</span></div>
              <div class="kv"><b>说明</b><span>{{ selected.note || '—' }}</span></div>
            </el-tab-pane>
            <el-tab-pane label="请求头" name="reqh">
              <div class="body-toolbar">
                <el-button link type="primary" size="small" @click="copyText(kvToText(selected.reqHeaders))">复制</el-button>
              </div>
              <pre class="code">{{ kvToText(selected.reqHeaders) }}</pre>
            </el-tab-pane>
            <el-tab-pane label="请求体" name="reqb">
              <div class="body-toolbar">
                <el-button link type="primary" size="small" @click="copyText(displayBody(selected.reqBody, true))">复制</el-button>
                <el-button link type="primary" size="small" @click="toggleFormat('req')">
                  {{ reqFormatted ? '查看原文' : '格式化' }}
                </el-button>
              </div>
              <pre class="code">{{ displayBody(selected.reqBody, reqFormatted) }}</pre>
            </el-tab-pane>
            <el-tab-pane label="响应头" name="resh">
              <div class="body-toolbar">
                <el-button link type="primary" size="small" @click="copyText(kvToText(selected.respHeaders))">复制</el-button>
              </div>
              <pre class="code">{{ kvToText(selected.respHeaders) }}</pre>
            </el-tab-pane>
            <el-tab-pane label="响应体" name="resb">
              <template v-if="isImageResp(selected)">
                <div class="img-preview">
                  <img :src="imgSrc(selected.respBody)" alt="响应图片预览" />
                </div>
              </template>
              <template v-else>
                <div class="body-toolbar">
                  <el-button link type="primary" size="small" @click="copyText(displayBody(selected.respBody, true))">复制</el-button>
                  <el-button link type="primary" size="small" @click="toggleFormat('resp')">
                    {{ respFormatted ? '查看原文' : '格式化' }}
                  </el-button>
                </div>
                <pre class="code">{{ displayBody(selected.respBody, respFormatted) }}</pre>
              </template>
            </el-tab-pane>
          </el-tabs>
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

    <!-- 导入根证书（复用 Fiddler 等现有 CA） -->
    <el-dialog v-model="importCADialog" title="导入根证书（复用 Fiddler 等现有 CA）" width="600px">
      <div style="display:flex;align-items:center;gap:8px;margin-bottom:10px">
        <el-button size="small" type="primary" plain @click="pickCAFile" :loading="pickingFile">选择证书文件（FiddlerRoot.cer）</el-button>
        <span style="color:#86909c;font-size:12px">或手动粘贴下方 PEM</span>
      </div>
      <p style="color:#86909c;font-size:13px;margin:0 0 10px">
        导入后将替换当前 CA。解密 HTTPS 需<b>私钥</b>；<code>FiddlerRoot.cer</code> 通常只含证书、不含私钥，若缺少私钥请从 Fiddler 导出含私钥的证书，或手动填写私钥 PEM。导入后重新「安装根证书」即可生效。
      </p>
      <div class="import-row" style="align-items:flex-start">
        <span class="ir-label">证书 PEM</span>
        <el-input v-model="importCertPem" type="textarea" :rows="5" placeholder="-----BEGIN CERTIFICATE-----&#10;..." style="flex:1" />
      </div>
      <div class="import-row" style="align-items:flex-start">
        <span class="ir-label">私钥 PEM</span>
        <el-input v-model="importKeyPem" type="textarea" :rows="5" placeholder="-----BEGIN RSA PRIVATE KEY----- / -----BEGIN PRIVATE KEY-----" style="flex:1" />
      </div>
      <template #footer>
        <el-button size="small" @click="importCADialog = false">取消</el-button>
        <el-button size="small" type="primary" :loading="importingCA" @click="doImportCA">导入并应用</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onBeforeUnmount } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  SniffStatus, SniffStart, SniffStop, SniffSetFilter, SniffListSessions,
  SniffGetSession, SniffDeleteSession, SniffExportOpenAPI, SniffInstallCA, SniffCAPEM,
  SniffSetSystemProxy, SniffGenerateApiFromSession, SniffGenerateApiFromRecords,
  SniffImportCA, SniffPickCAFile, CopyToClipboard
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
const liveFilter = ref('')
const viewMode = ref('list') // list / group
const collapsedHosts = ref(new Set())
const onlyErr = ref(false) // 仅查看有解密失败记录的连接
const errTypeFilter = ref('') // 按错误类型筛选（pinning/untrusted/tls/connect/non_http，空=全部）
const errHostsByType = ref({}) // { type: Set<host> } 按类型收集解密失败 host

// 实时流量窗口内模糊过滤（host/url/方法/仅异常/按类型）
const filteredRecords = computed(() => {
  const kw = liveFilter.value.trim().toLowerCase()
  const only = onlyErr.value
  const type = errTypeFilter.value
  return liveRecords.value.filter(r => {
    if (only && r.host) {
      // 按类型筛选
      if (type) {
        const hosts = errHostsByType.value[type]
        if (!hosts || !hosts.has(r.host)) return false
      } else {
        const any = Object.values(errHostsByType.value).some(s => s && s.has(r.host))
        if (!any) return false
      }
    }
    if (!kw) return true
    return (r.host && r.host.toLowerCase().includes(kw)) ||
      (r.url && r.url.toLowerCase().includes(kw)) ||
      (r.method && r.method.toLowerCase().includes(kw))
  })
})

// 按 URL 路径分组展示（host + 路径第一段）
function groupKeyOf(r) {
  const host = r.host || '(未知)'
  let seg = ''
  if (r.path) {
    const p = r.path.split('/').filter(Boolean)
    if (p.length) seg = '/' + p[0]
  } else if (r.url) {
    try {
      const u = new URL(r.url)
      const p = u.pathname.split('/').filter(Boolean)
      if (p.length) seg = '/' + p[0]
    } catch (e) { /* ignore */ }
  }
  return { host, seg }
}

const groupedRecords = computed(() => {
  const map = {}
  filteredRecords.value.forEach(r => {
    const gk = groupKeyOf(r)
    const key = gk.host + gk.seg
    const label = gk.seg ? gk.host + gk.seg : gk.host
    if (!map[key]) map[key] = { host: label, seg: gk.seg, key, records: [], collapsed: false }
    map[key].records.push(r)
  })
  const groups = Object.values(map)
  groups.forEach(g => {
    g.collapsed = collapsedHosts.value.has(g.key)
    const set = new Set(selectedIds.value)
    const ids = g.records.map(x => x.id)
    g.allChecked = ids.every(id => set.has(id))
    g.someChecked = ids.some(id => set.has(id)) && !g.allChecked
  })
  return groups
})

function toggleGroup(key) {
  const s = new Set(collapsedHosts.value)
  if (s.has(key)) s.delete(key); else s.add(key)
  collapsedHosts.value = s
}

function toggleGroupSelect(g, val) {
  const set = new Set(selectedIds.value)
  g.records.forEach(r => {
    if (val) set.add(r.id); else set.delete(r.id)
  })
  selectedIds.value = Array.from(set)
}
const sessions = ref([])
const caDialog = ref(false)
const caPem = ref('')
const importCADialog = ref(false)
const importCertPem = ref('')
const importKeyPem = ref('')
const importingCA = ref(false)
const pickingFile = ref(false)

// 请求/响应体格式化
const reqFormatted = ref(false)
const respFormatted = ref(false)

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

const errorList = ref([])
function nowStr() {
  const d = new Date()
  return d.toLocaleTimeString('zh-CN', { hour12: false })
}

const ERR_LABELS = {
  pinning: '证书固定',
  untrusted: 'CA未信任',
  tls: 'TLS握手失败',
  connect: '连接失败',
  non_http: '非HTTP协议',
}
function errLabel(type) { return ERR_LABELS[type] || '错误' }
function errTagType(type) {
  switch (type) {
    case 'pinning': return 'danger'
    case 'untrusted': return 'warning'
    case 'tls': return 'warning'
    case 'non_http': return 'info'
    default: return 'danger'
  }
}
function setLiveFilter(host) {
  if (host) { liveFilter.value = host; viewMode.value = 'list' }
}

// 错误类型筛选选项
const errTypeOptions = [
  { value: 'pinning', label: '证书固定' },
  { value: 'untrusted', label: 'CA未信任' },
  { value: 'tls', label: 'TLS握手失败' },
  { value: 'connect', label: '连接失败' },
  { value: 'non_http', label: '非HTTP协议' },
]

// 各错误类型的解决引导
const GUIDES = {
  pinning: {
    type: 'pinning',
    solution: '目标 App/网站内置了证书固定（Certificate Pinning），即使信任根证书也会拒绝伪造证书。换任何代理/证书都无法直接解密。解决办法：1) 如为目标 App，可尝试使用 Fiddler 的证书固定绕过插件或安卓的 JustTrustMe/SSLUnpinning 等（需配合测试环境）；2) 若为第三方 SDK，查看其是否可关闭 pinning；3) 该流量只能透传查看 IP/端口，无法查看明文 body。',
  },
  untrusted: {
    type: 'untrusted',
    solution: '根证书未受系统信任，HTTPS 被降级为透传。解决办法：1) 以管理员身份运行 ApiTool；2) 点击「安装根证书」（certutil -addstore Root）；3) 重启浏览器/目标应用刷新证书缓存；4) 确认界面显示「CA 已安装」。',
    action: { label: '重新安装根证书', fn: () => installCA() },
  },
  tls: {
    type: 'tls',
    solution: 'TLS 握手失败，常见于证书固定或客户端校验。若错误同时提示证书相关（x509），请先确认根证书已安装并信任；若为 App 内证书校验，同「证书固定」处理。',
  },
  connect: {
    type: 'connect',
    solution: '连接目标失败（connectex/refused/timeout）。解决办法：1) 确认目标地址/端口可达（ping/telnet）；2) 确认走代理的应用未使用直连或 VPN；3) 该主机可能不支持 HTTP(S) 标准端口。',
  },
  non_http: {
    type: 'non_http',
    solution: '该连接为 SSH/FTP/自定义二进制等非 HTTP 协议，当前代理仅透传、不解密。如需解析需扩展协议支持（当前版本不支持）。',
  },
}
const currentGuide = computed(() => {
  const type = errTypeFilter.value
  if (type && GUIDES[type]) return GUIDES[type]
  // 未按类型筛选时，取最近一条有分类的错误作引导
  const last = errorList.value.find(e => GUIDES[e.type])
  return last ? GUIDES[last.type] : null
})

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
  errOff = EventsOn('sniff:error', (info) => {
    const obj = typeof info === 'string' ? { type: 'connect', host: '', message: info } : info
    const text = (obj && obj.message) || 'HTTPS 解密异常，请确认根证书已安装并信任'
    errorList.value.push({ time: nowStr(), type: (obj && obj.type) || 'connect', host: (obj && obj.host) || '', msg: text })
    if (errorList.value.length > 50) errorList.value = errorList.value.slice(-50)
    // 按类型收集解密失败的 host
    if (obj && obj.host && obj.type) {
      const m = { ...errHostsByType.value }
      const s = new Set(m[obj.type] || [])
      s.add(obj.host)
      m[obj.type] = s
      errHostsByType.value = m
    }
    ElMessage.warning(text)
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

function openImportCADialog() {
  importCertPem.value = ''
  importKeyPem.value = ''
  importCADialog.value = true
}

async function pickCAFile() {
  pickingFile.value = true
  try {
    const res = await SniffPickCAFile()
    if (!res) return
    if (res.certPem) importCertPem.value = res.certPem
    if (res.keyPem) {
      importKeyPem.value = res.keyPem
    } else {
      importKeyPem.value = ''
      ElMessage.warning('所选文件未包含私钥。若为 FiddlerRoot.cer（仅证书），请补充私钥 PEM，或从 Fiddler 导出含私钥的证书')
    }
  } catch (e) {
    ElMessage.error('读取证书失败：' + String(e))
  } finally {
    pickingFile.value = false
  }
}

async function doImportCA() {
  if (!importCertPem.value.trim() || !importKeyPem.value.trim()) {
    ElMessage.warning('请同时填写证书 PEM 与私钥 PEM')
    return
  }
  importingCA.value = true
  try {
    const fp = await SniffImportCA(importCertPem.value, importKeyPem.value)
    Object.assign(status, { caFingerprint: fp, caInstalled: false })
    ElMessage.success('已导入根证书（指纹 ' + fp + '）。如需解密 HTTPS，请点击「安装根证书」安装到系统信任库')
    importCADialog.value = false
  } catch (e) {
    ElMessage.error('导入失败：' + String(e))
  } finally {
    importingCA.value = false
  }
}

function selectRecord(r) { selected.value = r; detailTab.value = 'overview' }
function toggleSelect(id, val) {
  if (val) {
    if (!selectedIds.value.includes(id)) selectedIds.value.push(id)
  } else {
    selectedIds.value = selectedIds.value.filter(x => x !== id)
  }
}

function selectAll() {
  // 全选当前过滤后的记录
  const ids = filteredRecords.value.map(r => r.id)
  const set = new Set(selectedIds.value)
  ids.forEach(id => set.add(id))
  selectedIds.value = Array.from(set)
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
  // 最大不超过一半（50%），最小 25%，避免过大/过小
  leftWidth.value = Math.min(50, Math.max(25, pct))
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

// ---- 请求/响应体格式化 ----
function toggleFormat(kind) {
  if (kind === 'req') reqFormatted.value = !reqFormatted.value
  else respFormatted.value = !respFormatted.value
}

function displayBody(text, formatted) {
  if (!text) return text || '（无）'
  if (!formatted) return text
  // 尝试 JSON 美化
  try {
    return JSON.stringify(JSON.parse(text), null, 2)
  } catch (e) { /* 非 JSON，原文 */ }
  return text
}

// ---- 响应图片预览 ----
function isImageResp(r) {
  if (!r || !r.respBody) return false
  const ct = (r.respContentType || '').toLowerCase()
  if (ct && ct.startsWith('image/')) return true
  // 无 content-type 时按 base64 图片头判断
  const b = r.respBody
  return /^(\/9j\/|iVBOR|R0lGOD|SUkq|data:image\/)/i.test(b)
}

function imgSrc(body) {
  if (!body) return ''
  // 已是 data URL 直接用
  if (body.startsWith('data:image/')) return body
  // 取干净的 MIME 类型（去掉 ; 后参数）
  const rawCT = (selected.value && selected.value.respContentType) || 'image/png'
  const mime = (rawCT.split(';')[0] || 'image/png').trim().toLowerCase()
  // 去除可能的空白换行后作为 base64
  const cleaned = body.replace(/\s+/g, '')
  return `data:${mime};base64,${cleaned}`
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
.mitm-panel { flex: 1; width: 100%; min-width: 0; display: flex; flex-direction: column; height: 100%; min-height: 0; padding: 14px 16px; gap: 10px; box-sizing: border-box; overflow: hidden; }
.mitm-top { display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: 8px; }
.mitm-title h2 { margin: 0; font-size: 18px; }
.mitm-title .sub { color: #86909c; font-size: 12px; margin-left: 8px; }
.mitm-actions { display: flex; gap: 8px; align-items: center; }
.mitm-body { display: flex; gap: 4px; flex: 1; min-height: 0; overflow: hidden; }
.mitm-left { flex: 0 0 auto; min-width: 300px; max-width: 50%; display: flex; flex-direction: column; gap: 8px; min-height: 0; transition: width .08s ease; }
.mitm-right { flex: 1 1 auto; min-width: 0; border-left: 1px solid #e5e6eb; padding-left: 12px; min-height: 0; display: flex; flex-direction: column; overflow: hidden; }
.mitm-right .detail-empty { flex: 1; display: flex; align-items: center; justify-content: center; }
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
.detail-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 6px; flex-wrap: wrap; }
.detail-head .dh-meta { display: flex; align-items: center; gap: 8px; min-width: 0; flex: 1; }
.detail-head .dh-actions { display: flex; gap: 8px; flex-shrink: 0; }
.detail-head .du { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12px; color: #4e5969; max-width: 520px; }
.detail-tabs { flex: 1; min-height: 0; display: flex; flex-direction: column; }
.detail-tabs .el-tabs__content { flex: 1; overflow: auto; min-height: 0; }
.kv { display: flex; gap: 10px; padding: 4px 0; font-size: 13px; border-bottom: 1px dashed #f2f3f5; }
.kv b { color: #86909c; width: 80px; font-weight: 500; }
.code { background: #f7f8fa; border-radius: 8px; padding: 10px; font-size: 12px; white-space: pre-wrap; word-break: break-all; overflow: auto; }
.ca { max-height: 240px; }
.body-toolbar { display: flex; justify-content: flex-end; margin-bottom: 4px; min-height: 20px; }
.mitm-errors { margin: 6px 0; display: flex; flex-direction: column; gap: 4px; }
.mitm-errors .err-item { --el-alert-padding: 6px 10px; }
.mitm-errors .err-head { display: flex; align-items: center; gap: 8px; font-size: 12px; }
.mitm-errors .err-time { color: #86909c; }
.mitm-errors .err-tag { flex-shrink: 0; }
.mitm-errors .err-host { font-size: 12px; color: #165dff; cursor: pointer; }
.mitm-errors .err-host:hover { text-decoration: underline; }
.mitm-errors .err-body { font-size: 12px; color: #4e5969; word-break: break-all; margin-top: 2px; font-family: monospace; }
.mitm-errors .err-foot { display: flex; align-items: center; justify-content: space-between; font-size: 12px; color: #86909c; padding: 0 4px; }
.grp { margin-bottom: 2px; }
.grp-head { display: flex; align-items: center; gap: 6px; padding: 5px 8px; background: #f2f3f5; border-radius: 4px; cursor: pointer; font-size: 13px; }
.grp-head:hover { background: #e8eaed; }
.grp-arrow { display: inline-block; font-size: 10px; color: #86909c; transition: transform .15s; }
.grp-arrow.open { transform: rotate(90deg); }
.grp-host { flex: 1; font-weight: 600; color: #1d2129; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.grp-count { color: #86909c; font-size: 12px; }
.grp-check { display: flex; align-items: center; }
.grp-item { padding-left: 22px; }
.traffic-head .th-actions .el-radio-group { margin-right: 4px; }
.mitm-guide { margin: 6px 0; padding: 10px 12px; border-radius: 8px; font-size: 13px; line-height: 1.7; }
.mitm-guide.g-pinning { background: #fef0f0; border: 1px solid #fbc4c4; }
.mitm-guide.g-untrusted { background: #fdf6ec; border: 1px solid #f3d19e; }
.mitm-guide.g-tls { background: #fdf6ec; border: 1px solid #f3d19e; }
.mitm-guide.g-connect { background: #fef0f0; border: 1px solid #fbc4c4; }
.mitm-guide.g-non_http { background: #f4f4f5; border: 1px solid #e5e6eb; }
.mitm-guide .g-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 4px; }
.mitm-guide .g-title { font-weight: 600; color: #1d2129; }
.mitm-guide .g-body { color: #4e5969; word-break: break-word; }
.mitm-guide .g-act { margin-top: 8px; }
.img-preview { background: #f7f8fa; border-radius: 8px; padding: 10px; text-align: center; }
.img-preview img { max-width: 100%; max-height: 460px; object-fit: contain; border-radius: 4px; }
.mitm-sessions { border-top: 1px solid #e5e6eb; padding-top: 8px; }
.import-row { display: flex; align-items: center; gap: 8px; margin-bottom: 10px; }
.import-row .ir-label { width: 70px; color: #4e5969; font-size: 13px; flex-shrink: 0; }
</style>
