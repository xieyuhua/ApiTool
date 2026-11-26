package agent

// 内嵌前端 UI：复用桌面端的 Agent 页面（frontend/dist/agent.html 及其资源）。
//
// 构建前置：`build-agent.sh` 会把 frontend/dist 复制到 agent/webui/dist，
// 再编译二进制。若未复制，这里给出可读的降级页而不是静默 404。

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:webui/dist
var uiFS embed.FS

// mountUI 注册静态资源与 SPA 兜底。
func (s *Server) mountUI(m *http.ServeMux) {
	sub, err := fs.Sub(uiFS, "webui/dist")
	if err != nil {
		sub = nil
	}
	var fileServer http.Handler
	if sub != nil {
		// 注意：必须以 sub（已切到 webui/dist）为根，否则 /assets/x 会去找
		// webui/dist/assets/x 之外的路径而 404。
		fileServer = http.FileServer(http.FS(sub))
	}

	m.Handle("/", s.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		// API 路径已由更精确的模式匹配，这里只处理静态资源
		if strings.HasPrefix(path, "/api/") {
			http.NotFound(w, r)
			return
		}
		switch {
		case strings.HasPrefix(path, "/assets/"):
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			fileServer.ServeHTTP(w, r)
		case path == "/" || path == "/agent.html" || path == "/index.html":
			w.Header().Set("Cache-Control", "no-store")
			servePage(w, sub, "agent.html")
		default:
			// 其余路径一律回 agent.html（单页应用，无前端路由）
			w.Header().Set("Cache-Control", "no-store")
			servePage(w, sub, "agent.html")
		}
	})))
}

// servePage 输出嵌入的 HTML，并在 <head> 注入独立服务标记，
// 供前端 agent-main.js 识别并安装 REST/SSE 桥接。
func servePage(w http.ResponseWriter, sub fs.FS, name string) {
	if sub == nil {
		writeNotBuilt(w)
		return
	}
	b, err := fs.ReadFile(sub, name)
	if err != nil {
		writeNotBuilt(w)
		return
	}
	html := strings.Replace(string(b), "</head>", standaloneTag+"</head>", 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}

// standaloneTag 告诉前端：当前运行在独立 Agent 服务中。
const standaloneTag = `<script>window.__AGENT_STANDALONE__=true</script>`

// writeNotBuilt 前端产物缺失时的可读提示页。
func writeNotBuilt(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8">
<body style="font-family:system-ui;padding:32px;line-height:1.7">
<h3>网页端未构建</h3>
<p>未找到 <code>agent/webui/dist/agent.html</code>。</p>
<p>请在仓库根目录执行 <code>./build-agent.sh</code>（或先 <code>cd frontend &amp;&amp; npm run build</code>，
再把 <code>frontend/dist</code> 复制到 <code>agent/webui/dist</code>），然后重新编译。</p>
<p>API 不受影响：<code>/api/agent/*</code> 仍可直接调用。</p></body>`))
}
