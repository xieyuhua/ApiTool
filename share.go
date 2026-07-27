package main

import (
	"crypto/subtle"
	"encoding/json"
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
	host     string // 对外可访问 host：监听通配地址(0.0.0.0/[::]/空)或 127.0.0.1 回退时，均取本机局域网 IP
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
	// 解析对外可访问 host：通配地址（0.0.0.0/[::]）用本机局域网 IP，否则用实际监听 host
	if h, _, err := net.SplitHostPort(ln.Addr().String()); err == nil {
		if h == "" || h == "0.0.0.0" || h == "[::]" || h == "::" {
			s.host = localIP()
		} else {
			s.host = h
		}
	} else {
		s.host = "127.0.0.1"
	}
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

// buildHTMLForScope 生成指定范围的分享 HTML 文档。
// 返回值依次为 (HTML 内容, 标题, 错误)；标题已附加项目名称（项目名 / 目录或接口名）。
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
	common := CommonParams{}
	if idx := activeProjectIndex(data); idx >= 0 {
		title = data.Projects[idx].Name + " / " + title
		common = data.Projects[idx].Common
	}
	return buildHTML(title, rootID, dirs, apis, common), title, nil
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

// BuildSharedTitle 返回分享文档的标题（项目名 / 目录或接口名）
func (a *App) BuildSharedTitle(dirID, apiID string) (string, error) {
	_, title, err := a.buildHTMLForScope(dirID, apiID)
	return title, err
}

// shareContent 将一段 HTML 文档托管为分享链接（支持密码与有效期），返回可访问 URL
func (a *App) shareContent(html, title, password string, expireMinutes int) (string, error) {
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
	return fmt.Sprintf("http://%s:%d/?t=%s", shareSrv.host, shareSrv.port, token), nil
}

// CreateShareLink 创建分享链接（内嵌服务托管文档，支持密码与有效期）
func (a *App) CreateShareLink(dirID, apiID, password string, expireMinutes int) (string, error) {
	html, title, err := a.buildHTMLForScope(dirID, apiID)
	if err != nil {
		return "", err
	}
	return a.shareContent(html, title, password, expireMinutes)
}

// ShareTestReport 将测试报告（JSON）渲染为 HTML 并托管为分享链接，接入「文档中心」分享能力。
func (a *App) ShareTestReport(reportJSON, password string, expireMinutes int) (string, error) {
	var r TestReport
	if err := json.Unmarshal([]byte(reportJSON), &r); err != nil {
		return "", fmt.Errorf("报告解析失败: %v", err)
	}
	title := "测试报告 - " + r.PlanName
	return a.shareContent(reportHTML(r), title, password, expireMinutes)
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
			Link:       fmt.Sprintf("http://%s:%d/?t=%s", shareSrv.host, shareSrv.port, d.token),
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
