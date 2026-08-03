// Package sync 提供内置同步服务（客户端进程内启动，复用 server 包）。
// 抽离自根目录 syncserver.go，消除对 App 内部字段（syncDir）的依赖。
package sync

import (
	"fmt"
	"net"
	"net/http"

	"apitool/internal/util"
	"apitool/server"
)

// defaultSyncAddr 内置同步服务默认监听地址（端口 8080）。
// 升级检测（DefaultUpdateURL）复用此常量，避免端口硬编码散落多处。
const DefaultSyncAddr = ":8080"

var (
	syncSrv        *http.Server
	syncStore      *server.Store
	syncShareToken string
)

// Start 在客户端进程内启动内置同步服务（客户端与服务一起部署时使用）。
// syncDir 为同步数据存储目录。返回实际监听地址（host:port），便于前端展示。
func Start(syncDir string) (string, error) {
	if syncSrv != nil {
		return syncSrv.Addr, nil
	}
	s := server.NewStore(syncDir)
	syncStore = s
	syncShareToken = s.ShareToken() // 取本地分享 token，供分享文档托管到本服务
	// 先用 net.Listen 拿到真实监听地址（尤其 addr 含 :0 随机端口，或 :8080 会被系统解析为 [::]:8080）
	ln, err := net.Listen("tcp", DefaultSyncAddr)
	if err != nil {
		return "", fmt.Errorf("启动同步服务失败: %v", err)
	}
	srv := &http.Server{Addr: ln.Addr().String(), Handler: s.Handler()}
	syncSrv = srv
	go func() {
		_ = srv.Serve(ln)
	}()
	return ln.Addr().String(), nil
}

// Stop 停止内置同步服务并释放监听资源。
func Stop() error {
	if syncSrv == nil {
		return nil
	}
	_ = syncSrv.Close()
	syncSrv = nil
	return nil
}

// Running 返回内置同步服务是否正在运行。
func Running() bool {
	return syncSrv != nil
}

// URL 返回内置同步服务实际可访问的完整地址（http://IP:port）。
// 绑定到 0.0.0.0 / [::] / 空 host 时，用本机局域网 IP 拼出别人能连的地址。
func URL() string {
	if syncSrv == nil {
		return ""
	}
	addr := syncSrv.Addr
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	if host == "" || host == "0.0.0.0" || host == "[::]" || host == "::" {
		host = util.LocalIP()
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%s", host, port)
}

// ShareBackend 描述用于托管分享文档的内置同步服务
type ShareBackend struct {
	URL       string `json:"url"`       // 本机回环地址（http://127.0.0.1:port），前端 fetch 用，不受防火墙拦截
	PublicURL string `json:"publicUrl"` // 对外地址（http://局域网IP:port），供他人在浏览器打开 /s/ 链接
	Token     string `json:"token"`
	Running   bool   `json:"running"`
}

// ShareBackendInfo 返回内置同步服务作为分享后端的地址与 token（仅在其运行时有效）。
// URL 使用 127.0.0.1 回环，避免前端 webview 以局域网 IP fetch 时被本机防火墙拦截；
// PublicURL 使用局域网 IP，便于复制给同网段其他设备打开。无需再到「云同步」填写地址并登录。
func ShareBackendInfo() ShareBackend {
	if syncSrv == nil {
		return ShareBackend{}
	}
	_, port, err := net.SplitHostPort(syncSrv.Addr)
	if err != nil {
		port = "8080"
	}
	return ShareBackend{
		URL:       "http://127.0.0.1:" + port,
		PublicURL: URL(),
		Token:     syncShareToken,
		Running:   true,
	}
}
