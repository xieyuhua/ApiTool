<script setup>
import { newKV } from '../../store'

const props = defineProps({
  items: { type: Array, required: true },
  keyPlaceholder: { type: String, default: '参数名' },
})

const emit = defineEmits(['change'])

function add() { props.items.push(newKV()); emit('change') }
function remove(i) { props.items.splice(i, 1); emit('change') }
function onEdit() { emit('change') }
</script>

<template>
  <div class="kv">
    <div class="kv-head">
      <span class="c-enable"></span>
      <span class="c-key">{{ keyPlaceholder }}</span>
      <span class="c-val">值</span>
      <span class="c-desc">说明</span>
      <span class="c-op"></span>
    </div>
    <div v-for="(kv, i) in items" :key="i" class="kv-row">
      <span class="c-enable"><el-checkbox v-model="kv.enabled" @change="onEdit" /></span>
      <span class="c-key"><el-input v-model="kv.key" :placeholder="keyPlaceholder" @input="onEdit" /></span>
      <span class="c-val"><el-input v-model="kv.value" placeholder="值" @input="onEdit" /></span>
      <span class="c-desc"><el-input v-model="kv.description" placeholder="说明（写入文档）" @input="onEdit" /></span>
      <span class="c-op">
        <el-button link type="danger" title="删除" @click="remove(i)">✕</el-button>
      </span>
    </div>
    <div v-if="!items.length" class="kv-empty">暂无数据，点击下方「添加一行」</div>
  </div>
  <el-button link type="primary" style="margin-top:10px" @click="add">＋ 添加一行</el-button>
</template>

<style scoped>
.kv { display: flex; flex-direction: column; gap: 10px; min-width: 0; }
.kv-head, .kv-row {
  display: grid;
  grid-template-columns: 42px minmax(150px, 1fr) minmax(170px, 1.2fr) minmax(150px, 1fr) 44px;
  gap: 12px;
  align-items: center;
}
.kv-head { font-size: 12px; color: #86909c; padding: 0 2px; }
.c-enable { display: flex; justify-content: center; }
.kv-row .c-key, .kv-row .c-val, .kv-row .c-desc { min-width: 0; }
.kv-empty { color: #c9cdd4; font-size: 13px; text-align: center; padding: 16px 0; border: 1px dashed #e5e6eb; border-radius: 8px; }
</style>
