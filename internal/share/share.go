// Package share 实现分享服务（局域网内托管文档中心 HTML，供他人浏览器查看）、
// 文档构建（HTML/OpenAPI/Markdown 经 doc 包渲染）与测试报告分享。
// 抽离自根目录 share.go，服务状态收归本包，通过 bus.Bus / store.Store 解耦 App。
package share

import (
	"apitool/internal/bus"
	"apitool/internal/doc"
	"apitool/internal/model"
	"apitool/internal/store"
	"apitool/internal/util"

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
)

// ShareServerInfo 分享服务状态，供前端展示与打开
type ShareServerInfo struct {
	Running bool   `json:"running"`
	Addr    string `json:"addr"` // 监听地址 host:port
	Port    string `json:"port"`
	URL     string `json:"url"`    // http://127.0.0.1:port
	Public  string `json:"public"` // http://局域网IP:port（对外）
	Host    string `json:"host"`   // 局域网 IP
}

// ShareItem 单条分享记录（内存态，应用退出即失效）
type ShareItem struct {
	Token      string `json:"token"`
	Title      string `json:"title"`
	HTML       string `json:"-"`
	Password   string `json:"-"`
	HasPassword bool  `json:"hasPassword"`
	ExpireAt   int64  `json:"expireAt"` // 过期 unix 秒，0 表示长期
	CreatedAt  int64  `json:"createdAt"`
}

// ShareItemView 对外暴露的分享项视图（不含 HTML 与明文密码）
type ShareItemView struct {
	Token       string `json:"token"`
	Title       string `json:"title"`
	HasPassword bool   `json:"hasPassword"`
	ExpireAt    int64  `json:"expireAt"`
}

// 包级单例状态：本包以单例服务形式运行（同一进程仅一个分享服务实例）。
// 所有字段的读写都必须持有 shareMu，禁止在锁外直接访问，否则并发写入会触发 panic。
var (
	shareMu    sync.Mutex
	shareSrv   *http.Server
	shareDoc   string // 当前分享的 HTML 文档内容（OpenDoc 单文档模式）
	shareHost  string // 当前对外 host（局域网 IP）
	shareItems = map[string]*ShareItem{} // token -> 分享项（多分享模式）
)

// Start 启动分享服务（默认端口 8082），托管当前文档与多条分享链接（/s/{token}）。
func Start(port string) error {
	shareMu.Lock()
	defer shareMu.Unlock()
	if shareSrv != nil {
		return nil
	}
	if port == "" {
		port = "8082"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		shareMu.Lock()
		doc := shareDoc
		shareMu.Unlock()
		_, _ = w.Write([]byte(doc))
	})
	mux.HandleFunc("/s/", func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.URL.Path, "/s/")
		shareMu.Lock()
		item, ok := shareItems[token]
		shareMu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		if item.ExpireAt != 0 && time.Now().Unix() > item.ExpireAt {
			http.Error(w, "分享链接已过期", http.StatusGone)
			return
		}
		if item.HasPassword {
			pwd := r.URL.Query().Get("pwd")
			if pwd != item.Password {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = w.Write([]byte(passwordPromptHTML))
				return
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(item.HTML))
	})
	addr := "0.0.0.0:" + port
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Addr: ln.Addr().String(), Handler: mux}
	shareSrv = srv
	shareHost = util.LocalIP()
	go func() {
		if e := srv.Serve(ln); e != nil && e != http.ErrServerClosed {
			log.Println("分享服务异常退出:", e)
		}
	}()
	return nil
}

// Stop 停止分享服务
func Stop() error {
	shareMu.Lock()
	defer shareMu.Unlock()
	if shareSrv == nil {
		return nil
	}
	_ = shareSrv.Close()
	shareSrv = nil
	shareDoc = ""
	return nil
}

// Running 分享服务是否在运行
func Running() bool {
	shareMu.Lock()
	defer shareMu.Unlock()
	return shareSrv != nil
}

// Info 返回分享服务信息（含内外网地址）
func Info() ShareServerInfo {
	shareMu.Lock()
	defer shareMu.Unlock()
	info := ShareServerInfo{Running: shareSrv != nil, Host: shareHost}
	if shareSrv != nil {
		_, port, _ := net.SplitHostPort(shareSrv.Addr)
		info.Addr = shareSrv.Addr
		info.Port = port
		info.URL = "http://127.0.0.1:" + port
		if shareHost == "" {
			shareHost = util.LocalIP()
		}
		info.Public = "http://" + shareHost + ":" + port
	}
	return info
}

