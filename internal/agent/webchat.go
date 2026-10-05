package agent

// 局域网 Web 访问：把桌面端**同一份**前端（frontend/dist，随二进制内嵌）通过内嵌 HTTP
// 服务暴露到局域网，手机 / 其他电脑用浏览器打开即可获得与桌面端完全一致的界面与功能。
//
// 分工：
//   - 本包（agent）：服务生命周期、访问令牌鉴权、对话互斥，以及把 UI 路由挂上来；
//   - 宿主包（main，webui.go）：静态资源托管、Wails 兼容的 RPC 桥（window.go.main.App.*）、
//     事件流桥（window.runtime.EventsOn*，SSE）。前端零改动复用桌面端全部组件。
//
// 安全：Agent 具备文件读写、执行命令、数据库查询能力，必须凭令牌访问；
// 首次 URL 带 ?token=xxx 校验后写入 Cookie 并重定向到干净地址。

import (
	"crypto/subtle"
	_ "embed" // 用于 //go:embed 嵌入网页
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"apitool/internal/util"
)

const (
	defaultWebChatPort = "8090"
	webChatTokenFile   = "agent_webchat_token.txt"
	webChatCookieName  = "apitool_webchat_token"
	// 令牌 Cookie 有效期（天）
	webChatCookieDays = 30
)

// 包级单例状态：所有字段读写必须持有 webMu。
var (
	webMu     sync.Mutex
	webSrv    *http.Server
	webToken  string
	webHost   string
	webBusy   bool // 是否有对话正在执行，防止桌面端与网页端重复提交
	webUI     http.Handler
)

// ---------------- 对外数据结构 ----------------

// WebChatInfo 局域网 Web 访问服务状态（供桌面端展示与复制链接）。
type WebChatInfo struct {
	Running bool   `json:"running"`
	Addr    string `json:"addr"`
	Port    string `json:"port"`
	URL     string `json:"url"`    // http://127.0.0.1:port
	Public  string `json:"public"` // http://局域网IP:port
	Host    string `json:"host"`   // 局域网 IP
	Token   string `json:"token"`
	Link    string `json:"link"` // 带 token 的完整访问链接
}

// WebLockedHTML 令牌无效时返回的提示页（供宿主包复用）。
const WebLockedHTML = `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>需要访问令牌</title>
<style>body{font-family:system-ui,-apple-system,"Microsoft YaHei",sans-serif;display:flex;align-items:center;
justify-content:center;height:100vh;margin:0;background:#f5f6f8;color:#1f2329}
.box{background:#fff;padding:28px 32px;border-radius:12px;box-shadow:0 4px 20px rgba(0,0,0,.08);max-width:420px;text-align:center}
h3{margin:0 0 10px;font-size:17px}p{margin:0;color:#86909c;font-size:13px;line-height:1.7}</style></head>
<body><div class="box"><h3>🔒 需要有效的访问链接</h3>
<p>该地址缺少或已失效的访问令牌。<br>请在桌面端「AI Agent → 📱 局域网」重新开启或重置链接后再访问。</p>
<p style="margin-top:10px">Agent 具备文件读写与命令执行能力，因此必须凭令牌访问。</p></div></body></html>`

// ---------------- 服务生命周期 ----------------

// RegisterWebUI 注册局域网 UI 处理器（静态资源 + RPC + 事件流）。
// 由宿主包在启动时调用（assets 与 App 方法都只在 main 包可用），需在 StartWebChat 之前。
func (m *Manager) RegisterWebUI(h http.Handler) {
	webMu.Lock()
	webUI = h
	webMu.Unlock()
}

// CheckWebToken 校验请求携带的访问令牌（Cookie / X-Agent-Token / URL 参数）。
func (m *Manager) CheckWebToken(r *http.Request) bool { return m.checkWebToken(r) }

// WebTryBusy 抢占「运行中」标记，返回 false 表示已有对话在执行。
func (m *Manager) WebTryBusy() bool { return m.webTryBusy() }

// WebReleaseBusy 释放「运行中」标记。
func (m *Manager) WebReleaseBusy() { m.webReleaseBusy() }

