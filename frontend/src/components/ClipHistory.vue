<template>
  <transition name="clip-fade">
    <div v-if="visible" class="clip-overlay" @click.self="close">
      <div class="clip-popup" ref="popup" tabindex="-1" @keydown="onKey">
        <div class="clip-head">
          <span class="clip-title">剪贴板历史 <small>({{ items.length }})</small></span>
          <div class="clip-actions">
            <el-button link size="small" @click="clearAll">清空</el-button>
            <el-button link size="small" @click="close">关闭 (Esc)</el-button>
          </div>
        </div>
        <el-input v-model="filter" placeholder="筛选…" size="small" class="clip-filter" />
        <div class="clip-list">
          <div
            v-for="(it, i) in filtered"
            :key="it.id"
            :class="['clip-item', { active: i === selIndex }]"
            @mouseenter="selIndex = i"
            @click="choose(it)"
          >
            <pre class="clip-text">{{ preview(it.text) }}</pre>
            <div class="clip-meta">
              <span>{{ timeText(it.time) }}</span>
              <el-button link type="danger" size="small" @click.stop="remove(it)">删除</el-button>
            </div>
          </div>
          <div v-if="!filtered.length" class="clip-empty">暂无记录</div>
        </div>
        <div class="clip-foot">↑ ↓ 选择 · Enter 复制 · Esc 关闭</div>
      </div>
    </div>
  </transition>
</template>

<script setup>
import { ref, computed, watch, nextTick } from 'vue'
import { store, clipboardHistoryVisible, removeClip, clearClipHistory } from '../store'
import { SetClipboardText } from '../../wailsjs/go/main/App'
import { ElMessage } from 'element-plus'

const visible = clipboardHistoryVisible
const items = computed(() => store.data.clipboard.history)
const filter = ref('')
const selIndex = ref(0)
const popup = ref(null)

const filtered = computed(() => {
  const f = filter.value.trim().toLowerCase()
  if (!f) return items.value
  return items.value.filter(x => (x.text || '').toLowerCase().includes(f))
})

watch(visible, (v) => {
  if (v) {
    selIndex.value = 0
    filter.value = ''
    nextTick(() => { if (popup.value) popup.value.focus() })
  }
})

function preview(t) {
  return (t || '').replace(/\s+/g, ' ').slice(0, 300)
}
function timeText(t) {
  if (!t) return ''
  const d = new Date(t)
  if (isNaN(d.getTime())) return t
  return d.toLocaleString()
}
function close() { visible.value = false }
async function copyText(text) {
  try { await SetClipboardText(text) }
  catch (e) {
    try { await navigator.clipboard.writeText(text) } catch (_) { /* ignore */ }
  }
}
async function choose(it) {
  await copyText(it.text)
  ElMessage.success('已复制到剪贴板')
  close()
}
function remove(it) { removeClip(it.id) }
function clearAll() { clearClipHistory() }
function onKey(e) {
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    selIndex.value = Math.min(selIndex.value + 1, filtered.value.length - 1)
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    selIndex.value = Math.max(selIndex.value - 1, 0)
  } else if (e.key === 'Enter') {
    e.preventDefault()
    const it = filtered.value[selIndex.value]
    if (it) choose(it)
  } else if (e.key === 'Escape') {
    e.preventDefault()
    close()
  }
}
</script>

<style scoped>
.clip-overlay {
  position: fixed; inset: 0; z-index: 3000;
  background: rgba(0, 0, 0, 0.25);
  display: flex; align-items: center; justify-content: center;
}
.clip-popup {
  width: 640px; max-width: 92vw; max-height: 70vh;
  background: #fff; border-radius: 10px; box-shadow: 0 12px 40px rgba(0,0,0,.2);
  display: flex; flex-direction: column; outline: none; overflow: hidden;
}
.clip-head {
  display: flex; align-items: center; justify-content: space-between;
  padding: 12px 16px; border-bottom: 1px solid #e5e6eb;
}
.clip-title { font-weight: 600; color: #1d2129; }
.clip-title small { color: #86909c; font-weight: 400; }
.clip-actions :deep(.el-button.is-link) { font-size: 12px; }
.clip-filter { padding: 10px 16px 0; }
.clip-list { padding: 8px 8px; overflow-y: auto; flex: 1; }
.clip-item {
  border: 1px solid transparent; border-radius: 8px; padding: 8px 10px;
  cursor: pointer; transition: background .12s;
}
.clip-item:hover, .clip-item.active { background: #f2f3f5; border-color: #c9cdd4; }
.clip-text {
  margin: 0; font-family: ui-monospace, Menlo, Consolas, monospace;
  font-size: 12px; color: #1d2129; white-space: pre-wrap; word-break: break-all;
  max-height: 60px; overflow: hidden;
}
.clip-meta {
  display: flex; align-items: center; justify-content: space-between;
  margin-top: 4px; color: #86909c; font-size: 11px;
}
.clip-empty { text-align: center; color: #86909c; padding: 30px; }
.clip-foot {
  padding: 8px 16px; border-top: 1px solid #e5e6eb; color: #86909c; font-size: 11px;
  text-align: center;
}
.clip-fade-enter-active, .clip-fade-leave-active { transition: opacity .15s; }
.clip-fade-enter-from, .clip-fade-leave-to { opacity: 0; }
</style>
