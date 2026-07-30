<template>
  <div class="pm">
    <!-- 卡片网格（列表）头部：分类标题 + 新增 -->
    <div class="pm-grid-head" v-if="!selected">
      <span class="pm-grid-title">{{ currentCat }}</span>
      <el-button size="small" type="primary" @click="openAdd">+ 新增连接</el-button>
    </div>

    <!-- 卡片网格（列表） -->
    <div class="pm-grid" v-if="!selected">
      <el-empty v-if="!categoryConns.length" :description="'暂无「' + currentCat + '」连接'">
        <el-button type="primary" size="small" @click="openAdd">新增连接</el-button>
      </el-empty>
      <div v-else class="pm-cards">
        <div v-for="c in categoryConns" :key="c.id" class="pm-card" @click="selectConn(c)">
          <div class="pm-card-top">
            <span class="pm-card-ico">{{ catIcon(c.category) }}</span>
            <span class="pm-card-name" :title="c.name">{{ c.name }}</span>
          </div>
          <div class="pm-card-host">{{ c.host || '—' }}<template v-if="c.port">:{{ c.port }}</template></div>
          <div class="pm-card-meta">{{ cardMeta(c) }}</div>
          <div class="pm-card-actions" @click.stop>
            <span class="pm-act" @click="editConn(c)">编辑</span>
            <span class="pm-act pm-act-del" @click="removeConn(c.id)">删除</span>
          </div>
        </div>
      </div>
    </div>

    <!-- 详情 / 操作区 -->
    <div class="pm-detail" v-else v-loading="loading">
      <div class="pm-detail-bar">
        <el-button size="small" @click="selectedId = ''">← 返回列表</el-button>
        <span class="pm-detail-title">{{ selected.name }}</span>
        <span class="pm-detail-tag">{{ currentCat }}</span>
        <span class="pm-detail-host">{{ selected.host }}<template v-if="selected.port">:{{ selected.port }}</template></span>
      </div>

      <!-- SSH：模仿 XShell 的实时交互终端 -->
      <div class="pm-body" v-if="selected.category === 'ssh'">
        <div class="term-bar">
          <span class="term-status" :class="{ on: sshConnected }">
            <i class="dot" />{{ sshConnected ? '已连接' : '未连接' }}
          </span>
          <span class="term-host">{{ selected.username ? selected.username + '@' : '' }}{{ selected.host }}<template v-if="selected.port">:{{ selected.port }}</template></span>
          <span style="flex:1" />
          <el-button size="small" type="danger" plain :disabled="!sshConnected" @click="closeSsh">断开</el-button>
          <el-button size="small" @click="clearSshLog">清屏</el-button>
        </div>

        <div class="term" ref="termRef" @click="focusTermInput">
          <pre class="term-out">{{ sshLog }}<span v-if="sshConnected" class="term-cursor">█</span></pre>
          <div v-if="!sshConnected" class="term-overlay">
            <el-button type="primary" size="small" :loading="sshConnecting" @click="openSsh">连接</el-button>
            <span class="term-hint">点击「连接」打开实时终端会话</span>
          </div>
          <input
            v-if="sshConnected"
            ref="sshInputRef"
            class="term-input"
            v-model="sshInput"
            @keyup.enter="sendSsh"
            spellcheck="false"
            autocomplete="off"
          />
        </div>
      </div>

      <!-- SFTP / FTP -->
      <div class="pm-body" v-else-if="selected.category === 'sftp' || selected.category === 'ftp'">
        <div class="pm-line pm-wrap">
          <el-button size="small" @click="gotoParent">返回上级</el-button>
          <el-button size="small" type="primary" @click="listRemote">刷新</el-button>
          <el-button size="small" @click="remoteMkdirShown = true">新建目录</el-button>
          <span class="pm-path">
            <span v-for="(seg, i) in pathSegments" :key="i" class="pm-path-seg" @click="gotoSeg(seg)">{{ seg.name }}<span v-if="i < pathSegments.length - 1"> / </span></span>
          </span>
        </div>
        <div class="pm-tbl-grow">
          <el-table :data="remoteFiles" size="small" height="100%" @row-dblclick="onRemoteRowDblclick">
            <el-table-column label="名称">
              <template #default="{ row }">
                <span>{{ row.isDir ? '📁' : '📄' }} {{ row.name }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="size" label="大小" width="110" />
            <el-table-column label="操作" width="200">
              <template #default="{ row }">
                <el-button v-if="!row.isDir" size="small" @click="remoteRead(row)">查看</el-button>
                <el-button v-if="row.isDir" size="small" @click="gotoSeg({ path: row.path })">进入</el-button>
                <el-button size="small" type="danger" plain @click="remoteDelete(row)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
        </div>

        <div class="pm-sub">文件内容 / 编辑（{{ currentRemotePath || '未选择' }}）</div>
        <el-input v-model="remoteContent" type="textarea" class="pm-content" placeholder="选择文件后查看或编辑" />
        <el-button size="small" type="primary" class="pm-sep" @click="remoteWrite" :disabled="!currentRemotePath">保存写入</el-button>

        <el-dialog v-model="remoteMkdirShown" title="新建目录" width="360px">
          <el-input v-model="remoteMkdirName" placeholder="目录名（相对当前路径）" />
          <template #footer>
            <el-button @click="remoteMkdirShown = false">取消</el-button>
            <el-button type="primary" @click="remoteMkdir">确定</el-button>
          </template>
        </el-dialog>
      </div>
    </div>

    <!-- 新增/编辑连接 -->
    <el-dialog v-model="showAdd" :title="editing ? '编辑连接' : '新增连接'" width="440px">
      <el-form label-width="90px" size="small">
        <el-form-item label="名称"><el-input v-model="form.name" /></el-form-item>
        <el-form-item label="分类">
          <el-select v-model="form.category" style="width:100%">
            <el-option label="XShell(SSH)" value="ssh" />
            <el-option label="FTP" value="ftp" />
            <el-option label="SFTP" value="sftp" />
          </el-select>
        </el-form-item>
        <el-form-item label="主机"><el-input v-model="form.host" /></el-form-item>
        <el-form-item label="端口"><el-input v-model="form.port" /></el-form-item>
        <el-form-item label="用户名"><el-input v-model="form.username" /></el-form-item>
        <el-form-item label="密码"><el-input v-model="form.password" type="password" show-password /></el-form-item>
        <el-form-item label="备注"><el-input v-model="form.remark" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showAdd = false">取消</el-button>
        <el-button type="primary" @click="saveConn">保存</el-button>
        <el-button type="success" plain @click="testConn">测试连接</el-button>
      </template>
      <div v-if="testResult" style="margin-top:8px" :style="{ color: testResult.Ok ? '#67c23a' : '#f56c6c' }">
        {{ testResult.Ok ? '连接成功' : ('失败：' + testResult.Error) }}
      </div>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch, nextTick } from 'vue'
