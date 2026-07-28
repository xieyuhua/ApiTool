package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ---------------- 捕获请求数据模型 ----------------
// CapturedRequest 浏览器扩展回传的一条被监听网页的请求记录。
// 字段命名尽量贴近 ApiInfo，便于直接转换为接口定义。
type CapturedRequest struct {
	ID          string            `json:"id"`
	CapturedAt  string            `json:"capturedAt"` // RFC3339
	Method      string            `json:"method"`
	URL         string            `json:"url"`
	Host        string            `json:"host"`
	Path        string            `json:"path"`
	Origin      string            `json:"origin"`    // 协议+主机，如 https://api.example.com
	Query       []KV              `json:"query"`
	Headers     []KV              `json:"headers"`
	BodyType    string            `json:"bodyType"`  // none | json | form | text
	Body        string            `json:"body"`
	StatusCode  int               `json:"statusCode"`
	StatusText  string            `json:"statusText"`
	DurationMs  int64             `json:"durationMs"`
	RespHeaders map[string]string `json:"respHeaders"`
	RespBody    string            `json:"respBody"`
	RespIsJSON  bool              `json:"respIsJson"`
	PageURL     string            `json:"pageUrl"` // 触发该请求的页面地址
	MatchedURL  string            `json:"matchedUrl"` // 命中的监控规则（扩展侧填写）
	Error       string            `json:"error"`
}

// capturedIn 扩展上报的请求体（数组或单条）
type capturedIn struct {
	Requests      []CapturedRequest `json:"requests"`
	CaptureStatic bool              `json:"captureStatic"` // 扩展侧「过滤静态资源」取反：true 表示「保留/捕获」静态资源
	Blacklist     []string          `json:"blacklist"`     // 扩展侧自定义黑名单（支持 * 通配）
}

// CaptureServerInfo 返回捕获服务状态，供前端展示与扩展配置
type CaptureServerInfo struct {
	Running   bool   `json:"running"`
	Addr      string `json:"addr"`      // 监听地址 host:port
	Port      string `json:"port"`      // 端口
	URL       string `json:"url"`       // http://127.0.0.1:port
	Token     string `json:"token"`     // 当前鉴权 Token
	Count     int    `json:"count"`     // 已捕获条数
}

// ---------------- 全局状态 ----------------

const defaultCaptureAddr = "127.0.0.1:8653"

var captureSrv *http.Server
var captureMu sync.Mutex
var capturedList []*CapturedRequest

// ---------------- 启动 / 停止 ----------------

// StartCaptureServer 启动独立捕获服务（端口 8653，带 Token 鉴权），
// 仅用于接收浏览器扩展回传的被监听网页请求。
func (a *App) StartCaptureServer(addr string, token string) (string, error) {
	captureMu.Lock()
	defer captureMu.Unlock()
	if captureSrv != nil {
		return captureSrv.Addr, nil
	}
	if addr == "" {
		addr = defaultCaptureAddr
	}
	if token == "" {
		token = a.captureToken
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", fmt.Errorf("启动请求捕获服务失败: %v（端口可能被占用）", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/capture", a.handleCapture)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeCaptureJSON(w, 200, map[string]interface{}{"ok": true, "port": strings.Split(ln.Addr().String(), ":")[1]})
	})
	srv := &http.Server{Addr: ln.Addr().String(), Handler: captureCORS(mux, token)}
	captureSrv = srv
	go func() { _ = srv.Serve(ln) }()
	return ln.Addr().String(), nil
}

// StopCaptureServer 停止捕获服务
func (a *App) StopCaptureServer() error {
	captureMu.Lock()
	defer captureMu.Unlock()
	if captureSrv == nil {
		return nil
	}
	_ = captureSrv.Close()
	captureSrv = nil
	return nil
}

// CaptureServerRunning 捕获服务是否在运行
func (a *App) CaptureServerRunning() bool {
	captureMu.Lock()
	defer captureMu.Unlock()
	return captureSrv != nil
}

