<script setup>
// 导出水印配置面板（Word / PDF 导出时生效）。
// 直接读写全局设置并即时保存，供「文档中心」等导出入口复用。
import { computed, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { PreviewDocWatermark } from '../../../wailsjs/go/main/App'
import { store, saveNow } from '../../store'

const props = defineProps({
  dirId: { type: String, default: '' }, // 预览水印时使用的目录范围
  apiId: { type: String, default: '' },
})

const wm = computed(() => store.data.settings.watermark)
const previewing = ref(false)

async function save() {
  try {
    await saveNow()
    ElMessage.success(wm.value.enabled ? '水印已启用并保存' : '水印已关闭并保存')
  } catch (e) {
    ElMessage.error(String(e))
  }
}

function resetStyle() {
  Object.assign(wm.value, {
    text: '内部资料', color: '#C0C4CC', size: 42, opacity: 0.14, angle: -30, repeat: true,
  })
  save()
}

// 水印文字支持的占位符（导出时由后端替换为实际值）
const WM_VARS = [
  { key: '{{project}}', label: '项目名' },
  { key: '{{date}}', label: '导出日期' },
  { key: '{{time}}', label: '导出时间' },
  { key: '{{datetime}}', label: '日期+时间' },
]
// 示例文案（拼接写，避免模板把大括号当成插值）
const WM_EXAMPLE = '内部资料 ' + WM_VARS[0].key + ' ' + WM_VARS[1].key

// 把占位符追加到水印文字末尾（超出长度上限时截断），方便不想手输的用户
function appendVar(key) {
  const cur = String(wm.value.text || '')
  if (cur.includes(key)) {
    ElMessage.info(`已包含 ${key}`)
    return
  }
  wm.value.text = (cur + key).slice(0, 40)
  save()
}

// 预览：用当前配置渲染 HTML 文档并在浏览器打开（与 PDF 导出共用同一水印层）
async function preview() {
  if (!wm.value.enabled) {
    ElMessage.warning('请先开启「启用水印」再预览')
    return
  }
  previewing.value = true
  try {
    await saveNow()
    const path = await PreviewDocWatermark(props.dirId || '', props.apiId || '')
    if (path) ElMessage.success('已在浏览器打开预览：' + path)
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    previewing.value = false
  }
}

// 实时样式预览：把 pt 字号按面板尺寸等比缩小，颜色/透明度/角度直接沿用配置
const previewStyle = computed(() => ({
  color: wm.value.color,
  opacity: wm.value.opacity,
  fontSize: Math.max(9, Math.min(26, (Number(wm.value.size) || 42) * 0.4)) + 'px',
  transform: `rotate(${Number(wm.value.angle) || 0}deg)`,
}))
</script>

<template>
  <div class="card">
    <div class="card-title">
      导出水印（Word / PDF）
      <el-tag :type="wm.enabled ? 'success' : 'info'" size="small" effect="plain" style="margin-left:8px">
        {{ wm.enabled ? '已启用' : '未启用' }}
      </el-tag>
    </div>
    <div class="wm-tip">
      启用水印后，导出的 Word（.docx）、PDF（接口文档 / 测试报告）每页都会叠加水印文字（如「内部资料」「禁止外传」），
      留存打印也可追溯；Markdown / HTML / OpenAPI 导出不受影响。配置自动保存。
    </div>
    <el-form label-width="92px" label-position="left">
      <el-form-item label="启用水印">
        <el-switch v-model="wm.enabled" @change="save" />
        <span class="wm-hint">关闭则导出文件不加水印</span>
      </el-form-item>
      <el-form-item label="水印文字">
        <el-input v-model="wm.text" maxlength="40" show-word-limit placeholder="内部资料"
          style="width:300px" @change="save" />
        <span class="wm-hint">留空则使用默认「内部资料」</span>
      </el-form-item>
      <el-form-item label="占位符">
        <el-tag v-for="v in WM_VARS" :key="v.key" class="wm-var" size="small" effect="plain"
          @click="appendVar(v.key)">{{ v.key }} {{ v.label }}</el-tag>
        <div class="wm-hint" style="margin-left:0">
          导出时自动替换为实际值，例如「{{ WM_EXAMPLE }}」适合标注来源与时间
        </div>
      </el-form-item>
      <el-form-item label="颜色/字号">
        <el-color-picker v-model="wm.color" @change="save" />
        <el-input-number v-model="wm.size" :min="8" :max="120" :step="2" size="small"
          style="margin-left:10px;width:110px" @change="save" />
        <span class="wm-hint">pt</span>
      </el-form-item>
      <el-form-item label="透明度">
        <el-slider v-model="wm.opacity" :min="0.03" :max="0.6" :step="0.01" style="width:220px" @change="save" />
        <span class="wm-hint">{{ Math.round(wm.opacity * 100) }}%</span>
      </el-form-item>
      <el-form-item label="倾斜角度">
        <el-input-number v-model="wm.angle" :min="-90" :max="90" :step="5" size="small"
          style="width:110px" @change="save" />
        <span class="wm-hint">度（负值向上倾斜）</span>
      </el-form-item>
      <el-form-item label="平铺">
        <el-switch v-model="wm.repeat" @change="save" />
        <span class="wm-hint">开启为整页平铺；关闭为页面居中单个水印</span>
      </el-form-item>
      <el-form-item label="效果">
        <el-button size="small" type="primary" plain :loading="previewing" @click="preview">👁 预览水印</el-button>
        <el-button size="small" @click="resetStyle">恢复默认样式</el-button>
        <span class="wm-hint">预览使用当前范围（{{ dirId ? '所选目录' : '全部接口' }}），在浏览器中按 Ctrl+P 可看打印效果</span>
      </el-form-item>
      <el-form-item label="实时样式">
        <div class="wm-preview" :class="{ tiled: wm.repeat, off: !wm.enabled }">
          <span v-for="i in (wm.repeat ? 6 : 1)" :key="i" :style="previewStyle">{{ wm.text || '内部资料' }}</span>
          <div v-if="!wm.enabled" class="wm-preview-off">水印未启用</div>
        </div>
        <span class="wm-hint">按当前文字 / 颜色 / 字号 / 透明度 / 角度实时呈现，最终以导出效果为准</span>
      </el-form-item>
    </el-form>
  </div>
</template>

<style scoped>
.wm-tip { font-size: 13px; color: #4e5969; margin-bottom: 12px; }
.wm-hint { font-size: 12px; color: #86909c; margin-left: 10px; }
.wm-var { margin: 0 6px 6px 0; cursor: pointer; }
.wm-preview {
  position: relative; width: 300px; height: 84px; border: 1px dashed #c9cdd4; border-radius: 8px;
  background: #fff; overflow: hidden; display: flex; align-items: center; justify-content: center;
  gap: 6px 14px; flex-wrap: nowrap; user-select: none;
}
.wm-preview.tiled { flex-wrap: wrap; align-content: center; justify-content: center; }
.wm-preview span { font-weight: 600; white-space: nowrap; line-height: 1.2; }
.wm-preview.off span { opacity: .25 !important; }
.wm-preview-off {
  position: absolute; inset: 0; display: flex; align-items: center; justify-content: center;
  font-size: 12px; color: #86909c; background: rgba(247, 248, 250, .7);
}
</style>
