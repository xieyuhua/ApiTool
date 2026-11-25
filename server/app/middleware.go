package app

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ---- IP 令牌桶限流 ----

// rateLimiter 简单的每 IP 令牌桶（容量=burst，速率=rps）。
// 内存实现，适用于单实例服务端；多实例部署需换成 Redis 等共享存储。
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rps     float64
	burst   float64
	lastGC  time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newRateLimiter(rps, burst float64) *rateLimiter {
	return &rateLimiter{buckets: map[string]*bucket{}, rps: rps, burst: burst, lastGC: time.Now()}
}

// allow 判断该 key 当前是否允许一次请求。
func (l *rateLimiter) allow(key string) bool {
	if l == nil || l.rps <= 0 {
		return true // 未启用
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	// 定期清理长时间不活跃的桶，避免内存无限增长
	if now.Sub(l.lastGC) > 10*time.Minute {
		for k, b := range l.buckets {
			if now.Sub(b.last) > 10*time.Minute {
				delete(l.buckets, k)
			}
		}
		l.lastGC = now
	}

	b := l.buckets[key]
	if b == nil {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	// 补充令牌
	b.tokens += now.Sub(b.last).Seconds() * l.rps
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// clientIP 取客户端 IP（优先 X-Forwarded-For 第一个，兼容反向代理）。
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.Index(xff, ","); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if xr := r.Header.Get("X-Real-IP"); xr != "" {
		return strings.TrimSpace(xr)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// withRateLimit 全局限流中间件（按 IP）。
func (s *Server) withRateLimit(next http.Handler) http.Handler {
	if s.limiter == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 静态资源与事件流不计入限流（SSE 是长连接）
		p := r.URL.Path
		if strings.HasPrefix(p, "/assets/") || p == "/events" {
			next.ServeHTTP(w, r)
			return
		}
		if !s.limiter.allow(clientIP(r)) {
			w.Header().Set("Retry-After", "1")
			writeErr(w, http.StatusTooManyRequests, "请求过于频繁，请稍后再试")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---- 登录/注册失败计数（防暴力破解）----

// loginGuard 记录按 IP 的连续失败次数，超阈值后临时锁定。
type loginGuard struct {
	mu       sync.Mutex
	fails    map[string]*failRec
	maxFails int
	window   time.Duration
}

type failRec struct {
	count int
	since time.Time
}

func newLoginGuard(maxFails int, window time.Duration) *loginGuard {
	return &loginGuard{fails: map[string]*failRec{}, maxFails: maxFails, window: window}
}

// locked 判断该 key 是否处于锁定期。
func (g *loginGuard) locked(key string) (bool, time.Duration) {
	if g == nil || g.maxFails <= 0 {
		return false, 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	rec := g.fails[key]
	if rec == nil {
		return false, 0
	}
	// 窗口过期则重置
	if time.Since(rec.since) > g.window {
		delete(g.fails, key)
		return false, 0
	}
	if rec.count >= g.maxFails {
		return true, g.window - time.Since(rec.since)
	}
	return false, 0
}

// fail 记一次失败。
func (g *loginGuard) fail(key string) {
	if g == nil || g.maxFails <= 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	rec := g.fails[key]
	if rec == nil || time.Since(rec.since) > g.window {
		g.fails[key] = &failRec{count: 1, since: time.Now()}
		return
	}
	rec.count++
}

// ok 成功登录后清零。
func (g *loginGuard) ok(key string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.fails, key)
}

// loginKey IP + 用户名（避免同一 IP 多个正常用户互相影响）。
func loginKey(r *http.Request, username string) string {
	return clientIP(r) + "|" + strings.ToLower(strings.TrimSpace(username))
}

// ---- 请求体大小上限 ----

// maxBytes 限制请求体大小（超限返回 413）。
func maxBytes(handler http.HandlerFunc, limit int64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if limit > 0 && r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
		}
		handler(w, r)
	}
}