import { ElMessage } from 'element-plus'
import {
  PluginTest,
  PluginSSHOpen, PluginSSHInput, PluginSSHClose, PluginSSHResize,
  PluginSFTPList, PluginSFTPRead, PluginSFTPWrite, PluginSFTPMkdir, PluginSFTPDelete,
  PluginFTPList, PluginFTPRead, PluginFTPWrite, PluginFTPMkdir, PluginFTPDelete,
} from '../../wailsjs/go/main/App'
import { EventsOn, EventsOff } from '../../wailsjs/runtime/runtime'
import { pluginConnections, addPluginConn, updatePluginConn, removePluginConn } from '../store.js'

// 连接分类定义
const categories = [
  { value: 'ssh', label: 'XShell(SSH)', ico: '💻' },
  { value: 'ftp', label: 'FTP', ico: '📁' },
  { value: 'sftp', label: 'SFTP', ico: '📂' },
]
const catIconMap = { ssh: '💻', ftp: '📁', sftp: '📂' }

// Props：当前选中的分类（由 Tools.vue 左侧导航传入）
const props = defineProps({ category: { type: String, default: 'ssh' } })

// 视图状态
const selectedId = ref('')
const loading = ref(false)
const showAdd = ref(false)
const editing = ref(false)
const editingId = ref('')
const testResult = ref(null)

// 表单状态（新增/编辑连接）
const form = reactive({ name: '', category: 'ssh', host: '', port: 0, username: '', password: '', remark: '' })

