<template>
  <div class="tool-card" :class="{ err: step.error }">
    <div class="tc-head">
      <span class="tc-ic">{{ icon }}</span>
      <span class="tc-name">{{ step.name }}</span>
      <span v-if="step.server && step.server !== 'builtin'" class="tc-server">@{{ step.server }}</span>
      <span v-else-if="step.server === 'builtin'" class="tc-server builtin">内置</span>
      <span v-if="step.error" class="tc-badge err">失败</span>
      <span v-else class="tc-badge ok">成功</span>
    </div>
  </div>
</template>

<script setup>
const props = defineProps({ step: { type: Object, required: true } })

const iconMap = { tool: '🔧', skill: '✨', thought: '💭', plan: '📋' }
const icon = iconMap[props.step.type] || '🔧'
</script>

<style scoped>
.tool-card {
  display: inline-flex;
  align-items: center;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--surface-2);
  padding: 2px 10px;
  margin: 3px 4px 3px 0;
  font-size: 12px;
  line-height: 1.8;
}
.tool-card.err { border-color: var(--el-color-danger); background: var(--el-color-danger-light-9); }
.tc-head { display: flex; align-items: center; gap: 6px; flex-wrap: nowrap; }
.tc-ic { font-size: 13px; }
.tc-name { font-weight: 600; color: var(--text); }
.tc-server { font-size: 11px; color: var(--text-muted); background: var(--surface); padding: 0 6px; border-radius: 6px; }
.tc-server.builtin { color: var(--primary); border: 1px solid var(--primary); }
.tc-badge { font-size: 11px; padding: 0 8px; border-radius: 10px; }
.tc-badge.ok { color: var(--el-color-success); background: var(--el-color-success-light-9); }
.tc-badge.err { color: var(--el-color-danger); background: var(--el-color-danger-light-9); }
</style>
