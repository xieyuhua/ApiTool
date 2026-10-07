<script setup>
import { ref, reactive, onMounted, onBeforeUnmount, nextTick, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { store } from '../../store'
import { EventsOn, EventsOff } from '../../../wailsjs/runtime/runtime'
import { AgentAPI, hasBridge, isWebUI } from './agentApi'
import { renderMarkdown, renderMermaid } from './markdown'
import AgentSettings from './AgentSettings.vue'
import AgentWebChat from './AgentWebChat.vue'
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
const input = ref('')
const running = ref(false)
const settingsVisible = ref(false)
const logsVisible = ref(false)
const webChatVisible = ref(false)
const exporting = ref(false)   // 导出中（避免重复点击）
const ssOpen = ref(false)      // 小屏下会话抽屉是否展开（桌面端恒为 false，样式保证不生效）
const isNarrow = ref(false)    // 窄屏（≤820px，移动端）标记：顶栏折叠为「⋯」、简化输入提示
const showThinkMap = reactive({})  // 消息级思考展开
const bodyRef = ref(null)

// 窄屏判定：与样式中的 @media (max-width: 820px) 保持一致
function updateNarrow() { isNarrow.value = window.innerWidth <= 820 }

// 窄屏「⋯」菜单命令分发（桌面端按钮直显，无需此路径）
function onNarrowCommand(cmd) {
  if (cmd === 'export-pdf') exportSession(activeSession.value, 'pdf')
  else if (cmd === 'export-html') exportSession(activeSession.value, 'html')
  else if (cmd === 'webchat') webChatVisible.value = true
  else if (cmd === 'logs') logsVisible.value = true
  else if (cmd === 'settings') settingsVisible.value = true
  else if (cmd === 'clear') clearChat()
}

// stepSameCall 判断两个 step 是否属于同一次调用实例。
// 有 callId 时以它为准（后端为每次调用生成唯一值）；
// 缺失时（历史数据）退回按 类型+名称+服务器 归并，宁可少展示也不重复刷屏。
function stepSameCall(a, b) {
  if (!a || !b) return false
  if (a.callId && b.callId) return a.callId === b.callId
  if (a.callId || b.callId) return false
  if (a.type !== b.type) return false
  if (a.name !== b.name) return false
  return (a.server || '') === (b.server || '')
}

// 实时运行态（当前轮次的临时展示）
const live = reactive({ thinking: '', content: '', steps: [] })
const polishing = ref(false) // 是否处于「回答润色」阶段（此时不重打正文，只显示状态）

const currentUserName = computed(() => {
  const u = users.value.find(x => x.id === config.currentUserId)
  return u ? u.name : ''
})
const enabledSkillCount = computed(() => skills.value.filter(s => s.enabled).length)
// 是否在每条回复上显示该轮 token 消耗（设置里开关；旧数据无该字段时默认开启）
const showUsage = computed(() => config.showUsage !== false)

// 当前运行中的实时用量（后端 agent:usage 事件推送），让流式过程中也能看到 token 增长
const liveUsage = ref({ promptTokens: 0, completionTokens: 0, totalTokens: 0 })

// 当前会话累计用量
const sessionUsage = computed(() => {
  const s = sessions.value.find(x => x.id === activeSession.value)
  return (s && s.usage) || { promptTokens: 0, completionTokens: 0, totalTokens: 0 }
})

// token 明细（悬停提示）：输入 / 输出 / 合计
function usageTitle(u) {
  if (!u || !u.totalTokens) return '服务端未返回 token 用量（部分本地/自建模型不支持 stream_options.include_usage）'
  return '本轮消耗\n输入 ' + fmtTokens(u.promptTokens) + '\n输出 ' + fmtTokens(u.completionTokens) + '\n合计 ' + fmtTokens(u.totalTokens)
}

// 数字千分位，便于快速核对
function fmtTokens(n) { return Number(n || 0).toLocaleString('zh-CN') }
const enabledServerCount = computed(() => servers.value.filter(s => s.enabled).length)

// 运行在局域网网页端（此时「开启局域网访问」这类桌面专属入口不再展示）
const webMode = isWebUI()

// 本端（桌面窗口或某个浏览器标签）的稳定标识。
// 后端会把它随 agent:done / agent:sessions-changed 回传，用于区分这次对话是谁发起的：
// 自己发起的走本地流程，不重复刷新；别的端（典型是局域网网页端）发起的才刷新会话并提示，
// 否则两端会互相覆盖对方正在展示的内容。
const MY_CLIENT_ID = 'c_' + Date.now().toString(36) + '_' + Math.random().toString(36).slice(2, 10)

// 别的端正在对话（局域网网页端提问时桌面端能看到），用于顶栏提示
const remoteRunning = ref(false)

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
  offFns.push(EventsOn('agent:loop-start', (d) => {
    live.content = ''; scheduleScroll()
    // 别的端开始了对话：顶栏提示，避免用户以为「没反应」
    remoteRunning.value = !!(d && d.clientId && d.clientId !== MY_CLIENT_ID)
  }))
  // 会话数据变更（后端推送）：局域网网页端提问/新建/删除/切换会话时，桌面端据此刷新。
  // 这是多端共享同一份会话却能实时同步的关键——前端不做轮询，
  // 桌面端原本只在本地 send() 结束后刷新一次，网页端的改动它完全感知不到。
  offFns.push(EventsOn('agent:sessions-changed', async (d) => {
    d = d || {}
    // 自己发起的对话由 send() 自己收尾（会 refreshSessions），这里跳过避免重复请求
    if (d.reason === 'run' && d.clientId === MY_CLIENT_ID) return
    // refreshSessions 默认不跟随后端游标：本端浏览哪个会话是本端自己的视图状态，
    // 不能被局域网网页端的会话操作带走（否则桌面端正在看的记录会「消失」）。
    await refreshSessions()
    if (d.reason === 'run') {
      remoteRunning.value = false
      const u = d.usage || {}
      // 只有当新回复写入的正是本端正在浏览的会话时才说明「我这边也该看到它」
      const sameSess = !d.sessionId || d.sessionId === activeSession.value
      ElMessage.success(
        (d.input ? '【' + d.input + '】\n' : '') +
        '收到另一端（局域网网页）的新回复 · 本次 ' + fmtTokens(u.totalTokens) + ' token' +
        (sameSess ? '' : '（在另一个会话，可从左侧会话列表查看）')
      )
    }
  }))
  // 实时 token 用量：每收到一次模型 usage 就刷新，运行中即可看到输入/输出增长
  offFns.push(EventsOn('agent:usage', (u) => {
    if (!u) return
    liveUsage.value = { promptTokens: u.promptTokens || 0, completionTokens: u.completionTokens || 0, totalTokens: u.totalTokens || 0 }
  }))
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
    // 同一次工具调用会发两次事件（开始=仅入参 / 结束=带结果），
    // 依据 callId 合并为一条，避免出现两张一样的卡片；
    // 而模型多次调用同一个工具时 callId 各不相同，会逐条保留，
    // 不会发生「调了 5 次只看到 1 条」的情况。
    // 老数据没有 callId 时回退到「按名称归并」，仅影响历史消息的展示。
    const s0 = live.steps.findIndex(x => x && stepSameCall(x, s))
    if (s0 >= 0) live.steps[s0] = s
    else live.steps.push(s)
    scheduleScroll()
  }))
}
function unbindEvents() {
  EventsOff('agent:loop-start'); EventsOff('agent:polish-start'); EventsOff('agent:delta')
  EventsOff('agent:thinking'); EventsOff('agent:plan'); EventsOff('agent:step')
  EventsOff('agent:usage'); EventsOff('agent:sessions-changed')
  offFns = []
}

