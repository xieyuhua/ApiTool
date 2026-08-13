package sniff

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lqqyt2423/go-mitmproxy/cert"
	mitm "github.com/lqqyt2423/go-mitmproxy/proxy"

	"apitool/internal/model"
)

// emitFlushInterval 批量推送实时记录的合并窗口。避免每条记录都触发一次
// Wails IPC 事件，从而大幅降低高流量抓包下的前端渲染压力。
const emitFlushInterval = 40 * time.Millisecond

// proxy 封装 go-mitmproxy，负责实际的中间人解密与多协议流量记录。
// 嵌入 mitm.BaseAddon 以获得 Addon 接口的空实现，仅覆写需要的方法。
type proxy struct {
	mitm.BaseAddon

	p         *mitm.Proxy
	ca        *caBundle
	store     *SessionStore
	filter    Filter
	emit      func([]TrafficRecord) // 批量推送实时记录
	errEmit   func(ErrorInfo)
	sessionID string // 当前活动会话 ID（manager.Start 设置）

	mu        sync.Mutex
	reqBodies map[string][]byte // 预留：按连接关联请求体暂存

	emitBuf      []TrafficRecord // 待推送记录缓冲
	emitRunning  bool            // 推送协程是否已在运行
	stopEmit     chan struct{}   // 停止推送协程

	stopped bool // 是否已停止：停止后忽略 go-mitmproxy 残留回调（错误/记录）
}

// SetSessionID 设置当前活动会话 ID，使实时记录能正确落入该会话。
func (p *proxy) SetSessionID(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sessionID = id
}

// finalize 为记录设置会话 ID、存入会话存储，并进入批量推送缓冲（定时合并推送前端）。
func (p *proxy) finalize(rec *TrafficRecord) {
	if rec == nil {
		return
	}
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return // 已停止：不再入库/推送，避免停止后残留记录
	}
	sid := p.sessionID
	p.mu.Unlock()
	rec.SessionID = sid
	p.store.Append(*rec)
	if p.emit != nil {
		p.bufferEmit(*rec)
	}
}

// isStopped 返回代理是否已停止（用于忽略残留回调）。
func (p *proxy) isStopped() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stopped
}

// bufferEmit 将记录追加到推送缓冲；若推送协程未运行则启动它。
// 协程每隔 emitFlushInterval 把缓冲内的记录合并成一批推送，从而将高频事件降频。
func (p *proxy) bufferEmit(rec TrafficRecord) {
	p.mu.Lock()
	p.emitBuf = append(p.emitBuf, rec)
	if !p.emitRunning {
		p.emitRunning = true
		p.stopEmit = make(chan struct{})
		go p.emitLoop(p.stopEmit)
	}
	p.mu.Unlock()
}

// emitLoop 定时冲刷推送缓冲。
func (p *proxy) emitLoop(stop <-chan struct{}) {
	t := time.NewTicker(emitFlushInterval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			p.flushEmit()
		}
	}
}

// flushEmit 取出缓冲内全部记录并一次性推送给前端。
func (p *proxy) flushEmit() {
	p.mu.Lock()
	if len(p.emitBuf) == 0 {
		p.mu.Unlock()
		return
	}
	batch := p.emitBuf
	p.emitBuf = nil
	p.mu.Unlock()
	p.emit(batch)
}

// startTimeout 等待代理监听端口成功的阈值。go-mitmproxy 的 Serve 会阻塞，
// 若端口被占用 net.Listen 会立刻返回错误，因此在短时间内即可捕获启动失败。
const startTimeout = 500 * time.Millisecond

// Start 启动代理监听。内部放入 goroutine 运行阻塞的 Serve，避免阻塞调用方；
// 通过 channel 捕获启动阶段的立即错误（如端口占用）并返回。
func (p *proxy) Start() error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- p.p.Start()
	}()
	select {
	case err := <-errCh:
		// Serve 立即返回：监听失败或 server 已关闭
		if err != nil {
			return err
		}
		return nil
	case <-time.After(startTimeout):
		// 进入阻塞 Serve，说明监听成功
		return nil
	}
}

// Stop 关闭代理并停止推送协程（避免泄漏）。置 stopped 以忽略停止后
// go-mitmproxy 在途连接触发的残留回调（错误/记录推送）。
func (p *proxy) Stop() error {
	p.mu.Lock()
	p.stopped = true
	var rest []TrafficRecord
	if p.emitRunning {
		close(p.stopEmit)
		p.emitRunning = false
		p.stopEmit = nil
		// 停止前冲刷剩余缓冲，避免丢最后一批记录
		rest = p.emitBuf
		p.emitBuf = nil
	}
	p.mu.Unlock()
	if len(rest) > 0 && p.emit != nil {
		p.emit(rest)
	}
	return p.p.Close()
}

