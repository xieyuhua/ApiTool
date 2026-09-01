<template>
  <div class="tool-card" :class="{ err: step.error, open: expanded }">
    <div class="tc-head" @click="toggle" :title="hasDetail ? '点击查看执行参数' : ''">
      <span class="tc-ic">{{ icon }}</span>
      <span class="tc-name">{{ step.name }}</span>
      <span v-if="step.server && step.server !== 'builtin'" class="tc-server">@{{ step.server }}</span>
      <span v-else-if="step.server === 'builtin'" class="tc-server builtin">内置</span>
      <span v-if="step.error" class="tc-badge err">失败</span>
      <span v-else class="tc-badge ok">成功</span>
      <span v-if="hasDetail" class="tc-toggle">{{ expanded ? '▾' : '▸' }}</span>
    </div>

    <!-- 展开详情：执行参数与返回结果 -->
    <div v-if="expanded && hasDetail" class="tc-detail">
      <div v-if="step.input" class="tc-sec">
        <div class="tc-sec-title">📥 执行参数</div>
        <pre class="tc-pre">{{ step.input }}</pre>
      </div>
      <div v-if="step.output || step.error" class="tc-sec">
        <div class="tc-sec-title" :class="{ err: step.error }">
          {{ step.error ? '⚠️ 错误信息' : '📤 返回结果' }}
        </div>
        <pre class="tc-pre" :class="{ err: step.error }">{{ step.error || step.output }}</pre>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'

const props = defineProps({ step: { type: Object, required: true } })

const iconMap = { tool: '🔧', skill: '✨', thought: '💭', plan: '📋' }
const icon = iconMap[props.step.type] || '🔧'

const expanded = ref(false)
const hasDetail = computed(() => !!(props.step.input || props.step.output || props.step.error))

function toggle() {
  if (hasDetail.value) expanded.value = !expanded.value
}
</script>

<style scoped>
.tool-card {
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface-2);
  padding: 3px 10px;
  margin: 3px 4px 3px 0;
  font-size: 12px;
  line-height: 1.8;
  max-width: 100%;
}
.tool-card.err { border-color: var(--el-color-danger); background: var(--el-color-danger-light-9); }
.tool-card.open { background: var(--surface); }
.tc-head { display: flex; align-items: center; gap: 6px; flex-wrap: nowrap; cursor: default; }
.tc-head[title] { cursor: pointer; }
.tc-ic { font-size: 13px; }
.tc-name { font-weight: 600; color: var(--text); }
.tc-server { font-size: 11px; color: var(--text-muted); background: var(--surface); padding: 0 6px; border-radius: 6px; }
.tc-server.builtin { color: var(--primary); border: 1px solid var(--primary); }
.tc-badge { font-size: 11px; padding: 0 8px; border-radius: 10px; }
.tc-badge.ok { color: var(--el-color-success); background: var(--el-color-success-light-9); }
.tc-badge.err { color: var(--el-color-danger); background: var(--el-color-danger-light-9); }
.tc-toggle { color: var(--text-muted); margin-left: 2px; font-size: 11px; }

.tc-detail { margin-top: 6px; border-top: 1px dashed var(--border); padding-top: 6px; }
.tc-sec { margin-bottom: 6px; }
.tc-sec-title { font-size: 11px; color: var(--primary); margin-bottom: 2px; font-weight: 600; }
.tc-sec-title.err { color: var(--el-color-danger); }
.tc-pre {
  white-space: pre-wrap; word-break: break-all; max-height: 260px; overflow: auto;
  background: var(--surface-2); padding: 6px 8px; border-radius: 6px; font-size: 12px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace; margin: 0; color: var(--text);
}
.tc-pre.err { color: var(--el-color-danger); }
</style>
