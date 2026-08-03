<script setup>
import { computed } from 'vue'
import { newField } from '../../store'

const props = defineProps({
  fields: { type: Array, required: true },
})
const emit = defineEmits(['change'])
function changed() { emit('change') }

const types = ['string', 'integer', 'number', 'boolean', 'object',
  'array[object]', 'array[string]', 'array[integer]', 'array[number]', 'null']

// 扁平化渲染（带深度与父级引用）
const rows = computed(() => {
  const out = []
  const walk = (list, depth) => {
    list.forEach((f, i) => {
      out.push({ f, depth, parent: list, index: i })
      if (f.children && f.children.length) walk(f.children, depth + 1)
    })
  }
  walk(props.fields, 0)
  return out
})

function canHaveChildren(f) {
  return f.type === 'object' || f.type.startsWith('array')
}
function addChild(f) {
  f.children ||= []
  f.children.push(newField())
  changed()
}
function removeRow(row) {
  row.parent.splice(row.index, 1)
  changed()
}
function addRoot() {
  props.fields.push(newField())
  changed()
}
function prefix(depth) {
  return depth === 0 ? '' : '  '.repeat(depth - 1) + '└ '
}
</script>

<template>
  <div>
    <table class="field-table">
      <thead>
        <tr>
          <th style="width:24%">字段名</th>
          <th style="width:130px">类型</th>
          <th style="width:56px">必填</th>
          <th>说明（写入文档，可 AI 补全）</th>
          <th style="width:18%">示例值</th>
          <th style="width:76px">操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(row, i) in rows" :key="i">
          <td>
            <div class="fname-cell">
              <span class="tree-prefix">{{ prefix(row.depth) }}</span>
              <el-input v-model="row.f.name" placeholder="字段名" @input="changed" />
            </div>
          </td>
          <td>
            <el-select v-model="row.f.type" size="small" @change="changed">
              <el-option v-for="t in types" :key="t" :label="t" :value="t" />
            </el-select>
          </td>
          <td style="text-align:center"><el-checkbox v-model="row.f.required" @change="changed" /></td>
          <td><el-input v-model="row.f.description" placeholder="字段描述" @input="changed" /></td>
          <td><el-input v-model="row.f.example" placeholder="示例" @input="changed" /></td>
          <td style="text-align:center; white-space:nowrap">
            <el-button v-if="canHaveChildren(row.f)" link type="primary" title="添加子字段"
              @click="addChild(row.f)">＋子</el-button>
            <el-button link type="danger" title="删除" @click="removeRow(row)">✕</el-button>
          </td>
        </tr>
        <tr v-if="!rows.length">
          <td colspan="6" style="text-align:center; color:#c9cdd4; padding:16px">
            暂无字段，可从 JSON 导入、从响应导入，或手动添加
          </td>
        </tr>
      </tbody>
    </table>
    <el-button link type="primary" style="margin-top:6px" @click="addRoot">+ 添加字段</el-button>
  </div>
</template>
