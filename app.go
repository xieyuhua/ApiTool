//go:build !serveronly

package main

import (
	"apitool/internal/appcore"
	"apitool/internal/capture"
	"apitool/internal/doc"
	"apitool/internal/model"
	"apitool/internal/platform"
	"apitool/internal/share"
	"apitool/internal/sniff"
	"apitool/internal/store"
	"apitool/internal/stress"
	syncsrv "apitool/internal/sync"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"apitool/internal/wailsrt"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// AppVersion 客户端版本号（用于升级检测与本地配置标记）
const AppVersion = "1.0.0"

// DefaultUpdateURL 默认升级服务地址（本地同步服务）
const DefaultUpdateURL = "http://127.0.0.1" + syncsrv.DefaultSyncAddr

// NewApp 创建桌面壳实例。
func NewApp() *App {
	return &App{}
}

// App struct：桌面壳。业务方法全部由嵌入的 appcore.App 提供（Wails 绑定路径
// main.App.X 经方法提升保持不变）；本结构体只保留桌面专属状态（窗口/托盘/
// 剪贴板采集/抓包管理器/本地存储句柄）。
type App struct {
	*appcore.App // 业务核心：数据/文档/测试/插件/Agent 等全部导出方法

	ctx            context.Context
	store          *store.Store // 本地存储（托盘/剪贴板/抓包等桌面功能落盘用）
	dataFile       string
	syncDir        string
	sniffMgr       *sniff.Manager // MITM 抓包管理器（桌面专属）
	mu             sync.Mutex
	ctxReady       atomic.Bool  // 桌面窗口上下文是否就绪（v3 中在 application.New 后即为 true）
	windowVisible  bool         // 主窗口当前是否可见（托盘显隐用）
	clipWinVisible bool         // 剪贴板历史浮层当前是否可见
	clipMonitor    atomic.Bool  // 设置中的剪贴板监听开关（轮询线程零 IO 读取）
	clipMaxItems   atomic.Int32 // 剪贴板历史上限（写入时零 IO 读取）
	quitting       bool         // 是否正在主动退出（绕过 beforeClose 的隐藏逻辑）
}

// AppVersion 返回客户端版本号。
func (a *App) AppVersion() string { return AppVersion }

func (a *App) startup(wailsApp *application.App) {
	ctx := wailsApp.Context()
	a.ctx = ctx
	a.ctxReady.Store(true)
	// 数据目录使用「程序可执行文件所在目录」下的 apitool 子目录，
	// 便于随程序整体迁移/打包，不再依赖系统用户配置目录（%APPDATA% 等）。
	dir := "."
	if exe, err := os.Executable(); err == nil {
		dir = filepath.Dir(exe)
	}
	dataDir := filepath.Join(dir, "apitool")
	_ = os.MkdirAll(dataDir, 0o755)
	a.dataFile = filepath.Join(dataDir, "data.json")
	a.syncDir = filepath.Join(dataDir, "syncserver")
	a.store = store.New(a.dataFile, AppVersion, DefaultUpdateURL)
	// 初始化业务核心（本地存储 + wailsrt 总线），Wails 绑定经方法提升生效
	{
		core, err := appcore.New(a.store, wailsrt.RuntimeImpl, appcore.HostInfo{Version: AppVersion})
		if err != nil {
			fmt.Printf("初始化业务核心失败: %v\n", err)
			return
		}
		a.App = core
	}
	// 初始化剪贴板设置（轮询线程与写入路径读取，避免每次读库）
	{
		cs := a.store.GetData().Settings.Clipboard
		a.clipMonitor.Store(cs.Monitor)
		a.clipMaxItems.Store(int32(cs.MaxItems))
	}
	// 注册局域网 Web UI（复用本份前端 + HTTP/SSE 桥），使手机/其他电脑打开网页即与桌面端一致
	a.Manager.RegisterWebUI(a.webUIHandler())
	// 初始化配置：本地 JSON 不存在时，自动生成默认配置文件
	if _, err := os.Stat(a.dataFile); err != nil {
		_ = a.SaveData(a.store.GetData())
	}
	// 仅加载/生成捕获服务 Token；捕获服务不再自动启动，改由用户在「请求捕获」页面手动开启
	capture.LoadOrCreateToken(a.syncDir)
	// 启动系统托盘（独立于主界面，提供显隐/测试/退出）
	a.windowVisible = true
	a.startTray(wailsApp)
	// 启动剪贴板后台采集（文本 + 图片）
	go a.StartClipboardCapture()
	// 初始化 MITM 抓包管理器（CA 与抓包数据目录）
	sniffDir := filepath.Join(dataDir, "sniff")
	_ = os.MkdirAll(sniffDir, 0o755)
	mgr, err := sniff.NewManager(sniffDir, a)
	if err != nil {
		// 非致命：抓包功能不可用，但不影响其它功能
		fmt.Printf("初始化抓包模块失败: %v\n", err)
	} else {
		a.sniffMgr = mgr
	}
}

// shutdown 在应用退出时清理资源（停止采集）。
func (a *App) shutdown() {
	a.StopClipboardCapture()
}

// beforeClose 处理「用户点击关闭」：隐藏到托盘而非退出。
// v3 中由窗口事件钩子（main.go 的 bindWindowEvents）调用。
func (a *App) beforeClose() {
	// 主动退出（托盘「退出」）时不做隐藏处理
	if a.quitting {
		return
	}
	// 若剪贴板浮层处于打开状态，一并关闭并复位状态，避免下次 toggle 判断错位。
	if a.clipWinVisible {
		a.clipWinVisible = false
		a.Emit("apitool:hide-clipboard-history")
	}
	// 隐藏前先取消置顶：浮层打开过会置顶，否则下次从托盘显示时窗口会盖住其它应用
	a.WindowSetAlwaysOnTop(false)
	a.WindowHide()
	a.windowVisible = false
}

// GetClipboardText 读取系统剪贴板文本（供前端轮询记录历史）
func (a *App) GetClipboardText() (string, error) {
	return wailsrt.RuntimeImpl.ClipboardGetText()
}

// SetClipboardText 写入系统剪贴板
func (a *App) SetClipboardText(text string) error {
	return wailsrt.RuntimeImpl.ClipboardSetText(text)
}

func (a *App) CopyToClipboard(text string) error {
	return wailsrt.RuntimeImpl.ClipboardSetText(text)
}

// ClipHistory 返回剪贴板历史（供原生菜单 / 托盘菜单读取）。
func (a *App) ClipHistory() []model.ClipItem {
	data := a.LoadData()
	return data.Clipboard.History
}

// ClearClipHistory 清空剪贴板历史并落盘（同时删除图片文件）。
func (a *App) ClearClipHistory() {
	clip := a.loadClipData()
	for _, it := range clip.History {
		if it.Type == model.ClipTypeImage && it.ImagePath != "" {
			_ = os.Remove(filepath.Join(a.store.Dir(), it.ImagePath))
		}
	}
	b, err := json.Marshal(model.ClipData{History: []model.ClipItem{}})
	if err != nil {
		return
	}
	_ = a.store.SaveClipboardRaw(string(b))
}

func (a *App) OpenInBrowser(url string) {
	wailsrt.RuntimeImpl.BrowserOpenURL(url)
}

// ExportDoc 导出文档（转发到 doc 包）
func (a *App) ExportDoc(dirID string, apiID string, format string) (string, error) {
	return doc.ExportDoc(a.ctx, a.store, dirID, apiID, format)
}

// PreviewDocWatermark 生成带水印的文档 HTML 并用浏览器打开，供导出前核对水印效果
func (a *App) PreviewDocWatermark(dirID string, apiID string) (string, error) {
	return doc.PreviewWatermark(a.ctx, a.store, dirID, apiID)
}

func (a *App) ImportDoc() (string, error) {
	return doc.ImportDoc(a.ctx, a.store, "")
}

// ShareDoc 生成 HTML 文档并用浏览器打开（转发到 doc 包）
func (a *App) ShareDoc(dirID string, apiID string) (string, error) {
	return doc.ShareDoc(a.ctx, a.store, dirID, apiID)
}

// CopyDocMarkdown 复制 Markdown 文档到剪贴板（转发到 doc 包）
func (a *App) CopyDocMarkdown(dirID string, apiID string) error {
	return doc.CopyDocMarkdown(a.ctx, a.store, dirID, apiID)
}

// UpdateInfo 升级服务返回的版本信息
type UpdateInfo struct {
	Version string `json:"version"` // 服务端最新版本号
	URL     string `json:"url"`     // 新版本下载地址
	Notes   string `json:"notes"`   // 更新说明
}

// CheckUpdateResult 检测结果返回给前端
type CheckUpdateResult struct {
	Current string `json:"current"` // 当前版本
	Latest  string `json:"latest"`  // 服务端版本
	HasNew  bool   `json:"hasNew"`  // 是否有新版本
	URL     string `json:"url"`     // 新版本下载地址
	Notes   string `json:"notes"`   // 更新说明
	Error   string `json:"error"`   // 检测错误信息（网络/解析失败）
}

func (a *App) Emit(event string, data ...interface{}) {
	// 局域网网页端不依赖 Wails 上下文，始终可推送
	webBroadcast(event, data...)
	// 桌面窗口：上下文未就绪时跳过（此时窗口尚未创建，无接收方）
	if !a.ctxReady.Load() {
		return
	}
	wailsrt.RuntimeImpl.Emit(event, data...)
}

// Quit 退出应用。
func (a *App) Quit() {
	wailsrt.RuntimeImpl.Quit()
}

// WindowShow 显示主窗口。
func (a *App) WindowShow() {
	wailsrt.RuntimeImpl.WindowShow()
}

// WindowHide 隐藏主窗口。
func (a *App) WindowHide() {
	wailsrt.RuntimeImpl.WindowHide()
}

// WindowUnminimise 还原最小化窗口。
func (a *App) WindowUnminimise() {
	wailsrt.RuntimeImpl.WindowUnminimise()
}

// WindowCenter 居中主窗口。
func (a *App) WindowCenter() {
	wailsrt.RuntimeImpl.WindowCenter()
}

// WindowSetAlwaysOnTop 设置主窗口是否置顶。
func (a *App) WindowSetAlwaysOnTop(b bool) {
	wailsrt.RuntimeImpl.WindowSetAlwaysOnTop(b)
}

// BrowserOpenURL 用系统浏览器打开 URL（实现 bus.Bus）。
func (a *App) BrowserOpenURL(url string) {
	wailsrt.RuntimeImpl.BrowserOpenURL(url)
}

// OpenExportFile 用系统默认程序打开导出的文件（xlsx/csv/html/md）。
// 供 AI Agent 导出表格后，用户在「导出结果」中一键打开。
func (a *App) OpenExportFile(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("路径为空")
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("文件不存在: %s", path)
	}
	// 仅在桌面端有效：没有主窗口时无法调起系统默认程序
	if wailsrt.MainWindow() == nil {
		return fmt.Errorf("当前环境无法打开本地文件，请手动到该路径查看：%s", path)
	}
	wailsrt.RuntimeImpl.BrowserOpenURL("file:///"+strings.ReplaceAll(filepath.ToSlash(path), " ", "%20"))
	return nil
}

