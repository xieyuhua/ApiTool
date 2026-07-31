package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	ctx          context.Context
	dataFile     string
	syncDir      string
	mu           sync.Mutex
	captureToken string // 浏览器扩展回传鉴权 Token（持久化）
	windowVisible bool  // 主窗口当前是否可见（托盘显隐用）
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// AppVersion 客户端版本号（用于升级检测与本地配置标记）
const AppVersion = "1.0.0"

// DefaultUpdateURL 默认升级服务地址（本地同步服务，端口与 defaultSyncAddr 一致）
const DefaultUpdateURL = "http://127.0.0.1" + defaultSyncAddr

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	dataDir := filepath.Join(dir, "apitool")
	_ = os.MkdirAll(dataDir, 0o755)
	a.dataFile = filepath.Join(dataDir, "data.json")
	a.syncDir = filepath.Join(dataDir, "syncserver")
	// 初始化配置：本地 JSON 不存在时，自动生成默认配置文件
	if _, err := os.Stat(a.dataFile); err != nil {
		_ = a.SaveData(defaultData())
	}
	// 仅加载/生成捕获服务 Token；捕获服务不再自动启动，改由用户在「请求捕获」页面手动开启
	a.loadOrCreateCaptureToken()
	// 启动系统托盘（独立于主界面，提供显隐/测试/退出）
	a.windowVisible = true
	go a.startTray()
	// 安装系统级全局快捷键（即使窗口失焦也能调出剪贴板历史）
	go a.startGlobalHotkey()
}

// beforeClose 在用户点击窗口关闭/Alt+F4 时触发。
// 返回 true 表示阻止退出，改为最小化窗口并驻留系统托盘，实现「关闭即最小化到托盘」。
// 注意：使用 WindowMinimise 而非 WindowHide，使 WebView 始终存活、全局快捷键事件可被前端接收，
// 托盘态下按快捷键仍可正常弹出剪贴板。仅当用户通过托盘菜单「退出」时才真正退出。
func (a *App) beforeClose(ctx context.Context) (prevent bool) {
	runtime.WindowMinimise(a.ctx)
	a.windowVisible = false
	return true
}

func defaultData() AppData {
	return AppData{
		Projects: []Project{
			{
				ID:        "default",
				Name:      "默认项目",
				Dirs:      []Directory{},
				Apis:      []ApiInfo{},
				Environments: []Environment{},
				UpdatedAt: time.Now().Format(time.RFC3339),
			},
		},
		CurrentProjectID: "default",
		Settings: Settings{
			AIBaseURL:  "https://api.openai.com/v1",
			AIModel:    "gpt-4o-mini",
			TimeoutSec: 30,
			Version:    AppVersion,
			UpdateURL:  DefaultUpdateURL,
		},
		Plugins:   PluginsData{Connections: []PluginConn{}},
		Clipboard: ClipData{History: []ClipItem{}},
	}
}

// GetClipboardText 读取系统剪贴板文本（供前端轮询记录历史）
func (a *App) GetClipboardText() (string, error) {
	return runtime.ClipboardGetText(a.ctx)
}

// SetClipboardText 写入系统剪贴板
func (a *App) SetClipboardText(text string) error {
	return runtime.ClipboardSetText(a.ctx, text)
}

// readData 从磁盘加载并反序列化全部数据，处理旧版本兼容与默认值补全。
// 返回的是经过迁移/修正后的最新结构，调用方无需再处理兼容逻辑。
func (a *App) readData() AppData {
	a.mu.Lock()
	defer a.mu.Unlock()
	data := defaultData()
	b, err := os.ReadFile(a.dataFile)
	if err != nil {
		return data
	}
	_ = json.Unmarshal(b, &data)
	if data.Settings.TimeoutSec <= 0 {
		data.Settings.TimeoutSec = 30
	}
	// 兼容旧版本配置（无 version / updateURL 字段）
	if data.Settings.Version == "" {
		data.Settings.Version = AppVersion
	}
	if data.Settings.UpdateURL == "" {
		data.Settings.UpdateURL = DefaultUpdateURL
	}
	// 兼容旧版数据（顶层 dirs/apis/environments）：反序列化到新结构失败时，
	// 通过 migrateLegacy 额外解析旧结构并迁移进默认项目。
	if len(data.Projects) == 0 {
		proj := Project{ID: "default", Name: "默认项目", UpdatedAt: time.Now().Format(time.RFC3339)}
		if migrated := a.migrateLegacy(b, &proj); migrated {
			data.Projects = []Project{proj}
			data.CurrentProjectID = proj.ID
		}
	}
	if len(data.Projects) == 0 {
		data = defaultData()
	}
	if data.CurrentProjectID == "" || !hasProject(data, data.CurrentProjectID) {
		data.CurrentProjectID = data.Projects[0].ID
	}
	return data
}

