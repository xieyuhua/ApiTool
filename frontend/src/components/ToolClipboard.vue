<script setup>
import { ref, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { store, removeClip, clearClipHistory, toggleClipboardHistory } from '../store'

const kw = ref('')
const list = computed(() => {
  const all = store.data.clipboard.history || []
  if (!kw.value.trim()) return all
  const q = kw.value.toLowerCase()
  return all.filter(x => (x.text || '').toLowerCase().includes(q))
})
const total = computed(() => (store.data.clipboard.history || []).length)
const maxItems = computed(() => (store.data.settings.clipboard.maxItems) || 200)
const monitor = computed(() => store.data.settings.clipboard.monitor)

function fmtTime(t) {
  if (!t) return ''
  const d = new Date(t)
  const p = n => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}
function preview(text) {
  const s = (text || '').replace(/\s+/g, ' ')
  return s.length > 200 ? s.slice(0, 200) + '…' : s
}

async function copyItem(it) {
  const text = it.text || ''
  try {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      await navigator.clipboard.writeText(text)
    } else if (window.go && window.go.main && window.go.main.App && window.go.main.App.SetClipboardText) {
      await window.go.main.App.SetClipboardText(text)
    } else {
      throw new Error('当前环境不支持复制')
    }
    ElMessage.success('已复制到剪贴板')
  } catch (e) {
    ElMessage.error('复制失败：' + (e && e.message ? e.message : e))
  }
}
function delItem(it) { removeClip(it.id) }
async function clearAll() {
  try {
    await ElMessageBox.confirm(`确定清空全部 ${total.value} 条剪贴板记录？`, '提示', { type: 'warning' })
  } catch { return }
  clearClipHistory()
  ElMessage.success('已清空')
}
function openPopup() { toggleClipboardHistory(true) }
</script>

<template>
  <div class="clip-tool">
    <div class="clip-head">
      <div class="clip-stats">
        <span class="cs-num">{{ total }}</span>
        <span class="cs-label">条记录（上限 {{ maxItems }}）</span>
        <el-tag size="small" :type="monitor ? 'success' : 'info'" effect="light" style="margin-left:8px">
          {{ monitor ? '正在监听剪贴板' : '已停止监听' }}
        </el-tag>
      </div>
      <div class="clip-actions">
        <el-input v-model="kw" placeholder="搜索内容…" clearable size="default" style="width:220px" />
        <el-button @click="openPopup">弹出浮窗</el-button>
        <el-button type="danger" plain :disabled="!total" @click="clearAll">清空</el-button>
      </div>
    </div>

    <div v-if="!list.length" class="clip-empty">
      <div class="big">📋</div>
      <div>暂无剪贴板记录</div>
      <div class="tip">复制任意文本后，会自动记录到这里（可在「设置」中调整监听与上限）</div>
    </div>

    <div v-else class="clip-list">
      <div v-for="it in list" :key="it.id" class="clip-item">
        <div class="ci-main">
          <div class="ci-text" :title="it.text" @click="copyItem(it)">{{ preview(it.text) }}</div>
          <div class="ci-meta">
            <span>{{ fmtTime(it.time) }}</span>
            <span class="ci-len">{{ (it.text || '').length }} 字符</span>
          </div>
        </div>
        <div class="ci-ops">
          <el-button size="small" @click="copyItem(it)">复制</el-button>
          <el-button size="small" type="danger" text @click="delItem(it)">删除</el-button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.clip-tool { flex: 1; min-width: 0; height: 100%; display: flex; flex-direction: column; padding: 18px 22px; overflow: hidden; }
.clip-head {
  display: flex; align-items: center; justify-content: space-between; gap: 12px;
  flex-wrap: wrap; margin-bottom: 14px;
}
.clip-stats { display: flex; align-items: center; color: var(--text-muted); font-size: 13px; }
.cs-num { font-size: 22px; font-weight: 700; color: var(--primary); margin-right: 6px; }
.cs-label { margin-right: 4px; }
.clip-actions { display: flex; align-items: center; gap: 8px; }

.clip-empty {
  flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center;
  color: var(--text-muted); gap: 8px;
}
.clip-empty .big { font-size: 44px; }
.clip-empty .tip { font-size: 12px; opacity: .8; }

.clip-list { flex: 1; overflow: auto; display: flex; flex-direction: column; gap: 8px; }
.clip-item {
  display: flex; align-items: center; gap: 12px;
  background: var(--surface); border: 1px solid var(--border); border-radius: 8px;
  padding: 10px 12px; transition: border-color .15s;
}
.clip-item:hover { border-color: var(--primary); }
.ci-main { flex: 1; min-width: 0; }
.ci-text {
  font-size: 13px; color: var(--text); line-height: 1.5; cursor: pointer;
  display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden;
  word-break: break-all;
}
.ci-text:hover { color: var(--primary); }
.ci-meta { display: flex; gap: 14px; margin-top: 4px; font-size: 11px; color: var(--text-muted); }
.ci-ops { flex-shrink: 0; display: flex; gap: 4px; }
</style>
