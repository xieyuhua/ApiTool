<script setup>
import { ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { ExportDoc, CopyDocMarkdown, ImportDoc, ShareTestReport, ExportTestReport, CopyToClipboard } from '../../wailsjs/go/main/App'
import { store, buildDirTree, saveNow, reloadStore, projectApis, projectDirs, projectReports } from '../store'
import ShareDialog from './ShareDialog.vue'

const scopeDirId = ref('')
const busy = ref(false)
const shareVisible = ref(false)
const dirTree = computed(() => buildDirTree())

// 计算所选范围内的接口
const scopedApis = computed(() => {
  const dirId = scopeDirId.value
  const apis = projectApis()
  if (!dirId) return apis
  const dirs = projectDirs()
  const ids = new Set([dirId])
  let changed = true
  while (changed) {
    changed = false
    for (const d of dirs) {
      if (ids.has(d.parentId) && !ids.has(d.id)) { ids.add(d.id); changed = true }
    }
  }
  return apis.filter(a => ids.has(a.dirId))
})

const scopeName = computed(() => {
  if (!scopeDirId.value) return '全部接口'
  return projectDirs().find(d => d.id === scopeDirId.value)?.name || '全部接口'
})

async function doExport(format) {
  if (!scopedApis.value.length) { ElMessage.warning('所选范围内没有接口'); return }
  busy.value = true
  try {
    await saveNow()
    const path = await ExportDoc(scopeDirId.value, '', format)
    if (path) ElMessage.success('已导出：' + path)
  } catch (e) {
    ElMessage.error(String(e))
  } finally { busy.value = false }
}

async function share() {
  if (!scopedApis.value.length) { ElMessage.warning('所选范围内没有接口'); return }
  shareVisible.value = true
}

async function copyMd() {
  if (!scopedApis.value.length) { ElMessage.warning('所选范围内没有接口'); return }
  try {
    await saveNow()
    await CopyDocMarkdown(scopeDirId.value, '')
    ElMessage.success('Markdown 已复制到剪贴板')
  } catch (e) { ElMessage.error(String(e)) }
}

async function importDoc() {
  busy.value = true
  try {
    const msg = await ImportDoc()
    if (msg) {
      await reloadStore()
      ElMessage.success(msg)
    }
  } catch (e) {
    if (String(e) && String(e) !== 'User cancelled') ElMessage.error(String(e))
  } finally { busy.value = false }
}

function openApi(id) {
  store.currentApiId = id
  store.view = 'workspace'
  store.activeTab = 'doc'
}

// ---------------- 测试报告（接入文档中心） ----------------
const reports = computed(() => projectReports())

const reportShareVisible = ref(false)
const reportShareTarget = ref('')
const sharePassword = ref('')
const shareExpire = ref(0)
const shareLink = ref('')
const shareLoading = ref(false)

function openShareReport(r) {
  reportShareTarget.value = JSON.stringify(r)
  sharePassword.value = ''
  shareExpire.value = 0
  shareLink.value = ''
  reportShareVisible.value = true
}
async function doShareReport() {
  shareLoading.value = true
  try {
    const link = await ShareTestReport(reportShareTarget.value, sharePassword.value, shareExpire.value)
    shareLink.value = link
    ElMessage.success('分享链接已生成')
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    shareLoading.value = false
  }
}
async function copyLink() {
  try {
    await CopyToClipboard(shareLink.value)
    ElMessage.success('链接已复制到剪贴板')
  } catch (e) { ElMessage.error(String(e)) }
}
async function exportReportHtml(r) {
  try {
    const path = await ExportTestReport(JSON.stringify(r), 'html')
    if (path) ElMessage.success('已导出：' + path)
  } catch (e) { ElMessage.error(String(e)) }
}

const formats = [
  { key: 'markdown', name: 'Markdown', ext: '.md', desc: '通用文档格式，适合 Git 仓库、Wiki' },
  { key: 'html', name: 'HTML', ext: '.html', desc: '带目录导航的网页文档，可直接分享' },
  { key: 'word', name: 'Word', ext: '.doc', desc: '可用 Office / WPS 打开编辑' },
  { key: 'openapi', name: 'OpenAPI 3.0', ext: '.json', desc: '可导入 Swagger、Postman、Apifox' },
]
</script>

<template>
  <div class="main-area">
    <div class="panel-page" style="max-width:960px; margin:0 auto; width:100%">
      <h2 style="margin:6px 0 16px">📄 文档中心</h2>

      <div class="card">
        <div class="card-title">导入现有接口文档</div>
        <div style="display:flex; gap:12px; align-items:center; flex-wrap:wrap">
          <el-button type="primary" :loading="busy" @click="importDoc">📥 导入文档</el-button>
          <span style="color:#86909c; font-size:13px">
            支持 OpenAPI 3.0 / Swagger 2.0 / Postman Collection（JSON 与 YAML），导入内容将归入独立的「导入-文件名」目录
          </span>
        </div>
      </div>

      <div class="card">
        <div class="card-title">选择分享 / 导出范围（按目录层级）</div>
        <div style="display:flex; gap:12px; align-items:center; flex-wrap:wrap">
          <el-tree-select v-model="scopeDirId" :data="dirTree" check-strictly :render-after-expand="false"
            style="width:320px" placeholder="选择目录范围" default-expand-all />
          <span style="color:#86909c; font-size:13px">
            当前范围：<b style="color:#165dff">{{ scopeName }}</b> · 共 {{ scopedApis.length }} 个接口
          </span>
        </div>
        <div style="margin-top:14px; display:flex; gap:10px; flex-wrap:wrap">
          <el-button type="primary" @click="share">🔗 分享为网页链接</el-button>
          <el-button @click="copyMd">复制 Markdown</el-button>
        </div>
      </div>

      <div class="card">
        <div class="card-title">导出文档</div>
        <div class="fmt-grid">
          <div v-for="f in formats" :key="f.key" class="fmt-item" @click="doExport(f.key)">
            <div class="fmt-name">{{ f.name }} <span class="fmt-ext">{{ f.ext }}</span></div>
            <div class="fmt-desc">{{ f.desc }}</div>
          </div>
        </div>
      </div>

      <div class="card">
        <div class="card-title">范围内接口（{{ scopedApis.length }}）</div>
        <div v-if="!scopedApis.length" style="color:#c9cdd4; text-align:center; padding:20px">暂无接口</div>
        <div v-for="a in scopedApis" :key="a.id" class="api-row" @click="openApi(a.id)">
          <span class="method-tag" :class="'m-' + a.method">{{ a.method }}</span>
          <span class="api-row-name">{{ a.name }}</span>
          <span class="api-row-url">{{ a.url }}</span>
        </div>
      </div>

      <div class="card">
        <div class="card-title">测试报告（{{ reports.length }}）</div>
        <div style="color:#86909c; font-size:13px; margin-bottom:10px">
          在「接口测试」中运行用例或计划后生成的测试分析报告，可在此直接分享为网页链接或导出 HTML，与接口文档统一分发。
        </div>
        <div v-if="!reports.length" style="color:#c9cdd4; text-align:center; padding:18px">暂无测试报告，请先到「接口测试」运行</div>
        <div v-for="r in reports" :key="r.id" class="report-row">
          <div class="rr-main">
            <span class="rr-name">{{ r.planName }}</span>
            <span class="rr-meta">{{ r.passed }}/{{ r.total }} 通过 · {{ r.durationMs }}ms · {{ r.createdAt }}</span>
          </div>
          <div class="rr-actions">
            <el-button size="small" type="primary" plain @click="openShareReport(r)">分享链接</el-button>
            <el-button size="small" @click="exportReportHtml(r)">导出 HTML</el-button>
          </div>
        </div>
      </div>
    </div>

    <ShareDialog v-model:visible="shareVisible" :dir-id="scopeDirId" api-id="" />

    <el-dialog v-model="reportShareVisible" title="分享测试报告" width="560px">
      <div v-if="!shareLink" class="share-form">
        <div class="sf-item">
          <label>访问密码（可选）</label>
          <el-input v-model="sharePassword" placeholder="留空表示无需密码" show-password />
        </div>
        <div class="sf-item">
          <label>有效期</label>
          <el-select v-model="shareExpire" style="width:100%">
            <el-option label="永久有效" :value="0" />
            <el-option label="30 分钟" :value="30" />
            <el-option label="1 天" :value="1440" />
            <el-option label="7 天" :value="10080" />
          </el-select>
        </div>
      </div>
      <div v-else class="share-result">
        <el-alert type="success" :closable="false" title="分享链接已生成" />
        <div class="sr-link">{{ shareLink }}</div>
        <el-button size="small" type="primary" @click="copyLink">复制链接</el-button>
      </div>
      <template #footer>
        <el-button v-if="!shareLink" @click="reportShareVisible = false">取消</el-button>
        <el-button v-if="!shareLink" type="primary" :loading="shareLoading" @click="doShareReport">生成链接</el-button>
        <el-button v-else @click="reportShareVisible = false">关闭</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.fmt-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 12px; }