func (a *App) OpenExportFolder(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("路径为空")
	}
	st, err := os.Stat(path)
	dir := path
	if err == nil && !st.IsDir() {
		dir = filepath.Dir(path)
	}
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("目录不存在: %s", dir)
	}
	if wailsrt.MainWindow() == nil {
		return fmt.Errorf("当前环境无法打开本地目录，请手动到该路径查看：%s", dir)
	}
	wailsrt.RuntimeImpl.BrowserOpenURL("file:///"+strings.ReplaceAll(filepath.ToSlash(dir), " ", "%20"))
	return nil
}

// ClipboardGetText 读取系统剪贴板文本（实现 bus.Bus）。
func (a *App) ClipboardGetText() (string, error) {
	return wailsrt.RuntimeImpl.ClipboardGetText()
}

// ClipboardSetText 写入系统剪贴板（实现 bus.Bus）。
func (a *App) ClipboardSetText(text string) error {
	return wailsrt.RuntimeImpl.ClipboardSetText(text)
}

// SaveFileDialog 弹出保存文件对话框（实现 bus.Bus）。
func (a *App) SaveFileDialog(opts wailsrt.DialogOptions) (string, error) {
	return wailsrt.RuntimeImpl.SaveFileDialog(opts)
}

// OpenFileDialog 弹出打开文件对话框（实现 bus.Bus）。
func (a *App) OpenFileDialog(opts wailsrt.DialogOptions) (string, error) {
	return wailsrt.RuntimeImpl.OpenFileDialog(opts)
}

