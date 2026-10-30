<script setup>
// 文档模板（企业级排版）：模板库（预置 + 我的模板）＋ 结构表单。
// 独立菜单页，便于团队统一维护接口文档的章节结构与规范模板；支持模板导出/导入 JSON。
import { ref, computed, onMounted, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  DocPresets, DocPresetLayout, SaveDocLayoutTemplate, DeleteDocLayoutTemplate,
  PullDocTemplates, PushDocTemplates, PreviewDocTemplateHTML, PreviewDocHTML,
  RenameDocLayoutTemplate, DuplicateDocLayoutTemplate,
  DefaultDocConvention, DefaultDocGlossary, DefaultDocEnums, DefaultDocBusinessCodes, DocSignTemplates,
} from '../../../wailsjs/go/main/App'
import { store, saveNow, reloadStore, buildDirTree, currentProject, cloudConfigured, cloudSyncEnabled } from '../../store'
import DocLayoutPanel from './DocLayoutPanel.vue'
import DocWatermarkPanel from './DocWatermarkPanel.vue'

const presets = ref([])
const applying = ref('')
const panelRef = ref(null)
const scopeDirId = ref('')      // 预览用的目录范围（空 = 全部接口）
const fileInput = ref(null)
const syncing = ref('')   // '' | 'push' | 'pull'
const cloudReady = computed(() => cloudConfigured())

// 作用范围：global 全局配置（默认，所有项目共用）；project 当前项目覆盖
const scope = ref('global')
const project = computed(() => currentProject())
const projectOverride = computed(() => {
  const id = project.value?.id
  if (!id) return null
  return store.data.settings.projectDocLayouts?.[id] || null
})
const globalLayout = computed(() => store.data.settings.docLayout)
const dl = computed(() => (scope.value === 'project' && projectOverride.value) ? projectOverride.value : globalLayout.value)
// 当前选中的模板名（导出即按它生成文档）
const activeName = computed(() => dl.value?.name || '自定义结构')
const isActive = p => dl.value?.name === p.label
const myTemplates = computed(() => store.data.settings.docLayoutTemplates || [])
const dirTree = computed(() => buildDirTree())

// normalize 补齐字段（后端返回的 null 数组转成可用结构）
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

// 切到「当前项目」时，若项目尚无覆盖，则复制一份全局配置作为起点
watch(scope, async (v) => {
  if (v !== 'project') return
  if (!project.value) { ElMessage.warning('没有可用的项目'); scope.value = 'global'; return }
  if (projectOverride.value) return
  if (!store.data.settings.projectDocLayouts) store.data.settings.projectDocLayouts = {}
  store.data.settings.projectDocLayouts[project.value.id] = JSON.parse(JSON.stringify(globalLayout.value))
  normalize(store.data.settings.projectDocLayouts[project.value.id])
  await saveNow()
})

// 清除项目级覆盖，恢复使用全局配置
async function clearProjectOverride() {
  const id = project.value?.id
  if (!id || !projectOverride.value) return
  if (!window.confirm('清除后该项目将改用「全局配置」的文档结构，确定吗？')) return
  delete store.data.settings.projectDocLayouts[id]
  await saveNow()
  ElMessage.success('已恢复为全局配置')
}

onMounted(async () => {
  normalize(dl.value)
  try { presets.value = (await DocPresets()) || [] } catch { presets.value = [] }
})

// 当前结构的简要摘要，便于一眼看出选用了哪些章节
const summary = computed(() => {
  const v = dl.value
  const parts = []
  if (v.cover?.enabled) parts.push('封面')
  if (v.revisions?.length) parts.push(`修订记录 ${v.revisions.length} 条`)
  if (v.toc) parts.push('目录')
  if (v.numbering) parts.push('编号')
  if (v.prefaces?.length) parts.push(`公共描述 ${v.prefaces.length} 章`)
  if (v.convention) parts.push('统一定义规范')
  if (v.statusCodes) parts.push('附录状态码')
  return parts.length ? parts.join(' · ') : '仅接口正文'
})

// 输出自检：逐项检查「勾选的章节」是否真的有数据（缺数据时导出会输出占位提示）
const checks = computed(() => {
  const v = dl.value || {}
  const proj = project.value || {}
  const envs = proj.environments || []
  const common = proj.common || {}
  const hasBaseURL = envs.some(e => (e.vars || []).some(x =>
    x.enabled && String(x.value || '').trim() && /base_?url|host|domain/i.test(x.key || '')))
  const list = []
  const push = (label, ok, hint) => list.push({ label, ok, hint })

  if (v.cover?.enabled) push('封面', true)
  if (v.revisionsOn) push(`修订记录 ${(v.revisions || []).length} 条`, (v.revisions || []).length > 0, '尚未填写修订记录，导出会输出「暂无修订记录」占位')
  if (v.toc) push('目录', true)
  if (v.quickStart) push('接入指引', true)
  if (v.environment) push('接入地址（环境）', envs.length > 0 && hasBaseURL,
    envs.length === 0 ? '当前项目还没有环境变量，导出会输出占位提示'
      : (!hasBaseURL ? '环境变量里缺少 baseUrl / host / domain' : ''))
  if (v.commonHeaders) push('公共请求头', (common.headers || []).length + (common.query || []).length > 0,
    '当前项目未配置「公共参数」，导出会输出占位提示')
  if (v.signOn) push('加密与验签', String(v.sign || '').trim() !== '', '未填写验签内容，导出会输出占位提示')
  if (v.glossaryOn) push(`术语表 ${(v.glossary || []).length} 条`, (v.glossary || []).length > 0, '尚未填写术语表，导出会输出占位提示')
  if (v.conventionOn) push('统一定义规范', String(v.convention || '').trim() !== '', '未填写规范内容，导出会输出占位提示')
  if (v.businessCodesOn) push(`业务错误码 ${(v.businessCodes || []).length} 条`, (v.businessCodes || []).length > 0, '尚未维护业务错误码，导出会输出占位提示')
  if (v.enumsOn) push(`数据字典 ${(v.enums || []).length} 条`, (v.enums || []).length > 0, '尚未维护数据字典，导出会输出占位提示')
  if (v.support?.enabled) push('技术支持与联系', true)
  return list
})

