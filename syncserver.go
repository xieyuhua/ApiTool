package main

import (
	"net/http"

	"apitool/server"
)

var syncSrv *http.Server

// StartSyncServer 在客户端进程内启动内置同步服务（客户端与服务一起部署时使用）
func (a *App) StartSyncServer(addr string) (string, error) {
	if syncSrv != nil {
		return "同步服务已在运行", nil
	}
	if addr == "" {
		addr = ":8080"
	}
	s := server.NewStore(a.syncDir)
	srv := &http.Server{Addr: addr, Handler: s.Handler()}
	go func() {
		_ = srv.ListenAndServe()
	}()
	syncSrv = srv
	return "内置同步服务已启动：" + addr, nil
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
