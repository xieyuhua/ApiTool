<script setup>
import { ref, reactive, onMounted, onBeforeUnmount, nextTick, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { store } from '../../store'
import { EventsOn, EventsOff } from '../../../wailsjs/runtime/runtime'
import { AgentAPI, hasBridge } from './agentApi'
import { renderMarkdown, renderMermaid } from './markdown'
import AgentSettings from './AgentSettings.vue'
import AgentLogs from './AgentLogs.vue'
import ToolCard from './ToolCard.vue'

const config = reactive({
  systemPrompt: '', mode: 'react', maxLoops: 6, contextLimit: 20,
  showThinking: true, enablePolish: false, enableChart: true, temperature: 0.3, currentUserId: '',
  maxToolOutput: 4000, maxFileRead: 200000,
})
const skills = ref([])
const servers = ref([])
const users = ref([])
const messages = ref([])   // 当前会话消息 {id, role, content, thinking, steps, time}
const sessions = ref([])   // 会话列表
const activeSession = ref('')
const globalUsage = ref({ promptTokens: 0, completionTokens: 0, totalTokens: 0 })
const input = ref('')
const running = ref(false)
const settingsVisible = ref(false)
const logsVisible = ref(false)
const showThinkMap = reactive({})  // 消息级思考展开
const bodyRef = ref(null)

// 实时运行态（当前轮次的临时展示）
const live = reactive({ thinking: '', content: '', steps: [] })
const polishing = ref(false) // 是否处于「回答润色」阶段（此时不重打正文，只显示状态）

const currentUserName = computed(() => {
  const u = users.value.find(x => x.id === config.currentUserId)
  return u ? u.name : ''
})
const enabledSkillCount = computed(() => skills.value.filter(s => s.enabled).length)
const enabledServerCount = computed(() => servers.value.filter(s => s.enabled).length)

async function loadAll() {
  if (!hasBridge()) { ElMessage.warning('请在桌面应用内使用 AI Agent'); return }
  try {
    const d = await AgentAPI.load()
    Object.assign(config, d.config || {})
    skills.value = d.skills || []
    servers.value = d.servers || []
    users.value = d.users || []
    sessions.value = d.sessions || []
    activeSession.value = d.activeSession || (sessions.value[0] && sessions.value[0].id) || ''
    globalUsage.value = d.usage || { promptTokens: 0, completionTokens: 0, totalTokens: 0 }
    loadActiveMessages()
    await scrollBottom()
    await nextTick(); renderMermaid(bodyRef.value)
  } catch (e) { ElMessage.error('加载失败：' + String(e)) }
}

// 取当前激活会话的消息
function loadActiveMessages() {
  const s = sessions.value.find(x => x.id === activeSession.value)
  messages.value = (s && s.messages) ? [...s.messages] : []
}

function md(text) { return renderMarkdown(text) }

async function scrollBottom() {
  await nextTick()
  const el = bodyRef.value
  if (el) el.scrollTop = el.scrollHeight
}

let offFns = []
let streamRAF = 0
function scheduleScroll() {
  // 高频 delta 时用 rAF 合并滚动，避免抖动
  if (streamRAF) return
  streamRAF = requestAnimationFrame(() => { streamRAF = 0; const el = bodyRef.value; if (el) el.scrollTop = el.scrollHeight })
}
function bindEvents() {
  // 新一轮流式开始：清空当前正文流（思考区在思考事件里单独维护）
  offFns.push(EventsOn('agent:loop-start', () => { live.content = ''; scheduleScroll() }))
  // 润色开始：后端不再重推正文（避免同一篇回答打两次字），这里只切换到润色态提示
  offFns.push(EventsOn('agent:polish-start', () => {
    live.content = ''
    polishing.value = true
    scheduleScroll()
  }))
  // 流式增量：打字机效果
  offFns.push(EventsOn('agent:delta', (d) => {
    if (!d) return
    if (d.thinking) live.thinking += d.text
    else live.content += d.text
    scheduleScroll()
  }))
  // 思考区整块（收尾，用于去重换行）
  offFns.push(EventsOn('agent:thinking', () => { scheduleScroll() }))
  offFns.push(EventsOn('agent:plan', (t) => { live.steps.push({ type: 'plan', name: '计划', output: t }); scheduleScroll() }))
  offFns.push(EventsOn('agent:step', (s) => {
    // 同一工具（按 name+server）仅保留一条，运行中更新入参，结束更新结果；避免重复卡片
    if (s && (s.type === 'tool' || s.type === 'skill')) {
      const idx = live.steps.findIndex(x => x && (x.type === 'tool' || x.type === 'skill') && x.name === s.name && (x.server || '') === (s.server || ''))
      if (idx >= 0) live.steps[idx] = s
      else live.steps.push(s)
    } else {
      live.steps.push(s)
    }
    scheduleScroll()
  }))
}
function unbindEvents() {
  EventsOff('agent:loop-start'); EventsOff('agent:polish-start'); EventsOff('agent:delta')
  EventsOff('agent:thinking'); EventsOff('agent:plan'); EventsOff('agent:step')
  offFns = []
}

async function send() {
  const text = input.value.trim()
  if (!text || running.value) return
  const s = store.data.settings
  input.value = ''
  running.value = true
  live.thinking = ''; live.content = ''; live.steps = []
  polishing.value = false
  // 本地立即回显用户消息
  messages.value.push({ id: 'u_' + Date.now(), role: 'user', content: text, time: nowStr() })
  await scrollBottom()
  try {
    const res = await AgentAPI.run({
      input: text,
      baseUrl: s.aiBaseUrl, apiKey: s.aiKey, model: s.aiModel, timeoutSec: s.timeoutSec || 60,
    })
    if (res.error) {
      ElMessage.error(res.error)
      messages.value.push({ id: 'e_' + Date.now(), role: 'assistant', content: '⚠️ ' + res.error, time: nowStr() })
    } else {
      messages.value.push({
        id: 'a_' + Date.now(), role: 'assistant',
        content: res.content, thinking: res.thinking, steps: res.steps || [], time: nowStr(),
      })
    }
    await nextTick(); renderMermaid(bodyRef.value)
    await scrollBottom()
    // 刷新会话列表（标题/更新时间）与全局 token 统计
    await refreshSessions()
    if (res.usage) {
      globalUsage.value = {
        promptTokens: globalUsage.value.promptTokens + (res.usage.promptTokens || 0),
        completionTokens: globalUsage.value.completionTokens + (res.usage.completionTokens || 0),
        totalTokens: globalUsage.value.totalTokens + (res.usage.totalTokens || 0),
      }
    }
  } catch (e) {
    ElMessage.error(String(e))
    messages.value.push({ id: 'e_' + Date.now(), role: 'assistant', content: '⚠️ ' + String(e), time: nowStr() })
  } finally {
    running.value = false
    live.thinking = ''; live.content = ''; live.steps = []
    polishing.value = false
  }
}

// 刷新会话列表（从后端重新加载）
async function refreshSessions() {
  try {
    const d = await AgentAPI.load()
    sessions.value = d.sessions || []
    activeSession.value = d.activeSession || activeSession.value
    if (d.usage) globalUsage.value = d.usage
    loadActiveMessages()
  } catch { /* ignore */ }
}

async function createSession() {
  try {
    const id = await AgentAPI.createSession('新会话')
    activeSession.value = id
    await refreshSessions()
    ElMessage.success('已新建会话')
  } catch (e) { ElMessage.error(String(e)) }
}

async function switchSession(id) {
  if (id === activeSession.value) return
  try {
    await AgentAPI.switchSession(id)
    activeSession.value = id
    loadActiveMessages()
    await scrollBottom()
  } catch (e) { ElMessage.error(String(e)) }
}

async function deleteSession(id) {
  try {
    await AgentAPI.deleteSession(id)
    if (id === activeSession.value) {
      await refreshSessions()
    } else {
      await refreshSessions()
    }
    ElMessage.success('已删除会话')
  } catch (e) { ElMessage.error(String(e)) }
}

async function renameSession(id) {
  const s = sessions.value.find(x => x.id === id)
  if (!s) return
  const title = prompt('会话名称', s.title || '')
  if (title == null) return
  try {
    await AgentAPI.renameSession(id, title.trim() || '新会话')
    await refreshSessions()
  } catch (e) { ElMessage.error(String(e)) }
}

async function polish() {
  const text = input.value.trim()
  if (!text) return
  const s = store.data.settings
  try {
    const out = await AgentAPI.polish({ input: text, baseUrl: s.aiBaseUrl, apiKey: s.aiKey, model: s.aiModel, timeoutSec: s.timeoutSec || 60, maxTokens: store.data.agentConfig?.maxTokens || 8000 })
    if (out) input.value = out
    ElMessage.success('已润色')
  } catch (e) { ElMessage.error(String(e)) }
}

async function toggleMode() {
  config.mode = config.mode === 'react' ? 'plan' : 'react'
  try { await AgentAPI.saveConfig(JSON.parse(JSON.stringify(config))) } catch { /* ignore */ }
}

async function clearChat() {
  try {
    await AgentAPI.clearMessages()
    messages.value = []
    ElMessage.success('已清空会话')
  } catch (e) { ElMessage.error(String(e)) }
}

function onSettingsSaved() { loadAll() }

function nowStr() {
  const d = new Date()
  return d.toLocaleTimeString('zh-CN', { hour12: false })
}

function formatTime(iso) {
  if (!iso) return ''
  const d = new Date(iso)
  if (isNaN(d.getTime())) return ''
  return d.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false })
}



