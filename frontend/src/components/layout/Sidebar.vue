<script setup>
import { computed, ref, reactive, watch, nextTick } from 'vue'
import { ElMessageBox, ElMessage } from 'element-plus'
import { store, buildTree, addDir, addApi, removeDir, removeApi, uid, normalizeApi, currentProject, switchProject, addProject, removeProject, saveNow, savedApiSnapshots, logStore } from '../../store'
import { parseCli, FORMATS } from '../../cli'
import { CopyToClipboard } from '../../../wailsjs/go/main/App'
import LogPanel from './LogPanel.vue'

// 左侧选项卡：目录 / 日志 切换（持久化）
const sidebarTab = ref(localStorage.getItem('sb-tab') || 'tree')
function setSbTab(t) {
  sidebarTab.value = t
  localStorage.setItem('sb-tab', t)
}
const logErrorCount = computed(() => logStore.entries.filter(e => e.type === 'error').length)

const keyword = ref('')
const treeRef = ref(null)
const treeData = computed(() => buildTree())

// 新建接口（支持 URL 解析导入）
const newApiVisible = ref(false)
const newApiName = ref('')
const newApiUrl = ref('')
const newApiParentId = ref('')
const newApiTab = ref('form')

function openNewApi(parentId) {
  newApiParentId.value = parentId || ''
  newApiName.value = ''
  newApiUrl.value = ''
  resetNewApi()
  newApiVisible.value = true
}

function confirmNewApi() {
  const api = addApi(newApiParentId.value)
  // 命令行导入分支：当前在「粘贴命令行」标签页且已解析出请求
  if (newApiTab.value === 'cli' && cliParsed.value) {
    applyParsedToApi(api, cliParsed.value)
    if (newApiName.value.trim()) api.name = newApiName.value.trim()
    newApiVisible.value = false
    return
  }
  const raw = newApiUrl.value.trim()
  if (raw) {
    let u
    try {
      u = new URL(raw)
    } catch {
      try { u = new URL('https://' + raw) } catch { u = null }
    }
    if (u) {
      api.url = u.origin + u.pathname
      for (const [k, v] of u.searchParams.entries()) {
        api.query.push({ key: k, value: v, description: '', enabled: true })
      }
      ElMessage.success('已从 URL 解析出 ' + u.searchParams.size + ' 个 Query 参数')
    } else {
      api.url = raw
    }
  }
  if (newApiName.value.trim()) api.name = newApiName.value.trim()
  newApiVisible.value = false
}

function applyParsedToApi(api, p) {
  api.method = p.method || 'GET'
  api.url = p.url
  api.query = (p.queryArr || []).map(q => ({ key: q.key, value: q.value, description: '', enabled: true }))
  if (p.headersArr && p.headersArr.length) {
    for (const h of p.headersArr) {
      if (/^content-type$/i.test(h.key)) continue
      api.headers.push({ key: h.key, value: h.value, description: '', enabled: true })
    }
  }
  if (p.body) {
    api.body = p.body
    api.bodyType = p.bodyType || 'text'
  }
  ElMessage.success('已从命令行解析出 ' + (p.method || 'GET') + ' 请求（含 ' +
    api.headers.length + ' 个请求头、' + api.query.length + ' 个 Query）')
}

// 命令行导入
const cliText = ref('')
const cliParsed = ref(null)
const cliError = ref('')

watch(cliText, (v) => {
  cliError.value = ''
  cliParsed.value = null
  if (!v || !v.trim()) return
  try {
    const p = parseCli(v)
    if (!p || !p.url) {
      cliError.value = '未能从命令中识别到 URL，请检查格式（支持 curl / httpie / bash / powershell）'
      return
    }
    cliParsed.value = p
  } catch (e) {
    cliError.value = '解析失败：' + String(e)
  }
})

function resetNewApi() {
  cliText.value = ''
  cliParsed.value = null
  cliError.value = ''
}

const cliPlaceholder = `例如：
curl -X POST 'https://api.example.com/user' \\
  -H 'Content-Type: application/json' \\
  -d '{"name":"tom"}'`

