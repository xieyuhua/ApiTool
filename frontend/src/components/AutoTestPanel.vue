<script setup>
import { ref, computed, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  GetCapturedRequests, ImportCapturedAsTestCases, ImportApisAsTestCases,
  RunTestCases, RunStressTest, ExportTestReport, ExportStressReport, CopyToClipboard, GenerateReportSummary,
} from '../../wailsjs/go/main/App'
import {
  currentProject, projectTestCases, projectApis, projectReports, appendReport, removeReport,
  saveNow, reloadStore, stressStat,
} from '../store'

const mode = ref('func') // func 功能测试 | stress 压力测试
const cases = computed(() => projectTestCases())
const curEnvId = computed(() => currentProject().activeEnvId)

// ---------------- 选择 ----------------
const selectedIds = ref([])
const allSelected = computed(() => cases.value.length > 0 && selectedIds.value.length === cases.value.length)
const someSelected = computed(() => selectedIds.value.length > 0 && !allSelected.value)
function onRowCheck(row, v) {
  if (v) { if (!selectedIds.value.includes(row.id)) selectedIds.value.push(row.id) }
  else selectedIds.value = selectedIds.value.filter(i => i !== row.id)
}
function onHeaderCheck() {
  selectedIds.value = allSelected.value ? [] : cases.value.map(c => c.id)
}
const catType = { '正常流程': 'success', '参数边界': 'warning', '异常场景': 'danger', '权限安全': 'info' }
function methodClass(m) { return 'm-' + (m || 'get').toLowerCase() }

function selectAllCases() { selectedIds.value = cases.value.map(c => c.id) }
function clearSel() { selectedIds.value = [] }
function onDeleteCase(c) {
  ElMessageBox.confirm(`确定删除用例「${c.name}」？`, '提示', { type: 'warning' }).then(() => {
    const p = currentProject()
    p.testCases = (p.testCases || []).filter(x => x.id !== c.id)
    p.testPlans?.forEach(pl => { pl.caseIds = (pl.caseIds || []).filter(id => id !== c.id) })
    saveNow()
    selectedIds.value = selectedIds.value.filter(id => id !== c.id)
    ElMessage.success('已删除')
  }).catch(() => {})
}

// ---------------- 导入对话框 ----------------
const capDialog = ref(false)
const capList = ref([])
const capSel = ref([])
const capLoading = ref(false)
async function openCapImport() {
  capSel.value = []
  capLoading.value = true
  try { capList.value = await GetCapturedRequests() } catch (e) { ElMessage.error(String(e)) }
  finally { capLoading.value = false }
  capDialog.value = true
}
async function doCapImport() {
  if (!capSel.value.length) { ElMessage.warning('请至少选择一条捕获记录'); return }
  try {
    const n = await ImportCapturedAsTestCases(capSel.value)
    await reloadStore()
    capDialog.value = false
    ElMessage.success(`已导入 ${n} 条用例到「自动化测试」（可在下方勾选后运行）`)
  } catch (e) { ElMessage.error(String(e)) }
}

const apiDialog = ref(false)
const apiSel = ref([])
function openApiImport() { apiSel.value = []; apiDialog.value = true }
async function doApiImport() {
  if (!apiSel.value.length) { ElMessage.warning('请至少选择一个接口'); return }
  try {
    const n = await ImportApisAsTestCases(apiSel.value)
    await reloadStore()
    apiDialog.value = false
    ElMessage.success(`已导入 ${n} 条用例（复用接口请求定义）`)
  } catch (e) { ElMessage.error(String(e)) }
}
function onCapCheck(row, v) {
  if (v) { if (!capSel.value.includes(row.id)) capSel.value.push(row.id) }
  else { capSel.value = capSel.value.filter(i => i !== row.id) }
}
function onApiCheck(row, v) {
  if (v) { if (!apiSel.value.includes(row.id)) apiSel.value.push(row.id) }
  else { apiSel.value = apiSel.value.filter(i => i !== row.id) }
}

// ---------------- 功能测试 ----------------
const funcConc = ref(3)
const running = ref(false)
const reportVisible = ref(false)
const viewingReport = ref(null)
const summaryLoading = ref(false)

