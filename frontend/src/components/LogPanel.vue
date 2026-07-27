<script setup>
import { computed } from 'vue'
import { ElMessage } from 'element-plus'
import { logStore, clearLogs } from '../store'

const levelMeta = {
  request:  { label: '请求', color: '#165dff', bg: '#e8f0ff' },
  response: { label: '响应', color: '#00b42a', bg: '#e8ffea' },
  error:    { label: '错误', color: '#f53f3f', bg: '#ffece8' },
  info:     { label: '信息', color: '#86909c', bg: '#f2f3f5' },
}

const errorCount = computed(() => logStore.entries.filter(e => e.type === 'error').length)

const list = computed(() => {
  const f = logStore.filter
  if (f === 'error') return logStore.entries.filter(e => e.type === 'error')
  if (f === 'request') return logStore.entries.filter(e => e.type === 'request' || e.type === 'response' || e.type === 'info')
  return logStore.entries
})

function isOpen(e) {
  // 调试模式默认展开；否则按用户点击状态
  if (logStore.debug) return true
  return !!logStore.expanded[e.id]
}
function toggle(e) {
  if (logStore.debug) return
  logStore.expanded[e.id] = !logStore.expanded[e.id]
}
function doClear() {
  clearLogs()
  ElMessage.success('已清空日志')
}
function setFilter(v) {
  logStore.filter = v
}
</script>

<template>
  <div class="log-panel">
    <div class="lp-head">
      <div class="lp-title">
        请求日志
        <span v-if="errorCount > 0" class="lp-err-badge">{{ errorCount }}</span>
      </div>
      <div class="lp-actions">
        <el-tooltip content="调试模式：记录请求/响应详情" placement="bottom">
          <el-switch v-model="logStore.debug" size="small" inline-prompt active-text="调试" inactive-text="精简" />
        </el-tooltip>
        <el-button size="small" text title="清空" @click="doClear">清空</el-button>
      </div>
    </div>

    <div class="lp-filter">
      <span class="lp-filter-label">筛选：</span>
      <span class="lp-chip" :class="{ active: logStore.filter === 'all' }" @click="setFilter('all')">全部</span>
      <span class="lp-chip" :class="{ active: logStore.filter === 'request' }" @click="setFilter('request')">请求/响应</span>
      <span class="lp-chip" :class="{ active: logStore.filter === 'error' }" @click="setFilter('error')">错误</span>
    </div>

    <div class="lp-list">
      <div v-if="!list.length" class="lp-empty">暂无日志。发送请求或开启调试后，这里会显示请求明细与错误。</div>
      <div v-for="e in list" :key="e.id" class="log-item" :class="'lv-' + e.type" @click="toggle(e)">
        <div class="li-head">
          <span class="li-tag" :style="{ color: levelMeta[e.type].color, background: levelMeta[e.type].bg }">{{ levelMeta[e.type].label }}</span>
          <span class="li-time">{{ e.time }}</span>
          <span class="li-title" :title="e.title">{{ e.title }}</span>
        </div>
        <pre v-if="isOpen(e) && e.detail" class="li-detail">{{ e.detail }}</pre>
      </div>
    </div>
  </div>
</template>

<style scoped>
.log-panel {
  height: 100%;
  background: #fff;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.lp-head {
  display: flex; align-items: center; justify-content: space-between;
  padding: 10px 12px; border-bottom: 1px solid #f0f1f3; flex-shrink: 0;
}
.lp-title { font-weight: 600; font-size: 14px; display: flex; align-items: center; gap: 8px; }
.lp-err-badge {
  background: #f53f3f; color: #fff; font-size: 11px; font-weight: 700;
  min-width: 18px; height: 18px; line-height: 18px; text-align: center;
  border-radius: 9px; padding: 0 5px;
}
.lp-actions { display: flex; align-items: center; gap: 4px; }
.lp-filter {
  display: flex; align-items: center; gap: 6px; padding: 6px 12px;
  border-bottom: 1px solid #f0f1f3; font-size: 12px; color: #86909c; flex-shrink: 0;
}
.lp-filter-label { flex-shrink: 0; }
.lp-chip {
  cursor: pointer; padding: 2px 8px; border-radius: 10px; background: #f2f3f5; color: #4e5969; user-select: none;
}
.lp-chip:hover { background: #e5e6eb; }
.lp-chip.active { background: #165dff; color: #fff; }
.lp-list { flex: 1; overflow: auto; padding: 6px; }
.lp-empty { text-align: center; color: #c9cdd4; font-size: 12px; padding: 30px 16px; line-height: 1.7; }
.log-item {
  border: 1px solid #eef0f3; border-radius: 8px; margin-bottom: 6px;
  background: #fafbfc; cursor: pointer; overflow: hidden;
}
.log-item:hover { border-color: #d0d3d9; }
.log-item.lv-error { background: #fff5f5; border-color: #ffd6d6; }
.log-item.lv-request { background: #f7faff; border-color: #d9e6ff; }
.log-item.lv-response { background: #f6fff8; border-color: #cdeed3; }
.li-head { display: flex; align-items: center; gap: 8px; padding: 7px 10px; }
.li-tag { font-size: 11px; font-weight: 700; padding: 1px 7px; border-radius: 4px; flex-shrink: 0; }
.li-time { color: #a9aeb8; font-size: 11px; font-family: Consolas, monospace; flex-shrink: 0; }
.li-title {
  flex: 1; min-width: 0; font-size: 12.5px; color: #1f2329; overflow: hidden;
  text-overflow: ellipsis; white-space: nowrap;
}
.lv-error .li-title { color: #f53f3f; font-weight: 600; }
.li-detail {
  margin: 0; padding: 8px 10px; border-top: 1px dashed #e5e6eb;
  background: #1d2129; color: #d7dbe0; border-radius: 0 0 8px 8px;
  font-family: Consolas, "Courier New", monospace; font-size: 12px;
  white-space: pre-wrap; word-break: break-all; max-height: 320px; overflow: auto;
}
.log-rail {
  width: 30px; flex-shrink: 0; background: #fff; border-right: 1px solid #e5e6eb;
  display: flex; flex-direction: column; align-items: center; gap: 8px;
  padding-top: 12px; cursor: pointer; position: relative;
}
.log-rail:hover { background: #f2f3f5; }
.rail-text { writing-mode: vertical-rl; letter-spacing: 4px; font-size: 13px; color: #4e5969; }
.rail-badge {
  position: absolute; top: 8px; right: 2px; background: #f53f3f; color: #fff;
  font-size: 10px; font-weight: 700; min-width: 16px; height: 16px; line-height: 16px;
  text-align: center; border-radius: 8px; padding: 0 4px;
}
</style>