// migrateLegacy 兼容旧版数据（顶层 dirs/apis/environments）
func (a *App) migrateLegacy(b []byte, proj *Project) bool {
	var legacy struct {
		Dirs        []Directory   `json:"dirs"`
		Apis        []ApiInfo     `json:"apis"`
		Environments []Environment `json:"environments"`
		ActiveEnvID string        `json:"activeEnvId"`
	}
	if err := json.Unmarshal(b, &legacy); err != nil {
		return false
	}
	if len(legacy.Dirs) == 0 && len(legacy.Apis) == 0 && len(legacy.Environments) == 0 {
		return false
	}
	proj.Dirs = legacy.Dirs
	proj.Apis = legacy.Apis
	proj.Environments = legacy.Environments
	proj.ActiveEnvID = legacy.ActiveEnvID
	return true
}

func hasProject(data AppData, id string) bool {
	for _, p := range data.Projects {
		if p.ID == id {
			return true
		}
	}
	return false
}

// LoadData 加载全部数据（上次保存的接口信息）
func (a *App) LoadData() AppData {
	return a.readData()
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
	idx := activeProjectIndex(data)
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
		proj.TestCases = []TestCase{}
	case "plans":
		removed = len(proj.TestPlans)
		proj.TestPlans = []TestPlan{}
	case "reports":
		removed = len(proj.TestReports)
		proj.TestReports = []TestReport{}
	case "all":
		removed = len(proj.TestCases) + len(proj.TestPlans) + len(proj.TestReports)
		proj.TestCases = []TestCase{}
		proj.TestPlans = []TestPlan{}
		proj.TestReports = []TestReport{}
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
func (a *App) SaveData(data AppData) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	tmp := a.dataFile + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, a.dataFile)
}

// GetDataFilePath 返回数据文件路径
func (a *App) GetDataFilePath() string {
	return a.dataFile
}

// CopyToClipboard 复制文本到剪贴板
func (a *App) CopyToClipboard(text string) error {
	return runtime.ClipboardSetText(a.ctx, text)
}

// ChatMessage 简单的聊天消息结构（供 CallAI 使用）
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// CallAIArgs CallAI 入参（由前端传入已解析好的配置，避免在 Go 端读取前端 store）
type CallAIArgs struct {
	BaseURL string          `json:"baseUrl"`
	APIKey  string          `json:"apiKey"`
	Model   string          `json:"model"`
	Timeout int             `json:"timeoutSec"`
	Messages []ChatMessage  `json:"messages"`
}

// callAIRequest / callAIResponse 对应 OpenAI 兼容接口的请求与响应
type callAIRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	Stream      bool          `json:"stream"`
}
type callAIChoice struct {
	Message ChatMessage `json:"message"`
}
type callAIResult struct {
	Choices []callAIChoice `json:"choices"`
	Error   interface{}    `json:"error"`
}

// CallAI 由 Go 后端代发 AI 请求，规避前端 webview 的 CORS 限制。
// 返回模型回复的文本内容；失败时返回错误。
func (a *App) CallAI(args CallAIArgs) (string, error) {
	base := strings.TrimSpace(args.BaseURL)
	base = strings.TrimRight(base, "/")
	if base == "" {
		return "", fmt.Errorf("未配置 AI 接口地址（设置 → AI 配置）")
	}
	if args.APIKey == "" {
		return "", fmt.Errorf("未配置 AI API Key（设置 → AI 配置）")
	}
	model := strings.TrimSpace(args.Model)
	if model == "" {
		model = "gpt-4o-mini"
	}
	// 兼容以 /v1 结尾的地址
	url := base
	if !strings.HasSuffix(url, "/chat/completions") {
		if strings.HasSuffix(url, "/v1") {
			url = url + "/chat/completions"
		} else {
			url = url + "/v1/chat/completions"
		}
	}

	payload, err := json.Marshal(callAIRequest{
		Model:       model,
		Messages:    args.Messages,
		Temperature: 0.3,
		Stream:      false,
	})
	if err != nil {
		return "", fmt.Errorf("构造请求失败: %w", err)
	}

	timeout := args.Timeout
	if timeout <= 0 {
		timeout = 30
	}
	client := &http.Client{Timeout: time.Duration(timeout) * time.Second}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+args.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("AI 请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var r callAIResult
		_ = json.Unmarshal(body, &r)
		if r.Error != nil {
			if m, ok := r.Error.(map[string]interface{}); ok {
				if msg, ok := m["message"].(string); ok {
					return "", fmt.Errorf("AI 请求失败: %s", msg)
				}
			}
		}
		return "", fmt.Errorf("AI 请求失败 %d: %s", resp.StatusCode, string(body))
	}

	var r callAIResult
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}
	if len(r.Choices) == 0 {
		return "", fmt.Errorf("AI 返回内容为空")
	}
	return r.Choices[0].Message.Content, nil
}

// OpenInBrowser 使用系统浏览器打开
func (a *App) OpenInBrowser(url string) {
	runtime.BrowserOpenURL(a.ctx, url)
}

// activeProjectIndex 返回当前项目的索引（无效时回退到第一个）
func activeProjectIndex(data AppData) int {
	for i, p := range data.Projects {
		if p.ID == data.CurrentProjectID {
			return i
		}
	}
	if len(data.Projects) > 0 {
		return 0
	}
	return -1
}

