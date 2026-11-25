//go:build !serveronly

package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"apitool/internal/serverclient"
	"apitool/internal/store"
)

// ----------------------------------------------------------------------------
// 2.0 服务端双模式：桌面端登录服务端后，业务数据（项目/设置/文档/测试）全部
// 经 DualStore 读写服务端（本地 SQLite 作为写穿镜像保证离线可用）；剪贴板、
// 抓包、agent 配置等桌面专属数据保持本地。
// ----------------------------------------------------------------------------

const serverSessionFile = "server_session.json"

var serverMu sync.Mutex

func (a *App) sessionPath() string {
	return filepath.Join(filepath.Dir(a.dataFile), serverSessionFile)
}

// ServerLoginArgs 桌面端登录服务端入参。
type ServerLoginArgs struct {
	BaseURL  string `json:"baseUrl"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// ServerUserInfo 登录态信息（前端展示用）。
type ServerUserInfo struct {
	LoggedIn bool   `json:"loggedIn"`
	BaseURL  string `json:"baseUrl"`
	UserID   string `json:"userId"`
	Username string `json:"username"`
	// Projects 服务端可见项目（含角色），供前端 role 表填充。
	Projects []serverclient.ProjectMeta `json:"projects"`
}

// ServerLogin 登录服务端（成功后业务数据自动切换到服务端）。
func (a *App) ServerLogin(args ServerLoginArgs) (ServerUserInfo, error) {
	serverMu.Lock()
	defer serverMu.Unlock()

	base := strings.TrimSpace(args.BaseURL)
	if base == "" {
		return ServerUserInfo{}, fmt.Errorf("请填写服务端地址")
	}
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	// 重建客户端（可能换了服务器地址）
	a.srvClient = serverclient.New(base)
	a.dual = serverclient.NewDualStore(a.store, a.srvClient)
	a.srvClient.SetOnChange(func(userID, username string) {
		a.Emit("apitool:server-auth", map[string]interface{}{
			"loggedIn": username != "", "userID": userID, "username": username,
		})
	})
	if err := a.srvClient.Login(strings.TrimSpace(args.Username), args.Password); err != nil {
		return ServerUserInfo{}, err
	}
	_ = a.srvClient.SaveCredentials(a.sessionPath())

	// 切换业务核心的存储到新 DualStore（appcore 持有 data 接口引用）
	a.App.SetDataStore(a.dual)

	metas, _ := a.dual.FetchProjectsMeta()
	return ServerUserInfo{
		LoggedIn: true,
		BaseURL:  base,
		UserID:   a.srvClient.UserID(),
		Username: a.srvClient.Username(),
		Projects: metas,
	}, nil
}

// ServerRegister 注册并登录服务端（桌面端直接注册，无需先去网页端）。
func (a *App) ServerRegister(args ServerLoginArgs) (ServerUserInfo, error) {
	serverMu.Lock()
	defer serverMu.Unlock()

	base := strings.TrimSpace(args.BaseURL)
	if base == "" {
		return ServerUserInfo{}, fmt.Errorf("请填写服务端地址")
	}
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	a.srvClient = serverclient.New(base)
	a.dual = serverclient.NewDualStore(a.store, a.srvClient)
	a.srvClient.SetOnChange(func(userID, username string) {
		a.Emit("apitool:server-auth", map[string]interface{}{
			"loggedIn": username != "", "userID": userID, "username": username,
		})
	})
	if err := a.srvClient.Register(strings.TrimSpace(args.Username), args.Password); err != nil {
		return ServerUserInfo{}, err
	}
	_ = a.srvClient.SaveCredentials(a.sessionPath())
	a.App.SetDataStore(a.dual)

	metas, _ := a.dual.FetchProjectsMeta()
	return ServerUserInfo{
		LoggedIn: true,
		BaseURL:  base,
		UserID:   a.srvClient.UserID(),
		Username: a.srvClient.Username(),
		Projects: metas,
	}, nil
}

// ServerLogout 退出服务端登录（数据切回本地模式）。
func (a *App) ServerLogout() error {
	serverMu.Lock()
	defer serverMu.Unlock()
	a.srvClient.Logout()
	a.srvClient.ClearCredentials(a.sessionPath())
	a.dual = serverclient.NewDualStore(a.store, a.srvClient)
	a.App.SetDataStore(a.dual)
	return nil
}

// ServerStatus 当前登录态（前端启动/切换页面时查询）。
func (a *App) ServerStatus() ServerUserInfo {
	serverMu.Lock()
	defer serverMu.Unlock()
	info := ServerUserInfo{}
	if a.srvClient == nil {
		return info
	}
	if a.srvClient.LoggedIn() {
		info.LoggedIn = true
		info.BaseURL = a.srvClient.BaseURL()
		info.UserID = a.srvClient.UserID()
		info.Username = a.srvClient.Username()
	}
	return info
}

// ServerRefreshProjects 拉取服务端项目元数据（登录后刷新 role 表）。
func (a *App) ServerRefreshProjects() ([]serverclient.ProjectMeta, error) {
	if a.srvClient == nil || !a.srvClient.LoggedIn() {
		return nil, fmt.Errorf("未登录服务端")
	}
	return a.dual.FetchProjectsMeta()
}

// 确保导入被使用（DualStore 在 ServerLogin 中重建）。
var _ = store.ActiveProjectIndex
