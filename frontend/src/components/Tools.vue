<script setup>
import { ref } from 'vue'
import PluginManager from './PluginManager.vue'
import Toolbox from './Toolbox.vue'

const PLUGIN_KEYS = ['ssh', 'ftp', 'sftp']

const groups = [
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
    ],
  },
]

const activeKey = ref('db')

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
      <PluginManager v-if="isPlugin(activeKey)" :category="activeKey" />
      <Toolbox v-else :tool="activeKey" />
    </div>
  </div>
</template>

<style scoped>
.tools { flex: 1; min-width: 0; display: flex; height: 100%; box-sizing: border-box; overflow: hidden; }
.tools-nav {
  width: 188px; flex-shrink: 0; background: #f7f8fa; border-right: 1px solid #e5e6eb;
  padding: 12px 10px; overflow: auto;
}
.tn-group { margin-bottom: 14px; }
.tn-group-title {
  font-size: 12px; color: #86909c; padding: 4px 10px; margin-bottom: 4px; font-weight: 600;
  letter-spacing: .5px;
}
.tn-item {
  display: flex; align-items: center; gap: 8px; padding: 9px 10px; border-radius: 6px;
  cursor: pointer; font-size: 13px; color: #4e5969; margin-bottom: 2px; transition: all .15s;
}
.tn-item:hover { background: #eef0f3; color: #1d2129; }
.tn-item.active { background: #e8f0ff; color: #165dff; font-weight: 600; }
.tn-ico { font-size: 15px; line-height: 1; }
.tn-label { white-space: nowrap; }

.tools-body { flex: 1; min-width: 0; height: 100%; }
</style>