// OpenDirectoryDialog 弹出选择目录对话框（实现 bus.Bus）。
func (a *App) OpenDirectoryDialog(opts wailsrt.DialogOptions) (string, error) {
	return wailsrt.RuntimeImpl.OpenDirectoryDialog(opts)
}

// ----------------------------------------------------------------------------
// 同步服务转发层（实现在 internal/sync）
// ----------------------------------------------------------------------------

// StartSyncServer 在客户端进程内启动内置同步服务。
func (a *App) StartSyncServer() (string, error) { return syncsrv.Start(a.syncDir) }

// StopSyncServer 停止内置同步服务。
func (a *App) StopSyncServer() error { return syncsrv.Stop() }

// SyncServerRunning 返回同步服务是否运行中。
func (a *App) SyncServerRunning() bool { return syncsrv.Running() }

// SyncServerURL 返回同步服务可访问地址。
func (a *App) SyncServerURL() string { return syncsrv.URL() }

// SyncShareBackend 返回内置同步服务作为分享后端的地址与 token。
func (a *App) SyncShareBackend() syncsrv.ShareBackend { return syncsrv.ShareBackendInfo() }

// ----------------------------------------------------------------------------
// 捕获服务转发层（实现在 internal/capture）
// ----------------------------------------------------------------------------

// StartCaptureServer 启动独立捕获服务（端口 8653，带 Token 鉴权）。
func (a *App) StartCaptureServer(addr string, token string) (string, error) {
	return capture.Start(addr, token, a.syncDir)
}

// StopCaptureServer 停止捕获服务。
func (a *App) StopCaptureServer() error { return capture.Stop() }

// CaptureServerRunning 捕获服务是否在运行。
func (a *App) CaptureServerRunning() bool { return capture.Running() }

// CaptureInfo 返回捕获服务信息（地址、端口、Token、条数）。
func (a *App) CaptureInfo() capture.CaptureServerInfo { return capture.Info() }

// ----------------------------------------------------------------------------
// MITM 抓包转发层（实现在 internal/sniff）
// ----------------------------------------------------------------------------

