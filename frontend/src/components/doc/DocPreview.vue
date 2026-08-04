<script setup>
import { ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { ExportDoc, CopyDocMarkdown } from '../../../wailsjs/go/main/App'
import { saveNow, currentProject, savedApiSnapshots } from '../../store'
import ShareDialog from './ShareDialog.vue'

const props = defineProps({ api: { type: Object, required: true } })
const busy = ref(false)
const shareVisible = ref(false)

// 文档预览只读「已保存快照」：只有点击保存请求后快照才会刷新，
// 实时调试编辑（未保存）不会体现到文档里。
const docApi = computed(() => savedApiSnapshots[props.api.id] || props.api)

async function doExport(format) {
  busy.value = true
  try {
    await saveNow()
    const path = await ExportDoc('', props.api.id, format)
    if (path) ElMessage.success('已导出：' + path)
  } catch (e) {
    ElMessage.error(String(e))
  } finally { busy.value = false }
}

async function copyMd() {
  try {
    await saveNow()
    await CopyDocMarkdown('', props.api.id)
    ElMessage.success('Markdown 已复制到剪贴板')
  } catch (e) { ElMessage.error(String(e)) }
}

function flatRows(fields, depth = 0) {
  const out = []
  for (const f of fields || []) {
    out.push({ f, depth })
    out.push(...flatRows(f.children, depth + 1))
  }
  return out
}

// 项目公共参数（自动附加到所有接口请求）
const commonHeaders = computed(() => (currentProject().common?.headers || []).filter(h => h.enabled && h.key))
const commonQuery = computed(() => (currentProject().common?.query || []).filter(q => q.enabled && q.key))

// 将文本中的 {{变量}} 高亮显示（v-html 注入的内容需在全局样式中定义 .var-chip）
function escapeHtml(s) {
  return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}
function highlightVars(text) {
  if (!text) return ''
  return String(text).split(/(\{\{[^}]+\}\})/g).map(p =>
    /^\{\{[^}]+\}\}$/.test(p) ? '<span class="var-chip">' + escapeHtml(p) + '</span>' : escapeHtml(p)
  ).join('')
}
function formFileName(path) {
  if (!path) return ''
  return String(path).split(/[\\/]/).pop()
}
</script>

