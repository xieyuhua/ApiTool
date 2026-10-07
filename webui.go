package main

// 局域网 Web UI：把桌面端同一份前端（frontend/dist，已由 go:embed 内嵌）托管到局域网
// HTTP 服务上，并用 HTTP / SSE 替代 Wails IPC，从而让网页端**零改动**复用桌面端全部组件：
//
//   1) 静态资源：/assets/*、/ 等由 embed FS 直接提供，SPA 路由回退到 index.html；
//   2) RPC 桥：前端 wailsjs/go/main/App.js 里的每个方法最终都调用 window.go.main.App.X，
//      这里用反射调用 App 的导出方法并返回 JSON，协议保持一致；
//   3) 事件桥：前端 wailsjs/runtime/runtime.js 调用 window.runtime.EventsOn*，
//      这里用 SSE(/events) 推送 App.Emit 的事件，实现流式输出 / 思考过程 / 工具卡片 / 日志。
//
// 前端因此不需要任何为局域网而做的改动，桌面端与网页端像素级一致、功能相同。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"apitool/internal/agent"
)

// ---------------- SSE 订阅者 ----------------

type webSubscriber struct {
	ch chan []byte
}

var (
	webSubMu sync.Mutex
	webSubs  = map[*webSubscriber]struct{}{}
)

// webBroadcast 把事件推送给所有局域网网页端订阅者（桌面端窗口仍由 Wails 自身推送）。
// data 与 Wails EventsEmit 保持一致：单参数直接作为负载，多参数组成数组。
func webBroadcast(event string, data ...interface{}) {
	var payload interface{}
	switch len(data) {
	case 0:
		payload = nil
	case 1:
		payload = data[0]
	default:
		payload = data
	}
	msg, err := json.Marshal(map[string]interface{}{"event": event, "data": payload})
	if err != nil {
		return
	}
	webSubMu.Lock()
	defer webSubMu.Unlock()
	for s := range webSubs {
		select {
		case s.ch <- msg:
		default: // 订阅者消费不过来（网络慢）时丢弃，避免阻塞业务 goroutine
		}
	}
}

// ---------------- UI 处理器 ----------------

// webUIHandler 组装局域网 UI 的 HTTP 处理器：鉴权 + 静态资源 + RPC + 事件流。
func (a *App) webUIHandler() http.Handler {
	sub, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		fmt.Println("局域网 UI: frontend/dist 不可用:", err)
		sub = nil
	}
	fileServer := http.FileServer(http.FS(sub))

	mux := http.NewServeMux()
	mux.HandleFunc("/rpc/", a.webRPCHandler)
	mux.HandleFunc("/events", a.webEventsHandler)
	// 导出文件下载（局域网网页端点击导出结果时使用；桌面端走本地打开）
	mux.HandleFunc("/export/download", a.webExportDownload)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if sub == nil {
			http.Error(w, "frontend assets missing", http.StatusInternalServerError)
			return
		}
		switch {
		case strings.HasPrefix(r.URL.Path, "/assets/"):
			// 构建产物带内容哈希，可长缓存
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			fileServer.ServeHTTP(w, r)
		case r.URL.Path == "/" || r.URL.Path == "/index.html" || r.URL.Path == "/agent.html":
			// 只提供 Agent 对话页（访问 / 或 /agent.html 都给这一页）
			w.Header().Set("Cache-Control", "no-store")
			serveAgentPage(w, sub)
		default:
			// 其余路径一律不提供：主应用的其它页面（接口调试 / 抓包 / 测试 / 设置等）
			// 在局域网端不存在，避免整个应用被暴露
			http.NotFound(w, r)
		}
	})
	return a.webAuth(mux)
}

// serveAgentPage 返回 Agent 对话页 HTML（Vite 多页构建的独立入口）。
func serveAgentPage(w http.ResponseWriter, sub fs.FS) {
	page, err := fs.ReadFile(sub, "agent.html")
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8"><body style="font-family:system-ui;padding:32px;line-height:1.8">
			<h3>Agent 网页端未构建</h3><p>未找到 <code>dist/agent.html</code>，请在 frontend 目录执行 <code>npm run build</code> 后重新构建程序。</p></body>`))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(page)
}

// webAuth 校验令牌并放开 CORS（方便用 curl / 其它端口页面调试）。
func (a *App) webAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Headers", "Content-Type, X-Agent-Token")
		h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(204)
			return
		}
		if !a.CheckWebToken(r) {
			if strings.HasPrefix(r.URL.Path, "/rpc/") || r.URL.Path == "/events" {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"访问令牌无效或已被重置，请向开启者索取新的链接"}`))
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(agent.WebLockedHTML))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// webRPCHandler 以 JSON 数组为参数调用 App 的导出方法（等价于 Wails IPC 的调用面）。
// POST /rpc/{Method}  body: [args...]   ->  {"ok":true,"result":...} / {"ok":false,"error":"..."}
func (a *App) webRPCHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeWebJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"ok": false, "error": "仅支持 POST"})
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/rpc/")
	if msg, blocked := webRPCBlocked[name]; blocked {
		writeWebJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": msg})
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		writeWebJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "读取请求体失败"})
		return
	}
	var args []json.RawMessage
	if len(strings.TrimSpace(string(raw))) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			// 容错：允许直接传单个对象（无参数数组）
			args = []json.RawMessage{json.RawMessage(raw)}
		}
	}
	result, err := a.callWebMethod(name, args)
	if err != nil {
		writeWebJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	writeWebJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "result": result})
}