async function copyParsedAs(key) {
  if (!cliParsed.value) return
  const fmt = FORMATS.find(f => f.key === key)
  if (!fmt) return
  try {
    const text = fmt.gen(cliParsed.value)
    await CopyToClipboard(text)
    ElMessage.success(`已复制为 ${fmt.label} 命令`)
  } catch (e) { ElMessage.error(String(e)) }
}

// 窗口控制：关闭即后台常驻，可在此恢复或彻底退出
async function hideToTray() {
  try { await HideWindow() } catch (e) { ElMessage.error(String(e)) }
}
async function quitApp() {
  try {
    await ElMessageBox.confirm('确定要退出 ApiTool 吗？', '退出', { type: 'warning' })
      .then(() => QuitApp())
      .catch(() => {})
  } catch (e) { /* 取消 */ }
}

function filterNode(value, node) {
  if (!value) return true
  const v = value.toLowerCase()
  if (node.label.toLowerCase().includes(v)) return true
  // 接口节点额外匹配接口地址（url）
  if (node.type === 'api' && node.url && node.url.toLowerCase().includes(v)) return true
  return false
}
function onKeyword(v) { treeRef.value?.filter(v) }

function onNodeClick(node) {
  if (node.type === 'api') {
    store.currentApiId = node.id
  }
}

// ---------------- 批量选中 / 批量删除 ----------------
const multiSelect = ref(false)
const checkedApiIds = ref([])
const checkedCount = computed(() => checkedApiIds.value.length)

function onCheck() {
  if (!treeRef.value) { checkedApiIds.value = []; return }
  const nodes = treeRef.value.getCheckedNodes().filter(n => n.type === 'api')
  checkedApiIds.value = nodes.map(n => n.id)
}
// 切换项目时清空选择
function onSwitchProject(id) {
  switchProject(id)
  checkedApiIds.value = []
}
// 目录重建（增删/切换项目）后同步勾选状态
watch(treeData, () => { nextTick(onCheck) })

