package main

import (
	"fmt"
	"net"
	"net/http"

	"apitool/server"
)

var syncSrv *http.Server

// localIP 返回本机第一个非回环 IPv4 地址（用于生成局域网可访问的分享地址）
func localIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		if v4 := ipnet.IP.To4(); v4 != nil {
			return v4.String()
		}
	}
	return "127.0.0.1"
}

// StartSyncServer 在客户端进程内启动内置同步服务（客户端与服务一起部署时使用）。
// 返回实际监听地址（host:port），便于前端展示“别人真正能连的地址”。
func (a *App) StartSyncServer(addr string) (string, error) {
	if syncSrv != nil {
		return syncSrv.Addr, nil
	}
	if addr == "" {
		addr = ":8080"
	}
	s := server.NewStore(a.syncDir)
	// 先用 net.Listen 拿到真实监听地址（尤其 addr 含 :0 随机端口，或 :8080 会被系统解析为 [::]:8080）
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", fmt.Errorf("启动同步服务失败: %v", err)
	}
	srv := &http.Server{Handler: s.Handler()}
	syncSrv = srv
	go func() {
		_ = srv.Serve(ln)
	}()
	return ln.Addr().String(), nil
}

// StopSyncServer 停止内置同步服务
func (a *App) StopSyncServer() error {
	if syncSrv == nil {
		return nil
	}
	_ = syncSrv.Close()
	syncSrv = nil
	return nil
}

// SyncServerRunning 返回内置同步服务是否运行中
func (a *App) SyncServerRunning() bool {
	return syncSrv != nil
}

// SyncServerAddr 返回内置同步服务实际监听地址（host:port），未运行时为空
func (a *App) SyncServerAddr() string {
	if syncSrv == nil {
		return ""
	}
	return syncSrv.Addr
}

// SyncServerURL 返回内置同步服务实际可访问的完整地址（http://IP:port）。
// 绑定到 0.0.0.0 / [::] / 空 host 时，用本机局域网 IP 拼出别人能连的地址。
func (a *App) SyncServerURL() string {
	addr := a.SyncServerAddr()
	if addr == "" {
		return ""
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	if host == "" || host == "0.0.0.0" || host == "[::]" || host == "::" {
		host = localIP()
	}
	return fmt.Sprintf("http://%s:%s", host, port)
}
