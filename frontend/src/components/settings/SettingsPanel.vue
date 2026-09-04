<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { GetDataFilePath, StartSyncServer, StopSyncServer, SyncServerRunning, SyncServerURL, OpenInBrowser } from '../../../wailsjs/go/main/App'
import { store, saveNow, scheduleAutoSync, checkUpdate, setTheme, setAccent, setClipboardMonitor, setCodeFont, setCodeFontSize, THEMES, SCHEMES, setScheme } from '../../store'
import CloudSync from './CloudSync.vue'

const dataPath = ref('')
const cloudVisible = ref(false)
const syncAddr = ref(':8080')
const syncRunning = ref(false)
const syncBusy = ref(false)
const syncURL = ref('') // 实际可访问地址（http://IP:port），与启动地址区分

// 版本与升级检测状态
const checking = ref(false)
const updateResult = ref(null)

onMounted(async () => {
  try { dataPath.value = await GetDataFilePath() } catch { /* ignore */ }
  try {
    syncRunning.value = await SyncServerRunning()
    if (syncRunning.value) syncURL.value = await SyncServerURL()
  } catch { /* ignore */ }
})

function openURL(url) {
  if (!url) return
  try { OpenInBrowser(url) } catch { window.open(url, '_blank') }
}

async function doCheckUpdate() {
  checking.value = true
  updateResult.value = null
  try {
    const r = await checkUpdate()
    updateResult.value = r
    if (r.error) {
      ElMessage.warning('检测失败：' + r.error)
    } else if (r.hasNew) {
      ElMessage.success('发现新版本 ' + r.latest)
    } else {
      ElMessage.info('已是最新版本（' + (r.latest || store.appVersion) + '）')
    }
  } catch (e) {
    updateResult.value = { error: String(e) }
    ElMessage.error('检测异常：' + String(e))
  } finally {
    checking.value = false
  }
}

async function save() {
  await saveNow()
  ElMessage.success('设置已保存')
}

async function onAutoSyncChange(val) {
  await saveNow()
  if (val) {
    scheduleAutoSync()
    ElMessage.success('已开启自动同步，正在备份到云端')
  } else {
    ElMessage.info('已关闭自动同步（仅手动推送/拉取）')
  }
}

