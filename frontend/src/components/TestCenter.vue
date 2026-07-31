<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  GenerateTestCasesAsync, RunTestPlan, RunTestCases,
  GenerateReportSummary, ExportTestReport, CopyToClipboard,
  GetCapturedRequests, ImportCapturedAsTestCases, ImportApisAsTestCases,
} from '../../wailsjs/go/main/App'
import {
  saveNow, currentProject, uid, projectApis,
  projectTestCases, projectTestPlans, projectReports,
  removeTestCase, saveTestCase, addTestPlan, removeTestPlan,
  appendReport, removeReport,
  genJobId, genStat, genDoneInfo, genErrorInfo, startGenJob,
  clearTestData,
} from '../store'
import KVEditor from './KVEditor.vue'

const tab = ref('cases')

// ---------------- 用例列表 ----------------
const cases = computed(() => projectTestCases())
const plans = computed(() => projectTestPlans())
const reports = computed(() => projectReports())
const curEnvId = computed(() => currentProject().activeEnvId)

const selectedCaseIds = ref([])
function toggleCase(id) {
  const i = selectedCaseIds.value.indexOf(id)
  if (i >= 0) selectedCaseIds.value.splice(i, 1)
  else selectedCaseIds.value.push(id)
}

const catType = {
  '正常流程': 'success', '参数边界': 'warning', '异常场景': 'danger', '权限安全': 'info',
}
function methodClass(m) { return 'm-' + (m || 'get').toLowerCase() }

// 断言类型选项（含新增的 contentType / cookie / regex / size）
const assertionTypes = ['status', 'json', 'bodyContains', 'header', 'duration', 'contentType', 'cookie', 'regex', 'size']
const assertionOps = ['eq', 'ne', 'gt', 'gte', 'lt', 'lte', 'contains', 'exists', 'isTrue', 'isFalse']
const assertionLabels = {
  status: '状态码', json: 'JSON 字段', bodyContains: '响应体包含', header: '响应头',
  duration: '响应耗时(ms)', contentType: 'Content-Type', cookie: 'Cookie', regex: '正则匹配', size: '响应体积(bytes)',
}
// 不同断言类型需要填写的字段提示
function assertionHint(t) {
  switch (t) {
    case 'json': return '目标填 JSONPath，如 $.code'
    case 'header': return '目标填响应头名'
    case 'cookie': return '期望值填 cookie 名或 名=值 片段'
    case 'regex': return '期望值填正则；eq 需匹配，ne 需不匹配'
    case 'size': return '期望值填字节数，用 gt/lt/gte/lte'
    case 'duration': return '期望值填毫秒数，用 gt/lt/gte/lte'
    case 'contentType': return '期望值填如 application/json'
    default: return '该类型无需填写目标'
  }
}
function assertionTargetDisabled(t) {
  return t === 'status' || t === 'bodyContains' || t === 'duration' || t === 'contentType' || t === 'cookie' || t === 'regex' || t === 'size'
}

// ---------------- AI 生成 ----------------
const genVisible = ref(false)
const genApiIds = ref([])
const genLoading = ref(false)

function openGenerate() {
  if (genJobId.value) { genVisible.value = true; return } // 进行中：直接查看进度
  genApiIds.value = []
  genVisible.value = true
}

// 生成任务状态来自全局 store（genJobId / genStat），跨视图不丢失。
const genPercent = computed(() => {
  const s = genStat.value
  if (!s.total) return 0
  return Math.min(100, Math.round((s.done / s.total) * 100))
})

// 全选 / 清空当前项目的所有接口
function selectAllApis() {
  genApiIds.value = projectApis().map(a => a.id)
}
function clearApiSelection() {
  genApiIds.value = []
}

async function doGenerate() {
  if (!genApiIds.value.length) { ElMessage.warning('请至少选择一个接口'); return }
  if (genJobId.value) { ElMessage.warning('已有生成任务在进行中，请稍候'); return }
  genLoading.value = true
  try {
    const jobId = await GenerateTestCasesAsync(genApiIds.value)
    startGenJob(jobId, genApiIds.value.length) // 写入全局状态
    genVisible.value = false // 关掉选择框，任务在后台跑；进度可在任意时刻通过弹窗查看
    ElMessage.info('已提交生成任务，正在后台生成用例（可切到其它页面，稍后回来查看进度）')
  } catch (e) {
    ElMessage.error(String(e))
    genLoading.value = false
  }
}

// 进入「接口测试」视图时：若任务仍在进行，自动弹出进度；若期间已结束/出错，补提示。
function flushGenResult() {
  if (genJobId.value) {
    genVisible.value = true // 进行中：恢复进度弹窗
    return
  }
  genLoading.value = false
  if (genDoneInfo.value) {
    if (genDoneInfo.value.count > 0) ElMessage.success(`AI 已生成 ${genDoneInfo.value.count} 条测试用例`)
    else ElMessage.warning('AI 未生成用例')
    genDoneInfo.value = null
  }
  if (genErrorInfo.value) {
    ElMessage.error(genErrorInfo.value.error)
    genErrorInfo.value = null
  }
}
// 任务从「进行中」变为「已结束」时（含切走期间完成），补弹提示。
watch(genJobId, (now, old) => {
  if (old && !now) flushGenResult()
})
// 进入「用例导入」页时自动刷新捕获列表
watch(tab, (now) => {
  if (now === 'import') refreshCaptured()
})
onMounted(flushGenResult)