// ---------------- 模板预览（弹窗内嵌，只渲染章节结构，不含具体接口）----------------
const previewVisible = ref(false)
const previewLoading = ref(false)
const previewTitle = ref('模板预览')
const previewHTML = ref('')
const previewSrc = ref('')   // Blob URL：比 srcdoc 更稳，且支持 #anchor 跳转
const previewing = ref('')
const previewMode = ref('template') // template | full
const buildStamp = typeof __BUILD_STAMP__ !== 'undefined' ? __BUILD_STAMP__ : '未知'
let previewSeq = 0
const previewKey = ref(0)   // 每次预览换 key，强制 iframe 重新挂载

// setPreviewDoc 把预览 HTML 转成 Blob URL（超大文档用 srcdoc 容易导致渲染卡死）
function setPreviewDoc(html) {
  if (previewSrc.value) {
    URL.revokeObjectURL(previewSrc.value)
    previewSrc.value = ''
  }
  if (!html) return
  previewSrc.value = URL.createObjectURL(new Blob([html], { type: 'text/html;charset=utf-8' }))
}

// withTimeout 给预览 RPC 加超时，避免界面一直停在「正在加载」
function withTimeout(promise, ms = 20000, tip = '加载超时') {
  return Promise.race([
    promise,
    new Promise((_, reject) => setTimeout(() => reject(new Error(tip)), ms)),
  ])
}

// openPreview 拉取「模板结构」预览 HTML 并在弹窗内用 iframe 展示（不含具体接口）
async function openPreview(layout, title) {
  previewKey.value = ++previewSeq
  previewMode.value = 'template'
  previewTitle.value = `模板预览 · ${title}（不含具体接口）`
  previewHTML.value = ''
  previewVisible.value = true
  previewLoading.value = true
  try {
    const html = await withTimeout(
      PreviewDocTemplateHTML(JSON.stringify({ ...layout, name: title })),
      20000, '生成模板预览超时，请减少章节内容后重试')
    previewHTML.value = html || ''
    setPreviewDoc(html)
    if (!html) ElMessage.warning('预览内容为空')
  } catch (e) {
    ElMessage.error('预览失败：' + String(e))
    previewVisible.value = false
  } finally {
    previewLoading.value = false
  }
}

// openFullPreview 弹窗预览「整份文档」：含所选范围内的全部接口（按当前模板与水印）
function closePreview() {
  previewVisible.value = false
  previewLoading.value = false
  previewHTML.value = ''
  setPreviewDoc('')
}

async function previewFullDoc() {
  if (previewing.value) return
  previewKey.value = ++previewSeq
  previewing.value = 'full'
  previewMode.value = 'full'
  previewTitle.value = '预览整份文档（含接口）'
  previewHTML.value = ''
  previewVisible.value = true
  previewLoading.value = true
  try {
    // 结构以「当前编辑中的」为准直接传给后端；落库异步进行，避免大项目保存阻塞预览
    saveNow().catch(() => {})
    const html = await withTimeout(
      PreviewDocHTML(scopeDirId.value || '', '', JSON.stringify(dl.value)),
      30000, '生成文档预览超时：接口数量较多时可先选一个较小的目录范围再预览')
    previewHTML.value = html || ''
    setPreviewDoc(html)
    if (!html) ElMessage.warning('预览内容为空（所选范围内可能没有接口）')
  } catch (e) {
    ElMessage.error('预览失败：' + String(e))
    previewVisible.value = false
  } finally {
    previewLoading.value = false
    previewing.value = ''
  }
}

async function previewPreset(p) {
  if (previewing.value) return
  previewing.value = p.key
  try {
    const layout = await DocPresetLayout(p.key)
    await openPreview(layout, p.label)
  } catch (e) {
    ElMessage.error('预览失败：' + String(e))
  } finally {
    previewing.value = ''
  }
}

async function previewTemplate(tpl) {
  if (previewing.value) return
  previewing.value = tpl.id
  try {
    await openPreview(tpl.layout, tpl.name)
  } catch (e) {
    ElMessage.error('预览失败：' + String(e))
  } finally {
    previewing.value = ''
  }
}

async function previewCurrent() {
  if (previewing.value) return
  previewing.value = 'current'
  try {
    await openPreview(dl.value, activeName.value)
  } catch (e) {
    ElMessage.error('预览失败：' + String(e))
  } finally {
    previewing.value = ''
  }
}


// chipsOf 生成模板的章节摘要（与预置模板卡片一致）
function chipsOf(l) {
  const v = l || {}
  const c = []
  if (v.cover?.enabled) c.push('封面')
  if (v.revisionsOn && v.revisions?.length) c.push(`修订记录 ${v.revisions.length}`)
  if (v.toc) c.push('目录')
  if (v.quickStart) c.push('接入指引')
  if (v.environment) c.push('接入地址')
  if (v.commonHeaders) c.push('公共请求头')
  if (v.signOn && v.sign) c.push('加密验签')
  if (v.glossaryOn && v.glossary?.length) c.push(`术语表 ${v.glossary.length}`)
  if (v.conventionOn && v.convention) c.push('统一规范')
  if (v.statusCodes) c.push('状态码')
  if (v.businessCodesOn && v.businessCodes?.length) c.push(`错误码 ${v.businessCodes.length}`)
  if (v.enumsOn && v.enums?.length) c.push(`数据字典 ${v.enums.length}`)
  if (v.support?.enabled) c.push('技术支持')
  return c
}