.fmt-item {
  border: 1px solid #e5e6eb; border-radius: 8px; padding: 14px; cursor: pointer;
  transition: all .15s;
}
.fmt-item:hover { border-color: #165dff; box-shadow: 0 2px 10px rgba(22, 93, 255, .12); }
.fmt-name { font-weight: 600; }
.fmt-ext { color: #86909c; font-weight: 400; font-size: 12px; }
.fmt-desc { color: #86909c; font-size: 12px; margin-top: 6px; }
.api-row {
  display: flex; align-items: center; gap: 10px; padding: 9px 10px;
  border-radius: 6px; cursor: pointer;
}
.api-row:hover { background: #f2f3f5; }
.api-row-name { font-size: 13px; }
.api-row-url { color: #86909c; font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.report-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 10px 4px; border-bottom: 1px dashed #f2f3f5; }
.report-row:last-child { border-bottom: none; }
.rr-main { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
.rr-name { font-size: 13px; font-weight: 600; }
.rr-meta { font-size: 12px; color: #86909c; }
.rr-actions { display: flex; gap: 8px; flex-shrink: 0; }
.share-form { display: flex; flex-direction: column; gap: 14px; }
.sf-item { display: flex; flex-direction: column; gap: 5px; }
.sf-item label { font-size: 12px; color: #86909c; }
.share-result { display: flex; flex-direction: column; gap: 12px; }
.sr-link { background: #f7f8fa; border: 1px solid #e5e6eb; border-radius: 6px; padding: 10px 12px; font-size: 12px; word-break: break-all; color: #4e5969; }
</style>