// ---------------- 用例编辑 ----------------
const caseVisible = ref(false)
const editingCase = ref(null)

function newAssertion() {
  return { type: 'status', target: '', operator: 'eq', expected: '200', enabled: true }
}
function openCaseEditor(c) {
  editingCase.value = JSON.parse(JSON.stringify(c))
  if (!editingCase.value.assertions) editingCase.value.assertions = []
  caseVisible.value = true
}
function addAssertion() { editingCase.value.assertions.push(newAssertion()) }
function removeAssertion(i) { editingCase.value.assertions.splice(i, 1) }
function saveCase() {
  if (!editingCase.value.name) editingCase.value.name = '未命名用例'
  saveTestCase(editingCase.value)
  caseVisible.value = false
  ElMessage.success('用例已保存')
}
function onDeleteCase(c) {
  ElMessageBox.confirm(`确定删除用例「${c.name}」？`, '提示', { type: 'warning' }).then(() => {
    removeTestCase(c.id)
    ElMessage.success('已删除')
  }).catch(() => {})
}

// ---------------- 计划 ----------------
const planTarget = ref('')
async function addSelectedToPlan() {
  if (!selectedCaseIds.value.length) { ElMessage.warning('请先勾选用例'); return }
  let planId = planTarget.value
  if (!planId) {
    try {
      const { value } = await ElMessageBox.prompt('请输入新计划名称', '新建测试计划', { inputValue: '默认计划' })
      const plan = {
        id: uid(), name: value || '默认计划', caseIds: [], envId: curEnvId.value,
        concurrency: 1, createdAt: new Date().toISOString(), updatedAt: new Date().toISOString(),
      }
      addTestPlan(plan)
      planId = plan.id
      ElMessage.success('已创建计划')
    } catch { return }
  }
  const p = currentProject()
  const plan = (p.testPlans || []).find(x => x.id === planId)
  if (!plan) { ElMessage.error('计划不存在'); return }
  const set = new Set(plan.caseIds || [])
  for (const id of selectedCaseIds.value) set.add(id)
  plan.caseIds = [...set]
  plan.updatedAt = new Date().toISOString()
  saveNow()
  selectedCaseIds.value = []
  planTarget.value = ''
  ElMessage.success('已加入计划')
}

const planVisible = ref(false)
const editingPlan = ref(null)
function openPlanEditor(plan) {
  editingPlan.value = JSON.parse(JSON.stringify(plan || {
    id: uid(), name: '新计划', caseIds: [], envId: curEnvId.value,
    concurrency: 1, createdAt: new Date().toISOString(), updatedAt: new Date().toISOString(),
  }))
  planVisible.value = true
}
function savePlan() {
  const p = currentProject()
  p.testPlans ||= []
  const i = p.testPlans.findIndex(x => x.id === editingPlan.value.id)
  editingPlan.value.updatedAt = new Date().toISOString()
  if (i >= 0) p.testPlans[i] = editingPlan.value
  else p.testPlans.push(editingPlan.value)
  saveNow()
  planVisible.value = false
  ElMessage.success('计划已保存')
}
function onDeletePlan(plan) {
  ElMessageBox.confirm(`确定删除计划「${plan.name}」？`, '提示', { type: 'warning' }).then(() => {
    removeTestPlan(plan.id)
    ElMessage.success('已删除')
  }).catch(() => {})
}
// 一键清空：清空当前项目下的 用例 / 计划 / 报告
async function clearScope(scope, label) {
  const p = currentProject()
  const count = scope === 'cases' ? p.testCases.length
    : scope === 'plans' ? p.testPlans.length
    : scope === 'reports' ? p.testReports.length : 0
  if (count === 0) {
    ElMessage.info(`当前没有可清空的${label}`)
    return
  }
  try {
    await ElMessageBox.confirm(
      `确定清空当前项目下的全部${label}（共 ${count} 条）？此操作不可恢复。`,
      '一键清空', { type: 'warning', confirmButtonText: '清空', cancelButtonText: '取消' }
    )
  } catch (e) {
    return
  }
  try {
    const removed = await clearTestData(scope)
    ElMessage.success(`已清空 ${removed} 条${label}`)
  } catch (e) {
    ElMessage.error('清空失败：' + (e?.message || e))
  }
}
function clearCases() { return clearScope('cases', '测试用例') }
function clearPlans() { return clearScope('plans', '测试计划') }
function clearReports() { return clearScope('reports', '测试报告') }
function togglePlanCase(id) {
  const arr = editingPlan.value.caseIds || (editingPlan.value.caseIds = [])
  const i = arr.indexOf(id)
  if (i >= 0) arr.splice(i, 1)
  else arr.push(id)
}
// 全选：按用例列表顺序加入所有用例
function selectAllPlanCases() {
  editingPlan.value.caseIds = cases.value.map(c => c.id)
}
// 清空：移除计划中的所有用例
function clearPlanCases() {
  editingPlan.value.caseIds = []
}

