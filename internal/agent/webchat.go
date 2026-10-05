package agent

// 局域网 Web 聊天：把 AI Agent 的会话能力通过内嵌 HTTP 服务暴露到局域网，
// 手机 / 其他电脑用浏览器打开带 token 的地址即可远程对话，所见即桌面端同一份会话。
//
// 设计要点：
//  1. 完全复用 Manager 现有能力（会话读写、RunAgent、工具调用），不重复实现业务逻辑；
//     桌面端与网页端操作同一份数据，一边提问另一边刷新即可看到。
//  2. 必须携带 token 才能访问（首次 URL 带 ?token=xxx，服务端校验后写入 Cookie 并重定向，
//     避免 token 长期残留在地址栏与浏览器历史里）。Agent 具备文件读写、执行命令、
//     数据库查询等能力，绝不能匿名暴露到局域网。
//  3. API Key 只在服务端从应用设置读取并用于请求，绝不下发给网页。

import (
	"crypto/subtle"
	_ "embed" // 用于 //go:embed 嵌入网页
	"encoding/json"
	"fmt"
	"io"
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

//go:embed webchat_page.html
var webchatPage []byte

const (
	defaultWebChatPort = "8090"
	webChatTokenFile   = "agent_webchat_token.txt"
	webChatCookieName  = "apitool_webchat_token"
	// 令牌 Cookie 有效期（天）
	webChatCookieDays = 30
	// 单次请求体上限，防止超大输入拖垮服务
	webChatMaxBody = 1 << 20
	// 状态接口单次返回的最大消息条数（更早的历史在桌面端查看）
	webChatMaxMessages = 300
)

// 包级单例状态：所有字段读写必须持有 webMu。
var (
	webMu      sync.Mutex
	webSrv     *http.Server
	webToken   string
	webHost    string
	webBusy    bool // 是否有对话正在执行，防止重复提交
	webBusyWho string
)

// ---------------- 对外数据结构 ----------------

// WebChatInfo 局域网 Web 聊天服务状态（供桌面端展示与复制链接）。
type WebChatInfo struct {
	Running bool   `json:"running"`
	Addr    string `json:"addr"`
	Port    string `json:"port"`
	URL     string `json:"url"`    // http://127.0.0.1:port
	Public  string `json:"public"` // http://局域网IP:port
	Host    string `json:"host"`   // 局域网 IP
	Token   string `json:"token"`
	Link    string `json:"link"` // 带 token 的完整分享链接
}

// webAppInfo 网页端展示用的应用信息（不含任何密钥）。
type webAppInfo struct {
	Version      string `json:"version"`
	Mode         string `json:"mode"`
	Model        string `json:"model"`
	User         string `json:"user"`
	Skills       int    `json:"skills"`
	Servers      int    `json:"servers"`
	ShowThinking bool   `json:"showThinking"`
	MaxLoops     int    `json:"maxLoops"`
	ContextLimit int    `json:"contextLimit"`
}

// webSessionItem 会话列表项。
type webSessionItem struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	UpdatedAt    string `json:"updatedAt"`
	MessageCount int    `json:"messageCount"`
	TotalTokens  int64  `json:"totalTokens"`
}

// webState 网页端首屏 / 刷新所需的全部状态。
type webState struct {
	App           webAppInfo       `json:"app"`
	Sessions      []webSessionItem `json:"sessions"`
	ActiveSession string           `json:"activeSession"`
	Messages      []AgentMsg       `json:"messages"`
	Usage         TokenUsage       `json:"usage"`
	Running       bool             `json:"running"`
}

// webChatArgs /api/chat 请求体。
type webChatArgs struct {
	Input     string `json:"input"`
	SessionID string `json:"sessionId"`
}

// webSessionArgs /api/session 请求体，action: new/switch/delete/rename/clear。
type webSessionArgs struct {
	Action string `json:"action"`
	ID     string `json:"id"`
	Title  string `json:"title"`
}

// ---------------- 服务生命周期 ----------------

// StartWebChat 启动局域网 Web 聊天服务（默认端口 8090），返回访问信息。
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
		return WebChatInfo{}, fmt.Errorf("启动局域网聊天服务失败: %v（端口 %s 可能被占用）", err, port)
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
			log.Println("局域网聊天服务异常退出:", e)
		}
	}()
	return m.webInfoLocked(), nil
}

// StopWebChat 停止局域网 Web 聊天服务。
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
	webBusyWho = ""
	return nil
}

// WebChatInfo 返回局域网 Web 聊天服务状态。
func (m *Manager) WebChatInfo() WebChatInfo {
	webMu.Lock()
	defer webMu.Unlock()
	return m.webInfoLocked()
}

