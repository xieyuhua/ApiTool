<script setup>
import { ref, computed, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { store, currentProject, saveNow, scheduleAutoSync, cloudBase } from '../store'

const visible = defineModel('visible')
const username = ref('')
const password = ref('')
const busy = ref(false)
const cloudProjects = ref([])

const settings = store.data.settings
const loggedIn = computed(() => !!settings.cloudToken)
const proj = computed(() => currentProject())

async function api(path, opts = {}) {
  const url = cloudBase() + path
  opts.headers = opts.headers || {}
  if (settings.cloudToken) opts.headers['Authorization'] = 'Bearer ' + settings.cloudToken
  opts.headers['Content-Type'] = 'application/json'
  const r = await fetch(url, opts)
  let body = null
  try { body = await r.json() } catch { /* ignore */ }
  if (!r.ok) {
    throw new Error((body && body.error) || ('请求失败 ' + r.status))
  }
  return body
}

async function doLogin() {
  if (!cloudBase()) { ElMessage.warning('请先填写云服务器地址'); return }
  if (!username.value || !password.value) { ElMessage.warning('请输入账号和密码'); return }
  busy.value = true
  try {
    const b = await api('/api/login', {
      method: 'POST',
      body: JSON.stringify({ username: username.value, password: password.value }),
    })
    settings.cloudToken = b.token
    settings.cloudUser = username.value
    saveNow()
    ElMessage.success('登录成功')
    await refreshList()
  } catch (e) {
    ElMessage.error(String(e))
  } finally { busy.value = false }
}

async function doRegister() {
  if (!cloudBase()) { ElMessage.warning('请先填写云服务器地址'); return }
  if (!username.value || !password.value) { ElMessage.warning('请输入账号和密码'); return }
  busy.value = true
  try {
    const b = await api('/api/register', {
      method: 'POST',
      body: JSON.stringify({ username: username.value, password: password.value }),
    })
    settings.cloudToken = b.token
    settings.cloudUser = username.value
    saveNow()
    ElMessage.success('注册并登录成功')
    await refreshList()
  } catch (e) {
    ElMessage.error(String(e))
  } finally { busy.value = false }
}

function logout() {
  settings.cloudToken = ''
  settings.cloudUser = ''
  saveNow()
  cloudProjects.value = []
  ElMessage.success('已退出登录')
}

function onAutoSyncChange(val) {
  saveNow()
  if (val) {
    scheduleAutoSync()
    ElMessage.success('已开启自动同步')
  } else {
    ElMessage.info('已关闭自动同步')
  }
}

async function refreshList() {
  try {
    const list = await api('/api/projects')
    cloudProjects.value = Array.isArray(list) ? list : []
  } catch (e) {
    cloudProjects.value = []
  }
}

// 推送当前项目到云端（按项目 ID 创建或更新）
async function pushCurrent() {
  if (!loggedIn.value) { ElMessage.warning('请先登录'); return }
  busy.value = true
  try {
    const p = currentProject()
    const payload = {
      name: p.name,
      updatedAt: p.updatedAt || new Date().toISOString(),
      data: p,
    }
    const b = await api('/api/projects/' + p.id, {
      method: 'PUT',
      body: JSON.stringify(payload),
    })
    p.updatedAt = b.updatedAt
    saveNow()
    ElMessage.success('已推送到云端：' + p.name)
    await refreshList()
  } catch (e) {
    ElMessage.error(String(e))
  } finally { busy.value = false }
}

// 从云端拉取项目并覆盖本地同名项目
async function pullProject(id) {
  busy.value = true
  try {
    const remote = await api('/api/projects/' + id)
    const data = remote.data
    const i = store.data.projects.findIndex(x => x.id === data.id)
    if (i >= 0) {
      store.data.projects[i] = data
    } else {
      store.data.projects.push(data)
    }
    store.data.currentProjectId = data.id
    saveNow()
    ElMessage.success('已拉取并切换到：' + data.name)
    await refreshList()
  } catch (e) {
    ElMessage.error(String(e))
  } finally { busy.value = false }
}

watch(visible, v => { if (v && loggedIn.value) refreshList() })
</script>

<template>
  <el-dialog v-model="visible" title="云同步（多端共享）" width="640px" top="6vh">
    <div v-if="!loggedIn">
      <el-form label-width="90px">
        <el-form-item label="云服务器">
          <el-input v-model="settings.cloudURL" placeholder="如 http://localhost:8080 或 https://api.yours.com" />
        </el-form-item>
        <el-form-item label="账号">
          <el-input v-model="username" placeholder="用户名" @keyup.enter="doLogin" />
        </el-form-item>
        <el-form-item label="密码">
          <el-input v-model="password" type="password" show-password placeholder="密码" @keyup.enter="doLogin" />
        </el-form-item>
      </el-form>
      <div style="display:flex; gap:10px; justify-content:flex-end">
        <el-button :loading="busy" @click="doRegister">注册</el-button>
        <el-button type="primary" :loading="busy" @click="doLogin">登录</el-button>
      </div>
      <div style="font-size:12px; color:#86909c; margin-top:8px">
        服务端需运行 apitool 同步服务（可独立部署到云服务器，也可在「设置-内置同步服务」里与本工具一起启动）。
      </div>
    </div>

    <div v-else>
      <div style="display:flex; align-items:center; justify-content:space-between; margin-bottom:12px">
        <span>已登录：<b>{{ settings.cloudUser }}</b></span>
        <div style="display:flex; align-items:center; gap:8px">
          <span style="font-size:12px;color:#86909c">自动同步</span>
          <el-switch v-model="settings.autoSync" @change="onAutoSyncChange" />
          <el-button size="small" @click="logout">退出登录</el-button>
        </div>
      </div>

      <div class="sync-card">
        <div class="sync-card-title">当前项目：{{ proj.name }}</div>
        <el-button type="primary" :loading="busy" @click="pushCurrent">⬆ 推送到云端</el-button>
        <span style="font-size:12px;color:#86909c;margin-left:8px">按项目 ID 创建或覆盖云端同名项目</span>
      </div>

      <div class="sync-card">
        <div class="sync-card-title">云端项目（点击拉取到本地并切换）</div>
        <div style="font-size:12px;color:#86909c;margin-bottom:8px">
          用途：①多设备同步（公司推、家里拉）；②本地数据丢失后从云端恢复；③拿别人共享的项目。开启「自动同步」后日常无需手动拉取。
        </div>
        <div v-if="!cloudProjects.length" class="sync-empty">云端暂无项目，先推送当前项目</div>
        <div v-for="c in cloudProjects" :key="c.id" class="cloud-row">
          <span class="cloud-name">{{ c.name }}</span>
          <span class="cloud-time">{{ c.updatedAt }}</span>
          <el-button size="small" type="success" plain :loading="busy" @click="pullProject(c.id)">拉取</el-button>
        </div>
      </div>
    </div>
  </el-dialog>
</template>

<style scoped>
.sync-card { border: 1px solid #e5e6eb; border-radius: 8px; padding: 12px; margin-bottom: 12px; }
.sync-card-title { font-weight: 600; margin-bottom: 10px; }
.cloud-row { display: flex; align-items: center; gap: 10px; padding: 6px 0; border-top: 1px solid #f2f3f5; }
.cloud-name { flex: 1; font-size: 13px; }
.cloud-time { color: #c9cdd4; font-size: 12px; }
.sync-empty { color: #c9cdd4; font-size: 13px; padding: 8px 0; }
</style>
