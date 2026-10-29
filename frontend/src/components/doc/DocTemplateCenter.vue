<script setup>
// 文档模板（企业级排版）：模板库（预置 + 我的模板）＋ 结构表单。
// 独立菜单页，便于团队统一维护接口文档的章节结构与规范模板；支持模板导出/导入 JSON。
import { ref, computed, onMounted, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  DocPresets, DocPresetLayout, SaveDocLayoutTemplate, DeleteDocLayoutTemplate,
  PullDocTemplates, PushDocTemplates,
} from '../../../wailsjs/go/main/App'
import { store, saveNow, reloadStore, buildDirTree, currentProject, cloudConfigured, cloudSyncEnabled } from '../../store'
import DocLayoutPanel from './DocLayoutPanel.vue'

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

// 套用模板（预置或自定义），覆盖当前结构
async function applyLayout(layout, name) {
  if (!window.confirm(`套用「${name}」会覆盖当前文档结构配置，确定继续吗？`)) return
  try {
    Object.assign(dl.value, normalize(JSON.parse(JSON.stringify(layout))))
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
        <div class="tpl-current">当前结构：<b>{{ summary }}</b>（{{ scope === 'project' ? '项目级覆盖' : '全局配置' }}）</div>
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
          <div v-for="p in presets" :key="p.key" class="tpl-card" :class="{ busy: applying === p.key }"
            @click="applyPreset(p)">
            <div class="tpl-head">
              <span class="tpl-name">{{ p.label }}</span>
              <el-tag size="small" effect="plain" type="info">{{ p.tag }}</el-tag>
            </div>
            <div class="tpl-desc">{{ p.desc }}</div>
            <div class="tpl-chips">
              <el-tag v-for="c in (p.chips || [])" :key="c" size="small" effect="plain" class="tpl-chip">{{ c }}</el-tag>
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
        <div class="tpl-mine">
          <el-tag v-for="tpl in myTemplates" :key="tpl.id" class="tpl-tag" closable
            @close="removeTemplate(tpl)" @click="applyTemplate(tpl)">{{ tpl.name }}</el-tag>
          <span v-if="!myTemplates.length" class="tpl-empty">暂无自定义模板，配置好结构后点右侧保存</span>
          <el-button size="small" type="primary" plain style="margin-left:auto" @click="saveAsTemplate">
            + 保存当前结构为模板
          </el-button>
        </div>
        <div class="tpl-hint">
          最多保存 20 套；同名保存会覆盖旧模板，点击标签即可套用。
          <span v-if="cloudReady && cloudSyncEnabled()">已开启自动同步：保存 / 删除模板后会自动上传云端。</span>
        </div>
      </div>

      <DocLayoutPanel ref="panelRef" :layout="dl" :dir-id="scopeDirId" />

      <div class="tpl-actions">
        <el-tree-select v-model="scopeDirId" :data="dirTree" check-strictly :render-after-expand="false"
          style="width:260px" placeholder="预览范围（默认全部接口）" default-expand-all clearable />
        <el-button type="primary" plain style="margin-left:10px" @click="preview">👁 预览整份文档</el-button>
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
.tpl-ops { float: right; }
.tpl-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(230px, 1fr)); gap: 12px; }
.tpl-card { border: 1px solid #e5e6eb; border-radius: 10px; padding: 14px; cursor: pointer; transition: all .15s; background: #fff; }
.tpl-card:hover { border-color: #165dff; box-shadow: 0 2px 10px rgba(22, 93, 255, .12); }
.tpl-card.busy { opacity: .5; pointer-events: none; }
.tpl-head { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
.tpl-name { font-weight: 600; font-size: 14px; }
.tpl-desc { font-size: 12px; color: #86909c; line-height: 1.7; }
.tpl-chips { margin-top: 10px; display: flex; gap: 6px; flex-wrap: wrap; }
.tpl-chip { color: #4e5969; }
.tpl-mine { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.tpl-tag { cursor: pointer; }
.tpl-empty { font-size: 12px; color: #86909c; }
.tpl-hint { font-size: 12px; color: #86909c; margin-top: 8px; }
.tpl-actions { display: flex; align-items: center; margin: 4px 0 24px; flex-wrap: wrap; }
</style>
