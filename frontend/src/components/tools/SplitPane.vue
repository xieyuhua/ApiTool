<script setup>
import { ref, onBeforeUnmount } from 'vue'

const props = defineProps({
  // 左右最小像素宽度，防止拖到不可见
  min: { type: Number, default: 180 },
  // 拖拽位置持久化到 localStorage 的 key（为空则不持久化）
  storageKey: { type: String, default: '' },
  // 默认左侧占比（百分比）
  defaultLeft: { type: Number, default: 50 },
})

const containerRef = ref(null)
const leftPct = ref(props.defaultLeft)
const dragging = ref(false)

// 读取持久化的分栏位置
if (props.storageKey && typeof localStorage !== 'undefined') {
  const saved = localStorage.getItem(props.storageKey)
  if (saved !== null && !isNaN(Number(saved))) leftPct.value = Number(saved)
}

function clamp(pct, rectWidth) {
  const minPct = (props.min / rectWidth) * 100
  const maxPct = 100 - minPct
  return Math.max(minPct, Math.min(maxPct, pct))
}

function onMove(e) {
  if (!dragging.value || !containerRef.value) return
  const rect = containerRef.value.getBoundingClientRect()
  const pct = ((e.clientX - rect.left) / rect.width) * 100
  leftPct.value = clamp(pct, rect.width)
}

function stop() {
  if (!dragging.value) return
  dragging.value = false
  document.body.style.cursor = ''
  document.body.style.userSelect = ''
  document.body.classList.remove('sp-resizing')
  window.removeEventListener('mousemove', onMove)
  window.removeEventListener('mouseup', stop)
  if (props.storageKey && typeof localStorage !== 'undefined') {
    localStorage.setItem(props.storageKey, String(leftPct.value))
  }
}

function start(e) {
  e.preventDefault()
  dragging.value = true
  document.body.style.cursor = 'col-resize'
  document.body.style.userSelect = 'none'
  document.body.classList.add('sp-resizing')
  window.addEventListener('mousemove', onMove)
  window.addEventListener('mouseup', stop)
}

// 双击分隔条重置为默认比例
function reset() {
  leftPct.value = props.defaultLeft
  if (props.storageKey && typeof localStorage !== 'undefined') {
    localStorage.setItem(props.storageKey, String(leftPct.value))
  }
}

onBeforeUnmount(stop)
</script>

<template>
  <div ref="containerRef" class="split-pane" :class="{ 'is-dragging': dragging }">
    <div class="sp-side sp-left" :style="{ width: leftPct + '%' }">
      <slot name="left" />
    </div>
    <div class="sp-divider" @mousedown="start" @dblclick="reset" title="拖拽调整宽度，双击复位">
      <span class="sp-grip" />
    </div>
    <div class="sp-side sp-right">
      <slot name="right" />
    </div>
  </div>
</template>

<style scoped>
.split-pane {
  display: flex;
  width: 100%;
  height: 100%;
  min-height: 0;
  overflow: hidden;
}
.sp-side {
  height: 100%;
  min-width: 0;
  display: flex;
  flex-direction: column;
}
.sp-right { flex: 1; }
.sp-divider {
  flex-shrink: 0;
  width: 9px;
  cursor: col-resize;
  display: flex;
  align-items: center;
  justify-content: center;
  background: transparent;
  position: relative;
  transition: background .15s;
}
.sp-divider::before {
  content: '';
  position: absolute;
  top: 0; bottom: 0; left: 50%;
  width: 1px;
  transform: translateX(-50%);
  background: #e5e6eb;
}
.sp-divider:hover, .split-pane.is-dragging .sp-divider {
  background: #eef3ff;
}
.sp-divider:hover::before, .split-pane.is-dragging .sp-divider::before {
  width: 2px;
  background: #165dff;
}
.sp-grip {
  width: 3px;
  height: 26px;
  border-radius: 3px;
  background: #c9cdd4;
  opacity: 0;
  transition: opacity .15s;
}
.sp-divider:hover .sp-grip, .split-pane.is-dragging .sp-grip {
  opacity: 1;
  background: #165dff;
}
</style>

<!-- 拖拽时禁止选中文本（全局，不受 scoped 限制） -->
<style>
.sp-resizing, .sp-resizing * { user-select: none !important; }
</style>