// webRPCBlocked 列出无法在网页端生效的桌面专属方法，返回可读提示而非晦涩报错。
var webRPCBlocked = map[string]string{
	"SaveFileDialog":        "网页端无法弹出系统保存对话框，请回到桌面端使用该功能",
	"OpenFileDialog":        "网页端无法弹出系统文件对话框，请回到桌面端使用该功能",
	"OpenDirectoryDialog":   "网页端无法弹出系统目录对话框，请回到桌面端使用该功能",
	"WindowShow":            "该操作仅适用于桌面端窗口",
	"WindowHide":            "该操作仅适用于桌面端窗口",
	"WindowUnminimise":      "该操作仅适用于桌面端窗口",
	"WindowCenter":          "该操作仅适用于桌面端窗口",
	"WindowSetAlwaysOnTop":  "该操作仅适用于桌面端窗口",
	"Quit":                  "该操作仅适用于桌面端",
	"GetDataFilePath":       "该操作仅适用于桌面端",
	"CloseClipboardWindow":  "该操作仅适用于桌面端",
	"ToggleClipboardWindow": "该操作仅适用于桌面端",
}

// callWebMethod 反射调用 App 的导出方法，处理 (T, error) / (T) / (error) / () 几种签名。
func (a *App) callWebMethod(name string, args []json.RawMessage) (result interface{}, err error) {
	defer func() {
		// 任何 panic 都转成错误，避免单个调用打挂 HTTP 连接
		if rec := recover(); rec != nil {
			result, err = nil, fmt.Errorf("调用 %s 失败: %v", name, rec)
		}
	}()
	m := reflect.ValueOf(a).MethodByName(name)
	if !m.IsValid() {
		return nil, fmt.Errorf("方法不存在: %s", name)
	}
	mt := m.Type()
	// 依赖窗口上下文的内部方法（带 context.Context 参数）不对网页开放
	if mt.NumIn() > 0 && mt.In(0) == reflect.TypeOf((*context.Context)(nil)).Elem() {
		return nil, fmt.Errorf("方法 %s 仅适用于桌面端", name)
	}
	if mt.NumIn() != len(args) {
		return nil, fmt.Errorf("参数个数不匹配: %s 需要 %d 个，收到 %d 个", name, mt.NumIn(), len(args))
	}
	in := make([]reflect.Value, mt.NumIn())
	for i := 0; i < mt.NumIn(); i++ {
		v := reflect.New(mt.In(i))
		if len(args[i]) > 0 {
			if err := json.Unmarshal(args[i], v.Interface()); err != nil {
				return nil, fmt.Errorf("参数 %d 解析失败: %v", i+1, err)
			}
		}
		in[i] = v.Elem()
	}
	// 对话互斥：桌面端正在跑 Agent 时，网页端不再重复提交
	if name == "RunAgent" {
		if !a.WebTryBusy() {
			return nil, fmt.Errorf("已有对话正在执行（桌面端或另一台设备），请稍候")
		}
		defer a.WebReleaseBusy()
	}
	out := m.Call(in)
	// 返回值处理：剥离尾部的 error（Go 惯例 (T, error) / (T1, T2, error)），剩余按数量返回。
	//
	// 这里必须按「类型是否为 error」判断，而不能写 `o.Interface().(error)`：
	// 当 error 返回值为 nil 时，o.Interface() 是一个 **nil interface**，
	// 对它断言 error 得到 ok=false，于是 nil 会被当成正常返回值追加进去，
	// (T, error) 变成 [T, nil] 两个元素，进而走多返回值分支返回数组 ——
	// 前端拿到的就是数组（日志详情等场景表现为「无详情」），与桌面端行为不一致。
	errType := reflect.TypeOf((*error)(nil)).Elem()
	n := len(out)
	for n > 0 && out[n-1].Type() == errType {
		if !out[n-1].IsNil() {
			return nil, out[n-1].Interface().(error)
		}
		n--
	}
	res := make([]interface{}, 0, n)
	for i := 0; i < n; i++ {
		res = append(res, out[i].Interface())
	}
	switch len(res) {
	case 0:
		return nil, nil
	case 1:
		return res[0], nil
	default:
		return res, nil
	}
}

// webEventsHandler 以 SSE 推送 App.Emit 的事件（流式输出 / 思考过程 / 工具卡片 / 日志等）。
func (a *App) webEventsHandler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	sub := &webSubscriber{ch: make(chan []byte, 256)}
	webSubMu.Lock()
	webSubs[sub] = struct{}{}
	webSubMu.Unlock()
	defer func() {
		webSubMu.Lock()
		delete(webSubs, sub)
		webSubMu.Unlock()
	}()

	// 提示客户端连接已建立，立即重连的客户端也能马上收到
	_, _ = w.Write([]byte(": connected\n\n"))
	flusher.Flush()

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-sub.ch:
			if _, err := w.Write(append([]byte("data: "), append(msg, '\n', '\n')...)); err != nil {
				return
			}
			flusher.Flush()
		case <-ping.C:
			// 心跳注释行，保持连接不被中间设备回收
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeWebJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