// 连接操作相关状态
const sshConnected = ref(false)   // 实时终端是否已连接
const sshConnecting = ref(false)  // 正在建立连接
const sshSessionId = ref('')      // 后端 SSH 会话 ID
const sshLog = ref('')            // 终端回显文本
const sshInput = ref('')          // 当前命令输入
const termRef = ref(null)         // 终端容器
const sshInputRef = ref(null)     // 隐藏输入框
const remotePath = ref('/')
const remoteFiles = ref([])
const remoteContent = ref('')
const currentRemotePath = ref('')
const remoteMkdirShown = ref(false)
const remoteMkdirName = ref('')

// 当前分类标题
const currentCat = computed(() => (categories.find(c => c.value === props.category) || {}).label || '')
// 当前分类下的连接列表
const categoryConns = computed(() => pluginConnections().filter(c => c.category === props.category))
// 当前选中的连接对象
const selected = computed(() => categoryConns.value.find(c => c.id === selectedId.value))

// 切换分类时清空选中与操作状态
watch(() => props.category, () => { selectedId.value = ''; resetOps() })

// 分类图标
function catIcon(cat) { return catIconMap[cat] || '🔌' }
// 卡片副标题信息
function cardMeta(c) {
  switch (c.category) {
    case 'ssh': return c.username ? '用户 ' + c.username : '远程终端'
    case 'sftp':
    case 'ftp': return c.username ? '用户 ' + c.username : '文件传输'
    default: return c.remark || ''
  }
}

function selectConn(c) { selectedId.value = c.id }

// 路径分段，用于面包屑导航
const pathSegments = computed(() => {
  const p = remotePath.value || '/'
  if (p === '/' || p === '') return [{ name: '根目录', path: '/' }]
  const parts = p.split('/').filter(Boolean)
  const segs = [{ name: '根', path: '/' }]
  let acc = ''
  for (const part of parts) {
    acc += '/' + part
    segs.push({ name: part, path: acc })
  }
  return segs
})

// 统一调用包装：处理 loading 与错误提示
async function call(fn, ...args) {
  loading.value = true
  try {
    const r = await fn(...args)
    if (r && r.Ok === false) ElMessage.error(r.Error || '操作失败')
    return r
  } catch (e) {
    ElMessage.error((e && e.message) ? e.message : String(e))
    return null
  } finally {
    loading.value = false
  }
}

// 重置所有操作区状态
function resetOps() {
  remoteFiles.value = []
  remoteContent.value = ''
  currentRemotePath.value = ''
  closeSsh()
  sshLog.value = ''
}

// 打开连接时按需加载数据：SSH 自动建立实时会话，文件类自动列目录
watch(selected, (val) => {
  if (!val) return
  resetOps()
  if (val.category === 'ssh') openSsh()
  else if (val.category === 'sftp' || val.category === 'ftp') listRemote()
})

// ===================== 连接 CRUD =====================
function blankForm() {
  Object.assign(form, {
    name: '', category: props.category, host: '', port: 0,
    username: '', password: '', remark: '',
  })
}
function openAdd() { editing.value = false; editingId.value = ''; testResult.value = null; blankForm(); showAdd.value = true }
function editConn(c) {
  editing.value = true; editingId.value = c.id; testResult.value = null
  Object.assign(form, { name: c.name, category: c.category, host: c.host, port: c.port,
    username: c.username, password: c.password, remark: c.remark })
  showAdd.value = true
}
function saveConn() {
  if (!form.name) { ElMessage.error('请填写名称'); return }
  if (!form.host) { ElMessage.error('请填写主机'); return }
  const conn = {
    id: editing.value ? editingId.value : ('pl_' + Date.now()),
    category: form.category, name: form.name, host: form.host,
    port: Number(form.port) || 0, username: form.username, password: form.password,
    remark: form.remark,
    updatedAt: new Date().toISOString(),
  }
  if (editing.value) updatePluginConn(conn)
  else addPluginConn(conn)
  showAdd.value = false
  ElMessage.success('已保存')
}
async function testConn() {
  const conn = {
    id: editingId.value || 'test', category: form.category, name: form.name, host: form.host,
    port: Number(form.port) || 0, username: form.username, password: form.password,
  }
  testResult.value = await call(PluginTest, conn)
}