// ---------------- 执行 ----------------
const running = ref(false)
const reportVisible = ref(false)
const viewingReport = ref(null)
const summaryLoading = ref(false)
const runConcurrency = ref(3) // 并发执行数（手动运行选中用例时使用）

async function runPlan(plan) {
  running.value = true
  try {
    const r = await RunTestPlan(plan.id)
    appendReport(r)
    viewingReport.value = r
    reportVisible.value = true
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    running.value = false
  }
}
async function runSelected() {
  if (!selectedCaseIds.value.length) { ElMessage.warning('请先勾选用例'); return }
  running.value = true
  try {
    const r = await RunTestCases(selectedCaseIds.value, curEnvId.value, runConcurrency.value)
    appendReport(r)
    viewingReport.value = r
    reportVisible.value = true
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    running.value = false
  }
}

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
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    summaryLoading.value = false
  }
}
async function exportReport(fmt) {
  if (!viewingReport.value) return
  try {
    const path = await ExportTestReport(JSON.stringify(viewingReport.value), fmt)
    if (path) ElMessage.success('已导出：' + path)
  } catch (e) {
    ElMessage.error(String(e))
  }
}
async function copyReport() {
  if (!viewingReport.value) return
  try {
    await CopyToClipboard(JSON.stringify(viewingReport.value, null, 2))
    ElMessage.success('报告 JSON 已复制')
  } catch (e) {
    ElMessage.error(String(e))
  }
}
function viewReport(r) {
  viewingReport.value = r
  reportVisible.value = true
}
function onDeleteReport(r) {
  ElMessageBox.confirm('确定删除该报告？', '提示', { type: 'warning' }).then(() => {
    removeReport(r.id)
    ElMessage.success('已删除')
  }).catch(() => {})
}

const passRate = computed(() => {
  const r = viewingReport.value
  if (!r || !r.total) return '0%'
  return Math.round((r.passed / r.total) * 100) + '%'
})

// ---------------- 运行全部用例（功能测试） ----------------
async function runAll() {
  if (!cases.value.length) { ElMessage.warning('暂无可运行的用例'); return }
  running.value = true
  try {
    const r = await RunTestCases(cases.value.map(c => c.id), curEnvId.value, runConcurrency.value)
    appendReport(r)
    viewingReport.value = r
    reportVisible.value = true
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    running.value = false
  }
}

// ---------------- 用例导入（捕获 / 接口） ----------------
const capturedList = ref([])
const capSelected = ref([])
const apiSelected = ref([])
const importing = ref(false)
async function refreshCaptured() {
  try {
    capturedList.value = await GetCapturedRequests() || []
  } catch (e) {
    ElMessage.error(String(e))
  }
}
function toggleCap(id) {
  const i = capSelected.value.indexOf(id)
  if (i >= 0) capSelected.value.splice(i, 1)
  else capSelected.value.push(id)
}
function toggleApi(id) {
  const i = apiSelected.value.indexOf(id)
  if (i >= 0) apiSelected.value.splice(i, 1)
  else apiSelected.value.push(id)
}
async function importSelectedCap() {
  if (!capSelected.value.length) { ElMessage.warning('请勾选要导入的捕获请求'); return }
  importing.value = true
  try {
    const n = await ImportCapturedAsTestCases(capSelected.value)
    ElMessage.success(`已导入 ${n} 条用例`)
    capSelected.value = []
    await refreshCaptured()
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    importing.value = false
  }
}
async function importSelectedApis() {
  if (!apiSelected.value.length) { ElMessage.warning('请勾选要导入的接口'); return }
  importing.value = true
  try {
    const n = await ImportApisAsTestCases(apiSelected.value)
    ElMessage.success(`已导入 ${n} 条用例`)
    apiSelected.value = []
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    importing.value = false
  }
}

// ---------------- 压力测试 ----------------
const pressure = ref({
  running: false,
  selectedIds: [],
  concurrency: 5,
  iterations: 10,
  current: 0,
  result: null, // { total, passed, failed, totalMs, avgMs, tps, errorRate, byCode }
})
function togglePressureCase(id) {
  const i = pressure.value.selectedIds.indexOf(id)
  if (i >= 0) pressure.value.selectedIds.splice(i, 1)
  else pressure.value.selectedIds.push(id)
}
async function runPressure() {
  const ids = pressure.value.selectedIds
  if (!ids.length) { ElMessage.warning('请勾选压测用例'); return }
  pressure.value.running = true
  pressure.value.current = 0
  pressure.value.result = null
  let total = 0, passed = 0, failed = 0, totalMs = 0
  const byCode = {}
  const t0 = performance.now()
  try {
    for (let it = 1; it <= pressure.value.iterations; it++) {
      pressure.value.current = it
      const r = await RunTestCases(ids, curEnvId.value, pressure.value.concurrency)
      total += r.total
      passed += r.passed
      failed += r.failed
      totalMs += r.durationMs
      for (const res of (r.results || [])) {
        const c = String(res.status || (res.error ? 'ERR' : 'NA'))
        byCode[c] = (byCode[c] || 0) + 1
      }
    }
    const wallMs = performance.now() - t0
    pressure.value.result = {
      total, passed, failed, totalMs, avgMs: Math.round(totalMs / total || 0),
      tps: +(total / (wallMs / 1000)).toFixed(1),
      errorRate: total ? +((failed / total) * 100).toFixed(1) : 0,
      byCode,
    }
    ElMessage.success('压测完成')
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    pressure.value.running = false
  }
}
</script>