// SniffStatus 返回抓包运行状态（代理地址、CA 指纹、是否已安装）。
func (a *App) SniffStatus() sniff.Status {
	if a.sniffMgr == nil {
		return sniff.Status{Error: "抓包模块未初始化"}
	}
	return a.sniffMgr.Status()
}

// SniffStart 启动抓包（addr 形如 "127.0.0.1:8888"，传 "0" 或空则用默认端口）。
func (a *App) SniffStart(addr string) error {
	if a.sniffMgr == nil {
		return fmt.Errorf("抓包模块未初始化")
	}
	if addr == "" || addr == "0" {
		addr = "127.0.0.1:8888"
	}
	return a.sniffMgr.Start(addr)
}

// SniffStop 停止抓包并还原系统代理。
func (a *App) SniffStop() error {
	if a.sniffMgr == nil {
		return fmt.Errorf("抓包模块未初始化")
	}
	return a.sniffMgr.Stop()
}

// SniffSetFilter 设置抓包过滤条件（按 Host / 进程名 / 仅 HTTP）。
func (a *App) SniffSetFilter(f sniff.Filter) error {
	if a.sniffMgr == nil {
		return fmt.Errorf("抓包模块未初始化")
	}
	a.sniffMgr.SetFilter(f)
	return nil
}

// SniffGetRewrites 返回域名重定向规则列表（供前端弹窗维护）。
func (a *App) SniffGetRewrites() []sniff.HostRewrite {
	if a.sniffMgr == nil {
		return nil
	}
	return a.sniffMgr.GetRewrites()
}

// SniffSetRewrites 整体保存域名重定向规则并持久化，运行中的代理立即生效。
func (a *App) SniffSetRewrites(list []sniff.HostRewrite) error {
	if a.sniffMgr == nil {
		return fmt.Errorf("抓包模块未初始化")
	}
	return a.sniffMgr.SetRewrites(list)
}

// SniffListSessions 返回抓包会话列表。
func (a *App) SniffListSessions() []sniff.Session {
	if a.sniffMgr == nil {
		return nil
	}
	return a.sniffMgr.ListSessions()
}

// SniffGetSession 返回完整抓包会话（含全部流量记录）。
func (a *App) SniffGetSession(id string) (*sniff.Session, error) {
	if a.sniffMgr == nil {
		return nil, fmt.Errorf("抓包模块未初始化")
	}
	sess, ok := a.sniffMgr.GetSession(id)
	if !ok {
		return nil, fmt.Errorf("会话不存在")
	}
	return sess, nil
}

// SniffGetSessionErrors 返回指定会话的解密/连接失败日志。
func (a *App) SniffGetSessionErrors(id string) []sniff.ErrorInfo {
	if a.sniffMgr == nil {
		return nil
	}
	return a.sniffMgr.GetSessionErrors(id)
}

// SniffDeleteSession 删除抓包会话。
func (a *App) SniffDeleteSession(id string) error {
	if a.sniffMgr == nil {
		return fmt.Errorf("抓包模块未初始化")
	}
	return a.sniffMgr.DeleteSession(id)
}

// SniffExportOpenAPI 将抓包会话导出为 OpenAPI 3.0（弹出保存对话框）。
// hostMode："original"=接口地址用抓包实际完整地址；"env"=host 替换为环境变量 {{host}}。
func (a *App) SniffExportOpenAPI(id, title, hostMode string) (string, error) {
	if a.sniffMgr == nil {
		return "", fmt.Errorf("抓包模块未初始化")
	}
	return a.sniffMgr.ExportOpenAPI(id, title, hostMode)
}

// SniffInstallCA 将根 CA 安装到系统信任库（需管理员权限）。
func (a *App) SniffInstallCA() error {
	if a.sniffMgr == nil {
		return fmt.Errorf("抓包模块未初始化")
	}
	return a.sniffMgr.InstallCA()
}

// SniffCAPath 返回根 CA 证书文件路径（供用户手动安装）。
func (a *App) SniffCAPath() string {
	if a.sniffMgr == nil {
		return ""
	}
	return a.sniffMgr.ExportCAPath()
}

// SniffCAPEM 返回根 CA 证书 PEM 文本（供前端展示/复制）。
func (a *App) SniffCAPEM() string {
	if a.sniffMgr == nil {
		return ""
	}
	return string(a.sniffMgr.CA().CertPEM())
}

// SniffImportCA 导入用户提供的根证书（证书 + 私钥 PEM），替换当前 CA。
// 可直接复用 Fiddler 等现有根证书；返回新 CA 的 SHA1 指纹。
func (a *App) SniffImportCA(certPEM, keyPEM string) (string, error) {
	if a.sniffMgr == nil {
		return "", fmt.Errorf("抓包模块未初始化")
	}
	return a.sniffMgr.ImportCA(certPEM, keyPEM)
}

