// 局域网「Agent 对话网页」独立入口。
//
// 与桌面端共用**同一套**组件（AgentChat.vue 及其子组件），因此界面与功能完全一致；
// 但入口独立：不挂载 App.vue 的导航与其它模块，网页端只做「AI Agent 对话」这一件事，
// 接口文档 / 抓包 / 测试等页面在局域网端不存在。
//
// 数据最小化：只向后端取设置（AI 接口配置）与数据库连接列表（App.GetAgentBootstrap），
// 不加载接口文档、测试用例等业务数据。

import { createApp } from 'vue'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import 'element-plus/theme-chalk/dark/css-vars.css'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import './style.css'
// 移动端适配（仅网页端引入，桌面端不受影响）：覆盖 Element Plus 弹窗/输入框等
// Teleport 到 body 的组件样式，组件内布局适配见 AgentChat.vue 的 @media。
import './agent-mobile.css'
import { installWebBridge } from './httpBridge'
import { store, initAgentWebStore, applyScheme } from './store'
import AgentChat from './components/agent/AgentChat.vue'

// 桌面端：window.go 由 Wails 注入；局域网网页：安装 HTTP/SSE 兼容桥端
installWebBridge()
// 标记为网页端：移动端样式（agent-mobile.css）以 body.web-agent 限定作用域
document.body.classList.add('web-agent')

const app = createApp(AgentChat)
app.use(ElementPlus, { locale: zhCn })
app.mount('#app')

// 拉取最小数据后套用与桌面端一致的主题（明暗 / 主题方案 / 主色）
initAgentWebStore()
  .then(() => applyScheme())
  .catch((e) => console.error('初始化 Agent 网页端数据失败', e))

// 主题为「跟随系统」时，系统切换深浅色要实时生效（与桌面端行为一致）
try {
  if (window.matchMedia) {
    window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
      if ((store.data.settings.theme || 'light') === 'auto') applyScheme()
    })
  }
} catch (e) { /* 旧浏览器忽略 */ }
