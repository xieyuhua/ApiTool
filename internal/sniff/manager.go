package sniff

import (
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"

	"apitool/internal/bus"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Event 名常量（前端 EventsOn 监听）。
const (
	EventRecord = "sniff:record" // 实时流量记录推送
	EventStatus = "sniff:status" // 抓包状态变更
	EventError  = "sniff:error"  // 证书/TLS 等错误提示推送
)

// Manager 抓包管理器：聚合 CA / 代理 / 会话存储 / 系统代理，对外提供控制面。
type Manager struct {
	ca       *caBundle
	store    *SessionStore
	proxy    *proxy
	session  *Session
	bus      bus.Bus
	caDir    string
	filter   Filter
	status   Status
	sysProxy bool // 是否随抓包自动设置系统代理

	rwMu     sync.Mutex
	rewrites []HostRewrite // 域名重定向规则（持久化到 caDir/rewrites.json）
}

// Status 抓包运行状态（推送给前端）。
type Status struct {
	Running       bool   `json:"running"`
	ProxyAddr     string `json:"proxyAddr"`
	CertURL       string `json:"certURL"`     // 局域网地址，供手机访问下载证书
	LocalCertURL  string `json:"localCertURL"` // 本机地址，供电脑浏览器下载证书
	CAInstalled   bool   `json:"caInstalled"`
	CAFingerprint string `json:"caFingerprint"`
	SystemProxy   bool   `json:"systemProxy"`
	Error         string `json:"error"`
	ActiveSessionID string `json:"activeSessionId"` // 当前活动会话 ID（供查看错误日志等）
}

// Filter 抓包过滤条件（前端设置后回传）。
// 解密失败错误类型分类
const (
	ErrPinning   = "pinning"   // 证书固定
	ErrUntrusted = "untrusted" // 根证书未受信任
	ErrTLS       = "tls"       // TLS 握手失败
	ErrConnect   = "connect"   // 连接目标失败
	ErrNonHTTP   = "non_http"  // 非 HTTP 连接（仅透传）
)

// ErrorInfo 解密/连接失败的结构化信息（推送给前端分类展示）。
type ErrorInfo struct {
	Type    string `json:"type"`
	Host    string `json:"host"`
	Message string `json:"message"`
}

type Filter struct {
	Host         string   `json:"host"`         // 按主机名包含匹配（逗号分隔，不区分大小写，空=全部）
	ExcludeHosts []string `json:"excludeHosts"` // 排除的主机名（不区分大小写）
	ProcessName  string   `json:"processName"`  // 预留：按进程名过滤（Windows 需驱动级，暂未启用）
	Method       string   `json:"method"`       // 限定 HTTP 方法（空表示不限）
	PathKeyword  string   `json:"pathKeyword"`  // 路径关键字包含匹配
	OnlyHTTP     bool     `json:"onlyHttp"`     // 仅记录 HTTP/HTTPS
	Protocols    []string `json:"protocols"`    // 勾选的协议列表（http/https/websocket/wss/sse/grpc/graphql）；空=全部解析。https 与 http、wss 与 websocket 互为别名
}

// NewManager 创建管理器。caDir 为 CA 与抓包数据目录。
func NewManager(caDir string, b bus.Bus) (*Manager, error) {
	ca, err := loadOrCreateCA(caDir)
	if err != nil {
		return nil, err
	}
	store := NewSessionStore(caDir)
	m := &Manager{
		ca:    ca,
		store: store,
		bus:   b,
		caDir: caDir,
	}
	m.status.CAFingerprint = ca.FingerprintSHA1()
	m.status.CAInstalled = IsCAInstalled(ca.FingerprintSHA1())
	m.loadRewrites()
	return m, nil
}

// CA 返回根 CA（供前端导出/展示）。
func (m *Manager) CA() *caBundle { return m.ca }

// SetSystemProxyEnabled 设置是否随抓包自动切换系统代理。
// 关闭时仅启动监听端口，供用户手动在浏览器/应用中配置代理。
func (m *Manager) SetSystemProxyEnabled(enabled bool) {
	m.sysProxy = enabled
	m.status.SystemProxy = enabled
	m.emitStatus()
}

// Start 启动抓包。addr 为监听地址（如 "127.0.0.1:8888"）。
func (m *Manager) Start(addr string) error {
	if m.proxy != nil {
		return fmt.Errorf("抓包已在运行")
	}
	p, err := newProxy(addr, m.ca, m.store, m.filter, m.onRecord, m.onError)
	if err != nil {
		return err
	}
	m.rwMu.Lock()
	p.SetRewrites(m.rewrites)
	m.rwMu.Unlock()
	// 创建活动会话，使实时记录能落入其中（Stop 时落盘）
	sess := m.store.NewSession(addr)
	p.SetSessionID(sess.ID)
	m.session = sess
	m.status.ActiveSessionID = sess.ID

	p.Start()
	m.proxy = p
	m.status.Running = true
	m.status.ProxyAddr = p.Addr()
	port := portOf(p.Addr())
	m.status.LocalCertURL = "http://127.0.0.1:" + port + "/proxy.pem"
	m.status.CertURL = "http://" + lanIPv4() + ":" + port + "/proxy.pem"
	m.status.Error = ""
	m.status.SystemProxy = m.sysProxy
	m.emitStatus()
	// 仅在用户开启“系统代理”开关时切换全局代理
	if m.sysProxy {
		if err := SetSystemProxy(p.Addr()); err != nil {
			// 非致命：提示用户手动设置
			m.status.Error = "已启动代理，但自动设置系统代理失败：" + err.Error()
		}
	}
	m.emitStatus()
	return nil
}

// Stop 停止抓包、保存会话并还原系统代理（仅在启用过时）。
// 为避免大流量会话（含完整响应体，单条可达数十 MB）的序列化落盘阻塞
// Wails 主调用线程导致前端「停止」按钮长时间无响应，这里仅同步关闭代理
// 并立即将状态置为已停止（前端 await 秒回），将耗时的会话落盘放入后台 goroutine。
func (m *Manager) Stop() error {
	if m.proxy == nil {
		return nil
	}
	// 同步关闭代理：立刻停止接收新流量，状态立即更新
	m.proxy.Stop()
	saved := m.session
	m.proxy = nil
	m.session = nil

	m.status.Running = false
	m.status.ProxyAddr = ""
	m.status.CertURL = ""
	m.status.LocalCertURL = ""
	if m.sysProxy {
		_ = ClearSystemProxy()
	}
	m.emitStatus()

	// 后台异步落盘：不阻塞前端「停止」操作
	if saved != nil {
		go func(sess *Session) {
			_ = m.store.Save(sess)
		}(saved)
	}
	return nil
}

// SetFilter 更新过滤条件。
func (m *Manager) SetFilter(f Filter) {
	m.filter = f
	if m.proxy != nil {
		m.proxy.SetFilter(f)
	}
}

// ListSessions 返回抓包会话列表。
func (m *Manager) ListSessions() []Session {
	return m.store.List()
}

// GetSession 获取完整会话（records 全量）。
func (m *Manager) GetSession(id string) (*Session, bool) {
	return m.store.Get(id)
}

// GetSessionErrors 返回指定会话的解密失败日志。
func (m *Manager) GetSessionErrors(id string) []ErrorInfo {
	return m.store.GetErrors(id)
}

// DeleteSession 删除会话。
func (m *Manager) DeleteSession(id string) error {
	return m.store.Delete(id)
}

// ExportOpenAPI 将会话导为 OpenAPI 文档（弹出保存对话框）。
func (m *Manager) ExportOpenAPI(id, title string) (string, error) {
	sess, ok := m.store.Get(id)
	if !ok {
		return "", fmt.Errorf("会话不存在")
	}
	text, err := m.store.ToOpenAPI(sess)
	if err != nil {
		if err == io.EOF {
			return "", fmt.Errorf("该会话没有可导出的 HTTP(S) 流量")
		}
		return "", err
	}
	if m.bus == nil {
		// 无对话框能力（理论上不会发生），不弹窗，返回空路径避免误展示文档内容
		return "", nil
	}
	save, serr := m.bus.SaveFileDialog(runtimeSaveDialog(title))
	if serr != nil {
		return "", serr
	}
	if save == "" {
		// 用户取消保存，返回空路径（不返回文档内容，避免被当作结果展示）
		return "", nil
	}
	if werr := writeFile(save, text); werr != nil {
		return "", werr
	}
	return save, nil
}

// ExportOpenAPIText 返回 OpenAPI 文本（供前端复制到剪贴板）。
func (m *Manager) ExportOpenAPIText(id, title string) (string, error) {
	sess, ok := m.store.Get(id)
	if !ok {
		return "", fmt.Errorf("会话不存在")
	}
	return m.store.ToOpenAPI(sess)
}

// Status 返回当前状态。
func (m *Manager) Status() Status { return m.status }

// InstallCA 将根 CA 安装到系统信任库（需管理员权限）。
func (m *Manager) InstallCA() error {
	if err := InstallCA(m.ca.CertPEM()); err != nil {
		return err
	}
	m.status.CAInstalled = true
	m.emitStatus()
	return nil
}

// ExportCAPath 返回根 CA 证书文件路径（供用户手动安装）。
func (m *Manager) ExportCAPath() string {
	if m.ca == nil {
		return ""
	}
	return m.ca.CAPath()
}

// ImportCA 导入用户提供的根证书（证书 + 私钥 PEM），替换当前 CA 并持久化。
// 返回新 CA 的指纹。可用于直接复用 Fiddler 等现有根证书。
func (m *Manager) ImportCA(certPEM, keyPEM string) (string, error) {
	if m.ca == nil {
		return "", fmt.Errorf("CA 未初始化")
	}
	if err := m.ca.ImportCA([]byte(certPEM), []byte(keyPEM)); err != nil {
		return "", err
	}
	fp := m.ca.FingerprintSHA1()
	m.status.CAFingerprint = fp
	m.status.CAInstalled = IsCAInstalled(fp)
	m.emitStatus()
	return fp, nil
}

// runtimeSaveDialog 构造保存对话框选项。
func runtimeSaveDialog(title string) runtime.SaveDialogOptions {
	return runtime.SaveDialogOptions{
		Title:           title,
		DefaultFilename: "openapi.json",
		Filters: []runtime.FileFilter{
			{DisplayName: "OpenAPI JSON", Pattern: "*.json"},
			{DisplayName: "YAML", Pattern: "*.yaml;*.yml"},
		},
	}
}

// writeFile 写文本文件。
func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// liveBodyClipLimit 实时推送单条 body 剪裁阈值：超大非图片 body 截断，
// 避免大响应体（最大 64MB）通过 IPC 传输与前端内存占用导致界面卡顿。
// 会话存储仍保存完整数据，不影响历史查看与生成接口等功能。
const liveBodyClipLimit = 1 << 20 // 1MB

// liveRawBodyClipLimit 实时推送时原始字节 base64 的保留上限
// （1MB 原始字节对应的 base64 长度，取整到 4 的倍数以保证解码合法）。
const liveRawBodyClipLimit = (liveBodyClipLimit / 3) * 4

// clipLiveRecords 对实时推送的记录做剪裁：非图片 body 超过阈值时截断并打标记。
// 图片响应保留完整 base64（预览需要）。仅修改推送切片副本，不影响已入库的完整数据。
func clipLiveRecords(records []TrafficRecord) []TrafficRecord {
	for i := range records {
		r := &records[i]
		if len(r.ReqBody) > liveBodyClipLimit {
			r.ReqBody = r.ReqBody[:liveBodyClipLimit] + "\n...[已截断]"
			r.ReqClipped = true
		}
		if len(r.RespBody) > liveBodyClipLimit && !strings.HasPrefix(strings.ToLower(r.RespContentType), "image/") {
			r.RespBody = r.RespBody[:liveBodyClipLimit] + "\n...[已截断]"
			r.RespClipped = true
		}
		// 原始字节 base64 同步裁剪，保证与文本视图看到的数据范围一致
		if len(r.RespBodyBase64) > liveRawBodyClipLimit {
			r.RespBodyBase64 = r.RespBodyBase64[:liveRawBodyClipLimit]
			r.RespClipped = true
		}
	}
	return records
}

func (m *Manager) onRecord(records []TrafficRecord) {
	if m.bus != nil && len(records) > 0 {
		m.bus.Emit(EventRecord, clipLiveRecords(records))
	}
}

func (m *Manager) onError(info ErrorInfo) {
	if m.bus != nil {
		m.bus.Emit(EventError, info)
	}
}

func (m *Manager) emitStatus() {
	if m.bus != nil {
		m.bus.Emit(EventStatus, m.status)
	}
}

// lanIPv4 返回本机第一个非回环 IPv4 地址（供手机通过局域网访问）。
func lanIPv4() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ip4 := ipnet.IP.To4(); ip4 != nil {
				return ip4.String()
			}
		}
	}
	return "127.0.0.1"
}

// portOf 从监听地址（host:port）中提取端口部分。
func portOf(addr string) string {
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[i+1:]
	}
	return addr
}