// ---------------- 自定义模板：新建 / 重命名 / 复制 / 自动回写 ----------------
// 新建模板：可复制当前结构或从空白开始，创建后立即设为当前模板
// ---------------- 新建模板：名称 + 结构勾选 ----------------
// 可勾选的章节定义（apply 负责把该章节的开关/默认内容写进 layout）
// 章节选项：type = flag（仅开关）/ text（可编辑文本）/ list（可维护列表）
// fillDefault 提供内置默认内容；clear 清空内容并关闭开关
const chapterOptions = [
  { key: 'cover', label: '作者封面', type: 'flag', desc: '标题 / 副标题 / 作者 / 单位 / 版本 / 日期，单独一页',
    apply: l => { l.cover = l.cover || {}; l.cover.enabled = true },
    clear: l => { if (l.cover) l.cover.enabled = false } },
  { key: 'revisions', label: '文档修订记录', type: 'list', desc: '版本 / 日期 / 作者 / 修订说明',
    count: l => (l.revisions || []).length,
    fillDefault: l => { l.revisions = [{ version: 'V1.0.0', date: todayStr(), author: '', note: '首次发布' }]; l.revisionsOn = true },
    clear: l => { l.revisionsOn = false } },
  { key: 'toc', label: '目录', type: 'flag', desc: 'Word 为可更新域，PDF/HTML 为带锚点目录',
    apply: l => { l.toc = true }, clear: l => { l.toc = false } },
  { key: 'numbering', label: '章节编号', type: 'flag', desc: '1 / 1.1 / 1.1.1，三种格式一致',
    apply: l => { l.numbering = true }, clear: l => { l.numbering = false } },
  { key: 'quickStart', label: '接入指引（快速开始）', type: 'flag', desc: '申请凭证 → 选环境 → 公共头 → 签名 → 调用与排错',
    apply: l => { l.quickStart = true }, clear: l => { l.quickStart = false } },
  { key: 'environment', label: '接入地址（环境）', type: 'flag', desc: '按项目环境变量生成测试/预发/生产地址表',
    apply: l => { l.environment = true }, clear: l => { l.environment = false } },
  { key: 'commonHeaders', label: '公共请求头与公共参数', type: 'flag', desc: '按项目「公共参数」生成表格',
    apply: l => { l.commonHeaders = true }, clear: l => { l.commonHeaders = false } },
  { key: 'sign', label: '加密与验签', type: 'text', field: 'sign', flagField: 'signOn',
    desc: '可编辑签名规则 / 示例代码，支持一键填入内置验签方案',
    fillDefault: async l => {
      if (!String(l.sign || '').trim()) {
        const list = await DocSignTemplates().catch(() => [])
        l.sign = list?.[0]?.content || ''
      }
      l.signOn = true
    },
    clear: l => { l.signOn = false } },
  { key: 'glossary', label: '术语表', type: 'list', desc: '术语解释，可一键填入通用术语后再增删改',
    count: l => (l.glossary || []).length,
    fillDefault: async l => { l.glossary = await DefaultDocGlossary(); l.glossaryOn = true },
    clear: l => { l.glossaryOn = false } },
  { key: 'convention', label: '接口统一定义规范', type: 'text', field: 'convention', flagField: 'conventionOn',
    desc: '编写或一键填入默认团队规范',
    fillDefault: async l => {
      if (!String(l.convention || '').trim()) l.convention = await DefaultDocConvention()
      l.conventionOn = true
    },
    clear: l => { l.conventionOn = false } },
  { key: 'statusCodes', label: '附录：HTTP 状态码', type: 'flag', desc: '常用状态码名称 / 含义 / 场景',
    apply: l => { l.statusCodes = true }, clear: l => { l.statusCodes = false } },
  { key: 'businessCodes', label: '附录：业务错误码', type: 'list', desc: '业务码 / 含义 / 排查建议，可一键填入默认表',
    count: l => (l.businessCodes || []).length,
    fillDefault: async l => { l.businessCodes = await DefaultDocBusinessCodes(); l.businessCodesOn = true },
    clear: l => { l.businessCodesOn = false } },
  { key: 'enums', label: '附录：数据字典', type: 'list', desc: '字段 / 枚举值 / 含义，可一键填入默认枚举',
    count: l => (l.enums || []).length,
    fillDefault: async l => { l.enums = await DefaultDocEnums(); l.enumsOn = true },
    clear: l => { l.enumsOn = false } },
  { key: 'support', label: '技术支持与联系', type: 'flag', desc: '对接人 / 邮箱 / 群 / 工单 / 响应时效（创建后在表单填写）',
    apply: l => { l.support = l.support || {}; l.support.enabled = true },
    clear: l => { if (l.support) l.support.enabled = false } },
  { key: 'apis', label: '接口正文（真实接口）', type: 'flag', locked: true,
    desc: '所选范围内的全部真实接口：请求/响应参数、请求与响应示例（必选，可拖动调整位置）',
    apply: () => {}, clear: () => {} },
]

const newVisible = ref(false)
const newBusy = ref(false)
const newName = ref('')
const newBase = ref('blank')     // blank | current
const newChapters = ref(['toc', 'numbering', 'apis'])
const newDraft = ref({})         // 正在编辑的模板草稿（含各章节内容）
const newCleared = ref([])       // 用户显式「清空内容」的章节，创建时不再自动补默认值
const newOrder = ref(chapterOptions.map(c => c.key))   // 拖拽后的展示顺序

function todayStr() {
  return new Date().toISOString().slice(0, 10)
}