// SniffPickCAFile 弹出文件选择框让用户选择根证书文件（支持 .cer/.crt/.pem/.pfx 等），
// 读取并解析其中证书（与私钥，若有），返回给前端预览/确认。DER 二进制证书会自动转 PEM。
func (a *App) SniffPickCAFile() (map[string]string, error) {
	if a.sniffMgr == nil {
		return nil, fmt.Errorf("抓包模块未初始化")
	}
	path, err := wailsrt.OpenFileDialog(a.ctx, wailsrt.DialogOptions{
		Title: "选择根证书文件（FiddlerRoot.cer 等）",
		Filters: []wailsrt.FileFilter{
			{DisplayName: "证书文件", Pattern: "*.cer;*.crt;*.pem;*.crt;*.der;*.pfx;*.*"},
			{DisplayName: "所有文件", Pattern: "*.*"},
		},
	})
	if err != nil || path == "" {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseCAFile(data)
}

// parseCAFile 从证书文件中提取证书 PEM 与私钥 PEM。
func parseCAFile(data []byte) (map[string]string, error) {
	text := strings.TrimSpace(string(data))
	certPEM := ""
	keyPEM := ""
	if strings.Contains(text, "BEGIN") {
		// PEM 文本：分离证书与私钥 block
		rest := text
		for {
			block, r := pem.Decode([]byte(rest))
			if block == nil {
				break
			}
			enc := pem.EncodeToMemory(block)
			switch {
			case block.Type == "CERTIFICATE":
				if certPEM == "" {
					certPEM = string(enc)
				}
			case strings.Contains(block.Type, "PRIVATE KEY"):
				if keyPEM == "" {
					keyPEM = string(enc)
				}
			}
			rest = string(r)
		}
		if certPEM == "" {
			return nil, fmt.Errorf("文件中未找到 CERTIFICATE 证书块")
		}
		return map[string]string{"certPem": certPEM, "keyPem": keyPEM, "path": ""}, nil
	}
	// DER 二进制证书（.cer 常见）：转 PEM
	block, _ := pem.Decode(data)
	if block == nil {
		if _, err := x509.ParseCertificate(data); err != nil {
			return nil, fmt.Errorf("无法解析证书文件（仅支持 PEM / DER 格式）：%v", err)
		}
		certPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: data}))
		return map[string]string{"certPem": certPEM, "keyPem": "", "path": ""}, nil
	}
	if block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("无法解析证书文件：内容不是证书")
	}
	certPEM = string(pem.EncodeToMemory(block))
	return map[string]string{"certPem": certPEM, "keyPem": keyPEM, "path": ""}, nil
}

// SniffSetSystemProxy 设置抓包时是否自动切换系统代理。
// 关闭时仅监听端口，供用户在浏览器/应用中手动配置代理。
func (a *App) SniffSetSystemProxy(enabled bool) {
	if a.sniffMgr == nil {
		return
	}
	a.sniffMgr.SetSystemProxyEnabled(enabled)
}

// SniffGenerateApiFromSession 将抓包会话中选中的流量记录转换为接口草稿，
// 写入指定项目/目录的接口树，返回写入条数。
func (a *App) SniffGenerateApiFromSession(sessionID string, recordIDs []string, projectID, dirID string) (int, error) {
	if a.sniffMgr == nil {
		return 0, fmt.Errorf("抓包模块未初始化")
	}
	if len(recordIDs) == 0 {
		return 0, fmt.Errorf("请至少选择一条流量记录")
	}
	sess, ok := a.sniffMgr.GetSession(sessionID)
	if !ok {
		return 0, fmt.Errorf("抓包会话不存在")
	}
	idSet := map[string]bool{}
	for _, id := range recordIDs {
		idSet[id] = true
	}
	var sel []sniff.TrafficRecord
	for _, r := range sess.Records {
		if idSet[r.ID] && r.Method != "" {
			sel = append(sel, r)
		}
	}
	if len(sel) == 0 {
		return 0, fmt.Errorf("所选记录中无有效 HTTP(S) 流量")
	}

	data := a.store.GetData()
	idx := -1
	for i, p := range data.Projects {
		if p.ID == projectID {
			idx = i
			break
		}
	}
	if idx < 0 {
		idx = store.ActiveProjectIndex(data)
	}
	if idx < 0 {
		return 0, fmt.Errorf("没有可用的项目")
	}
	// 去重：同批次内相同 Method+URL 只保留一条；且与接口树已有接口去重
	seen := map[string]bool{}
	existing := data.Projects[idx].Apis
	added := 0
	for _, r := range sel {
		api := sniff.RecordToApi(r, dirID)
		key := strings.ToLower(api.Method) + "|" + strings.TrimSuffix(strings.TrimSpace(api.URL), "/")
		if seen[key] || apiExists(existing, api) {
			continue
		}
		seen[key] = true
		data.Projects[idx].Apis = append(data.Projects[idx].Apis, api)
		added++
	}
	if added == 0 {
		return 0, fmt.Errorf("所选流量均已存在相同接口，无新增")
	}
	data.Projects[idx].UpdatedAt = time.Now().Format(time.RFC3339)
	if err := a.store.SaveData(data); err != nil {
		return 0, err
	}
	return added, nil
}

