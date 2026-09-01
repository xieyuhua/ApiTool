package sniff

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// RewriteItem 参数替换项：对请求的 Query 参数或 Header 做替换/新增/删除，
// 应用在域名（及可选路径/查询串）改写之后。
type RewriteItem struct {
	Type    string `json:"type"`    // "query"（URL 查询参数）/ "header"（请求头）
	Action  string `json:"action"`  // "set"（替换值，不存在则新增）/ "del"（删除该键）
	Key     string `json:"key"`     // 参数名 / Header 名
	Value   string `json:"value"`   // 替换后的值（Action=del 时忽略）
	Enabled bool   `json:"enabled"` // 是否启用
}

// HostRewrite 请求改写规则：把 From 域名的请求改发到 To 目标地址，便于本地测试。
// 例如 dev.test.com → 127.0.0.1:8200，可把线上域名指向本地联调服务。
type HostRewrite struct {
	ID      string `json:"id"`      // 规则 ID
	From    string `json:"from"`    // 源域名（含端口可省略），如 dev.test.com
	To      string `json:"to"`      // 目标地址，如 127.0.0.1:8200；支持带路径/查询串，如 api.test.com/v2/api?v=2
	Enabled bool   `json:"enabled"` // 是否启用
	Desc    string `json:"desc"`    // 备注说明
	// Scheme 目标转发协议："" = 自动（跟随原请求；目标端口非 443 时视为 HTTP，
	// 因为本地联调服务通常是明文 HTTP）；"http"/"https" = 强制指定。
	Scheme string `json:"scheme,omitempty"`
	// Replacements 参数替换列表：对 Query 参数 / Header 做替换、新增、删除。
	// 置空则不修改路径、查询参数与请求头。
	Replacements []RewriteItem `json:"replacements,omitempty"`
}

// splitRewriteTarget 将 To 拆分为「主机目标」与「路径/查询串」两部分。
// 支持三种写法：
//   - "127.0.0.1:8200"            → host 目标，路径保留原样
//   - "api.test.com/v2/api?v=2"    → host + 路径 + 查询串（路径/查询整体替换）
//   - "http(s)://..."              → 去掉显式协议前缀后同上
func splitRewriteTarget(to string) (target, pathQuery string) {
	to = strings.TrimSpace(to)
	if i := strings.Index(to, "://"); i >= 0 {
		to = to[i+3:] // 去掉显式协议前缀
	}
	if i := strings.IndexAny(to, "/?"); i >= 0 {
		return to[:i], to[i:]
	}
	return to, ""
}

// applyReplacements 应用参数替换列表到请求（Query 参数与 Header）。
// 修改 URL.RawQuery 时保留原始参数的顺序与未命中项的原始编码。
func applyReplacements(reqURL *url.URL, h http.Header, items []RewriteItem) {
	if len(items) == 0 {
		return
	}
	applyQueryParams(reqURL, items)
	applyHeaderRewrites(h, items)
}

// applyQueryParams 对 URL 查询参数应用替换项。
func applyQueryParams(u *url.URL, items []RewriteItem) {
	// 先收集命中 query 的替换项；无则跳过
	hasQuery := false
	for _, it := range items {
		if it.Type == "query" && it.Enabled && it.Key != "" {
			hasQuery = true
			break
		}
	}
	if !hasQuery || u == nil {
		return
	}
	// hasEq 记录原始参数是否带等号，用于原样还原 key= 这类空值参数
	type kv struct {
		k, v  string
		hasEq bool
	}
	var parts []kv
	if u.RawQuery != "" {
		for _, pair := range strings.Split(u.RawQuery, "&") {
			if pair == "" {
				continue
			}
			k := pair
			v := ""
			hasEq := false
			if i := strings.IndexByte(pair, '='); i >= 0 {
				k, v, hasEq = pair[:i], pair[i+1:], true
			}
			parts = append(parts, kv{k, v, hasEq})
		}
	}
	for _, it := range items {
		if it.Type != "query" || !it.Enabled || it.Key == "" {
			continue
		}
		if it.Action == "del" {
			var out []kv
			for _, p := range parts {
				if p.k != it.Key {
					out = append(out, p)
				}
			}
			parts = out
			continue
		}
		// set：替换所有同名键；不存在则追加
		replaced := false
		for i := range parts {
			if parts[i].k == it.Key {
				parts[i].v = url.QueryEscape(it.Value)
				parts[i].hasEq = true
				replaced = true
			}
		}
		if !replaced {
			parts = append(parts, kv{it.Key, url.QueryEscape(it.Value), true})
		}
	}
	var sb strings.Builder
	for i, p := range parts {
		if i > 0 {
			sb.WriteByte('&')
		}
		sb.WriteString(p.k)
		if p.hasEq {
			sb.WriteByte('=')
			sb.WriteString(p.v)
		}
	}
	u.RawQuery = sb.String()
}

