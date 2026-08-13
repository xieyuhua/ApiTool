<template>
  <div>
    <!-- host / 目录节点行 -->
    <div v-if="node.type !== 'leaf'" class="grp-node"
      :class="{ 'is-host': node.type === 'host' }"
      :style="{ paddingLeft: node.level * 14 + 'px' }">
      <span class="grp-arrow" :class="{ open: expanded }" @click="onToggle">▶</span>
      <span class="grp-name" @click="onToggle"
        :title="node.type === 'host' ? node.host : node.name">
        {{ node.type === 'host' ? node.host : node.name }}
      </span>
      <span class="grp-count">{{ nodeCount }}</span>
      <span class="grp-check" @click.stop>
        <el-checkbox size="small" :model-value="node.allChecked" :indeterminate="node.someChecked"
          @change="onSelectNode" />
      </span>
    </div>

    <!-- 展开时渲染子节点：目录递归 / 请求叶子 -->
    <template v-if="expanded">
      <GroupTreeNode
        v-for="child in node.children"
        :key="child.key"
        :node="child"
        :expanded="expandedSet.has(child.key)"
        :expanded-set="expandedSet"
        :selected="selected"
        :selected-set="selectedSet"
        @toggle="onToggleUp"
        @select-node="onSelectNodeUp"
        @select-rec="onSelectRecUp"
        @select-rec-toggle="onSelectRecToggleUp"
      />
      <div
        v-for="rec in node.records"
        :key="rec.id"
        class="traffic-item grp-item"
        :class="{ active: selected && selected.id === rec.id, checked: selectedSet.has(rec.id) }"
        :style="{ paddingLeft: (node.level + 1) * 14 + 22 + 'px' }"
        @click="onSelectRec(rec)">
        <el-checkbox size="small" :model-value="selectedSet.has(rec.id)" @click.stop
          @change="(v) => onSelectRecToggle(rec.id, v)" />
        <span class="proto" :class="'p-' + rec.protocol.toLowerCase()">{{ rec.protocol }}</span>
        <span class="method" v-if="rec.method">{{ rec.method }}</span>
        <span class="url" :title="rec.url">{{ rec.url }}</span>
        <span class="status" v-if="rec.statusCode">{{ rec.statusCode }}</span>
      </div>
    </template>
  </div>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  node: { type: Object, required: true },
  expanded: { type: Boolean, default: false },
  expandedSet: { type: Set, default: () => new Set() },
  selected: { type: Object, default: null },
  selectedSet: { type: Set, default: () => new Set() },
})
const emit = defineEmits(['toggle', 'select-node', 'select-rec', 'select-rec-toggle'])

// 节点下请求总数（含子目录）
const nodeCount = computed(() => {
  let n = props.node.records.length
  for (const c of props.node.children) n += c.records.length
  return n
})

function onToggle() { emit('toggle', props.node.key) }
// 子节点 toggle 直接向上冒泡
function onToggleUp(key) { emit('toggle', key) }

// host/目录勾选：把节点与勾选值打包为 { node, val }，便于逐层冒泡
function onSelectNode(val) { emit('select-node', { node: props.node, val }) }
function onSelectNodeUp(payload) { emit('select-node', payload) }

// 选中请求：直接冒泡记录
function onSelectRec(rec) { emit('select-rec', rec) }
function onSelectRecUp(rec) { emit('select-rec', rec) }

// 勾选请求：打包 { id, val }
function onSelectRecToggle(id, val) { emit('select-rec-toggle', { id, val }) }
function onSelectRecToggleUp(payload) { emit('select-rec-toggle', payload) }
</script>

<script>
export default {
  name: 'GroupTreeNode',
}
</script>

<style scoped>
.grp-node { display: flex; align-items: center; gap: 6px; padding: 5px 8px; cursor: pointer; font-size: 13px; }
.grp-node:hover { background: #f2f3f5; }
.grp-node.is-host { background: #f2f3f5; font-weight: 600; border-radius: 4px; margin-top: 2px; }
.grp-node.is-host:hover { background: #e8eaed; }
.grp-arrow { display: inline-block; font-size: 10px; color: #86909c; transition: transform .15s; width: 10px; }
.grp-arrow.open { transform: rotate(90deg); }
.grp-name { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.grp-count { color: #86909c; font-size: 12px; }
.grp-check { display: flex; align-items: center; }
</style>
