<script setup>
import { ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { ToolHash, ToolHmac, ToolCipher } from '../../wailsjs/go/main/App'

const props = defineProps({ tool: { type: String, default: 'json' } })

// 判断是否在 Wails 桌面运行时（Go 桥可用），用于区分纯预览环境
function hasGoBridge() {
  return !!(window.go && window.go.main && window.go.main.App)
}

// ------------------------------------------------------------------
// 通用
// ------------------------------------------------------------------
async function copyText(t) {
  if (!t) return
  try {
    await navigator.clipboard.writeText(t)
    ElMessage.success('已复制到剪贴板')
  } catch {
    const ta = document.createElement('textarea')
    ta.value = t
    document.body.appendChild(ta)
    ta.select()
    try { document.execCommand('copy'); ElMessage.success('已复制到剪贴板') } catch { ElMessage.error('复制失败') }
    document.body.removeChild(ta)
  }
}

// Base64（UTF-8 安全）
function b64encode(str) {
  const bytes = new TextEncoder().encode(str)
  let bin = ''
  bytes.forEach(b => { bin += String.fromCharCode(b) })
  return btoa(bin)
}
function b64decode(b64) {
  const bin = atob(b64.trim())
  const bytes = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i)
  return new TextDecoder().decode(bytes)
}

// ------------------------------------------------------------------
// JSON：格式化 / 压缩 / 转义 / 反转义
// ------------------------------------------------------------------
const jsonInput = ref('')
const jsonOutput = ref('')

