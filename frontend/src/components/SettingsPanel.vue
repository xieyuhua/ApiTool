<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { GetDataFilePath, StartSyncServer, StopSyncServer, SyncServerRunning } from '../../wailsjs/go/main/App'
import { store, saveNow } from '../store'
import CloudSync from './CloudSync.vue'

const dataPath = ref('')
const cloudVisible = ref(false)
const syncAddr = ref(':8080')
const syncRunning = ref(false)
const syncBusy = ref(false)

onMounted(async () => {
  try { dataPath.value = await GetDataFilePath() } catch { /* ignore */ }
  try { syncRunning.value = await SyncServerRunning() } catch { /* ignore */ }
})

async function save() {
  await saveNow()
  ElMessage.success('设置已保存')
}

async function toggleSync() {
  syncBusy.value = true
  try {
    if (syncRunning.value) {
      await StopSyncServer()
      syncRunning.value = false
      ElMessage.success('已停止内置同步服务')
    } else {
      const msg = await StartSyncServer(syncAddr.value)
      syncRunning.value = true
      ElMessage.success(msg)
    }
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    syncBusy.value = false
  }
}
</script>

<template>
  <div class="main-area">
    <div class="panel-page" style="max-width:760px; margin:0 auto; width:100%">
      <h2 style="margin:6px 0 16px">⚙ 设置</h2>

      <div class="card">
        <div class="card-title">AI 配置（用于自动生成字段描述）</div>
        <el-form label-width="110px" label-position="left">
          <el-form-item label="接口地址">
            <el-input v-model="store.data.settings.aiBaseUrl"
              placeholder="OpenAI 兼容接口，如 https://api.openai.com/v1 或 https://api.deepseek.com/v1" />
          </el-form-item>
          <el-form-item label="API Key">
            <el-input v-model="store.data.settings.aiKey" type="password" show-password placeholder="sk-..." />
          </el-form-item>
          <el-form-item label="模型">
            <el-input v-model="store.data.settings.aiModel" placeholder="如 gpt-4o-mini / deepseek-chat" />
          </el-form-item>
        </el-form>
        <div style="color:#86909c; font-size:12px">
          支持任意 OpenAI 兼容服务（OpenAI / DeepSeek / 通义千问 / 本地 Ollama 等）。
        </div>
      </div>

      <div class="card">
        <div class="card-title">请求设置</div>
        <el-form label-width="110px" label-position="left">
          <el-form-item label="超时时间(秒)">
            <el-input-number v-model="store.data.settings.timeoutSec" :min="1" :max="600" />
          </el-form-item>
        </el-form>
      </div>

      <div class="card">
        <div class="card-title">云同步（多端共享接口文档）</div>
        <div style="font-size:13px; color:#4e5969; margin-bottom:12px">
          登录云账号后，可将项目推送到云端、从云端拉取到其他设备，实现多人/多端共享。
        </div>
        <el-button type="primary" @click="cloudVisible = true">☁ 云同步 / 多端共享</el-button>
      </div>

      <div class="card">
        <div class="card-title">内置同步服务（工具与服务一起部署，可选）</div>
        <div style="font-size:13px; color:#4e5969; margin-bottom:12px">
          启动后本工具同时作为同步服务器，其他人可将云服务器地址填为
          <code>http://你的IP{{ syncAddr }}</code> 来连接。停止后仅本机使用。
        </div>
        <div style="display:flex; gap:12px; align-items:center">
          <el-input v-model="syncAddr" placeholder=":8080" style="width:200px" />
          <el-button :type="syncRunning ? 'danger' : 'success'" :loading="syncBusy" @click="toggleSync">
            {{ syncRunning ? '停止服务' : '启动服务' }}
          </el-button>
          <el-tag v-if="syncRunning" type="success" effect="plain">运行中</el-tag>
        </div>
      </div>

      <div class="card">
        <div class="card-title">数据存储</div>
        <div style="font-size:13px; color:#4e5969">
          所有接口信息（含最近一次请求与响应）自动保存在本地：<br>
          <code style="font-size:12px; color:#165dff">{{ dataPath }}</code>
        </div>
      </div>

      <el-button type="primary" @click="save">保存设置</el-button>
    </div>

    <CloudSync v-model:visible="cloudVisible" />
  </div>
</template>

<style scoped>
code { background: #f2f3f5; padding: 1px 5px; border-radius: 4px; }
</style>
