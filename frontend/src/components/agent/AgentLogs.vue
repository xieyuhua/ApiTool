<script setup>
// Agent 请求与调度日志面板。桌面端与局域网网页端共用本组件，
// 事件订阅统一走 window.runtime（桌面端由 Wails 注入，网页端由 httpBridge 用 SSE 模拟），
// 因此两端行为完全一致。
import { ref, watch, reactive, onBeforeUnmount } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { AgentAPI } from './agentApi'

const props = defineProps({ visible: Boolean })
const emit = defineEmits(['update:visible'])

const logs = ref([])
const keyword = ref('')
const level = ref('')
const category = ref('')
const loading = ref(false)
// expanded / details / loadingDetail 都是普通响应式对象（reactive），直接用属性访问，不要 .value
const expanded = reactive({})
const details = reactive({})
const loadingDetail = reactive({})
// 全部日志累计的 token 用量（来自后端 facets 汇总）
const tokenUsage = reactive({ promptTokens: 0, completionTokens: 0, totalTokens: 0 })

// 筛选项由后端 facets 动态生成（按实际存在的级别/分类 + 条数），
// 不再写死 —— 写死会出现「选项存在但永远查不到数据」：
// 例如历史上提供过的 plan 级别、skill 分类在旧版本里从不产生日志。
const levels = ref([{ v: '', t: '全部级别', n: 0 }])
const cats = ref([{ v: '', t: '全部分类', n: 0 }])

const LEVEL_LABEL = { info: 'info', request: 'request', response: 'response', tool: 'tool', plan: 'plan', error: 'error' }
const CAT_LABEL = { agent: 'agent', llm: 'llm', mcp: 'mcp', skill: 'skill' }

function fmtTokens(n) { return Number(n || 0).toLocaleString('zh-CN') }

async function reload() {
  loading.value = true
  try {
    logs.value = await AgentAPI.queryLogs({ keyword: keyword.value, level: level.value, category: category.value, limit: 500, withDetail: false }) || []
  } catch (e) {
    ElMessage.error(String(e))
  } finally { loading.value = false }
}

// 汇总当前筛选结果内的 token 用量 + 动态刷新筛选项（仅日志数据变化时调用）
async function reloadFacets() {
  try {
    const f = await AgentAPI.logFacets()
    const t = f && f.tokenUsage && f.tokenUsage[0]
    tokenUsage.promptTokens = (t && t.promptTokens) || 0
    tokenUsage.completionTokens = (t && t.completionTokens) || 0
    tokenUsage.totalTokens = (t && t.totalTokens) || 0

    const build = (list, labels, cur) => {
      const items = [{ v: '', t: cur, n: 0 }]
      for (const it of (list || [])) {
        items.push({ v: it.value, t: `${labels[it.value] || it.value} (${it.count})`, n: it.count || 0 })
      }
      return items
    }
    levels.value = build(f && f.levels, LEVEL_LABEL, '全部级别')
    cats.value = build(f && f.categories, CAT_LABEL, '全部分类')
    // 当前选中的值若已不存在（例如清空日志后），回退到「全部」，避免停留在空结果上
    if (level.value && !levels.value.some(x => x.v === level.value)) level.value = ''
    if (category.value && !cats.value.some(x => x.v === category.value)) category.value = ''
  } catch (e) { /* 忽略：老数据或后端未升级时无 facets */ }
}

function reloadAll() { reload(); reloadFacets() }

// 详情按需加载：列表接口不返回 detail（可能很长），点开时再取完整内容
function toggle(id) {
  const open = !expanded[id]
  expanded[id] = open
  if (!open) return
  if (details[id] !== undefined) return
  loadingDetail[id] = true
  AgentAPI.getAgentLog(id).then(l => {
    details[id] = l && l.detail ? l.detail : '(无详情)'
  }).catch(e => {
    details[id] = '加载失败：' + String(e)
  }).finally(() => { loadingDetail[id] = false })
}

// 复制单条日志的完整详情（便于贴到别处分析）
async function copyDetail(l, ev) {
  if (ev) ev.stopPropagation()
  try {
    if (details[l.id] === undefined) {
      loadingDetail[l.id] = true
      try {
        const d = await AgentAPI.getAgentLog(l.id)
        details[l.id] = d && d.detail ? d.detail : '(无详情)'
      } finally { loadingDetail[l.id] = false }
    }
    const text = details[l.id] || l.detail
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
  } catch { return }
  try {
    await AgentAPI.clearLogs()
    logs.value = []
    await reloadFacets()
    ElMessage.success('已清空')
  } catch (e) { ElMessage.error(String(e)) }
}

// ---------------- 实时刷新 ----------------
// 一次对话可能连续产生十几条日志，直接逐条 reload 会打爆后端，这里做 300ms 合并。
let reloadTimer = 0
function scheduleReload() {
  if (reloadTimer) return
  reloadTimer = setTimeout(() => { reloadTimer = 0; if (props.visible) reloadAll() }, 300)
}

