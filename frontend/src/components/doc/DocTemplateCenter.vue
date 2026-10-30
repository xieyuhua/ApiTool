<script setup>
// 文档模板（企业级排版）：模板库（预置 + 我的模板）＋ 结构表单。
// 独立菜单页，便于团队统一维护接口文档的章节结构与规范模板；支持模板导出/导入 JSON。
import { ref, computed, onMounted, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  DocPresets, DocPresetLayout, SaveDocLayoutTemplate, DeleteDocLayoutTemplate,
  PullDocTemplates, PushDocTemplates, PreviewDocTemplate, PreviewDocTemplateHTML,
  RenameDocLayoutTemplate, DuplicateDocLayoutTemplate,
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
const previewing = ref('')

// openPreview 拉取预览 HTML 并在弹窗内用 iframe 展示
async function openPreview(layout, title) {
  previewTitle.value = `模板预览 · ${title}（不含具体接口）`
  previewHTML.value = ''
  previewVisible.value = true
  previewLoading.value = true
  try {
    const html = await PreviewDocTemplateHTML(JSON.stringify({ ...layout, name: title }))
    previewHTML.value = html || ''
    if (!html) ElMessage.warning('预览内容为空')
  } catch (e) {
    ElMessage.error('预览失败：' + String(e))
    previewVisible.value = false
  } finally {
    previewLoading.value = false
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

// 弹窗内的「在浏览器打开」：写入临时文件并调用系统浏览器（便于打印 PDF）
async function openInBrowser() {
  try {
    const path = await PreviewDocTemplate(JSON.stringify({ ...dl.value, name: activeName.value }))
    if (path) ElMessage.success('已在浏览器打开：' + path)
  } catch (e) {
    ElMessage.error(String(e))
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
async function newTemplate() {
  let name = ''
  try {
    const r = await ElMessageBox.prompt('新模板名称（如「xx 事业部标准」「内部评审版」）', '新建自定义模板', {
      inputPlaceholder: '模板名称', inputValidator: v => (v && v.trim() ? true : '请输入模板名称'),
    })
    name = (r.value || '').trim()
  } catch { return }
  if (myTemplates.value.some(t => t.name === name)) {
    ElMessage.warning('已存在同名模板，请换个名称')
    return
  }
  const copyCurrent = window.confirm('以「当前结构」为蓝本创建吗？\n确定 = 复制当前结构；取消 = 从空白模板（仅目录 + 编号）开始')
  const base = copyCurrent ? dl.value : { toc: true, numbering: true }
  const layout = normalize(JSON.parse(JSON.stringify(base)))
  layout.name = name
  try {
    await SaveDocLayoutTemplate(name, JSON.stringify(layout))
    await reloadStore()
    normalize(dl.value)
    await applyLayout(layout, name, true)
    ElMessage.success('已创建模板：' + name)
  } catch (e) {
    ElMessage.error(String(e))
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

// 套用模板（预置或自定义），覆盖当前结构
async function applyLayout(layout, name, silent = false) {
  if (!silent && !window.confirm(`套用「${name}」会覆盖当前文档结构配置，确定继续吗？`)) return
  try {
    const next = normalize(JSON.parse(JSON.stringify(layout)))
    next.name = name
    Object.assign(dl.value, next)
    await saveNow()
    ElMessage.success('已套用模板：' + name)
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
          <el-button size="small" type="primary" plain @click="newTemplate">＋ 新建模板</el-button>
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

      <DocLayoutPanel ref="panelRef" :layout="dl" :dir-id="scopeDirId" />

      <DocWatermarkPanel :dir-id="scopeDirId" />

      <el-dialog v-model="previewVisible" :title="previewTitle" width="1080px" top="4vh" destroy-on-close>
        <div v-loading="previewLoading" element-loading-text="正在生成预览…" class="preview-wrap">
          <iframe v-if="previewHTML" class="preview-frame" :srcdoc="previewHTML" />
        </div>
        <template #footer>
          <span class="tpl-hint" style="margin-right:auto">预览只渲染章节结构，不含具体接口；实际导出会输出所选范围内的全部接口</span>
          <el-button @click="openInBrowser">在浏览器打开</el-button>
          <el-button type="primary" @click="previewVisible = false">关闭</el-button>
        </template>
      </el-dialog>

      <div class="tpl-actions">
        <el-tree-select v-model="scopeDirId" :data="dirTree" check-strictly :render-after-expand="false"
          style="width:260px" placeholder="预览范围（默认全部接口）" default-expand-all clearable />
        <el-button plain style="margin-left:10px" :loading="previewing === 'current'" @click="previewCurrent">👁 预览当前模板（不含接口）</el-button>
        <el-button type="primary" plain style="margin-left:8px" @click="preview">👁 预览整份文档（含接口）</el-button>
        <span class="tpl-hint" style="margin-left:10px">用当前结构与水印在浏览器打开，Ctrl+P 可看打印分页</span>
      </div>
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
.tpl-chip { color: #4e5969; }
.tpl-mine { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.tpl-tag { cursor: pointer; }
.tpl-empty { font-size: 12px; color: #86909c; }
.tpl-hint { font-size: 12px; color: #86909c; margin-top: 8px; }
.tpl-actions { display: flex; align-items: center; margin: 4px 0 24px; flex-wrap: wrap; }
.preview-wrap { height: 76vh; background: #f7f8fa; border: 1px solid #e5e6eb; border-radius: 8px; overflow: hidden; }
.preview-frame { width: 100%; height: 100%; border: 0; background: #fff; }
</style>