async function send() {
  const text = input.value.trim()
  if (!text || running.value) return
  const s = store.data.settings
  input.value = ''
  running.value = true
  live.thinking = ''; live.content = ''; live.steps = []
  liveUsage.value = { promptTokens: 0, completionTokens: 0, totalTokens: 0 }
  polishing.value = false
  // 本地立即回显用户消息
  messages.value.push({ id: 'u_' + Date.now(), role: 'user', content: text, time: nowStr() })
  await scrollBottom()
  try {
    const res = await AgentAPI.run({
      input: text,
      baseUrl: s.aiBaseUrl, apiKey: s.aiKey, model: s.aiModel, timeoutSec: s.timeoutSec || 180,
      clientId: MY_CLIENT_ID,
      sessionId: activeSession.value,
    })
    if (res.error) {
      ElMessage.error(res.error)
      messages.value.push({ id: 'e_' + Date.now(), role: 'assistant', content: '⚠️ ' + res.error, time: nowStr(), usage: res.usage || null })
    } else {
      messages.value.push({
        id: 'a_' + Date.now(), role: 'assistant',
        content: res.content, thinking: res.thinking, steps: res.steps || [], time: nowStr(),
        usage: res.usage || null,
      })
    }
    await nextTick(); renderMermaid(bodyRef.value)
    await scrollBottom()
    // 刷新会话列表（标题/更新时间）与 token 统计；保持本端正在浏览的会话
    await refreshSessions()

  } catch (e) {
    ElMessage.error(String(e))
    messages.value.push({ id: 'e_' + Date.now(), role: 'assistant', content: '⚠️ ' + String(e), time: nowStr() })
  } finally {
    running.value = false
    live.thinking = ''; live.content = ''; live.steps = []
    polishing.value = false
  }
}

