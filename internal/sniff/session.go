package sniff

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	mitm "github.com/lqqyt2423/go-mitmproxy/proxy"

	"apitool/internal/doc"
	"apitool/internal/jsonutil"
	"apitool/internal/model"
	"apitool/internal/util"
)

// TrafficRecord 一条被抓取的流量记录（独立于接口树，存于抓包会话）。
type TrafficRecord struct {
	ID          string            `json:"id"`
	SessionID   string            `json:"sessionId"`
	Timestamp   string            `json:"timestamp"` // RFC3339
	Protocol    string            `json:"protocol"`  // HTTP/HTTPS/TLS/SSH/FTP/SMTP/...
	Decrypted   bool              `json:"decrypted"`
	Method      string            `json:"method"`
	URL         string            `json:"url"`
	Host        string            `json:"host"`
	Path        string            `json:"path"`
	Query       []model.KV        `json:"query"`
	ReqHeaders  []model.KV        `json:"reqHeaders"`
	ReqBody     string            `json:"reqBody"`
	ReqBodyType string            `json:"reqBodyType"` // none|json|form|text
	StatusCode  int               `json:"statusCode"`
	StatusText  string            `json:"statusText"`
	RespHeaders []model.KV        `json:"respHeaders"`
	RespBody    string            `json:"respBody"`
	RespBodyType string           `json:"respBodyType"`
	RespContentType string        `json:"respContentType"` // 响应 Content-Type（真实值，用于图片等预览）
	DurationMs  int64             `json:"durationMs"`
	ProcessName string            `json:"processName"` // 进程名（尽力而为，Windows 需驱动级，暂留空/Host）
	Note        string            `json:"note"`
	Error       string            `json:"error"`
}

// Session 一次抓包会话。
type Session struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	StartedAt string          `json:"startedAt"`
	StoppedAt string          `json:"stoppedAt"`
	ProxyAddr string          `json:"proxyAddr"`
	Records   []TrafficRecord `json:"records"`
}

// SessionStore 抓包会话存储：内存为主，按会话落盘 JSON，互不打扰现有业务数据。
type SessionStore struct {
	mu       sync.Mutex
	dir      string
	sessions map[string]*Session
}

// NewSessionStore 创建存储（dir 为抓包数据目录）。
func NewSessionStore(dir string) *SessionStore {
	_ = os.MkdirAll(dir, 0o755)
	s := &SessionStore{dir: dir, sessions: map[string]*Session{}}
	s.loadAll()
	return s
}

func (s *SessionStore) pathOf(id string) string {
	return filepath.Join(s.dir, "session-"+id+".json")
}

func (s *SessionStore) loadAll() {
	ents, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		if e.IsDir() || !hasPrefix(e.Name(), "session-") || !hasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var sess Session
		if json.Unmarshal(b, &sess) == nil {
			s.sessions[sess.ID] = &sess
		}
	}
}

// NewSession 新建会话（不落盘，Stop 时保存）。
func (s *SessionStore) NewSession(proxyAddr string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := newID("sess")
	sess := &Session{
		ID:        id,
		Name:      "抓包会话 " + time.Now().Format("2006-01-02 15:04:05"),
		StartedAt: time.Now().Format(time.RFC3339),
		ProxyAddr: proxyAddr,
		Records:   []TrafficRecord{},
	}
	s.sessions[id] = sess
	return sess
}

// Append 追加一条记录（同时写内存；调用方负责最终 Save）。
func (s *SessionStore) Append(rec TrafficRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[rec.SessionID]; ok {
		sess.Records = append(sess.Records, rec)
	}
}

// Save 将会话落盘。
func (s *SessionStore) Save(sess *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess.StoppedAt = time.Now().Format(time.RFC3339)
	b, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.pathOf(sess.ID), b, 0o644)
}

// List 返回会话摘要列表。
func (s *SessionStore) List() []Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Session, 0, len(s.sessions))
	for _, v := range s.sessions {
		out = append(out, *v)
	}
	return out
}

// Get 获取完整会话。
func (s *SessionStore) Get(id string) (*Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.sessions[id]
	return v, ok
}

// Delete 删除会话（含磁盘文件）。
func (s *SessionStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
	return os.Remove(s.pathOf(id))
}

// ToOpenAPI 将整个会话的 HTTP(S) 记录转换为 OpenAPI 3.0 文档（按 URL+方法聚合去重）。
func (s *SessionStore) ToOpenAPI(sess *Session) (string, error) {
	var apis []model.ApiInfo
	seen := map[string]bool{}
	for _, r := range sess.Records {
		if !r.Decrypted || r.Method == "" {
			continue
		}
		key := r.Method + " " + r.URL
		if seen[key] {
			continue
		}
		seen[key] = true
		apis = append(apis, model.ApiInfo{
			Name:        r.Method + " " + r.Path,
			Method:      r.Method,
			URL:         r.URL,
			Headers:     r.ReqHeaders,
			Query:       r.Query,
			FormItems:   nil, // 抓包记录中表单以 body 形式保存，需要时由前端/后续解析
			BodyType:    r.ReqBodyType,
			Body:        r.ReqBody,
			RespFields:  []*model.Field{},
			Description: "抓包自动生成 · " + r.Host,
		})
	}
	if len(apis) == 0 {
		return "", io.EOF // 调用方据此提示“无 HTTP 流量”
	}
	return doc.BuildOpenAPI(sess.Name, apis, model.CommonParams{})
}

