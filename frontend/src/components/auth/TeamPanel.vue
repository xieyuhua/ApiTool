<script setup>
// 2.0 协作面板：团队管理 + 项目共享授权 + 冲突对话框 + 账号信息。
// 仅服务端模式（store.user 存在）时由设置页挂载。
import { ref, computed, watch, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { store, forceSaveNow, pullRemote, saveConflict, canEditProject, currentProject } from '../../store'
import { logout, apiBase } from '../../auth'

const tab = ref('account') // account | teams | share

// ---- 账号 ----
async function doLogout() {
  await logout()
  store.user = null
  store.projectRoles = {}
  store.view = 'login'
}

// ---- 团队 ----
const teams = ref([])
const members = ref([])
const activeTeam = ref('')
const newTeamName = ref('')
const newMemberName = ref('')
const newMemberRole = ref('editor')
const busyTeam = ref(false)

async function loadTeams() {
  busyTeam.value = true
  try {
    const r = await fetch(apiBase() + '/api/teams', { headers: authHeaders() })
    const d = await r.json()
    teams.value = (d && d.result) || []
    if (!activeTeam.value && teams.value.length) activeTeam.value = teams.value[0].id
    await loadMembers()
  } catch (e) {
    ElMessage.error('读取团队失败：' + e)
  } finally { busyTeam.value = false }
}

async function loadMembers() {
  if (!activeTeam.value) { members.value = []; return }
  const r = await fetch(apiBase() + '/api/teams/' + activeTeam.value + '/members', { headers: authHeaders() })
  const d = await r.json()
  members.value = (d && d.result) || []
}

function authHeaders() {
  return { 'Content-Type': 'application/json' }
}

async function createTeam() {
  if (!newTeamName.value.trim()) { ElMessage.warning('请输入团队名'); return }
  const r = await fetch(apiBase() + '/api/teams', { method: 'POST', headers: authHeaders(), body: JSON.stringify({ name: newTeamName.value.trim() }) })
  const d = await r.json()
  if (d && d.ok === false) { ElMessage.error(d.error); return }
  newTeamName.value = ''
  ElMessage.success('已创建')
  await loadTeams()
}

async function addMember() {
  if (!newMemberName.value.trim()) { ElMessage.warning('请输入用户名'); return }
  const r = await fetch(apiBase() + '/api/teams/' + activeTeam.value + '/members', {
    method: 'POST', headers: authHeaders(),
    body: JSON.stringify({ username: newMemberName.value.trim(), role: newMemberRole.value }),
  })
  const d = await r.json()
  if (d && d.ok === false) { ElMessage.error(d.error); return }
  newMemberName.value = ''
  ElMessage.success('已添加')
  await loadMembers()
}

async function removeMember(uid) {
  const r = await fetch(apiBase() + '/api/teams/' + activeTeam.value + '/members/' + uid, { method: 'DELETE', headers: authHeaders() })
  const d = await r.json()
  if (d && d.ok === false) { ElMessage.error(d.error); return }
  await loadMembers()
}

const roleLabel = { admin: '管理员', editor: '编辑者', viewer: '只读' }

// ---- 项目共享 ----
const projects = ref([])
const shareTeamId = ref('')
const shareRole = ref('viewer')
const shareList = ref([])

async function loadProjects() {
  const r = await fetch(apiBase() + '/api/projects', { headers: authHeaders() })
  const d = await r.json()
  projects.value = (d && d.result) || []
  if (projects.value.length) shareTeamId.value = activeTeam.value || (teams.value[0] && teams.value[0].id) || ''
}

async function grantShare() {
  const p = currentProject()
  if (!p || !shareTeamId.value) return
  const r = await fetch(apiBase() + '/api/projects/' + p.id + '/share', {
    method: 'PUT', headers: authHeaders(),
    body: JSON.stringify({ teamId: shareTeamId.value, role: shareRole.value }),
  })
  const d = await r.json()
  if (d && d.ok === false) { ElMessage.error(d.error); return }
  ElMessage.success('已授权团队访问「' + p.name + '」（' + roleLabel[shareRole.value] + '）')
}

const readOnly = computed(() => store.serverInfo && !canEditProject())

// ---- 冲突对话框 ----
const conflictVisible = ref(false)
watch(saveConflict, (v) => { if (v) conflictVisible.value = true })

onMounted(() => {
  if (store.user) { loadTeams(); loadProjects() }
})
</script>

<template>
  <div class="team-panel">
    <div class="tp-tabs">
      <span class="tp-tab" :class="{ active: tab === 'account' }" @click="tab = 'account'">账号</span>
      <span class="tp-tab" :class="{ active: tab === 'teams' }" @click="tab = 'teams'">团队</span>
      <span class="tp-tab" :class="{ active: tab === 'share' }" @click="tab = 'share'">项目共享</span>
    </div>

    <!-- 账号 -->
    <div v-if="tab === 'account'" class="tp-body">
      <div class="acc-row" v-if="store.user">
        <span class="lbl">当前用户</span>
        <b>{{ store.user.username }}</b>
        <el-tag size="small" type="success">服务端 v{{ store.user.server || '?' }}</el-tag>
      </div>
      <div class="acc-row" v-if="readOnly">
        <span class="lbl">当前项目权限</span>
        <el-tag size="small" type="warning">只读（viewer）</el-tag>
        <span class="tip">该账号对此项目为只读，修改不会被保存</span>
      </div>
      <div class="acc-row">
        <el-button type="danger" plain size="small" @click="doLogout">退出登录</el-button>
      </div>
    </div>

    <!-- 团队 -->
    <div v-else-if="tab === 'teams'" class="tp-body" v-loading="busyTeam">
      <div class="row">
        <el-input v-model="newTeamName" size="small" placeholder="新团队名称" style="width:200px" @keyup.enter="createTeam" />
        <el-button size="small" type="primary" @click="createTeam">创建团队</el-button>
      </div>
      <div class="row" v-if="teams.length">
        <el-select v-model="activeTeam" size="small" style="width:200px" @change="loadMembers">
          <el-option v-for="t in teams" :key="t.id" :label="t.name" :value="t.id" />
        </el-select>
        <el-input v-model="newMemberName" size="small" placeholder="成员用户名" style="width:160px" />
        <el-select v-model="newMemberRole" size="small" style="width:100px">
          <el-option label="管理员" value="admin" />
          <el-option label="编辑者" value="editor" />
          <el-option label="只读" value="viewer" />
        </el-select>
        <el-button size="small" type="primary" plain @click="addMember">添加</el-button>
      </div>
      <div v-if="members.length" class="member-list">
        <div v-for="m in members" :key="m.userId" class="member-row">
          <span class="m-name">{{ m.userId }}</span>
          <el-tag size="small" :type="m.role === 'admin' ? 'danger' : m.role === 'editor' ? 'primary' : 'info'">
            {{ roleLabel[m.role] }}
          </el-tag>
          <el-button v-if="m.role !== 'admin'" link size="small" type="danger" @click="removeMember(m.userId)">移除</el-button>
        </div>
      </div>
      <div v-else class="empty-tip">还没有团队。创建团队并添加成员后，可把项目共享给整个团队。</div>
    </div>

    <!-- 项目共享 -->
    <div v-else class="tp-body">
      <div class="row" v-if="teams.length">
        <span class="lbl">共享「{{ currentProject().name }}」给</span>
        <el-select v-model="shareTeamId" size="small" style="width:180px">
          <el-option v-for="t in teams" :key="t.id" :label="t.name" :value="t.id" />
        </el-select>
        <el-select v-model="shareRole" size="small" style="width:100px">
          <el-option label="可编辑" value="editor" />
          <el-option label="只读" value="viewer" />
        </el-select>
        <el-button size="small" type="primary" @click="grantShare">授权</el-button>
      </div>
      <div v-else class="empty-tip">先在「团队」页创建团队，再把当前项目共享给它。</div>
      <div class="proj-list" v-if="projects.length">
        <div v-for="p in projects" :key="p.id" class="proj-row">
          <span class="p-name">{{ p.name }}</span>
          <el-tag size="small" :type="p.role === 'admin' ? 'success' : p.role === 'editor' ? 'primary' : 'info'">
            {{ roleLabel[p.role] || '—' }}
          </el-tag>
          <span class="p-ver">v{{ p.version }}</span>
        </div>
      </div>
    </div>

    <!-- 冲突对话框 -->
    <el-dialog v-model="conflictVisible" title="保存冲突" width="440px">
      <div class="conflict-body">
        <p>远端已有更新的版本（其他人或其他设备先保存了）。选择如何处理：</p>
        <div class="conflict-err">{{ saveConflict && saveConflict.error }}</div>
      </div>
      <template #footer>
        <el-button @click="conflictVisible = false">暂不处理</el-button>
        <el-button @click="conflictVisible = false; pullRemote()">以远端为准（丢弃我的修改）</el-button>
        <el-button type="primary" @click="conflictVisible = false; forceSaveNow()">以我的为准（覆盖远端）</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.team-panel { padding: 4px 0; }
.tp-tabs { display: flex; gap: 4px; margin-bottom: 14px; border-bottom: 1px solid var(--border); }
.tp-tab { padding: 8px 14px; font-size: 13px; cursor: pointer; color: var(--text-muted); border-bottom: 2px solid transparent; }
.tp-tab.active { color: var(--primary); border-bottom-color: var(--primary); font-weight: 600; }
.tp-body { display: flex; flex-direction: column; gap: 12px; min-height: 120px; }
.row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.lbl { font-size: 13px; color: var(--text-muted); }
.acc-row { display: flex; align-items: center; gap: 10px; }
.tip { font-size: 12px; color: #d97706; }
.member-list { border: 1px solid var(--border); border-radius: 8px; padding: 6px; }
.member-row { display: flex; align-items: center; gap: 10px; padding: 6px 8px; font-size: 13px; }
.m-name { flex: 1; }
.proj-list { border: 1px solid var(--border); border-radius: 8px; padding: 6px; }
.proj-row { display: flex; align-items: center; gap: 10px; padding: 6px 8px; font-size: 13px; }
.p-name { flex: 1; }
.p-ver { color: var(--text-muted); font-size: 12px; }
.empty-tip { font-size: 13px; color: var(--text-muted); padding: 16px 0; text-align: center; }
.conflict-body { font-size: 13px; line-height: 1.7; }
.conflict-err { margin-top: 8px; padding: 8px 10px; background: var(--surface-2); border-radius: 6px; color: #f53f3f; font-size: 12px; word-break: break-all; }
</style>
