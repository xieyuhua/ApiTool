<script setup>
// AI Agent 局域网聊天：开启后可在手机 / 其他电脑的浏览器里访问同一份会话并远程提问。
// 访问地址带随机 token（Agent 具备文件读写与命令执行能力，绝不能匿名暴露）。
import { ref, watch, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { AgentAPI, isWebUI } from './agentApi'

const props = defineProps({ visible: Boolean })
const emit = defineEmits(['update:visible'])

// 兜底：当前已经运行在局域网网页端（本身就是被「局域网访问」打开的那一端），
// 再展示「开启局域网访问」没有意义。顶栏按钮虽已隐藏，这里再拦一道，
// 避免任何入口（含旧缓存的页面）漏出来。
const webMode = computed(() => isWebUI())

const info = ref(null)
const port = ref('8090')
const busy = ref(false)

async function reload() {
  if (!props.visible) return
  try { info.value = await AgentAPI.webChatInfo() } catch (e) { ElMessage.error(String(e)) }
}

async function start() {
  busy.value = true
  try {
    info.value = await AgentAPI.startWebChat(port.value.trim() || '8090')
    ElMessage.success('已开启局域网访问')
  } catch (e) { ElMessage.error(String(e)) } finally { busy.value = false }
}

async function stop() {
  try {
    await ElMessageBox.confirm('停止后局域网将无法访问，已打开的网页会失效。确认停止？', '提示', { type: 'warning' })
  } catch { return }
  busy.value = true
  try {
    await AgentAPI.stopWebChat()
    info.value = await AgentAPI.webChatInfo()
    ElMessage.success('已停止')
  } catch (e) { ElMessage.error(String(e)) } finally { busy.value = false }
}

async function resetToken() {
  try {
    await ElMessageBox.confirm('重置后旧的分享链接会立即失效，需要把新链接重新发给对方。确认重置？', '提示', { type: 'warning' })
  } catch { return }
  try {
    info.value = await AgentAPI.resetWebChatToken()
    ElMessage.success('已生成新链接')
  } catch (e) { ElMessage.error(String(e)) }
}

async function copyLink() {
  const link = info.value && info.value.link
  if (!link) return
  const text = `${link}\n（局域网内其他设备直接打开；请勿转发到公网）`
  try {
    await navigator.clipboard.writeText(text)
  } catch {
    const ta = document.createElement('textarea')
    ta.value = text
    document.body.appendChild(ta)
    ta.select()
    try { document.execCommand('copy') } catch { ElMessage.error('复制失败，请手动选中复制'); return }
    document.body.removeChild(ta)
  }
  ElMessage.success('链接已复制：' + info.value.public)
}

watch(() => props.visible, (v) => { if (v && !isWebUI()) reload() })
</script>

<template>
  <el-dialog :model-value="visible" @update:model-value="emit('update:visible', $event)"
    title="📱 局域网访问（手机 / 其他电脑）" width="560px">
    <!-- 网页端兜底提示：当前页面本身就是局域网网页，无需再次开启 -->
    <div v-if="webMode">
      <el-alert type="info" :closable="false" show-icon
        title="当前已经是局域网网页端" />
      <p class="desc" style="margin-top:12px">
        你正在通过局域网网页访问 AI Agent，无需再次开启。如需更换访问地址、查看令牌或停止服务，请回到运行本程序的电脑上，在「📱 局域网」中操作。
      </p>
      <div class="ops">
        <el-button size="small" type="primary" @click="emit('update:visible', false)">知道了</el-button>
      </div>
    </div>

    <div v-else-if="info && info.running" class="on">
      <el-alert type="warning" :closable="false" show-icon
        title="Agent 具备文件读写、执行命令、数据库查询能力，请只在可信局域网内分享链接。" />
      <div class="field">
        <label>访问地址</label>
        <div class="link-row">
          <el-input :model-value="info.link" readonly />
          <el-button type="primary" @click="copyLink">复制链接</el-button>
        </div>
        <div class="hint">本机：{{ info.url }}　·　局域网：{{ info.public }}　·　端口：{{ info.port }}</div>
      </div>
      <div class="field">
        <label>令牌（重置后旧链接立即失效）</label>
        <div class="link-row">
          <el-input :model-value="info.token" readonly />
          <el-button @click="resetToken">重置令牌</el-button>
        </div>
      </div>
      <div class="tips">
        <div>· 手机与电脑连同一个 Wi-Fi / 局域网，浏览器打开上面的链接即可对话。</div>
        <div>· 打开一次后令牌会写入该浏览器 Cookie，后续直接访问根地址即可。</div>
        <div>· 网页端与桌面端共享同一份会话：这边提问，桌面端刷新即可看到，反之亦然。</div>
        <div>· 若打不开，请确认 Windows 防火墙已放行该端口。</div>
      </div>
      <div class="ops">
        <el-button size="small" @click="reload">刷新状态</el-button>
        <el-button size="small" type="danger" plain :loading="busy" @click="stop">停止服务</el-button>
      </div>
    </div>

    <div v-else class="off">
      <p class="desc">开启后会在本机启动一个仅局域网可访问的网页服务，手机或其他电脑用浏览器打开带令牌的地址，即可远程与 AI Agent 对话（会话与桌面端实时共享）。</p>
      <div class="field">
        <label>服务端口</label>
        <el-input v-model="port" style="width:160px" placeholder="8090" />
        <span class="hint">默认 8090，被占用时可换一个</span>
      </div>
      <el-button type="primary" :loading="busy" @click="start">开启局域网访问</el-button>
    </div>
  </el-dialog>
</template>

<style scoped>
.desc { font-size: 13px; color: var(--text-muted); line-height: 1.7; margin: 0 0 14px; }
.field { margin: 14px 0; }
.field > label { display: block; font-size: 12px; color: var(--text-muted); margin-bottom: 6px; }
.link-row { display: flex; gap: 8px; align-items: center; }
.hint { font-size: 11px; color: var(--text-muted); margin-top: 6px; }
.tips { font-size: 12px; color: var(--text-muted); line-height: 1.9; margin-top: 4px; }
.ops { margin-top: 16px; display: flex; gap: 8px; }
</style>
