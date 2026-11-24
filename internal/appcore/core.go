package appcore

import (
	"fmt"

	"apitool/internal/agent"
	"apitool/internal/bus"
	"apitool/internal/model"
	"apitool/internal/store/db"
	"apitool/internal/storeapi"
	"apitool/internal/testing"
	"apitool/internal/tool"
)

// DataStore 业务核心所需的存储能力（storeapi.Store 的子集视图，便于未来收窄）。
type DataStore = storeapi.Store

// App 业务核心实例。
// 每个客户端形态持有一份：桌面端全局一份；服务端每个登录用户一份（会话）。
// 并发模型：dataMu 串行化本实例的数据读写（服务端同用户并发 /rpc 与桌面端
// 托盘/剪贴板 goroutine 都可能并发写）。
type App struct {
	*agent.Manager
	*testing.Engine
	*tool.Service

	data DataStore
	bus  bus.Bus
	info HostInfo

	dataMu chan struct{}
}

// HostInfo 宿主元信息。
type HostInfo struct {
	Version string
	// Dir 数据根目录（图片/导出文件基准）。空则取 DataStore.Dir()。
	Dir string
}

// New 创建业务核心。ds 提供数据存储（必填），b 提供事件与系统能力（可为 nil，
// 此时事件推送为 no-op、系统能力返回不可用——服务端按需裁剪）。
func New(ds DataStore, b bus.Bus, info HostInfo) (*App, error) {
	if ds == nil {
		return nil, fmt.Errorf("appcore: DataStore 不能为空")
	}
	a := &App{
		data:   ds,
		bus:    b,
		info:   info,
		dataMu: make(chan struct{}, 1),
	}
	a.Manager = agent.NewManager(a, a)
	a.Engine = testing.NewEngine(a, nil)
	a.Service = &tool.Service{}
	return a, nil
}

func (a *App) lock() { a.dataMu <- struct{}{} }
func (a *App) unlock() { <-a.dataMu }

// ---- agent.Host 实现 ----

// Store 返回存储句柄（agent.Host 要求）。
func (a *App) Store() storeapi.Store { return a.data }

// ReadData 返回全部应用数据（agent.Host / testing.Host）。
func (a *App) ReadData() model.AppData { return a.data.GetData() }

// SendRequest 执行 HTTP 请求（agent.Host / testing.Host）。
func (a *App) SendRequest(spec model.RequestSpec) model.ResponseData {
	return sendRequest(spec)
}

// AppVersion 返回宿主版本号（agent.Host / testing.Host）。
func (a *App) AppVersion() string { return a.info.Version }

// ---- testing.Host 补充 ----

// Emit 向前端推送事件。
func (a *App) Emit(event string, data ...interface{}) {
	if a.bus != nil {
		a.bus.Emit(event, data...)
	}
}

// SaveFileDialog 保存文件对话框（testing.Host / bus.Bus 要求）。
// 桌面端由 bus（wailsrt）实现；服务端 bus 无对话框能力，返回不可用。
func (a *App) SaveFileDialog(opts DialogOptions) (string, error) {
	if a.bus != nil {
		return a.bus.SaveFileDialog(toBusDialog(opts))
	}
	return "", errNoDialog
}

// OpenFileDialog 打开文件对话框（bus.Bus 要求）。
func (a *App) OpenFileDialog(opts DialogOptions) (string, error) {
	if a.bus != nil {
		return a.bus.OpenFileDialog(toBusDialog(opts))
	}
	return "", errNoDialog
}

// OpenDirectoryDialog 选择目录对话框（bus.Bus 要求）。
func (a *App) OpenDirectoryDialog(opts DialogOptions) (string, error) {
	if a.bus != nil {
		return a.bus.OpenDirectoryDialog(toBusDialog(opts))
	}
	return "", errNoDialog
}

// ---- bus 能力透传（供桌面壳与服务端共用）----

// BrowserOpenURL 用系统浏览器打开 URL。
func (a *App) BrowserOpenURL(url string) {
	if a.bus != nil {
		a.bus.BrowserOpenURL(url)
	}
}

