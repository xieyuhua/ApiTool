<script setup>
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { ExportDoc, CopyDocMarkdown } from '../../wailsjs/go/main/App'
import { saveNow } from '../store'
import ShareDialog from './ShareDialog.vue'

const props = defineProps({ api: { type: Object, required: true } })
const busy = ref(false)
const shareVisible = ref(false)

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
</script>

<template>
  <div class="panel-page">
    <div class="card">
      <div class="card-title">
        <span>文档预览（随调试自动生成）</span>
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
        <h2>{{ api.name }}</h2>
        <div class="urlbar">
          <span class="method-tag" :class="'m-' + api.method">{{ api.method }}</span>
          <span v-html="highlightVars(api.url || '（未填写地址）')"></span>
        </div>
        <p v-if="api.description" style="color:#4e5969" v-html="highlightVars(api.description)"></p>

        <template v-if="api.headers.some(h => h.enabled && h.key)">
          <h4>请求头</h4>
          <table>
            <thead><tr><th>参数名</th><th>值/示例</th><th>说明</th></tr></thead>
            <tbody>
              <tr v-for="(h, i) in api.headers.filter(x => x.enabled && x.key)" :key="i">
                <td>{{ h.key }}</td><td v-html="highlightVars(h.value)"></td><td>{{ h.description }}</td>
              </tr>
            </tbody>
          </table>
        </template>

        <template v-if="api.query.some(q => q.enabled && q.key)">
          <h4>Query 参数</h4>
          <table>
            <thead><tr><th>参数名</th><th>值/示例</th><th>说明</th></tr></thead>
            <tbody>
              <tr v-for="(q, i) in api.query.filter(x => x.enabled && q.key)" :key="i">
                <td>{{ q.key }}</td><td v-html="highlightVars(q.value)"></td><td>{{ q.description }}</td>
              </tr>
            </tbody>
          </table>
        </template>

        <template v-for="part in [
          { title: '请求参数', rows: flatRows(api.reqFields) },
          { title: '响应参数', rows: flatRows(api.respFields) },
        ]" :key="part.title">
          <template v-if="part.rows.length">
            <h4>{{ part.title }}</h4>
            <table>
              <thead><tr><th>字段名</th><th>类型</th><th>必填</th><th>说明</th><th>示例</th></tr></thead>
              <tbody>
                <tr v-for="(r, i) in part.rows" :key="i">
                  <td :style="{ paddingLeft: (10 + r.depth * 18) + 'px', fontFamily: 'Consolas, monospace' }">
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

        <template v-if="api.bodyType === 'json' && api.body.trim()">
          <h4>请求示例</h4>
          <pre v-html="highlightVars(api.body)"></pre>
        </template>
        <template v-if="api.lastResponse && api.lastResponse.isJson">
          <h4>响应示例（最近一次调试结果）</h4>
          <pre v-html="highlightVars(api.lastResponse.body.length > 4000 ? api.lastResponse.body.slice(0, 4000) + '\n...(截断)' : api.lastResponse.body)"></pre>
        </template>
      </div>
    </div>

    <ShareDialog v-model:visible="shareVisible" dir-id="" :api-id="api.id" />
  </div>
</template>
