package main

import (
	"apitool/internal/agent"
	"apitool/internal/ai"
	"apitool/internal/bus"
	"apitool/internal/capture"
	"apitool/internal/doc"
	"apitool/internal/httpx"
	"apitool/internal/jsonutil"
	"apitool/internal/model"
	"apitool/internal/platform"
	"apitool/internal/plugins"
	"apitool/internal/share"
	"apitool/internal/sniff"
	"apitool/internal/store"
	"apitool/internal/stress"
	syncsrv "apitool/internal/sync"
	"apitool/internal/testing"
	"apitool/internal/tool"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	*agent.Manager  // 嵌入 agent 模块，提升其 Agent*/RunAgent/MCP* 等导出方法供 Wails 绑定
	*testing.Engine // 嵌入测试引擎，提升其 Generate*/RunTest*/ExportTestReport 等导出方法供 Wails 绑定
	*tool.Service   // 嵌入通用工具，提升其 ToolHash/ToolHmac/ToolCipher 等导出方法供 Wails 绑定
	ctx             context.Context
	store           *store.Store
	dataFile        string
	syncDir         string
	sniffMgr        *sniff.Manager // MITM 抓包管理器
	mu              sync.Mutex
	windowVisible   bool // 主窗口当前是否可见（托盘显隐用）
	clipWinVisible  bool // 剪贴板历史浮层当前是否可见
	quitting        bool // 是否正在主动退出（绕过 beforeClose 的隐藏逻辑）
}

// Store 返回应用数据存储（实现 agent.Host 接口）
func (a *App) Store() *store.Store { return a.store }

// ReadData 返回应用数据（实现 agent.Host 接口）
func (a *App) ReadData() model.AppData { return a.readData() }

// AppVersion 返回应用版本号（实现 agent.Host 接口）
func (a *App) AppVersion() string { return AppVersion }