async function runFunc() {
  if (!selectedIds.value.length) { ElMessage.warning('请先勾选要运行的用例'); return }
  running.value = true
  try {
    const r = await RunTestCases(selectedIds.value, curEnvId.value, funcConc.value)
    appendReport(r)
    viewingReport.value = r
    reportVisible.value = true
  } catch (e) { ElMessage.error(String(e)) }
  finally { running.value = false }
}
const passRate = computed(() => {
  const r = viewingReport.value
  if (!r || !r.total) return '0%'
  return Math.round((r.passed / r.total) * 100) + '%'
})
async function genSummary() {
  if (!viewingReport.value) return
  summaryLoading.value = true
  try {
    const s = await GenerateReportSummary(JSON.stringify(viewingReport.value))
    viewingReport.value.summary = s
    const p = currentProject()
    const i = (p.testReports || []).findIndex(x => x.id === viewingReport.value.id)
    if (i >= 0) p.testReports[i].summary = s
    saveNow()
    ElMessage.success('已生成 AI 分析摘要')
  } catch (e) { ElMessage.error(String(e)) }
  finally { summaryLoading.value = false }
}
async function exportReport(fmt) {
  if (!viewingReport.value) return
  try {
    const path = await ExportTestReport(JSON.stringify(viewingReport.value), fmt)
    if (path) ElMessage.success('已导出：' + path)
  } catch (e) { ElMessage.error(String(e)) }
}
async function copyReport() {
  if (!viewingReport.value) return
  try { await CopyToClipboard(JSON.stringify(viewingReport.value, null, 2)); ElMessage.success('报告 JSON 已复制') }
  catch (e) { ElMessage.error(String(e)) }
}

// ---------------- 压测导出 ----------------
async function exportStress(fmt) {
  if (!stressReport.value) return
  try {
    const path = await ExportStressReport(JSON.stringify(stressReport.value), fmt)
    if (path) ElMessage.success('已导出：' + path)
  } catch (e) { ElMessage.error(String(e)) }
}
async function copyStress() {
  if (!stressReport.value) return
  try { await CopyToClipboard(JSON.stringify(stressReport.value, null, 2)); ElMessage.success('压测报告 JSON 已复制') }
  catch (e) { ElMessage.error(String(e)) }
}

// ---------------- 功能测试报告历史 ----------------
// 运行功能测试后报告会被持久化，关闭弹窗后仍可在下方记录中重新查看 / 导出 / 删除。
const reports = computed(() => projectReports())
function viewHistoryReport(r) {
  viewingReport.value = r
  reportVisible.value = true
}
async function exportHistoryReport(r, fmt) {
  try {
    const path = await ExportTestReport(JSON.stringify(r), fmt)
    if (path) ElMessage.success('已导出：' + path)
  } catch (e) { ElMessage.error(String(e)) }
}
function deleteReport(r) {
  removeReport(r.id)
  if (viewingReport.value && viewingReport.value.id === r.id) reportVisible.value = false
}

// ---------------- 压力测试 ----------------
const stressConc = ref(10)
const stressReqs = ref(100)
const stressRunning = ref(false)
const stressReport = ref(null)

async function runStress() {
  if (!selectedIds.value.length) { ElMessage.warning('请先勾选用例作为压测目标'); return }
  const p = currentProject()
  const caseMap = {}
  for (const c of (p.testCases || [])) caseMap[c.id] = c
  const targets = selectedIds.value
    .map(id => caseMap[id])
    .filter(Boolean)
    .map(c => ({
      name: c.name, method: c.method, url: c.url,
      headers: c.headers, query: c.query, bodyType: c.bodyType,
      body: c.body, formItems: c.formItems, contentType: c.contentType,
    }))
  if (!targets.length) { ElMessage.error('所选用例无效'); return }
  stressRunning.value = true
  stressStat.value = { running: true, done: 0, total: targets.length * stressReqs.value }
  try {
    const r = await RunStressTest(targets, {
      envId: curEnvId.value, concurrency: stressConc.value,
      requests: stressReqs.value, timeoutSec: 30,
    })
    stressReport.value = r
    ElMessage.success('压测完成')
  } catch (e) { ElMessage.error(String(e)) }
  finally {
    stressRunning.value = false
    stressStat.value = { running: false, done: 0, total: 0 }
  }
}