// 拖拽排序（原生 HTML5 DnD，无需额外依赖）
const dragKey = ref('')
function onDragStart(key) { dragKey.value = key }
function onDrop(targetKey) {
  const from = dragKey.value
  dragKey.value = ''
  if (!from || from === targetKey) return
  const list = newOrder.value.slice()
  const i = list.indexOf(from), j = list.indexOf(targetKey)
  if (i < 0 || j < 0) return
  list.splice(i, 1)
  list.splice(j, 0, from)
  newOrder.value = list
}
const orderedOptions = computed(() => newOrder.value.map(k => chapterOptions.find(c => c.key === k)).filter(Boolean))

// 勾选 / 取消：勾上即打开该章节（内容为空时补默认），取消则关闭开关
async function toggleChapter(opt, on) {
  if (opt.locked) return
  const l = newDraft.value
  if (on) {
    newCleared.value = newCleared.value.filter(k => k !== opt.key)
    if (!newChapters.value.includes(opt.key)) newChapters.value.push(opt.key)
    if (opt.fillDefault) await opt.fillDefault(l)
    else if (opt.apply) opt.apply(l)
  } else {
    newChapters.value = newChapters.value.filter(k => k !== opt.key)
    if (opt.clear) opt.clear(l)
  }
}

// 单章「填入默认」/「清空内容」
async function fillChapter(opt) {
  try {
    if (opt.fillDefault) await opt.fillDefault(newDraft.value)
    else if (opt.apply) opt.apply(newDraft.value)
    ElMessage.success('已填入默认内容')
  } catch (e) { ElMessage.error(String(e)) }
}
function clearChapter(opt) {
  if (!newCleared.value.includes(opt.key)) newCleared.value.push(opt.key)
  if (opt.fillDefault) {
    if (opt.key === 'glossary') newDraft.value.glossary = []
    if (opt.key === 'businessCodes') newDraft.value.businessCodes = []
    if (opt.key === 'enums') newDraft.value.enums = []
    if (opt.key === 'revisions') newDraft.value.revisions = []
    if (opt.key === 'sign') newDraft.value.sign = ''
    if (opt.key === 'convention') newDraft.value.convention = ''
  }
  if (opt.clear) opt.clear(newDraft.value)
  ElMessage.success('已清空该章节内容')
}

// 按当前 layout 的开关状态反推勾选项（用于「复制当前结构」）
function chaptersOf(l) {
  const v = l || {}
  const on = []
  if (v.cover?.enabled) on.push('cover')
  if (v.revisionsOn && (v.revisions || []).length) on.push('revisions')
  if (v.toc) on.push('toc')
  if (v.numbering) on.push('numbering')
  if (v.quickStart) on.push('quickStart')
  if (v.environment) on.push('environment')
  if (v.commonHeaders) on.push('commonHeaders')
  if (v.signOn && v.sign) on.push('sign')
  if (v.glossaryOn && (v.glossary || []).length) on.push('glossary')
  if (v.conventionOn && v.convention) on.push('convention')
  if (v.statusCodes) on.push('statusCodes')
  if (v.businessCodesOn && (v.businessCodes || []).length) on.push('businessCodes')
  if (v.enumsOn && (v.enums || []).length) on.push('enums')
  if (v.support?.enabled) on.push('support')
  on.push('apis')
  return on
}

// 用「起点 + 当前勾选」重新构建草稿
function resetDraft() {
  newCleared.value = []
  const base = newBase.value === 'current' ? dl.value : { toc: true, numbering: true }
  newDraft.value = normalize(JSON.parse(JSON.stringify(base)))
  newChapters.value = newBase.value === 'current' ? chaptersOf(dl.value) : ['toc', 'numbering', 'apis']
  // 按库里的 sectionOrder 还原拖拽顺序（没有则默认）
  const saved = Array.isArray(dl.value?.sectionOrder) ? dl.value.sectionOrder : []
  const order = []
  for (const k of saved) if (chapterOptions.some(c => c.key === k)) order.push(k)
  for (const c of chapterOptions) if (!order.includes(c.key)) order.push(c.key)
  newOrder.value = order
}

function openNewTemplate() {
  newName.value = ''
  newBase.value = 'blank'
  resetDraft()
  newVisible.value = true
}

watch(newBase, () => resetDraft())

function toggleAllChapters(on) {
  newChapters.value = on
    ? chapterOptions.map(c => c.key)
    : ['apis']  // 接口正文必选
}

async function createTemplate() {
  const name = String(newName.value || '').trim()
  if (!name) { ElMessage.warning('请输入模板名称'); return }
  if (myTemplates.value.some(t => t.name === name)) { ElMessage.warning('已存在同名模板，请换个名称'); return }
  if (!newChapters.value.length) { ElMessage.warning('请至少勾选一个章节'); return }

  newBusy.value = true
  try {
    const layout = newDraft.value
    // 草稿可能只改了一部分内容，这里确保勾选项都有内容（空则补默认）
    for (const opt of chapterOptions) {
      if (!newChapters.value.includes(opt.key)) {
        if (opt.clear) opt.clear(layout)
        continue
      }
      if (opt.fillDefault) {
        const isEmpty = opt.count ? opt.count(layout) === 0 : !String(layout[opt.field] || '').trim()
        if (isEmpty && !newCleared.value.includes(opt.key)) await opt.fillDefault(layout)
        else if (opt.flagField) layout[opt.flagField] = true
      } else if (opt.apply) {
        opt.apply(layout)
      }
    }
    // 章节顺序：按拖拽顺序写入（numbering 不是章节，不参与）
    layout.sectionOrder = newOrder.value.filter(k => newChapters.value.includes(k) && k !== 'numbering')
    layout.name = name

    await SaveDocLayoutTemplate(name, JSON.stringify(layout))
    await reloadStore()
    normalize(dl.value)
    await applyLayout(layout, name, true)
    newVisible.value = false
    ElMessage.success('已创建模板：' + name)
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    newBusy.value = false
  }
}