// GenerateDescriptions 使用 AI 为字段自动生成描述（转发到 internal/ai，保持 Wails 绑定签名）
func (a *App) GenerateDescriptions(apiName string, apiDesc string, fields []*model.Field) ([]*model.Field, error) {
	return ai.GenerateDescriptions(a, apiName, apiDesc, fields)
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// AppVersion 客户端版本号（用于升级检测与本地配置标记）
const AppVersion = "1.0.0"

// DefaultUpdateURL 默认升级服务地址（本地同步服务）
const DefaultUpdateURL = "http://127.0.0.1" + syncsrv.DefaultSyncAddr

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
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
	// 初始化 agent 模块（MCP/技能/会话/运行），注入宿主能力与事件总线
	a.Manager = agent.NewManager(a, a, filepath.Join(dataDir, "agent.json"))
	// 初始化测试引擎（用例生成/执行/报告导出），注入宿主能力与事件总线
	a.Engine = testing.NewEngine(a, ctx)
	// 初始化通用工具服务（Hash/HMAC/Cipher），嵌入 App 供 Wails 绑定
	a.Service = &tool.Service{}
	// 初始化配置：本地 JSON 不存在时，自动生成默认配置文件
	if _, err := os.Stat(a.dataFile); err != nil {
		_ = a.SaveData(a.store.GetData())
	}
	// 仅加载/生成捕获服务 Token；捕获服务不再自动启动，改由用户在「请求捕获」页面手动开启
	capture.LoadOrCreateToken(a.syncDir)
	// 启动系统托盘（独立于主界面，提供显隐/测试/退出）
	a.windowVisible = true
	go a.startTray()
	// 安装系统级全局快捷键（即使窗口失焦也能调出剪贴板历史）
	platform.SetHotkeyHandlers(a.toggleClipboardWindow)
	go platform.StartGlobalHotkey()
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

// shutdown 在应用退出时清理资源（停止采集）。托盘由 systray.Quit 自行退出。
func (a *App) shutdown(ctx context.Context) {
	a.StopClipboardCapture()
}

// beforeClose 在用户点击窗口关闭/Alt+F4 时触发。
// 返回 true 表示阻止退出，改为隐藏窗口并驻留系统托盘，实现「关闭即最小化到托盘」。
// 使用 WindowHide 而非 WindowMinimise：隐藏后窗口不在任务栏保留按钮，
// 托盘图标仍然存在，用户可通过托盘菜单「显示主窗口」恢复。
// 全局快捷键（Ctrl+Shift+V / Ctrl+`）由 Go 端 WH_KEYBOARD_LL 钩子处理，
// 不依赖 WebView 存活，因此隐藏窗口不影响剪贴板历史弹出。
func (a *App) beforeClose(ctx context.Context) (prevent bool) {
	// 主动退出（托盘「退出」）时跳过隐藏逻辑，直接放行关闭
	if a.quitting {
		return false
	}
	// 用户点击窗口关闭：隐藏到托盘而非真正退出。
	// 若剪贴板浮层处于打开状态，一并关闭并复位状态，避免下次 toggle 判断错位。
	if a.clipWinVisible {
		a.clipWinVisible = false
		a.WindowSetAlwaysOnTop(false)
		runtime.EventsEmit(a.ctx, "apitool:hide-clipboard-history")
	}
	a.WindowHide()
	a.windowVisible = false
	return true
}

// GetClipboardText 读取系统剪贴板文本（供前端轮询记录历史）
func (a *App) GetClipboardText() (string, error) {
	return runtime.ClipboardGetText(a.ctx)
}

// SetClipboardText 写入系统剪贴板
func (a *App) SetClipboardText(text string) error {
	return runtime.ClipboardSetText(a.ctx, text)
}

// readData 从磁盘加载并反序列化全部数据（兼容与默认值补全逻辑见 internal/store）。
func (a *App) readData() model.AppData {
	return a.store.GetData()
}

// LoadData 加载全部数据（上次保存的接口信息）
func (a *App) LoadData() model.AppData {
	return a.readData()
}

// ParseFields 将 JSON 文本解析为字段树（转发到 jsonutil）
func (a *App) ParseFields(jsonStr string, existing []*model.Field) ([]*model.Field, error) {
	return jsonutil.ParseFields(jsonStr, existing)
}

// FormatJSON 格式化 JSON 文本（转发到 jsonutil）
func (a *App) FormatJSON(jsonStr string) (string, error) {
	return jsonutil.FormatJSON(jsonStr)
}

// SendRequest 执行 HTTP 请求（转发到 httpx）
func (a *App) SendRequest(spec model.RequestSpec) model.ResponseData {
	return httpx.SendRequest(spec)
}

// ClearTestData 一键清空当前项目的测试数据。
// scope 取值：
//   - "cases"  仅清空测试用例
//   - "plans"  仅清空测试计划
//   - "reports"仅清空测试报告
//   - "all"    清空以上全部（默认）
//
// 返回被清空的数据总条数。
func (a *App) ClearTestData(projectID string, scope string) (int, error) {
	data := a.readData()
	idx := store.ActiveProjectIndex(data)
	if idx < 0 {
		return 0, fmt.Errorf("没有可用的项目")
	}
	if projectID != "" {
		found := false
		for i, p := range data.Projects {
			if p.ID == projectID {
				idx = i
				found = true
				break
			}
		}
		if !found {
			return 0, fmt.Errorf("项目不存在：%s", projectID)
		}
	}
	if scope == "" {
		scope = "all"
	}
	removed := 0
	proj := &data.Projects[idx]
	switch scope {
	case "cases":
		removed = len(proj.TestCases)
		proj.TestCases = []model.TestCase{}
	case "plans":
		removed = len(proj.TestPlans)
		proj.TestPlans = []model.TestPlan{}
	case "reports":
		removed = len(proj.TestReports)
		proj.TestReports = []model.TestReport{}
	case "all":
		removed = len(proj.TestCases) + len(proj.TestPlans) + len(proj.TestReports)
		proj.TestCases = []model.TestCase{}
		proj.TestPlans = []model.TestPlan{}
		proj.TestReports = []model.TestReport{}
	default:
		return 0, fmt.Errorf("未知的清空范围：%s", scope)
	}
	proj.UpdatedAt = time.Now().Format(time.RFC3339)
	if err := a.SaveData(data); err != nil {
		return 0, err
	}
	return removed, nil
}

// SaveData 保存全部数据
func (a *App) SaveData(data model.AppData) error {
	return a.store.SaveData(data)
}

// GetDataFilePath 返回数据文件路径
func (a *App) GetDataFilePath() string {
	return a.store.Path()
}

// CopyToClipboard 复制文本到剪贴板
func (a *App) CopyToClipboard(text string) error {
	return runtime.ClipboardSetText(a.ctx, text)
}

// ClipHistory 返回剪贴板历史（供原生菜单 / 托盘菜单读取）。
func (a *App) ClipHistory() []model.ClipItem {
	data := a.LoadData()
	return data.Clipboard.History
}

// ClearClipHistory 清空剪贴板历史并落盘（同时删除图片文件）。
func (a *App) ClearClipHistory() {
	data := a.LoadData()
	for _, it := range data.Clipboard.History {
		if it.Type == model.ClipTypeImage && it.ImagePath != "" {
			_ = os.Remove(filepath.Join(a.store.Dir(), it.ImagePath))
		}
	}
	data.Clipboard.History = nil
	_ = a.SaveData(data)
}

// ChatMessage 简单的聊天消息结构（供 CallAI 使用），别名指向 internal/ai。
type ChatMessage = ai.ChatMessage

// CallAIArgs CallAI 入参（由前端传入已解析好的配置，避免在 Go 端读取前端 store）
type CallAIArgs struct {
	BaseURL  string        `json:"baseUrl"`
	APIKey   string        `json:"apiKey"`
	Model    string        `json:"model"`
	Timeout  int           `json:"timeoutSec"`
	Messages []ChatMessage `json:"messages"`
}

// CallAI 由 Go 后端代发 AI 请求，规避前端 webview 的 CORS 限制。
// 返回模型回复的文本内容；失败时返回错误。
func (a *App) CallAI(args CallAIArgs) (string, error) {
	base := strings.TrimSpace(args.BaseURL)
	if base == "" {
		return "", fmt.Errorf("未配置 AI 接口地址（设置 → AI 配置）")
	}
	if args.APIKey == "" {
		return "", fmt.Errorf("未配置 AI API Key（设置 → AI 配置）")
	}
	model := strings.TrimSpace(args.Model)
	// 委托 internal/ai 统一处理 URL 拼接、超时与错误解析（与 Chat 共用底层实现）
	return ai.ChatRaw(base, args.APIKey, model, args.Messages, args.Timeout)
}

// OpenInBrowser 使用系统浏览器打开
func (a *App) OpenInBrowser(url string) {
	runtime.BrowserOpenURL(a.ctx, url)
}

// ExportDoc 导出文档（转发到 doc 包）
func (a *App) ExportDoc(dirID string, apiID string, format string) (string, error) {
	return doc.ExportDoc(a.ctx, a.store, AppVersion, DefaultUpdateURL, dirID, apiID, format)
}

// ImportDoc 选择并导入接口文档（转发到 doc 包）
func (a *App) ImportDoc() (string, error) {
	return doc.ImportDoc(a.ctx, a.store, AppVersion, DefaultUpdateURL, "")
}

// ShareDoc 生成 HTML 文档并用浏览器打开（转发到 doc 包）
func (a *App) ShareDoc(dirID string, apiID string) (string, error) {
	return doc.ShareDoc(a.ctx, a.store, AppVersion, DefaultUpdateURL, dirID, apiID)
}

// CopyDocMarkdown 复制 Markdown 文档到剪贴板（转发到 doc 包）
func (a *App) CopyDocMarkdown(dirID string, apiID string) error {
	return doc.CopyDocMarkdown(a.ctx, a.store, AppVersion, DefaultUpdateURL, dirID, apiID)
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

// GetVersion 返回当前客户端版本号
func (a *App) GetVersion() string {
	return AppVersion
}

// CheckUpdate 调用升级服务地址的 /version 接口检测是否有新版本。
// 升级服务需返回 JSON：{ "version": "1.2.0", "url": "...", "notes": "..." }
func (a *App) CheckUpdate() CheckUpdateResult {
	data := a.readData()
	res := CheckUpdateResult{Current: data.Settings.Version}
	if data.Settings.Version == "" {
		res.Current = AppVersion
	}
	res.Latest = res.Current

	base := strings.TrimSpace(data.Settings.UpdateURL)
	if base == "" {
		base = DefaultUpdateURL
	}
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	base = strings.TrimRight(base, "/")

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(base + "/version")
	if err != nil {
		res.Error = "无法连接升级服务：" + err.Error()
		return res
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		res.Error = fmt.Sprintf("升级服务返回状态 %d", resp.StatusCode)
		return res
	}
	var info UpdateInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		res.Error = "升级服务返回数据解析失败：" + err.Error()
		return res
	}
	if info.Version == "" {
		res.Error = "升级服务未返回版本号"
		return res
	}
	res.Latest = info.Version
	res.URL = info.URL
	res.Notes = info.Notes
	res.HasNew = compareVersion(info.Version, res.Current) > 0
	return res
}

// compareVersion 比较两个语义化版本号（仅比较数值段），a>b 返回 1，相等 0，a<b 返回 -1
func compareVersion(a, b string) int {
	as := strings.Split(strings.TrimSpace(a), ".")
	bs := strings.Split(strings.TrimSpace(b), ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		ai, bi := 0, 0
		if i < len(as) {
			ai = atoiSafe(as[i])
		}
		if i < len(bs) {
			bi = atoiSafe(bs[i])
		}
		if ai > bi {
			return 1
		}
		if ai < bi {
			return -1
		}
	}
	return 0
}

func atoiSafe(s string) int {
	v := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		v = v*10 + int(c-'0')
	}
	return v
}