// scopeToDirID 将前端 scope（all/project/dir）映射为 doc.CollectScope 需要的 dirID。
func scopeToDirID(scope, dirID string) string {
	if scope == "dir" {
		return dirID
	}
	return ""
}

// passwordPromptHTML 是带密码分享的简易密码输入页（GET ?pwd= 提交后跳转回当前地址）。
var passwordPromptHTML = `<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>需要密码</title>
<style>body{font-family:system-ui;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;background:#f5f6f8}
form{background:#fff;padding:28px 32px;border-radius:12px;box-shadow:0 4px 20px rgba(0,0,0,.08)}
input{padding:8px 12px;border:1px solid #dcdfe6;border-radius:6px;font-size:14px;width:220px}
button{padding:8px 18px;border:0;border-radius:6px;background:#165dff;color:#fff;cursor:pointer;margin-left:8px}
h3{margin:0 0 16px;font-size:16px;color:#1f2329}</style></head>
<body><form method="get"><h3>该分享需要访问密码</h3>
<input name="pwd" type="password" placeholder="请输入密码" autofocus/><button type="submit">查看</button></form></body></html>`

// htmlByAPI 构建指定目录/接口范围的 HTML 文档（供多分享链接使用）。
func htmlByAPI(s *store.Store, dirID, apiID string) (title, html string, err error) {
	data := s.GetData()
	title, dirs, apis := doc.CollectScope(data, dirID, apiID)
	if len(apis) == 0 {
		return "", "", fmt.Errorf("所选范围内没有接口")
	}
	idx := store.ActiveProjectIndex(data)
	proj := data.Projects[idx]
	return title, doc.BuildHTML(title, dirID, dirs, apis, proj.Common), nil
}

// BuildHTMLByAPI 导出：按 dirID/apiID 构建 HTML 文档内容。
func BuildHTMLByAPI(s *store.Store, dirID, apiID string) (string, error) {
	_, html, err := htmlByAPI(s, dirID, apiID)
	return html, err
}

// BuildTitleByAPI 导出：返回 dirID/apiID 范围的文档标题。
func BuildTitleByAPI(s *store.Store, dirID, apiID string) (string, error) {
	title, _, err := htmlByAPI(s, dirID, apiID)
	if err != nil {
		return "", err
	}
	return title, nil
}

// CreateShare 创建一条分享链接（内存态，应用退出失效）。
// password 为空表示无需密码；expireMinutes<=0 表示长期有效。
// 返回 (token, link)，link 形如 http://局域网IP:port/s/{token}。
func CreateShare(s *store.Store, dirID, apiID, password string, expireMinutes int) (string, string, error) {
	title, html, err := htmlByAPI(s, dirID, apiID)
	if err != nil {
		return "", "", err
	}
	token := util.GenID()
	item := &ShareItem{
		Token:       token,
		Title:       title,
		HTML:        html,
		Password:    password,
		HasPassword: strings.TrimSpace(password) != "",
		CreatedAt:   time.Now().Unix(),
	}
	if expireMinutes > 0 {
		item.ExpireAt = time.Now().Add(time.Duration(expireMinutes) * time.Minute).Unix()
	}
	shareMu.Lock()
	shareItems[token] = item
	shareMu.Unlock()
	if err := Start(""); err != nil {
		return "", "", err
	}
	info := Info()
	return token, info.Public + "/s/" + token, nil
}

// ListShares 返回当前所有分享链接的视图（不含 HTML 与明文密码）。
func ListShares() []ShareItemView {
	shareMu.Lock()
	defer shareMu.Unlock()
	out := make([]ShareItemView, 0, len(shareItems))
	for _, it := range shareItems {
		out = append(out, ShareItemView{
			Token:       it.Token,
			Title:       it.Title,
			HasPassword: it.HasPassword,
			ExpireAt:    it.ExpireAt,
		})
	}
	return out
}

