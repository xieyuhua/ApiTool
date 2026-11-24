package app

import (
	"io/fs"
	"net/http"
	"strings"
)

// mountStatic 注册网页端静态资源与 SPA 回退。
// 静态资源由宿主注入（New 时传入 embed.FS）：go:embed 不能跨上级目录引用，
// 服务端入口位于仓库根（cmd/），由它 embed frontend/dist 后传入。
func (s *Server) mountStatic(m *http.ServeMux) {
	if s.assets == nil {
		return // 纯 API 服务端（无网页端资源）
	}
	sub, err := fs.Sub(s.assets, "frontend/dist")
	if err != nil {
		return
	}
	fileServer := http.FileServer(http.FS(sub))
	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.HasPrefix(path, "/assets/"):
			// 构建产物带内容哈希，长缓存
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			fileServer.ServeHTTP(w, r)
		case path == "/" || path == "/index.html" || path == "/agent.html":
			w.Header().Set("Cache-Control", "no-store")
			servePage(w, sub, "index.html")
		case path == "/manifest.webmanifest" || path == "/sw.js" || strings.HasPrefix(path, "/icons/"):
			w.Header().Set("Cache-Control", "no-cache")
			fileServer.ServeHTTP(w, r)
		default:
			// SPA 回退：其余路径一律回 index.html（前端无路由，仅兜底刷新）
			w.Header().Set("Cache-Control", "no-store")
			servePage(w, sub, "index.html")
		}
	})
}

// servePage 输出嵌入的 HTML。
func servePage(w http.ResponseWriter, sub fs.FS, name string) {
	b, err := fs.ReadFile(sub, name)
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8"><body style="font-family:system-ui;padding:32px">
			<h3>网页端未构建</h3><p>未找到 <code>frontend/dist/` + name + `</code>，请在 frontend 目录执行 <code>npm run build</code> 后重新构建服务端。</p></body>`))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(b)
}
