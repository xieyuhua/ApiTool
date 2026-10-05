import {defineConfig} from 'vite'
import vue from '@vitejs/plugin-vue'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [vue()],
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