// newProxy 创建并配置一个 go-mitmproxy 实例。
// addr 为监听地址（如 "127.0.0.1:8888"）。
func newProxy(addr string, caBundle *caBundle, store *SessionStore, filter Filter, emit func([]TrafficRecord), errEmit func(ErrorInfo)) (*proxy, error) {
	p := &proxy{
		ca:        caBundle,
		store:     store,
		filter:    filter,
		emit:      emit,
		errEmit:   errEmit,
		reqBodies: make(map[string][]byte),
	}

	opts := &mitm.Options{
		Addr:              addr,
		StreamLargeBodies: 5 * 1024 * 1024,
		SslInsecure:       true,
		NewCaFunc: func() (cert.CA, error) {
			return caBundle.newCA(), nil
		},
	}

	pr, err := mitm.NewProxy(opts)
	if err != nil {
		return nil, err
	}
	pr.AddAddon(p)
	pr.SetShouldInterceptRule(p.shouldIntercept)
	p.p = pr
	return p, nil
}

// Addr 返回代理实际监听的地址。
func (p *proxy) Addr() string {
	return p.p.Opts.Addr
}

// SetFilter 更新过滤条件。
func (p *proxy) SetFilter(f Filter) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.filter = f
}

func (p *proxy) getFilter() Filter {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.filter
}

// ---- go-mitmproxy Addon 接口实现（嵌入 BaseAddon）----

// shouldIntercept 依据协议勾选决定是否对该请求做 MITM 解密。
// 未勾选任何协议（Protocols 为空）时解析全部。
func (p *proxy) shouldIntercept(req *http.Request) bool {
	f := p.getFilter()
	if len(f.Protocols) == 0 {
		return true // 一个都不选 -> 全部抓取解析
	}
	prot := p.protocolOf(req)
	for _, want := range f.Protocols {
		if strings.EqualFold(want, prot) {
			return true
		}
	}
	return false
}

// protocolOf 推断请求所属协议。
func (p *proxy) protocolOf(req *http.Request) string {
	if req == nil {
		return "http"
	}
	// WebSocket 升级
	if strings.EqualFold(req.Header.Get("Upgrade"), "websocket") ||
		(strings.EqualFold(req.Header.Get("Connection"), "upgrade") && strings.Contains(strings.ToLower(req.Header.Get("Upgrade")), "websocket")) {
		return "websocket"
	}
	// 服务端推送 SSE
	if strings.EqualFold(req.Header.Get("Accept"), "text/event-stream") {
		return "sse"
	}
	// HTTPS：CONNECT 隧道或已建立 TLS
	if req.Method == "CONNECT" || req.TLS != nil || strings.EqualFold(req.URL.Scheme, "https") {
		return "https"
	}
	return "http"
}

// Response 在完整 HTTP(S) 响应读取后调用，记录流量。
func (p *proxy) Response(f *mitm.Flow) {
	if p.isStopped() || f == nil || f.Request == nil {
		return
	}
	req := f.Request
	filter := p.getFilter()

	// 协议标记：SSE/WebSocket 由对应 hook 记录，普通 HTTP(S) 在此记录
	prot := p.protocolOfRaw(req)
	if f.SSE != nil {
		prot = "sse"
	} else if f.WebScoket != nil {
		prot = "websocket"
	}

	// 应用 Host / Method / Path 过滤
	if !p.passFilter(req, filter) {
		return
	}

	var respBody []byte
	statusCode := 0
	statusText := ""
	var respHeaders []model.KV
	if f.Response != nil {
		statusCode = f.Response.StatusCode
		statusText = http.StatusText(statusCode)
		// 解码压缩响应体（gzip/br/deflate/zstd），否则记录到的是压缩乱码
		respBody, _ = f.Response.DecodedBody()
		if respBody == nil {
			respBody = f.Response.Body
		}
		respHeaders = headerToKV(f.Response.Header)
	}

	// 请求体也做同样的解压处理
	reqBody, _ := f.Request.DecodedBody()

	rec := p.store.recordFromReqRespExt(req, respBody, reqBody, headerToKV(req.Header),
		respHeaders, statusCode, statusText, prot, f.StartTime)
	p.finalize(rec)
}