// SniffGenerateApiFromRecords 将前端选中的实时流量记录（自带完整数据）批量转换为接口定义，
// 写入指定项目/目录的接口树，返回写入条数。不依赖会话存储。
func (a *App) SniffGenerateApiFromRecords(records []sniff.TrafficRecord, projectID, dirID string) (int, error) {
	if len(records) == 0 {
		return 0, fmt.Errorf("请至少选择一条流量记录")
	}
	var valid []sniff.TrafficRecord
	for _, r := range records {
		if r.Method != "" && r.URL != "" {
			valid = append(valid, r)
		}
	}
	if len(valid) == 0 {
		return 0, fmt.Errorf("所选记录中无有效 HTTP(S) 流量")
	}

	data := a.store.GetData()
	idx := -1
	for i, p := range data.Projects {
		if p.ID == projectID {
			idx = i
			break
		}
	}
	if idx < 0 {
		idx = store.ActiveProjectIndex(data)
	}
	if idx < 0 {
		return 0, fmt.Errorf("没有可用的项目")
	}
	// 去重：同批次内相同 Method+URL 只保留一条；且与接口树已有接口去重
	seen := map[string]bool{}
	existing := data.Projects[idx].Apis
	added := 0
	for _, r := range valid {
		api := sniff.RecordToApi(r, dirID)
		key := strings.ToLower(api.Method) + "|" + strings.TrimSuffix(strings.TrimSpace(api.URL), "/")
		if seen[key] || apiExists(existing, api) {
			continue
		}
		seen[key] = true
		data.Projects[idx].Apis = append(data.Projects[idx].Apis, api)
		added++
	}
	if added == 0 {
		return 0, fmt.Errorf("所选流量均已存在相同接口，无新增")
	}
	data.Projects[idx].UpdatedAt = time.Now().Format(time.RFC3339)
	if err := a.store.SaveData(data); err != nil {
		return 0, err
	}
	return added, nil
}

// apiExists 判断接口树中是否已存在相同 Method+URL 的接口。
func apiExists(apis []model.ApiInfo, target model.ApiInfo) bool {
	tu := strings.TrimSuffix(strings.TrimSpace(strings.ToLower(target.URL)), "/")
	tm := strings.ToLower(target.Method)
	for _, a := range apis {
		if strings.EqualFold(a.Method, tm) &&
			strings.TrimSuffix(strings.TrimSpace(strings.ToLower(a.URL)), "/") == tu {
			return true
		}
	}
	return false
}

// GetCapturedRequests 返回全部已捕获请求（供前端展示）。
func (a *App) GetCapturedRequests() []capture.CapturedRequest { return capture.GetRequests() }

// ClearCapturedRequests 清空已捕获列表。
func (a *App) ClearCapturedRequests() { capture.Clear() }

// GenerateApiFromCaptured 将选中的捕获请求转换为接口定义并导入当前项目。
func (a *App) GenerateApiFromCaptured(ids []string, projectID, dirID string) (int, error) {
	return capture.GenerateApi(ids, projectID, dirID, a.store)
}

// ImportCapturedAsTestCases 将选中的捕获请求转换为测试用例，导入当前项目。
func (a *App) ImportCapturedAsTestCases(ids []string) (int, error) {
	return capture.ImportTestCases(ids, a.store)
}

// ExportCapturedOpenAPI 将选中的捕获请求生成 OpenAPI 文档并弹出保存对话框。
func (a *App) ExportCapturedOpenAPI(ids []string, title string) (string, error) {
	return capture.ExportOpenAPI(ids, title, a)
}

// BuildCapturedOpenAPI 生成 OpenAPI JSON 文本（不弹保存框）。
func (a *App) BuildCapturedOpenAPI(ids []string, title string) (string, error) {
	return capture.BuildOpenAPI(ids, title)
}

// ----------------------------------------------------------------------------
// 分享服务转发层（实现在 internal/share）
// ----------------------------------------------------------------------------

// StartShareServer 启动分享服务（默认端口 8082）。
func (a *App) StartShareServer(port string) error { return share.Start(port) }

// StopShareServer 停止分享服务。
func (a *App) StopShareServer() error { return share.Stop() }

// ShareServerRunning 分享服务是否在运行。
func (a *App) ShareServerRunning() bool { return share.Running() }

// ShareInfo 返回分享服务信息（内外网地址）。
func (a *App) ShareInfo() share.ShareServerInfo { return share.Info() }

// BuildShareDoc 构建文档内容（不启动服务），供前端内嵌预览。
func (a *App) BuildShareDoc(scope, projectID, dirID, format string) (string, error) {
	return share.BuildDoc(a.store, scope, projectID, dirID, format)
}

// RefreshShareDoc 重新生成并刷新当前分享文档内容。
func (a *App) RefreshShareDoc(scope, projectID, dirID string) error {
	return share.Refresh(a.store, scope, projectID, dirID)
}

// OpenShareDoc 按 openType 内嵌预览 / 复制链接 / 打开分享文档。
func (a *App) OpenShareDoc(scope, projectID, dirID, openType, format string) (string, error) {
	return share.OpenDoc(a, a.store, scope, projectID, dirID, openType, format)
}

