// 代码块展示字体（字体 + 字号）的全局配置。
// 在「设置」页统一配置，所有展示代码块的面板（如网络抓包右侧详情）共用同一份设置。
// 仅存浏览器本地（localStorage），属于界面偏好，不参与文档数据同步。
import { ref, computed, watch } from 'vue'

// 字体栈：靠前者优先，系统中不存在时自动回退到后面的字体
export const CODE_FONT_OPTIONS = [
  { label: '默认（系统等宽）', value: 'ui-monospace, SFMono-Regular, "Cascadia Code", "Cascadia Mono", Menlo, Consolas, "Courier New", monospace' },
  { label: 'Cascadia Code', value: '"Cascadia Code", "Cascadia Mono", Consolas, monospace' },
  { label: 'JetBrains Mono', value: '"JetBrains Mono", Consolas, monospace' },
  { label: 'Consolas', value: 'Consolas, "Lucida Console", monospace' },
  { label: 'Source Code Pro', value: '"Source Code Pro", Consolas, monospace' },
  { label: 'Fira Code', value: '"Fira Code", Consolas, monospace' },
  { label: 'Courier New', value: '"Courier New", monospace' },
  { label: '微软雅黑', value: '"Microsoft YaHei", "微软雅黑", sans-serif' },
  { label: '黑体', value: 'SimHei, "Microsoft YaHei", sans-serif' },
  { label: '宋体', value: 'SimSun, "Songti SC", serif' },
  { label: '楷体', value: 'KaiTi, "Kaiti SC", serif' },
]

export const CODE_FONT_SIZES = [12, 13, 14, 15, 16, 18]

const KEY_FONT = 'code.fontFamily'
const KEY_SIZE = 'code.fontSize'

function load(key, def) {
  try {
    const v = localStorage.getItem(key)
    return v == null ? def : v
  } catch (e) {
    return def
  }
}
function persist(key, val) {
  try { localStorage.setItem(key, String(val)) } catch (e) { /* 隐私模式下忽略 */ }
}

// 当前字体栈
export const codeFontFamily = ref(load(KEY_FONT, CODE_FONT_OPTIONS[0].value))
// 当前字号
export const codeFontSize = ref(Number(load(KEY_SIZE, 12)) || 12)

watch(codeFontFamily, v => persist(KEY_FONT, v))
watch(codeFontSize, v => persist(KEY_SIZE, v))

// 代码块内联样式：直接绑到 <pre> 等元素上
export const codeStyle = computed(() => ({
  fontFamily: codeFontFamily.value || undefined,
  fontSize: codeFontSize.value + 'px',
}))
