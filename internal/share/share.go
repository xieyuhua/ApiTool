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
	"sync"
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

var (
	shareMu   sync.Mutex
	shareSrv  *http.Server
	shareDoc  string // 当前分享的 HTML 文档内容
	shareHost string // 当前对外 host（局域网 IP）
)

// Start 启动分享服务（默认端口 8082），仅托管当前文档，不承载其他业务。
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
