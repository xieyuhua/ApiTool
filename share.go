package main

import (
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ShareInfo 分享链接信息（返回给前端展示）
type ShareInfo struct {
	Token      string `json:"token"`
	Title      string `json:"title"`
	HasPassword bool  `json:"hasPassword"`
	ExpireAt   int64  `json:"expireAt"` // 0 表示不失效
	CreatedAt  int64  `json:"createdAt"`
	Link       string `json:"link"`
}

type shareDoc struct {
	token     string
	title     string
	html      string
	password  string
	createdAt time.Time
	expireAt  time.Time
}

type shareServer struct {
	mu       sync.Mutex
	docs     map[string]*shareDoc
	server   *http.Server
	listener net.Listener
	port     int
}

var shareSrv = &shareServer{docs: map[string]*shareDoc{}}

// ensure 懒启动内嵌 HTTP 服务（绑定随机端口，监听 0.0.0.0 以支持局域网访问）
func (s *shareServer) ensure() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server != nil {
		return nil
	}
	// 优先监听 0.0.0.0（IPv4 通配，支持局域网）；若失败（如被防火墙拦截）回退 127.0.0.1（仅本机）
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return err
		}
	}
	s.listener = ln
	s.port = ln.Addr().(*net.TCPAddr).Port
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handle)
	s.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go s.server.Serve(ln)
	return nil
}

func (s *shareServer) handle(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("t")
	if token == "" {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) >= 2 && parts[0] == "t" {
			token = parts[1]
		}
	}

	s.mu.Lock()
	doc, ok := s.docs[token]
	s.mu.Unlock()
	if !ok {
		http.Error(w, "链接无效或已失效", http.StatusNotFound)
		return
	}
	if !doc.expireAt.IsZero() && time.Now().After(doc.expireAt) {
		s.mu.Lock()
		delete(s.docs, token)
		s.mu.Unlock()
		http.Error(w, "链接已过期", http.StatusGone)
		return
	}

	if doc.password != "" {
		_, pass, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(pass), []byte(doc.password)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="ApiTool 分享文档"`)
			http.Error(w, "需要访问密码", http.StatusUnauthorized)
			return
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(doc.html))
}

// buildHTMLForScope 生成指定范围的 HTML 文档内容
func (a *App) buildHTMLForScope(dirID, apiID string) (string, string, error) {
	data := a.readData()
	title, dirs, apis := a.collectScope(data, dirID, apiID)
	if len(apis) == 0 {
		return "", "", fmt.Errorf("所选范围内没有接口")
	}
	rootID := ""
	if apiID == "" {
		rootID = dirID
	}
	// 文档标题附加项目名称
	if idx := activeProjectIndex(data); idx >= 0 {
		title = data.Projects[idx].Name + " / " + title
	}
	return buildHTML(title, rootID, dirs, apis), title, nil
}

// BuildSharedHTML 生成可独立分享的网页文档（含内联样式的完整 HTML 源码）。
// 返回的内容不依赖任何服务端，可直接保存为 .html 文件或粘贴部署，发给任何人都能打开。
func (a *App) BuildSharedHTML(dirID, apiID string) (string, error) {
	html, _, err := a.buildHTMLForScope(dirID, apiID)
	if err != nil {
		return "", err
	}
	return html, nil
}

// CreateShareLink 创建分享链接（内嵌服务托管文档，支持密码与有效期）
func (a *App) CreateShareLink(dirID, apiID, password string, expireMinutes int) (string, error) {
	html, title, err := a.buildHTMLForScope(dirID, apiID)
	if err != nil {
		return "", err
	}
	if err := shareSrv.ensure(); err != nil {
		return "", fmt.Errorf("启动分享服务失败: %v", err)
	}
	token := strings.ReplaceAll(uuid.NewString()[:12], "-", "")
	expire := time.Time{}
	if expireMinutes > 0 {
		expire = time.Now().Add(time.Duration(expireMinutes) * time.Minute)
	}
	shareSrv.mu.Lock()
	shareSrv.docs[token] = &shareDoc{
		token:     token,
		title:     title,
		html:      html,
		password:  strings.TrimSpace(password),
		createdAt: time.Now(),
		expireAt:  expire,
	}
	shareSrv.mu.Unlock()
	// 注意：服务监听 0.0.0.0（仅 IPv4），故链接必须用 127.0.0.1 而非 localhost，
	// 否则 localhost 在部分系统优先解析为 IPv6 ::1 而连不上服务（页面空白）。
	return fmt.Sprintf("http://127.0.0.1:%d/?t=%s", shareSrv.port, token), nil
}

// ListShares 列出当前有效的分享链接
func (a *App) ListShares() []ShareInfo {
	shareSrv.mu.Lock()
	defer shareSrv.mu.Unlock()
	var out []ShareInfo
	for _, d := range shareSrv.docs {
		exp := int64(0)
		if !d.expireAt.IsZero() {
			exp = d.expireAt.Unix()
		}
		out = append(out, ShareInfo{
			Token:      d.token,
			Title:      d.title,
			HasPassword: d.password != "",
			ExpireAt:   exp,
			CreatedAt:  d.createdAt.Unix(),
			Link:       fmt.Sprintf("http://127.0.0.1:%d/?t=%s", shareSrv.port, d.token),
		})
	}
	return out
}

// StopShare 停止某个分享链接
func (a *App) StopShare(token string) error {
	shareSrv.mu.Lock()
	defer shareSrv.mu.Unlock()
	delete(shareSrv.docs, token)
	return nil
}