// 刷新会话列表（从后端重新加载）。
//
// 注意：后端只有一个**全局共享**的 ActiveSession 游标（多端共用同一份会话数据），
// 而各端浏览哪一会话本应是**各自的视图状态**。所以这里默认**不**跟随后端游标 ——
// 否则局域网网页端新建/切换会话时，会把桌面端正在浏览的会话强行带走，
// 表现为「桌面版之前的记录突然没了」（数据其实没丢，切回该会话即可见）。
// 仅当本端会话已被另一端删除时才回退到后端游标。
async function refreshSessions(opts) {
  const followBackend = !!(opts && opts.followBackend)
  try {
    const d = await AgentAPI.load()
    sessions.value = d.sessions || []
    const stillExists = sessions.value.some(s => s.id === activeSession.value)
    if (followBackend || !stillExists) {
      activeSession.value = d.activeSession || (sessions.value[0] && sessions.value[0].id) || ''
    }
    loadActiveMessages()
  } catch { /* ignore */ }
}

async function createSession() {
  try {
    const id = await AgentAPI.createSession('新会话')
    activeSession.value = id
    // 本端主动新建：后端游标已指向新会话，跟随它
    await refreshSessions({ followBackend: true })
    ElMessage.success('已新建会话')
  } catch (e) { ElMessage.error(String(e)) }
}

async function switchSession(id) {
  if (id === activeSession.value) { ssOpen.value = false; return }
  try {
    await AgentAPI.switchSession(id)
    activeSession.value = id
    loadActiveMessages()
    await scrollBottom()
    ssOpen.value = false   // 小屏：切换后自动收起抽屉
  } catch (e) { ElMessage.error(String(e)) }
}

