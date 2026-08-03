<script setup>
import { ref } from 'vue'
import PluginManager from './PluginManager.vue'
import Toolbox from './Toolbox.vue'
import ToolClipboard from '../clipboard/ToolClipboard.vue'

const PLUGIN_KEYS = ['ssh', 'ftp', 'sftp']

const groups = [
  {
    title: '效率工具',
    items: [
      { key: 'clipboard', label: '剪贴板记录', ico: '📋' },
    ],
  },
  {
    title: '连接管理',
    items: [
      { key: 'ssh', label: 'XShell (SSH)', ico: '💻' },
      { key: 'ftp', label: 'FTP', ico: '📁' },
      { key: 'sftp', label: 'SFTP', ico: '📂' },
    ],
  },
  {
    title: '开发工具',
    items: [
      { key: 'json', label: 'JSON 格式化', ico: '🔤' },
      { key: 'sql', label: 'SQL 格式化', ico: '📝' },
      { key: 'crypto', label: '加密 / 解密', ico: '🔐' },
      { key: 'timestamp', label: '时间戳转换', ico: '⏱️' },
      { key: 'serialize', label: 'PHP 序列化', ico: '🔗' },
      { key: 'namer', label: 'AI 命名 / 取名', ico: '🏷️' },
    ],
  },
]

const activeKey = ref('clipboard')

function isPlugin(k) {
  return PLUGIN_KEYS.includes(k)
}
</script>

<template>
  <div class="tools">
    <div class="tools-nav">
      <div v-for="g in groups" :key="g.title" class="tn-group">
        <div class="tn-group-title">{{ g.title }}</div>
        <div v-for="it in g.items" :key="it.key"
             class="tn-item" :class="{ active: activeKey === it.key }"
             @click="activeKey = it.key">
          <span class="tn-ico">{{ it.ico }}</span>
          <span class="tn-label">{{ it.label }}</span>
        </div>
      </div>
    </div>
    <div class="tools-body">
      <ToolClipboard v-if="activeKey === 'clipboard'" />
      <PluginManager v-else-if="isPlugin(activeKey)" :category="activeKey" />
      <Toolbox v-else :tool="activeKey" />
    </div>
  </div>
</template>

<style scoped>
.tools { flex: 1; min-width: 0; display: flex; height: 100%; box-sizing: border-box; overflow: hidden; }
.tools-nav {
  width: 188px; flex-shrink: 0; background: var(--surface-2); border-right: 1px solid var(--border);
  padding: 12px 10px; overflow: auto;
}
.tn-group { margin-bottom: 14px; }
.tn-group-title {
  font-size: 12px; color: var(--text-muted); padding: 4px 10px; margin-bottom: 4px; font-weight: 600;
  letter-spacing: .5px;
}
.tn-item {
  display: flex; align-items: center; gap: 8px; padding: 9px 10px; border-radius: 6px;
  cursor: pointer; font-size: 13px; color: var(--text); margin-bottom: 2px; transition: all .15s;
}
.tn-item:hover { background: var(--surface-2); color: var(--text); }
.tn-item.active { background: var(--primary-soft); color: var(--primary); font-weight: 600; }
.tn-ico { font-size: 15px; line-height: 1; }
.tn-label { white-space: nowrap; }

.tools-body { flex: 1; min-width: 0; height: 100%; }
</style>
