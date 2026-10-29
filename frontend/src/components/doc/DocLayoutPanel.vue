<script setup>
// 文档结构表单：封面 / 目录 / 章节编号 / 公共描述 / 统一定义规范 / 附录状态码 / 修订记录。
// 模板库（预置 + 我的模板）在「文档模板」页面维护，本组件只负责结构本身。
import { computed, ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import {
  DefaultDocBusinessCodes, DefaultDocConvention, DocSignTemplates, PreviewDocWatermark,
} from '../../../wailsjs/go/main/App'
import { saveNow } from '../../store'

const props = defineProps({
  // 要编辑的文档结构对象：全局配置或项目级覆盖（由「文档模板」页面决定）
  layout: { type: Object, required: true },
  dirId: { type: String, default: '' },
  apiId: { type: String, default: '' },
})

const dl = computed(() => props.layout)
const previewing = ref(false)
const signTemplates = ref([]) // 后端返回的预置验签模板清单

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
  if (typeof v.environment !== 'boolean') v.environment = true
  if (typeof v.commonHeaders !== 'boolean') v.commonHeaders = true
  if (typeof v.sign !== 'string') v.sign = ''
  if (!Array.isArray(v.businessCodes)) v.businessCodes = []
  if (typeof v.convention !== 'string') v.convention = ''
  if (!Array.isArray(v.prefaces)) v.prefaces = []
  if (!Array.isArray(v.revisions)) v.revisions = []
  return v
}

onMounted(async () => {
  normalize(dl.value)
  try { signTemplates.value = (await DocSignTemplates()) || [] } catch { signTemplates.value = [] }
})

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
    if (String(dl.value.convention || '').trim() && !window.confirm('当前已有规范内容，确定用默认模板覆盖吗？')) return
    dl.value.convention = await DefaultDocConvention()
    saveWithTip('已填入默认规范模板，可按团队要求修改')
  } catch (e) {
    ElMessage.error(String(e))
  }
}

// ---------------- 加密与验签 ----------------
// 按所选模板填入（默认 MD5 参数排序拼接，含示例代码与错误码）
function fillSign(key) {
  const tpl = signTemplates.value.find(t => t.key === key)
  if (!tpl) return
  if (String(dl.value.sign || '').trim() && !window.confirm(`当前已有验签说明，确定用「${tpl.label}」覆盖吗？`)) return
  dl.value.sign = tpl.content
  saveWithTip(`已填入验签模板：${tpl.label}`)
}

