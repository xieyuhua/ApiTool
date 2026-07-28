<script setup>
import { onMounted, computed, ref, onBeforeUnmount } from 'vue'
import { store, initStore, currentApi, saveNow, currentProject, envDialogVisible, commonDialogVisible, openEnvDialog, openCommonDialog, initGenListener } from './store'
import Sidebar from './components/Sidebar.vue'
import DebugPanel from './components/DebugPanel.vue'
import ParamsPanel from './components/ParamsPanel.vue'
import DocPreview from './components/DocPreview.vue'
import DocCenter from './components/DocCenter.vue'
import SettingsPanel from './components/SettingsPanel.vue'
import TestCenter from './components/TestCenter.vue'
import EnvManager from './components/EnvManager.vue'
import CommonParams from './components/CommonParams.vue'
import CapturePanel from './components/CapturePanel.vue'

onMounted(initStore)
initGenListener()
window.addEventListener('beforeunload', saveNow)

// 目录栏宽度可拖拽调整（持久化到 localStorage）
const NAV_W = 60
const MIN_W = 180
const MAX_W = 600
const sidebarWidth = ref(Number(localStorage.getItem('sb-width')) || 280)
let resizing = false

function startResize(e) {
  resizing = true
  document.body.style.cursor = 'col-resize'
  document.body.style.userSelect = 'none'
  window.addEventListener('mousemove', onResize)
  window.addEventListener('mouseup', stopResize)
  e.preventDefault()
}
function onResize(e) {
  if (!resizing) return
  let w = e.clientX - NAV_W
  if (w < MIN_W) w = MIN_W
  if (w > MAX_W) w = MAX_W
  sidebarWidth.value = w
  localStorage.setItem('sb-width', String(w))
}
function stopResize() {
  resizing = false
  document.body.style.cursor = ''
  document.body.style.userSelect = ''
  window.removeEventListener('mousemove', onResize)
  window.removeEventListener('mouseup', stopResize)
}
onBeforeUnmount(stopResize)

const api = computed(() => currentApi())
const navs = [
  { key: 'workspace', label: '接口调试', icon: '⚡' },
  { key: 'docs', label: '文档中心', icon: '📄' },
  { key: 'testing', label: '接口测试', icon: '🧪' },
  { key: 'capture', label: '请求捕获', icon: '🌐' },
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

    <EnvManager v-model:visible="envDialogVisible" />
    <CommonParams v-model:visible="commonDialogVisible" />

    <template v-if="store.view === 'workspace'">
      <Sidebar :style="{ width: sidebarWidth + 'px' }" />
      <div class="sb-resizer" :class="{ active: resizing }" @mousedown="startResize" title="拖动调整目录宽度"></div>
      <div class="main-area">
        <div class="global-bar">
          <span class="gb-label">环境</span>
          <el-select v-model="currentProject().activeEnvId" size="small" class="gb-env" placeholder="无环境" title="当前环境变量">
            <el-option label="无环境" value="" />
            <el-option v-for="e in currentProject().environments" :key="e.id" :label="e.name" :value="e.id" />
          </el-select>
          <el-button size="small" text title="环境管理" @click="openEnvDialog">⚙ 环境</el-button>
          <el-button size="small" text title="公共参数（对所有接口自动附加）" @click="openCommonDialog">☰ 公共</el-button>
        </div>
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
    <TestCenter v-else-if="store.view === 'testing'" />
    <CapturePanel v-else-if="store.view === 'capture'" />
    <SettingsPanel v-else-if="store.view === 'settings'" />
  </div>

  <div v-if="!store.loaded" class="boot-loading">
    <div class="spinner"></div>
    <div class="boot-text">正在加载接口数据…</div>
  </div>
</template>

<style scoped>
.global-bar {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  padding: 8px 22px;
  background: #fff;
  border-bottom: 1px solid #e5e6eb;
}
.gb-label { font-size: 12px; color: #86909c; margin-right: 2px; }
.global-bar .gb-env { width: 140px; }
.global-bar :deep(.el-button.is-text) { color: #86909c; padding: 4px 8px; font-size: 12px; }
.global-bar :deep(.el-button.is-text:hover) { color: #165dff; background: #f2f3f5; }
.api-header {
  display: flex; align-items: center; gap: 10px;
  padding: 12px 22px 0; background: #fff;
}
.api-name-input { width: 260px; font-weight: 600; }
.api-desc-input { flex: 1; }
.main-tabs { background: #fff; padding: 0 22px; border-bottom: 1px solid #e5e6eb; }
.main-tabs :deep(.el-tabs__header) { margin-bottom: 0; }
.sb-resizer {
  width: 5px; flex-shrink: 0; cursor: col-resize; background: transparent;
  position: relative; z-index: 5; transition: background .15s;
}
.sb-resizer:hover, .sb-resizer.active { background: #165dff; }
.boot-loading {
  position: fixed; inset: 0; z-index: 9999; background: #f5f6f8;
  display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 16px;
}
.boot-loading .spinner {
  width: 38px; height: 38px; border: 4px solid #e5e6eb; border-top-color: #165dff;
  border-radius: 50%; animation: spin .8s linear infinite;
}
.boot-loading .boot-text { color: #86909c; font-size: 14px; }
@keyframes spin { to { transform: rotate(360deg); } }
</style>
