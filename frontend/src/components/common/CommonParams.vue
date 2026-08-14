<script setup>
import { ref, computed } from 'vue'
import { store, currentProject, requestSave } from '../../store'
import KVEditor from '../test/KVEditor.vue'

const visible = defineModel('visible')

const proj = computed(() => currentProject())
</script>

<template>
  <el-dialog v-model="visible" title="公共参数（对所有接口自动附加）" width="820px" top="6vh">
    <div style="font-size:12px;color:#86909c;margin-bottom:12px;line-height:1.7">
      设置后，本项目<b>所有接口</b>发送请求时会自动带上这些 Header / Query；
      接口自身已设置的<b>同名参数优先</b>（接口覆盖公共）。常用于统一携带 <code>Authorization</code>、<code>token</code> 等。
      可配合环境变量 <code>{{变量}}</code> 使用。
    </div>

    <el-tabs>
      <el-tab-pane label="公共 Header">
        <KVEditor :items="proj.common.headers" key-placeholder="Header 名（如 Authorization）" @change="requestSave" />
      </el-tab-pane>
      <el-tab-pane label="公共 Query">
        <KVEditor :items="proj.common.query" key-placeholder="参数名" @change="requestSave" />
      </el-tab-pane>
    </el-tabs>

    <template #footer>
      <span style="float:left;font-size:12px;color:#c2c7cf">
        当前已配置：Header {{ proj.common.headers.length }} 项 · Query {{ proj.common.query.length }} 项
      </span>
      <el-button @click="visible = false">关闭</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
code { background: #f2f3f5; padding: 1px 5px; border-radius: 4px; font-family: Consolas, monospace; color: #165dff; }
</style>
