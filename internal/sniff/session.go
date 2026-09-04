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
	"unicode/utf8"

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
	// RespBodyBase64 为原始响应体字节的 base64。仅在响应体不是合法 UTF-8（二进制/GBK 等本地编码）
	// 或 Content-Type 声明了非 UTF-8 字符集时才附带，供前端按原字符编码或十六进制查看原始内容
	// （RespBody 是强转的字符串，此时已是乱码且不可逆）。图片响应已整体 base64 存入 RespBody，不再重复附带。
	RespBodyBase64 string `json:"respBodyBase64,omitempty"`
	// RespBodyBinary 表示原始响应体字节不是合法 UTF-8（二进制数据，如 protobuf/字体/压缩包等）。
	RespBodyBinary bool `json:"respBodyBinary,omitempty"`
	DurationMs  int64             `json:"durationMs"`
	ProcessName string            `json:"processName"` // 进程名（尽力而为，Windows 需驱动级，暂留空/Host）
	Note        string            `json:"note"`
	Error       string            `json:"error"`
	ReqClipped  bool              `json:"reqClipped,omitempty"`  // 实时推送时请求体过大被截断标记（会话内保存完整数据）
	RespClipped bool              `json:"respClipped,omitempty"` // 实时推送时响应体过大被截断标记
	RespBodyTruncated bool        `json:"respBodyTruncated,omitempty"` // 落盘时响应体超长被截断（内存中仍为完整数据）
}

// Session 一次抓包会话。
type Session struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	StartedAt   string          `json:"startedAt"`
	StoppedAt   string          `json:"stoppedAt"`
	ProxyAddr   string          `json:"proxyAddr"`
	Records     []TrafficRecord `json:"records"`
	Errors      []ErrorInfo     `json:"errors"` // 解密/连接失败日志（本地持久化）
	RecordCount int             `json:"recordCount,omitempty"` // 记录数（列表摘要用，历史会话懒加载时由文件头统计）
}

// SessionStore 抓包会话存储：活跃会话驻留内存，历史会话落盘 JSON 懒加载。
// 启动时仅扫描文件名建立索引，不读取文件内容，避免历史会话（含大响应体）全量载入内存。
type SessionStore struct {
	mu       sync.Mutex
	dir      string
	sessions map[string]*Session // 仅活跃会话（正在抓包/尚未落盘）
	index    map[string]string   // 历史会话 id -> 文件名（按需懒加载）
}

// NewSessionStore 创建存储（dir 为抓包数据目录）。
func NewSessionStore(dir string) *SessionStore {
	_ = os.MkdirAll(dir, 0o755)
	s := &SessionStore{dir: dir, sessions: map[string]*Session{}, index: map[string]string{}}
	s.scanDisk()
	return s
}

func (s *SessionStore) pathOf(id string) string {
	return filepath.Join(s.dir, "session-"+id+".json")
}

// scanDisk 只扫描文件名建立索引（不读取内容），历史会话按需懒加载。
func (s *SessionStore) scanDisk() {
	ents, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		if e.IsDir() || !hasPrefix(e.Name(), "session-") || !hasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(e.Name(), "session-"), ".json")
		if id != "" {
			s.index[id] = e.Name()
		}
	}
}

// readSessionFile 从磁盘读取完整会话。
func readSessionFile(path string) (*Session, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var sess Session
	if err := json.Unmarshal(b, &sess); err != nil {
		return nil, err
	}
	return &sess, nil
}

// skipJSONValue 流式跳过一个完整的 JSON 值（不解析其内容），内存占用 O(1)。
func skipJSONValue(dec *json.Decoder) error {
	depth := 0
	for {
		t, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := t.(json.Delim); ok {
			switch d {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return nil
				}
			}
		} else if depth == 0 {
			return nil
		}
	}
}