// CaptureInfo 返回捕获服务信息（地址、端口、Token、条数）
func (a *App) CaptureInfo() CaptureServerInfo {
	captureMu.Lock()
	defer captureMu.Unlock()
	info := CaptureServerInfo{
		Running: captureSrv != nil,
		Token:   a.captureToken,
		Count:   len(capturedList),
	}
	if captureSrv != nil {
		info.Addr = captureSrv.Addr
		_, port, err := net.SplitHostPort(captureSrv.Addr)
		if err == nil {
			info.Port = port
		}
		host := "127.0.0.1"
		if info.Port == "" {
			info.Port = strings.Split(defaultCaptureAddr, ":")[1]
		}
		info.URL = fmt.Sprintf("http://%s:%s", host, info.Port)
	}
	return info
}

// ---------------- HTTP 处理 ----------------

func (a *App) handleCapture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeCaptureJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeCaptureJSON(w, 400, map[string]string{"error": "读取请求体失败: " + err.Error()})
		return
	}
	var in capturedIn
	if err := json.Unmarshal(raw, &in); err == nil && len(in.Requests) > 0 {
		for i := range in.Requests {
			a.appendCaptured(in.Requests[i], in.CaptureStatic, in.Blacklist)
		}
		writeCaptureJSON(w, 200, map[string]interface{}{"ok": true, "count": len(capturedList)})
		return
	}
	// 兼容单条上报
	var single CapturedRequest
	if err2 := json.Unmarshal(raw, &single); err2 == nil && (single.URL != "" || single.Method != "") {
		a.appendCaptured(single, false, nil)
		writeCaptureJSON(w, 200, map[string]interface{}{"ok": true, "count": len(capturedList)})
		return
	}
	writeCaptureJSON(w, 400, map[string]string{"error": "请求体解析失败，需为 {requests:[...]} 或单条捕获记录"})
}

func (a *App) appendCaptured(c CapturedRequest, captureStatic bool, blacklist []string) {
	// 服务端兜底过滤：即便扩展端因运行异常未过滤，css/图片/字体/data:URI 等静态资源也不会入库。
	// 仅当用户在扩展端显式关闭「过滤静态资源」（即 captureStatic=true）时才放行。
	if !captureStatic && isStaticResourceURL(c.URL) {
		return
	}
	if matchBlacklistPat(c.URL, blacklist) {
		return
	}
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	if c.CapturedAt == "" {
		c.CapturedAt = time.Now().Format(time.RFC3339)
	}
	// 解析 URL 以补全 host/path/origin/query
	if c.URL != "" {
		if u, err := url.Parse(c.URL); err == nil {
			if c.Host == "" {
				c.Host = u.Host
			}
			if c.Path == "" {
				c.Path = u.Path
			}
			if c.Origin == "" {
				c.Origin = u.Scheme + "://" + u.Host
			}
			if len(c.Query) == 0 && u.RawQuery != "" {
				for k, vs := range u.Query() {
					if len(vs) > 0 {
						c.Query = append(c.Query, KV{Key: k, Value: vs[0], Enabled: true})
					}
				}
			}
		}
	}
	if c.Method == "" {
		c.Method = "GET"
	}
	c.Method = strings.ToUpper(c.Method)
	captureMu.Lock()
	// 去重：同一 (method+url) 仅保留最新一条，避免重复刷新页面堆积大量记录
	for i, old := range capturedList {
		if old.Method == c.Method && old.URL == c.URL {
			capturedList[i] = &c
			captureMu.Unlock()
			return
		}
	}
	capturedList = append(capturedList, &c)
	captureMu.Unlock()
}

// GetCapturedRequests 返回全部已捕获请求（供前端展示）
func (a *App) GetCapturedRequests() []CapturedRequest {
	captureMu.Lock()
	defer captureMu.Unlock()
	out := make([]CapturedRequest, 0, len(capturedList))
	for _, c := range capturedList {
		out = append(out, *c)
	}
	return out
}

