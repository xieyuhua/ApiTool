// ApiTool PWA Service Worker。
//
// 缓存策略：
//   - /assets/*   cache-first（构建产物带内容哈希， immutable）
//   - /icons/*    cache-first
//   - index.html  network-first（保证入口及时更新，离线时回退缓存）
//   - /rpc、/api、/events  network-only（业务数据与实时事件绝不缓存）
//
// 更新策略：self.skipWaiting() + clients.claim()，新 SW 立即接管；
// 前端在 controllerchange 时提示用户刷新。
const CACHE_NAME = 'apitool-cache-v1'
const PRECACHE = ['/', '/manifest.webmanifest']

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME).then((c) => c.addAll(PRECACHE)).then(() => self.skipWaiting())
  )
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(keys.filter((k) => k !== CACHE_NAME).map((k) => caches.delete(k)))
    ).then(() => self.clients.claim())
  )
})

self.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url)
  if (event.request.method !== 'GET') return

  // 业务请求绝不缓存
  if (url.pathname.startsWith('/rpc/') || url.pathname.startsWith('/api/') || url.pathname === '/events') {
    return
  }

  // 带哈希的构建产物与图标：cache-first
  if (url.pathname.startsWith('/assets/') || url.pathname.startsWith('/icons/')) {
    event.respondWith(
      caches.match(event.request).then((hit) => hit || fetch(event.request).then((res) => {
        const copy = res.clone()
        caches.open(CACHE_NAME).then((c) => c.put(event.request, copy))
        return res
      }))
    )
    return
  }

  // 入口 HTML：network-first
  if (url.pathname === '/' || url.pathname === '/index.html') {
    event.respondWith(
      fetch(event.request).then((res) => {
        const copy = res.clone()
        caches.open(CACHE_NAME).then((c) => c.put(event.request, copy))
        return res
      }).catch(() => caches.match('/'))
    )
  }
})