// readSessionMeta 流式读取会话文件的元信息并统计 records 数量。
// 不反序列化 records/errors 大字段，内存占用 O(1)。
func readSessionMeta(path string) (Session, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return Session{}, 0, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	var sess Session
	recCount := 0
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return sess, 0, err
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return sess, 0, err
		}
		key, _ := kt.(string)
		switch key {
		case "id":
			_ = dec.Decode(&sess.ID)
		case "name":
			_ = dec.Decode(&sess.Name)
		case "startedAt":
			_ = dec.Decode(&sess.StartedAt)
		case "stoppedAt":
			_ = dec.Decode(&sess.StoppedAt)
		case "proxyAddr":
			_ = dec.Decode(&sess.ProxyAddr)
		case "records":
			if at, err := dec.Token(); err == nil && at == json.Delim('[') {
				for dec.More() {
					_ = skipJSONValue(dec)
					recCount++
				}
				_, _ = dec.Token() // 消费 ']'
			}
		default:
			_ = skipJSONValue(dec)
		}
	}
	return sess, recCount, nil
}

// summaryOf 取活跃会话的摘要字段（不复制 records/errors 大字段）。
func summaryOf(sess *Session) Session {
	return Session{
		ID:          sess.ID,
		Name:        sess.Name,
		StartedAt:   sess.StartedAt,
		StoppedAt:   sess.StoppedAt,
		ProxyAddr:   sess.ProxyAddr,
		RecordCount: len(sess.Records),
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

// AppendError 追加一条解密/连接失败日志到指定会话（随会话落盘）。
func (s *SessionStore) AppendError(sessionID string, info ErrorInfo) {
	if sessionID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[sessionID]; ok {
		sess.Errors = append(sess.Errors, info)
	}
}

// GetErrors 返回指定会话的解密失败日志（历史会话按需从磁盘读取）。
func (s *SessionStore) GetErrors(sessionID string) []ErrorInfo {
	s.mu.Lock()
	if v, ok := s.sessions[sessionID]; ok {
		out := make([]ErrorInfo, len(v.Errors))
		copy(out, v.Errors)
		s.mu.Unlock()
		return out
	}
	name, ok := s.index[sessionID]
	s.mu.Unlock()
	if !ok {
		return nil
	}
	sess, err := readSessionFile(filepath.Join(s.dir, name))
	if err != nil {
		return nil
	}
	return sess.Errors
}

// maxSaveRespBody 落盘时单条响应体裁剪上限，避免大流量会话产生数十 MB 的
// JSON 文件导致序列化/磁盘写入过慢。内存中仍保留完整数据供实时查看。
const maxSaveRespBody = 4 * 1024 * 1024

// Save 将会话落盘，落盘后从内存移除（转为磁盘懒加载），释放大体积会话内存。
// 为避免超大响应体拖慢序列化，落盘副本会对超长 respBody 做截断标注。
func (s *SessionStore) Save(sess *Session) error {
	// 复制会话做裁剪，避免修改仍可能被读取的内存对象
	snap := *sess
	if len(sess.Records) > 0 {
		recs := make([]TrafficRecord, len(sess.Records))
		for i, rec := range sess.Records {
			if len(rec.RespBody) > maxSaveRespBody {
				rec.RespBody = rec.RespBody[:maxSaveRespBody]
				rec.RespBodyTruncated = true
			}
			// 原始字节 base64 体积约为原文的 1.33 倍，过大时丢弃（仅落盘副本），避免会话文件膨胀
			if len(rec.RespBodyBase64) > maxSaveRespBody {
				rec.RespBodyBase64 = ""
			}
			recs[i] = rec
		}
		snap.Records = recs
	}
	snap.StoppedAt = time.Now().Format(time.RFC3339)

	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.pathOf(snap.ID), b, 0o644); err != nil {
		return err
	}
	delete(s.sessions, sess.ID)
	s.index[sess.ID] = "session-" + sess.ID + ".json"
	return nil
}

// List 返回会话摘要列表（不含 records/errors 大字段），供前端列表展示。
func (s *SessionStore) List() []Session {
	s.mu.Lock()
	out := make([]Session, 0, len(s.sessions)+len(s.index))
	for _, v := range s.sessions {
		out = append(out, summaryOf(v))
	}
	names := make([]string, 0, len(s.index))
	for _, n := range s.index {
		names = append(names, n)
	}
	s.mu.Unlock()

	// 历史会话：流式读取元信息 + records 计数，不载入大 body 到内存
	for _, name := range names {
		sess, count, err := readSessionMeta(filepath.Join(s.dir, name))
		if err == nil {
			sess.RecordCount = count
			out = append(out, sess)
		}
	}
	return out
}

// Get 获取完整会话。内存中活跃会话直接返回；历史会话按需从磁盘读取（不缓存，用后即释放）。
func (s *SessionStore) Get(id string) (*Session, bool) {
	s.mu.Lock()
	if v, ok := s.sessions[id]; ok {
		s.mu.Unlock()
		return v, true
	}
	name, ok := s.index[id]
	s.mu.Unlock()
	if !ok {
		return nil, false
	}
	sess, err := readSessionFile(filepath.Join(s.dir, name))
	if err != nil {
		return nil, false
	}
	return sess, true
}

// Delete 删除会话（含磁盘文件）。
func (s *SessionStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
	delete(s.index, id)
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
	return doc.BuildOpenAPI(sess.Name, nil, apis, "", model.CommonParams{})
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

// rawBodyKeepLimit 附带原始响应体字节 base64 的上限（按原始字节数计）。
// 超过该大小不再附带，避免实时推送与落盘体积膨胀；此时前端退化为文本展示。
const rawBodyKeepLimit = 1 << 20 // 1MB

// nonUTF8Charsets 需要保留原始字节的非 UTF-8 字符集（这些编码直接转 string 会乱码）。
var nonUTF8Charsets = map[string]bool{
	"gbk": true, "gb2312": true, "gb18030": true, "x-gbk": true,
	"big5": true, "big5-hkscs": true,
	"shift_jis": true, "shift-jis": true, "euc-jp": true,
	"euc-kr": true, "ks_c_5601-1987": true,
	"windows-1251": true, "cp1251": true,
}

// declaredNonUTF8Charset 返回 Content-Type 中声明的非 UTF-8 字符集名（gbk/big5/...），
// 返回空串表示未声明或声明为 UTF-8/ASCII（此时转 string 不会乱码）。
func declaredNonUTF8Charset(contentType string) string {
	ct := strings.ToLower(contentType)
	i := strings.Index(ct, "charset=")
	if i < 0 {
		return ""
	}
	cs := strings.TrimSpace(ct[i+len("charset="):])
	if j := strings.IndexAny(cs, "; \t\"'") ; j >= 0 {
		cs = cs[:j]
	}
	cs = strings.Trim(cs, `"' `)
	if nonUTF8Charsets[cs] {
		return cs
	}
	return ""
}

// fillRespRawBody 必要时为记录填充原始响应体字节的 base64，
// 让前端可以按原字符编码（如 GBK）或十六进制查看原始内容，而不是展示强转后的乱码。
// 触发条件：字节不是合法 UTF-8（二进制数据或本地编码），或 Content-Type 声明了非 UTF-8 字符集。
func fillRespRawBody(rec *TrafficRecord, respBody []byte, contentType string) {
	if rec == nil || len(respBody) == 0 {
		return
	}
	// 图片响应已整体 base64 存入 RespBody（供前端预览），不重复附带
	if strings.HasPrefix(strings.ToLower(contentType), "image/") {
		return
	}
	binary := !utf8.Valid(respBody)
	if !binary && declaredNonUTF8Charset(contentType) == "" {
		return
	}
	rec.RespBodyBinary = binary
	if len(respBody) > rawBodyKeepLimit {
		return
	}
	rec.RespBodyBase64 = base64.StdEncoding.EncodeToString(respBody)
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
	fillRespRawBody(&rec, respBody, resp.Header.Get("Content-Type"))
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
	fillRespRawBody(&rec, respBody, respCT)
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