// StartWebChat 启动局域网 Web 访问服务（默认端口 8090），返回访问信息。
func (m *Manager) StartWebChat(port string) (WebChatInfo, error) {
	webMu.Lock()
	defer webMu.Unlock()
	if webSrv != nil {
		return m.webInfoLocked(), nil
	}
	if strings.TrimSpace(port) == "" {
		port = defaultWebChatPort
	}
	m.loadWebTokenLocked()
	ln, err := net.Listen("tcp", "0.0.0.0:"+port)
	if err != nil {
		return WebChatInfo{}, fmt.Errorf("启动局域网访问服务失败: %v（端口 %s 可能被占用）", err, port)
	}
	srv := &http.Server{
		Addr:              ln.Addr().String(),
		Handler:           m.webHandler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	webSrv = srv
	webHost = util.LocalIP()
	go func() {
		if e := srv.Serve(ln); e != nil && e != http.ErrServerClosed {
			log.Println("局域网访问服务异常退出:", e)
		}
	}()
	return m.webInfoLocked(), nil
}

// StopWebChat 停止局域网 Web 访问服务。
func (m *Manager) StopWebChat() error {
	webMu.Lock()
	defer webMu.Unlock()
	if webSrv == nil {
		return nil
	}
	_ = webSrv.Close()
	webSrv = nil
	webHost = ""
	webBusy = false
	return nil
}

// WebChatInfo 返回局域网 Web 访问服务状态。
func (m *Manager) WebChatInfo() WebChatInfo {
	webMu.Lock()
	defer webMu.Unlock()
	return m.webInfoLocked()
}

// ResetWebChatToken 重新生成访问令牌（旧的分享链接立即失效）。
func (m *Manager) ResetWebChatToken() (WebChatInfo, error) {
	token := "wc_" + util.Token()
	webMu.Lock()
	webToken = token
	webMu.Unlock()
	if dir := m.webTokenDir(); dir != "" {
		_ = os.WriteFile(filepath.Join(dir, webChatTokenFile), []byte(token), 0o600)
	}
	return m.WebChatInfo(), nil
}

// webInfoLocked 组装访问信息，调用方需持有 webMu。
func (m *Manager) webInfoLocked() WebChatInfo {
	info := WebChatInfo{Running: webSrv != nil, Host: webHost, Token: m.loadWebTokenLocked()}
	if webSrv == nil {
		return info
	}
	_, port, err := net.SplitHostPort(webSrv.Addr)
	if err != nil {
		return info
	}
	info.Addr = webSrv.Addr
	info.Port = port
	info.URL = "http://127.0.0.1:" + port
	if info.Host == "" {
		info.Host = util.LocalIP()
	}
	info.Public = "http://" + info.Host + ":" + port
	info.Link = info.Public + "/?token=" + info.Token
	return info
}

// webTokenDir 返回令牌持久化目录（应用数据目录）。
func (m *Manager) webTokenDir() string {
	if s := m.host.Store(); s != nil {
		return s.Dir()
	}
	return ""
}

// loadWebTokenLocked 读取或生成持久化令牌，调用方需持有 webMu。
func (m *Manager) loadWebTokenLocked() string {
	if webToken != "" {
		return webToken
	}
	dir := m.webTokenDir()
	if dir != "" {
		if b, err := os.ReadFile(filepath.Join(dir, webChatTokenFile)); err == nil {
			if t := strings.TrimSpace(string(b)); t != "" {
				webToken = t
				return webToken
			}
		}
	}
	webToken = "wc_" + util.Token()
	if dir != "" {
		_ = os.WriteFile(filepath.Join(dir, webChatTokenFile), []byte(webToken), 0o600)
	}
	return webToken
}

// ---------------- HTTP 处理 ----------------

func (m *Manager) webHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		webMu.Lock()
		ok := webSrv != nil
		webMu.Unlock()
		writeWebJSON(w, 200, map[string]interface{}{"ok": ok, "service": "apitool-agent-webui"})
	})
	// 根路径交给宿主注册的 UI 处理器（桌面端同一份前端 + RPC + 事件流）
	mux.HandleFunc("/", m.serveWebUI)
	return mux
}

// serveWebUI 首次带 token 访问时写入 Cookie 并重定向，随后交由 UI 处理器接管。
func (m *Manager) serveWebUI(w http.ResponseWriter, r *http.Request) {
	webMu.Lock()
	h := webUI
	webMu.Unlock()
	if q := strings.TrimSpace(r.URL.Query().Get("token")); q != "" {
		if q != m.WebChatInfo().Token {
			writeWebHTML(w, http.StatusForbidden, WebLockedHTML)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     webChatCookieName,
			Value:    q,
			Path:     "/",
			MaxAge:   webChatCookieDays * 24 * 3600,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
		// 去掉 token 参数后重定向，避免令牌残留在地址栏与浏览器历史中
		target := "/"
		if v := strings.TrimSpace(r.URL.Query().Get("view")); v != "" {
			target += "?view=" + v
		}
		http.Redirect(w, r, target, http.StatusFound)
		return
	}
	if !m.checkWebToken(r) {
		writeWebHTML(w, http.StatusUnauthorized, WebLockedHTML)
		return
	}
	if h == nil {
		writeWebHTML(w, http.StatusServiceUnavailable,
			`<!doctype html><meta charset="utf-8"><p style="font-family:system-ui;padding:24px">局域网 UI 未注册（RegisterWebUI 未调用）。</p>`)
		return
	}
	h.ServeHTTP(w, r)
}

// checkWebToken 依次校验 Cookie、请求头、URL 参数中的令牌。
func (m *Manager) checkWebToken(r *http.Request) bool {
	want := m.WebChatInfo().Token
	if want == "" {
		return false
	}
	match := func(s string) bool {
		return s != "" && subtle.ConstantTimeCompare([]byte(s), []byte(want)) == 1
	}
	if c, err := r.Cookie(webChatCookieName); err == nil && match(c.Value) {
		return true
	}
	if match(r.Header.Get("X-Agent-Token")) {
		return true
	}
	return match(strings.TrimSpace(r.URL.Query().Get("token")))
}

func (m *Manager) webTryBusy() bool {
	webMu.Lock()
	defer webMu.Unlock()
	if webBusy {
		return false
	}
	webBusy = true
	return true
}

func (m *Manager) webReleaseBusy() {
	webMu.Lock()
	webBusy = false
	webMu.Unlock()
}

// ---------------- 小工具 ----------------

func writeWebJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeWebHTML(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(body))
}