// ShareTestReport 分享测试报告（HTML 默认打开分享服务）。
func (a *App) ShareTestReport(reportJSON, format, openType string) (string, error) {
	return share.TestReport(a, a.store, reportJSON, format, openType)
}

// ----------------------------------------------------------------------------
// 多分享链接绑定（实现在 internal/share），对齐前端 ShareDialog 契约
// ----------------------------------------------------------------------------

// BuildSharedHTML 构建指定目录/接口的完整 HTML 网页源码（前端"网页代码"页）。
func (a *App) BuildSharedHTML(dirID, apiID string) (string, error) {
	return share.BuildHTMLByAPI(a.store, dirID, apiID)
}

// BuildSharedTitle 返回指定目录/接口的分享文档标题。
func (a *App) BuildSharedTitle(dirID, apiID string) (string, error) {
	return share.BuildTitleByAPI(a.store, dirID, apiID)
}

// CreateShareLink 创建一条本地分享链接（应用退出后失效），返回可访问 URL。
func (a *App) CreateShareLink(dirID, apiID, password string, expireMinutes int) (string, error) {
	_, link, err := share.CreateShare(a.store, dirID, apiID, password, expireMinutes)
	return link, err
}

// ListShares 返回当前有效分享链接列表。
func (a *App) ListShares() []share.ShareItemView {
	return share.ListShares()
}

// StopShare 停止单条分享链接。
func (a *App) StopShare(token string) error {
	return share.StopShare(token)
}

// ----------------------------------------------------------------------------
// 压测转发层（实现在 internal/stress）
// ----------------------------------------------------------------------------

// RunStressTest 对给定目标发起并发压测，返回含延迟分布与吞吐的报告。
func (a *App) RunStressTest(targets []stress.StressTarget, config stress.StressConfig) (stress.StressReport, error) {
	return stress.Run(targets, config, a.store, a)
}

// ExportStressReport 将压测报告（JSON）导出为 Markdown / HTML 文件，返回保存路径。
func (a *App) ExportStressReport(reportJSON string, format string) (string, error) {
	return stress.ExportReport(reportJSON, format, a)
}

// ----------------------------------------------------------------------------
// 剪贴板（底层采集在 internal/platform，业务存储与窗口控制在此）
// ----------------------------------------------------------------------------

// StartClipboardCapture 启动后台采集（platform 负责轮询与去重，结果经 ClipSink 落盘）。
func (a *App) StartClipboardCapture() { platform.StartCapture(a) }

// StopClipboardCapture 停止后台采集。
func (a *App) StopClipboardCapture() { platform.StopCapture() }

// ---- platform.ClipSink 实现：剪贴板采集结果的落盘与去重 ----

// SaveClipText 记录一条文本剪贴板（ClipSink 回调）。
// 只读写 meta.clipboard 单列，避免复制一次就全库重写（接口/用例越多越慢）。
func (a *App) SaveClipText(text, sig string) error {
	item := model.ClipItem{
		ID:        genClipID(),
		Type:      model.ClipTypeText,
		Text:      text,
		Time:      time.Now().Format("2006-01-02 15:04:05"),
		Timestamp: time.Now().UnixMilli(),
	}
	return a.pushClip(item)
}