function keydown(e) {
  if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) { e.preventDefault(); send() }
}

onMounted(() => { loadAll(); bindEvents() })
onBeforeUnmount(() => { unbindEvents(); if (streamRAF) cancelAnimationFrame(streamRAF) })
</script>

<template>
  <div class="agent-wrap">
    <!-- 会话侧边栏 -->
    <div class="session-sidebar">
      <div class="ss-head">
        <span class="ss-title">会话</span>
        <el-button size="small" circle @click="createSession" title="新建会话">＋</el-button>
      </div>
      <div class="ss-list">
        <div
          v-for="s in sessions"
          :key="s.id"
          class="ss-item"
          :class="{ active: s.id === activeSession }"
          @click="switchSession(s.id)"
        >
          <div class="ss-item-main">
            <div class="ss-item-title">{{ s.title || '新会话' }}</div>
            <div class="ss-item-sub">{{ formatTime(s.updatedAt) }} · {{ (s.usage && s.usage.totalTokens) || 0 }} token</div>
          </div>
          <div class="ss-item-ops" @click.stop>
            <span class="ss-op" title="重命名" @click="renameSession(s.id)">✎</span>
            <span class="ss-op" title="删除" @click="deleteSession(s.id)">🗑</span>
          </div>
        </div>
      </div>
      <div class="ss-foot">
        <div class="ss-usage">
          <div class="ss-usage-row"><span>累计 Token</span><b>{{ globalUsage.totalTokens || 0 }}</b></div>
          <div class="ss-usage-sub">输入 {{ globalUsage.promptTokens || 0 }} · 输出 {{ globalUsage.completionTokens || 0 }}</div>
        </div>
      </div>
    </div>

    <!-- 主区域 -->
    <div class="agent-main">
    <!-- 顶栏 -->
    <div class="agent-bar">
      <div class="left">
        <span class="brand">🤖 AI Agent</span>
        <el-tag size="small" :type="config.mode === 'plan' ? 'warning' : 'success'" @click="toggleMode" class="mode-tag">
          {{ config.mode === 'plan' ? 'Plan 模式' : 'ReAct 模式' }}
        </el-tag>
        <span class="stat">技能 {{ enabledSkillCount }} · MCP {{ enabledServerCount }} · 上下文 {{ config.contextLimit }} · Loop {{ config.maxLoops }}</span>
        <el-tag v-if="currentUserName" size="small" type="info">👤 {{ currentUserName }}</el-tag>
      </div>
      <div class="right">
        <el-button size="small" text @click="logsVisible = true">📊 日志</el-button>
        <el-button size="small" text @click="settingsVisible = true">⚙ 设置</el-button>
        <el-button size="small" text @click="clearChat">🗑 清空</el-button>
      </div>
    </div>

    <!-- 对话区 -->
    <div class="agent-body" ref="bodyRef">
      <div v-if="!messages.length" class="welcome">
        <div class="wc-icon">🤖</div>
        <div class="wc-title">AI Agent 助手</div>
        <div class="wc-sub">支持 Skill 热加载、MCP 工具调用、思考过程、图表输出、ReAct/Plan 模式</div>
      </div>

      <div v-for="m in messages" :key="m.id" class="msg" :class="'role-' + m.role">
        <div class="avatar">{{ m.role === 'user' ? '🧑' : '🤖' }}</div>
        <div class="bubble">
          <!-- 思考过程 -->
          <div v-if="m.role === 'assistant' && m.thinking && config.showThinking" class="think-box">
            <div class="think-head" @click="showThinkMap[m.id] = !showThinkMap[m.id]">
              💭 思考过程 <span class="toggle">{{ showThinkMap[m.id] ? '收起' : '展开' }}</span>
            </div>
            <pre v-show="showThinkMap[m.id]" class="think-content">{{ m.thinking }}</pre>
          </div>
          <!-- 使用的 skill / tool：卡片方式详细展示 -->
          <div v-if="m.role === 'assistant' && m.steps && m.steps.length" class="steps-cards">
            <ToolCard v-for="(s, i) in m.steps.filter(x => x.type !== 'thought')" :key="i" :step="s" />
          </div>
          <!-- 正文（markdown + 图表） -->
          <div class="md-body" v-html="md(m.content)"></div>
          <div class="msg-time">{{ m.time }}</div>
        </div>
      </div>

      <!-- 运行中实时态（流式打字机） -->
      <div v-if="running" class="msg role-assistant">
        <div class="avatar">🤖</div>
        <div class="bubble">
          <div v-if="live.thinking && config.showThinking" class="think-box">
            <div class="think-head">💭 思考中…</div>
            <pre class="think-content">{{ live.thinking }}</pre>
          </div>
          <div v-if="live.steps.length" class="steps-cards">
            <ToolCard v-for="(s, i) in live.steps.filter(x => x.type !== 'thought')" :key="i" :step="s" />
          </div>
          <!-- 流式正文（打字机）；润色阶段不再重打全文，只显示状态 -->
          <div v-if="polishing" class="running-tip polish-tip">
            <span class="dot"></span> ✨ 正在润色回答…
          </div>
          <div v-else-if="live.content" class="md-body streaming" v-html="md(live.content)"></div>
          <div v-else class="running-tip"><span class="dot"></span> Agent 运行中…</div>
        </div>
      </div>
    </div>

    <!-- 输入区 -->
    <div class="agent-input">
      <el-input v-model="input" type="textarea" :rows="3" resize="none"
        placeholder="输入你的需求，Ctrl+Enter 发送。可切换 ReAct/Plan，支持 MCP 工具与技能调用。" @keydown="keydown" />
      <div class="input-actions">
        <div class="left-acts">
          <el-button size="small" text @click="toggleMode" title="切换 ReAct / Plan">
            {{ config.mode === 'plan' ? '📋 Plan' : '🔄 ReAct' }}
          </el-button>
          <el-button size="small" text @click="polish" :disabled="!input.trim() || running" title="AI 润色输入">✨ 润色</el-button>
        </div>
        <el-button type="primary" :loading="running" @click="send" :disabled="!input.trim()">发送</el-button>
      </div>
    </div>

    <AgentSettings v-model:visible="settingsVisible" :config="config" :skills="skills" :servers="servers" :users="users" @saved="onSettingsSaved" />
    <AgentLogs v-model:visible="logsVisible" />
    </div>
  </div>