// StopShare 停止单条分享；若已无分享项且服务仅用于分享，则关闭服务。
func StopShare(token string) error {
	shareMu.Lock()
	delete(shareItems, token)
	empty := len(shareItems) == 0
	shareMu.Unlock()
	if empty {
		return Stop()
	}
	return nil
}

// BuildDoc 构建文档内容（HTML/OpenAPI/Markdown），不启动分享服务。
// projectID 保留以兼容前端调用契约（当前单项目模型下内部未使用）。
func BuildDoc(s *store.Store, scope, projectID, dirID, format string) (string, error) {
	data := s.GetData()
	idx := store.ActiveProjectIndex(data)
	if idx < 0 {
		return "", fmt.Errorf("没有可用的项目")
	}
	proj := data.Projects[idx]
	realDir := scopeToDirID(scope, dirID)
	title, dirs, apis := doc.CollectScope(data, realDir, "")
	if len(apis) == 0 {
		return "", fmt.Errorf("所选范围内没有接口")
	}
	switch format {
	case "openapi":
		return doc.BuildOpenAPI(title, apis, proj.Common)
	case "markdown":
		return doc.BuildMarkdown(title, realDir, dirs, apis, proj.Common), nil
	default:
		return doc.BuildHTML(title, realDir, dirs, apis, proj.Common), nil
	}
}

// Refresh 重新生成当前分享文档并刷新托管内容（不重启服务）。
func Refresh(s *store.Store, scope, projectID, dirID string) error {
	content, err := BuildDoc(s, scope, projectID, dirID, "html")
	if err != nil {
		return err
	}
	shareMu.Lock()
	shareDoc = content
	shareMu.Unlock()
	return nil
}

// OpenDoc 构建文档并（按 openType）内嵌预览 / 复制链接 / 系统浏览器打开。
// 返回实际地址（view=内嵌 html，copy/open=http 链接）。
func OpenDoc(b bus.Bus, s *store.Store, scope, projectID, dirID, openType, format string) (string, error) {
	content, err := BuildDoc(s, scope, projectID, dirID, format)
	if err != nil {
		return "", err
	}
	if format == "openapi" || format == "markdown" {
		// 非 HTML 形式不通过分享服务托管，直接写临时文件并打开/复制
		ext := ".json"
		if format == "markdown" {
			ext = ".md"
		}
		tmp := filepath.Join(os.TempDir(), "apitool-share-"+util.GenID()+ext)
		if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
			return "", err
		}
		switch openType {
		case "copy":
			_ = b.ClipboardSetText(tmp)
			return tmp, nil
		default:
			b.BrowserOpenURL("file://" + tmp)
			return "file://" + tmp, nil
		}
	}
	shareMu.Lock()
	shareDoc = content
	shareMu.Unlock()
	if err := Start(""); err != nil {
		return "", err
	}
	info := Info()
	switch openType {
	case "copy":
		_ = b.ClipboardSetText(info.Public)
		return info.Public, nil
	case "open":
		b.BrowserOpenURL(info.URL)
		return info.URL, nil
	default: // view
		return content, nil
	}
}

// TestReport 分享测试报告（HTML 默认打开分享服务，或按 openType 复制/打开）。
func TestReport(b bus.Bus, s *store.Store, reportJSON, format, openType string) (string, error) {
	var r model.TestReport
	if err := json.Unmarshal([]byte(reportJSON), &r); err != nil {
		return "", fmt.Errorf("报告解析失败: %v", err)
	}
	content := doc.BuildTestReportHTML(r)
	if format == "markdown" {
		content = doc.BuildTestReportMarkdown(r)
	}
	if format == "markdown" {
		tmp := filepath.Join(os.TempDir(), "apitool-report-"+util.GenID()+".md")
		if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
			return "", err
		}
		switch openType {
		case "copy":
			_ = b.ClipboardSetText(tmp)
			return tmp, nil
		default:
			b.BrowserOpenURL("file://" + tmp)
			return "file://" + tmp, nil
		}
	}
	shareMu.Lock()
	shareDoc = content
	shareMu.Unlock()
	if err := Start(""); err != nil {
		return "", err
	}
	info := Info()
	switch openType {
	case "copy":
		_ = b.ClipboardSetText(info.Public)
		return info.Public, nil
	case "open":
		b.BrowserOpenURL(info.URL)
		return info.URL, nil
	default:
		return content, nil
	}
}
