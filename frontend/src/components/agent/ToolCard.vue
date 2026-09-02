<template>
  <div class="tool-card" :class="{ err: step.error || step.type === 'tool-failed', open: expanded, running }">
    <div class="tc-head" @click="toggle" :title="canToggle ? '点击查看执行参数' : ''">
      <span class="tc-ic">{{ icon }}</span>
      <span class="tc-name">{{ step.name }}</span>
      <span v-if="step.server && step.server !== 'builtin'" class="tc-server">@{{ step.server }}</span>
      <span v-else-if="step.server === 'builtin'" class="tc-server builtin">内置</span>
      <span v-if="step.error || step.type === 'tool-failed'" class="tc-badge err">失败</span>
      <span v-else-if="step.type === 'skill' && step.name === '技能匹配'" class="tc-badge skill-none">未匹配</span>
      <span v-else-if="step.type === 'skill' && step.output" class="tc-badge skill">已运用</span>
      <span v-else-if="running" class="tc-badge run">运行中</span>
      <span v-else class="tc-badge ok">成功</span>
      <span v-if="running" class="tc-spin"></span>
      <span v-if="canToggle" class="tc-toggle">{{ expanded ? '▾' : '▸' }}</span>
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
        <pre class="tc-pre" :class="{ err: step.error || step.type === 'tool-failed' }">{{ step.error || step.output }}</pre>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch } from 'vue'

const props = defineProps({ step: { type: Object, required: true } })

const iconMap = { tool: '🔧', skill: '✨', thought: '💭', plan: '📋', 'tool-failed': '⚠️' }
const icon = iconMap[props.step.type] || '🔧'

// 由 step 自身推断「是否运行中」：已有入参、但还没有结果/错误 → 仍在进行中。
// 注意：skill 类型不是实时工具调用（是 system prompt 注入），不应视为「运行中」，始终可展开查看。
const running = computed(() => {
  const s = props.step
  if (s.type === 'skill') return false
  return !!(s.input || s.type === 'tool') &&
    !(s.output || s.error || s.type === 'tool-failed')
})
// 有内容才允许展开/折叠
const hasDetail = computed(() => !!(props.step.input || props.step.output || props.step.error))
const canToggle = computed(() => hasDetail.value && !running.value)

// 运行中自动展开；一旦结束（running 变 true → false）自动折叠
const expanded = ref(running.value)
watch(running, (v) => { expanded.value = v }, { immediate: true })

function toggle() {
  // 运行中不允许折叠，仅手动（运行结束）可切换
  if (!running.value && hasDetail.value) expanded.value = !expanded.value
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
/* 展开详情或运行中的工具单独占一整行，避免与其他工具并排 */
.tool-card.open, .tool-card.running { width: 100%; flex-basis: 100%; margin-right: 0; }
.tool-card.running { border-color: var(--primary); background: var(--el-color-primary-light-9); }
.tc-head { display: flex; align-items: center; gap: 6px; flex-wrap: nowrap; cursor: default; }
.tc-head[title] { cursor: pointer; }
.tc-ic { font-size: 13px; }
.tc-name { font-weight: 600; color: var(--text); }
.tc-server { font-size: 11px; color: var(--text-muted); background: var(--surface); padding: 0 6px; border-radius: 6px; }
.tc-server.builtin { color: var(--primary); border: 1px solid var(--primary); }
.tc-badge { font-size: 11px; padding: 0 8px; border-radius: 10px; }
.tc-badge.ok { color: var(--el-color-success); background: var(--el-color-success-light-9); }
.tc-badge.err { color: var(--el-color-danger); background: var(--el-color-danger-light-9); }
.tc-badge.run { color: var(--primary); background: var(--el-color-primary-light-9); }
.tc-badge.skill { color: #8e44ad; background: #f5eef8; border: 1px solid #8e44ad; }
.tc-badge.skill-none { color: #8a8a8a; background: #f0f0f0; border: 1px solid #cfcfcf; }
.tc-toggle { color: var(--text-muted); margin-left: 2px; font-size: 11px; }
/* 运行中旋转的加载小圈 */
.tc-spin { width: 11px; height: 11px; margin-left: 2px; border: 2px solid var(--primary); border-top-color: transparent; border-radius: 50%; animation: tc-spin 0.8s linear infinite; }
@keyframes tc-spin { to { transform: rotate(360deg); } }

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