</template>

<style scoped>
.agent-wrap { flex: 1; display: flex; flex-direction: column; height: 100vh; background: var(--bg); overflow: hidden; }
.agent-bar { display: flex; align-items: center; justify-content: space-between; padding: 8px 16px; background: var(--surface); border-bottom: 1px solid var(--border); }
.agent-bar .left { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.brand { font-weight: 600; font-size: 14px; }
.mode-tag { cursor: pointer; }
.stat { font-size: 12px; color: var(--text-muted); }

.agent-body { flex: 1; overflow-y: auto; padding: 18px; }
.welcome { text-align: center; margin-top: 12vh; color: var(--text-muted); }
.wc-icon { font-size: 52px; }
.wc-title { font-size: 20px; font-weight: 600; margin: 10px 0 6px; color: var(--text); }
.wc-sub { font-size: 13px; }

.msg { display: flex; gap: 10px; margin-bottom: 18px; }
.msg.role-user { flex-direction: row-reverse; }
.avatar { width: 34px; height: 34px; border-radius: 50%; background: var(--surface-2); display: flex; align-items: center; justify-content: center; font-size: 18px; flex-shrink: 0; }
.bubble { max-width: 78%; background: var(--surface); border: 1px solid var(--border); border-radius: 10px; padding: 10px 14px; }
.role-user .bubble { background: var(--el-color-primary-light-9); }
.msg-time { font-size: 11px; color: var(--text-muted); margin-top: 6px; text-align: right; }

.think-box { background: var(--surface-2); border-radius: 8px; padding: 6px 10px; margin-bottom: 8px; border-left: 3px solid #8b5cf6; }
.think-head { font-size: 12px; color: #8b5cf6; cursor: pointer; display: flex; justify-content: space-between; }
.think-head .toggle { color: var(--text-muted); }
.think-content { margin: 6px 0 0; font-size: 12px; color: var(--text-muted); white-space: pre-wrap; word-break: break-word; }

.steps { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 8px; }
.step { display: inline-flex; align-items: center; gap: 4px; background: var(--surface-2); border: 1px solid var(--border); border-radius: 12px; padding: 2px 10px; font-size: 12px; }
.step.err { border-color: #ef4444; color: #ef4444; }
.step-detail-btn { color: var(--primary); cursor: pointer; margin-left: 2px; }
.steps-cards { display: flex; flex-wrap: wrap; align-items: center; margin: 4px 0 8px; }
.pop pre { white-space: pre-wrap; word-break: break-all; max-height: 200px; overflow: auto; background: var(--surface-2); padding: 6px; border-radius: 4px; font-size: 12px; }
.pop .err { color: #ef4444; }

/* 流式打字机：末尾闪烁光标 */
.md-body.streaming :deep(.md-p:last-child)::after,
.md-body.streaming :deep(.md-h:last-child)::after,
.md-body.streaming :deep(.md-ul:last-child li:last-child)::after,
.md-body.streaming :deep(.md-ol:last-child li:last-child)::after {
  content: '▋'; display: inline-block; margin-left: 2px; color: var(--primary);
  animation: blink 1s steps(1) infinite;
}
@keyframes blink { 0%,50% { opacity: 1; } 51%,100% { opacity: 0; } }

.running-tip { font-size: 12px; color: var(--text-muted); display: flex; align-items: center; gap: 6px; }
.polish-tip { color: #8b5cf6; }
.dot { width: 8px; height: 8px; border-radius: 50%; background: var(--primary); animation: pulse 1s infinite; }
.polish-tip .dot { background: #8b5cf6; }
@keyframes pulse { 0%,100% { opacity: .3; } 50% { opacity: 1; } }

.agent-input { padding: 12px 16px; background: var(--surface); border-top: 1px solid var(--border); }
.input-actions { display: flex; justify-content: space-between; align-items: center; margin-top: 8px; }
.left-acts { display: flex; gap: 4px; }

/* markdown 正文样式 */
.md-body :deep(.md-p) { margin: 6px 0; line-height: 1.7; }
.md-body :deep(.md-h) { margin: 10px 0 6px; font-weight: 600; }
.md-body :deep(.md-h1) { font-size: 20px; } .md-body :deep(.md-h2) { font-size: 18px; }
.md-body :deep(.md-h3) { font-size: 16px; } .md-body :deep(.md-h4) { font-size: 14px; }
.md-body :deep(.md-pre) { background: var(--surface-2); padding: 10px; border-radius: 6px; overflow-x: auto; font-size: 13px; }
.md-body :deep(.md-code-inline) { background: var(--surface-2); padding: 1px 5px; border-radius: 4px; font-family: monospace; font-size: 13px; }
.md-body :deep(.md-ul), .md-body :deep(.md-ol) { padding-left: 22px; margin: 6px 0; }
.md-body :deep(.md-table) { border-collapse: collapse; width: 100%; margin: 8px 0; font-size: 13px; }
.md-body :deep(.md-table th), .md-body :deep(.md-table td) { border: 1px solid var(--border); padding: 6px 10px; text-align: left; }
.md-body :deep(.md-table th) { background: var(--surface-2); }
.md-body :deep(.md-quote) { border-left: 3px solid var(--border); padding-left: 12px; color: var(--text-muted); margin: 8px 0; }
.md-body :deep(.md-hr) { border: none; border-top: 1px solid var(--border); margin: 12px 0; }
.md-body :deep(.md-mermaid) { text-align: center; margin: 10px 0; background: var(--surface); }
.md-body :deep(.md-mermaid-tip) { font-size: 12px; color: var(--text-muted); }
.md-body :deep(a) { color: var(--primary); }

/* 会话侧边栏 */
.agent-wrap { flex: 1; display: flex; flex-direction: row; height: 100vh; background: var(--bg); overflow: hidden; }
.session-sidebar { width: 230px; flex-shrink: 0; background: var(--surface); border-right: 1px solid var(--border); display: flex; flex-direction: column; }
.ss-head { display: flex; align-items: center; justify-content: space-between; padding: 12px 14px; border-bottom: 1px solid var(--border); }
.ss-title { font-weight: 600; font-size: 14px; }
.ss-list { flex: 1; overflow-y: auto; padding: 8px; }
.ss-item { display: flex; align-items: center; gap: 6px; padding: 8px 10px; border-radius: 8px; cursor: pointer; margin-bottom: 4px; border: 1px solid transparent; }
.ss-item:hover { background: var(--surface-2); }
.ss-item.active { background: var(--el-color-primary-light-9); border-color: var(--primary); }
.ss-item-main { flex: 1; min-width: 0; }
.ss-item-title { font-size: 13px; color: var(--text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.ss-item-sub { font-size: 11px; color: var(--text-muted); margin-top: 2px; }
.ss-item-ops { display: flex; gap: 4px; opacity: 0; transition: opacity .15s; }
.ss-item:hover .ss-item-ops { opacity: 1; }
.ss-op { cursor: pointer; font-size: 13px; padding: 0 2px; }
.ss-op:hover { color: var(--primary); }
.ss-foot { padding: 10px 14px; border-top: 1px solid var(--border); }
.ss-usage-row { display: flex; justify-content: space-between; font-size: 13px; }
.ss-usage-row b { color: var(--primary); }
.ss-usage-sub { font-size: 11px; color: var(--text-muted); margin-top: 2px; }

/* 主区域铺满剩余空间 */
.agent-main { flex: 1; display: flex; flex-direction: column; min-width: 0; height: 100vh; }
</style>
