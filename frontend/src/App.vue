<script setup>
import { onMounted, computed } from 'vue'
import { store, initStore, currentApi, saveNow } from './store'
import Sidebar from './components/Sidebar.vue'
import DebugPanel from './components/DebugPanel.vue'
import ParamsPanel from './components/ParamsPanel.vue'
import DocPreview from './components/DocPreview.vue'
import DocCenter from './components/DocCenter.vue'
import SettingsPanel from './components/SettingsPanel.vue'

onMounted(initStore)
window.addEventListener('beforeunload', saveNow)

const api = computed(() => currentApi())
const navs = [
  { key: 'workspace', label: '接口调试', icon: '⚡' },
  { key: 'docs', label: '文档中心', icon: '📄' },
  { key: 'settings', label: '设置', icon: '⚙' },
]
</script>

<template>
  <div class="shell">
    <div class="navbar">
      <div class="logo">Ap</div>
      <div v-for="n in navs" :key="n.key" class="nav-item" :class="{ active: store.view === n.key }"
        @click="store.view = n.key">
        <span class="ni-icon">{{ n.icon }}</span>{{ n.label }}
      </div>
    </div>

    <template v-if="store.view === 'workspace'">
      <Sidebar />
      <div class="main-area">
        <template v-if="api">
          <div class="api-header">
            <span class="method-tag" :class="'m-' + api.method">{{ api.method }}</span>
            <el-input v-model="api.name" class="api-name-input" placeholder="接口名称" />
            <el-input v-model="api.description" placeholder="接口说明（用于文档与 AI 理解接口）" class="api-desc-input" />
          </div>
          <el-tabs v-model="store.activeTab" class="main-tabs">
            <el-tab-pane label="接口调试" name="debug" />
            <el-tab-pane label="参数设置" name="params" />
            <el-tab-pane label="文档预览" name="doc" />
          </el-tabs>
          <DebugPanel v-show="store.activeTab === 'debug'" :api="api" />
          <ParamsPanel v-show="store.activeTab === 'params'" :api="api" />
          <DocPreview v-if="store.activeTab === 'doc'" :api="api" />
        </template>
        <div v-else class="empty-state">
          <div class="big">🛠</div>
          <div>从左侧选择接口，或点击「+ 接口」新建一个接口开始调试</div>
          <div style="font-size:12px">调试过程中会自动生成接口文档</div>
        </div>
      </div>
    </template>

    <DocCenter v-else-if="store.view === 'docs'" />
    <SettingsPanel v-else-if="store.view === 'settings'" />
  </div>
</template>

<style scoped>
.api-header {
  display: flex; align-items: center; gap: 10px;
  padding: 12px 22px 0; background: #fff;
}
.api-name-input { width: 260px; font-weight: 600; }
.api-desc-input { flex: 1; }
.main-tabs { background: #fff; padding: 0 22px; border-bottom: 1px solid #e5e6eb; }
.main-tabs :deep(.el-tabs__header) { margin-bottom: 0; }
</style>
