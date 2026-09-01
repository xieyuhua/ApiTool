<script setup>
import { ref, reactive, watch, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { AgentAPI } from './agentApi'
import { pluginConnections, addPluginConn, updatePluginConn, removePluginConn } from '../../store.js'
import {
  PluginTest, PluginDBDatabases, PluginDBTables, PluginDBColumns,
} from '../../../wailsjs/go/main/App'

const props = defineProps({
  visible: Boolean,
  config: Object,
  skills: Array,
  servers: Array,
  users: Array,
})
const emit = defineEmits(['update:visible', 'saved'])

const tab = ref('config')

// 内置工具清单【唯一数据源在后端 BuiltinToolMeta()】。前端不硬编码，完全由
// GetBuiltinTools() 拉取，确保新增/修改工具只改后端一处即可，前后端始终一致。
const toolList = ref([])

// 从后端拉取内置工具元信息（设置页唯一来源，无本地兜底副本）
async function loadBuiltinTools() {
  const list = await AgentAPI.getBuiltinTools()
  if (Array.isArray(list)) {
    toolList.value = list
  }
}

const local = reactive({
  config: {},
  skills: [],
  servers: [],
  users: [],
})

// 工具描述编辑状态：key=工具名，true 表示正在编辑（默认只读查看）
const editing = reactive({})

watch(() => props.visible, async (v) => {
  if (v) {
    // 先拉取内置工具清单（唯一数据源），再基于它填充配置，避免竞态导致工具为空
    try {
      await loadBuiltinTools()
    } catch (e) {
      console.warn('拉取内置工具失败', e)
    }
    local.config = JSON.parse(JSON.stringify(props.config || {}))
    // 工具开关：从旧版的分组布尔迁移为各工具独立 enabled/desc
    if (typeof local.config.tools !== 'object' || local.config.tools === null) {
      local.config.tools = {}
    }
    if (!local.config.tools.enabled || typeof local.config.tools.enabled !== 'object') {
      local.config.tools.enabled = {}
    }
    if (!local.config.tools.desc || typeof local.config.tools.desc !== 'object') {
      local.config.tools.desc = {}
    }
    const old = local.config.tools
    const hadOldGroup = typeof old.fileOp === 'boolean' || typeof old.webSearch === 'boolean' ||
      typeof old.sysInfo === 'boolean' || typeof old.common === 'boolean' || typeof old.dbAnalysis === 'boolean'
    const groupOn = {
      '文件操作': !hadOldGroup ? true : !!old.fileOp,
      '网页搜索': !hadOldGroup ? true : !!old.webSearch,
      '系统信息': !hadOldGroup ? true : !!old.sysInfo,
      '常用工具': !hadOldGroup ? true : !!old.common,
      '数据库连接分析': !hadOldGroup ? true : !!old.dbAnalysis,
    }
    for (const t of toolList.value) {
      if (typeof old.enabled[t.name] !== 'boolean') {
        local.config.tools.enabled[t.name] = groupOn[t.group]
      } else {
        local.config.tools.enabled[t.name] = !!old.enabled[t.name]
      }
      if (typeof old.desc[t.name] !== 'string' || old.desc[t.name] === '') {
        local.config.tools.desc[t.name] = t.def
      }
    }
    // 数据库连接分析总开关：与 db_schema/db_query 两个工具的开启状态保持一致（任一开启即视为开启）
    local.config.enableDBAnalysis = !!old.dbAnalysis ||
      local.config.tools.enabled['db_schema'] || local.config.tools.enabled['db_query']
    delete local.config.tools.fileOp
    delete local.config.tools.webSearch
    delete local.config.tools.sysInfo
    delete local.config.tools.common
    delete local.config.tools.dbAnalysis
    local.config.maxToolOutput = local.config.maxToolOutput || 4000
    local.config.maxFileRead = local.config.maxFileRead || 200000
    local.config.maxTokens = local.config.maxTokens || 8000
    local.skills = JSON.parse(JSON.stringify(props.skills || []))
    local.servers = JSON.parse(JSON.stringify(props.servers || []))
    local.users = JSON.parse(JSON.stringify(props.users || []))
  }
})

function close() { emit('update:visible', false) }

// 数据库连接分析总开关变化时，联动两个 DB 工具的独立开关
function syncDBAnalysis(v) {
  local.config.tools.enabled['db_schema'] = v
  local.config.tools.enabled['db_query'] = v
}

// ---- 工具描述编辑 ----
function startEdit(name) { editing[name] = true }
function saveEdit(name) {
  editing[name] = false
  ElMessage.success('已更新工具描述')
}
function cancelEdit(name) {
  editing[name] = false
  // 还原为当前配置中的值（丢弃未保存修改）
  for (const t of toolList.value) {
    if (t.name === name) {
      local.config.tools.desc[name] = t.def
    }
  }
}

// ---- 技能 ----
function addSkill() {
  local.skills.push({ id: '', name: '新技能', description: '', prompt: '', enabled: true })
}
function removeSkill(i) { local.skills.splice(i, 1) }

// ---- MCP 服务器 ----
function addServer() {
  local.servers.push({ id: '', name: '新服务器', transport: 'stdio', command: '', args: [], env: {}, url: '', headers: {}, enabled: true })
}
function removeServer(i) { local.servers.splice(i, 1) }
function argsText(srv) { return (srv.args || []).join(' ') }
function setArgs(srv, v) { srv.args = v.split(/\s+/).filter(Boolean) }
function kvText(obj) { return Object.entries(obj || {}).map(([k, v]) => k + '=' + v).join('\n') }
function setKv(srv, field, v) {
  const o = {}
  v.split('\n').forEach(line => {
    const idx = line.indexOf('=')
    if (idx > 0) o[line.slice(0, idx).trim()] = line.slice(idx + 1).trim()
  })
  srv[field] = o
}

const testing = ref('')
async function testServer(srv) {
  testing.value = srv.name
  try {
    const tools = await AgentAPI.testServer(JSON.parse(JSON.stringify(srv)))
    ElMessage.success(`连接成功，发现 ${tools.length} 个工具：` + tools.map(t => t.name).join(', '))
  } catch (e) {
    ElMessage.error('连接失败：' + String(e))
  } finally { testing.value = '' }
}

// ---- 用户 ----
function addUser() { local.users.push({ id: '', name: '新用户', token: '', roles: [] }) }
function removeUser(i) { local.users.splice(i, 1) }
function rolesText(u) { return (u.roles || []).join(',') }
function setRoles(u, v) { u.roles = v.split(',').map(s => s.trim()).filter(Boolean) }

async function saveAll() {
  try {
    // 保存前将总开关状态同步到两个 DB 工具
    syncDBAnalysis(!!local.config.enableDBAnalysis)
    await AgentAPI.saveConfig(JSON.parse(JSON.stringify(local.config)))
    await AgentAPI.saveSkills(JSON.parse(JSON.stringify(local.skills)))
    await AgentAPI.saveServers(JSON.parse(JSON.stringify(local.servers)))
    await AgentAPI.saveUsers(JSON.parse(JSON.stringify(local.users)))
    ElMessage.success('已保存并热加载')
    emit('saved')
    close()
  } catch (e) {
    ElMessage.error('保存失败：' + String(e))
  }
}

// ===================== 数据库连接列表（供运行配置「激活分析连接」下拉使用） =====================
const dbConns = computed(() => (pluginConnections() || []).filter(c => c.category === 'db'))
// call 封装：wails 函数可能返回 Promise 或带 .then 的对象
function call(fn, ...args) {
  const r = fn(...args)
  return r && typeof r.then === 'function' ? r : Promise.resolve(r)
}
</script>

<template>
  <el-drawer :model-value="visible" @update:model-value="emit('update:visible', $event)" title="Agent 设置" size="620px" :destroy-on-close="false">
    <el-tabs v-model="tab">
      <!-- 运行配置 -->
      <el-tab-pane label="运行配置" name="config">
        <div class="form-block">
          <label>自定义系统提示词</label>
          <el-input v-model="local.config.systemPrompt" type="textarea" :rows="4" placeholder="设定 Agent 的角色与行为" />
        </div>
        <div class="form-row">
          <div class="form-col">
            <label>运行模式</label>
            <el-radio-group v-model="local.config.mode">
              <el-radio-button label="react">ReAct</el-radio-button>
              <el-radio-button label="plan">Plan</el-radio-button>
            </el-radio-group>
          </div>
          <div class="form-col">
            <label>当前登录用户（传入 MCP 区分权限）</label>
            <el-select v-model="local.config.currentUserId" clearable placeholder="未指定" style="width:100%">
              <el-option v-for="u in local.users" :key="u.id || u.name" :label="u.name" :value="u.id" />
            </el-select>
          </div>
        </div>
        <div class="form-row">
          <div class="form-col">
            <label>Agent Loop 最大轮数</label>
            <el-input-number v-model="local.config.maxLoops" :min="1" :max="30000" />
          </div>
          <div class="form-col">
            <label>加载最近上下文条数</label>
            <el-input-number v-model="local.config.contextLimit" :min="0" :max="20000" />
          </div>
          <div class="form-col">
            <label>温度</label>
            <el-input-number v-model="local.config.temperature" :min="0" :max="2" :step="0.1" />
          </div>
        </div>
        <div class="form-subtitle">内容截断长度（可配置，0 表示使用默认值）</div>
        <div class="form-row">
          <div class="form-col">
            <label>工具输出回灌上限（字符）</label>
            <el-input-number v-model="local.config.maxToolOutput" :min="0" :max="200000" :step="500" />
            <div class="hint">工具执行结果回传给模型前的最大字符数，默认 4000。调大可让模型看到更完整的工具结果。</div>
          </div>
          <div class="form-col">
            <label>文件读取上限（字符）</label>
            <el-input-number v-model="local.config.maxFileRead" :min="0" :max="2000000" :step="10000" />
            <div class="hint">read_file 等内置文件工具读取的最大字符数，默认 200000。</div>
          </div>
          <div class="form-col">
            <label>回复长度上限（token）</label>
            <el-input-number v-model="local.config.maxTokens" :min="0" :max="200000" :step="1000" />
            <div class="hint">模型回复最大 token 数，默认 8000。若 AI 回复总被截断、显示不全，请调大此项（0=模型默认值，可能偏短）。</div>
          </div>
        </div>
        <div class="form-row switches">
          <el-switch v-model="local.config.showThinking" active-text="输出思考过程" />
          <el-switch v-model="local.config.enableChart" active-text="图表输出" />
          <el-switch v-model="local.config.enablePolish" active-text="回答 AI 润色" />
        </div>
        <div class="form-subtitle">数据配置 / 数据库连接分析</div>
        <div class="form-row switches">
          <el-switch
            v-model="local.config.enableDBAnalysis"
            active-text="数据库连接分析（同步表结构 / 字段语义维护）"
            @change="syncDBAnalysis"
          />
        </div>
        <div class="form-row" v-if="local.config.enableDBAnalysis">
          <label style="width:120px">激活分析连接</label>
          <el-select v-model="local.config.activeDBConn" size="small" placeholder="选择用于分析的连接（仅可一个）" style="width:300px" filterable>
            <el-option v-for="c in dbConns" :key="c.id" :label="c.name + ' (' + (c.dbType||'mysql').toUpperCase() + ')'" :value="c.id" />
          </el-select>
          <span class="hint" style="margin:0">同一时间仅允许一个数据库连接参与分析；可在「插件 / 数据库连接」中启用。</span>
        </div>
        <div class="hint" style="margin-top:-6px;margin-bottom:12px">
          开启后，Agent 可连接已配置的 MySQL / PostgreSQL / Oracle 数据库，读取并同步表结构与字段语义（注释）用于数据分析；
          也可在「内置工具」中单独微调 db_schema / db_query 的开关与描述。需先在「插件 / 数据库连接」中配置连接。
        </div>
      </el-tab-pane>

      <!-- 内置工具 -->
      <el-tab-pane label="内置工具" name="builtin">
        <div class="tab-tip">内置工具无需配置 MCP 服务器，由 Agent 在本地直接执行。可单独开启/关闭，保存后立即生效。</div>
        <div class="form-subtitle">工具开关（可单独开启/关闭，描述可编辑）</div>
        <div class="tool-list">
          <div v-for="t in toolList" :key="t.name" class="tool-card">
            <div class="tool-card-head">
              <span class="tool-icon">{{ t.icon }}</span>
              <span class="tool-name">{{ t.name }}</span>
              <el-tag size="small" effect="plain" class="tool-group">{{ t.group }}</el-tag>
              <el-switch v-model="local.config.tools.enabled[t.name]" />
            </div>
            <div class="tool-card-body">
              <div class="tool-desc-head">
                <label>工具描述（模型可见）</label>
                <el-button
                  v-if="!editing[t.name]"
                  size="small" text type="primary"
                  @click="startEdit(t.name)"
                >编辑</el-button>
                <span v-else class="tool-desc-actions">
                  <el-button size="small" text type="success" @click="saveEdit(t.name)">保存</el-button>
                  <el-button size="small" text @click="cancelEdit(t.name)">取消</el-button>
                </span>
              </div>
              <el-input
                v-model="local.config.tools.desc[t.name]"
                type="textarea"
                :rows="2"
                :placeholder="t.def"
                size="small"
                :readonly="!editing[t.name]"
              />
            </div>
          </div>
        </div>
        <div class="tab-tip" style="margin-top:14px">
          说明：开启后模型可在对话中调用这些工具（如"读取某个文件""搜索网页""查看系统信息"）。若模型未使用，通常是其判断无需调用，可在提问时明确要求使用对应工具。
        </div>
      </el-tab-pane>

      <!-- 技能 -->
      <el-tab-pane label="技能(Skill)" name="skills">
        <div class="tab-tip">技能保存后立即热加载。描述用于让 AI 判断何时使用，提示词在命中时注入。</div>
        <el-button size="small" type="primary" plain @click="addSkill">+ 新增技能</el-button>
        <div v-for="(s, i) in local.skills" :key="i" class="card">
          <div class="card-head">
            <el-switch v-model="s.enabled" size="small" />
            <el-input v-model="s.name" size="small" placeholder="技能名称" style="width:200px" />
            <el-button size="small" text type="danger" @click="removeSkill(i)">删除</el-button>
          </div>
          <el-input v-model="s.description" size="small" placeholder="何时使用（描述）" style="margin:6px 0" />
          <el-input v-model="s.prompt" type="textarea" :rows="3" size="small" placeholder="命中时注入的提示词" />
        </div>
      </el-tab-pane>

      <!-- MCP -->
      <el-tab-pane label="MCP 服务器" name="mcp">
        <div class="tab-tip">支持 stdio（本地命令）与 http（远程 JSON-RPC）。调用时自动携带当前登录用户身份。</div>
        <el-button size="small" type="primary" plain @click="addServer">+ 新增服务器</el-button>
        <div v-for="(s, i) in local.servers" :key="i" class="card">
          <div class="card-head">
            <el-switch v-model="s.enabled" size="small" />
            <el-input v-model="s.name" size="small" placeholder="名称" style="width:160px" />
            <el-select v-model="s.transport" size="small" style="width:100px">
              <el-option label="stdio" value="stdio" />
              <el-option label="http" value="http" />
            </el-select>
            <el-button size="small" :loading="testing === s.name" @click="testServer(s)">测试</el-button>
            <el-button size="small" text type="danger" @click="removeServer(i)">删除</el-button>
          </div>
          <template v-if="s.transport === 'stdio'">
            <el-input v-model="s.command" size="small" placeholder="命令，如 npx / python" style="margin:6px 0" />
            <el-input :model-value="argsText(s)" @update:model-value="setArgs(s, $event)" size="small" placeholder="参数（空格分隔）" style="margin-bottom:6px" />
            <el-input :model-value="kvText(s.env)" @update:model-value="setKv(s, 'env', $event)" type="textarea" :rows="2" size="small" placeholder="环境变量，每行 KEY=VALUE" />
          </template>
          <template v-else>
            <el-input v-model="s.url" size="small" placeholder="服务地址 http://..." style="margin:6px 0" />
            <el-input :model-value="kvText(s.headers)" @update:model-value="setKv(s, 'headers', $event)" type="textarea" :rows="2" size="small" placeholder="请求头，每行 Key=Value" />
          </template>
        </div>
      </el-tab-pane>

      <!-- 用户 -->
      <el-tab-pane label="登录用户" name="users">
        <div class="tab-tip">用户身份会通过 X-User-Id / X-User-Token / _meta 传入 MCP 服务器，用于权限区分。</div>
        <el-button size="small" type="primary" plain @click="addUser">+ 新增用户</el-button>
        <div v-for="(u, i) in local.users" :key="i" class="card">
          <div class="card-head">
            <el-input v-model="u.name" size="small" placeholder="用户名" style="width:160px" />
            <el-button size="small" text type="danger" @click="removeUser(i)">删除</el-button>
          </div>
          <el-input v-model="u.token" size="small" placeholder="Token" style="margin:6px 0" />
          <el-input :model-value="rolesText(u)" @update:model-value="setRoles(u, $event)" size="small" placeholder="角色（逗号分隔），如 admin,tester" />
        </div>
      </el-tab-pane>

    </el-tabs>

    <template #footer>
      <el-button @click="close">取消</el-button>
      <el-button type="primary" @click="saveAll">保存并热加载</el-button>
    </template>
  </el-drawer>
</template>

<style scoped>
.form-block { margin-bottom: 14px; }
.form-block > label, .form-col > label { display: block; font-size: 12px; color: var(--text-muted); margin-bottom: 4px; }
.form-row { display: flex; gap: 16px; margin-bottom: 14px; flex-wrap: wrap; }
.form-col { flex: 1; min-width: 140px; }
.form-subtitle { font-size: 12px; color: var(--text); font-weight: 600; margin: 4px 0 6px; }
.tool-list { display: grid; grid-template-columns: repeat(2, 1fr); gap: 12px; }
.tool-card { border: 1px solid var(--border); border-radius: 8px; padding: 10px 12px; background: var(--surface-2); }
.tool-card-head { display: flex; align-items: center; gap: 8px; }
.tool-icon { font-size: 16px; }
.tool-name { font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 13px; font-weight: 600; }
.tool-group { margin-left: 2px; }
.tool-card-head .el-switch { margin-left: auto; }
.tool-card-body { margin-top: 8px; }
.tool-card-body label { display: block; font-size: 11px; color: var(--text-muted); margin-bottom: 4px; }
.tool-desc-head { display: flex; align-items: center; justify-content: space-between; }
.tool-desc-actions { display: inline-flex; gap: 4px; }
.tool-card-body .el-textarea.is-disabled .el-textarea__inner,
.tool-card-body .el-textarea__inner[readonly] { background: var(--surface-3, #f5f6f8); color: var(--text); opacity: 0.92; }
.tab-tip { font-size: 12px; color: var(--text-muted); margin-bottom: 10px; line-height: 1.5; }
.card { border: 1px solid var(--border); border-radius: 8px; padding: 10px; margin: 10px 0; background: var(--surface-2); }
.card-head { display: flex; align-items: center; gap: 8px; }
.conn-name { font-weight: 600; font-size: 13px; }
.conn-host { font-size: 12px; color: var(--text-muted); }
.spacer { flex: 1; }
.empty-hint { font-size: 12px; color: var(--text-muted); padding: 6px 0; }
.table-pick { border: 1px solid var(--border); border-radius: 8px; padding: 10px; margin: 8px 0; background: var(--surface-2); }
.tp-head { font-size: 12px; color: var(--text-muted); margin-bottom: 8px; }
.tp-list { display: flex; flex-wrap: wrap; gap: 8px; }
.tp-rows { color: var(--text-muted); font-size: 11px; }
.sem-card { background: var(--surface-2); }
.tp-count { font-size: 12px; color: var(--text-muted); }
.sem-row { display: flex; align-items: center; gap: 10px; margin: 6px 0; flex-wrap: wrap; }
.sem-col { font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 13px; min-width: 140px; font-weight: 600; }
.sem-type { font-size: 11px; color: var(--text-muted); flex: 1; min-width: 120px; }
.sem-input { max-width: 280px; }
.db-conns-bar { display: flex; gap: 12px; align-items: flex-start; flex-wrap: wrap; margin-bottom: 6px; }
.db-conn-chips { display: flex; gap: 8px; flex-wrap: wrap; }
.db-conn-chip { display: inline-flex; align-items: center; gap: 6px; padding: 4px 8px; border: 1px solid var(--border); border-radius: 999px; background: var(--surface-2); cursor: pointer; font-size: 12px; }
.db-conn-chip.active { border-color: var(--primary, #409eff); box-shadow: 0 0 0 2px rgba(64,158,255,.15); }
.db-conn-chip .conn-host { margin-left: 2px; }
.db-cols-layout { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; margin-top: 8px; }
.db-col { min-width: 0; }
.db-col .form-subtitle { margin-top: 0; }
.synced-list { display: flex; flex-direction: column; gap: 10px; max-height: 480px; overflow: auto; padding-right: 4px; }
.tbl-search { width: 160px; float: right; }
.expand-toggle { cursor: pointer; user-select: none; margin-right: 6px; color: var(--primary, #409eff); font-size: 12px; }
.table-sem-row { display: flex; align-items: center; gap: 8px; padding: 6px 10px; border-top: 1px solid var(--border); }
.sem-col.sm { min-width: 48px; color: #888; font-size: 12px; }
.tp-synced { color: #2e7d32; font-size: 11px; margin-left: 4px; }
@media (max-width: 860px) { .db-cols-layout { grid-template-columns: 1fr; } }
.test-res { font-size: 12px; padding: 2px 8px; border-radius: 4px; }
.test-res.ok { color: #2e7d32; background: #e8f5e9; }
.test-res.err { color: #c62828; background: #ffebee; }
</style>
