<script setup>
import { ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { CreateShareLink, ListShares, StopShare, OpenInBrowser, CopyToClipboard } from '../../wailsjs/go/main/App'

const props = defineProps({
  dirId: { type: String, default: '' },
  apiId: { type: String, default: '' },
})
const visible = defineModel('visible')

const password = ref('')
const expire = ref(60) // 默认 1 小时
const expireOptions = [
  { label: '10 分钟', value: 10 },
  { label: '1 小时', value: 60 },
  { label: '1 天', value: 1440 },
  { label: '7 天', value: 10080 },
  { label: '不失效（长期有效）', value: 0 },
]
const result = ref('')
const shares = ref([])
const busy = ref(false)

async function refresh() {
  try { shares.value = await ListShares() } catch { shares.value = [] }
}

watch(visible, (v) => { if (v) { result.value = ''; refresh() } })

async function create() {
  busy.value = true
  try {
    const link = await CreateShareLink(props.dirId, props.apiId, password.value, expire.value)
    result.value = link
    ElMessage.success('分享链接已生成')
    refresh()
  } catch (e) {
    ElMessage.error(String(e))
  } finally { busy.value = false }
}

async function stop(token) {
  try {
    await StopShare(token)
    ElMessage.success('已停止该分享')
    refresh()
  } catch (e) { ElMessage.error(String(e)) }
}

async function copy(link) {
  try { await CopyToClipboard(link); ElMessage.success('链接已复制') } catch (e) { ElMessage.error(String(e)) }
}
async function open(link) {
  try { await OpenInBrowser(link) } catch (e) { ElMessage.error(String(e)) }
}

function fmtExpire(exp) {
  if (!exp) return '长期有效'
  const left = exp - Math.floor(Date.now() / 1000)
  if (left <= 0) return '已过期'
  if (left < 60) return left + ' 秒后失效'
  if (left < 3600) return Math.floor(left / 60) + ' 分钟后失效'
  if (left < 86400) return Math.floor(left / 3600) + ' 小时后失效'
  return Math.floor(left / 86400) + ' 天后失效'
}
</script>

<template>
  <el-dialog v-model="visible" title="分享为网页链接" width="640px">
    <div class="share-box">
      <el-alert type="info" :closable="false" show-icon
        style="margin-bottom:14px"
        title="说明：该链接由本工具内置的本地服务托管，他人在浏览器打开即可查看文档（区别于「导出 HTML 文件」）。分享期间请保持本程序运行；同一局域网可将 localhost 替换为你的 IP 访问。" />

      <el-form label-width="90px">
        <el-form-item label="访问密码">
          <el-input v-model="password" type="password" show-password placeholder="留空表示无需密码" style="max-width:260px" />
        </el-form-item>
        <el-form-item label="有效期">
          <el-select v-model="expire" style="width:220px">
            <el-option v-for="o in expireOptions" :key="o.value" :label="o.label" :value="o.value" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :loading="busy" @click="create">生成分享链接</el-button>
        </el-form-item>
      </el-form>

      <div v-if="result" class="result">
        <div class="result-label">分享链接（已含密码保护时由链接访问时输入密码）：</div>
        <div class="link-row">
          <el-input :model-value="result" readonly />
          <el-button @click="copy(result)">复制</el-button>
          <el-button type="primary" @click="open(result)">打开</el-button>
        </div>
      </div>

      <div class="active-list" v-if="shares.length">
        <div class="al-title">当前有效分享（{{ shares.length }}）</div>
        <div v-for="s in shares" :key="s.token" class="al-item">
          <div class="al-main">
            <span class="al-name">{{ s.title }}</span>
            <el-tag v-if="s.hasPassword" size="small" type="warning" effect="plain">有密码</el-tag>
            <span class="al-exp">{{ fmtExpire(s.expireAt) }}</span>
          </div>
          <div class="al-ops">
            <el-button link type="primary" @click="copy(s.link)">复制</el-button>
            <el-button link type="primary" @click="open(s.link)">打开</el-button>
            <el-button link type="danger" @click="stop(s.token)">停止</el-button>
          </div>
        </div>
      </div>
    </div>
  </el-dialog>
</template>

<style scoped>
.share-box { font-size: 13px; }
.result { background:#f7f8fa; border-radius:8px; padding:12px; margin-bottom:14px; }
.result-label { color:#4e5969; margin-bottom:8px; }
.link-row { display:flex; gap:8px; }
.link-row .el-input { flex:1; }
.active-list { border-top:1px solid #f0f1f3; padding-top:12px; }
.al-title { color:#4e5969; margin-bottom:8px; }
.al-item { display:flex; align-items:center; justify-content:space-between; padding:8px 0; border-bottom:1px dashed #eef0f3; }
.al-main { display:flex; align-items:center; gap:8px; overflow:hidden; }
.al-name { font-weight:600; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; max-width:280px; }
.al-exp { color:#86909c; font-size:12px; }
</style>
