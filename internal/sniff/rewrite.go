package sniff

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
)

// HostRewrite 域名重定向规则：把 From 域名的请求改发到 To 目标地址，便于本地测试。
// 例如 dev.test.com → 127.0.0.1:8200，可把线上域名指向本地联调服务。
type HostRewrite struct {
	ID      string `json:"id"`      // 规则 ID
	From    string `json:"from"`    // 源域名（含端口可省略），如 dev.test.com
	To      string `json:"to"`      // 目标地址，如 127.0.0.1:8200
	Enabled bool   `json:"enabled"` // 是否启用
	Desc    string `json:"desc"`    // 备注说明
	// Scheme 目标转发协议："" = 自动（跟随原请求；目标端口非 443 时视为 HTTP，
	// 因为本地联调服务通常是明文 HTTP）；"http"/"https" = 强制指定。
	Scheme string `json:"scheme,omitempty"`
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