// ===================== SSH 实时终端 =====================
// 建立带 PTY 的持久会话，并通过事件流实时接收输出
async function openSsh() {
  if (sshConnecting.value || sshConnected.value) return
  sshConnecting.value = true
  sshLog.value = ''
  try {
    const id = await PluginSSHOpen(selected.value)
    if (!id) throw new Error('未能建立会话')
    sshSessionId.value = id
    sshConnected.value = true
    // 监听后端推送的实时输出与断开事件
    EventsOn('ssh:' + id + ':data', (chunk) => {
      sshLog.value += chunk
      scrollTermToBottom()
    })
    EventsOn('ssh:' + id + ':close', () => {
      sshConnected.value = false
      sshLog.value += '\n[连接已关闭]\n'
    })
  } catch (e) {
    ElMessage.error('SSH 连接失败：' + (e.message || e))
  } finally {
    sshConnecting.value = false
  }
}

// 向会话发送命令（带回车），并回显到终端
function sendSsh() {
  if (!sshConnected.value || !sshSessionId.value) return
  const cmd = sshInput.value
  sshLog.value += cmd + '\n'
  PluginSSHInput(sshSessionId.value, cmd + '\n')
  sshInput.value = ''
  scrollTermToBottom()
}

// 关闭会话并清理事件监听
function closeSsh() {
  if (sshSessionId.value) {
    EventsOff('ssh:' + sshSessionId.value + ':data')
    EventsOff('ssh:' + sshSessionId.value + ':close')
    PluginSSHClose(sshSessionId.value).catch(() => {})
    sshSessionId.value = ''
  }
  sshConnected.value = false
}

function clearSshLog() { sshLog.value = '' }
// 终端点击任意处即聚焦输入（模仿 XShell 体验）
function focusTermInput() { nextTick(() => sshInputRef.value && sshInputRef.value.focus()) }
// 新输出到达后滚动到底部
function scrollTermToBottom() {
  nextTick(() => { if (termRef.value) termRef.value.scrollTop = termRef.value.scrollHeight })
}

// ===================== SFTP / FTP =====================
// 根据连接类别返回对应的后端 API 集合
function api() {
  return selected.value.category === 'sftp'
    ? { list: PluginSFTPList, read: PluginSFTPRead, write: PluginSFTPWrite, mkdir: PluginSFTPMkdir, del: PluginSFTPDelete }
    : { list: PluginFTPList, read: PluginFTPRead, write: PluginFTPWrite, mkdir: PluginFTPMkdir, del: PluginFTPDelete }
}
async function listRemote() {
  const r = await call(api().list, selected.value, remotePath.value)
  if (r) remoteFiles.value = r
}
async function remoteRead(row) {
  if (row.isDir) return
  currentRemotePath.value = row.path
  remoteContent.value = (await call(api().read, selected.value, row.path)) || ''
}
async function remoteWrite() {
  if (!currentRemotePath.value) return
  await call(api().write, selected.value, currentRemotePath.value, remoteContent.value)
  ElMessage.success('已写入')
}
async function remoteMkdir() {
  if (!remoteMkdirName.value) return
  const p = (remotePath.value === '/' ? '' : remotePath.value) + '/' + remoteMkdirName.value
  await call(api().mkdir, selected.value, p)
  remoteMkdirShown.value = false
  remoteMkdirName.value = ''
  await listRemote()
}
async function remoteDelete(row) {
  await call(api().del, selected.value, row.path)
  ElMessage.success('已删除')
  await listRemote()
}
function gotoParent() {
  const p = remotePath.value || '/'
  if (p === '/' || p === '') return
  const idx = p.lastIndexOf('/')
  remotePath.value = idx <= 0 ? '/' : p.slice(0, idx)
  listRemote()
}
function gotoSeg(seg) { remotePath.value = seg.path; listRemote() }
function onRemoteRowDblclick(row) {
  if (row.isDir) gotoSeg({ path: row.path })
  else remoteRead(row)
}

function removeConn(id) {
  removePluginConn(id)
  if (selectedId.value === id) selectedId.value = ''
  ElMessage.success('已删除')
}
</script>

