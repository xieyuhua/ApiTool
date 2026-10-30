import {defineConfig} from 'vite'
import vue from '@vitejs/plugin-vue'

// https://vitejs.dev/config/
const buildStamp = new Date().toLocaleString('zh-CN', { hour12: false })

export default defineConfig({
  plugins: [vue()],
  define: {
    // 构建时间戳：界面底部展示，便于确认当前运行的是哪次前端构建
    __BUILD_STAMP__: JSON.stringify(buildStamp),
  },
  build: {
    rollupOptions: {
      input: {
        // 桌面端主应用（Wails 窗口内加载）
        main: 'index.html',
        // 局域网「Agent 对话网页」独立入口：只挂载 AgentChat，不含导航与其它模块
        agent: 'agent.html',
      },
    },
  },
})