func kvToMap(kvs []model.KV) map[string]string {
	m := map[string]string{}
	for _, kv := range kvs {
		if kv.Enabled && kv.Key != "" {
			m[kv.Key] = kv.Value
		}
	}
	return m
}

// 小工具：前缀/后缀判断（避免在多处重复 strings 包调用）
func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }
func hasSuffix(s, p string) bool { return len(s) >= len(p) && s[len(s)-len(p):] == p }

// newID 生成短随机 ID。
func newID(prefix string) string {
	return prefix + "-" + time.Now().Format("20060102150405") + "-" + randHex(4)
}

// recordFromReqResp 根据解密后的请求与响应生成一条流量记录。
// reqBody / respBody 为原始字节，由调用方读取。
func (s *SessionStore) recordFromReqResp(req *http.Request, resp *http.Response, reqBody, respBody []byte) *TrafficRecord {
	method := ""
	rawURL := ""
	host := ""
	path := ""
	var query []model.KV
	var reqHeaders []model.KV
	if req != nil {
		method = req.Method
		if req.URL != nil {
			rawURL = req.URL.String()
			host = req.URL.Hostname()
			path = req.URL.Path
			for k, vs := range req.URL.Query() {
				if len(vs) > 0 {
					query = append(query, model.KV{Key: k, Value: vs[0], Enabled: true})
				}
			}
		}
		for k, vs := range req.Header {
			if len(vs) > 0 {
				reqHeaders = append(reqHeaders, model.KV{Key: k, Value: vs[0], Enabled: true})
			}
		}
	}

	statusCode := 0
	statusText := ""
	var respHeaders []model.KV
	if resp != nil {
		statusCode = resp.StatusCode
		statusText = resp.Status
		for k, vs := range resp.Header {
			if len(vs) > 0 {
				respHeaders = append(respHeaders, model.KV{Key: k, Value: vs[0], Enabled: true})
			}
		}
	}

	rec := TrafficRecord{
		ID:           newID("rec"),
		Timestamp:    time.Now().Format(time.RFC3339),
		Protocol:     classifyRequest(req),
		Decrypted:    true, // gomitmproxy 仅在成功 MITM 后才会触发 OnResponse
		Method:       method,
		URL:          rawURL,
		Host:         host,
		Path:         path,
		Query:        query,
		ReqHeaders:   reqHeaders,
		ReqBody:      string(reqBody),
		ReqBodyType:  bodyTypeOf(req.Header.Get("Content-Type"), reqBody),
		StatusCode:   statusCode,
		StatusText:   statusText,
		RespHeaders:  respHeaders,
		RespBody:     string(respBody),
		RespBodyType: bodyTypeOf(resp.Header.Get("Content-Type"), respBody),
		DurationMs:   0,
		ProcessName:  "",
	}
	return &rec
}

// RecordToApi 将一条抓包流量记录转换为 model.ApiInfo（接口草稿），
// 供「写入接口树」使用。dirID 为目标目录；为空表示写入根目录。
func RecordToApi(r TrafficRecord, dirID string) model.ApiInfo {
	api := model.ApiInfo{
		ID:          uuid.NewString(),
		DirID:       dirID,
		Method:      r.Method,
		URL:         r.URL,
		Name:        r.Method + " " + util.FirstNonEmpty(r.Path, r.Host),
		Headers:     r.ReqHeaders,
		Query:       r.Query,
		Description: "网络抓包自动生成 · " + r.Host,
		UpdatedAt:   time.Now().Format(time.RFC3339),
	}

	// Body 类型映射
	switch r.ReqBodyType {
	case "form":
		api.BodyType = "form"
	case "text", "none":
		if r.ReqBody == "" {
			api.BodyType = "none"
		} else {
			api.BodyType = "text"
		}
	default: // json / xml / 其它
		api.BodyType = r.ReqBodyType
	}
	api.Body = r.ReqBody

	// 请求字段：请求体为 JSON 时解析
	if api.BodyType == "json" && strings.TrimSpace(r.ReqBody) != "" {
		if fields, err := jsonutil.ParseFields(r.ReqBody, nil); err == nil && len(fields) > 0 {
			api.ReqFields = fields
		}
	}
	// 响应字段：响应体为 JSON 时解析
	if r.RespBodyType == "json" && strings.TrimSpace(r.RespBody) != "" {
		if fields, err := jsonutil.ParseFields(r.RespBody, nil); err == nil && len(fields) > 0 {
			api.RespFields = fields
		}
	}

	api.LastResponse = &model.ResponseData{
		Status:     r.StatusCode,
		StatusText: r.StatusText,
		Headers:    kvToMap(r.RespHeaders),
		Body:       r.RespBody,
		DurationMs: r.DurationMs,
		IsJSON:     r.RespBodyType == "json",
		Error:      r.Error,
	}
	return api
}

