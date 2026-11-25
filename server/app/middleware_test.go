package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiter(t *testing.T) {
	l := newRateLimiter(10, 5) // 10 rps，容量 5
	allowed := 0
	for i := 0; i < 10; i++ {
		if l.allow("ip1") {
			allowed++
		}
	}
	// 突发容量 5（初始满桶），10 次连续请求应只允许 ~5 次
	if allowed < 4 || allowed > 6 {
		t.Fatalf("突发限流应允许约 5 次，实际 %d", allowed)
	}
	// 其它 IP 不受影响
	if !l.allow("ip2") {
		t.Fatal("不同 IP 应独立计数")
	}
	// 关闭限流
	l2 := newRateLimiter(0, 0)
	for i := 0; i < 100; i++ {
		if !l2.allow("x") {
			t.Fatal("rps=0 应不限流")
		}
	}
}

func TestLoginGuard(t *testing.T) {
	g := newLoginGuard(3, 100*time.Millisecond)
	key := "1.2.3.4|alice"
	for i := 0; i < 3; i++ {
		if locked, _ := g.locked(key); locked {
			t.Fatalf("第 %d 次前不应锁定", i+1)
		}
		g.fail(key)
	}
	if locked, _ := g.locked(key); !locked {
		t.Fatal("3 次失败后应锁定")
	}
	// 其它 key 不受影响
	if locked, _ := g.locked("5.6.7.8|bob"); locked {
		t.Fatal("其它用户不应被锁定")
	}
	// 窗口过期后解锁
	time.Sleep(120 * time.Millisecond)
	if locked, _ := g.locked(key); locked {
		t.Fatal("窗口过期后应解锁")
	}
	// 成功清零
	g.fail(key)
	g.fail(key)
	g.ok(key)
	if locked, _ := g.locked(key); locked {
		t.Fatal("成功后应清零")
	}
}

func TestAgentQuota(t *testing.T) {
	q := newAgentQuotaTracker(2)
	if err := q.take("u1"); err != nil {
		t.Fatal(err)
	}
	if err := q.take("u1"); err != nil {
		t.Fatal(err)
	}
	if err := q.take("u1"); err == nil {
		t.Fatal("超过配额应报错")
	}
	if q.usage("u1") != 2 {
		t.Fatalf("用量应为 2，实际 %d", q.usage("u1"))
	}
	// 其它用户独立
	if err := q.take("u2"); err != nil {
		t.Fatal(err)
	}
	// 不限额
	q0 := newAgentQuotaTracker(0)
	for i := 0; i < 100; i++ {
		if err := q0.take("u"); err != nil {
			t.Fatal("配额 0 应不限")
		}
	}
}

func TestMaxBytes(t *testing.T) {
	h := maxBytes(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1024)
		_, err := r.Body.Read(buf)
		if err != nil && err.Error() != "EOF" {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}, 10)
	big := httptest.NewRequest("POST", "/x", nil)
	big.Body = http.NoBody
	// 超大请求体通过 MaxBytesReader 在读取时返回错误
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/x", &largeReader{})
	h(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("超限请求应 413，实际 %d", rec.Code)
	}
}

// largeReader 产生 100 字节数据。
type largeReader struct{ n int }

func (l *largeReader) Read(p []byte) (int, error) {
	if l.n >= 100 {
		return 0, nil
	}
	n := len(p)
	if n > 100-l.n {
		n = 100 - l.n
	}
	for i := 0; i < n; i++ {
		p[i] = 'a'
	}
	l.n += n
	return n, nil
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:12345"
	if got := clientIP(r); got != "10.0.0.1" {
		t.Fatalf("直连 IP: %s", got)
	}
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8")
	if got := clientIP(r); got != "1.2.3.4" {
		t.Fatalf("XFF 首段: %s", got)
	}
	r2 := httptest.NewRequest("GET", "/", nil)
	r2.Header.Set("X-Real-IP", "9.9.9.9")
	if got := clientIP(r2); got != "9.9.9.9" {
		t.Fatalf("X-Real-IP: %s", got)
	}
}