// ClipboardGetText 读取剪贴板文本。
func (a *App) ClipboardGetText() (string, error) {
	if a.bus == nil {
		return "", errNoSystem
	}
	return a.bus.ClipboardGetText()
}

// ClipboardSetText 写入剪贴板文本。
func (a *App) ClipboardSetText(text string) error {
	if a.bus == nil {
		return errNoSystem
	}
	return a.bus.ClipboardSetText(text)
}

// Quit 退出应用（仅桌面端有意义）。
func (a *App) Quit() {
	if a.bus != nil {
		a.bus.Quit()
	}
}

// ---- 窗口控制（bus.Bus 要求；服务端 bus 为 no-op）----

// WindowShow 显示主窗口。
func (a *App) WindowShow() {
	if a.bus != nil {
		a.bus.WindowShow()
	}
}

// WindowHide 隐藏主窗口。
func (a *App) WindowHide() {
	if a.bus != nil {
		a.bus.WindowHide()
	}
}

// WindowUnminimise 取消最小化。
func (a *App) WindowUnminimise() {
	if a.bus != nil {
		a.bus.WindowUnminimise()
	}
}

// WindowCenter 居中主窗口。
func (a *App) WindowCenter() {
	if a.bus != nil {
		a.bus.WindowCenter()
	}
}

// WindowSetAlwaysOnTop 设置主窗口置顶。
func (a *App) WindowSetAlwaysOnTop(b bool) {
	if a.bus != nil {
		a.bus.WindowSetAlwaysOnTop(b)
	}
}

// ---- 数据读写（前端绑定的主路径）----

// readData 内部读。
func (a *App) readData() model.AppData {
	a.lock()
	defer a.unlock()
	return a.data.GetData()
}

// LoadData 加载全部数据。
func (a *App) LoadData() model.AppData { return a.readData() }

// SaveData 保存全部数据。
func (a *App) SaveData(data model.AppData) error {
	a.lock()
	defer a.unlock()
	return a.data.SaveData(data)
}

// GetDataFilePath 返回数据文件路径（诊断展示）。
func (a *App) GetDataFilePath() string { return a.data.Path() }

// GetAgentBootstrap 返回「Agent 对话网页」启动所需的最小数据集。
func (a *App) GetAgentBootstrap() map[string]interface{} {
	d := a.readData()
	return map[string]interface{}{
		"settings":    d.Settings,
		"connections": d.Plugins.Connections,
	}
}

// ---- 独立存储域转发（agent 数据/技能/剪贴板）----

// SaveAgentRaw 仅持久化 agent 数据。
func (a *App) SaveAgentRaw(raw string) error { return a.data.SaveAgentRaw(raw) }

// LoadAgentRaw 读取 agent 数据。
func (a *App) LoadAgentRaw() string { return a.data.LoadAgentRaw() }

// LoadSkills 读取技能列表。
func (a *App) LoadSkills() []db.AgentSkill { return a.data.LoadSkills() }

// SaveSkills 保存技能列表。
func (a *App) SaveSkills(skills []db.AgentSkill) error { return a.data.SaveSkills(skills) }

// LoadDBAnalysis 读取数据库连接分析数据。
func (a *App) LoadDBAnalysis() *db.DBAnalysisSnapshot { return a.data.LoadDBAnalysis() }

// SaveDBAnalysis 保存数据库连接分析数据。
func (a *App) SaveDBAnalysis(snap *db.DBAnalysisSnapshot) error {
	return a.data.SaveDBAnalysis(snap)
}

// ClipHistory 返回剪贴板历史（前端 ClipHistory 契约）。
func (a *App) ClipHistory() []model.ClipItem {
	d := a.readData()
	return d.Clipboard.History
}

// ---- 错误 ----

type coreError string

func (e coreError) Error() string { return string(e) }

const (
	errNoDialog coreError = "当前环境不支持文件对话框"
	errNoSystem coreError = "当前环境不支持系统能力"
)
