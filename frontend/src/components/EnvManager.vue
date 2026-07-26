<script setup>
import { ref, computed, watch } from 'vue'
import { uid, currentProject } from '../store'
import KVEditor from './KVEditor.vue'

const visible = defineModel('visible')
const activeId = ref('')
const cur = computed(() => currentProject().environments.find(e => e.id === activeId.value) || null)

// 打开时自动选中当前激活环境，否则选中第一个
watch(visible, (v) => {
  if (!v) return
  const envs = currentProject().environments
  if (!envs.length) { activeId.value = ''; return }
  activeId.value = envs.find(e => e.id === currentProject().activeEnvId)?.id || envs[0].id
})

function selectEnv(id) { activeId.value = id }
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
</script>

<template>
  <el-dialog v-model="visible" title="环境管理" width="920px" top="5vh">
    <div class="env-wrap">
      <!-- 环境选择：横向芯片 -->
      <div class="env-bar">
        <div class="env-chips">
          <span v-for="e in currentProject().environments" :key="e.id"
            class="env-chip" :class="{ active: e.id === activeId, using: currentProject().activeEnvId === e.id }"
            @click="selectEnv(e.id)">
            <span class="chip-name">{{ e.name }}</span>
            <span v-if="currentProject().activeEnvId === e.id" class="chip-tag">使用中</span>
            <span class="chip-op" title="设为当前环境" @click.stop="useEnv(e.id)">✓</span>
            <span class="chip-op danger" title="删除环境" @click.stop="delEnv(e.id)">✕</span>
          </span>
          <el-button size="small" type="primary" plain @click="addEnv">＋ 新建环境</el-button>
        </div>
      </div>

      <!-- 编辑区 -->
      <div v-if="cur" class="env-edit">
        <div class="edit-head">
          <div class="name-row">
            <span class="lbl">环境名称</span>
            <el-input v-model="cur.name" placeholder="如：开发环境 / 生产环境" style="max-width:320px" />
          </div>
          <div class="tip">
            在 URL / Header / Body / Query 中以 <code>{{ 变量名 }}</code> 引用；发送请求时，当前环境的变量会自动替换接口中的占位符。
          </div>
        </div>

        <div class="kv-title">环境变量（共 {{ cur.vars.length }} 项）</div>
        <!-- 变量行的新增/删除由 KVEditor 自带的「添加一行」按钮负责，无需再单独提供「添加变量」入口 -->
        <KVEditor :items="cur.vars" key-placeholder="变量名" />
      </div>

      <div v-else class="env-empty">
        <div class="empty-text">还没有选择环境</div>
        <el-button type="primary" @click="addEnv">＋ 新建环境</el-button>
      </div>
    </div>
  </el-dialog>
</template>

<style scoped>
.env-wrap { display: flex; flex-direction: column; min-height: 380px; }
.env-bar { padding-bottom: 14px; border-bottom: 1px solid #f0f1f3; }
.env-chips { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; }
.env-chip {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 7px 10px; border-radius: 8px; cursor: pointer;
  background: #f2f3f5; border: 1px solid transparent; font-size: 13px;
  transition: all .15s;
}
.env-chip:hover { background: #e9eaed; }
.env-chip.active { background: #e8f0ff; border-color: #165dff; color: #165dff; }
.chip-name { max-width: 160px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.chip-tag { font-size: 11px; background: #e8ffea; color: #00b42a; border-radius: 4px; padding: 1px 5px; }
.env-chip.using { font-weight: 600; }
.chip-op { color: #86909c; padding: 0 2px; font-size: 12px; }
.chip-op:hover { color: #165dff; }
.chip-op.danger:hover { color: #f53f3f; }

.env-edit { padding-top: 16px; flex: 1; }
.edit-head { margin-bottom: 16px; }
.name-row { display: flex; align-items: center; gap: 12px; }
.lbl { font-size: 13px; color: #4e5969; flex-shrink: 0; }
.tip { font-size: 12px; color: #86909c; margin-top: 10px; line-height: 1.6; }
.tip code { background: #f2f3f5; padding: 1px 5px; border-radius: 4px; font-family: Consolas, monospace; color: #165dff; }
.kv-title { font-size: 13px; font-weight: 600; color: #1f2329; margin-bottom: 12px; }
.add-row { margin-top: 10px; }

.env-empty { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 14px; }
.empty-text { color: #c9cdd4; font-size: 14px; }
</style>