// ----------------------------------------------------------------------------
// bus.Bus 实现：App 作为业务子包与 Wails 运行时之间的桥接。
// 子包通过 bus.Bus 接口收发事件/弹窗/剪贴板，不再依赖 runtime 或 *App 具体类型。
// ----------------------------------------------------------------------------

// Assert 编译期检查 App 实现了 bus.Bus。
var _ bus.Bus = (*App)(nil)

// Emit 向前端发送事件。
func (a *App) Emit(event string, data ...interface{}) {
	runtime.EventsEmit(a.ctx, event, data...)
}

// Quit 退出应用。
func (a *App) Quit() {
	runtime.Quit(a.ctx)
}

// WindowShow 显示主窗口。
func (a *App) WindowShow() {
	runtime.WindowShow(a.ctx)
}

// WindowHide 隐藏主窗口。
func (a *App) WindowHide() {
	runtime.WindowHide(a.ctx)
}

// WindowUnminimise 还原最小化窗口。
func (a *App) WindowUnminimise() {
	runtime.WindowUnminimise(a.ctx)
}

// WindowCenter 居中主窗口。
func (a *App) WindowCenter() {
	runtime.WindowCenter(a.ctx)
}

// WindowSetAlwaysOnTop 设置主窗口是否置顶。
func (a *App) WindowSetAlwaysOnTop(b bool) {
	runtime.WindowSetAlwaysOnTop(a.ctx, b)
}