// ResetWebChatToken 重新生成访问令牌（旧的分享链接立即失效）。
func (m *Manager) ResetWebChatToken() (WebChatInfo, error) {
	webMu.Lock()
	token := "wc_" + util.Token()
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
		writeWebJSON(w, 200, map[string]interface{}{"ok": ok, "service": "apitool-agent-webchat"})
	})
	mux.HandleFunc("/api/ping", m.authWeb(m.handleWebPing))
	mux.HandleFunc("/api/state", m.authWeb(m.handleWebState))
	mux.HandleFunc("/api/chat", m.authWeb(m.handleWebChat))
	mux.HandleFunc("/api/session", m.authWeb(m.handleWebSession))
	mux.HandleFunc("/", m.handleWebPage)
	return mux
}

// authWeb 校验访问令牌，并放开 CORS 方便用 curl / 其他端口的页面调试。
func (m *Manager) authWeb(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Headers", "Content-Type, X-Agent-Token")
		h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(204)
			return
		}
		if !m.checkWebToken(r) {
			writeWebJSON(w, http.StatusUnauthorized, map[string]interface{}{
				"error": "访问令牌无效或已被重置，请向开启者索取新的链接",
			})
			return
		}
		next(w, r)
	}
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

// handleWebPage 托管网页：首次带 token 访问写入 Cookie 后重定向到干净地址。
func (m *Manager) handleWebPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		http.NotFound(w, r)
		return
	}
	want := m.WebChatInfo().Token
	if q := strings.TrimSpace(r.URL.Query().Get("token")); q != "" {
		if q != want {
			writeWebHTML(w, http.StatusForbidden, webChatLockedHTML)
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
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	if !m.checkWebToken(r) {
		writeWebHTML(w, http.StatusUnauthorized, webChatLockedHTML)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(webchatPage)
}

// handleWebPing 轻量状态探测：网页端据此判断是否需要刷新（不返回会话正文）。
func (m *Manager) handleWebPing(w http.ResponseWriter, r *http.Request) {
	d := m.readAgentData()
	count := 0
	updated := ""
	if s := d.activeSession(); s != nil {
		count = len(s.Messages)
		updated = s.UpdatedAt
	}
	webMu.Lock()
	busy := webBusy
	webMu.Unlock()
	writeWebJSON(w, 200, map[string]interface{}{
		"running":       busy,
		"activeSession": d.ActiveSession,
		"messageCount":  count,
		"updatedAt":     updated,
		"sessionCount":  len(d.Sessions),
	})
}

// handleWebState 返回网页端首屏所需的全部状态。
func (m *Manager) handleWebState(w http.ResponseWriter, r *http.Request) {
	writeWebJSON(w, 200, m.buildWebState())
}

// buildWebState 组装网页端状态快照。
func (m *Manager) buildWebState() webState {
	d := m.readAgentData()
	settings := m.host.ReadData().Settings
	st := webState{
		Sessions:      make([]webSessionItem, 0, len(d.Sessions)),
		ActiveSession: d.ActiveSession,
		Usage:         d.Usage,
		Messages:      []AgentMsg{},
	}
	skills, servers := 0, 0
	for _, s := range d.Skills {
		if s.Enabled {
			skills++
		}
	}
	for _, s := range d.Servers {
		if s.Enabled {
			servers++
		}
	}
	user := ""
	for _, u := range d.Users {
		if u.ID == d.Config.CurrentUserID {
			user = u.Name
			break
		}
	}
	st.App = webAppInfo{
		Version:      m.host.AppVersion(),
		Mode:         d.Config.Mode,
		Model:        settings.AIModel,
		User:         user,
		Skills:       skills,
		Servers:      servers,
		ShowThinking: d.Config.ShowThinking,
		MaxLoops:     d.Config.MaxLoops,
		ContextLimit: d.Config.ContextLimit,
	}
	for _, s := range d.Sessions {
		title := s.Title
		if title == "" {
			title = "新会话"
		}
		st.Sessions = append(st.Sessions, webSessionItem{
			ID: s.ID, Title: title, UpdatedAt: s.UpdatedAt,
			MessageCount: len(s.Messages), TotalTokens: s.Usage.TotalTokens,
		})
	}
	if s := d.activeSession(); s != nil {
		msgs := s.Messages
		if len(msgs) > webChatMaxMessages {
			msgs = msgs[len(msgs)-webChatMaxMessages:]
		}
		st.Messages = append(st.Messages, msgs...)
	}
	webMu.Lock()
	st.Running = webBusy
	webMu.Unlock()
	return st
}

// handleWebChat 处理网页端提问：切换会话 → 调用 RunAgent（内部负责落库与 token 累计）。
func (m *Manager) handleWebChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeWebJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "仅支持 POST"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, webChatMaxBody))
	if err != nil {
		writeWebJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "读取请求体失败"})
		return
	}
	var args webChatArgs
	if err := json.Unmarshal(body, &args); err != nil {
		writeWebJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "请求体不是合法 JSON"})
		return
	}
	input := strings.TrimSpace(args.Input)
	if input == "" {
		writeWebJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "请输入内容"})
		return
	}
	if !m.webTryBusy() {
		writeWebJSON(w, http.StatusConflict, map[string]interface{}{"error": "已有对话正在执行（桌面端或另一台设备），请稍候"})
		return
	}
	defer m.webReleaseBusy()

	// 网页端可指定在哪个会话里提问：先切换，保证上下文正确
	if args.SessionID != "" {
		d := m.readAgentData()
		if d.ActiveSession != args.SessionID {
			if err := m.SwitchAgentSession(args.SessionID); err != nil {
				writeWebJSON(w, http.StatusNotFound, map[string]interface{}{"error": "会话不存在"})
				return
			}
		}
	}
	settings := m.host.ReadData().Settings
	res := m.RunAgent(RunAgentArgs{
		Input:   input,
		BaseURL: settings.AIBaseURL,
		APIKey:  settings.AIKey,
		Model:   settings.AIModel,
		Timeout: settings.TimeoutSec,
	})
	// RunAgent 出错时不落库，这里补一条可见的失败消息，方便网页端与桌面端排查
	if res.Error != "" {
		d := m.readAgentData()
		now := time.Now().Format("2006-01-02 15:04:05")
		if s := d.activeSession(); s != nil {
			s.Messages = append(s.Messages,
				AgentMsg{ID: agentID("msg"), Role: "user", Content: input, Time: now},
				AgentMsg{ID: agentID("msg"), Role: "assistant", Content: "⚠️ " + res.Error, Time: now},
			)
			s.UpdatedAt = time.Now().Format(time.RFC3339)
			_ = m.writeAgentData(d)
		}
	}
	st := m.buildWebState()
	reply := AgentMsg{}
	if n := len(st.Messages); n > 0 {
		reply = st.Messages[n-1]
	}
	writeWebJSON(w, 200, map[string]interface{}{
		"ok":    res.Error == "",
		"error": res.Error,
		"reply": reply,
	})
}

