<script setup>
// 桌面端「服务端同步」面板：登录服务端后业务数据切换到服务端（本地留镜像），
// 剪贴板/抓包/Agent 配置等桌面数据保持本地。与网页端 TeamPanel 能力互补。
import { ref, onMounted, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ServerLogin, ServerRegister, ServerLogout, ServerStatus, ServerRefreshProjects } from '../../../wailsjs/go/main/App'
import { store } from '../../store'

const baseUrl = ref('')
const username = ref('')
const password = ref('')
const busy = ref(false)
const mode = ref('login') // login | register
const info = ref(null) // ServerUserInfo

async function refresh() {
  try {
    info.value = await ServerStatus()
    if (info.value && info.value.baseUrl) baseUrl.value = info.value.baseUrl
  } catch (e) { /* 非桌面端或未就绪 */ }
}

async function login() {
  if (!baseUrl.value.trim() || !username.value.trim() || !password.value) {
    ElMessage.warning('请填写服务端地址、用户名与密码')
    return
  }
  busy.value = true
  try {
    const args = {
      baseUrl: baseUrl.value.trim(),
      username: username.value.trim(),
      password: password.value,
    }
    const fn = mode.value === 'register' ? ServerRegister : ServerLogin
    const r = await fn(args)
    info.value = r
    // 同步前端登录态与项目角色表（服务端模式 UI 生效）
    store.user = { id: r.userId, username: r.username, server: 'desktop' }
    const roles = {}
    for (const pm of (r.projects || [])) roles[pm.id] = pm.role || 'viewer'
    store.projectRoles = roles
    password.value = ''
    ElMessage.success('已连接服务端：' + r.username)
    // 重新拉取数据（现在走服务端）
    const mod = await import('../../store')
    await mod.reloadStore()
  } catch (e) {
    const msg = String(e && e.message ? e.message : e)
    if (mode.value === 'login' && /用户名或密码错误/.test(msg)) {
      ElMessageBox.confirm('该用户名在服务端不存在，是否注册新账号？', '未找到用户', {
        confirmButtonText: '注册并登录', cancelButtonText: '取消', type: 'info',
      }).then(() => { mode.value = 'register'; login() }).catch(() => {})
      return
    }
    ElMessage.error(msg)
  } finally {
    busy.value = false
  }
}

async function doLogout() {
  try {
    await ServerLogout()
    info.value = { loggedIn: false }
    store.user = null
    store.projectRoles = {}
    ElMessage.success('已退出服务端，数据回到本地模式')
    const mod = await import('../../store')
    await mod.reloadStore()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function refreshProjects() {
  try {
    const metas = await ServerRefreshProjects()
    const roles = {}
    for (const pm of (metas || [])) roles[pm.id] = pm.role || 'viewer'
    store.projectRoles = roles
    ElMessage.success('已刷新项目角色（' + (metas || []).length + ' 个项目）')
  } catch (e) {
    ElMessage.error(String(e))
  }
}

onMounted(refresh)
// 服务端登录事件（Go 侧 Emit）也刷新状态
watch(() => store.user, (v) => { if (!v) refresh() })
</script>

<template>
  <div class="srv-sync">
    <!-- 未登录：登录表单 -->
    <template v-if="!info || !info.loggedIn">
      <div class="srv-row">
        <el-input v-model="baseUrl" placeholder="服务端地址，如 http://192.168.1.10:8080" size="small" />
      </div>
      <div class="srv-row">
        <el-input v-model="username" placeholder="用户名" size="small" style="width:180px" />
        <el-input v-model="password" type="password" show-password placeholder="密码" size="small" style="width:180px" />
        <el-button type="primary" size="small" :loading="busy" @click="login">
          {{ mode === 'login' ? '登录' : '注册并登录' }}
        </el-button>
      </div>
      <div class="srv-row" v-if="mode === 'register'">
        <span class="srv-tip">注册模式：将创建新账号并登录</span>
        <el-button size="small" text @click="mode = 'login'">返回登录</el-button>
      </div>
      <div class="srv-tip">登录后接口/文档/测试数据将保存到服务端（本地保留镜像，离线可用）。团队协作请在网页端管理。</div>
    </template>

    <!-- 已登录：状态 -->
    <template v-else>
      <div class="srv-row">
        <el-tag type="success" size="small">已连接</el-tag>
        <b>{{ info.username }}</b>
        <span class="srv-addr">@ {{ info.baseUrl }}</span>
        <el-button size="small" text type="primary" @click="refreshProjects">刷新项目角色</el-button>
        <el-button size="small" text type="danger" @click="doLogout">退出登录</el-button>
      </div>
      <div class="srv-tip">业务数据保存在服务端并与本地镜像同步；退出后回到纯本地模式。</div>
    </template>
  </div>
</template>

<style scoped>
.srv-sync { display: flex; flex-direction: column; gap: 10px; }
.srv-row { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.srv-row .el-input { max-width: 380px; }
.srv-addr { color: var(--text-muted); font-size: 13px; }
.srv-tip { font-size: 12px; color: var(--text-muted); line-height: 1.7; }
</style>
