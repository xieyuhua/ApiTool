import { createApp } from 'vue'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import 'element-plus/theme-chalk/dark/css-vars.css'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import App from './App.vue'
import './style.css'
import { installWebBridge } from './httpBridge'
import { installV3BridgeWhenReady } from './wailsV3Bridge'

// 桥接安装顺序（关键）：
//
// Wails v3 的 runtime.js 由 WebView 异步注入，进入本文件时 window.wails 可能
// 还没出现。若此时直接判定「没有运行时」而安装局域网 HTTP/SSE 桥，就会把
// 真正的 v3 绑定覆盖掉（所有后端调用 404）。
//
// 因此这里先等 v3 就绪：
//   - v3 就绪：安装兼容桥，把 v3 的 Call/Events 映射回 v2 的 window.go/window.runtime，
//     现有 wailsjs 封装与 19 个业务组件零改动；
//   - 超时仍无 v3（浏览器预览 / 局域网网页）：回退安装 HTTP/SSE 桥。
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
function installSafeProxyFallback() {
  if (!window.go || !window.go.main || !window.go.main.App) {
    const safe = makeSafeProxy(3) // main -> App -> method
    window.go = window.go || {}
    window.go.main = window.go.main || {}
    window.go.main.App = safe
  }
}

installV3BridgeWhenReady().then((v3Ready) => {
  if (v3Ready) {
    console.info('[wails] 已启用 Wails v3 兼容桥')
  } else if (installWebBridge()) {
    console.info('[webui] 已启用局域网 Web 桥接（HTTP + SSE）')
  }
  installSafeProxyFallback()

  const app = createApp(App)
  app.use(ElementPlus, { locale: zhCn })
  app.mount('#app')
})
