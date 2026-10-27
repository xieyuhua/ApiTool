<script setup>
// 企业级文档结构配置：封面 / 目录 / 文档公共描述 / 接口统一定义规范 / 附录状态码 / 修订记录。
// 直接读写全局设置并即时保存，供「文档中心」导出时使用（Word / PDF / HTML / Markdown 均生效）。
import { computed, ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { ElMessageBox } from 'element-plus'
import {
  DefaultDocConvention, PreviewDocWatermark, DocPresets, DocPresetLayout,
  SaveDocLayoutTemplate, DeleteDocLayoutTemplate,
} from '../../../wailsjs/go/main/App'
import { store, saveNow, reloadStore } from '../../store'

const props = defineProps({
  dirId: { type: String, default: '' },
  apiId: { type: String, default: '' },
})

const dl = computed(() => store.data.settings.docLayout)
const previewing = ref(false)
const presets = ref([])   // 后端返回的预置模板清单
const applying = ref('')

// normalize 补齐字段：老数据或后端返回的 null 数组都要转成可用结构
function normalize(v) {
  v.cover = v.cover || {}
  const coverDef = { enabled: false, title: '', subtitle: '', author: '', org: '', version: 'V1.0.0', date: '' }
  for (const k of Object.keys(coverDef)) {
    if (v.cover[k] === undefined || v.cover[k] === null) v.cover[k] = coverDef[k]
  }
  if (typeof v.toc !== 'boolean') v.toc = true
  if (typeof v.numbering !== 'boolean') v.numbering = true
  if (typeof v.statusCodes !== 'boolean') v.statusCodes = false
  if (typeof v.convention !== 'string') v.convention = ''
  if (!Array.isArray(v.prefaces)) v.prefaces = []
  if (!Array.isArray(v.revisions)) v.revisions = []
  return v
}

onMounted(async () => {
  normalize(dl.value)
  try { presets.value = await DocPresets() || [] } catch { presets.value = [] }
})

// 套用预置模板（覆盖当前文档结构，水印配置不受影响）
async function applyPreset(p) {
  if (applying.value) return
  if (!window.confirm(`套用「${p.label}」会覆盖当前文档结构配置，确定继续吗？`)) return
  applying.value = p.key
  try {
    const layout = normalize(await DocPresetLayout(p.key))
    // 整体替换当前结构（水印配置独立保存，不受影响）
    Object.assign(store.data.settings.docLayout, layout)
    await saveNow()
    ElMessage.success('已套用模板：' + p.label)
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    applying.value = ''
  }
}

// ---------------- 自定义模板（团队模板库） ----------------
const myTemplates = computed(() => store.data.settings.docLayoutTemplates || [])

// 把当前结构保存为命名模板（同名覆盖）
async function saveAsTemplate() {
  let name = ''
  try {
    const r = await ElMessageBox.prompt('模板名称（如「对外版 v2」「评审版」）', '保存为自定义模板', {
      inputPlaceholder: '模板名称', inputValidator: v => (v && v.trim() ? true : '请输入模板名称'),
    })
    name = (r.value || '').trim()
  } catch { return }
  try {
    await SaveDocLayoutTemplate(name, JSON.stringify(store.data.settings.docLayout))
    await reloadTemplates()
    ElMessage.success('已保存模板：' + name)
  } catch (e) {
    ElMessage.error(String(e))
  }
}

// 套用自定义模板
async function applyTemplate(tpl) {
  if (!window.confirm(`套用「${tpl.name}」会覆盖当前文档结构配置，确定继续吗？`)) return
  try {
    Object.assign(store.data.settings.docLayout, normalize(JSON.parse(JSON.stringify(tpl.layout))))
    await saveNow()
    ElMessage.success('已套用模板：' + tpl.name)
  } catch (e) {
    ElMessage.error(String(e))
  }
}

// 删除自定义模板
async function removeTemplate(tpl) {
  if (!window.confirm(`删除模板「${tpl.name}」？`)) return
  try {
    await DeleteDocLayoutTemplate(tpl.id)
    await reloadTemplates()
    ElMessage.success('已删除模板：' + tpl.name)
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function reloadTemplates() {
  try {
    await reloadStore()
    normalize(dl.value)
  } catch { /* 忽略：模板列表刷新失败不影响导出 */ }
}

async function save() {
  try {
    await saveNow()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function saveWithTip(msg) {
  await save()
  ElMessage.success(msg)
}

// ---------------- 封面 ----------------
function toggleCover(on) {
  dl.value.cover.enabled = on
  saveWithTip(on ? '已启用封面' : '已关闭封面')
}

// ---------------- 文档公共描述 ----------------
function addPreface() {
  dl.value.prefaces.push({ title: `文档说明 ${dl.value.prefaces.length + 1}`, content: '' })
  save()
}
function removePreface(i) {
  dl.value.prefaces.splice(i, 1)
  saveWithTip('已删除该章节')
}

// ---------------- 接口统一定义规范 ----------------
async function fillConvention() {
  try {
    if (String(dl.value.convention || '').trim() && !confirmFilled()) return
    dl.value.convention = await DefaultDocConvention()
    saveWithTip('已填入默认规范模板，可按团队要求修改')
  } catch (e) {
    ElMessage.error(String(e))
  }
}
function confirmFilled() {
  return window.confirm('当前已有规范内容，确定用默认模板覆盖吗？')
}

// ---------------- 修订记录 ----------------
function addRevision() {
  const today = new Date().toISOString().slice(0, 10)
  dl.value.revisions.push({ version: '', date: today, author: '', note: '' })
  save()
}
function removeRevision(i) {
  dl.value.revisions.splice(i, 1)
  saveWithTip('已删除该修订记录')
}

// ---------------- 预览 ----------------
async function preview() {
  previewing.value = true
  try {
    await saveNow()
    const path = await PreviewDocWatermark(props.dirId || '', props.apiId || '')
    if (path) ElMessage.success('已在浏览器打开预览：' + path)
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    previewing.value = false
  }
}
</script>

<template>
  <div class="card">
    <div class="card-title">文档结构（企业级排版）</div>
    <div class="dl-tip">
      按需勾选要输出的章节，导出顺序固定为：封面 → 修订记录 → 目录 → 文档公共描述 → 接口统一定义规范 → 接口正文 → 附录。
      Word / PDF / HTML / Markdown 都会套用同一套结构，配置自动保存。
    </div>

    <div class="dl-presets" v-if="presets.length">
      <span class="dl-presets-label">套用模板：</span>
      <el-tooltip v-for="p in presets" :key="p.key" :content="p.desc" placement="top">
        <el-button size="small" :loading="applying === p.key" @click="applyPreset(p)">{{ p.label }}</el-button>
      </el-tooltip>
    </div>

    <div class="dl-presets">
      <span class="dl-presets-label">我的模板：</span>
      <el-tag v-for="tpl in myTemplates" :key="tpl.id" class="dl-tpl" closable
        @close="removeTemplate(tpl)" @click="applyTemplate(tpl)">{{ tpl.name }}</el-tag>
      <span v-if="!myTemplates.length" class="dl-hint" style="margin-left:0">暂无自定义模板</span>
      <el-button size="small" link type="primary" @click="saveAsTemplate">+ 保存当前结构为模板</el-button>
    </div>

    <el-form label-width="104px" label-position="left">
      <el-form-item label="生成目录">
        <el-switch v-model="dl.toc" @change="save" />
        <span class="dl-hint">Word 为可「更新域」的目录（带页码），PDF / HTML 为带锚点的章节目录</span>
      </el-form-item>
      <el-form-item label="章节编号">
        <el-switch v-model="dl.numbering" @change="save" />
        <span class="dl-hint">章节与接口自动编号（1 / 1.1 / 1.1.1），Word、PDF、Markdown 编号一致</span>
      </el-form-item>

      <el-form-item label="作者封面">
        <el-switch :model-value="dl.cover.enabled" @change="toggleCover" />
        <span class="dl-hint">单独一页：标题 / 副标题 / 作者 / 单位 / 版本 / 日期</span>
      </el-form-item>
      <template v-if="dl.cover.enabled">
        <el-form-item label="标题">
          <el-input v-model="dl.cover.title" placeholder="留空则用文档标题" style="width:340px" @change="save" />
        </el-form-item>
        <el-form-item label="副标题">
          <el-input v-model="dl.cover.subtitle" placeholder="如：内部技术文档 · 禁止外传" style="width:340px" @change="save" />
        </el-form-item>
        <el-form-item label="作者/单位">
          <el-input v-model="dl.cover.author" placeholder="作者" style="width:160px" @change="save" />
          <el-input v-model="dl.cover.org" placeholder="公司 / 部门" style="width:170px;margin-left:10px" @change="save" />
        </el-form-item>
        <el-form-item label="版本/日期">
          <el-input v-model="dl.cover.version" placeholder="V1.0.0" style="width:120px" @change="save" />
          <el-date-picker v-model="dl.cover.date" type="date" value-format="YYYY-MM-DD" placeholder="留空用导出日期"
            style="margin-left:10px;width:190px" @change="save" />
        </el-form-item>
      </template>

      <el-form-item label="公共描述">
        <el-button size="small" @click="addPreface">+ 添加章节</el-button>
        <span class="dl-hint">{{ dl.prefaces.length }} 个章节（文档说明 / 变更背景 / 术语表等，支持标题、列表、表格、代码块）</span>
      </el-form-item>
      <template v-for="(p, i) in dl.prefaces" :key="i">
        <el-form-item :label="`章节 ${i + 1}`">
          <div class="dl-chapter">
            <el-input v-model="p.title" placeholder="章节标题" style="width:300px" @change="save" />
            <el-button link type="danger" style="margin-left:8px" @click="removePreface(i)">删除</el-button>
            <el-input v-model="p.content" type="textarea" :rows="4" class="dl-md"
              placeholder="支持简化 Markdown：#### 小标题 / - 列表 / **加粗** / `代码` / 表格 / ```代码块```" @change="save" />
          </div>
        </el-form-item>
      </template>

      <el-form-item label="统一定义规范">
        <div style="width:100%">
          <div style="margin-bottom:6px">
            <el-button size="small" @click="fillConvention">填入默认模板</el-button>
            <span class="dl-hint">命名与路径、请求/响应约定、鉴权、错误码、版本兼容等团队规范</span>
          </div>
          <el-input v-model="dl.convention" type="textarea" :rows="6" class="dl-md"
            placeholder="留空则不输出该章节；可点「填入默认模板」后修改" @change="save" />
        </div>
      </el-form-item>

      <el-form-item label="附录状态码">
        <el-switch v-model="dl.statusCodes" @change="save" />
        <span class="dl-hint">附上常用 HTTP 状态码的名称、含义与典型场景</span>
      </el-form-item>

      <el-form-item label="修订记录">
        <div>
          <el-button size="small" @click="addRevision">+ 添加记录</el-button>
          <span class="dl-hint">{{ dl.revisions.length }} 条（版本 / 日期 / 作者 / 说明）</span>
          <div v-for="(r, i) in dl.revisions" :key="i" class="dl-rev">
            <el-input v-model="r.version" placeholder="V1.0.0" style="width:100px" @change="save" />
            <el-date-picker v-model="r.date" type="date" value-format="YYYY-MM-DD" placeholder="日期"
              style="width:150px" @change="save" />
            <el-input v-model="r.author" placeholder="作者" style="width:120px" @change="save" />
            <el-input v-model="r.note" placeholder="修订说明" style="width:220px" @change="save" />
            <el-button link type="danger" @click="removeRevision(i)">删除</el-button>
          </div>
        </div>
      </el-form-item>

      <el-form-item label="效果预览">
        <el-button size="small" type="primary" plain :loading="previewing" @click="preview">👁 预览整份文档</el-button>
        <span class="dl-hint">用当前范围与结构在浏览器打开，按 Ctrl+P 可看打印分页效果</span>
      </el-form-item>
    </el-form>
  </div>
</template>

<style scoped>
.dl-tip { font-size: 13px; color: #4e5969; margin-bottom: 12px; }
.dl-presets { display: flex; align-items: center; gap: 8px; margin-bottom: 14px; flex-wrap: wrap; }
.dl-presets-label { font-size: 13px; color: #4e5969; }
.dl-tpl { cursor: pointer; }
.dl-hint { font-size: 12px; color: #86909c; margin-left: 10px; }
.dl-chapter { width: 100%; display: flex; flex-direction: column; gap: 6px; }
.dl-chapter :deep(.el-input) { align-self: flex-start; }
.dl-md :deep(textarea) { font-family: Consolas, 'Courier New', monospace; font-size: 12.5px; }
.dl-rev { display: flex; gap: 8px; align-items: center; margin-top: 8px; flex-wrap: wrap; }
</style>
