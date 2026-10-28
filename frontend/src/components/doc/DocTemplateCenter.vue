<script setup>
// 文档模板（企业级排版）：模板库（预置 + 我的模板）＋ 结构表单。
// 独立菜单页，便于团队统一维护接口文档的章节结构与规范模板。
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  DocPresets, DocPresetLayout, SaveDocLayoutTemplate, DeleteDocLayoutTemplate,
} from '../../../wailsjs/go/main/App'
import { store, saveNow, reloadStore } from '../../store'
import DocLayoutPanel from './DocLayoutPanel.vue'

const presets = ref([])
const applying = ref('')
const panelRef = ref(null)

const dl = computed(() => store.data.settings.docLayout)
const myTemplates = computed(() => store.data.settings.docLayoutTemplates || [])

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
    const layout = await DocPresetLayout(p.key)
    await applyLayout(layout, p.label)
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
  } catch (e) {
    ElMessage.error(String(e))
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
        <div class="tpl-current">当前结构：<b>{{ summary }}</b></div>
      </div>

      <div class="card">
        <div class="card-title">模板库（一键套用）</div>
        <div class="tpl-grid">
          <div v-for="p in presets" :key="p.key" class="tpl-card" :class="{ busy: applying === p.key }"
            @click="applyPreset(p)">
            <div class="tpl-head">
              <span class="tpl-name">{{ p.label }}</span>
              <el-tag size="small" effect="plain" type="info">{{ p.tag }}</el-tag>
            </div>
            <div class="tpl-desc">{{ p.desc }}</div>
          </div>
        </div>
      </div>

      <div class="card">
        <div class="card-title">我的模板（{{ myTemplates.length }}）</div>
        <div class="tpl-mine">
          <el-tag v-for="tpl in myTemplates" :key="tpl.id" class="tpl-tag" closable
            @close="removeTemplate(tpl)" @click="applyTemplate(tpl)">{{ tpl.name }}</el-tag>
          <span v-if="!myTemplates.length" class="tpl-empty">暂无自定义模板，配置好结构后点右侧保存</span>
          <el-button size="small" type="primary" plain style="margin-left:auto" @click="saveAsTemplate">
            + 保存当前结构为模板
          </el-button>
        </div>
        <div class="tpl-hint">最多保存 20 套；同名保存会覆盖旧模板。点击标签即可套用。</div>
      </div>

      <DocLayoutPanel ref="panelRef" :dir-id="''" />

      <div class="tpl-actions">
        <el-button type="primary" plain @click="preview">👁 预览整份文档</el-button>
        <span class="tpl-hint" style="margin-left:10px">用当前结构与水印在浏览器打开，Ctrl+P 可看打印分页</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.tpl-lead { font-size: 13px; color: #4e5969; line-height: 1.9; margin-bottom: 14px; }
.tpl-current { color: #165dff; margin-top: 4px; }
.tpl-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 12px; }
.tpl-card { border: 1px solid #e5e6eb; border-radius: 10px; padding: 14px; cursor: pointer; transition: all .15s; background: #fff; }
.tpl-card:hover { border-color: #165dff; box-shadow: 0 2px 10px rgba(22, 93, 255, .12); }
.tpl-card.busy { opacity: .5; pointer-events: none; }
.tpl-head { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
.tpl-name { font-weight: 600; font-size: 14px; }
.tpl-desc { font-size: 12px; color: #86909c; line-height: 1.7; }
.tpl-mine { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.tpl-tag { cursor: pointer; }
.tpl-empty { font-size: 12px; color: #86909c; }
.tpl-hint { font-size: 12px; color: #86909c; margin-top: 8px; }
.tpl-actions { display: flex; align-items: center; margin: 4px 0 24px; }
</style>