// WebSocketMessage 记录 WebSocket 消息。
func (p *proxy) WebSocketMessage(f *mitm.Flow) {
	if p.isStopped() || f == nil || f.Request == nil || f.WebScoket == nil || len(f.WebScoket.Messages) == 0 {
		return
	}
	filter := p.getFilter()
	if !p.passFilter(f.Request, filter) {
		return
	}
	last := f.WebScoket.Messages[len(f.WebScoket.Messages)-1]
	rec := p.store.recordWebSocket(f.Request, last, f.StartTime)
	p.finalize(rec)
}

// SSEMessage 记录 SSE 事件。
func (p *proxy) SSEMessage(f *mitm.Flow) {
	if p.isStopped() || f == nil || f.Request == nil || f.SSE == nil || len(f.SSE.Events) == 0 {
		return
	}
	filter := p.getFilter()
	if !p.passFilter(f.Request, filter) {
		return
	}
	last := f.SSE.Events[len(f.SSE.Events)-1]
	rec := p.store.recordSSE(f.Request, last, f.StartTime)
	p.finalize(rec)
}

// classifyErr 依据错误内容分类解密失败原因。
func classifyErr(f *mitm.Flow, err error) ErrorInfo {
	msg := err.Error()
	info := ErrorInfo{Type: ErrConnect, Message: msg}
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "x509"):
		// 可能是未信任，也可能是证书固定
		if strings.Contains(lower, "certificate signed by unknown authority") ||
			strings.Contains(lower, "unknown authority") ||
			strings.Contains(lower, "self-signed") {
			info.Type = ErrUntrusted
			info.Message = "根证书未受信任（请先安装并信任根证书）：" + msg
		} else {
			info.Type = ErrPinning
			info.Message = "疑似证书固定（Certificate Pinning），该站点/App 拒绝伪造证书，无法解密：" + msg
		}
	case strings.Contains(lower, "handshake") || strings.Contains(lower, "tls"):
		info.Type = ErrTLS
		info.Message = "TLS 握手失败（可能是证书固定或协议不支持）：" + msg
	case strings.Contains(lower, "connectex") || strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "no such host") || strings.Contains(lower, "timeout"):
		info.Type = ErrConnect
		info.Message = "连接目标失败（网络/端口不通）：" + msg
	case strings.Contains(lower, "upgrade") || strings.Contains(lower, "websocket"):
		info.Type = ErrNonHTTP
		info.Message = "非 HTTP/WebSocket 连接，仅透传未解密：" + msg
	}
	if f != nil && f.Request != nil && f.Request.URL != nil {
		info.Host = f.Request.URL.Host
	}
	return info
}

func (p *proxy) RequestError(f *mitm.Flow, err error) {
	if p.isStopped() || p.errEmit == nil || err == nil {
		return
	}
	p.errEmit(classifyErr(f, err))
}

func (p *proxy) HTTPConnectError(f *mitm.Flow, err error) {
	if p.isStopped() || p.errEmit == nil || err == nil {
		return
	}
	p.errEmit(classifyErr(f, err))
}

// protocolOfRaw 基于原始请求判断协议（无 Flow.SSE/WebScoket 时用）。
func (p *proxy) protocolOfRaw(req *mitm.Request) string {
	if strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
		return "websocket"
	}
	if strings.EqualFold(req.Header.Get("Accept"), "text/event-stream") {
		return "sse"
	}
	if strings.EqualFold(req.URL.Scheme, "https") {
		return "https"
	}
	return "http"
}

// passFilter 依据 Host/Method/Path 过滤条件判断是否记录。
func (p *proxy) passFilter(req *mitm.Request, f Filter) bool {
	if req == nil || req.URL == nil {
		return false
	}
	host := req.URL.Hostname()
	for _, ex := range f.ExcludeHosts {
		if containsStr(host, ex) {
			return false
		}
	}
	if f.Host != "" {
		matched := false
		for _, inc := range splitComma(f.Host) {
			if containsStr(host, inc) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if f.Method != "" && req.Method != f.Method {
		return false
	}
	if f.PathKeyword != "" && !containsStr(req.URL.Path, f.PathKeyword) {
		return false
	}
	return true
}

// headerToKV 将 http.Header 转为键值对列表。
func headerToKV(h http.Header) []model.KV {
	if h == nil {
		return nil
	}
	out := make([]model.KV, 0, len(h))
	for k, vs := range h {
		if len(vs) > 0 {
			out = append(out, model.KV{Key: k, Value: vs[0], Enabled: true})
		}
	}
	return out
}

// splitComma 按逗号分割并去除空白项。
func splitComma(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if t := strings.TrimSpace(part); t != "" {
			out = append(out, t)
		}
	}
	return out
}
