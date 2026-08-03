<script setup>
import { ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ParseFields, FormatJSON, GenerateDescriptions } from '../../../wailsjs/go/main/App'
import { getLiveResponse, markDebugDirty, debugDirty, saveDebugNow } from '../../store'
import FieldTable from './FieldTable.vue'
import KVEditor from '../test/KVEditor.vue'

const props = defineProps({ api: { type: Object, required: true } })

const tab = ref('req') // req | resp | headers
const importVisible = ref(false)
const importTarget = ref('req')
const importJson = ref('')
const aiLoading = ref(false)

function openImport(target) {
  importTarget.value = target
  importJson.value = ''
  importVisible.value = true
}

async function fmtImportJson() {
  try {
    importJson.value = await FormatJSON(importJson.value)
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function doImport() {
  if (!importJson.value.trim()) { ElMessage.warning('请粘贴 JSON 内容'); return }
  const target = importTarget.value
  const existing = target === 'req' ? props.api.reqFields : props.api.respFields
  try {
    const fields = await ParseFields(importJson.value, JSON.parse(JSON.stringify(existing)))
    if (target === 'req') props.api.reqFields = fields || []
    else props.api.respFields = fields || []
    importVisible.value = false
    tab.value = target
    markDebugDirty()
    ElMessage.success('导入成功，已保留原有字段描述')
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function importFromResponse() {
  const resp = getLiveResponse(props.api.id)
  if (!resp || !resp.isJson) { ElMessage.warning('没有可用的 JSON 响应，请先在「接口调试」中发送请求'); return }
  try {
    const fields = await ParseFields(resp.body, JSON.parse(JSON.stringify(props.api.respFields)))
    props.api.respFields = fields || []
    tab.value = 'resp'
    markDebugDirty()
    ElMessage.success('已从最近一次响应导入')
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function importFromBody() {
  if (!props.api.body || !props.api.body.trim()) { ElMessage.warning('请求体为空，请先在「接口调试」中填写 JSON 请求体'); return }
  try {
    const fields = await ParseFields(props.api.body, JSON.parse(JSON.stringify(props.api.reqFields)))
    props.api.reqFields = fields || []
    tab.value = 'req'
    markDebugDirty()
    ElMessage.success('已从请求体导入')
  } catch (e) {
    ElMessage.error(String(e))
  }
}

// 将 Query 参数并入请求参数文档（同名不重复），使查询参数也出现在结构化请求参数中
async function importFromQuery() {
  const qs = props.api.query.filter(q => q.enabled && q.key)
  if (!qs.length) { ElMessage.warning('没有可用的 Query 参数，请先在「接口调试」中填写'); return }
  const existing = JSON.parse(JSON.stringify(props.api.reqFields))
  const have = new Set(existing.map(f => f.name))
  for (const q of qs) {
    if (have.has(q.key)) continue
    existing.push({ name: q.key, type: 'string', required: false, example: q.value, description: q.description, children: [] })
    have.add(q.key)
  }
  props.api.reqFields = existing
  tab.value = 'req'
  markDebugDirty()
  ElMessage.success('已从 Query 参数导入请求参数文档')
}

async function aiComplete(target) {
  const fields = target === 'req' ? props.api.reqFields : props.api.respFields
  if (!fields.length) { ElMessage.warning('没有字段可补全'); return }
  aiLoading.value = true
  try {
    const result = await GenerateDescriptions(
      props.api.name, props.api.description || '',
      JSON.parse(JSON.stringify(fields)))
    if (target === 'req') props.api.reqFields = result || []
    else props.api.respFields = result || []
    markDebugDirty()
    ElMessage.success('AI 已补全空白字段描述')
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    aiLoading.value = false
  }
}

async function clearFields(target) {
  try {
    await ElMessageBox.confirm('确定清空所有字段？', '提示', { type: 'warning' })
    if (target === 'req') props.api.reqFields = []
    else props.api.respFields = []
    markDebugDirty()
  } catch { /* 取消 */ }
}

// 显式保存：把当前「参数设置」中的修改落盘。
// 与「接口调试」的保存请求一致——只有点击才会持久化，编辑过程不会自动保存。
async function saveParams() {
  try {
    await saveDebugNow()
    ElMessage.success('已保存参数设置')
  } catch (e) {
    ElMessage.error('保存失败：' + String(e))
  }
}
</script>

<template>
  <div class="panel-page">
    <div class="card">
      <div class="params-head">
        <span class="params-title">参数设置</span>
        <span style="flex:1" />
        <el-button size="small" type="success" @click="saveParams" title="保存当前参数设置（仅点击时落盘）">保存</el-button>
        <span v-if="debugDirty" class="dirty-tip">有未保存的修改</span>
      </div>
      <el-tabs v-model="tab">
        <el-tab-pane label="请求参数" name="req" />
        <el-tab-pane label="响应参数" name="resp" />
        <el-tab-pane label="请求头" name="headers" />
      </el-tabs>

      <div class="toolbar">
        <template v-if="tab === 'req'">
          <el-button size="small" type="primary" plain @click="openImport('req')">粘贴 JSON 导入</el-button>
          <el-button size="small" @click="importFromBody">从请求体导入</el-button>
          <el-button size="small" @click="importFromQuery">从 Query 导入</el-button>
          <el-button size="small" type="success" plain :loading="aiLoading" @click="aiComplete('req')">
            ✨ AI 补全字段描述
          </el-button>
          <el-button size="small" type="danger" text @click="clearFields('req')">清空</el-button>
        </template>
        <template v-else-if="tab === 'resp'">
          <el-button size="small" type="primary" plain @click="openImport('resp')">粘贴 JSON 导入</el-button>
          <el-button size="small" @click="importFromResponse">从最近响应导入</el-button>
          <el-button size="small" type="success" plain :loading="aiLoading" @click="aiComplete('resp')">
            ✨ AI 补全字段描述
          </el-button>
          <el-button size="small" type="danger" text @click="clearFields('resp')">清空</el-button>
        </template>
        <template v-else>
          <span class="tip">在此统一维护该接口的请求头，调试发送时会一并生效；说明字段会写入文档的请求头表格</span>
        </template>
        <span v-if="tab !== 'headers'" class="tip">导入时会自动保留同名字段已填写的描述；AI 补全仅填充空白描述</span>
      </div>

      <template v-if="tab === 'req'">
        <div class="sub-block">
          <div class="sub-title">Query 参数 <span class="sub-tip">（也属于请求参数）</span></div>
          <KVEditor :items="api.query" key-placeholder="参数名" @change="markDebugDirty" />
        </div>
        <div class="sub-block">
          <div class="sub-title">请求体字段（请求参数）</div>
          <FieldTable :fields="api.reqFields" @change="markDebugDirty" />
        </div>
      </template>
      <FieldTable v-if="tab === 'resp'" :fields="api.respFields" @change="markDebugDirty" />
      <KVEditor v-if="tab === 'headers'" :items="api.headers" key-placeholder="参数名" @change="markDebugDirty" />
    </div>

    <el-dialog v-model="importVisible" :title="importTarget === 'req' ? '导入请求参数 JSON' : '导入响应参数 JSON'"
      width="640px">
      <el-input v-model="importJson" type="textarea" :rows="14" class="mono"
        placeholder='粘贴 JSON，如：{"code": 0, "data": {"id": 1, "name": "张三"}}' />
      <template #footer>
        <el-button @click="fmtImportJson">格式化</el-button>
        <el-button @click="importVisible = false">取消</el-button>
        <el-button type="primary" @click="doImport">解析并导入</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.params-head { display: flex; align-items: center; gap: 10px; margin-bottom: 12px; }
.params-title { font-size: 15px; font-weight: 600; color: #1d2129; }
.dirty-tip { color: #fa8c16; font-size: 12px; font-weight: 500; }
.toolbar { display: flex; align-items: center; gap: 4px; margin-bottom: 12px; flex-wrap: wrap; }
.tip { color: #c2c7cf; font-size: 12px; margin-left: 10px; }
.mono :deep(textarea) { font-family: Consolas, "Courier New", monospace; font-size: 12.5px; }
.sub-block { margin-bottom: 18px; }
.sub-title { font-size: 13px; font-weight: 600; color: #4e5969; margin-bottom: 8px; }
.sub-tip { font-weight: 400; color: #c2c7cf; font-size: 12px; }
</style>