<template>
  <div class="test-center">
    <el-tabs v-model="tab" class="tc-tabs">
      <el-tab-pane label="测试用例" name="cases" />
      <el-tab-pane label="用例导入" name="import" />
      <el-tab-pane label="压力测试" name="pressure" />
      <el-tab-pane label="执行计划" name="plans" />
      <el-tab-pane label="测试报告" name="reports" />
    </el-tabs>

    <!-- ========== 用例 ========== -->
    <div v-show="tab === 'cases'" class="tc-page">
      <div class="toolbar">
        <el-button type="primary" @click="openGenerate">✨ AI 生成用例（选择接口）</el-button>
        <el-button type="success" :loading="running" :disabled="!selectedCaseIds.length" @click="runSelected">
          运行选中用例
        </el-button>
        <el-button type="warning" :loading="running" :disabled="!cases.length" @click="runAll">
          运行全部用例
        </el-button>
        <el-tooltip content="并发执行数（同时发起的请求数），可显著加快大量用例的运行" placement="top">
          <span class="conc-wrap">
            并发
            <el-input-number v-model="runConcurrency" :min="1" :max="20" size="small" controls-position="right" style="width:96px" />
          </span>
        </el-tooltip>
        <span class="spacer" />
        <el-select v-model="planTarget" placeholder="选择计划" size="default" style="width:180px" clearable>
          <el-option v-for="p in plans" :key="p.id" :label="p.name" :value="p.id" />
        </el-select>
        <el-button :disabled="!selectedCaseIds.length" @click="addSelectedToPlan">加入计划</el-button>
        <span class="sel-tip">已选 {{ selectedCaseIds.length }} 条</span>
        <el-button style="margin-left:auto" type="danger" plain @click="clearCases">清空用例</el-button>
      </div>

      <el-table :data="cases" style="width:100%" empty-text="暂无测试用例，点击「AI 生成用例」后生成">
        <el-table-column width="46">
          <template #default="{ row }">
            <el-checkbox :model-value="selectedCaseIds.includes(row.id)" @change="toggleCase(row.id)" />
          </template>
        </el-table-column>
        <el-table-column prop="name" label="用例名称" min-width="180" />
        <el-table-column label="分类" width="110">
          <template #default="{ row }">
            <el-tag :type="catType[row.category] || 'info'" size="small">{{ row.category }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="请求" min-width="260">
          <template #default="{ row }">
            <span class="mtag" :class="methodClass(row.method)">{{ row.method }}</span>
            <span class="murl">{{ row.url }}</span>
          </template>
        </el-table-column>
        <el-table-column label="断言" width="80">
          <template #default="{ row }">{{ (row.assertions || []).length }}</template>
        </el-table-column>
        <el-table-column label="操作" width="150">
          <template #default="{ row }">
            <el-button link type="primary" @click="openCaseEditor(row)">编辑</el-button>
            <el-button link type="danger" @click="onDeleteCase(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- ========== 用例导入 ========== -->
    <div v-show="tab === 'import'" class="tc-page">
      <el-tabs>
        <el-tab-pane label="从浏览器捕获导入">
          <div class="toolbar">
            <el-button :loading="importing" @click="importSelectedCap" :disabled="!capSelected.length">导入选中（{{ capSelected.length }}）</el-button>
            <el-button link type="primary" @click="refreshCaptured">刷新捕获列表</el-button>
            <span class="tip">将「请求捕获」中抓到的请求转为测试用例</span>
          </div>
          <el-table :data="capturedList" style="width:100%" empty-text="暂无捕获请求，请先在「请求捕获」中开启捕获">
            <el-table-column width="46">
              <template #default="{ row }">
                <el-checkbox :model-value="capSelected.includes(row.id)" @change="toggleCap(row.id)" />
              </template>
            </el-table-column>
            <el-table-column label="方法" width="90">
              <template #default="{ row }"><span class="mtag" :class="methodClass(row.method)">{{ row.method }}</span></template>
            </el-table-column>
            <el-table-column prop="url" label="URL" min-width="320" />
            <el-table-column prop="source" label="来源" width="140" />
          </el-table>
        </el-tab-pane>
        <el-tab-pane label="从接口导入">
          <div class="toolbar">
            <el-button :loading="importing" @click="importSelectedApis" :disabled="!apiSelected.length">导入选中（{{ apiSelected.length }}）</el-button>
            <span class="tip">将「接口管理」中的接口转为测试用例</span>
          </div>
          <el-table :data="projectApis()" style="width:100%" empty-text="暂无接口">
            <el-table-column width="46">
              <template #default="{ row }">
                <el-checkbox :model-value="apiSelected.includes(row.id)" @change="toggleApi(row.id)" />
              </template>
            </el-table-column>
            <el-table-column label="方法" width="90">
              <template #default="{ row }"><span class="mtag" :class="methodClass(row.method)">{{ row.method }}</span></template>
            </el-table-column>
            <el-table-column prop="name" label="接口名称" min-width="200" />
            <el-table-column prop="url" label="URL" min-width="280" />
          </el-table>
        </el-tab-pane>
      </el-tabs>
    </div>

    <!-- ========== 压力测试 ========== -->
    <div v-show="tab === 'pressure'" class="tc-page">
      <div class="toolbar">
        <span class="tip">勾选用例并设置并发数与轮次，对一组用例进行多轮并发压测，统计吞吐与错误率</span>
      </div>
      <div class="pressure-config">
        <div class="pcfg-item">
          <label>并发数</label>
          <el-input-number v-model="pressure.concurrency" :min="1" :max="50" size="small" controls-position="right" />
        </div>
        <div class="pcfg-item">
          <label>压测轮次</label>
          <el-input-number v-model="pressure.iterations" :min="1" :max="100" size="small" controls-position="right" />
        </div>
        <el-button type="danger" :loading="pressure.running" :disabled="!pressure.selectedIds.length" @click="runPressure">
          {{ pressure.running ? `压测中 ${pressure.current}/${pressure.iterations}` : '开始压测' }}
        </el-button>
        <span class="tip">已选 {{ pressure.selectedIds.length }} 条用例</span>
      </div>

      <el-table :data="cases" style="width:100%; margin-top:14px" empty-text="请在下方勾选压测用例">
        <el-table-column width="46">
          <template #default="{ row }">
            <el-checkbox :model-value="pressure.selectedIds.includes(row.id)" @change="togglePressureCase(row.id)" />
          </template>
        </el-table-column>
        <el-table-column prop="name" label="用例名称" min-width="200" />
        <el-table-column label="方法" width="90">
          <template #default="{ row }"><span class="mtag" :class="methodClass(row.method)">{{ row.method }}</span></template>
        </el-table-column>
        <el-table-column prop="url" label="URL" min-width="280" />
      </el-table>

      <div v-if="pressure.result" class="pressure-result">
        <div class="rs"><div class="rs-n">{{ pressure.result.total }}</div><div>总请求</div></div>
        <div class="rs ok"><div class="rs-n">{{ pressure.result.passed }}</div><div>通过</div></div>
        <div class="rs fail"><div class="rs-n">{{ pressure.result.failed }}</div><div>失败</div></div>
        <div class="rs"><div class="rs-n">{{ pressure.result.tps }}</div><div>TPS</div></div>
        <div class="rs"><div class="rs-n">{{ pressure.result.avgMs }}ms</div><div>平均耗时</div></div>
        <div class="rs"><div class="rs-n">{{ pressure.result.errorRate }}%</div><div>错误率</div></div>
        <div class="rs"><div class="rs-n">{{ pressure.result.totalMs }}ms</div><div>累计耗时</div></div>
      </div>
      <div v-if="pressure.result && pressure.result.byCode && Object.keys(pressure.result.byCode).length" class="pressure-codes">
        <span class="tip">状态码分布：</span>
        <el-tag v-for="(v, k) in pressure.result.byCode" :key="k" size="small" class="code-tag">{{ k }}: {{ v }}</el-tag>
      </div>
    </div>

    <!-- ========== 计划 ========== -->
    <div v-show="tab === 'plans'" class="tc-page">
      <div class="toolbar">
        <el-button type="primary" @click="openPlanEditor(null)">＋ 新建计划</el-button>
        <span class="tip">将用例编排为有序计划并绑定运行环境后统一执行</span>
        <el-button style="margin-left:auto" type="danger" plain @click="clearPlans">清空计划</el-button>
      </div>
      <el-table :data="plans" style="width:100%" empty-text="暂无测试计划">
        <el-table-column prop="name" label="计划名称" min-width="180" />
        <el-table-column label="用例数" width="90">
          <template #default="{ row }">{{ (row.caseIds || []).length }}</template>
        </el-table-column>
        <el-table-column label="环境" min-width="140">
          <template #default="{ row }">
            <span v-if="row.envId">{{ (currentProject().environments.find(e => e.id === row.envId) || {}).name || row.envId }}</span>
            <span v-else class="tip">无环境</span>
          </template>
        </el-table-column>
        <el-table-column label="并发" width="80">
          <template #default="{ row }">{{ row.concurrency || 1 }}</template>
        </el-table-column>
        <el-table-column label="更新时间" min-width="170">
          <template #default="{ row }">{{ row.updatedAt }}</template>
        </el-table-column>
        <el-table-column label="操作" width="220">
          <template #default="{ row }">
            <el-button link type="primary" :loading="running" @click="runPlan(row)">运行</el-button>
            <el-button link type="primary" @click="openPlanEditor(row)">编辑</el-button>
            <el-button link type="danger" @click="onDeletePlan(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- ========== 报告 ========== -->
    <div v-show="tab === 'reports'" class="tc-page">
      <div class="toolbar">
        <span class="tip">运行计划或用例后将生成测试报告</span>
        <el-button style="margin-left:auto" type="danger" plain @click="clearReports">清空报告</el-button>
      </div>
      <el-table :data="reports" style="width:100%" empty-text="暂无测试报告，运行计划或用例后将生成">
        <el-table-column prop="planName" label="计划 / 来源" min-width="180" />
        <el-table-column label="通过率" width="120">
          <template #default="{ row }">
            <span :class="row.failed === 0 ? 'ok' : 'fail'">
              {{ row.passed }}/{{ row.total }}
            </span>
          </template>
        </el-table-column>
        <el-table-column label="耗时" width="100">
          <template #default="{ row }">{{ row.durationMs }} ms</template>
        </el-table-column>
        <el-table-column prop="createdAt" label="生成时间" min-width="170" />
        <el-table-column label="操作" width="160">
          <template #default="{ row }">
            <el-button link type="primary" @click="viewReport(row)">查看</el-button>
            <el-button link type="danger" @click="onDeleteReport(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- AI 生成对话框 -->
    <el-dialog v-model="genVisible" title="AI 生成测试用例" width="620px">
      <div class="gen-tip">
        选择要生成测试的接口（可多选）。AI 会基于接口的方法、地址、参数结构自动生成覆盖正常流程、边界值、异常与权限场景的测试用例，每条用例包含完整请求与断言。
      </div>
      <div class="gen-select-bar">
        <span class="sel-count">已选 {{ genApiIds.length }} / {{ projectApis().length }} 个接口</span>
        <span class="spacer" />
        <el-button size="small" link type="primary" @click="selectAllApis">全选</el-button>
        <el-button size="small" link @click="clearApiSelection">清空</el-button>
      </div>
      <el-select v-model="genApiIds" multiple filterable placeholder="选择接口（可全选）" :disabled="!!genJobId" style="width:100%; margin-top:12px">
        <el-option v-for="a in projectApis()" :key="a.id"
          :label="`${a.method} ${a.name || a.url}`" :value="a.id" />
      </el-select>

      <div v-if="genJobId" class="gen-progress">
        <el-progress :percentage="genPercent"
          :status="genStat.phase.startsWith('error') ? 'exception' : (genStat.done >= genStat.total && genStat.total ? 'success' : '')" />
        <div class="gen-status">
          <template v-if="genStat.phase === 'queued'">任务已排队，等待生成…</template>
          <template v-else-if="genStat.phase === 'generating'">正在生成：{{ genStat.name }}（{{ genStat.done }}/{{ genStat.total }}）</template>
          <template v-else-if="genStat.phase.startsWith('error')">「{{ genStat.name }}」生成失败：{{ genStat.phase.replace('error:', '') }}</template>
          <template v-else>已完成 {{ genStat.done }}/{{ genStat.total }} 个接口</template>
        </div>
        <div class="gen-hint">任务在后台运行，可关闭此弹窗或切换到其它页面；稍后回到「接口测试」即可查看进度与结果。</div>
      </div>

      <template #footer>
        <el-button @click="genVisible = false">关闭</el-button>
        <el-button type="primary" :loading="genLoading" :disabled="!!genJobId" @click="doGenerate">生成</el-button>
      </template>
    </el-dialog>

    <!-- 用例编辑对话框 -->
    <el-dialog v-model="caseVisible" title="编辑测试用例" width="760px" top="4vh">
      <template v-if="editingCase">
        <div class="editor-grid">
          <div class="eg-item">
            <label>用例名称</label>
            <el-input v-model="editingCase.name" />
          </div>
          <div class="eg-item">
            <label>分类</label>
            <el-select v-model="editingCase.category" style="width:100%">
              <el-option label="正常流程" value="正常流程" />
              <el-option label="参数边界" value="参数边界" />
              <el-option label="异常场景" value="异常场景" />
              <el-option label="权限安全" value="权限安全" />
            </el-select>
          </div>
          <div class="eg-item">
            <label>请求方法</label>
            <el-select v-model="editingCase.method" style="width:100%">
              <el-option v-for="m in ['GET','POST','PUT','DELETE','PATCH','HEAD','OPTIONS']" :key="m" :label="m" :value="m" />
            </el-select>
          </div>
          <div class="eg-item eg-url">
            <label>请求地址</label>
            <el-input v-model="editingCase.url" placeholder="支持 {{变量}}" />
          </div>
        </div>
        <div class="eg-desc">
          <label>用例说明</label>
          <el-input v-model="editingCase.description" type="textarea" :rows="2" />
        </div>

        <el-tabs class="eg-tabs">
          <el-tab-pane label="请求头" name="h">
            <KVEditor :items="editingCase.headers" key-placeholder="参数名" />
          </el-tab-pane>
          <el-tab-pane label="Query 参数" name="q">
            <KVEditor :items="editingCase.query" key-placeholder="参数名" />
          </el-tab-pane>
          <el-tab-pane label="请求体 Body" name="b">
            <el-radio-group v-model="editingCase.bodyType" size="small">
              <el-radio-button value="none">none</el-radio-button>
              <el-radio-button value="json">JSON</el-radio-button>
              <el-radio-button value="form">Form</el-radio-button>
              <el-radio-button value="text">Raw</el-radio-button>
            </el-radio-group>
            <el-input v-if="editingCase.bodyType !== 'none'" v-model="editingCase.body"
              type="textarea" :rows="6" class="mono" style="margin-top:10px"
              placeholder='JSON 请求体，如 {"name":"张三"}' />
          </el-tab-pane>
          <el-tab-pane label="断言 Assertions" name="a">
            <div class="assert-head">
              <el-button size="small" type="primary" plain @click="addAssertion">＋ 添加断言</el-button>
              <span class="tip">断言用于校验响应是否符合预期</span>
            </div>
            <div class="assert-table">
              <div class="at-row at-head">
                <span>启用</span><span>类型</span><span>目标</span><span>操作符</span><span>期望值</span><span></span>
              </div>
              <div v-for="(a, i) in editingCase.assertions" :key="i" class="at-item">
                <div class="at-row">
                  <el-checkbox v-model="a.enabled" />
                  <el-select v-model="a.type" size="small">
                    <el-option v-for="t in assertionTypes" :key="t" :label="assertionLabels[t] || t" :value="t" />
                  </el-select>
                  <el-input v-model="a.target" size="small" :placeholder="assertionHint(a.type)" :disabled="assertionTargetDisabled(a.type)" />
                  <el-select v-model="a.operator" size="small">
                    <el-option v-for="o in assertionOps" :key="o" :label="o" :value="o" />
                  </el-select>
                  <el-input v-model="a.expected" size="small" placeholder="200" />
                  <el-button link type="danger" @click="removeAssertion(i)">✕</el-button>
                </div>
                <div v-if="a.type==='regex' || a.type==='size' || a.type==='duration' || a.type==='contentType' || a.type==='cookie'" class="at-hint">
                  {{ assertionHint(a.type) }}
                </div>
              </div>
              <div v-if="!editingCase.assertions.length" class="assert-empty">暂无断言</div>
            </div>
          </el-tab-pane>
        </el-tabs>
      </template>
      <template #footer>
        <el-button @click="caseVisible = false">取消</el-button>
        <el-button type="primary" @click="saveCase">保存</el-button>
      </template>
    </el-dialog>

    <!-- 计划编辑对话框 -->
    <el-dialog v-model="planVisible" title="编辑测试计划" width="720px" top="4vh">
      <template v-if="editingPlan">
        <div class="editor-grid">
          <div class="eg-item eg-url">
            <label>计划名称</label>
            <el-input v-model="editingPlan.name" />
          </div>
          <div class="eg-item">
            <label>运行环境</label>
            <el-select v-model="editingPlan.envId" style="width:100%" placeholder="无环境" clearable>
              <el-option v-for="e in currentProject().environments" :key="e.id" :label="e.name" :value="e.id" />
            </el-select>
          </div>
          <div class="eg-item">
            <label>并发执行数</label>
            <el-input-number v-model="editingPlan.concurrency" :min="1" :max="20" size="small" controls-position="right" style="width:100%" />
          </div>
        </div>
        <div class="plan-cases">
          <div class="pc-title">
            计划用例（按顺序执行，勾选加入）
            <span class="pc-ops">
              <el-button size="small" link type="primary" :disabled="!cases.length" @click="selectAllPlanCases">全选</el-button>
              <el-button size="small" link :disabled="!(editingPlan.caseIds||[]).length" @click="clearPlanCases">清空</el-button>
              <span class="pc-count">已选 {{ (editingPlan.caseIds||[]).length }} / {{ cases.length }}</span>
            </span>
          </div>
          <div v-if="!cases.length" class="assert-empty">请先在「测试用例」中生成用例</div>
          <div v-for="c in cases" :key="c.id" class="pc-row">
            <el-checkbox :model-value="(editingPlan.caseIds||[]).includes(c.id)" @change="togglePlanCase(c.id)" />
            <span class="mtag" :class="methodClass(c.method)">{{ c.method }}</span>
            <span class="pc-name">{{ c.name }}</span>
            <span class="pc-cat">{{ c.category }}</span>
          </div>
        </div>
      </template>
      <template #footer>
        <el-button @click="planVisible = false">取消</el-button>
        <el-button type="primary" @click="savePlan">保存</el-button>
      </template>
    </el-dialog>

    <!-- 报告查看对话框 -->
    <el-dialog v-model="reportVisible" title="测试分析报告" width="900px" top="3vh">
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
            <template #default="{ row }">{{ row.status || '-' }}</template>
          </el-table-column>
          <el-table-column label="耗时" width="90">
            <template #default="{ row }">{{ row.durationMs }} ms</template>
          </el-table-column>
          <el-table-column label="结果" width="90">
            <template #default="{ row }">
              <el-tag :type="row.passed ? 'success' : 'danger'" size="small">{{ row.passed ? '通过' : '失败' }}</el-tag>
            </template>
          </el-table-column>
        </el-table>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.test-center { padding: 16px 22px; flex: 1; min-width: 0; height: 100%; display: flex; flex-direction: column; }
.tc-tabs { background: #fff; padding: 0 12px; border-radius: 8px 8px 0 0; }
.tc-page { flex: 1; min-height: 0; overflow: auto; background: #fff; padding: 16px; border-radius: 0 0 8px 8px; }
.toolbar { display: flex; align-items: center; gap: 10px; margin-bottom: 14px; flex-wrap: wrap; }
.toolbar .spacer { flex: 1; }
.tip { color: #c2c7cf; font-size: 12px; }
.sel-tip { color: #86909c; font-size: 12px; }
.mtag { display: inline-block; font-size: 11px; font-weight: 700; color: #fff; padding: 1px 7px; border-radius: 3px; margin-right: 8px; }
.m-get { background: #0fc6c2; } .m-post { background: #165dff; } .m-put { background: #ff7d00; }
.m-delete { background: #f53f3f; } .m-patch { background: #722ed1; } .m-head, .m-options { background: #86909c; }
.murl { color: #4e5969; font-size: 13px; word-break: break-all; }
.ok { color: #00b42a; font-weight: 600; } .fail { color: #f53f3f; font-weight: 600; }

.gen-tip { font-size: 13px; color: #4e5969; background: #f2f3f5; border-radius: 6px; padding: 10px 12px; line-height: 1.7; }
.gen-select-bar { display: flex; align-items: center; gap: 10px; margin-top: 12px; }
.gen-select-bar .sel-count { font-size: 12px; color: #86909c; }
.gen-select-bar .spacer { flex: 1; }
.gen-progress { margin-top: 16px; padding: 14px 16px; background: #f7f8fa; border: 1px solid #e5e6eb; border-radius: 8px; }
.gen-status { margin-top: 8px; font-size: 13px; color: #4e5969; }
.gen-hint { margin-top: 8px; font-size: 12px; color: #c2c7cf; line-height: 1.6; }

.editor-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.eg-item { display: flex; flex-direction: column; gap: 4px; }
.eg-item.eg-url { grid-column: 1 / -1; }
.eg-item label { font-size: 12px; color: #86909c; }
.eg-desc { margin-top: 12px; display: flex; flex-direction: column; gap: 4px; }
.eg-desc label { font-size: 12px; color: #86909c; }
.eg-tabs { margin-top: 16px; }
.mono :deep(textarea) { font-family: Consolas, "Courier New", monospace; font-size: 12.5px; }

.assert-head { display: flex; align-items: center; gap: 10px; margin-bottom: 10px; }
.assert-table { border: 1px solid #e5e6eb; border-radius: 8px; overflow: hidden; }
.at-item { border-bottom: 1px solid #f2f3f5; }
.at-item:last-child { border-bottom: none; }
.at-row { display: grid; grid-template-columns: 50px 110px 1fr 100px 1fr 40px; gap: 8px; align-items: center; padding: 8px 10px; }
.at-head { background: #f7f8fa; font-size: 12px; color: #86909c; }
.at-hint { font-size: 11px; color: #86909c; padding: 0 10px 8px 60px; line-height: 1.5; }
.assert-empty, .assert-empty { color: #c9cdd4; font-size: 13px; text-align: center; padding: 16px 0; }
.conc-wrap { display: inline-flex; align-items: center; gap: 4px; font-size: 13px; color: #4e5969; }

.plan-cases { margin-top: 16px; border: 1px solid #e5e6eb; border-radius: 8px; padding: 12px; max-height: 360px; overflow: auto; }
.pc-title { font-size: 13px; color: #4e5969; margin-bottom: 10px; font-weight: 600; display: flex; align-items: center; gap: 10px; }
.pc-ops { margin-left: auto; display: flex; align-items: center; gap: 6px; font-weight: 400; }
.pc-count { font-size: 12px; color: #86909c; }
.pc-row { display: flex; align-items: center; gap: 10px; padding: 6px 4px; border-bottom: 1px dashed #f2f3f5; }
.pc-name { font-size: 13px; } .pc-cat { color: #86909c; font-size: 12px; }

.report-stats { display: flex; gap: 12px; flex-wrap: wrap; margin-bottom: 14px; }
.rs { background: #f7f8fa; border: 1px solid #e5e6eb; border-radius: 10px; padding: 12px 18px; min-width: 96px; text-align: center; }
.rs .rs-n { font-size: 22px; font-weight: 700; } .rs.ok .rs-n { color: #00b42a; } .rs.fail .rs-n { color: #f53f3f; }
.report-bar { display: flex; gap: 8px; margin-bottom: 12px; flex-wrap: wrap; }
.report-summary { background: #f7f8fa; border: 1px solid #e5e6eb; border-radius: 8px; padding: 12px 14px; margin-bottom: 12px; }
.rs-title { font-weight: 600; margin-bottom: 8px; font-size: 14px; }
.report-summary pre { white-space: pre-wrap; word-break: break-word; font-family: inherit; font-size: 13px; line-height: 1.8; margin: 0; }
.ar-row { font-size: 12px; padding: 3px 0; color: #4e5969; }
.ar-error { font-size: 12px; padding: 3px 0; color: #f53f3f; }

.pressure-config { display: flex; align-items: center; gap: 16px; flex-wrap: wrap; }
.pcfg-item { display: flex; align-items: center; gap: 8px; font-size: 13px; color: #4e5969; }
.pressure-result { display: flex; gap: 12px; flex-wrap: wrap; margin-top: 16px; }
.pressure-codes { margin-top: 12px; display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.code-tag { font-family: Consolas, monospace; }
</style>