async function toggleSync() {
  syncBusy.value = true
  try {
    if (syncRunning.value) {
      await StopSyncServer()
      syncRunning.value = false
      syncURL.value = ''
      ElMessage.success('已停止内置同步服务')
    } else {
      const addr = await StartSyncServer(syncAddr.value)
      syncRunning.value = true
      syncURL.value = await SyncServerURL()
      ElMessage.success('内置同步服务已启动：' + syncURL.value)
      void addr
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
        <div class="card-title">版本与升级</div>
        <div style="font-size:13px;color:#4e5969;margin-bottom:12px">
          当前版本：<b>{{ store.appVersion || store.data.settings.version }}</b>
        </div>
        <el-form label-width="92px" label-position="left" style="margin-bottom:12px">
          <el-form-item label="升级地址">
            <el-input v-model="store.data.settings.updateURL" placeholder="http://127.0.0.1:8080" style="max-width:360px" />
            <span style="font-size:12px;color:#86909c;margin-left:10px">
              服务端需提供 <code>/version</code> 接口返回 {"version":"x.y.z","url":"下载地址","notes":"说明"}
            </span>
          </el-form-item>
        </el-form>
        <el-button type="primary" :loading="checking" @click="doCheckUpdate">
          {{ checking ? '检测中…' : '检测更新' }}
        </el-button>
        <div v-if="updateResult" style="margin-top:12px">
          <el-alert
            v-if="updateResult.error"
            type="warning" :closable="false"
            :title="'检测失败：' + updateResult.error" show-icon />
          <el-alert
            v-else-if="updateResult.hasNew"
            type="success" :closable="false"
            :title="'发现新版本 ' + updateResult.latest"
            :description="(updateResult.notes || '') + (updateResult.url ? '\n下载地址：' + updateResult.url : '')"
            show-icon>
            <template #default>
              <div>
                <div>{{ updateResult.notes || '点击下载安装新版本' }}</div>
                <el-button v-if="updateResult.url" type="primary" size="small" style="margin-top:8px"
                  @click="store.data.settings.updateURL && openURL(updateResult.url)">前往下载</el-button>
              </div>
            </template>
          </el-alert>
          <el-alert
            v-else
            type="info" :closable="false"
            :title="'已是最新版本（' + (updateResult.latest || store.appVersion) + '）'" show-icon />
        </div>
      </div>

      <div class="card">
        <div class="card-title">云同步（多端共享接口文档）</div>
        <div style="font-size:13px; color:#4e5969; margin-bottom:12px">
          登录云账号后，可将项目推送到云端、从云端拉取到其他设备，实现多人/多端共享。
        </div>
        <el-form label-width="120px" label-position="left" style="margin-bottom:12px">
          <el-form-item label="自动同步云端">
            <el-switch v-model="store.data.settings.autoSync" @change="onAutoSyncChange" />
            <span style="font-size:12px;color:#86909c;margin-left:10px">
              开启后：编辑自动推送到云端（防止本地丢失）；启动时自动拉取云端更新
            </span>
          </el-form-item>
        </el-form>
        <el-button type="primary" @click="cloudVisible = true">☁ 云同步 / 多端共享</el-button>
      </div>

      <div class="card">
        <div class="card-title">内置同步服务（工具与服务一起部署，可选）</div>
        <div style="font-size:13px; color:#4e5969; margin-bottom:12px">
          启动后本工具同时作为同步服务器。下面填写的是<b>监听地址</b>（绑定用，如
          <code>:8080</code> / <code>0.0.0.0:8080</code>，写 <code>:0</code> 让系统分配随机端口）；
          启动后下方会显示<b>实际访问地址</b>，其他人把该地址填到「云同步」的云服务器地址即可连接。
        </div>
        <div style="display:flex; gap:12px; align-items:center; flex-wrap:wrap">
          <el-input v-model="syncAddr" placeholder=":8080" style="width:220px"
            title="监听地址（绑定用，非访问地址）" />
          <el-button :type="syncRunning ? 'danger' : 'success'" :loading="syncBusy" @click="toggleSync">
            {{ syncRunning ? '停止服务' : '启动服务' }}
          </el-button>
          <el-tag v-if="syncRunning" type="success" effect="plain">运行中</el-tag>
        </div>
        <div v-if="syncRunning && syncURL" class="sync-url">
          <span class="su-label">实际访问地址（可分享给他人）：</span>
          <code class="su-code">{{ syncURL }}</code>
          <el-button size="small" link type="primary" @click="openURL(syncURL)">打开</el-button>
          <el-button size="small" link type="primary"
            @click="store.data.settings.cloudURL = syncURL; ElMessage.success('已填入云服务器地址')">填入云服务器</el-button>
        </div>
      </div>

      <div class="card">
        <div class="card-title">外观（主题）</div>
        <el-form label-width="110px" label-position="left">
          <el-form-item label="主题方案">
            <div style="font-size:12px;color:#86909c;margin-bottom:8px">
              选择「原始亮色」即回到默认浅色主题；也可点「自定义」后单独切换明暗与主题色。
            </div>
            <div class="scheme-grid">
              <div v-for="sc in SCHEMES" :key="sc.id" class="scheme-item"
                :class="{ active: store.data.settings.scheme === sc.id }" @click="setScheme(sc.id)">
                <span class="scheme-swatch" :style="{ background: sc.swatch }"></span>
                <span class="scheme-label">{{ sc.label }}</span>
                <span class="scheme-desc">{{ sc.desc }}</span>
              </div>
              <div class="scheme-item" :class="{ active: !store.data.settings.scheme }" @click="setScheme('')">
                <span class="scheme-swatch scheme-custom">自定义</span>
                <span class="scheme-label">自定义</span>
                <span class="scheme-desc">手动选择明暗与主题色</span>
              </div>
            </div>
          </el-form-item>
          <el-form-item label="明暗">
            <el-radio-group :model-value="store.data.settings.theme" @change="setTheme">
              <el-radio-button v-for="t in THEMES" :key="t.value" :value="t.value">{{ t.label }}</el-radio-button>
            </el-radio-group>
          </el-form-item>
          <el-form-item label="主题色">
            <el-color-picker :model-value="store.data.settings.accent" @change="setAccent" />
            <el-button link style="margin-left:8px" @click="setAccent('#165dff')">恢复默认蓝</el-button>
            <span style="font-size:12px;color:#86909c;margin-left:10px">用于按钮、高亮等主色</span>
          </el-form-item>
        </el-form>
      </div>

      <div class="card">
        <div class="card-title">详情代码块字体</div>
        <div style="font-size:13px;color:#4e5969;margin-bottom:12px">
          作用于右侧详情所有代码块（接口调试响应、文档预览的请求/响应示例等），选择后将自动记住。
        </div>
        <el-form label-width="110px" label-position="left">
          <el-form-item label="字体">
            <el-select :model-value="store.data.settings.codeFont" @change="setCodeFont" style="width:300px">
              <el-option label="Consolas（默认）" value='Consolas, "Courier New", monospace' />
              <el-option label="Courier New" value='"Courier New", monospace' />
              <el-option label="Menlo / Monaco" value='Menlo, Monaco, monospace' />
              <el-option label="JetBrains Mono" value='"JetBrains Mono", Consolas, monospace' />
              <el-option label="Fira Code" value='"Fira Code", Consolas, monospace' />
              <el-option label="Source Code Pro" value='"Source Code Pro", Consolas, monospace' />
              <el-option label="微软雅黑等宽" value='"Microsoft YaHei Mono", Consolas, monospace' />
            </el-select>
          </el-form-item>
          <el-form-item label="字号(px)">
            <el-input-number :model-value="Number(store.data.settings.codeFontSize) || 12.5" :min="10" :max="24" :step="0.5" @change="setCodeFontSize" />
          </el-form-item>
        </el-form>
        <div class="code-font-preview" :style="{ fontFamily: store.data.settings.codeFont, fontSize: (Number(store.data.settings.codeFontSize) || 12.5) + 'px' }">{
  "status": 200,
  "data": { "name": "张三", "list": [1, 2, 3] }
}</div>
      </div>

      <div class="card">
        <div class="card-title">剪贴板记录</div>
        <el-form label-width="120px" label-position="left">
          <el-form-item label="自动监听">
            <el-switch :model-value="store.data.settings.clipboard.monitor" @change="setClipboardMonitor" />
            <span style="font-size:12px;color:#86909c;margin-left:10px">
              开启后自动记录系统剪贴板中的文本
            </span>
          </el-form-item>
          <el-form-item label="最大条数">
            <el-input-number v-model="store.data.settings.clipboard.maxItems" :min="10" :max="2000" />
          </el-form-item>
        </el-form>
      </div>

      <div class="card">
        <div class="card-title">数据存储</div>
        <div style="font-size:13px; color:#4e5969">
          所有接口信息（含最近一次请求与响应）自动保存在本地：<br>
          <code style="font-size:12px; color:var(--primary)">{{ dataPath }}</code>
        </div>
      </div>

      <el-button type="primary" @click="save">保存设置</el-button>
    </div>

    <CloudSync v-model:visible="cloudVisible" />
  </div>
</template>

<style scoped>
code { background: #f2f3f5; padding: 1px 5px; border-radius: 4px; }
.code-font-preview {
  margin-top: 4px; background: var(--code-bg); color: var(--code-text); border-radius: 8px;
  padding: 12px; white-space: pre; overflow: auto; line-height: 1.6;
}
</style>
