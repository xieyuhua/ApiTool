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
        :col-style="colStyle"
        :col-resize-fn="colResizeFn"
        @toggle="onToggleUp"
        @select-node="onSelectNodeUp"
        @select-rec="onSelectRecUp"
        @select-rec-toggle="onSelectRecToggleUp"
        @ctx-menu="onCtxMenuUp"
      />
      <div
        v-for="rec in node.records"
        :key="rec.id"
        class="traffic-item grp-item"
        :class="{ active: selected && selected.id === rec.id, checked: selectedSet.has(rec.id) }"
        :style="{ paddingLeft: (node.level + 1) * 14 + 22 + 'px' }"
        @click="onSelectRec(rec)"
        @contextmenu.prevent="$emit('ctx-menu', { rec, event: $event })">
        <el-checkbox size="small" :model-value="selectedSet.has(rec.id)" @click.stop
          @change="(v) => onSelectRecToggle(rec.id, v)" />
        <span class="proto" :class="'p-' + rec.protocol.toLowerCase()" :style="colStyle('proto')">{{ rec.protocol }}</span>
        <i class="col-resizer" @mousedown.stop="colResizeFn('proto', $event)"></i>
        <span class="method" v-if="rec.method" :class="'m-' + rec.method.toUpperCase()" :style="colStyle('method')">{{ rec.method }}</span>
        <i class="col-resizer" v-if="rec.method" @mousedown.stop="colResizeFn('method', $event)"></i>
        <span class="url" :title="rec.url" :style="colStyle('url')">{{ rec.url }}</span>
        <i class="col-resizer" @mousedown.stop="colResizeFn('url', $event)"></i>
        <span class="status" v-if="rec.statusCode" :class="'s-' + String(rec.statusCode)[0]" :style="colStyle('status')">{{ rec.statusCode }}</span>
        <i class="col-resizer" v-if="rec.statusCode" @mousedown.stop="colResizeFn('status', $event)"></i>
        <span class="ctype" v-if="rec.respContentType" :title="rec.respContentType" :style="colStyle('ctype')">{{ rec.respContentType }}</span>
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
  // 列宽样式函数（由父组件传入，保证平铺/分组共用同一套宽度与拖拽状态）
  colStyle: { type: Function, default: () => ({}) },
  colResizeFn: { type: Function, default: () => {} },
})
const emit = defineEmits(['toggle', 'select-node', 'select-rec', 'select-rec-toggle', 'ctx-menu'])

// 节点下请求总数（含所有层级的子目录）
const nodeCount = computed(() => countRecords(props.node))
function countRecords(node) {
  let n = node.records ? node.records.length : 0
  if (node.children) for (const c of node.children) n += countRecords(c)
  return n
}

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
function onCtxMenuUp(payload) { emit('ctx-menu', payload) }
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

/* 叶子请求行：年轻风格彩色徽章（与平铺列表一致） */
.traffic-item.grp-item { display: flex; gap: 8px; align-items: center; padding: 7px 12px; border-bottom: 1px solid #f2f3f5; cursor: pointer; font-size: 13px; border-radius: 8px; transition: background .12s ease; width: max-content; min-width: 100%; }
.traffic-item.grp-item:hover { background: #f2f7ff; }
.traffic-item.grp-item.active { background: #e8f3ff; box-shadow: inset 3px 0 0 #165dff; }
.traffic-item.grp-item.checked { background: #f0f6ff; }
.traffic-item.grp-item > .el-checkbox { flex-shrink: 0; }
.traffic-item.grp-item .col-resizer { flex-shrink: 0; align-self: stretch; width: 7px; margin: 0 -3px; cursor: col-resize; z-index: 3; border-radius: 3px; }
.traffic-item.grp-item .col-resizer:hover { background: rgba(22, 93, 255, .25); }
.traffic-item.grp-item .proto, .traffic-item.grp-item .method, .traffic-item.grp-item .status {
  font-size: 11px; font-weight: 700; padding: 2px 7px; border-radius: 6px; flex-shrink: 0; letter-spacing: .2px;
}
.traffic-item.grp-item .proto { color: #fff; background: #86909c; }
.traffic-item.grp-item .p-https { background: #165dff; }
.traffic-item.grp-item .p-http { background: #00b42a; }
.traffic-item.grp-item .p-websocket { background: #722ed1; }
.traffic-item.grp-item .p-sse { background: #f76707; }
.traffic-item.grp-item .p-tls { background: #ff7d00; }
.traffic-item.grp-item .p-ssh, .traffic-item.grp-item .p-ftp, .traffic-item.grp-item .p-smtp { background: #eb0aa6; }
.traffic-item.grp-item .method { color: #165dff; background: #e8f3ff; }
.traffic-item.grp-item .m-GET { color: #00b42a; background: #e8ffea; }
.traffic-item.grp-item .m-POST { color: #165dff; background: #e8f3ff; }
.traffic-item.grp-item .m-PUT { color: #ff7d00; background: #fff3e8; }
.traffic-item.grp-item .m-DELETE { color: #f53f3f; background: #ffece8; }
.traffic-item.grp-item .m-PATCH { color: #722ed1; background: #f3edff; }
.traffic-item.grp-item .m-HEAD, .traffic-item.grp-item .m-OPTIONS { color: #86909c; background: #f2f3f5; }
.traffic-item.grp-item .status { color: #86909c; background: #f2f3f5; }
.traffic-item.grp-item .s-2 { color: #00b42a; background: #e8ffea; }
.traffic-item.grp-item .s-3 { color: #165dff; background: #e8f3ff; }
.traffic-item.grp-item .s-4 { color: #ff7d00; background: #fff3e8; }
.traffic-item.grp-item .s-5 { color: #f53f3f; background: #ffece8; }
.traffic-item.grp-item .url { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: #1d2129; }
.traffic-item.grp-item .ctype {
  flex-shrink: 0; max-width: 180px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  font-size: 11px; color: #4e5969; background: #f2f3f5; padding: 2px 7px; border-radius: 6px;
}
</style>