async function renameTemplate(tpl) {
  let name = ''
  try {
    const r = await ElMessageBox.prompt('新的模板名称', '重命名模板', { inputValue: tpl.name })
    name = (r.value || '').trim()
  } catch { return }
  if (!name || name === tpl.name) return
  try {
    await RenameDocLayoutTemplate(tpl.id, name)
    if (dl.value?.name === tpl.name) {
      dl.value.name = name
      await saveNow()
    }
    await reloadStore()
    normalize(dl.value)
    ElMessage.success('已重命名为：' + name)
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function duplicateTemplate(tpl) {
  try {
    const copy = await DuplicateDocLayoutTemplate(tpl.id)
    await reloadStore()
    normalize(dl.value)
    ElMessage.success('已复制为：' + copy.name)
  } catch (e) {
    ElMessage.error(String(e))
  }
}

// 当前模板是自定义模板时，编辑结构自动回写模板（防抖 + 快照去重，避免写库风暴）
let syncTimer = null
let lastSynced = ''
function scheduleTemplateSync() {
  const name = dl.value?.name
  if (!name) return
  if (!myTemplates.value.some(t => t.name === name)) return
  const snapshot = JSON.stringify(dl.value)
  if (snapshot === lastSynced) return
  clearTimeout(syncTimer)
  syncTimer = setTimeout(async () => {
    try {
      const cur = JSON.stringify(dl.value)
      if (cur === lastSynced) return
      await SaveDocLayoutTemplate(name, cur)
      lastSynced = cur
      await reloadStore()
      normalize(dl.value)
    } catch (e) {
      console.warn('模板自动保存失败', e)
    }
  }, 1000)
}
watch(() => JSON.stringify(dl.value), scheduleTemplateSync)

// 把当前全局结构同步到「当前项目覆盖」，避免导出/分享继续用旧的项目级结构
async function syncToProject() {
  const pid = project.value?.id
  if (!pid) { ElMessage.warning('没有可用的项目'); return }
  if (!store.data.settings.projectDocLayouts) store.data.settings.projectDocLayouts = {}
  store.data.settings.projectDocLayouts[pid] = JSON.parse(JSON.stringify(globalLayout.value))
  normalize(store.data.settings.projectDocLayouts[pid])
  await saveNow()
  scope.value = 'project'
  ElMessage.success('已把当前全局结构同步为「' + project.value.name + '」的项目级结构')
}

// 套用模板（预置或自定义），覆盖当前结构
async function applyLayout(layout, name, silent = false) {
  if (!silent && !window.confirm(`套用「${name}」会覆盖当前文档结构配置，确定继续吗？`)) return
  try {
    const next = normalize(JSON.parse(JSON.stringify(layout)))
    next.name = name
    Object.assign(dl.value, next)
    await saveNow()
    ElMessage.success('已套用模板：' + name)
    // 当前项目存在覆盖时，导出/分享会优先用它 —— 主动提示同步，避免「选了模板却不生效」
    if (scope.value === 'global' && projectOverride.value) {
      try {
        await ElMessageBox.confirm(
          `当前项目「${project.value?.name || ''}」存在项目级覆盖，导出与分享会优先使用它，` +
          `所以刚套用的模板不会生效。是否把新结构同步为该项目覆盖？`,
          '项目级覆盖优先', { confirmButtonText: '同步到当前项目', cancelButtonText: '暂不', type: 'warning' })
        await syncToProject()
      } catch { /* 用户选择暂不同步 */ }
    }
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function applyPreset(p) {
  if (applying.value) return
  applying.value = p.key
  try {
    await applyLayout(await DocPresetLayout(p.key), p.label)
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    applying.value = ''
  }
}

async function applyTemplate(tpl) {
  await applyLayout(tpl.layout, tpl.name)
}

// 保存当前结构为命名模板（同名覆盖）
async function saveAsTemplate() {
  let name = ''
  try {
    const r = await ElMessageBox.prompt('模板名称（如「对外版 v2」「评审版」）', '保存为自定义模板', {
      inputPlaceholder: '模板名称', inputValidator: v => (v && v.trim() ? true : '请输入模板名称'),
    })
    name = (r.value || '').trim()
  } catch { return }
  try {
    await SaveDocLayoutTemplate(name, JSON.stringify(dl.value))
    await reloadStore()
    normalize(dl.value)
    ElMessage.success('已保存模板：' + name)
    autoPushIfEnabled()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function removeTemplate(tpl) {
  if (!window.confirm(`删除模板「${tpl.name}」？`)) return
  try {
    await DeleteDocLayoutTemplate(tpl.id)
    await reloadStore()
    normalize(dl.value)
    ElMessage.success('已删除模板：' + tpl.name)
    autoPushIfEnabled()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

// ---------------- 云同步（团队模板库） ----------------
// 上传：用本地模板库覆盖云端；拉取：云端同名模板覆盖本地，并保留本地独有项
async function pushCloud(silent = false) {
  if (syncing.value) return
  syncing.value = 'push'
  try {
    const r = await PushDocTemplates()
    if (!silent) ElMessage.success(`已上传 ${r.changed} 套模板到云端`)
  } catch (e) {
    if (!silent) ElMessage.error('上传失败：' + String(e))
    else console.warn('模板库自动上传失败', e)
  } finally {
    syncing.value = ''
  }
}

async function pullCloud() {
  if (syncing.value) return
  syncing.value = 'pull'
  try {
    const r = await PullDocTemplates()
    await reloadStore()
    normalize(dl.value)
    ElMessage.success(r.changed ? `已从云端拉取，合并后共 ${r.total} 套（更新 ${r.changed} 套）` : '云端模板与本地一致')
  } catch (e) {
    ElMessage.error('拉取失败：' + String(e))
  } finally {
    syncing.value = ''
  }
}

// 保存 / 删除后，若开启了自动同步则顺手把模板库推到云端
function autoPushIfEnabled() {
  if (cloudSyncEnabled()) pushCloud(true)
}

// ---------------- 模板导出 / 导入（JSON，便于团队共享） ----------------
function exportTemplates() {
  const payload = {
    kind: 'apitool-doc-templates',
    version: 1,
    exportedAt: new Date().toISOString(),
    templates: [
      { name: `当前结构-${new Date().toISOString().slice(0, 10)}`, layout: dl.value },
      ...myTemplates.value.map(t => ({ name: t.name, layout: t.layout })),
    ],
  }
  const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `doc-templates-${new Date().toISOString().slice(0, 10)}.json`
  a.click()
  URL.revokeObjectURL(url)
  ElMessage.success(`已导出 ${payload.templates.length} 套模板`)
}

function pickImport() {
  fileInput.value?.click()
}

// parseTemplates 兼容三种文件形态：{templates:[...]} / [...] / 单个 layout 对象
function parseTemplates(data) {
  const pick = arr => arr.filter(x => x && x.layout).map(x => ({ name: String(x.name || '导入模板'), layout: x.layout }))
  if (Array.isArray(data)) return pick(data)
  if (data && Array.isArray(data.templates)) return pick(data.templates)
  if (data && (data.toc !== undefined || data.cover !== undefined || data.convention !== undefined)) {
    return [{ name: '导入模板', layout: data }]
  }
  throw new Error('无法识别的模板文件（应为导出的 doc-templates-*.json）')
}

async function onImportFile(e) {
  const file = e.target.files?.[0]
  if (!file) return
  try {
    const items = parseTemplates(JSON.parse(await file.text()))
    if (!items.length) throw new Error('文件中没有可导入的模板')
    for (const it of items) {
      await SaveDocLayoutTemplate(it.name, JSON.stringify(it.layout))
    }
    await reloadStore()
    normalize(dl.value)
    ElMessage.success(`已导入 ${items.length} 套模板`)
  } catch (err) {
    ElMessage.error('导入失败：' + String(err && err.message ? err.message : err))
  } finally {
    e.target.value = ''
  }
}

function preview() {
  panelRef.value?.preview?.()
}
</script>

<template>
  <div class="main-area">
    <div class="panel-page" style="max-width:960px; margin:0 auto; width:100%">
      <h2 style="margin:6px 0 8px">📐 文档模板</h2>
      <div class="tpl-lead">
        统一维护导出文档的章节结构（封面 / 修订记录 / 目录 / 公共描述 / 统一定义规范 / 附录）与规范模板，
        文档中心导出 Word / PDF / HTML / Markdown 时都会套用这里的配置。
        <el-alert v-if="scope === 'global' && projectOverride" type="warning" :closable="false" show-icon
          style="margin-bottom:10px"
          title="当前项目存在「项目级覆盖」：导出 / 分享 / 预览会优先使用它，全局配置的修改不会生效">
          <div style="font-size:12.5px; line-height:1.8">
            当前项目：<b>{{ project?.name || '未选择' }}</b>　生效结构：
            <b>{{ projectOverride.name || '自定义结构（无名称）' }}</b>
            <el-button size="small" type="primary" plain style="margin-left:10px" @click="syncToProject">
              用全局结构覆盖本项目
            </el-button>
            <el-button size="small" @click="clearProjectOverride">清除项目覆盖（改用全局）</el-button>
            <el-button size="small" @click="scope = 'project'">切换到项目范围编辑</el-button>
          </div>
        </el-alert>

        <div class="tpl-scope">
          <span class="tpl-scope-label">配置作用范围：</span>
          <el-radio-group v-model="scope" size="small">
            <el-radio-button value="global">全局配置（所有项目）</el-radio-button>
            <el-radio-button value="project">仅当前项目（{{ project?.name || '无项目' }}）</el-radio-button>
          </el-radio-group>
          <el-button v-if="scope === 'project' && projectOverride" size="small" link type="primary"
            style="margin-left:8px" @click="clearProjectOverride">清除项目覆盖，恢复全局</el-button>
        </div>
        <div class="tpl-current">
          当前使用模板：<b>{{ activeName }}</b>　结构：<b>{{ summary }}</b>
          （{{ scope === 'project' ? '项目级覆盖' : '全局配置' }}）
        </div>
        <div class="tpl-check">
          <span class="tpl-check-label">导出将输出：</span>
          <el-tooltip v-for="(c, i) in checks" :key="i" :content="c.hint || '配置完整'" placement="top">
            <el-tag size="small" :type="c.ok ? 'success' : 'warning'" effect="plain" class="tpl-check-tag">
              {{ c.ok ? '✓' : '⚠' }} {{ c.label }}
            </el-tag>
          </el-tooltip>
          <span v-if="!checks.length" class="tpl-empty">暂无章节，先套用一个模板或勾选章节</span>
        </div>
      </div>

      <div class="card">
        <div class="card-title">
          模板库（一键套用）
          <span class="tpl-ops">
            <el-button size="small" @click="exportTemplates">⬇ 导出模板</el-button>
            <el-button size="small" @click="pickImport">⬆ 导入模板</el-button>
            <input ref="fileInput" type="file" accept="application/json,.json" hidden @change="onImportFile" />
          </span>
        </div>
        <div class="tpl-grid">
          <div v-for="p in presets" :key="p.key" class="tpl-card"
            :class="{ busy: applying === p.key, active: isActive(p) }" @click="applyPreset(p)">
            <div class="tpl-head">
              <span class="tpl-name">{{ p.label }}</span>
              <el-tag size="small" effect="plain" type="info">{{ p.tag }}</el-tag>
              <el-tag v-if="isActive(p)" size="small" type="success" effect="dark">使用中</el-tag>
            </div>
            <div class="tpl-desc">{{ p.desc }}</div>
            <div class="tpl-chips">
              <el-tag v-for="c in (p.chips || [])" :key="c" size="small" effect="plain" class="tpl-chip">{{ c }}</el-tag>
            </div>
            <div class="tpl-card-ops">
              <el-button link size="small" type="primary" :loading="previewing === p.key"
                @click.stop="previewPreset(p)">👁 预览版式</el-button>
            </div>
          </div>
        </div>
      </div>

      <div class="card">
        <div class="card-title">
          我的模板（{{ myTemplates.length }}）
          <span class="tpl-ops">
            <el-tooltip :content="cloudReady ? '用本地模板库覆盖云端模板库' : '请先在「设置 → 云同步」配置服务器地址并登录'">
              <span>
                <el-button size="small" :disabled="!cloudReady || !myTemplates.length" :loading="syncing === 'push'"
                  @click="pushCloud()">☁ 上传到云端</el-button>
              </span>
            </el-tooltip>
            <el-tooltip :content="cloudReady ? '拉取云端模板并与本地合并（同名以云端为准）' : '请先在「设置 → 云同步」配置服务器地址并登录'">
              <span>
                <el-button size="small" :disabled="!cloudReady" :loading="syncing === 'pull'"
                  @click="pullCloud">☁ 从云端拉取</el-button>
              </span>
            </el-tooltip>
          </span>
        </div>
        <div style="margin-bottom:10px">
          <el-button size="small" type="primary" plain @click="openNewTemplate">＋ 新建模板</el-button>
          <el-button size="small" @click="saveAsTemplate">把当前结构保存为模板</el-button>
        </div>
        <div v-if="myTemplates.length" class="tpl-grid">
          <div v-for="tpl in myTemplates" :key="tpl.id" class="tpl-card" :class="{ active: dl.name === tpl.name }"
            @click="applyTemplate(tpl)">
            <div class="tpl-head">
              <span class="tpl-name">{{ tpl.name }}</span>
              <el-tag v-if="dl.name === tpl.name" size="small" type="success" effect="dark">使用中</el-tag>
            </div>
            <div class="tpl-chips">
              <el-tag v-for="c in chipsOf(tpl.layout)" :key="c" size="small" effect="plain" class="tpl-chip">{{ c }}</el-tag>
            </div>
            <div class="tpl-card-ops">
              <el-button link size="small" type="primary" :loading="previewing === tpl.id"
                @click.stop="previewTemplate(tpl)">预览</el-button>
              <el-button link size="small" @click.stop="renameTemplate(tpl)">重命名</el-button>
              <el-button link size="small" @click.stop="duplicateTemplate(tpl)">复制</el-button>
              <el-button link size="small" type="danger" @click.stop="removeTemplate(tpl)">删除</el-button>
            </div>
          </div>
        </div>
        <div v-else class="tpl-empty">
          还没有自定义模板：点「＋ 新建模板」，按自己的规范设计一套（章节、开关、术语表、错误码…全部可选），
          或把当前结构「保存为模板」。
        </div>
        <div class="tpl-hint">
          最多 20 套；编辑「使用中」的自定义模板会自动回写该模板。点击卡片套用，支持预览 / 重命名 / 复制 / 删除。
          <span v-if="cloudReady && cloudSyncEnabled()">已开启自动同步：保存 / 删除模板后会自动上传云端。</span>
        </div>
      </div>

      <DocLayoutPanel ref="panelRef" :layout="dl" :dir-id="scopeDirId" @preview-full="previewFullDoc" />

      <DocWatermarkPanel :dir-id="scopeDirId" />

      <el-dialog v-model="newVisible" title="新建文档模板" width="720px" top="6vh">
        <el-form label-width="86px" label-position="left">
          <el-form-item label="模板名称">
            <el-input v-model="newName" placeholder="如：xx 事业部标准 / 内部评审版" style="width:320px" maxlength="40" show-word-limit />
          </el-form-item>
          <el-form-item label="结构起点">
            <el-radio-group v-model="newBase">
              <el-radio-button value="blank">空白模板</el-radio-button>
              <el-radio-button value="current">复制当前结构</el-radio-button>
            </el-radio-group>
            <span class="tpl-hint" style="margin-left:8px">
              {{ newBase === 'current' ? '沿用当前模板的文字/表格内容，勾选决定最终输出哪些章节' : '内容为空，勾选的章节会自动填入默认内容' }}
            </span>
          </el-form-item>
          <el-form-item label="包含章节">
            <div style="width:100%">
              <div style="margin-bottom:6px">
                <el-button size="small" link type="primary" @click="toggleAllChapters(true)">全选</el-button>
                <el-button size="small" link @click="toggleAllChapters(false)">清空</el-button>
                <span class="tpl-hint">已选 {{ newChapters.length }} / {{ chapterOptions.length }} 项；<b>可拖动左侧手柄调整章节顺序</b></span>
              </div>
              <div class="chapter-list">
                <div v-for="opt in orderedOptions" :key="opt.key" class="chapter-row"
                  :class="{ dragging: dragKey === opt.key, off: !newChapters.includes(opt.key) }"
                  draggable="true" @dragstart="onDragStart(opt.key)" @dragover.prevent @drop.prevent="onDrop(opt.key)">
                  <span class="drag-handle" title="拖动调整顺序">⋮⋮</span>
                  <el-checkbox :model-value="newChapters.includes(opt.key)" :disabled="opt.locked"
                    @change="v => toggleChapter(opt, v)" />
                  <div class="chapter-main">
                    <div class="nc-label">
                      {{ opt.label }}
                      <el-tag v-if="opt.locked" size="small" type="info" effect="plain">必选</el-tag>
                    </div>
                    <div class="nc-desc">{{ opt.desc }}</div>

                    <template v-if="newChapters.includes(opt.key)">
                      <el-input v-if="opt.type === 'text'" v-model="newDraft[opt.field]" type="textarea" :rows="4"
                        class="chapter-md" placeholder="支持简化 Markdown：#### 小标题 / - 列表 / 表格 / 代码块" />
                      <div v-if="opt.type === 'text' || opt.type === 'list'" class="chapter-actions">
                        <span v-if="opt.type === 'list'" class="tpl-hint">当前 {{ opt.count(newDraft) }} 条</span>
                        <el-button link size="small" type="primary" @click="fillChapter(opt)">填入默认内容</el-button>
                        <el-button v-if="opt.type === 'list' || opt.type === 'text'" link size="small"
                          @click="clearChapter(opt)">清空内容</el-button>
                        <span class="tpl-hint">更细的编辑（表格行）可在创建后的结构表单里维护</span>
                      </div>
                    </template>
                  </div>
                </div>
              </div>
            </div>
          </el-form-item>
        </el-form>
        <template #footer>
          <el-button @click="newVisible = false">取消</el-button>
          <el-button type="primary" :loading="newBusy" @click="createTemplate">创建并使用</el-button>
        </template>
      </el-dialog>

      <el-dialog v-model="previewVisible" :title="previewTitle" width="1080px" top="4vh" destroy-on-close>
        <div v-loading="previewLoading && !previewHTML" element-loading-text="正在生成预览…" class="preview-wrap">
          <iframe v-if="previewSrc" :key="previewKey" class="preview-frame" :src="previewSrc" />
        </div>
        <template #footer>
          <span class="tpl-hint" style="margin-right:auto">
            {{ previewMode === 'full'
              ? '预览包含所选范围内的全部接口，与导出的 HTML / PDF 版式一致'
              : '预览只渲染章节结构，不含具体接口；实际导出会输出所选范围内的全部接口' }}
          </span>
          <el-button type="primary" @click="closePreview">关闭</el-button>
        </template>
      </el-dialog>

      <div class="tpl-actions">
        <el-tree-select v-model="scopeDirId" :data="dirTree" check-strictly :render-after-expand="false"
          style="width:260px" placeholder="预览范围（默认全部接口）" default-expand-all clearable />
        <el-button plain style="margin-left:10px" :loading="previewing === 'current'" @click="previewCurrent">👁 预览当前模板（不含接口）</el-button>
        <el-button type="primary" plain style="margin-left:8px" :loading="previewing === 'full'" @click="previewFullDoc">👁 预览整份文档（含接口）</el-button>
        <span class="tpl-hint" style="margin-left:10px">弹窗内按当前结构与水印渲染（含所选范围的接口）</span>
      </div>

      <div class="build-stamp">前端构建：{{ buildStamp }}（若时间戳不是最新的，说明运行的是旧构建，请重新 wails build）</div>
    </div>
  </div>
</template>

<style scoped>
.tpl-lead { font-size: 13px; color: #4e5969; line-height: 1.9; margin-bottom: 14px; }
.tpl-scope { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin: 10px 0 6px; }
.tpl-scope-label { font-size: 13px; color: #4e5969; }
.tpl-current { color: #165dff; margin-top: 4px; }
.tpl-check { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; margin-top: 8px; }
.tpl-check-label { font-size: 12px; color: #86909c; }
.tpl-check-tag { cursor: default; }
.tpl-ops { float: right; }
.tpl-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(230px, 1fr)); gap: 12px; }
.tpl-card { border: 1px solid #e5e6eb; border-radius: 10px; padding: 14px; cursor: pointer; transition: all .15s; background: #fff; }
.tpl-card:hover { border-color: #165dff; box-shadow: 0 2px 10px rgba(22, 93, 255, .12); }
.tpl-card.busy { opacity: .5; pointer-events: none; }
.tpl-card.active { border-color: #00b42a; box-shadow: 0 0 0 1px #00b42a inset; }
.tpl-head { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
.tpl-name { font-weight: 600; font-size: 14px; }
.tpl-desc { font-size: 12px; color: #86909c; line-height: 1.7; }
.tpl-chips { margin-top: 10px; display: flex; gap: 6px; flex-wrap: wrap; }
.tpl-card-ops { margin-top: 8px; text-align: right; }
.tpl-mine-item { display: inline-flex; align-items: center; gap: 4px; }
.chapter-list { width: 100%; max-height: 46vh; overflow: auto; border: 1px solid #e5e6eb; border-radius: 8px; padding: 6px 10px; }
.chapter-row { display: flex; align-items: flex-start; gap: 8px; padding: 6px 0; border-bottom: 1px dashed #f2f3f5; }
.chapter-row:last-child { border-bottom: none; }
.chapter-row.dragging { opacity: .5; background: #f7f8fa; }
.chapter-row.off .nc-desc { color: #c9cdd4; }
.drag-handle { cursor: grab; color: #c9cdd4; font-size: 12px; line-height: 22px; user-select: none; }
.chapter-main { flex: 1; min-width: 0; }
.chapter-actions { display: flex; align-items: center; gap: 10px; margin-top: 4px; flex-wrap: wrap; }
.chapter-md { margin-top: 6px; }
.chapter-md :deep(textarea) { font-family: Consolas, 'Courier New', monospace; font-size: 12px; }
.nc-label { font-size: 13px; }
.nc-desc { font-size: 11.5px; color: #86909c; }
.tpl-chip { color: #4e5969; }
.tpl-mine { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.tpl-tag { cursor: pointer; }
.tpl-empty { font-size: 12px; color: #86909c; }
.tpl-hint { font-size: 12px; color: #86909c; margin-top: 8px; }
.tpl-actions { display: flex; align-items: center; margin: 4px 0 12px; flex-wrap: wrap; }
.build-stamp { font-size: 11.5px; color: #c9cdd4; margin-bottom: 24px; }
.preview-wrap { height: 76vh; background: #f7f8fa; border: 1px solid #e5e6eb; border-radius: 8px; overflow: hidden; }
.preview-frame { width: 100%; height: 100%; border: 0; background: #fff; }
</style>