<template>
  <div class="panel-page">
    <div class="card">
      <div class="card-title">
        <span>文档预览（保存请求后生成）</span>
        <span>
          <el-button size="small" @click="copyMd">复制 Markdown</el-button>
          <el-button size="small" @click="shareVisible = true">分享为网页链接</el-button>
          <el-dropdown style="margin-left:10px" @command="doExport">
            <el-button size="small" type="primary" :loading="busy">导出文档 ▾</el-button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="markdown">Markdown (.md)</el-dropdown-item>
                <el-dropdown-item command="html">HTML (.html)</el-dropdown-item>
                <el-dropdown-item command="word">Word (.doc)</el-dropdown-item>
                <el-dropdown-item command="openapi">OpenAPI (.json)</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </span>
      </div>

      <div class="doc-preview">
        <h2>{{ docApi.name }}</h2>
        <div class="urlbar">
          <span class="method-tag" :class="'m-' + docApi.method">{{ docApi.method }}</span>
          <span v-html="highlightVars(docApi.url || '（未填写地址）')"></span>
        </div>
        <p v-if="docApi.description" style="color:#4e5969" v-html="highlightVars(docApi.description)"></p>

        <template v-if="docApi.headers.some(h => h.enabled && h.key)">
          <h4>请求头</h4>
          <table>
            <thead><tr><th>参数名</th><th>值/示例</th><th>说明</th></tr></thead>
            <tbody>
              <tr v-for="(h, i) in docApi.headers.filter(x => x.enabled && x.key)" :key="i">
                <td>{{ h.key }}</td><td v-html="highlightVars(h.value)"></td><td>{{ h.description }}</td>
              </tr>
            </tbody>
          </table>
        </template>

        <template v-if="docApi.query.some(q => q.enabled && q.key)">
          <h4>Query 参数</h4>
          <table>
            <thead><tr><th>参数名</th><th>值/示例</th><th>说明</th></tr></thead>
            <tbody>
              <tr v-for="(q, i) in docApi.query.filter(x => x.enabled && x.key)" :key="i">
                <td>{{ q.key }}</td><td v-html="highlightVars(q.value)"></td><td>{{ q.description }}</td>
              </tr>
            </tbody>
          </table>
        </template>

        <template v-if="(docApi.formItems || []).some(f => f.enabled && f.key)">
          <h4>表单参数（Form / 文件上传）</h4>
          <table>
            <thead><tr><th>字段名</th><th>类型</th><th>值/文件</th><th>说明</th></tr></thead>
            <tbody>
              <tr v-for="(f, i) in docApi.formItems.filter(x => x.enabled && x.key)" :key="i">
                <td>{{ f.key }}</td>
                <td style="color:#165dff">{{ f.type === 'file' ? '文件' : '文本' }}</td>
                <td v-html="f.type === 'file' ? (f.value ? ('📎 ' + formFileName(f.value)) : '（未选择文件）') : highlightVars(f.value)"></td>
                <td>{{ f.description }}</td>
              </tr>
            </tbody>
          </table>
        </template>

        <template v-if="commonHeaders.length || commonQuery.length">
          <h4>公共参数 <span class="common-tip">（项目级，自动附加到所有接口，接口同名优先）</span></h4>
          <table v-if="commonHeaders.length">
            <thead><tr><th>公共请求头</th><th>值/示例</th><th>说明</th></tr></thead>
            <tbody>
              <tr v-for="(h, i) in commonHeaders" :key="'ch'+i">
                <td>{{ h.key }}</td><td v-html="highlightVars(h.value)"></td><td>{{ h.description }}</td>
              </tr>
            </tbody>
          </table>
          <table v-if="commonQuery.length" style="margin-top:8px">
            <thead><tr><th>公共 Query 参数</th><th>值/示例</th><th>说明</th></tr></thead>
            <tbody>
              <tr v-for="(q, i) in commonQuery" :key="'cq'+i">
                <td>{{ q.key }}</td><td v-html="highlightVars(q.value)"></td><td>{{ q.description }}</td>
              </tr>
            </tbody>
          </table>
        </template>

        <template v-for="part in [
          { title: '请求参数', rows: flatRows(docApi.reqFields) },
          { title: '响应参数', rows: flatRows(docApi.respFields) },
        ]" :key="part.title">
          <template v-if="part.rows.length">
            <h4>{{ part.title }}</h4>
            <table>
              <thead><tr><th style="width:32%;min-width:220px">字段名</th><th>类型</th><th>必填</th><th>说明</th><th>示例</th></tr></thead>
              <tbody>
                <tr v-for="(r, i) in part.rows" :key="i">
                  <td class="doc-fname" :style="{ paddingLeft: (10 + r.depth * 18) + 'px', fontFamily: 'Consolas, monospace' }">
                    {{ (r.depth ? '└ ' : '') + r.f.name }}
                  </td>
                  <td style="color:#165dff">{{ r.f.type }}</td>
                  <td>{{ r.f.required ? '是' : '否' }}</td>
                  <td>{{ r.f.description }}</td>
                  <td style="color:#86909c">{{ r.f.example }}</td>
                </tr>
              </tbody>
            </table>
          </template>
        </template>

        <template v-if="docApi.bodyType === 'json' && docApi.body.trim()">
          <h4>请求示例</h4>
          <pre v-html="highlightVars(docApi.body)"></pre>
        </template>
        <template v-if="docApi.lastResponse && docApi.lastResponse.isJson">
          <h4>响应示例（最近一次保存的请求响应）</h4>
          <pre v-html="highlightVars(docApi.lastResponse.body.length > 4000 ? docApi.lastResponse.body.slice(0, 4000) + '\n...(截断)' : docApi.lastResponse.body)"></pre>
        </template>
      </div>
    </div>

    <ShareDialog v-model:visible="shareVisible" dir-id="" :api-id="api.id" />
  </div>
</template>

<style scoped>
.common-tip { font-size: 12px; color: #c2c7cf; font-weight: 400; }
/* 响应/请求参数：字段名列加宽且不换行，超长时显示省略号 */
.doc-fname { white-space: nowrap; max-width: 360px; overflow: hidden; text-overflow: ellipsis; }
</style>