function statusDistText(m) {
  if (!m) return '—'
  return Object.entries(m).map(([k, v]) => `${k}:${v}`).join('  ') || '—'
}
function successRate(r) {
  if (!r || !r.total) return '0%'
  return Math.round((r.success / r.total) * 100) + '%'
}
function assertCount(row) { return (row.assertions || []).length }
function apiLabel(row) { return row.name || row.url }
function statusLabel(row) { return row.status || '-' }

// 切换视图时清理选择
watch(mode, () => {})
</script>

<template>
  <div class="main-area">
    <div class="panel-page" style="max-width:1180px; margin:0 auto; width:100%">
      <h2 style="margin:6px 0 4px">🤖 自动化测试</h2>
      <div style="color:#86909c; font-size:13px; margin-bottom:16px">
        把浏览器捕获的请求或已有接口导入为测试用例，进行<strong>功能自动化测试</strong>（带断言校验）或
        <strong>压力测试 / 测压</strong>（并发压测并统计吞吐与延迟分布）。用例与「接口测试」共享同一项目数据。
      </div>

      <!-- 导入 -->
      <div class="card">
        <div class="toolbar">
          <el-button type="primary" @click="openCapImport">＋ 从捕获导入</el-button>
          <el-button type="primary" plain @click="openApiImport">＋ 从接口导入</el-button>
          <span class="tip">捕获请求与已有调试接口都可一键转为可执行测试用例</span>
          <span class="spacer" />
          <span class="sel-tip">已选 {{ selectedIds.length }} / {{ cases.length }} 条</span>
          <el-button size="small" @click="selectAllCases">全选</el-button>
          <el-button size="small" @click="clearSel">取消</el-button>
        </div>
      </div>

      <!-- 模式切换 + 运行 -->
      <div class="card">
        <el-tabs v-model="mode" class="mode-tabs">
          <el-tab-pane label="功能测试（带断言）" name="func" />
          <el-tab-pane label="压力测试 / 测压" name="stress" />
        </el-tabs>

        <div v-if="mode === 'func'" class="run-bar">
          <span class="sel-tip">运行环境</span>
          <el-select v-model="curEnvId" size="small" style="width:160px" placeholder="无环境">
            <el-option label="无环境" value="" />
            <el-option v-for="e in currentProject().environments" :key="e.id" :label="e.name" :value="e.id" />
          </el-select>
          <span class="sel-tip">并发</span>
          <el-input-number v-model="funcConc" :min="1" :max="20" size="small" controls-position="right" style="width:96px" />
          <el-button type="success" :loading="running" :disabled="!selectedIds.length" @click="runFunc">运行功能测试</el-button>
          <span class="tip">无断言的用例按 HTTP 2xx 判定通过</span>
        </div>

        <div v-else class="run-bar">
          <span class="sel-tip">并发数</span>
          <el-input-number v-model="stressConc" :min="1" :max="200" size="small" controls-position="right" style="width:110px" />
          <span class="sel-tip">每个目标请求次数</span>
          <el-input-number v-model="stressReqs" :min="1" :max="5000" size="small" controls-position="right" style="width:130px" />
          <el-button type="danger" :loading="stressRunning" :disabled="!selectedIds.length" @click="runStress">开始压测</el-button>
          <el-progress v-if="stressStat.running" :percentage="stressStat.total ? Math.round(stressStat.done / stressStat.total * 100) : 0"
            :text-inside="true" style="width:200px; margin-left:10px" />
          <span v-if="stressStat.running" class="tip">已发 {{ stressStat.done }} / {{ stressStat.total }}</span>
        </div>
      </div>

      <!-- 用例列表 -->
      <div class="card">
        <div class="card-title">测试用例（{{ cases.length }}）</div>
        <el-table :data="cases" style="width:100%" empty-text="暂无测试用例，请先从「捕获导入」或「接口导入」添加">
          <el-table-column width="46">
            <template #header>
              <el-checkbox :model-value="allSelected" :indeterminate="someSelected" @change="onHeaderCheck" />
            </template>
            <template #default="{ row }">
              <el-checkbox :model-value="selectedIds.includes(row.id)" @change="v => onRowCheck(row, v)" />
            </template>
          </el-table-column>
          <el-table-column prop="name" label="用例名称" min-width="200" show-overflow-tooltip />
          <el-table-column label="分类" width="110">
            <template #default="{ row }"><el-tag :type="catType[row.category] || 'info'" size="small">{{ row.category }}</el-tag></template>
          </el-table-column>
          <el-table-column label="请求" min-width="300">
            <template #default="{ row }">
              <span class="mtag" :class="methodClass(row.method)">{{ row.method }}</span>
              <span class="murl">{{ row.url }}</span>
            </template>
          </el-table-column>
          <el-table-column label="断言" width="70">
            <template #default="{ row }">
              {{ assertCount(row) }}
            </template>
          </el-table-column>
          <el-table-column label="操作" width="80">
            <template #default="{ row }">
              <el-button link type="danger" @click="onDeleteCase(row)">删除</el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>

      <!-- 功能测试报告记录 -->
      <div class="card">
        <div class="card-title">测试报告记录（{{ reports.length }}）</div>
        <el-table v-if="reports.length" :data="reports" style="width:100%" max-height="380">
          <el-table-column prop="planName" label="计划" min-width="120" show-overflow-tooltip />
          <el-table-column label="时间" width="172">
            <template #default="{ row }">{{ row.createdAt }}</template>
          </el-table-column>
          <el-table-column label="通过/失败" width="120">
            <template #default="{ row }"><span class="ok">{{ row.passed }}</span> / <span class="fail">{{ row.failed }}</span></template>
          </el-table-column>
          <el-table-column label="总耗时" width="100">
            <template #default="{ row }">{{ row.durationMs }} ms</template>
          </el-table-column>
          <el-table-column label="操作" width="270">
            <template #default="{ row }">
              <el-button link type="primary" size="small" @click="viewHistoryReport(row)">查看</el-button>
              <el-button link type="primary" size="small" @click="exportHistoryReport(row, 'markdown')">导出MD</el-button>
              <el-button link type="primary" size="small" @click="exportHistoryReport(row, 'html')">导出HTML</el-button>
              <el-button link type="danger" size="small" @click="deleteReport(row)">删除</el-button>
            </template>
          </el-table-column>
        </el-table>
        <div v-else class="tip">暂无报告。运行功能测试后，报告会记录在此，可随时重新查看或导出。</div>
      </div>

      <!-- 压测结果 -->
      <div v-if="mode === 'stress' && stressReport" class="card">
        <div class="card-title">压测结果</div>
        <div class="report-bar">
          <el-button size="small" @click="copyStress">复制 JSON</el-button>
          <el-button size="small" type="primary" plain @click="exportStress('markdown')">导出 Markdown</el-button>
          <el-button size="small" type="primary" plain @click="exportStress('html')">导出 HTML</el-button>
        </div>
        <div class="stress-summary">
          <div class="ss"><div class="ss-n">{{ stressReport.total }}</div><div>总请求</div></div>
          <div class="ss ok"><div class="ss-n">{{ stressReport.success }}</div><div>成功</div></div>
          <div class="ss fail"><div class="ss-n">{{ stressReport.failed }}</div><div>失败</div></div>
          <div class="ss"><div class="ss-n">{{ stressReport.rps ? stressReport.rps.toFixed(1) : 0 }}</div><div>吞吐 (RPS)</div></div>
          <div class="ss"><div class="ss-n">{{ stressReport.durationMs }}ms</div><div>总耗时</div></div>
        </div>
        <el-table :data="stressReport.results" style="width:100%; margin-top:12px">
          <el-table-column prop="name" label="目标" min-width="200" show-overflow-tooltip />
          <el-table-column label="成功率" width="100">
            <template #default="{ row }"><span :class="row.failed === 0 ? 'ok' : 'fail'">{{ successRate(row) }}</span></template>
          </el-table-column>
          <el-table-column label="最小/平均" width="130">
            <template #default="{ row }">
              {{ row.minMs }} / {{ row.avgMs }}ms
            </template>
          </el-table-column>
          <el-table-column label="P95 / P99" width="140">
            <template #default="{ row }">
              {{ row.p95 }} / {{ row.p99 }}ms
            </template>
          </el-table-column>
          <el-table-column label="状态码分布" min-width="220">
            <template #default="{ row }"><span class="dist">{{ statusDistText(row.statusDist) }}</span></template>
          </el-table-column>
        </el-table>
      </div>
    </div>

    <!-- 捕获导入对话框 -->
    <el-dialog v-model="capDialog" title="从捕获导入为测试用例" width="720px" top="5vh">
      <div class="gen-select-bar">
        <span class="sel-count">已选 {{ capSel.length }} / {{ capList.length }} 条</span>
        <span class="spacer" />
        <el-button size="small" link type="primary" @click="capSel = capList.map(x => x.id)">全选</el-button>
        <el-button size="small" link @click="capSel = []">清空</el-button>
      </div>
      <el-table v-loading="capLoading" :data="capList" style="width:100%; margin-top:10px" max-height="420">
        <el-table-column width="46">
          <template #default="{ row }">
            <el-checkbox :model-value="capSel.includes(row.id)" @change="onCapCheck(row, $event)" />
          </template>
        </el-table-column>
        <el-table-column label="方法" width="80">
          <template #default="{ row }"><span class="mtag" :class="methodClass(row.method)">{{ row.method }}</span></template>
        </el-table-column>
        <el-table-column label="URL" min-width="300" show-overflow-tooltip>
          <template #default="{ row }">
            {{ row.url }}
          </template>
        </el-table-column>
      </el-table>
      <template #footer>
        <el-button @click="capDialog = false">取消</el-button>
        <el-button type="primary" @click="doCapImport">导入为测试用例</el-button>
      </template>
    </el-dialog>

    <!-- 接口导入对话框 -->
    <el-dialog v-model="apiDialog" title="从已有接口导入为测试用例" width="720px" top="5vh">
      <div class="gen-select-bar">
        <span class="sel-count">已选 {{ apiSel.length }} / {{ projectApis().length }} 个</span>
        <span class="spacer" />
        <el-button size="small" link type="primary" @click="apiSel = projectApis().map(a => a.id)">全选</el-button>
        <el-button size="small" link @click="apiSel = []">清空</el-button>
      </div>
      <el-table :data="projectApis()" style="width:100%; margin-top:10px" max-height="420">
        <el-table-column width="46">
          <template #default="{ row }">
            <el-checkbox :model-value="apiSel.includes(row.id)" @change="onApiCheck(row, $event)" />
          </template>
        </el-table-column>
        <el-table-column label="方法" width="80">
          <template #default="{ row }"><span class="mtag" :class="methodClass(row.method)">{{ row.method }}</span></template>
        </el-table-column>
          <el-table-column label="接口" min-width="120" show-overflow-tooltip>
            <template #default="{ row }">
              {{ apiLabel(row) }}
            </template>
          </el-table-column>
        <el-table-column label="URL" min-width="260" show-overflow-tooltip>
          <template #default="{ row }">
            {{ row.url }}
          </template>
        </el-table-column>
      </el-table>
      <template #footer>
        <el-button @click="apiDialog = false">取消</el-button>
        <el-button type="primary" @click="doApiImport">导入为测试用例</el-button>
      </template>
    </el-dialog>

    <!-- 功能测试报告 -->
    <el-dialog v-model="reportVisible" title="功能测试报告" width="900px" top="3vh">
      <template v-if="viewingReport">
        <div class="report-stats">
          <div class="rs"><div class="rs-n">{{ viewingReport.total }}</div><div>用例总数</div></div>
          <div class="rs ok"><div class="rs-n">{{ viewingReport.passed }}</div><div>通过</div></div>
          <div class="rs fail"><div class="rs-n">{{ viewingReport.failed }}</div><div>失败</div></div>
          <div class="rs"><div class="rs-n">{{ passRate }}</div><div>通过率</div></div>
          <div class="rs"><div class="rs-n">{{ viewingReport.durationMs }}ms</div><div>总耗时</div></div>
        </div>
        <div class="report-bar">
          <el-button size="small" type="success" :loading="summaryLoading" @click="genSummary">✨ AI 分析摘要</el-button>
          <el-button size="small" @click="copyReport">复制 JSON</el-button>
          <el-button size="small" @click="exportReport('markdown')">导出 Markdown</el-button>
          <el-button size="small" @click="exportReport('html')">导出 HTML</el-button>
        </div>
        <div v-if="viewingReport.summary" class="report-summary">
          <div class="rs-title">AI 分析摘要</div>
          <pre>{{ viewingReport.summary }}</pre>
        </div>
        <el-table :data="viewingReport.results" style="width:100%; margin-top:10px" row-key="caseId">
          <el-table-column type="expand">
            <template #default="{ row }">
              <div v-if="row.error" class="ar-error">请求错误：{{ row.error }}</div>
              <div v-for="(ar, i) in row.assertionResults" :key="i" class="ar-row">
                <span :class="ar.passed ? 'ok' : 'fail'">{{ ar.passed ? '✓' : '✗' }}</span>
                {{ ar.description }} —— {{ ar.detail }}
              </div>
              <div v-if="!row.assertionResults.length && !row.error" class="ar-row">无断言（按 HTTP 2xx 判定）</div>
            </template>
          </el-table-column>
          <el-table-column prop="caseName" label="用例" min-width="200" />
          <el-table-column prop="category" label="分类" width="100" />
          <el-table-column label="状态码" width="90">
            <template #default="{ row }">{{ statusLabel(row) }}</template>
          </el-table-column>
          <el-table-column label="耗时" width="90">
            <template #default="{ row }">{{ row.durationMs }} ms</template>
          </el-table-column>
          <el-table-column label="结果" width="90">
            <template #default="{ row }"><el-tag :type="row.passed ? 'success' : 'danger'" size="small">{{ row.passed ? '通过' : '失败' }}</el-tag></template>
          </el-table-column>
        </el-table>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.card { background:#fff; border:1px solid #e5e6eb; border-radius:10px; padding:16px 18px; margin-bottom:16px; }
.card-title { font-weight:600; font-size:14px; margin-bottom:12px; }
.toolbar { display:flex; align-items:center; gap:10px; flex-wrap:wrap; }
.tip { color:#c2c7cf; font-size:12px; }
.spacer { flex:1; }
.sel-tip { font-size:13px; color:#86909c; }
.mode-tabs { margin-bottom:6px; }
.run-bar { display:flex; align-items:center; gap:10px; flex-wrap:wrap; }
.mtag { display:inline-block; font-size:11px; font-weight:700; color:#fff; padding:1px 7px; border-radius:3px; margin-right:8px; }
.m-get { background:#0fc6c2; } .m-post { background:#165dff; } .m-put { background:#ff7d00; }
.m-delete { background:#f53f3f; } .m-patch { background:#722ed1; } .m-head, .m-options { background:#86909c; }
.murl { color:#4e5969; font-size:13px; word-break:break-all; }
.ok { color:#00b42a; font-weight:600; } .fail { color:#f53f3f; font-weight:600; }
.dist { font-size:12px; color:#4e5969; }
.gen-select-bar { display:flex; align-items:center; gap:10px; }
.gen-select-bar .sel-count { font-size:12px; color:#86909c; }
.stress-summary { display:flex; gap:12px; flex-wrap:wrap; }
.ss { background:#f7f8fa; border:1px solid #e5e6eb; border-radius:10px; padding:12px 18px; min-width:96px; text-align:center; }
.ss .ss-n { font-size:22px; font-weight:700; } .ss.ok .ss-n { color:#00b42a; } .ss.fail .ss-n { color:#f53f3f; }
.report-stats { display:flex; gap:12px; flex-wrap:wrap; margin-bottom:14px; }
.rs { background:#f7f8fa; border:1px solid #e5e6eb; border-radius:10px; padding:12px 18px; min-width:96px; text-align:center; }
.rs .rs-n { font-size:22px; font-weight:700; } .rs.ok .rs-n { color:#00b42a; } .rs.fail .rs-n { color:#f53f3f; }
.report-bar { display:flex; gap:8px; margin-bottom:12px; flex-wrap:wrap; }
.report-summary { background:#f7f8fa; border:1px solid #e5e6eb; border-radius:8px; padding:12px 14px; margin-bottom:12px; }
.rs-title { font-weight:600; margin-bottom:8px; font-size:14px; }
.report-summary pre { white-space:pre-wrap; word-break:break-word; font-family:inherit; font-size:13px; line-height:1.8; margin:0; }
.ar-row { font-size:12px; padding:3px 0; color:#4e5969; }
.ar-error { font-size:12px; padding:3px 0; color:#f53f3f; }
</style>