function fmtJSON() {
  try {
    const obj = JSON.parse(jsonInput.value)
    jsonOutput.value = JSON.stringify(obj, null, 2)
  } catch (e) { ElMessage.error('JSON 解析失败：' + e.message) }
}
function cmpJSON() {
  try {
    const obj = JSON.parse(jsonInput.value)
    jsonOutput.value = JSON.stringify(obj)
  } catch (e) { ElMessage.error('JSON 解析失败：' + e.message) }
}
function escJSON() {
  const s = jsonInput.value
  jsonOutput.value = s
    .replace(/\\/g, '\\\\')
    .replace(/"/g, '\\"')
    .replace(/\n/g, '\\n')
    .replace(/\r/g, '\\r')
    .replace(/\t/g, '\\t')
}
function unescJSON() {
  const s = jsonInput.value
  jsonOutput.value = s
    .replace(/\\r/g, '\r')
    .replace(/\\n/g, '\n')
    .replace(/\\t/g, '\t')
    .replace(/\\b/g, '\b')
    .replace(/\\f/g, '\f')
    .replace(/\\"/g, '"')
    .replace(/\\\\/g, '\\')
}
function swapJSON() {
  const t = jsonOutput.value
  jsonOutput.value = jsonInput.value
  jsonInput.value = t
}

// ------------------------------------------------------------------
// SQL：格式化 / 压缩
// ------------------------------------------------------------------
const sqlInput = ref('')
const sqlOutput = ref('')

const SQL_CLAUSES = [
  'SELECT', 'FROM', 'WHERE', 'GROUP BY', 'ORDER BY', 'HAVING', 'LIMIT', 'OFFSET',
  'UNION ALL', 'UNION', 'LEFT JOIN', 'RIGHT JOIN', 'INNER JOIN', 'FULL JOIN',
  'CROSS JOIN', 'JOIN', 'ON', 'INSERT INTO', 'VALUES', 'UPDATE', 'SET',
  'DELETE FROM', 'CREATE TABLE', 'ALTER TABLE', 'DROP TABLE', 'CASE', 'WHEN', 'THEN', 'ELSE', 'END',
]

function fmtSQL() {
  if (!sqlInput.value) { sqlOutput.value = ''; return }
  let s = sqlInput.value.replace(/\s+/g, ' ').trim()
  for (const kw of SQL_CLAUSES) {
    const re = new RegExp('\\s+\\b' + kw + '\\b', 'gi')
    s = s.replace(re, '\n' + kw)
  }
  s = s.replace(/\s+\bAND\b/gi, '\n  AND')
  s = s.replace(/\s+\bOR\b/gi, '\n  OR')
  s = s.replace(/,\s*/g, ',\n  ')
  sqlOutput.value = s.replace(/^\s+/, '')
}
function cmpSQL() {
  if (!sqlInput.value) { sqlOutput.value = ''; return }
  sqlOutput.value = sqlInput.value.replace(/\s+/g, ' ').trim()
}

// ------------------------------------------------------------------
// 加密 / 解密
// ------------------------------------------------------------------
const cryptoAlgo = ref('md5')
const cryptoOp = ref('encrypt') // encrypt(encode) | decrypt(decode)
const cryptoText = ref('')
const cryptoKey = ref('')
const cryptoIv = ref('')
const cryptoMode = ref('cbc')
const cryptoOutEnc = ref('base64')
const cryptoOutput = ref('')
const cryptoLoading = ref(false)

const isCodec = computed(() => ['base64', 'url'].includes(cryptoAlgo.value))
const isHash = computed(() => ['md5', 'sha1', 'sha256', 'sha512'].includes(cryptoAlgo.value))
const isHmac = computed(() => cryptoAlgo.value.startsWith('hmac-'))
const isSym = computed(() => ['aes', 'des', '3des'].includes(cryptoAlgo.value))
const needKey = computed(() => isHmac.value || isSym.value)
const showOp = computed(() => !isHash.value && !isHmac.value)
const showMode = computed(() => isSym.value)
const showIv = computed(() => isSym.value && cryptoMode.value === 'cbc')

async function runCrypto() {
  const algo = cryptoAlgo.value
  const text = cryptoText.value
  if (!isCodec.value && !text) { ElMessage.warning('请输入内容'); return }
  if (needKey.value && !cryptoKey.value) { ElMessage.warning('请输入密钥'); return }
  cryptoLoading.value = true
  try {
    if (isCodec.value) {
      if (algo === 'base64') {
        cryptoOutput.value = cryptoOp.value === 'decrypt' ? b64decode(text) : b64encode(text)
      } else {
        cryptoOutput.value = cryptoOp.value === 'decrypt' ? decodeURIComponent(text) : encodeURIComponent(text)
      }
      return
    }
    if (!hasGoBridge()) { ElMessage.warning('当前为预览模式，加密功能需在桌面客户端使用'); return }
    if (isHash.value) {
      const r = await ToolHash(text, algo)
      cryptoOutput.value = r.ok ? r.output : '错误：' + r.error
    } else if (isHmac.value) {
      const sub = algo.split('-')[1]
      const r = await ToolHmac(text, cryptoKey.value, sub)
      cryptoOutput.value = r.ok ? r.output : '错误：' + r.error
    } else if (isSym.value) {
      const r = await ToolCipher(algo, cryptoMode.value, cryptoOp.value, text, cryptoKey.value, cryptoIv.value, cryptoOutEnc.value)
      cryptoOutput.value = r.ok ? r.output : '错误：' + r.error
    }
  } catch (e) {
    cryptoOutput.value = '错误：' + (e.message || e)
  } finally {
    cryptoLoading.value = false
  }
}

// ------------------------------------------------------------------
// 时间戳转换
// ------------------------------------------------------------------
const tsInput = ref('')
const tsNowSec = computed(() => Math.floor(Date.now() / 1000))
const tsNowMs = computed(() => Date.now())

function tsParse() {
  const v = tsInput.value.trim()
  if (!v) return null
  if (/^\d{1,13}$/.test(v)) {
    let n = Number(v)
    if (v.length <= 10) n = n * 1000
    return new Date(n)
  }
  const d = new Date(v)
  if (isNaN(d.getTime())) return null
  return d
}
const tsResult = computed(() => {
  const d = tsParse()
  if (!d) return null
  return {
    local: d.toLocaleString(),
    utc: d.toUTCString(),
    iso: d.toISOString(),
    sec: Math.floor(d.getTime() / 1000),
    ms: d.getTime(),
  }
})
function fillNow() { tsInput.value = String(Date.now()) }

// ------------------------------------------------------------------
// PHP 序列化 / 反序列化（JSON ⇄ PHP serialize() 格式）
// ------------------------------------------------------------------
const serMode = ref('serialize') // serialize: JSON→PHP | deserialize: PHP→JSON
const serInput = ref('')
const serOutput = ref('')

// 示例数据：便于直观了解 PHP 序列化格式
const SER_EXAMPLE_JSON = JSON.stringify(
  { name: '张三', age: 18, active: true, tags: ['php', 'go'], info: { city: '北京', zip: '100000' } },
  null, 2
)
const SER_EXAMPLE_PHP = 'a:5:{s:4:"name";s:6:"张三";s:3:"age";i:18;s:6:"active";b:1;s:4:"tags";a:2:{i:0;s:3:"php";i:1;s:2:"go";}s:4:"info";a:2:{s:4:"city";s:6:"北京";s:3:"zip";s:6:"100000";}}'
// 反序列化输入框的占位示例（避免在模板属性里写转义引号）
const SER_PH_PHP = 'a:2:{s:4:"name";s:6:"张三";s:3:"age";i:18;}'

// UTF-8 字节长度（PHP 中字符串按字节计数，而非字符数）
function utf8Bytes(str) {
  return new TextEncoder().encode(str).length
}

// JSON 值 → PHP 序列化字符串
function phpSerialize(value) {
  if (value === null || value === undefined) return 'N;'
  const t = typeof value
  if (t === 'boolean') return 'b:' + (value ? '1' : '0') + ';'
  if (t === 'number') {
    // 整数使用 i:，浮点使用 d:
    return (Number.isInteger(value) ? 'i:' : 'd:') + value + ';'
  }
  if (t === 'string') {
    // 字符串长度取 UTF-8 字节数
    return 's:' + utf8Bytes(value) + ':"' + value + '";'
  }
  // 数组：键为 0..n-1 连续整数时序列化为数字键，否则为字符串键
  if (Array.isArray(value)) {
    const parts = value.map((v, i) => 'i:' + i + ';' + phpSerialize(v))
    return 'a:' + value.length + ':{' + parts.join('') + '}'
  }
  // 对象：__php_class 字段用于序列化为 PHP 对象(O:)
  if (t === 'object') {
    const cls = (value.__php_class !== undefined) ? String(value.__php_class) : null
    const keys = Object.keys(value).filter(k => k !== '__php_class')
    const parts = keys.map(k =>
      (/^\d+$/.test(k) ? 'i:' + Number(k) : phpSerialize(k)) + phpSerialize(value[k])
    )
    if (cls !== null) {
      return 'O:' + utf8Bytes(cls) + ':"' + cls + '":' + keys.length + ':{' + parts.join('') + '}'
    }
    return 'a:' + keys.length + ':{' + parts.join('') + '}'
  }
  return 'N;'
}

// PHP 序列化字符串 → JSON 值（基于 UTF-8 字节流解析，支持中文等多字节）
function phpUnserialize(input) {
  const bytes = new TextEncoder().encode(input) // 转字节流，便于按字节长度读取字符串
  const dec = new TextDecoder()
  let pos = 0

  const readUntil = (token) => {
    let s = ''
    while (bytes[pos] !== token.charCodeAt(0)) { s += String.fromCharCode(bytes[pos]); pos++ }
    return s
  }
  const parseIntVal = () => { const n = parseInt(readUntil(';'), 10); pos++; return n }
  const parseFloatVal = () => { const n = parseFloat(readUntil(';')); pos++; return n }

  const parseString = () => {
    pos += 2                 // 跳过 's:'
    const len = parseInt(readUntil(':'), 10)
    pos += 2                 // 跳过 ':' 与 '"'
    const slice = bytes.slice(pos, pos + len)
    pos += len + 2           // 跳过内容、'"" 与 ';'
    return dec.decode(slice)
  }

  const parseArray = (isObj) => {
    pos += 2                 // 跳过 'a:' 或 'O:'
    let className = null
    if (isObj) {
      const nlen = parseInt(readUntil(':'), 10)
      pos += 2               // 跳过 ':' 与 '"'
      className = dec.decode(bytes.slice(pos, pos + nlen))
      pos += nlen + 2        // 跳过类名、'"' 与 ':'
    }
    const count = parseInt(readUntil(':'), 10)
    pos += 2                 // 跳过 ':' 与 '{'
    const pairs = []
    for (let i = 0; i < count; i++) {
      pairs.push([parseValue(), parseValue()])
    }
    pos++                    // 跳过 '}'
    // 键为 0..n-1 连续整数 → 还原为数组，否则为对象
    const sequential = pairs.length > 0 && pairs.every((p, i) => (typeof p[0] === 'number' && p[0] === i))
    if (sequential) return pairs.map(p => p[1])
    const obj = {}
    if (className) obj.__php_class = className
    for (const [k, v] of pairs) obj[typeof k === 'number' ? k : String(k)] = v
    return obj
  }

  function parseValue() {
    const ch = String.fromCharCode(bytes[pos])
    switch (ch) {
      case 'N': pos += 2; return null
      case 'b': { pos += 2; const v = bytes[pos] === 49; pos += 2; return v }
      case 'i': pos += 2; return parseIntVal()
      case 'd': pos += 2; return parseFloatVal()
      case 's': return parseString()
      case 'a': return parseArray(false)
      case 'O': return parseArray(true)
      case 'r': case 'R': pos += 2; readUntil(';'); pos++; return null // 引用，工具中忽略
      default: pos++; return null
    }
  }

  return parseValue()
}

function doSer() {
  try {
    if (serMode.value === 'serialize') {
      const obj = JSON.parse(serInput.value)
      serOutput.value = phpSerialize(obj)
    } else {
      serOutput.value = JSON.stringify(phpUnserialize(serInput.value.trim()), null, 2)
    }
  } catch (e) { ElMessage.error('转换失败：' + e.message) }
}

function loadSerExample() {
  serInput.value = serMode.value === 'serialize' ? SER_EXAMPLE_JSON : SER_EXAMPLE_PHP
}
</script>

<template>
  <div class="toolbox">
    <div class="tb-panel">
      <!-- ===================== JSON ===================== -->
      <template v-if="props.tool === 'json'">
        <div class="tb-row">
          <div class="tb-col">
            <div class="tb-col-hd">
              <span>输入</span>
              <el-button size="small" text @click="jsonInput = jsonOutput">← 取结果</el-button>
            </div>
            <el-input v-model="jsonInput" type="textarea" placeholder="粘贴 JSON 文本" />
          </div>
          <div class="tb-col">
            <div class="tb-col-hd">
              <span>输出</span>
              <el-button size="small" text type="primary" :disabled="!jsonOutput" @click="copyText(jsonOutput)">复制</el-button>
            </div>
            <el-input v-model="jsonOutput" type="textarea" readonly placeholder="处理结果" />
          </div>
        </div>
        <div class="tb-actions">
          <el-button type="primary" @click="fmtJSON">格式化</el-button>
          <el-button @click="cmpJSON">压缩</el-button>
          <el-button @click="escJSON">转义</el-button>
          <el-button @click="unescJSON">反转义</el-button>
          <el-button @click="swapJSON">交换</el-button>
        </div>
      </template>

      <!-- ===================== SQL ===================== -->
      <template v-else-if="props.tool === 'sql'">
        <div class="tb-row">
          <div class="tb-col">
            <div class="tb-col-hd"><span>输入</span></div>
            <el-input v-model="sqlInput" type="textarea" placeholder="粘贴 SQL 语句" />
          </div>
          <div class="tb-col">
            <div class="tb-col-hd">
              <span>输出</span>
              <el-button size="small" text type="primary" :disabled="!sqlOutput" @click="copyText(sqlOutput)">复制</el-button>
            </div>
            <el-input v-model="sqlOutput" type="textarea" readonly placeholder="处理结果" />
          </div>
        </div>
        <div class="tb-actions">
          <el-button type="primary" @click="fmtSQL">格式化</el-button>
          <el-button @click="cmpSQL">压缩</el-button>
        </div>
      </template>

      <!-- ===================== 加密 / 解密 ===================== -->
      <template v-else-if="props.tool === 'crypto'">
        <div class="crypto-form">
          <div class="cf-line">
            <span class="cf-label">算法</span>
            <el-select v-model="cryptoAlgo" style="width: 220px">
              <el-option-group label="编码">
                <el-option label="Base64" value="base64" />
                <el-option label="URL 编码" value="url" />
              </el-option-group>
              <el-option-group label="哈希（摘要）">
                <el-option label="MD5" value="md5" />
                <el-option label="SHA1" value="sha1" />
                <el-option label="SHA256" value="sha256" />
                <el-option label="SHA512" value="sha512" />
              </el-option-group>
              <el-option-group label="HMAC">
                <el-option label="HMAC-MD5" value="hmac-md5" />
                <el-option label="HMAC-SHA1" value="hmac-sha1" />
                <el-option label="HMAC-SHA256" value="hmac-sha256" />
                <el-option label="HMAC-SHA512" value="hmac-sha512" />
              </el-option-group>
              <el-option-group label="对称加密">
                <el-option label="AES" value="aes" />
                <el-option label="DES" value="des" />
                <el-option label="3DES" value="3des" />
              </el-option-group>
            </el-select>

            <template v-if="showOp">
              <span class="cf-label">操作</span>
              <el-radio-group v-model="cryptoOp">
                <el-radio-button value="encrypt">加密/编码</el-radio-button>
                <el-radio-button value="decrypt">解密/解码</el-radio-button>
              </el-radio-group>
            </template>
          </div>

          <div class="cf-line" v-if="showMode">
            <span class="cf-label">模式</span>
            <el-radio-group v-model="cryptoMode">
              <el-radio-button value="ecb">ECB</el-radio-button>
              <el-radio-button value="cbc">CBC</el-radio-button>
            </el-radio-group>
            <span class="cf-label">输出编码</span>
            <el-radio-group v-model="cryptoOutEnc">
              <el-radio-button value="base64">Base64</el-radio-button>
              <el-radio-button value="hex">Hex</el-radio-button>
            </el-radio-group>
          </div>

          <div class="cf-line" v-if="needKey">
            <span class="cf-label">密钥</span>
            <el-input v-model="cryptoKey" style="width: 320px" placeholder="Key" />
          </div>
          <div class="cf-line" v-if="showIv">
            <span class="cf-label">IV</span>
            <el-input v-model="cryptoIv" style="width: 320px" placeholder="CBC 模式初始化向量（长度须等于分组大小）" />
          </div>
        </div>

        <div class="tb-row" style="margin-top: 12px">
          <div class="tb-col">
            <div class="tb-col-hd"><span>{{ isHash ? '待摘要文本' : (cryptoOp === 'decrypt' ? '密文' : '明文') }}</span></div>
            <el-input v-model="cryptoText" type="textarea" :rows="12" placeholder="输入内容" />
          </div>
          <div class="tb-col">
            <div class="tb-col-hd">
              <span>结果</span>
              <el-button size="small" text type="primary" :disabled="!cryptoOutput" @click="copyText(cryptoOutput)">复制</el-button>
            </div>
            <el-input v-model="cryptoOutput" type="textarea" :rows="12" readonly placeholder="结果" />
          </div>
        </div>
        <div class="tb-actions">
          <el-button type="primary" :loading="cryptoLoading" @click="runCrypto">执行</el-button>
        </div>
      </template>

      <!-- ===================== 时间戳 ===================== -->
      <template v-else-if="props.tool === 'timestamp'">
        <div class="tb-block">
          <div class="tb-line">
            <span class="tb-tip">当前时间戳：</span>
            <code class="tb-code">秒 {{ tsNowSec }}</code>
            <code class="tb-code">毫秒 {{ tsNowMs }}</code>
            <el-button size="small" @click="fillNow">填入当前毫秒</el-button>
          </div>
          <div class="tb-line">
            <span class="cf-label">输入</span>
            <el-input v-model="tsInput" style="width: 360px" placeholder="时间戳（秒/毫秒）或日期字符串，如 2026-07-30 12:00:00" />
            <span class="tb-tip">（自动识别：纯数字按时间戳，含字符按日期）</span>
          </div>
          <template v-if="tsResult">
            <div class="tb-line"><span class="cf-label">本地时间</span><code class="tb-code wide">{{ tsResult.local }}</code></div>
            <div class="tb-line"><span class="cf-label">UTC 时间</span><code class="tb-code wide">{{ tsResult.utc }}</code></div>
            <div class="tb-line"><span class="cf-label">ISO 8601</span><code class="tb-code wide">{{ tsResult.iso }}</code></div>
            <div class="tb-line"><span class="cf-label">时间戳(秒)</span><code class="tb-code">{{ tsResult.sec }}</code><el-button size="small" text type="primary" @click="copyText(String(tsResult.sec))">复制</el-button></div>
            <div class="tb-line"><span class="cf-label">时间戳(毫秒)</span><code class="tb-code">{{ tsResult.ms }}</code><el-button size="small" text type="primary" @click="copyText(String(tsResult.ms))">复制</el-button></div>
          </template>
          <div v-else class="tb-empty">请输入有效的时间戳或日期</div>
        </div>
      </template>

      <!-- ===================== PHP 序列化 / 反序列化 ===================== -->
      <template v-else-if="props.tool === 'serialize'">
        <div class="tb-block">
          <div class="tb-line">
            <el-radio-group v-model="serMode">
              <el-radio-button value="serialize">JSON → PHP serialize</el-radio-button>
              <el-radio-button value="deserialize">PHP serialize → JSON</el-radio-button>
            </el-radio-group>
            <el-button size="small" @click="loadSerExample">载入示例</el-button>
            <span class="tb-tip">PHP 序列化格式：s:长度:"值"; i:整数; b:0/1; a:数量:{键;值;…}</span>
          </div>
          <div class="tb-row" style="margin-top: 12px">
            <div class="tb-col">
              <div class="tb-col-hd"><span>{{ serMode === 'serialize' ? 'JSON 输入' : 'PHP 序列化输入' }}</span></div>
              <el-input v-model="serInput" type="textarea" :rows="14" :placeholder="serMode === 'serialize' ? '粘贴 JSON 对象' : SER_PH_PHP" />
            </div>
            <div class="tb-col">
              <div class="tb-col-hd">
                <span>结果</span>
                <el-button size="small" text type="primary" :disabled="!serOutput" @click="copyText(serOutput)">复制</el-button>
              </div>
              <el-input v-model="serOutput" type="textarea" :rows="14" readonly placeholder="转换结果" />
            </div>
          </div>
          <div class="tb-actions">
            <el-button type="primary" @click="doSer">转换</el-button>
          </div>
        </div>
      </template>
    </div>
  </div>
</template>

<style scoped>
.toolbox { padding: 12px; height: 100%; box-sizing: border-box; overflow: hidden; }
.tb-panel { height: 100%; display: flex; flex-direction: column; }

.tb-row { flex: 1; min-height: 0; display: flex; gap: 16px; align-items: stretch; }
.tb-col { flex: 1; display: flex; flex-direction: column; min-width: 0; min-height: 0; }
.tb-col-hd { display: flex; align-items: center; justify-content: space-between; margin-bottom: 6px; font-size: 13px; color: #4e5969; flex-shrink: 0; }
.tb-col :deep(.el-textarea) { flex: 1; display: flex; min-height: 0; }
.tb-col :deep(.el-textarea__inner) { flex: 1; width: 100%; height: 100%; resize: none; }

.tb-actions { margin-top: 12px; display: flex; gap: 10px; flex-wrap: wrap; flex-shrink: 0; }

.crypto-form { display: flex; flex-direction: column; gap: 12px; flex-shrink: 0; }
.cf-line { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.cf-label { font-size: 13px; color: #4e5969; min-width: 42px; }

.tb-block { flex: 1; min-height: 0; display: flex; flex-direction: column; gap: 12px; overflow: auto; }
.tb-line { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.tb-tip { font-size: 12px; color: #86909c; }
.tb-code { background: #f2f3f5; padding: 3px 8px; border-radius: 4px; font-family: Consolas, monospace; font-size: 13px; color: #1d2129; }
.tb-code.wide { min-width: 360px; }
.tb-empty { color: #86909c; font-size: 13px; padding: 8px 0; }
</style>