<style scoped>
.pm { display: flex; flex-direction: column; height: 100%; }
.pm-grid-head {
  display: flex; align-items: center; justify-content: space-between;
  padding: 12px 18px; border-bottom: 1px solid #e5e6eb; background: #fff; flex-shrink: 0;
}
.pm-grid-title { font-size: 15px; font-weight: 600; color: #1d2129; }

.pm-grid { flex: 1; overflow: auto; padding: 18px; }
.pm-cards { display: grid; grid-template-columns: repeat(auto-fill, minmax(230px, 1fr)); gap: 14px; }
.pm-card {
  border: 1px solid #e5e6eb; border-radius: 10px; padding: 14px;
  background: #fff; cursor: pointer; transition: box-shadow .15s, border-color .15s, transform .15s;
  position: relative;
}
.pm-card:hover { border-color: #165dff; box-shadow: 0 4px 14px rgba(22, 93, 255, .12); transform: translateY(-2px); }
.pm-card-top { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
.pm-card-ico { font-size: 18px; }
.pm-card-name { font-weight: 600; font-size: 15px; color: #1d2129; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.pm-card-host { color: #86909c; font-size: 13px; font-family: Consolas, monospace; }
.pm-card-meta { color: #a9aeb8; font-size: 12px; margin-top: 4px; min-height: 18px; }
.pm-card-actions { display: flex; gap: 14px; margin-top: 12px; border-top: 1px dashed #eee; padding-top: 10px; }
.pm-act { font-size: 13px; color: #165dff; cursor: pointer; }
.pm-act:hover { text-decoration: underline; }
.pm-act-del { color: #f53f3f; }

.pm-detail { flex: 1; min-height: 0; display: flex; flex-direction: column; overflow: hidden; padding: 14px 18px; }
.pm-detail-bar { display: flex; align-items: center; gap: 10px; margin-bottom: 14px; flex-wrap: wrap; }
.pm-detail-title { font-size: 16px; font-weight: 600; color: #1d2129; }
.pm-detail-tag { font-size: 12px; color: #165dff; background: #e8f3ff; border-radius: 4px; padding: 1px 8px; }
.pm-detail-host { font-size: 13px; color: #86909c; font-family: Consolas, monospace; }
.pm-pre { white-space: pre-wrap; word-break: break-all; background: #f7f7f7; padding: 8px; border-radius: 4px; margin: 0; }
.pm-path { font-size: 13px; color: #409eff; }
.pm-path-seg { cursor: pointer; }
.pm-path-seg:hover { text-decoration: underline; }

/* 模仿 XShell 的实时终端 */
.term-bar { display: flex; align-items: center; gap: 10px; padding: 6px 10px; background: #1e1e1e; border-radius: 6px 6px 0 0; flex-shrink: 0; }
.term-status { display: flex; align-items: center; gap: 6px; font-size: 12px; color: #f56c6c; }
.term-status.on { color: #67c23a; }
.term-status .dot { width: 8px; height: 8px; border-radius: 50%; background: currentColor; }
.term-host { font-size: 12px; color: #c0c4cc; font-family: Consolas, monospace; }

.term {
  flex: 1; min-height: 0; position: relative; overflow: auto;
  background: #0c0c0c; border-radius: 0 0 6px 6px; padding: 10px 12px;
  font-family: Consolas, 'Courier New', monospace; font-size: 13px; line-height: 1.5;
  cursor: text;
}
.term-out { margin: 0; white-space: pre-wrap; word-break: break-all; color: #c8e6c9; text-shadow: 0 0 1px rgba(200,230,201,.3); }
.term-cursor { color: #67c23a; animation: termBlink 1s steps(1) infinite; }
@keyframes termBlink { 50% { opacity: 0; } }
.term-input {
  position: absolute; left: -9999px; /* 隐藏但可聚焦，用于捕获键盘输入 */
}
.term-overlay {
  position: absolute; inset: 0; display: flex; flex-direction: column; gap: 10px;
  align-items: center; justify-content: center; background: rgba(0,0,0,.55);
}
.term-hint { font-size: 12px; color: #8a8a8a; }

/* 详情操作区铺满 */
.pm-body { flex: 1; min-height: 0; overflow: auto; display: flex; flex-direction: column; }
.pm-line { display: flex; align-items: center; gap: 6px; flex-shrink: 0; margin-bottom: 8px; }
.pm-line.pm-wrap { flex-wrap: wrap; }
.pm-inline { width: 240px; flex-shrink: 0; }
.pm-sub { font-weight: 600; margin: 10px 0 4px; flex-shrink: 0; }
.pm-sep { margin-top: 8px; flex-shrink: 0; }
.pm-tbl-grow { flex: 1; min-height: 200px; margin-top: 4px; }
.pm-ssh-out { flex: 1; min-height: 0; overflow: auto; margin-top: 10px; }
.pm-content { flex-shrink: 0; }
.pm-content :deep(.el-textarea__inner) { height: 180px; resize: none; }
</style>