// handleWebSession 处理会话级操作：new / switch / delete / rename / clear。
func (m *Manager) handleWebSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeWebJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "仅支持 POST"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, webChatMaxBody))
	if err != nil {
		writeWebJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "读取请求体失败"})
		return
	}
	var args webSessionArgs
	if err := json.Unmarshal(body, &args); err != nil {
		writeWebJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "请求体不是合法 JSON"})
		return
	}
	var opErr error
	switch args.Action {
	case "new":
		args.Title = strings.TrimSpace(args.Title)
		if args.Title == "" {
			args.Title = "新会话"
		}
		args.ID = m.CreateAgentSession(args.Title)
	case "switch":
		opErr = m.SwitchAgentSession(args.ID)
	case "delete":
		opErr = m.DeleteAgentSession(args.ID)
	case "rename":
		title := strings.TrimSpace(args.Title)
		if title == "" {
			title = "新会话"
		}
		opErr = m.RenameAgentSession(args.ID, title)
	case "clear":
		opErr = m.ClearAgentMessages()
	default:
		opErr = fmt.Errorf("不支持的操作: %s", args.Action)
	}
	if opErr != nil {
		writeWebJSON(w, http.StatusBadRequest, map[string]interface{}{"error": opErr.Error()})
		return
	}
	st := m.buildWebState()
	writeWebJSON(w, 200, map[string]interface{}{"ok": true, "activeSession": st.ActiveSession, "sessions": st.Sessions})
}

// webTryBusy 抢占「运行中」标记，返回 false 表示已有对话在执行。
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
	webBusyWho = ""
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
	_, _ = io.WriteString(w, body)
}

// webChatLockedHTML 令牌无效时的提示页。
const webChatLockedHTML = `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>需要访问令牌</title>
<style>body{font-family:system-ui,-apple-system,"Microsoft YaHei",sans-serif;display:flex;align-items:center;
justify-content:center;height:100vh;margin:0;background:#f5f6f8;color:#1f2329}
.box{background:#fff;padding:28px 32px;border-radius:12px;box-shadow:0 4px 20px rgba(0,0,0,.08);max-width:420px;text-align:center}
h3{margin:0 0 10px;font-size:17px}p{margin:0;color:#86909c;font-size:13px;line-height:1.7}
code{background:#f2f3f5;padding:1px 5px;border-radius:4px}</style></head>
<body><div class="box"><h3>🔒 需要有效的访问链接</h3>
<p>该地址缺少或已失效的访问令牌。<br>请在桌面端「AI Agent → 📱 局域网」重新开启或重置链接后再访问。</p>
<p style="margin-top:10px">Agent 具备文件读写与命令执行能力，因此必须凭令牌访问。</p></div></body></html>`
