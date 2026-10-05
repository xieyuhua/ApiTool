<script setup>
import { ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { EventsOn, EventsOff } from '../../../wailsjs/runtime/runtime'
import { AgentAPI } from './agentApi'

const props = defineProps({ visible: Boolean })
const emit = defineEmits(['update:visible'])

const logs = ref([])
const keyword = ref('')
const level = ref('')
const category = ref('')
const loading = ref(false)
const expanded = ref({})

const levels = [
  { v: '', t: '全部级别' }, { v: 'info', t: 'info' }, { v: 'request', t: 'request' },
  { v: 'response', t: 'response' }, { v: 'tool', t: 'tool' }, { v: 'plan', t: 'plan' }, { v: 'error', t: 'error' },
]
const cats = [
  { v: '', t: '全部分类' }, { v: 'agent', t: 'agent' }, { v: 'llm', t: 'llm' }, { v: 'mcp', t: 'mcp' }, { v: 'skill', t: 'skill' },
]

async function reload() {
  loading.value = true
  try {
    logs.value = await AgentAPI.queryLogs({ keyword: keyword.value, level: level.value, category: category.value, limit: 500, withDetail: false }) || []
  } catch (e) {
    ElMessage.error(String(e))
  } finally { loading.value = false }
}

// 详情按需加载：列表接口不返回 detail（可能很长），点开时再取完整内容
const details = reactive({})
const loadingDetail = reactive({})

function toggle(id) {
  expanded.value[id] = !expanded.value[id]
  if (expanded.value[id] && details.value[id] === undefined) {
    loadingDetail.value[id] = true
    AgentAPI.getAgentLog(id).then(l => {
      details.value[id] = l && l.detail ? l.detail : '(无详情)'
    }).catch(e => {
      details.value[id] = '加载失败：' + String(e)
    }).finally(() => { loadingDetail.value[id] = false })
  }
}

// 复制单条日志的完整详情（便于贴到别处分析）
async function copyDetail(l, ev) {
  if (ev) ev.stopPropagation()
  try {
    if (details.value[l.id] === undefined) await toggle(l.id)
    const text = details.value[l.id] || l.detail
    if (!text) { ElMessage.warning('该条没有可复制的详情'); return }
    await navigator.clipboard.writeText(text)
    ElMessage.success('已复制完整详情')
  } catch (e) {
    ElMessage.error('复制失败，请手动选中复制')
  }
}

async function clearAll() {
  try {
    await ElMessageBox.confirm('确认清空所有 Agent 日志？', '提示', { type: 'warning' })
    await AgentAPI.clearLogs()
    logs.value = []
    ElMessage.success('已清空')
  } catch { /* cancel */ }
}

let off = null
watch(() => props.visible, (v) => {
  if (v) {
    reload()
    off = EventsOn('agent:log', () => { if (props.visible) reload() })
  } else if (off) {
    EventsOff('agent:log'); off = null
  }
})

function levelClass(l) { return 'lv lv-' + l }
</script>

<template>
  <el-drawer :model-value="visible" @update:model-value="emit('update:visible', $event)" title="Agent 请求与调度日志" size="720px">
    <div class="toolbar">
      <el-input v-model="keyword" size="small" placeholder="搜索标题/内容" clearable style="width:220px" @keyup.enter="reload" @clear="reload" />
      <el-select v-model="level" size="small" style="width:120px" @change="reload">
        <el-option v-for="l in levels" :key="l.v" :label="l.t" :value="l.v" />
      </el-select>
      <el-select v-model="category" size="small" style="width:120px" @change="reload">
        <el-option v-for="c in cats" :key="c.v" :label="c.t" :value="c.v" />
      </el-select>
      <el-button size="small" @click="reload" :loading="loading">搜索</el-button>
      <el-button size="small" type="danger" plain @click="clearAll">清空</el-button>
    </div>

    <div class="log-list">
      <div v-if="!logs.length" class="empty">暂无日志</div>
      <div v-for="l in logs" :key="l.id" class="log-item" @click="toggle(l.id)">
        <div class="log-line">
          <span :class="levelClass(l.level)">{{ l.level }}</span>
          <span class="cat">{{ l.category }}</span>
          <span class="title">{{ l.title }}</span>
          <span class="dur" v-if="l.durationMs">{{ l.durationMs }}ms</span>
          <span class="time">{{ l.time }}</span>
        </div>
        <div v-if="expanded[l.id]" class="detail-wrap">
          <div class="detail-bar">
            <span class="detail-tip">{{ loadingDetail[l.id] ? '正在加载完整内容…' : '完整内容' }}</span>
            <span class="detail-ops" @click.stop>
              <el-button size="small" text type="primary" @click="copyDetail(l, $event)">复制</el-button>
              <el-button size="small" text @click="expanded[l.id] = false">收起</el-button>
            </span>
          </div>
          <pre class="detail">{{ loadingDetail[l.id] ? '加载中…' : (details[l.id] !== undefined ? details[l.id] : l.detail) }}</pre>
        </div>
      </div>
    </div>
  </el-drawer>
</template>

<style scoped>
.toolbar { display: flex; gap: 8px; margin-bottom: 10px; flex-wrap: wrap; }
.log-list { overflow: auto; }
.empty { color: var(--text-muted); text-align: center; padding: 30px; }
.log-item { border-bottom: 1px solid var(--border); padding: 8px 4px; cursor: pointer; }
.log-item:hover { background: var(--surface-2); }
.log-line { display: flex; align-items: center; gap: 8px; font-size: 12px; }
.log-line .title { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.log-line .cat { color: var(--text-muted); }
.log-line .dur { color: #d97706; }
.log-line .time { color: var(--text-muted); font-family: monospace; }
.lv { padding: 1px 6px; border-radius: 4px; font-family: monospace; color: #fff; font-size: 11px; }
.lv-info { background: #3b82f6; }
.lv-request { background: #8b5cf6; }
.lv-response { background: #10b981; }
.lv-tool { background: #f59e0b; }
.lv-plan { background: #6366f1; }
.lv-error { background: #ef4444; }
.detail-wrap { margin: 6px 0 2px; }
.detail-bar { display: flex; align-items: center; justify-content: space-between; margin-bottom: 4px; }
.detail-tip { font-size: 11px; color: var(--text-muted); }
.detail-ops { display: flex; gap: 2px; }
/* 完整内容可能很长（整份请求体），给足高度并支持双向滚动 */
.detail {
  margin: 0; padding: 10px; background: var(--surface-2); border: 1px solid var(--border);
  border-radius: 6px; font-size: 12px; line-height: 1.6;
  white-space: pre-wrap; word-break: break-word;
  max-height: 60vh; overflow: auto;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}
</style>