// ClearCapturedRequests 清空已捕获列表
func (a *App) ClearCapturedRequests() {
	captureMu.Lock()
	defer captureMu.Unlock()
	capturedList = nil
}

func captureCORS(next http.Handler, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// /capture 校验 Token
		if strings.HasPrefix(r.URL.Path, "/capture") && !captureCheckToken(r, token) {
			writeCaptureJSON(w, 401, map[string]string{"error": "token 无效"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func captureCheckToken(r *http.Request, token string) bool {
	t := ""
	if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		t = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	if t == "" {
		t = r.URL.Query().Get("token")
	}
	if t == "" {
		t = r.Header.Get("X-Capture-Token")
	}
	return t == token && token != ""
}

func writeCaptureJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// ---------------- 转换为 ApiInfo ----------------

// capturedToTestCase 将单条捕获请求转换为可执行测试用例（用于自动化测试 / 压测）
func capturedToTestCase(c CapturedRequest) TestCase {
	name := c.Method + " " + firstNonEmpty(c.Path, c.URL)
	bodyType := "json"
	switch c.BodyType {
	case "form":
		bodyType = "form"
	case "text", "none":
		bodyType = "text"
	}
	tc := TestCase{
		ID:          uuid.NewString(),
		ApiName:     "捕获: " + name,
		Category:    "正常流程",
		Name:        name,
		Description: "由浏览器扩展捕获（页面：" + c.PageURL + "）",
		Method:      c.Method,
		URL:         c.URL,
		Headers:     c.Headers,
		Query:       c.Query,
		BodyType:    bodyType,
		Body:        c.Body,
		FormItems:   []KV{},
		ContentType: "",
		Assertions: []Assertion{
			{Type: "status", Target: "", Operator: "eq", Expected: "200", Enabled: true},
		},
		Enabled:   true,
		CreatedAt: time.Now().Format(time.RFC3339),
	}
	return tc
}

// capturedToApi 将单条捕获请求转换为 ApiInfo（推断请求/响应字段树）
func capturedToApi(c CapturedRequest) ApiInfo {
	api := ApiInfo{
		ID:          uuid.NewString(),
		Method:      c.Method,
		URL:         c.URL,
		Name:        c.Method + " " + firstNonEmpty(c.Path, c.URL),
		BodyType:    "json",
		Headers:     c.Headers,
		Query:       c.Query,
		Description: "由浏览器扩展捕获（页面：" + c.PageURL + "）",
		UpdatedAt:   time.Now().Format(time.RFC3339),
	}
	switch c.BodyType {
	case "form":
		api.BodyType = "form"
	case "text", "none":
		api.BodyType = "text"
	default:
		api.BodyType = "json"
	}
	api.Body = c.Body
	// 请求字段：请求体为 JSON 时解析
	if api.BodyType == "json" && strings.TrimSpace(c.Body) != "" {
		if fields, err := parseJSONBodyToFields(c.Body); err == nil && len(fields) > 0 {
			api.ReqFields = fields
		}
	}
	// 响应字段：响应体为 JSON 时解析
	if c.RespIsJSON && strings.TrimSpace(c.RespBody) != "" {
		if fields, err := parseJSONBodyToFields(c.RespBody); err == nil && len(fields) > 0 {
			api.RespFields = fields
		}
	}
	api.LastResponse = &ResponseData{
		Status:     c.StatusCode,
		StatusText: c.StatusText,
		Headers:    c.RespHeaders,
		Body:       c.RespBody,
		DurationMs: c.DurationMs,
		IsJSON:     c.RespIsJSON,
		Error:      c.Error,
	}
	return api
}

// parseJSONBodyToFields 解析 JSON 文本为字段树（复用 jsonparse.go 的基础逻辑）
func parseJSONBodyToFields(s string) ([]*Field, error) {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	v, err := decodeValue(dec)
	if err != nil {
		return nil, err
	}
	switch t := v.(type) {
	case omap:
		var fields []*Field
		for _, p := range t {
			fields = append(fields, fieldFromValue(p.key, p.val))
		}
		return fields, nil
	case []interface{}:
		root := fieldFromValue("(root)", t)
		if len(root.Children) > 0 {
			return root.Children, nil
		}
		return []*Field{root}, nil
	}
	return nil, fmt.Errorf("非 JSON 对象/数组")
}

// GenerateApiFromCaptured 将选中的捕获请求转换为接口定义并导入当前项目（指定目录）
func (a *App) GenerateApiFromCaptured(ids []string, projectID, dirID string) (int, error) {
	if len(ids) == 0 {
		return 0, fmt.Errorf("请至少选择一条捕获记录")
	}
	idSet := map[string]bool{}
	for _, id := range ids {
		idSet[id] = true
	}
	captureMu.Lock()
	var sel []CapturedRequest
	for _, c := range capturedList {
		if idSet[c.ID] {
			sel = append(sel, *c)
		}
	}
	captureMu.Unlock()
	if len(sel) == 0 {
		return 0, fmt.Errorf("未找到所选捕获记录（可能已被清空）")
	}

	data := a.readData()
	idx := -1
	for i, p := range data.Projects {
		if p.ID == projectID {
			idx = i
			break
		}
	}
	if idx < 0 {
		idx = activeProjectIndex(data)
	}
	if idx < 0 {
		return 0, fmt.Errorf("没有可用的项目")
	}
	for _, c := range sel {
		api := capturedToApi(c)
		api.DirID = dirID
		data.Projects[idx].Apis = append(data.Projects[idx].Apis, api)
	}
	data.Projects[idx].UpdatedAt = time.Now().Format(time.RFC3339)
	if err := a.SaveData(data); err != nil {
		return 0, err
	}
	return len(sel), nil
}

// ImportCapturedAsTestCases 将选中的捕获请求转换为测试用例，导入当前项目用于自动化测试 / 压测
func (a *App) ImportCapturedAsTestCases(ids []string) (int, error) {
	if len(ids) == 0 {
		return 0, fmt.Errorf("请至少选择一条捕获记录")
	}
	idSet := map[string]bool{}
	for _, id := range ids {
		idSet[id] = true
	}
	captureMu.Lock()
	var sel []CapturedRequest
	for _, c := range capturedList {
		if idSet[c.ID] {
			sel = append(sel, *c)
		}
	}
	captureMu.Unlock()
	if len(sel) == 0 {
		return 0, fmt.Errorf("未找到所选捕获记录（可能已被清空）")
	}

	data := a.readData()
	idx := activeProjectIndex(data)
	if idx < 0 {
		return 0, fmt.Errorf("没有可用的项目")
	}
	for _, c := range sel {
		data.Projects[idx].TestCases = append(data.Projects[idx].TestCases, capturedToTestCase(c))
	}
	data.Projects[idx].UpdatedAt = time.Now().Format(time.RFC3339)
	if err := a.SaveData(data); err != nil {
		return 0, err
	}
	return len(sel), nil
}

// ExportCapturedOpenAPI 将选中的捕获请求生成 OpenAPI 文档并弹出保存对话框
func (a *App) ExportCapturedOpenAPI(ids []string, title string) (string, error) {
	if len(ids) == 0 {
		return "", fmt.Errorf("请至少选择一条捕获记录")
	}
	idSet := map[string]bool{}
	for _, id := range ids {
		idSet[id] = true
	}
	captureMu.Lock()
	var sel []CapturedRequest
	for _, c := range capturedList {
		if idSet[c.ID] {
			sel = append(sel, *c)
		}
	}
	captureMu.Unlock()
	if len(sel) == 0 {
		return "", fmt.Errorf("未找到所选捕获记录")
	}
	apis := make([]ApiInfo, 0, len(sel))
	for _, c := range sel {
		apis = append(apis, capturedToApi(c))
	}
	if title == "" {
		title = "浏览器捕获接口"
	}
	content, err := buildOpenAPI(title, apis, CommonParams{})
	if err != nil {
		return "", err
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "导出 OpenAPI 文档",
		DefaultFilename: sanitizeFilename(title) + ".json",
		Filters: []runtime.FileFilter{
			{DisplayName: "OpenAPI JSON (*.json)", Pattern: "*.json"},
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

// BuildCapturedOpenAPI 生成 OpenAPI JSON 文本（不弹保存框），便于前端复制到剪贴板
func (a *App) BuildCapturedOpenAPI(ids []string, title string) (string, error) {
	if len(ids) == 0 {
		return "", fmt.Errorf("请至少选择一条捕获记录")
	}
	idSet := map[string]bool{}
	for _, id := range ids {
		idSet[id] = true
	}
	captureMu.Lock()
	var sel []CapturedRequest
	for _, c := range capturedList {
		if idSet[c.ID] {
			sel = append(sel, *c)
		}
	}
	captureMu.Unlock()
	if len(sel) == 0 {
		return "", fmt.Errorf("未找到所选捕获记录")
	}
	apis := make([]ApiInfo, 0, len(sel))
	for _, c := range sel {
		apis = append(apis, capturedToApi(c))
	}
	if title == "" {
		title = "浏览器捕获接口"
	}
	return buildOpenAPI(title, apis, CommonParams{})
}

// ---------------- Token 持久化 ----------------

const captureTokenFile = "capture_token.txt"

func (a *App) loadOrCreateCaptureToken() {
	// 优先读取已持久化的 token（避免每次重启都要重新配置扩展）
	p := filepath.Join(a.syncDir, captureTokenFile)
	if b, err := os.ReadFile(p); err == nil {
		t := strings.TrimSpace(string(b))
		if t != "" {
			a.captureToken = t
			return
		}
	}
	// 否则生成一个新的随机 token
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err == nil {
		a.captureToken = "cap_" + hex.EncodeToString(buf)
	} else {
		a.captureToken = "cap_" + uuid.NewString()
	}
	_ = os.WriteFile(p, []byte(a.captureToken), 0o600)
}

// ---------------- 静态资源 / 黑名单（服务端兜底过滤） ----------------

// staticExts 常见静态资源扩展名（与扩展端保持一致）
var staticExts = map[string]bool{
	"js": true, "mjs": true, "cjs": true, "jsx": true, "ts": true, "tsx": true,
	"css": true, "scss": true, "sass": true, "less": true,
	"png": true, "jpg": true, "jpeg": true, "gif": true, "webp": true, "avif": true, "svg": true, "bmp": true, "ico": true,
	"woff": true, "woff2": true, "eot": true, "ttf": true, "otf": true, "map": true,
	"mp3": true, "mp4": true, "webm": true, "wav": true, "ogg": true, "pdf": true,
}

// isStaticResourceURL 按扩展名 / 协议判断是否为静态资源（css/图片/字体/data:URI 等）
func isStaticResourceURL(u string) bool {
	if u == "" {
		return false
	}
	if strings.HasPrefix(u, "data:") || strings.HasPrefix(u, "blob:") {
		return true
	}
	path := u
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	dot := strings.LastIndex(path, ".")
	if dot < 0 || dot == len(path)-1 {
		return false
	}
	return staticExts[strings.ToLower(path[dot+1:])]
}

// wildcardToRegex 将 * 通配模式转为正则（^...$），如 *.example.com/* -> ^.*\.example\.com/.*$
func wildcardToRegex(p string) (*regexp.Regexp, error) {
	var sb strings.Builder
	sb.WriteString("^")
	for _, part := range strings.Split(p, "*") {
		sb.WriteString(regexp.QuoteMeta(part))
		sb.WriteString(".*")
	}
	s := strings.TrimSuffix(sb.String(), ".*") + "$"
	return regexp.Compile(s)
}

// matchBlacklistPat 命中自定义黑名单（每行一条，支持 * 通配）则返回 true
func matchBlacklistPat(u string, pats []string) bool {
	for _, p := range pats {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		re, err := wildcardToRegex(p)
		if err != nil {
			continue
		}
		if re.MatchString(u) {
			return true
		}
	}
	return false
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
