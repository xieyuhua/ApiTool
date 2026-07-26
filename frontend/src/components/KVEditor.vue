<script setup>
import { newKV } from '../store'

const props = defineProps({
  items: { type: Array, required: true },
  keyPlaceholder: { type: String, default: '参数名' },
})

function add() { props.items.push(newKV()) }
function remove(i) { props.items.splice(i, 1) }
</script>

<template>
  <div>
    <table class="field-table">
      <thead>
        <tr>
          <th style="width:36px"></th>
          <th style="width:26%">{{ keyPlaceholder }}</th>
          <th style="width:30%">值</th>
          <th>说明</th>
          <th style="width:44px"></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(kv, i) in items" :key="i">
          <td style="text-align:center"><el-checkbox v-model="kv.enabled" /></td>
          <td><el-input v-model="kv.key" :placeholder="keyPlaceholder" /></td>
          <td><el-input v-model="kv.value" placeholder="值" /></td>
          <td><el-input v-model="kv.description" placeholder="说明（写入文档）" /></td>
          <td style="text-align:center">
            <el-button link type="danger" @click="remove(i)">✕</el-button>
          </td>
        </tr>
      </tbody>
    </table>
    <el-button link type="primary" style="margin-top:6px" @click="add">+ 添加一行</el-button>
  </div>
</template>
