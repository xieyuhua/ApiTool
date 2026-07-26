<script setup>
import { computed, ref, reactive } from 'vue'
import { ElMessageBox, ElMessage } from 'element-plus'
import { store, buildTree, addDir, addApi, removeDir, removeApi, uid, normalizeApi, currentProject, switchProject, addProject, removeProject, saveNow } from '../store'

const keyword = ref('')
const treeRef = ref(null)
const treeData = computed(() => buildTree())

// 新建接口（支持 URL 解析导入）
const newApiVisible = ref(false)
const newApiName = ref('')
const newApiUrl = ref('')
const newApiParentId = ref('')

function openNewApi(parentId) {
  newApiParentId.value = parentId || ''
  newApiName.value = ''
  newApiUrl.value = ''
  newApiVisible.value = true
}

function confirmNewApi() {
  const api = addApi(newApiParentId.value)
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

function filterNode(value, node) {
  if (!value) return true
  return node.label.toLowerCase().includes(value.toLowerCase())
}
function onKeyword(v) { treeRef.value?.filter(v) }

function onNodeClick(node) {
  if (node.type === 'api') {
    store.currentApiId = node.id
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
        @change="switchProject" title="切换项目">
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
    <div class="sb-head">
      <el-input v-model="keyword" placeholder="搜索接口 / 目录" size="small" clearable @input="onKeyword" />
      <el-button size="small" @click="newDir('')">+ 目录</el-button>
      <el-button size="small" type="primary" @click="openNewApi('')">+ 接口</el-button>
    </div>
    <div class="sb-tree" @contextmenu.prevent>
      <el-tree ref="treeRef" :data="treeData" node-key="id" :props="{ label: 'label', children: 'children' }"
        :filter-node-method="filterNode" :expand-on-click-node="true" default-expand-all highlight-current
        :current-node-key="store.currentApiId" @node-click="onNodeClick" @node-contextmenu="onCtxMenu">
        <template #default="{ data }">
          <div class="tree-node" :class="{ selected: data.type === 'api' && data.id === store.currentApiId }">
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

    <el-dialog v-model="newApiVisible" title="新建接口" width="520px">
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
      <template #footer>
        <el-button @click="newApiVisible = false">取消</el-button>
        <el-button type="primary" @click="confirmNewApi">创建</el-button>
      </template>
    </el-dialog>

    <!-- 右键上下文菜单 -->
    <div v-if="ctx.visible" class="ctx-mask" @click="closeCtx" @contextmenu.prevent="closeCtx">
      <div class="ctx-menu" :style="{ left: ctx.x + 'px', top: ctx.y + 'px' }">
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
  </div>
</template>

<style scoped>
.sb-proj { display: flex; gap: 6px; align-items: center; padding: 10px 12px 6px; }
.sb-head { display: flex; gap: 6px; padding: 6px 12px 12px; border-bottom: 1px solid #f0f1f3; }
.sb-head .el-button + .el-button { margin-left: 0; }
.sb-tree { flex: 1; overflow: auto; padding: 8px 6px; }
.tree-node { display: flex; align-items: center; gap: 6px; width: 100%; overflow: hidden; padding-right: 4px; }
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
</style>
