<script setup>
import { onMounted, computed, ref, onBeforeUnmount } from 'vue'
import { store, initStore, currentApi, saveNow, currentProject, envDialogVisible, commonDialogVisible, openEnvDialog, openCommonDialog, initGenListener, initClipboardMonitor } from './store'
import { EventsOn, EventsOff } from '../wailsjs/runtime/runtime'
import { RunTestCases, CloseClipboardWindow } from '../wailsjs/go/main/App'
import Sidebar from './components/layout/Sidebar.vue'
import DebugPanel from './components/common/DebugPanel.vue'
import ParamsPanel from './components/common/ParamsPanel.vue'
import DocPreview from './components/doc/DocPreview.vue'
import DocCenter from './components/doc/DocCenter.vue'
import SettingsPanel from './components/settings/SettingsPanel.vue'
import TestCenter from './components/test/TestCenter.vue'
import EnvManager from './components/common/EnvManager.vue'
import CommonParams from './components/common/CommonParams.vue'
import CapturePanel from './components/common/CapturePanel.vue'
import Tools from './components/tools/Tools.vue'
import ClipboardHistory from './components/clipboard/ClipboardHistory.vue'

const clipVisible = ref(false)

onMounted(() => {
  initStore()
  initClipboardMonitor()
  // 系统托盘「运行全部测试」：执行当前项目全部用例
  EventsOn('apitool:tray-run-tests', async () => {
    try {
      const p = currentProject()
      await RunTestCases({ ProjectID: p.id, TestCaseIDs: [], EnvID: p.activeEnvId || '', Concurrency: 3 })
    } catch (e) {
      console.error('托盘触发运行测试失败', e)
    }
  })
  // 全局快捷键（Ctrl+Shift+V / Ctrl+`）与托盘菜单由 Go 端 WH_KEYBOARD_LL 钩子 /
  // systray 触发，Go 端负责弹出窗口并显示「剪贴板历史」浮层，这里只负责渲染。
  EventsOn('apitool:show-clipboard-history', () => {
    clipVisible.value = true
    initClipboardMonitor() // 重新从 Go 拉取最新历史
  })
  EventsOn('apitool:hide-clipboard-history', () => {
    clipVisible.value = false
  })
  EventsOn('apitool:clipboard-updated', () => {
    if (clipVisible.value) initClipboardMonitor()
  })
})

onBeforeUnmount(() => {
  EventsOff('apitool:tray-run-tests')
  EventsOff('apitool:show-clipboard-history')
  EventsOff('apitool:hide-clipboard-history')
  EventsOff('apitool:clipboard-updated')
})

// 关闭剪贴板历史浮层：隐藏前端覆盖层，并由 Go 端恢复窗口置顶状态/聚焦
async function closeClip() {
  clipVisible.value = false
  try { await CloseClipboardWindow() } catch (e) { /* ignore */ }
}
initGenListener()
window.addEventListener('beforeunload', saveNow)

// 将快捷键组合字符串（如 "Ctrl+Shift+V" / "Meta+Shift+V" / "Ctrl+`"）解析为匹配器
function matchCombo(e, combo) {
  if (!combo) return false
  const parts = combo.split('+').map(s => s.trim())
  const keyPart = parts[parts.length - 1].toLowerCase()
  const needCtrl = parts.includes('Ctrl')
  const needMeta = parts.includes('Meta') || parts.includes('Cmd')
  const needShift = parts.includes('Shift')
  const needAlt = parts.includes('Alt')
  const map = { '`': '`', 'space': ' ', 'esc': 'escape', 'up': 'arrowup', 'down': 'arrowdown', 'left': 'arrowleft', 'right': 'arrowright', 'enter': 'enter' }
  const raw = e.key === ' ' ? 'space' : e.key
  const norm = (map[raw.toLowerCase()] || raw).toLowerCase()
  return (
    !!e.ctrlKey === needCtrl &&
    !!e.metaKey === needMeta &&
    !!e.shiftKey === needShift &&
    !!e.altKey === needAlt &&
    norm === keyPart
  )
}
// 注：剪贴板历史全局快捷键（Ctrl+Shift+V / Ctrl+`）已由 Go 端 WH_KEYBOARD_LL 钩子
// 直接弹出「原生剪贴板历史菜单」，不依赖 WebView，窗口隐藏到托盘时也能用，
// 因此前端不再监听该组合键，避免与 Go 端重复触发。

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
  { key: 'testing', label: '测试中心', icon: '🧪' },
  { key: 'capture', label: '请求捕获', icon: '🌐' },
  { key: 'tools', label: '工具', icon: '🔧' },
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
    <Tools v-else-if="store.view === 'tools'" />
    <SettingsPanel v-else-if="store.view === 'settings'" />
  </div>

  <ClipboardHistory v-if="clipVisible" @close="closeClip" />

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
  background: var(--surface);
  border-bottom: 1px solid var(--border);
}
.gb-label { font-size: 12px; color: var(--text-muted); margin-right: 2px; }
.global-bar .gb-env { width: 140px; }
.global-bar :deep(.el-button.is-text) { color: var(--text-muted); padding: 4px 8px; font-size: 12px; }
.global-bar :deep(.el-button.is-text:hover) { color: var(--primary); background: var(--surface-2); }
.api-header {
  display: flex; align-items: center; gap: 10px;
  padding: 12px 22px 0; background: var(--surface);
}
.api-name-input { width: 260px; font-weight: 600; }
.api-desc-input { flex: 1; }
.main-tabs { background: var(--surface); padding: 0 22px; border-bottom: 1px solid var(--border); }
.main-tabs :deep(.el-tabs__header) { margin-bottom: 0; }
.sb-resizer {
  width: 5px; flex-shrink: 0; cursor: col-resize; background: transparent;
  position: relative; z-index: 5; transition: background .15s;
}
.sb-resizer:hover, .sb-resizer.active { background: var(--primary); }
.boot-loading {
  position: fixed; inset: 0; z-index: 9999; background: var(--bg);
  display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 16px;
}
.boot-loading .spinner {
  width: 38px; height: 38px; border: 4px solid var(--border); border-top-color: var(--primary);
  border-radius: 50%; animation: spin .8s linear infinite;
}
.boot-loading .boot-text { color: var(--text-muted); font-size: 14px; }
@keyframes spin { to { transform: rotate(360deg); } }
</style>