// applyHeaderRewrites 对请求头应用替换项。
func applyHeaderRewrites(h http.Header, items []RewriteItem) {
	for _, it := range items {
		if it.Type != "header" || !it.Enabled || it.Key == "" {
			continue
		}
		if it.Action == "del" {
			h.Del(it.Key)
		} else {
			h.Set(it.Key, it.Value)
		}
	}
}

// applyRewriteTarget 计算改写后的目标 host 与转发协议。
// 规则：
//   - 显式 Scheme（http/https）优先；
//   - 未指定时：目标带端口且端口 != 443 → http（避免 TLS 连明文端口报
//     "server gave HTTP response to HTTPS client"）；端口 443 或无端口 → 跟随原请求 scheme；
//   - 目标未带端口时按最终 scheme 补默认端口（HTTPS :443 / HTTP :80）。
func applyRewriteTarget(to, origScheme, explicit string) (string, string) {
	scheme := strings.ToLower(strings.TrimSpace(explicit))
	if scheme != "http" && scheme != "https" {
		scheme = strings.ToLower(origScheme)
		if host, port, err := net.SplitHostPort(to); err == nil {
			_ = host
			if port != "443" {
				scheme = "http"
			}
		}
	}
	if _, _, err := net.SplitHostPort(to); err != nil {
		if scheme == "https" {
			to += ":443"
		} else {
			to += ":80"
		}
	}
	return to, scheme
}

// rewritesPath 返回重定向规则持久化文件路径（与 CA 同目录）。
func (m *Manager) rewritesPath() string {
	return filepath.Join(m.caDir, "rewrites.json")
}

// loadRewrites 加载持久化的重定向规则（文件不存在/损坏时返回空列表）。
func (m *Manager) loadRewrites() {
	m.rwMu.Lock()
	defer m.rwMu.Unlock()
	data, err := os.ReadFile(m.rewritesPath())
	if err != nil {
		return
	}
	var list []HostRewrite
	if err := json.Unmarshal(data, &list); err != nil {
		return
	}
	m.rewrites = list
}

// saveRewrites 将当前规则持久化到磁盘。
func (m *Manager) saveRewrites() error {
	m.rwMu.Lock()
	list := append([]HostRewrite(nil), m.rewrites...)
	m.rwMu.Unlock()
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.rewritesPath(), data, 0o644)
}

// GetRewrites 返回全部重定向规则（供前端弹窗维护）。
func (m *Manager) GetRewrites() []HostRewrite {
	m.rwMu.Lock()
	defer m.rwMu.Unlock()
	return append([]HostRewrite(nil), m.rewrites...)
}

// SetRewrites 整体替换重定向规则并持久化；运行中的代理立即生效。
func (m *Manager) SetRewrites(list []HostRewrite) error {
	if list == nil {
		list = []HostRewrite{}
	}
	// 规范化协议字段：仅允许 ""/"http"/"https"，其余视为自动
	for i := range list {
		s := strings.ToLower(strings.TrimSpace(list[i].Scheme))
		if s != "http" && s != "https" {
			s = ""
		}
		list[i].Scheme = s
	}
	m.rwMu.Lock()
	m.rewrites = list
	m.rwMu.Unlock()
	if err := m.saveRewrites(); err != nil {
		return err
	}
	if m.proxy != nil {
		m.proxy.SetRewrites(list)
	}
	return nil
}

// matchRewrite 依据请求主机名匹配启用的重定向规则。
// 匹配逻辑：忽略端口与大小写；支持通配前缀 *.example.com 匹配子域。
func matchRewrite(rules []HostRewrite, host string) *HostRewrite {
	h := strings.ToLower(host)
	if i := strings.IndexByte(h, ':'); i >= 0 {
		h = h[:i] // 去掉端口
	}
	for i := range rules {
		r := &rules[i]
		if !r.Enabled {
			continue
		}
		from := strings.ToLower(r.From)
		if i := strings.IndexByte(from, ':'); i >= 0 {
			from = from[:i]
		}
		from = strings.TrimSpace(from)
		if from == "" {
			continue
		}
		if strings.HasPrefix(from, "*.") {
			suffix := from[1:] // ".example.com"
			if strings.HasSuffix(h, suffix) {
				return r
			}
			continue
		}
		if h == from {
			return r
		}
	}
	return nil
}
