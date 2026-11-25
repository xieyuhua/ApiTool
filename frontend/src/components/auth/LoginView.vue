<script setup>
// 2.0 服务端登录/注册视图。登录成功后 reloadStore() 拉取该用户的项目数据。
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { store } from '../../store'
import { login, register, apiBase, setApiBase, probeServer } from '../../auth'

const username = ref('')
const password = ref('')
const password2 = ref('')
const serverURL = ref(apiBase())
const mode = ref('login') // login | register
const busy = ref(false)
const serverOK = ref(null) // null=未探测 true/false

// Capacitor App 环境：App 内没有同源服务端，必须填写地址。
// 未配置过时给出明确提示，避免用户以为是"登录失败"。
const isApp = typeof window !== 'undefined' && !!window.Capacitor && !!window.Capacitor.isNativePlatform && window.Capacitor.isNativePlatform()
const needServerFirst = ref(isApp && !apiBase())

async function checkServer() {
  serverOK.value = null
  setApiBase(serverURL.value.trim())
  const h = await probeServer()
  serverOK.value = !!h
  if (h) {
    store.serverInfo = h
    ElMessage.success('服务端连接成功（v' + (h.version || '?') + '）' + (h.allowRegister ? '' : ' · 未开放注册'))
  } else {
    ElMessage.error('无法连接服务端，请检查地址')
  }
}

async function submit() {
  const name = username.value.trim()
  if (!name || !password.value) { ElMessage.warning('请输入用户名与密码'); return }
  if (serverOK.value === false) { ElMessage.error('请先确认服务端地址可连接'); return }
  if (mode.value === 'register' && password.value !== password2.value) {
    ElMessage.warning('两次密码不一致'); return
  }
  if (isApp && !serverURL.value.trim()) {
    ElMessage.warning('请先填写服务端地址')
    return
  }
  busy.value = true
  try {
    if (serverOK.value === null) await checkServer()
    const r = mode.value === 'login'
      ? await login(name, password.value)
      : await register(name, password.value)
    store.user = { id: r.user.id, username: r.user.username, server: (store.serverInfo || {}).version || '' }
    const roles = {}
    for (const pm of (r.projects || [])) roles[pm.id] = pm.role || 'viewer'
    store.projectRoles = roles
    ElMessage.success('欢迎，' + r.user.username)
    await reloadStore()
    store.view = 'workspace'
  } catch (e) {
    ElMessage.error(String(e && e.message ? e.message : e))
  } finally {
    busy.value = false
  }
}

// reloadStore 从 ../store 导入会循环依赖（store.js 动态 import auth.js 无碍，
// 这里反向引用需延迟）：直接动态引入。
async function reloadStore() {
  const mod = await import('../../store')
  await mod.reloadStore()
}
</script>

<template>
  <div class="login-wrap">
    <div class="login-card">
      <div class="brand">Ap</div>
      <h2>ApiTool 2.0</h2>
      <div class="sub">登录服务端以同步接口、文档与测试数据</div>

      <div v-if="needServerFirst" class="app-hint">
        📱 首次使用请先填写服务端地址（如 http://192.168.1.10:8080）
      </div>
      <div class="server-row">
        <el-input v-model="serverURL" size="small" :placeholder="isApp ? '服务端地址，如 http://192.168.1.10:8080' : '服务端地址（留空=当前站点）'"
          :disabled="busy" @keyup.enter="checkServer" />
        <el-button size="small" @click="checkServer" :loading="serverOK === null && busy">检测</el-button>
      </div>
      <div v-if="serverOK !== null" class="server-state" :class="serverOK ? 'ok' : 'bad'">
        {{ serverOK ? '● 服务端可连接' : '● 无法连接服务端' }}
      </div>

      <el-form @submit.prevent="submit">
        <el-input v-model="username" placeholder="用户名" size="large" @keyup.enter="submit" />
        <el-input v-model="password" type="password" show-password placeholder="密码" size="large" @keyup.enter="submit" />
        <el-input v-if="mode === 'register'" v-model="password2" type="password" show-password
          placeholder="确认密码" size="large" @keyup.enter="submit" />
        <el-button type="primary" size="large" style="width:100%" :loading="busy" @click="submit">
          {{ mode === 'login' ? '登 录' : '注 册 并 登 录' }}
        </el-button>
      </el-form>

      <div class="switch-row">
        <span>{{ mode === 'login' ? '还没有账号？' : '已有账号？' }}</span>
        <a @click.prevent="mode = mode === 'login' ? 'register' : 'login'">
          {{ mode === 'login' ? '去注册' : '去登录' }}
        </a>
      </div>
    </div>
  </div>
</template>

<style scoped>
.login-wrap {
  position: fixed; inset: 0; z-index: 5000;
  display: flex; align-items: center; justify-content: center;
  background: var(--bg);
}
.login-card {
  width: 380px; max-width: 92vw;
  background: var(--surface); border: 1px solid var(--border); border-radius: 14px;
  padding: 34px 32px; box-shadow: 0 8px 40px rgba(0,0,0,.12);
  text-align: center;
}
.brand {
  width: 52px; height: 52px; margin: 0 auto 12px; border-radius: 12px;
  background: var(--primary); color: #fff; font-size: 24px; font-weight: 700;
  display: flex; align-items: center; justify-content: center;
}
h2 { margin: 0 0 6px; color: var(--text); }
.sub { font-size: 13px; color: var(--text-muted); margin-bottom: 20px; }
.server-row { display: flex; gap: 8px; margin-bottom: 6px; }
.server-state { font-size: 12px; margin-bottom: 14px; text-align: left; }
.app-hint {
  font-size: 12px; color: #d97706; background: #fef7e6;
  border-radius: 6px; padding: 8px 10px; margin-bottom: 10px; text-align: left; line-height: 1.6;
}
.server-state.ok { color: #00b42a; }
.server-state.bad { color: #f53f3f; }
.el-form .el-input { margin-bottom: 12px; }
.switch-row { margin-top: 14px; font-size: 13px; color: var(--text-muted); }
.switch-row a { color: var(--primary); cursor: pointer; margin-left: 4px; }
</style>