// ---------------- 业务错误码 ----------------
function addCode() {
  dl.value.businessCodes.push({ code: '', message: '', desc: '' })
  save()
}
function removeCode(i) {
  dl.value.businessCodes.splice(i, 1)
  saveWithTip('已删除该错误码')
}
async function fillCodes() {
  try {
    if (dl.value.businessCodes.length && !window.confirm('当前已有错误码，确定用默认表覆盖吗？')) return
    dl.value.businessCodes = await DefaultDocBusinessCodes()
    saveWithTip('已填入默认业务错误码，可按项目实际修改')
  } catch (e) {
    ElMessage.error(String(e))
  }
}
function clearCodes() {
  if (!dl.value.businessCodes.length) return
  if (!window.confirm('清空后文档不再输出「附录：业务错误码」，确定吗？')) return
  dl.value.businessCodes = []
  saveWithTip('已清空业务错误码')
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

defineExpose({ preview })
</script>

<template>
  <div class="card">
    <div class="card-title">文档结构（企业级排版）</div>
    <div class="dl-tip">
      按需勾选要输出的章节，导出顺序固定为：封面 → 修订记录 → 目录 → 文档公共描述 → 接口统一定义规范 → 接口正文 → 附录。
      Word / PDF / HTML / Markdown 都会套用同一套结构，配置自动保存。
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

      <el-form-item label="接入地址">
        <el-switch v-model="dl.environment" @change="save" />
        <span class="dl-hint">按项目环境自动生成「接入地址（环境）」表格（测试 / 预发 / 生产等）</span>
      </el-form-item>
      <el-form-item label="公共请求头">
        <el-switch v-model="dl.commonHeaders" @change="save" />
        <span class="dl-hint">按项目「公共参数」自动生成公共请求头 / 公共查询参数表格</span>
      </el-form-item>
      <el-form-item label="加密与验签">
        <div style="width:100%">
          <div style="margin-bottom:6px">
            <el-dropdown trigger="click" @command="fillSign">
              <el-button size="small">填入验签模板 ▾</el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item v-for="t in signTemplates" :key="t.key" :command="t.key">
                    <div class="sign-item">
                      <b>{{ t.label }}</b>
                      <span>{{ t.desc }}</span>
                    </div>
                  </el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
            <span class="dl-hint">签名规则 + 示例代码（Java / Go / 前端）+ 验签错误码，填入后可自行修改</span>
          </div>
          <el-input v-model="dl.sign" type="textarea" :rows="6" class="dl-md"
            placeholder="留空则不输出该章节；可点「填入默认验签模板」后修改" @change="save" />
        </div>
      </el-form-item>

      <el-form-item label="公共描述">
        <el-button size="small" @click="addPreface">+ 添加章节</el-button>
        <span class="dl-hint">{{ dl.prefaces.length }} 个章节（文档说明 / 变更背景 / 术语表等，支持标题、列表、表格、代码块）</span>
      </el-form-item>
      <el-form-item v-for="(p, i) in dl.prefaces" :key="i" :label="`章节 ${i + 1}`">
        <div class="dl-chapter">
          <el-input v-model="p.title" placeholder="章节标题" style="width:300px" @change="save" />
          <el-button link type="danger" style="margin-left:8px" @click="removePreface(i)">删除</el-button>
          <el-input v-model="p.content" type="textarea" :rows="4" class="dl-md"
            placeholder="支持简化 Markdown：#### 小标题 / - 列表 / **加粗** / `代码` / 表格 / 代码块" @change="save" />
        </div>
      </el-form-item>

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

      <el-form-item label="业务错误码">
        <div style="width:100%">
          <div>
            <el-button size="small" @click="fillCodes">填入默认错误码</el-button>
            <el-button size="small" @click="addCode">+ 添加</el-button>
            <el-button size="small" :disabled="!dl.businessCodes.length" @click="clearCodes">清空</el-button>
            <span class="dl-hint">{{ dl.businessCodes.length }} 条（附录「业务错误码」表格，0 表示成功）</span>
          </div>
          <div v-for="(c, i) in dl.businessCodes" :key="i" class="dl-rev">
            <el-input v-model="c.code" placeholder="40001" style="width:100px" @change="save" />
            <el-input v-model="c.message" placeholder="参数校验失败" style="width:180px" @change="save" />
            <el-input v-model="c.desc" placeholder="说明 / 排查建议" style="width:330px" @change="save" />
            <el-button link type="danger" @click="removeCode(i)">删除</el-button>
          </div>
        </div>
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
        <span class="dl-hint">用当前结构与水印在浏览器打开，按 Ctrl+P 可看打印分页效果</span>
      </el-form-item>
    </el-form>
  </div>
</template>

<style scoped>
.dl-tip { font-size: 13px; color: #4e5969; margin-bottom: 12px; }
.dl-hint { font-size: 12px; color: #86909c; margin-left: 10px; }
.dl-chapter { width: 100%; display: flex; flex-direction: column; gap: 6px; }
.dl-chapter :deep(.el-input) { align-self: flex-start; }
.dl-md :deep(textarea) { font-family: Consolas, 'Courier New', monospace; font-size: 12.5px; }
.sign-item { display: flex; flex-direction: column; line-height: 1.5; }
.sign-item span { font-size: 11px; color: #86909c; }
.dl-rev { display: flex; gap: 8px; align-items: center; margin-top: 8px; flex-wrap: wrap; }
</style>
