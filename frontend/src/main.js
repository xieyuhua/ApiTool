import { createApp } from 'vue'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import App from './App.vue'
import './style.css'

// Wails 运行时会在桌面端把 window.go.main.App.* 注入为真实绑定。
// 浏览器预览（npm run dev）或桥接尚未就绪时，window.go / main / App 可能缺失，
// 直接访问会抛 "Cannot read properties of undefined (reading 'main')"。
// 这里用深层 Proxy 兜底：任意层级缺失都返回「友好报错」，绝不崩溃。
function makeSafeProxy(depth) {
  const noop = () => Promise.reject(new Error('当前环境未连接到 Go 后端，请用桌面端（wails build / wails dev）运行本程序'))
  if (depth <= 0) return noop
  return new Proxy({}, {
    get(_, prop) {
      if (prop === 'then') return undefined // 避免被当作 thenable
      return makeSafeProxy(depth - 1)
    },
  })
}
const safe = makeSafeProxy(3) // main -> App -> method
if (!window.go || !window.go.main || !window.go.main.App) {
  window.go = window.go || {}
  window.go.main = window.go.main || {}
  window.go.main.App = safe
}

const app = createApp(App)
app.use(ElementPlus, { locale: zhCn })
app.mount('#app')