async function delSelected() {
  if (!checkedCount.value) return
  try {
    await ElMessageBox.confirm(
      '确定删除选中的 ' + checkedCount.value + ' 个接口？此操作不可恢复。',
      '批量删除', { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' })
    const p = currentProject()
    p.apis = p.apis.filter(a => !checkedApiIds.value.includes(a.id))
    for (const id of checkedApiIds.value) delete savedApiSnapshots[id]
    if (checkedApiIds.value.includes(store.currentApiId)) store.currentApiId = ''
    checkedApiIds.value = []
    saveNow()
    ElMessage.success('已删除选中接口')
  } catch { /* 取消 */ }
}

// ---------------- 目录 / 接口 拖拽排序 / 移动 ----------------
// 目录与接口都允许拖动
function allowDrag(node) { return node.data && (node.data.type === 'dir' || node.data.type === 'api') }
// 校验放置：目录不能拖进它自己的子孙目录
function allowDrop(dragNode, dropNode, type) {
  if (!dragNode || !dragNode.data) return false
  if (dragNode.data.type === 'dir' && dropNode && dropNode.data.type === 'dir') {
    let p = dropNode
    while (p) {
      if (p.data.id === dragNode.data.id) return false
      p = p.parent
    }
  }
  return true
}

// 拖拽结束后，按 el-tree 当前展示顺序回写 p.apis / p.dirs 的顺序与归属目录
function onNodeDrop() {
  try {
    const p = currentProject()
    if (!treeRef.value) return
    const root = treeRef.value.store.root
    const dirOrder = {}
    const apiOrder = {}
    const collect = (childNodes, parentId) => {
      dirOrder[parentId] ||= []
      apiOrder[parentId] ||= []
      for (const cn of childNodes) {
        if (cn.data.type === 'dir') {
          dirOrder[parentId].push(cn.data.id)
          const d = p.dirs.find(x => x.id === cn.data.id)
          if (d) d.parentId = parentId
          collect(cn.childNodes, cn.data.id)
        } else {
          apiOrder[parentId].push(cn.data.id)
          const a = p.apis.find(x => x.id === cn.data.id)
          if (a) a.dirId = parentId
        }
      }
    }
    collect(root.childNodes, '')
    // 在每个父级组内按树中的顺序重排，跨组相对顺序保持不变
    const reorderByGroup = (items, keyOf, orderMap) => {
      const groups = {}
      for (const it of items) (groups[keyOf(it)] ||= []).push(it)
      for (const key in groups) {
        const ids = orderMap[key] || []
        groups[key].sort((x, y) => ids.indexOf(x.id) - ids.indexOf(y.id))
      }
    }
    reorderByGroup(p.dirs, d => d.parentId, dirOrder)
    reorderByGroup(p.apis, a => a.dirId, apiOrder)
    saveNow()
  } catch (e) {
    console.error('拖拽回写失败', e)
  }
}

async function rename(node) {
  try {
    const { value } = await ElMessageBox.prompt('请输入新名称', '重命名', {
      inputValue: node.label, confirmButtonText: '确定', cancelButtonText: '取消',
    })
    if (!value) return
    const p = currentProject()
    if (node.type === 'dir') {
      const d = p.dirs.find(x => x.id === node.id)
      if (d) d.name = value
    } else {
      const a = p.apis.find(x => x.id === node.id)
      if (a) a.name = value
    }
  } catch { /* 取消 */ }
}

async function del(node) {
  try {
    await ElMessageBox.confirm(
      node.type === 'dir' ? '删除目录会同时删除其下所有子目录和接口，确定删除？' : '确定删除该接口？',
      '删除确认', { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' })
    node.type === 'dir' ? removeDir(node.id) : removeApi(node.id)
    ElMessage.success('已删除')
  } catch { /* 取消 */ }
}

function duplicate(node) {
  const a = currentProject().apis.find(x => x.id === node.id)
  if (!a) return
  const copy = normalizeApi(JSON.parse(JSON.stringify(a)))
  copy.id = uid()
  copy.name = a.name + ' 副本'
  currentProject().apis.push(copy)
  store.currentApiId = copy.id
}

function handleCmd(cmd, node) {
  if (cmd === 'addDir') newDir(node.id)
  else if (cmd === 'addApi') openNewApi(node.id)
  else if (cmd === 'rename') rename(node)
  else if (cmd === 'delete') del(node)
  else if (cmd === 'duplicate') duplicate(node)
}

// 新增目录：弹窗输入名称
async function newDir(parentId = '') {
  try {
    const { value } = await ElMessageBox.prompt('请输入目录名称', '新建目录', {
      inputValue: '新建目录', confirmButtonText: '创建', cancelButtonText: '取消',
    })
    const name = (value || '').trim()
    if (!name) return
    const d = addDir(parentId)
    d.name = name
    saveNow()
    ElMessage.success('已创建目录：' + d.name)
  } catch { /* 取消 */ }
}

// 右键菜单
const ctx = reactive({ visible: false, x: 0, y: 0, node: null })
function onCtxMenu(e, data) {
  ctx.node = data
  ctx.x = e.clientX
  ctx.y = e.clientY
  ctx.visible = true
}
function ctxCmd(cmd) {
  if (ctx.node) handleCmd(cmd, ctx.node)
  ctx.visible = false
}
function closeCtx() { ctx.visible = false }

async function newProject() {
  try {
    const { value } = await ElMessageBox.prompt('请输入项目名称', '新建项目', {
      inputValue: '新项目', confirmButtonText: '创建', cancelButtonText: '取消',
    })
    if (!value) return
    const p = addProject()
    p.name = value
    ElMessage.success('已创建项目：' + p.name)
  } catch { /* 取消 */ }
}

function onProjCmd(cmd) {
  if (cmd === 'new') newProject()
  else if (cmd === 'rename') renameProjectNow()
  else if (cmd === 'delete') removeProjectNow()
}

// 重命名当前项目
async function renameProjectNow() {
  const p = currentProject()
  try {
    const { value } = await ElMessageBox.prompt('请输入项目名称', '重命名项目', {
      inputValue: p.name, confirmButtonText: '确定', cancelButtonText: '取消',
    })
    const name = (value || '').trim()
    if (!name) return
    p.name = name
    saveNow()
    ElMessage.success('项目已重命名为：' + name)
  } catch { /* 取消 */ }
}

// 删除当前项目（至少保留一个）
async function removeProjectNow() {
  if (store.data.projects.length <= 1) {
    ElMessage.warning('至少需保留一个项目')
    return
  }
  const p = currentProject()
  try {
    await ElMessageBox.confirm('确定删除项目「' + p.name + '」？该操作不可恢复。', '删除项目', {
      type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消',
    })
    removeProject(p.id)
    ElMessage.success('已删除项目：' + p.name)
  } catch { /* 取消 */ }
}
</script>

<template>
  <div class="sidebar">
    <div class="sb-proj">
      <el-select v-model="store.data.currentProjectId" size="small" style="flex:1; min-width:0"
        @change="onSwitchProject" title="切换项目">
        <el-option v-for="p in store.data.projects" :key="p.id" :label="p.name" :value="p.id" />
      </el-select>
      <el-dropdown trigger="click" @command="onProjCmd">
        <el-button size="small" title="项目操作">⋯</el-button>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item command="new">新建项目</el-dropdown-item>
            <el-dropdown-item command="rename">重命名当前项目</el-dropdown-item>
            <el-dropdown-item command="delete" divided>删除当前项目</el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
    </div>

    <div class="sb-tabs">
      <div class="sb-tab" :class="{ active: sidebarTab === 'tree' }" @click="setSbTab('tree')">目录</div>
      <div class="sb-tab" :class="{ active: sidebarTab === 'log' }" @click="setSbTab('log')">
        日志
        <span v-if="logErrorCount > 0" class="tab-badge">{{ logErrorCount > 99 ? '99+' : logErrorCount }}</span>
      </div>
    </div>

    <div v-show="sidebarTab === 'tree'" class="sb-body">
      <div class="sb-head">
        <el-input v-model="keyword" placeholder="搜索接口 / 目录" size="small" clearable @input="onKeyword" />
        <el-button size="small" :type="multiSelect ? 'primary' : 'default'" :plain="!multiSelect"
          title="进入后可勾选多个接口批量操作" @click="multiSelect = !multiSelect">多选</el-button>
        <el-button size="small" @click="newDir('')">+ 目录</el-button>
        <el-button size="small" type="primary" @click="openNewApi('')">+ 接口</el-button>
      </div>
      <div class="sb-tree" v-loading="store.treeLoading" element-loading-text="加载目录中…" @contextmenu.prevent>
        <el-tree ref="treeRef" :data="treeData" node-key="id" :props="{ label: 'label', children: 'children' }"
          :filter-node-method="filterNode" :expand-on-click-node="true" highlight-current
          :show-checkbox="multiSelect" @check="onCheck"
          draggable :allow-drag="allowDrag" :allow-drop="allowDrop" @node-drop="onNodeDrop"
          :current-node-key="store.currentApiId" @node-click="onNodeClick" @node-contextmenu="onCtxMenu">
          <template #default="{ data }">
            <div class="tree-node" :class="{ selected: data.type === 'api' && data.id === store.currentApiId, 'drag-node': data.type === 'api' }">
              <span v-if="data.type === 'dir'" class="dir-icon">📁</span>
              <span v-else class="method-tag" :class="'m-' + (data.method || 'GET')">{{ data.method || 'GET' }}</span>
              <span class="node-label" :title="data.label">{{ data.label }}</span>
              <el-dropdown trigger="click" @command="cmd => handleCmd(cmd, data)" @click.stop>
                <span class="more-btn" @click.stop>⋯</span>
                <template #dropdown>
                  <el-dropdown-menu>
                    <template v-if="data.type === 'dir'">
                      <el-dropdown-item command="addApi">新建接口</el-dropdown-item>
                      <el-dropdown-item command="addDir">新建子目录</el-dropdown-item>
                      <el-dropdown-item command="rename" divided>重命名</el-dropdown-item>
                      <el-dropdown-item command="delete">删除</el-dropdown-item>
                    </template>
                    <template v-else>
                      <el-dropdown-item command="rename">重命名</el-dropdown-item>
                      <el-dropdown-item command="duplicate">复制接口</el-dropdown-item>
                      <el-dropdown-item command="delete" divided>删除</el-dropdown-item>
                    </template>
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
            </div>
          </template>
        </el-tree>
        <div v-if="!treeData.length" class="sb-empty">暂无接口，点击上方「+ 接口」创建</div>
      </div>
    </div>

    <div v-show="sidebarTab === 'log'" class="sb-body sb-log">
      <LogPanel />
    </div>

    <el-dialog v-model="newApiVisible" title="新建接口" width="560px" @closed="resetNewApi">
      <el-tabs v-model="newApiTab">
        <el-tab-pane label="填写 / URL" name="form">
          <el-form label-width="80px">
            <el-form-item label="接口名称">
              <el-input v-model="newApiName" placeholder="如：获取用户列表" @keyup.enter="confirmNewApi" />
            </el-form-item>
            <el-form-item label="接口地址">
              <el-input v-model="newApiUrl" placeholder="粘贴完整 URL，如 https://api.example.com/user/list?page=1&size=10"
                @keyup.enter="confirmNewApi" />
            </el-form-item>
          </el-form>
          <div style="font-size:12px;color:#86909c">支持直接粘贴带 Query 参数的 URL，将自动解析出地址与 Query 参数</div>
        </el-tab-pane>
        <el-tab-pane label="粘贴命令行" name="cli">
          <div style="font-size:12px;color:#86909c;margin-bottom:8px">
            粘贴 curl / httpie / bash / powershell 命令，自动解析出方法、地址、请求头、Query 与请求体。
          </div>
          <el-input v-model="cliText" type="textarea" :rows="8" :placeholder="cliPlaceholder" />
          <div v-if="cliError" style="color:#f53f3f;font-size:12px;margin-top:6px">{{ cliError }}</div>
          <div v-else-if="cliParsed" style="font-size:12px;margin-top:6px;color:#00b42a">
            已识别：{{ cliParsed.method }} {{ cliParsed.url }}
            <span v-if="cliParsed.headersArr?.length">（{{ cliParsed.headersArr.length }} 请求头</span>
            <span v-if="cliParsed.queryArr?.length"> / {{ cliParsed.queryArr.length }} Query</span>
            <span v-if="cliParsed.headersArr?.length || cliParsed.queryArr?.length">）</span>
            <span v-if="cliParsed.body"> / 含请求体</span>
            <el-dropdown trigger="click" size="small" @command="copyParsedAs" style="margin-left:8px">
              <el-button size="small" type="primary" plain>复制为 ▾</el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item v-for="f in FORMATS" :key="f.key" :command="f.key">{{ f.label }}</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </div>
        </el-tab-pane>
      </el-tabs>
      <template #footer>
        <el-button @click="newApiVisible = false">取消</el-button>
        <el-button type="primary" @click="confirmNewApi">创建</el-button>
      </template>
    </el-dialog>

    <!-- 右键上下文菜单 -->
    <div v-if="ctx.visible" class="ctx-mask" @click="closeCtx" @contextmenu.prevent="closeCtx">
      <div class="ctx-menu" :style="{ left: ctx.x + 'px', top: ctx.y + 'px' }">
        <template v-if="checkedCount > 0">
          <div class="ctx-item danger" @click="delSelected">批量删除选中 ({{ checkedCount }})</div>
          <div class="ctx-sep"></div>
        </template>
        <template v-if="ctx.node && ctx.node.type === 'dir'">
          <div class="ctx-item" @click="ctxCmd('addApi')">新建接口</div>
          <div class="ctx-item" @click="ctxCmd('addDir')">新建子目录</div>
          <div class="ctx-item" @click="ctxCmd('rename')">重命名</div>
          <div class="ctx-item danger" @click="ctxCmd('delete')">删除目录</div>
        </template>
        <template v-else-if="ctx.node">
          <div class="ctx-item" @click="ctxCmd('rename')">重命名</div>
          <div class="ctx-item" @click="ctxCmd('duplicate')">复制接口</div>
          <div class="ctx-item danger" @click="ctxCmd('delete')">删除接口</div>
        </template>
      </div>
    </div>

    <div class="sb-footer">
      <span class="sb-tip">点窗口关闭将后台常驻，不退出</span>
      <div class="sb-foot-btns">
        <el-button size="small" text @click="hideToTray">隐藏窗口</el-button>
        <el-button size="small" text type="danger" @click="quitApp">退出</el-button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.sb-footer {
  flex-shrink: 0; border-top: 1px solid var(--border);
  padding: 8px 12px; display: flex; flex-direction: column; gap: 4px;
}
.sb-tip { font-size: 11px; color: #86909c; line-height: 1.4; }
.sb-foot-btns { display: flex; justify-content: flex-end; gap: 4px; }
.sb-proj { display: flex; gap: 6px; align-items: center; padding: 10px 12px 6px; }
.sb-tabs { display: flex; border-bottom: 1px solid #f0f1f3; flex-shrink: 0; }
.sb-tab {
  flex: 1; text-align: center; padding: 9px 0; font-size: 13px; color: #4e5969;
  cursor: pointer; position: relative; user-select: none;
}
.sb-tab:hover { color: #1f2329; }
.sb-tab.active { color: #165dff; font-weight: 600; }
.sb-tab.active::after {
  content: ''; position: absolute; left: 50%; bottom: -1px; transform: translateX(-50%);
  width: 28px; height: 2px; background: #165dff;
}
.tab-badge {
  display: inline-block; min-width: 16px; height: 16px; line-height: 16px; padding: 0 4px;
  margin-left: 4px; background: #f53f3f; color: #fff; font-size: 10px; font-weight: 700;
  border-radius: 8px; vertical-align: middle;
}
.sb-body { flex: 1; min-height: 0; display: flex; flex-direction: column; overflow: hidden; }
.sb-log { display: block; }
.sb-head { display: flex; gap: 6px; padding: 6px 12px 12px; border-bottom: 1px solid #f0f1f3; }
.sb-head .el-button + .el-button { margin-left: 0; }
.sb-tree { flex: 1; overflow: auto; padding: 8px 6px; }
.tree-node { display: flex; align-items: center; gap: 6px; width: 100%; overflow: hidden; padding-right: 4px; }
.tree-node.drag-node { cursor: grab; }
.tree-node.drag-node:active { cursor: grabbing; }
.node-label { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 13px; }
.dir-icon { font-size: 13px; }
.more-btn {
  visibility: hidden; color: #86909c; padding: 0 5px; border-radius: 4px; font-weight: 700;
}
.tree-node:hover .more-btn { visibility: visible; }
.more-btn:hover { background: #e5e6eb; color: #1f2329; }
.sb-empty { text-align: center; color: #c9cdd4; font-size: 12px; padding: 30px 0; }
.ctx-mask { position: fixed; inset: 0; z-index: 3000; }
.ctx-menu {
  position: fixed; min-width: 140px; background: #fff; border: 1px solid #e5e6eb;
  border-radius: 8px; box-shadow: 0 6px 24px rgba(0,0,0,.12); padding: 6px; z-index: 3001;
}
.ctx-item { padding: 8px 12px; font-size: 13px; border-radius: 6px; cursor: pointer; }
.ctx-item:hover { background: #f2f3f5; }
.ctx-item.danger { color: #f53f3f; }
.ctx-item.danger:hover { background: #ffece8; }
.ctx-sep { height: 1px; background: #f0f1f3; margin: 4px 0; }
</style>
