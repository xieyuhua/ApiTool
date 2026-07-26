<script setup>
import { ref, computed } from 'vue'
import { store, uid, newEnvVar, currentProject } from '../store'
import KVEditor from './KVEditor.vue'

const visible = defineModel('visible')
const activeId = ref('')
const cur = computed(() => currentProject().environments.find(e => e.id === activeId.value) || null)

function addEnv() {
  const e = { id: uid(), name: '新环境', vars: [] }
  currentProject().environments.push(e)
  activeId.value = e.id
}
function delEnv(id) {
  const envs = currentProject().environments
  currentProject().environments = envs.filter(e => e.id !== id)
  if (activeId.value === id) activeId.value = ''
  if (currentProject().activeEnvId === id) currentProject().activeEnvId = ''
}
function useEnv(id) {
  currentProject().activeEnvId = currentProject().activeEnvId === id ? '' : id
}
function addVar() {
  if (!cur.value) return
  cur.value.vars.push(newEnvVar())
}
</script>

<template>
  <el-dialog v-model="visible" title="环境管理" width="760px" top="6vh">
    <div class="env-wrap">
      <div class="env-list">
        <el-button size="small" type="primary" plain style="width:100%; margin-bottom:8px" @click="addEnv">+ 新建环境</el-button>
        <div v-for="e in currentProject().environments" :key="e.id"
          class="env-item" :class="{ active: e.id === activeId }" @click="activeId = e.id">
          <span class="env-name">{{ e.name }}</span>
          <span class="env-ops">
            <el-tag v-if="currentProject().activeEnvId === e.id" size="small" type="success" effect="plain">使用中</el-tag>
            <span class="op" @click.stop="useEnv(e.id)" title="设为当前环境">✓</span>
            <span class="op danger" @click.stop="delEnv(e.id)" title="删除环境">✕</span>
          </span>
        </div>
        <div v-if="!currentProject().environments.length" class="env-empty">还没有环境，点击上方新建</div>
      </div>

      <div class="env-edit" v-if="cur">
        <el-form label-width="70px">
          <el-form-item label="环境名称">
            <el-input v-model="cur.name" placeholder="如：开发环境 / 生产环境" />
          </el-form-item>
        </el-form>
        <div class="kv-title">环境变量（在 URL / Header / Body / Query 中以 <code>{{ name }}</code> 引用）</div>
        <KVEditor :items="cur.vars" key-placeholder="变量名" />
        <div style="margin-top:6px">
          <el-button link type="primary" @click="addVar">+ 添加变量</el-button>
        </div>
      </div>
      <div class="env-edit env-empty" v-else>请选择左侧环境，或新建一个环境</div>
    </div>
    <template #footer>
      <span style="font-size:12px;color:#86909c">提示：环境仅对当前项目生效；发送请求时，当前环境的变量会自动替换接口中的 {{ 变量名 }}</span>
    </template>
  </el-dialog>
</template>

<style scoped>
.env-wrap { display: flex; gap: 16px; min-height: 360px; }
.env-list { width: 220px; flex-shrink: 0; border-right: 1px solid #f0f1f3; padding-right: 12px; overflow:auto; }
.env-item { display:flex; align-items:center; justify-content:space-between; padding:8px 10px; border-radius:6px; cursor:pointer; }
.env-item:hover { background:#f2f3f5; }
.env-item.active { background:#e8f0ff; }
.env-name { font-size:13px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.env-ops { display:flex; align-items:center; gap:6px; }
.op { color:#86909c; padding:0 4px; }
.op:hover { color:#165dff; }
.op.danger:hover { color:#f53f3f; }
.env-edit { flex:1; min-width:0; }
.kv-title { font-size:12px; color:#4e5969; margin-bottom:8px; }
.kv-title code { background:#f2f3f5; padding:1px 5px; border-radius:4px; }
.env-empty { color:#c9cdd4; font-size:13px; padding:20px 0; text-align:center; }
</style>