// recordFromReqRespExt 基于 go-mitmproxy 的 Flow 数据生成一条流量记录。
func (s *SessionStore) recordFromReqRespExt(req *mitm.Request, respBody, reqBody []byte,
	reqHeaders, respHeaders []model.KV, statusCode int, statusText, protocol string,
	startTime time.Time) *TrafficRecord {
	if req == nil || req.URL == nil {
		return nil
	}
	rawURL := req.URL.String()
	respCT := firstHeader(respHeaders, "Content-Type")
	// 图片响应体以 base64 存储，供前端直接预览
	respBodyStr := string(respBody)
	if strings.Contains(strings.ToLower(respCT), "image/") && len(respBody) > 0 {
		respBodyStr = base64.StdEncoding.EncodeToString(respBody)
	}
	rec := TrafficRecord{
		ID:           newID("rec"),
		Timestamp:    time.Now().Format(time.RFC3339),
		Protocol:     protocol,
		Decrypted:    true,
		Method:       req.Method,
		URL:          rawURL,
		Host:         req.URL.Hostname(),
		Path:         req.URL.Path,
		Query:        urlQueryToKV(req.URL),
		ReqHeaders:   reqHeaders,
		ReqBody:      string(reqBody),
		ReqBodyType:  bodyTypeOf(firstHeader(reqHeaders, "Content-Type"), reqBody),
		StatusCode:   statusCode,
		StatusText:   statusText,
		RespHeaders:  respHeaders,
		RespBody:     respBodyStr,
		RespBodyType: bodyTypeOf(respCT, respBody),
		RespContentType: respCT,
		DurationMs:   int64(time.Since(startTime).Milliseconds()),
		ProcessName:  "",
	}
	return &rec
}

// recordWebSocket 记录一条 WebSocket 消息。
func (s *SessionStore) recordWebSocket(req *mitm.Request, msg *mitm.WebSocketMessage, startTime time.Time) *TrafficRecord {
	if req == nil || req.URL == nil || msg == nil {
		return nil
	}
	direction := "S->C"
	if msg.FromClient {
		direction = "C->S"
	}
	rec := TrafficRecord{
		ID:           newID("ws"),
		Timestamp:    time.Now().Format(time.RFC3339),
		Protocol:     "websocket",
		Decrypted:    true,
		Method:       req.Method,
		URL:          req.URL.String(),
		Host:         req.URL.Hostname(),
		Path:         req.URL.Path,
		ReqHeaders:   headerToKV(req.Header),
		ReqBody:      direction + " msg-type=" + itoa(msg.Type) + " " + string(msg.Content),
		ReqBodyType:  "text",
		StatusCode:   0,
		StatusText:   "WebSocket",
		DurationMs:   int64(time.Since(startTime).Milliseconds()),
		ProcessName:  "",
	}
	return &rec
}

// recordSSE 记录一条 SSE 事件。
func (s *SessionStore) recordSSE(req *mitm.Request, ev *mitm.SSEEvent, startTime time.Time) *TrafficRecord {
	if req == nil || req.URL == nil || ev == nil {
		return nil
	}
	rec := TrafficRecord{
		ID:           newID("sse"),
		Timestamp:    time.Now().Format(time.RFC3339),
		Protocol:     "sse",
		Decrypted:    true,
		Method:       req.Method,
		URL:          req.URL.String(),
		Host:         req.URL.Hostname(),
		Path:         req.URL.Path,
		ReqHeaders:   headerToKV(req.Header),
		ReqBody:      "event=" + ev.Event + " data=" + ev.Data,
		ReqBodyType:  "text",
		StatusCode:   0,
		StatusText:   "SSE",
		DurationMs:   int64(time.Since(startTime).Milliseconds()),
		ProcessName:  "",
	}
	return &rec
}

// urlQueryToKV 将 URL query 转为键值对。
func urlQueryToKV(u *url.URL) []model.KV {
	if u == nil {
		return nil
	}
	var out []model.KV
	for k, vs := range u.Query() {
		if len(vs) > 0 {
			out = append(out, model.KV{Key: k, Value: vs[0], Enabled: true})
		}
	}
	return out
}

// firstHeader 返回指定 header 的首个值。
func firstHeader(kvs []model.KV, key string) string {
	for _, kv := range kvs {
		if strings.EqualFold(kv.Key, key) {
			return kv.Value
		}
	}
	return ""
}

// itoa 整数转字符串。
func itoa(n int) string {
	return strconv.Itoa(n)
}