// SaveClipImage 保存 PNG 字节到磁盘并记录一条图片剪贴板（ClipSink 回调）。
func (a *App) SaveClipImage(pngData []byte, w, h int, sig string) error {
	id := genClipID()
	rel := filepath.Join("clipimg", id+".png")
	full := filepath.Join(a.store.Dir(), rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(full, pngData, 0o644); err != nil {
		return err
	}
	item := model.ClipItem{
		ID:        id,
		Type:      model.ClipTypeImage,
		ImagePath: rel,
		Width:     w,
		Height:    h,
		Time:      time.Now().Format("2006-01-02 15:04:05"),
		Timestamp: time.Now().UnixMilli(),
	}
	return a.pushClip(item)
}

// ClipMonitorEnabled 返回设置中的剪贴板监听开关（ClipSink 回调）。
// 关闭时 platform 的轮询协程只比对序列号，不读取/编码剪贴板内容。
// 标志位在启动时初始化、并在每次 SaveData 后更新，因此轮询路径零 IO。
func (a *App) ClipMonitorEnabled() bool {
	return a.clipMonitor.Load()
}

// NotifyUpdated 通知前端剪贴板历史已更新（ClipSink 回调）。
func (a *App) NotifyUpdated() {
	a.Emit("apitool:clipboard-updated")
}

// pushClip 将条目插入历史最前，并按上限裁剪（上限来自设置）。
// 只读写 meta.clipboard 单列：剪贴板历史很小，而业务数据可能很大，
// 走整库 SaveData 会让「每复制一次」都付出与项目规模成正比的 IO 代价。
func (a *App) pushClip(item model.ClipItem) error {
	var clip model.ClipData
	if raw := a.store.LoadClipboardRaw(); raw != "" {
		_ = json.Unmarshal([]byte(raw), &clip)
	}
	if clip.History == nil {
		clip.History = []model.ClipItem{}
	}
	for i, it := range clip.History {
		if it.Type == item.Type && item.Type == model.ClipTypeText && it.Text == item.Text {
			clip.History = append(clip.History[:i:i], clip.History[i+1:]...)
			break
		}
	}
	clip.History = append([]model.ClipItem{item}, clip.History...)
	maxItems := int(a.clipMaxItems.Load())
	if maxItems <= 0 {
		maxItems = 200
	}
	if len(clip.History) > maxItems {
		for _, it := range clip.History[maxItems:] {
			if it.Type == model.ClipTypeImage && it.ImagePath != "" {
				_ = os.Remove(filepath.Join(a.store.Dir(), it.ImagePath))
			}
		}
		clip.History = clip.History[:maxItems]
	}
	b, err := json.Marshal(clip)
	if err != nil {
		return err
	}
	return a.store.SaveClipboardRaw(string(b))
}

func genClipID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// ---- 对外暴露给前端的 API ----

// loadClipData 读取剪贴板历史（只读 meta.clipboard 单列，不回读整库）。
func (a *App) loadClipData() model.ClipData {
	var clip model.ClipData
	if raw := a.store.LoadClipboardRaw(); raw != "" {
		_ = json.Unmarshal([]byte(raw), &clip)
	}
	if clip.History == nil {
		clip.History = []model.ClipItem{}
	}
	return clip
}

// GetClipItems 返回剪贴板历史（最新在前）。
func (a *App) GetClipItems() []model.ClipItem {
	return a.loadClipData().History
}

// CopyClipItem 复制指定历史项到系统剪贴板（文本直接写入；图片读取本地 PNG 写回 CF_DIB）。
func (a *App) CopyClipItem(id string) error {
	data := a.loadClipData()
	var item *model.ClipItem
	for i := range data.History {
		if data.History[i].ID == id {
			item = &data.History[i]
			break
		}
	}
	if item == nil {
		return fmt.Errorf("未找到该记录")
	}
	if item.Type == model.ClipTypeText {
		sig := fmt.Sprintf("%x", sha256.Sum256([]byte(item.Text)))
		platform.MarkWritten(sig, "")
		return a.ClipboardSetText(item.Text)
	}
	full := filepath.Join(a.store.Dir(), item.ImagePath)
	pngBytes, err := os.ReadFile(full)
	if err != nil {
		return err
	}
	sig := fmt.Sprintf("%x", sha256.Sum256(pngBytes))
	platform.MarkWritten("", sig)
	return platform.WriteClipboardImagePNG(pngBytes)
}

// GetClipImageData 返回指定图片历史项的 PNG 数据（base64），用于前端直接展示缩略图。
func (a *App) GetClipImageData(id string) map[string]string {
	for _, it := range a.loadClipData().History {
		if it.ID == id && it.Type == model.ClipTypeImage && it.ImagePath != "" {
			if m, ok := platform.ReadPNGFileBase64(filepath.Join(a.store.Dir(), it.ImagePath)); ok {
				return m
			}
		}
	}
	return map[string]string{}
}

// DeleteClipItem 删除一条历史记录（同时删除图片文件）。
func (a *App) DeleteClipItem(id string) error {
	clip := a.loadClipData()
	for i, it := range clip.History {
		if it.ID == id {
			if it.Type == model.ClipTypeImage && it.ImagePath != "" {
				_ = os.Remove(filepath.Join(a.store.Dir(), it.ImagePath))
			}
			clip.History = append(clip.History[:i], clip.History[i+1:]...)
			break
		}
	}
	b, err := json.Marshal(clip)
	if err != nil {
		return err
	}
	return a.store.SaveClipboardRaw(string(b))
}

// ---- 剪贴板历史窗口控制 ----
// 语义：剪贴板历史是一个「独立弹出浮层」。打开时显示主窗口并置顶居中；
// 关闭时取消置顶并把窗口隐藏回托盘。

// toggleClipboardWindow 切换剪贴板历史浮层的显隐。
func (a *App) toggleClipboardWindow() {
	if a.clipWinVisible {
		a.CloseClipboardWindow()
	} else {
		a.ShowClipboardWindow()
	}
}

// ShowClipboardWindow 显示剪贴板历史浮层（复用主窗口，置顶居中）。
func (a *App) ShowClipboardWindow() {
	a.clipWinVisible = true
	a.WindowShow()
	a.WindowUnminimise()
	a.WindowSetAlwaysOnTop(true)
	a.WindowCenter()
	a.Emit("apitool:show-clipboard-history")
}

// CloseClipboardWindow 关闭剪贴板历史浮层（取消置顶并隐藏窗口回托盘）。
func (a *App) CloseClipboardWindow() {
	a.clipWinVisible = false
	a.WindowSetAlwaysOnTop(false)
	a.Emit("apitool:hide-clipboard-history")
	a.WindowHide()
}
