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
    logs.value = await AgentAPI.queryLogs({ keyword: keyword.value, level: level.value, category: category.value, limit: 500 }) || []
  } catch (e) {
    ElMessage.error(String(e))
  } finally { loading.value = false }
}

function toggle(id) { expanded.value[id] = !expanded.value[id] }

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
        <pre v-if="expanded[l.id] && l.detail" class="detail">{{ l.detail }}</pre>
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
.detail { margin: 6px 0 0; padding: 8px; background: var(--surface-2); border-radius: 6px; font-size: 12px; white-space: pre-wrap; word-break: break-all; max-height: 300px; overflow: auto; }
</style>