// 订阅/退订。用 EventsOn 返回的取消函数精确移除本组件的监听，
// 不用 EventsOff(name)——那会连带清掉同名事件的所有其它订阅者。
let offLog = null
function subscribe() {
  if (offLog) return
  const rt = typeof window !== 'undefined' ? window.runtime : null
  if (!rt || typeof rt.EventsOnMultiple !== 'function') return
  offLog = rt.EventsOnMultiple('agent:log', () => { if (props.visible) scheduleReload() }, -1)
}
function unsubscribe() {
  if (offLog) { try { offLog() } catch (e) { /* ignore */ } offLog = null }
}

watch(() => props.visible, (v) => {
  if (v) { reloadAll(); subscribe() } else { unsubscribe() }
})
onBeforeUnmount(() => { unsubscribe(); if (reloadTimer) { clearTimeout(reloadTimer); reloadTimer = 0 } })

function levelClass(l) { return 'lv lv-' + l }
</script>

<template>
  <el-drawer :model-value="visible" @update:model-value="emit('update:visible', $event)" title="Agent 请求与调度日志" size="720px">
    <div class="usage-bar" title="所有日志中记录的 LLM 调用累计用量（不受下方筛选影响）">
      <span class="usage-label">全部日志 Token</span>
      <span class="usage-item">输入 <b>{{ fmtTokens(tokenUsage.promptTokens) }}</b></span>
      <span class="usage-item">输出 <b>{{ fmtTokens(tokenUsage.completionTokens) }}</b></span>
      <span class="usage-item total">合计 <b>{{ fmtTokens(tokenUsage.totalTokens) }}</b></span>
    </div>

    <div class="toolbar">
      <el-input v-model="keyword" size="small" placeholder="搜索标题/内容" clearable style="width:200px" @keyup.enter="reloadAll" @clear="reloadAll" />
      <el-select v-model="level" size="small" style="width:150px" @change="reload">
        <el-option v-for="l in levels" :key="l.v" :label="l.t" :value="l.v" />
      </el-select>
      <el-select v-model="category" size="small" style="width:150px" @change="reload">
        <el-option v-for="c in cats" :key="c.v" :label="c.t" :value="c.v" />
      </el-select>
      <el-button size="small" @click="reloadAll" :loading="loading">搜索</el-button>
      <el-button size="small" type="danger" plain @click="clearAll">清空</el-button>
    </div>

    <div class="log-list">
      <div v-if="!logs.length" class="empty">{{ loading ? '加载中…' : '暂无日志' }}</div>
      <div v-for="l in logs" :key="l.id" class="log-item" @click="toggle(l.id)">
        <div class="log-line">
          <span :class="levelClass(l.level)">{{ l.level }}</span>
          <span class="cat">{{ l.category }}</span>
          <span class="title">{{ l.title }}</span>
          <span v-if="l.usage && l.usage.totalTokens" class="tok"
            :title="'输入 ' + fmtTokens(l.usage.promptTokens) + ' · 输出 ' + fmtTokens(l.usage.completionTokens) + ' · 合计 ' + fmtTokens(l.usage.totalTokens)">
            ↑{{ fmtTokens(l.usage.promptTokens) }} ↓{{ fmtTokens(l.usage.completionTokens) }} ⚡{{ fmtTokens(l.usage.totalTokens) }}
          </span>
          <span class="dur" v-if="l.durationMs">{{ l.durationMs }}ms</span>
          <span class="time">{{ l.time }}</span>
        </div>
        <div v-if="l.summary" class="summary">{{ l.summary }}</div>
        <div v-if="expanded[l.id]" class="detail-wrap">
          <div class="detail-bar">
            <span class="detail-tip">{{ loadingDetail[l.id] ? '正在加载完整内容…' : '完整内容' }}</span>
            <span class="detail-ops" @click.stop>
              <el-button size="small" text type="primary" @click="copyDetail(l, $event)">复制</el-button>
              <el-button size="small" text @click="expanded[l.id] = false">收起</el-button>
            </span>
          </div>
          <pre class="detail">{{ loadingDetail[l.id] ? '加载中…' : (details[l.id] !== undefined ? details[l.id] : (l.detail || l.summary || '(无详情)')) }}</pre>
        </div>
      </div>
    </div>
  </el-drawer>
</template>

<style scoped>
.usage-bar {
  display: flex; align-items: center; gap: 14px; flex-wrap: wrap;
  padding: 8px 10px; margin-bottom: 10px;
  background: var(--surface-2); border: 1px solid var(--border); border-radius: 6px;
  font-size: 12px; color: var(--text-muted);
}
.usage-label { font-weight: 600; color: var(--text); }
.usage-item b { color: var(--text); font-variant-numeric: tabular-nums; }
.usage-item.total b { color: var(--primary); }
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
.log-line .tok {
  color: var(--primary); font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 11px; white-space: nowrap; cursor: help;
}
.summary {
  font-size: 11px; color: var(--text-muted); margin-top: 3px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
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