package agent

// 单租户访问令牌：一个实例一个 token，通过 Cookie / 请求头 / URL 参数校验。
//
// 为什么必须有：Agent 具备文件读写、执行命令、数据库查询能力，
// 绝不能匿名暴露到网络上。

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

const (
	tokenCookieName = "agent_token"
	tokenHeaderName = "X-Agent-Token"
)

// checkToken 校验请求携带的访问令牌。
// 顺序：Header → Cookie → URL 参数（?token=）。URL 参数供首次访问使用，
// 校验通过后由中间件写入 Cookie 并重定向到不带 token 的地址。
func (s *Server) checkToken(r *http.Request) bool {
	if s.cfg.Token == "" {
		// 未配置令牌：仅允许回环地址访问（启动时已校验绑定地址）
		return s.cfg.AllowLocalNoTok
	}
	want := []byte(s.cfg.Token)
	if v := r.Header.Get(tokenHeaderName); v != "" {
		return subtle.ConstantTimeCompare([]byte(v), want) == 1
	}
	if c, err := r.Cookie(tokenCookieName); err == nil && c.Value != "" {
		return subtle.ConstantTimeCompare([]byte(c.Value), want) == 1
	}
	if v := strings.TrimSpace(r.URL.Query().Get("token")); v != "" {
		return subtle.ConstantTimeCompare([]byte(v), want) == 1
	}
	return false
}

// setTokenCookie 记录令牌，免去后续每次请求都要带参数。
func (s *Server) setTokenCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     tokenCookieName,
		Value:    s.cfg.Token,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   int(s.cfg.TokenTTL.Seconds()),
		SameSite: http.SameSiteLaxMode,
	})
}

// requireAuth 包装需要鉴权的处理器。
// 浏览器页面请求（GET + HTML）在令牌缺失时返回可读提示页，
// API 请求返回 401 JSON，便于前端桥接给出明确错误。
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.checkToken(r) {
			// URL 带 token 访问成功：写 Cookie 并从地址栏抹掉 token，
			// 避免历史记录/截图泄露。
			if r.URL.Query().Get("token") != "" {
				s.setTokenCookie(w)
				q := r.URL.Query()
				q.Del("token")
				target := r.URL.Path
				if enc := q.Encode(); enc != "" {
					target += "?" + enc
				}
				http.Redirect(w, r, target, http.StatusFound)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/rpc/") {
			writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
				"ok": false, "error": "访问令牌无效或已过期，请使用带 ?token= 的链接重新打开",
			})
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(lockedHTML))
	})
}

// lockedHTML 令牌无效时的提示页。
const lockedHTML = `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>需要访问令牌</title>
<style>body{font-family:system-ui,-apple-system,"Microsoft YaHei",sans-serif;display:flex;align-items:center;
justify-content:center;height:100vh;margin:0;background:#f5f6f8;color:#1f2329}
.box{background:#fff;padding:28px 32px;border-radius:12px;box-shadow:0 4px 20px rgba(0,0,0,.08);max-width:440px;text-align:center}
h3{margin:0 0 10px;font-size:17px}p{margin:0;color:#86909c;font-size:13px;line-height:1.7}
code{background:#f2f3f5;padding:1px 6px;border-radius:4px;font-size:12px}</style></head>
<body><div class="box"><h3>🔒 需要有效的访问令牌</h3>
<p>该地址缺少或已失效的访问令牌。<br>请使用启动服务时打印的、带 <code>?token=...</code> 的链接访问。</p>
<p style="margin-top:10px">Agent 具备文件读写与命令执行能力，因此必须凭令牌访问。</p></div></body></html>`

// tokenLink 生成带令牌的访问链接（启动日志打印用）。
func (s *Server) tokenLink(host string) string {
	base := "http://" + host
	if s.cfg.Token == "" {
		return base
	}
	return base + "/?token=" + s.cfg.Token
}