// BrowserOpenURL 用系统浏览器打开 URL（实现 bus.Bus）。
func (a *App) BrowserOpenURL(url string) {
	runtime.BrowserOpenURL(a.ctx, url)
}

// ClipboardGetText 读取系统剪贴板文本（实现 bus.Bus）。
func (a *App) ClipboardGetText() (string, error) {
	return runtime.ClipboardGetText(a.ctx)
}

// ClipboardSetText 写入系统剪贴板（实现 bus.Bus）。
func (a *App) ClipboardSetText(text string) error {
	return runtime.ClipboardSetText(a.ctx, text)
}

// SaveFileDialog 弹出保存文件对话框（实现 bus.Bus）。
func (a *App) SaveFileDialog(opts runtime.SaveDialogOptions) (string, error) {
	return runtime.SaveFileDialog(a.ctx, opts)
}

// OpenFileDialog 弹出打开文件对话框（实现 bus.Bus）。
func (a *App) OpenFileDialog(opts runtime.OpenDialogOptions) (string, error) {
	return runtime.OpenFileDialog(a.ctx, opts)
}

// OpenDirectoryDialog 弹出选择目录对话框（实现 bus.Bus）。
func (a *App) OpenDirectoryDialog(opts runtime.OpenDialogOptions) (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, opts)
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
func (a *App) SniffExportOpenAPI(id, title string) (string, error) {
	if a.sniffMgr == nil {
		return "", fmt.Errorf("抓包模块未初始化")
	}
	return a.sniffMgr.ExportOpenAPI(id, title)
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
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择根证书文件（FiddlerRoot.cer 等）",
		Filters: []runtime.FileFilter{
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
// 剪贴板 / 热键（底层在 internal/platform，业务存储与窗口控制在此）
// ----------------------------------------------------------------------------

// StartClipboardCapture 启动后台采集（platform 负责轮询与去重，结果经 ClipSink 落盘）。
func (a *App) StartClipboardCapture() { platform.StartCapture(a) }

// StopClipboardCapture 停止后台采集。
func (a *App) StopClipboardCapture() { platform.StopCapture() }

// ---- platform.ClipSink 实现：剪贴板采集结果的落盘与去重 ----

// SaveClipText 记录一条文本剪贴板（ClipSink 回调）。
func (a *App) SaveClipText(text, sig string) error {
	data := a.LoadData()
	item := model.ClipItem{
		ID:        genClipID(),
		Type:      model.ClipTypeText,
		Text:      text,
		Time:      time.Now().Format("2006-01-02 15:04:05"),
		Timestamp: time.Now().UnixMilli(),
	}
	a.pushClip(&data, item)
	return a.SaveData(data)
}

// SaveClipImage 保存 PNG 字节到磁盘并记录一条图片剪贴板（ClipSink 回调）。
func (a *App) SaveClipImage(pngData []byte, w, h int, sig string) error {
	id := genClipID()
	rel := filepath.Join("clipimg", id+".png")
	full := filepath.Join(a.store.Dir(), rel)
	if err := os.WriteFile(full, pngData, 0o644); err != nil {
		return err
	}
	data := a.LoadData()
	item := model.ClipItem{
		ID:        id,
		Type:      model.ClipTypeImage,
		ImagePath: rel,
		Width:     w,
		Height:    h,
		Time:      time.Now().Format("2006-01-02 15:04:05"),
		Timestamp: time.Now().UnixMilli(),
	}
	a.pushClip(&data, item)
	return a.SaveData(data)
}

// NotifyUpdated 通知前端剪贴板历史已更新（ClipSink 回调）。
func (a *App) NotifyUpdated() {
	runtime.EventsEmit(a.ctx, "apitool:clipboard-updated")
}

// pushClip 将条目插入历史最前，并按上限裁剪（上限来自设置）。
func (a *App) pushClip(data *model.AppData, item model.ClipItem) {
	for i, it := range data.Clipboard.History {
		if it.Type == item.Type {
			if item.Type == model.ClipTypeText && it.Text == item.Text {
				data.Clipboard.History = append(data.Clipboard.History[:i:i], data.Clipboard.History[i+1:]...)
				break
			}
		}
	}
	data.Clipboard.History = append([]model.ClipItem{item}, data.Clipboard.History...)
	maxItems := data.Settings.Clipboard.MaxItems
	if maxItems <= 0 {
		maxItems = 200
	}
	if len(data.Clipboard.History) > maxItems {
		for _, it := range data.Clipboard.History[maxItems:] {
			if it.Type == model.ClipTypeImage && it.ImagePath != "" {
				_ = os.Remove(filepath.Join(a.store.Dir(), it.ImagePath))
			}
		}
		data.Clipboard.History = data.Clipboard.History[:maxItems]
	}
}

func genClipID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// ---- 对外暴露给前端的 API ----

// GetClipItems 返回剪贴板历史（最新在前）。
func (a *App) GetClipItems() []model.ClipItem {
	data := a.LoadData()
	return data.Clipboard.History
}

// CopyClipItem 复制指定历史项到系统剪贴板（文本直接写入；图片读取本地 PNG 写回 CF_DIB）。
func (a *App) CopyClipItem(id string) error {
	data := a.LoadData()
	var item *model.ClipItem
	for i := range data.Clipboard.History {
		if data.Clipboard.History[i].ID == id {
			item = &data.Clipboard.History[i]
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
	data := a.LoadData()
	for _, it := range data.Clipboard.History {
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
	data := a.LoadData()
	for i, it := range data.Clipboard.History {
		if it.ID == id {
			if it.Type == model.ClipTypeImage && it.ImagePath != "" {
				_ = os.Remove(filepath.Join(a.store.Dir(), it.ImagePath))
			}
			data.Clipboard.History = append(data.Clipboard.History[:i], data.Clipboard.History[i+1:]...)
			break
		}
	}
	return a.SaveData(data)
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
	runtime.EventsEmit(a.ctx, "apitool:show-clipboard-history")
}

// CloseClipboardWindow 关闭剪贴板历史浮层（取消置顶并隐藏窗口回托盘）。
func (a *App) CloseClipboardWindow() {
	a.clipWinVisible = false
	a.WindowSetAlwaysOnTop(false)
	runtime.EventsEmit(a.ctx, "apitool:hide-clipboard-history")
	a.WindowHide()
}

// ----------------------------------------------------------------------------
// 外部连接插件转发层（实现在 internal/plugins：Redis/ES/SSH/SFTP/FTP/DB）
// ----------------------------------------------------------------------------

// PluginConnect 测试连接（按分类做最小连通性/登录校验）。
func (a *App) PluginConnect(conn model.PluginConn) plugins.PluginOpResult {
	return plugins.PluginTest(conn)
}

// PluginDisconnect 断开连接（当前连接采用 TTL 连接池自动回收，这里直接返回成功）。
func (a *App) PluginDisconnect(conn model.PluginConn) plugins.PluginOpResult {
	return plugins.PluginOpResult{Ok: true, Info: "已断开"}
}

// PluginTest 连接测试。
func (a *App) PluginTest(conn model.PluginConn) plugins.PluginOpResult {
	return plugins.PluginTest(conn)
}

// PluginRedisKeys 列举匹配模式的 Redis 键。
func (a *App) PluginRedisKeys(conn model.PluginConn, pattern string, db int) ([]plugins.RedisKey, error) {
	return plugins.PluginRedisKeys(conn, pattern, db)
}

// PluginRedisValue 获取 Redis 键的值。
func (a *App) PluginRedisValue(conn model.PluginConn, key string, db int) (plugins.RedisValue, error) {
	return plugins.PluginRedisValue(conn, key, db)
}

// PluginRedisSet 设置 Redis 字符串键。
func (a *App) PluginRedisSet(conn model.PluginConn, key, value string, ttl, db int) error {
	return plugins.PluginRedisSet(conn, key, value, ttl, db)
}

// PluginRedisDel 删除 Redis 键。
func (a *App) PluginRedisDel(conn model.PluginConn, key string, db int) error {
	return plugins.PluginRedisDel(conn, key, db)
}

// PluginRedisTTL 返回 Redis 键过期秒数。
func (a *App) PluginRedisTTL(conn model.PluginConn, key string, db int) (int, error) {
	return plugins.PluginRedisTTL(conn, key, db)
}

// PluginESIndices 列出 ES 索引。
func (a *App) PluginESIndices(conn model.PluginConn) ([]plugins.ESIndex, error) {
	return plugins.PluginESIndices(conn)
}

// PluginESSearch 执行 ES 查询。
func (a *App) PluginESSearch(conn model.PluginConn, index, query string) (string, error) {
	return plugins.PluginESSearch(conn, index, query)
}

// PluginSSHExec 在远端执行命令（XShell 风格）。
func (a *App) PluginSSHExec(conn model.PluginConn, command string) (string, error) {
	return plugins.PluginSSHExec(conn, command)
}

// PluginSSHOpen 建立带 PTY 的持久 SSH 会话，输出通过事件实时推送前端。
func (a *App) PluginSSHOpen(conn model.PluginConn) (string, error) {
	return plugins.PluginSSHOpen(a, conn)
}

// PluginSSHInput 向 SSH 会话写入数据。
func (a *App) PluginSSHInput(id string, data string) error {
	return plugins.PluginSSHInput(id, data)
}

// PluginSSHClose 关闭 SSH 会话。
func (a *App) PluginSSHClose(id string) error {
	return plugins.PluginSSHClose(id)
}

// PluginSSHResize 调整 SSH 会话伪终端尺寸。
func (a *App) PluginSSHResize(id string, rows, cols int) error {
	return plugins.PluginSSHResize(id, rows, cols)
}

// PluginSFTPList 列出远端目录。
func (a *App) PluginSFTPList(conn model.PluginConn, dir string) ([]plugins.FileInfo, error) {
	return plugins.PluginSFTPList(conn, dir)
}

// PluginSFTPRead 读取远端文件文本。
func (a *App) PluginSFTPRead(conn model.PluginConn, path string) (string, error) {
	return plugins.PluginSFTPRead(conn, path)
}

// PluginSFTPWrite 写入远端文件。
func (a *App) PluginSFTPWrite(conn model.PluginConn, path, content string) error {
	return plugins.PluginSFTPWrite(conn, path, content)
}

// PluginSFTPUploadB64 上传本地文件（base64）。
func (a *App) PluginSFTPUploadB64(conn model.PluginConn, remoteDir, name, b64 string) error {
	return plugins.PluginSFTPUploadB64(conn, remoteDir, name, b64)
}

// PluginSFTPRename 重命名 / 移动远端文件。
func (a *App) PluginSFTPRename(conn model.PluginConn, oldPath, newPath string) error {
	return plugins.PluginSFTPRename(conn, oldPath, newPath)
}

// PluginSFTPDownload 下载远端文件到本地。
func (a *App) PluginSFTPDownload(conn model.PluginConn, path, name string) (string, error) {
	return plugins.PluginSFTPDownload(a, conn, path, name)
}

// PluginSFTPMkdir 创建远端目录。
func (a *App) PluginSFTPMkdir(conn model.PluginConn, path string) error {
	return plugins.PluginSFTPMkdir(conn, path)
}

// PluginSFTPDelete 删除远端文件/目录。
func (a *App) PluginSFTPDelete(conn model.PluginConn, path string) error {
	return plugins.PluginSFTPDelete(conn, path)
}

// PluginFTPList 列出 FTP 目录。
func (a *App) PluginFTPList(conn model.PluginConn, dir string) ([]plugins.FileInfo, error) {
	return plugins.PluginFTPList(conn, dir)
}

// PluginFTPRead 读取 FTP 文件文本。
func (a *App) PluginFTPRead(conn model.PluginConn, path string) (string, error) {
	return plugins.PluginFTPRead(conn, path)
}

// PluginFTPWrite 上传 FTP 文件。
func (a *App) PluginFTPWrite(conn model.PluginConn, path, content string) error {
	return plugins.PluginFTPWrite(conn, path, content)
}

// PluginFTPUploadB64 上传本地文件（base64）。
func (a *App) PluginFTPUploadB64(conn model.PluginConn, remoteDir, name, b64 string) error {
	return plugins.PluginFTPUploadB64(conn, remoteDir, name, b64)
}

// PluginFTPDownload 下载 FTP 文件到本地。
func (a *App) PluginFTPDownload(conn model.PluginConn, path, name string) (string, error) {
	return plugins.PluginFTPDownload(a, conn, path, name)
}

// PluginFTPRename 重命名 / 移动 FTP 文件。
func (a *App) PluginFTPRename(conn model.PluginConn, oldPath, newPath string) error {
	return plugins.PluginFTPRename(conn, oldPath, newPath)
}

// PluginFTPMkdir 创建 FTP 目录。
func (a *App) PluginFTPMkdir(conn model.PluginConn, path string) error {
	return plugins.PluginFTPMkdir(conn, path)
}

// PluginFTPDelete 删除 FTP 文件/目录。
func (a *App) PluginFTPDelete(conn model.PluginConn, path string) error {
	return plugins.PluginFTPDelete(conn, path)
}

// PluginSetClipboard 写入系统剪贴板。
func (a *App) PluginSetClipboard(text string) error {
	return plugins.PluginSetClipboard(a, text)
}

// PluginDBTest 数据库连接测试。
func (a *App) PluginDBTest(conn model.PluginConn) plugins.PluginOpResult {
	return plugins.PluginDBTest(conn)
}

// PluginDBDatabases 列出数据库。
func (a *App) PluginDBDatabases(conn model.PluginConn) (plugins.DBInfo, error) {
	return plugins.PluginDBDatabases(conn)
}

// PluginDBTables 列出表。
func (a *App) PluginDBTables(conn model.PluginConn, database string) ([]plugins.DBTable, error) {
	return plugins.PluginDBTables(conn, database)
}

// PluginDBQuery 执行查询。
func (a *App) PluginDBQuery(conn model.PluginConn, req plugins.DBQueryReq) (*plugins.DBRow, error) {
	return plugins.PluginDBQuery(conn, req)
}

// PluginDBExec 执行 DML/DDL。
func (a *App) PluginDBExec(conn model.PluginConn, req plugins.DBExecReq) (int64, error) {
	return plugins.PluginDBExec(conn, req)
}