// collectScope 计算导出/分享范围内的目录与接口（限定当前项目）。
// 返回 (标题, 目录列表, 接口列表)；单接口时标题取接口名，目录范围时取目录名，否则为“接口文档”。
func (a *App) collectScope(data AppData, dirID string, apiID string) (title string, dirs []Directory, apis []ApiInfo) {
	idx := activeProjectIndex(data)
	if idx < 0 {
		return "接口文档", nil, nil
	}
	proj := data.Projects[idx]
	if apiID != "" {
		for _, api := range proj.Apis {
			if api.ID == apiID {
				return api.Name, nil, []ApiInfo{api}
			}
		}
		return "接口文档", nil, nil
	}
	if dirID == "" {
		return "接口文档", proj.Dirs, proj.Apis
	}
	// 收集 dirID 的整个子树
	include := map[string]bool{dirID: true}
	changed := true
	for changed {
		changed = false
		for _, d := range proj.Dirs {
			if include[d.ParentID] && !include[d.ID] {
				include[d.ID] = true
				changed = true
			}
		}
	}
	title = "接口文档"
	for _, d := range proj.Dirs {
		if d.ID == dirID {
			title = d.Name
		}
		if include[d.ID] {
			dirs = append(dirs, d)
		}
	}
	for _, api := range proj.Apis {
		if include[api.DirID] {
			apis = append(apis, api)
		}
	}
	return title, dirs, apis
}

// buildDocContent 按指定格式（markdown/html/word/openapi）生成文档内容。
// 返回 (内容, 标题, 错误)；当范围内无接口时返回错误。
func (a *App) buildDocContent(dirID, apiID, format string) (content string, title string, err error) {
	data := a.readData()
	idx := activeProjectIndex(data)
	if idx < 0 {
		return "", "", fmt.Errorf("没有可用的项目")
	}
	proj := data.Projects[idx]
	title, dirs, apis := a.collectScope(data, dirID, apiID)
	if len(apis) == 0 {
		return "", "", fmt.Errorf("所选范围内没有接口")
	}
	rootID := ""
	if apiID == "" {
		rootID = dirID
	}
	switch format {
	case "markdown":
		content = buildMarkdown(title, rootID, dirs, apis, proj.Common)
	case "html", "word":
		content = buildHTML(title, rootID, dirs, apis, proj.Common)
	case "openapi":
		content, err = buildOpenAPI(title, apis, proj.Common)
	default:
		return "", "", fmt.Errorf("不支持的格式: %s", format)
	}
	return content, title, err
}

// ExportDoc 导出文档，返回保存路径
func (a *App) ExportDoc(dirID string, apiID string, format string) (string, error) {
	content, title, err := a.buildDocContent(dirID, apiID, format)
	if err != nil {
		return "", err
	}
	ext, filter := ".md", "Markdown (*.md)|*.md"
	switch format {
	case "html":
		ext, filter = ".html", "HTML (*.html)|*.html"
	case "word":
		ext, filter = ".doc", "Word (*.doc)|*.doc"
	case "openapi":
		ext, filter = ".json", "OpenAPI JSON (*.json)|*.json"
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "导出接口文档",
		DefaultFilename: sanitizeFilename(title) + ext,
		Filters: []runtime.FileFilter{
			{DisplayName: filter, Pattern: "*" + ext},
		},
	})
	if err != nil || path == "" {
		return "", err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// ShareDoc 生成 HTML 文档并用浏览器打开（可将文件发送给他人分享）
func (a *App) ShareDoc(dirID string, apiID string) (string, error) {
	content, title, err := a.buildDocContent(dirID, apiID, "html")
	if err != nil {
		return "", err
	}
	path := filepath.Join(os.TempDir(), "apitool-share-"+sanitizeFilename(title)+".html")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	runtime.BrowserOpenURL(a.ctx, "file:///"+filepath.ToSlash(path))
	return path, nil
}

// CopyDocMarkdown 复制 Markdown 文档到剪贴板（用于快速分享）
func (a *App) CopyDocMarkdown(dirID string, apiID string) error {
	content, _, err := a.buildDocContent(dirID, apiID, "markdown")
	if err != nil {
		return err
	}
	return runtime.ClipboardSetText(a.ctx, content)
}

func sanitizeFilename(s string) string {
	out := []rune{}
	for _, r := range s {
		switch r {
		case '\\', '/', ':', '*', '?', '"', '<', '>', '|':
			out = append(out, '_')
		default:
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return "api-doc"
	}
	return string(out)
}

// UpdateInfo 升级服务返回的版本信息
type UpdateInfo struct {
	Version string `json:"version"` // 服务端最新版本号
	URL     string `json:"url"`     // 新版本下载地址
	Notes   string `json:"notes"`   // 更新说明
}

// CheckUpdateResult 检测结果返回给前端
type CheckUpdateResult struct {
	Current  string `json:"current"`  // 当前版本
	Latest   string `json:"latest"`   // 服务端版本
	HasNew   bool   `json:"hasNew"`   // 是否有新版本
	URL      string `json:"url"`      // 新版本下载地址
	Notes    string `json:"notes"`    // 更新说明
	Error    string `json:"error"`    // 检测错误信息（网络/解析失败）
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