async function deleteSession(id) {
  try {
    await AgentAPI.deleteSession(id)
    // 被删的正是本端在看的会话时需要跟随后端回退目标，否则停留在已不存在的会话上
    await refreshSessions({ followBackend: id === activeSession.value })
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

// 导出会话记录：format = 'pdf' | 'html'，id 为空时导出当前会话
async function exportSession(id, format) {
  const sid = id || activeSession.value
  if (exporting.value) return
  if (!sid) { ElMessage.warning('没有可导出的会话'); return }
  const s = sessions.value.find(x => x.id === sid)
  if (s && !(s.messages || []).length) { ElMessage.warning('当前会话还没有内容'); return }
  exporting.value = true
  try {
    const path = await AgentAPI.exportSession(sid, format)
    if (path) ElMessage.success('已导出：' + path)
  } catch (e) {
    ElMessage.error('导出失败：' + String(e))
    if (format === 'pdf') ElMessage.warning('可改用「导出 HTML」，再用浏览器打开并打印为 PDF')
  } finally {
    exporting.value = false
  }
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

onMounted(() => {
  loadAll(); bindEvents()
  updateNarrow()
  window.addEventListener('resize', updateNarrow)
})
onBeforeUnmount(() => {
  unbindEvents()
  if (streamRAF) cancelAnimationFrame(streamRAF)
  window.removeEventListener('resize', updateNarrow)
})
</script>

<template>
  <div class="agent-wrap">
    <!-- 小屏会话抽屉遮罩 -->
    <div v-if="ssOpen" class="ss-mask" @click="ssOpen = false"></div>
    <!-- 会话侧边栏（小屏变抽屉，见文末响应式样式） -->
    <div class="session-sidebar" :class="{ open: ssOpen }">
      <div class="ss-head">
        <span class="ss-title">会话</span>
        <div class="ss-head-ops">
          <el-button size="small" circle @click="createSession" title="新建会话">＋</el-button>
          <span class="ss-close" title="关闭" @click="ssOpen = false">✕</span>
        </div>
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
            <div class="ss-item-sub">{{ formatTime(s.updatedAt) }} · 合计 {{ fmtTokens((s.usage && s.usage.totalTokens) || 0) }}</div>
            <div class="ss-item-sub ss-item-tok" v-if="s.usage && s.usage.totalTokens">
              ↑输入 {{ fmtTokens(s.usage.promptTokens) }} · ↓输出 {{ fmtTokens(s.usage.completionTokens) }}
            </div>
          </div>
          <div class="ss-item-ops" @click.stop>
            <span class="ss-op" title="导出该会话为 HTML" @click="exportSession(s.id, 'html')">⬇</span>
            <span class="ss-op" title="重命名" @click="renameSession(s.id)">✎</span>
            <span class="ss-op" title="删除" @click="deleteSession(s.id)">🗑</span>
          </div>
        </div>
      </div>
      <div class="ss-foot">
        <!-- 只展示当前会话的 token 开销（输入 / 输出 / 合计），不再展示全局累计 -->
        <div class="ss-usage">
          <div class="ss-usage-row"><span>当前会话 Token</span><b>{{ fmtTokens(sessionUsage.totalTokens) }}</b></div>
          <div class="ss-usage-sub">输入 {{ fmtTokens(sessionUsage.promptTokens) }} · 输出 {{ fmtTokens(sessionUsage.completionTokens) }}</div>
        </div>
      </div>
    </div>

    <!-- 主区域 -->
    <div class="agent-main">
    <!-- 顶栏 -->
    <div class="agent-bar">
      <div class="left">
        <span class="ss-toggle" title="会话列表" @click="ssOpen = !ssOpen">☰</span>
        <span class="brand">🤖 AI Agent</span>
        <el-tag size="small" :type="config.mode === 'plan' ? 'warning' : 'success'" @click="toggleMode" class="mode-tag">
          {{ config.mode === 'plan' ? 'Plan 模式' : 'ReAct 模式' }}
        </el-tag>
        <span class="stat">技能 {{ enabledSkillCount }} · MCP {{ enabledServerCount }} · 上下文 {{ config.contextLimit }} · Loop {{ config.maxLoops }}</span>
        <!-- 运行中：实时展示本轮已消耗的 token（输入 / 输出 / 合计） -->
        <span class="stat token-live" v-if="running">本轮 ↑{{ fmtTokens(liveUsage.promptTokens) }} ↓{{ fmtTokens(liveUsage.completionTokens) }} ⚡{{ fmtTokens(liveUsage.totalTokens) }}</span>
        <!-- 局域网网页端正在对话时的提示（桌面端旁观时可见） -->
        <span class="stat token-remote" v-if="remoteRunning && !running">📱 局域网网页端正在对话…</span>
        <el-tag v-if="currentUserName" size="small" type="info">👤 {{ currentUserName }}</el-tag>
      </div>
      <div class="right">
        <template v-if="!isNarrow">
        <el-dropdown trigger="click" @command="f => exportSession(activeSession, f)">
          <el-button size="small" text :loading="exporting">📤 导出</el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="pdf">导出为 PDF（不可再编辑）</el-dropdown-item>
              <el-dropdown-item command="html">导出为 HTML（可分享/再打印）</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
        <el-button size="small" text @click="webChatVisible = true" v-if="!webMode">📱 局域网</el-button>
        <el-button size="small" text @click="logsVisible = true">📊 日志</el-button>
        <el-button size="small" text @click="settingsVisible = true">⚙ 设置</el-button>
        <el-button size="small" text @click="clearChat">🗑 清空</el-button>
        </template>
        <!-- 窄屏：全部收进「⋯」，避免顶栏换行挤掉按钮 -->
        <el-dropdown v-else trigger="click" @command="onNarrowCommand">
          <el-button size="small" text :loading="exporting">⋯</el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="export-pdf">📄 导出为 PDF</el-dropdown-item>
              <el-dropdown-item command="export-html">📄 导出为 HTML</el-dropdown-item>
              <el-dropdown-item command="webchat" v-if="!webMode" divided>📱 局域网访问</el-dropdown-item>
              <el-dropdown-item command="logs">📊 日志</el-dropdown-item>
              <el-dropdown-item command="settings">⚙ 设置</el-dropdown-item>
              <el-dropdown-item command="clear" divided>🗑 清空会话</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
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
          <!-- 思考过程（含工具 / skill 调用） -->
          <div v-if="m.role === 'assistant' && ((m.thinking && config.showThinking) || (m.steps && m.steps.filter(x => x.type !== 'thought').length))" class="think-box">
            <div class="think-head" @click="showThinkMap[m.id] = !showThinkMap[m.id]">
              💭 思考过程 <span class="toggle">{{ showThinkMap[m.id] ? '收起' : '展开' }}</span>
            </div>
            <pre v-show="showThinkMap[m.id]" class="think-content">{{ m.thinking }}</pre>
            <!-- 使用的 skill / tool：作为思考过程的一部分展示 -->
            <div v-show="showThinkMap[m.id]" v-if="m.steps && m.steps.length" class="steps-cards">
              <ToolCard v-for="(s, i) in m.steps.filter(x => x.type !== 'thought')" :key="s.callId || ('k' + i)" :step="s" />
            </div>
          </div>
          <!-- 正文（markdown + 图表） -->
          <div class="md-body" v-html="md(m.content)"></div>
          <!-- 页脚：每轮 token 开销 + 时间。用虚线与正文分隔，留出呼吸感 -->
          <div class="msg-foot" :class="{ 'has-usage': m.role === 'assistant' }">
            <span class="msg-usage" v-if="m.role === 'assistant' && showUsage"
              :class="{ dim: !(m.usage && m.usage.totalTokens) }" :title="usageTitle(m.usage)">
              <template v-if="m.usage && m.usage.totalTokens">
                <span class="tk"><i>输入</i>{{ fmtTokens(m.usage.promptTokens) }}</span>
                <span class="tk"><i>输出</i>{{ fmtTokens(m.usage.completionTokens) }}</span>
                <span class="tk total"><i>合计</i>{{ fmtTokens(m.usage.totalTokens) }}</span>
              </template>
              <span v-else class="tk-none">🔢 服务端未返回 Token 用量</span>
            </span>
            <span class="msg-time">{{ m.time }}</span>
          </div>
        </div>
      </div>

      <!-- 运行中实时态（流式打字机） -->
      <div v-if="running" class="msg role-assistant">
        <div class="avatar">🤖</div>
        <div class="bubble">
          <div v-if="live.thinking && config.showThinking || live.steps.length" class="think-box">
            <div class="think-head">💭 思考中…</div>
            <pre v-if="live.thinking && config.showThinking" class="think-content">{{ live.thinking }}</pre>
            <div v-if="live.steps.length" class="steps-cards">
              <ToolCard v-for="(s, i) in live.steps.filter(x => x.type !== 'thought')" :key="s.callId || ('k' + i)" :step="s" />
            </div>
          </div>
          <!-- 流式正文（打字机）；润色阶段不再重打全文，只显示状态 -->
          <div v-if="polishing" class="running-tip polish-tip">
            <span class="dot"></span> ✨ 正在润色回答…
          </div>
          <div v-else-if="live.content" class="md-body streaming" v-html="md(live.content)"></div>
          <div v-else class="running-tip"><span class="dot"></span> Agent 运行中…</div>
          <div class="msg-foot has-usage" v-if="showUsage">
            <span class="msg-usage live" title="本轮已消耗（模型返回实时上报）">
              <span class="tk"><i>输入</i>{{ fmtTokens(liveUsage.promptTokens) }}</span>
              <span class="tk"><i>输出</i>{{ fmtTokens(liveUsage.completionTokens) }}</span>
              <span class="tk total"><i>合计</i>{{ fmtTokens(liveUsage.totalTokens) }}</span>
            </span>
          </div>
        </div>
      </div>
    </div>

    <!-- 输入区 -->
    <div class="agent-input">
      <el-input v-model="input" type="textarea" :rows="isNarrow ? 2 : 3" resize="none"
        :placeholder="isNarrow ? '输入你的需求…' : '输入你的需求，Ctrl+Enter 发送。可切换 ReAct/Plan，支持 MCP 工具与技能调用。'" @keydown="keydown" />
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
    <AgentWebChat v-model:visible="webChatVisible" />
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
.stat.token-live { color: var(--primary); font-variant-numeric: tabular-nums; }
.stat.token-remote { color: #d97706; }

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
/* 页脚：与正文之间用留白 + 虚线分隔，避免 token 徽章紧贴最后一段文字 */
.msg-foot { display: flex; align-items: center; justify-content: flex-end; gap: 8px; margin-top: 4px; }
.msg-foot.has-usage { margin-top: 12px; padding-top: 9px; border-top: 1px dashed var(--border); }
/* 正文最后一段/标题去掉下边距，避免与分隔线叠在一起留双层空隙 */
.md-body :deep(> *:last-child) { margin-bottom: 0 !important; }

/* token 用量徽章：输入 / 输出 / 合计 三段式，等宽数字 */
.msg-usage {
  display: inline-flex; align-items: center; gap: 6px; cursor: help;
  font-size: 11px; line-height: 1;
}
.msg-usage .tk {
  display: inline-flex; align-items: baseline; gap: 3px;
  padding: 3px 7px; border-radius: 6px;
  background: var(--surface-2); border: 1px solid var(--border);
  color: var(--text); font-variant-numeric: tabular-nums; white-space: nowrap;
}
.msg-usage .tk i { font-style: normal; font-size: 10px; color: var(--text-muted); }
.msg-usage .tk.total { background: var(--el-color-primary-light-9); border-color: var(--primary); }
.msg-usage .tk.total i, .msg-usage .tk.total { color: var(--primary); }
.msg-usage .tk-none { padding: 3px 7px; border-radius: 6px; background: var(--surface-2); border: 1px dashed var(--border); color: var(--text-muted); }
.msg-usage.dim .tk-none { border-style: dashed; }
/* 运行中：虚线边框 + 轻微脉冲，提示数值还在增长 */
.msg-usage.live .tk { border-style: dashed; animation: tkPulse 1.6s ease-in-out infinite; }
.msg-usage.live .tk.total { border-style: solid; }
@keyframes tkPulse { 0%,100% { opacity: 1 } 50% { opacity: .55 } }

.msg-time { font-size: 11px; color: var(--text-muted); text-align: right; flex-shrink: 0; }

.think-box { background: var(--surface-2); border-radius: 8px; padding: 6px 10px; margin-bottom: 8px; border-left: 3px solid #8b5cf6; }
.think-head { font-size: 12px; color: #8b5cf6; cursor: pointer; display: flex; justify-content: space-between; }
.think-head .toggle { color: var(--text-muted); }
.think-content { margin: 6px 0 0; font-size: 12px; color: var(--text-muted); white-space: pre-wrap; word-break: break-word; }

.steps { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 8px; }
.step { display: inline-flex; align-items: center; gap: 4px; background: var(--surface-2); border: 1px solid var(--border); border-radius: 12px; padding: 2px 10px; font-size: 12px; }
.step.err { border-color: #ef4444; color: #ef4444; }
.step-detail-btn { color: var(--primary); cursor: pointer; margin-left: 2px; }
.steps-cards { display: flex; flex-direction: column; align-items: stretch; gap: 4px; margin: 4px 0 8px; }
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
.ss-item-tok { font-variant-numeric: tabular-nums; }
.ss-item-ops { display: flex; gap: 4px; opacity: 0; transition: opacity .15s; }
.ss-item:hover .ss-item-ops { opacity: 1; }
.ss-op { cursor: pointer; font-size: 13px; padding: 0 2px; }
.ss-op:hover { color: var(--primary); }
.ss-foot { padding: 10px 14px; border-top: 1px solid var(--border); }
.ss-usage-row { display: flex; justify-content: space-between; font-size: 13px; }
.ss-usage-row b { color: var(--primary); font-variant-numeric: tabular-nums; }
.ss-usage-sub { font-size: 11px; color: var(--text-muted); margin-top: 2px; font-variant-numeric: tabular-nums; }

/* 主区域铺满剩余空间 */
.agent-main { flex: 1; display: flex; flex-direction: column; min-width: 0; height: 100vh; }

/* ==========================================================================
   移动端响应式（局域网 Agent 网页）
   桌面端窗口最小 1080px，以下规则只在窄屏生效，桌面端布局与行为完全不变。
   ========================================================================== */

/* 会话抽屉开关与遮罩：宽屏隐藏 */
.ss-toggle, .ss-close, .ss-mask { display: none; }

@media (max-width: 820px) {
  .agent-wrap { flex-direction: row; }
  .agent-main { height: 100dvh; }

  /* 会话侧栏 → 左侧抽屉 */
  .session-sidebar {
    position: fixed; left: 0; top: 0; bottom: 0; z-index: 1200;
    width: 78vw; max-width: 300px; height: 100dvh;
    transform: translateX(-102%); transition: transform .22s ease;
    box-shadow: 2px 0 16px rgba(0,0,0,.18); padding-top: env(safe-area-inset-top);
  }
  .session-sidebar.open { transform: translateX(0); }
  .ss-mask { display: block; position: fixed; inset: 0; z-index: 1100; background: rgba(0,0,0,.38); }
  .ss-toggle { display: inline-flex; align-items: center; justify-content: center;
    width: 34px; height: 34px; border-radius: 8px; font-size: 17px;
    background: var(--surface-2); border: 1px solid var(--border); cursor: pointer; flex-shrink: 0; }
  .ss-toggle:active { background: var(--el-color-primary-light-9); }
  .ss-close { display: inline-flex; align-items: center; justify-content: center;
    width: 28px; height: 28px; border-radius: 6px; font-size: 14px; color: var(--text-muted); cursor: pointer; }
  .ss-head-ops { display: flex; align-items: center; gap: 6px; }

  /* 会话项在小屏常显操作按钮（无 hover） */
  .ss-item-ops { opacity: 1; }
  .ss-op { font-size: 15px; padding: 4px 6px; border-radius: 6px; }
  .ss-op:active { background: var(--surface-2); }

  /* 顶栏压缩 */
  .agent-bar { padding: 6px 10px; gap: 6px; flex-wrap: nowrap; }
  .agent-bar .left { gap: 6px; flex-wrap: nowrap; min-width: 0; }
  .agent-bar .right { gap: 2px; flex-shrink: 0; }
  .agent-bar :deep(.mode-tag) { flex-shrink: 0; }
  .brand { font-size: 13px; }
  .stat { display: none; }             /* 技能/MCP/上下文等统计在窄屏隐藏 */
  .stat.token-live { display: inline; } /* 但本轮 token 用量始终可见 */
  .agent-bar :deep(.el-button) { padding: 6px 6px; font-size: 12px; }

  /* 对话区 */
  .agent-body { padding: 12px 10px; }
  .msg { gap: 6px; margin-bottom: 14px; }
  .avatar { width: 26px; height: 26px; font-size: 14px; }
  .bubble { max-width: calc(100% - 32px); padding: 9px 11px; border-radius: 10px; }
  /* 窄屏：徽章换行排列，避免横向撑破气泡 */
  .msg-usage { flex-wrap: wrap; justify-content: flex-end; gap: 4px; }
  .msg-foot.has-usage { margin-top: 10px; padding-top: 8px; }
  .msg-usage .tk { padding: 2px 6px; }
  .think-content { font-size: 12px; max-height: 40vh; overflow: auto; }
  .md-body :deep(.md-pre) { font-size: 12px; padding: 8px; }
  /* 表格 / 代码块在窄屏横向滚动，避免撑破布局 */
  .md-body :deep(.md-table) { display: block; overflow-x: auto; white-space: nowrap; }
  .md-body :deep(.md-mermaid) { overflow-x: auto; }

  /* 输入区：适配刘海屏底部手势条 */
  .agent-input { padding: 8px 10px; padding-bottom: calc(8px + env(safe-area-inset-bottom)); }
  .input-actions { margin-top: 6px; }
  .welcome { margin-top: 18vh; }
  .wc-icon { font-size: 40px; }
  .wc-title { font-size: 17px; }
  .wc-sub { font-size: 12px; padding: 0 12px; }
}
</style>
