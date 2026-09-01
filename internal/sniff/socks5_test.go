package sniff

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeTestTLSCert 生成临时自签证书并写入 dir，返回 cert/key 文件路径。
func writeTestTLSCert(t *testing.T, dir string) (certFile, keyFile string) {
	t.Helper()
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	certFile = filepath.Join(dir, "srv.pem")
	keyFile = filepath.Join(dir, "srv.key")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	kb, _ := x509.MarshalECPrivateKey(priv)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb})
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile
}

// socks5Connect 完成一次 SOCKS5 握手并 CONNECT 到 target，返回已建立的隧道连接。
func socks5Connect(proxyAddr, target string) (net.Conn, error) {
	c, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		return nil, err
	}
	// 协商：仅无认证
	if _, err := c.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		c.Close()
		return nil, err
	}
	rep := make([]byte, 2)
	if _, err := io.ReadFull(c, rep); err != nil {
		c.Close()
		return nil, err
	}
	if rep[0] != 0x05 || rep[1] != 0x00 {
		c.Close()
		return nil, fmt.Errorf("socks5 协商失败: %v", rep)
	}
	// CONNECT：VER CMD RSV ATYP(域名) 长度 host port
	host, portStr, _ := net.SplitHostPort(target)
	port := 0
	fmt.Sscanf(portStr, "%d", &port)
	req := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	req = append(req, []byte(host)...)
	req = append(req, byte(port>>8), byte(port))
	if _, err := c.Write(req); err != nil {
		c.Close()
		return nil, err
	}
	hdr := make([]byte, 10)
	if _, err := io.ReadFull(c, hdr); err != nil {
		c.Close()
		return nil, err
	}
	if hdr[1] != 0x00 {
		c.Close()
		return nil, fmt.Errorf("socks5 CONNECT 失败: rep=%d", hdr[1])
	}
	return c, nil
}

// startTestProxy 启动一个混合代理用于测试，返回监听地址与 store（记录读取）。
func startTestProxy(t *testing.T) (*proxy, func()) {
	t.Helper()
	dir := t.TempDir()
	ca, err := loadOrCreateCA(dir)
	if err != nil {
		t.Fatalf("loadOrCreateCA: %v", err)
	}
	store := NewSessionStore(dir)
	p, err := newProxy("127.0.0.1:0", ca, store, Filter{}, func([]TrafficRecord) {}, func(ErrorInfo) {})
	if err != nil {
		t.Fatalf("newProxy: %v", err)
	}
	if err := p.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	sess := store.NewSession(p.Addr())
	p.SetSessionID(sess.ID)
	return p, func() { _ = p.Stop() }
}

// waitRecord 轮询等待出现匹配 host 的记录（解密抓包生效）。
func waitRecord(t *testing.T, store *SessionStore, host string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, s := range store.List() {
			if sess, ok := store.Get(s.ID); ok {
				for _, r := range sess.Records {
					if strings.Contains(r.Host, host) {
						return true
					}
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func TestHybridSOCKS5HTTPS(t *testing.T) {
	// 本地 HTTPS 测试服务器（自签证书，SslInsecure 会忽略校验）
	cf, kf := writeTestTLSCert(t, t.TempDir())
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("hello-over-socks5"))
		}),
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.ServeTLS(ln, cf, kf) }()
	defer srv.Close()
	target := ln.Addr().String()

	p, stop := startTestProxy(t)
	defer stop()
	store := p.store

	// 通过 SOCKS5 客户端经混合端口访问 HTTPS 服务器
	c, err := socks5Connect(p.Addr(), target)
	if err != nil {
		t.Fatalf("socks5Connect: %v", err)
	}
	defer c.Close()
	// 隧道内发起 TLS 请求
	client := tls.Client(c, &tls.Config{InsecureSkipVerify: true, ServerName: "localhost"})
	if err := client.Handshake(); err != nil {
		t.Fatalf("TLS handshake over socks5: %v", err)
	}
	req, _ := http.NewRequest("GET", "https://"+target+"/", nil)
	if err := req.Write(client); err != nil {
		t.Fatalf("write request: %v", err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(client), req)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "hello-over-socks5" {
		t.Fatalf("响应体异常: %q", body)
	}

	th, _, _ := net.SplitHostPort(target)
	if !waitRecord(t, store, th, 3*time.Second) {
		t.Fatalf("未抓到通过 SOCKS5 访问 %s 的 HTTPS 流量", target)
	}
}

func TestHybridSOCKS5HTTP(t *testing.T) {
	// 本地明文 HTTP 测试服务器
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("plain-http-via-socks5"))
		}),
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	defer srv.Close()
	target := ln.Addr().String()

	p, stop := startTestProxy(t)
	defer stop()
	store := p.store

	// SOCKS5 握手（不直接 CONNECT 内嵌 TLS，而是交给 relayHTTP 处理明文请求）
	c, err := socks5Connect(p.Addr(), target)
	if err != nil {
		t.Fatalf("socks5Connect: %v", err)
	}
	defer c.Close()
	// 隧道内直接发明文 HTTP 请求（relative-form）
	req, _ := http.NewRequest("GET", "/", nil)
	req.Host = target
	if err := req.Write(c); err != nil {
		t.Fatalf("write plain request: %v", err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(c), req)
	if err != nil {
		t.Fatalf("read plain response: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "plain-http-via-socks5" {
		t.Fatalf("明文响应体异常: %q", body)
	}

	th2, _, _ := net.SplitHostPort(target)
	if !waitRecord(t, store, th2, 3*time.Second) {
		t.Fatalf("未抓到通过 SOCKS5 访问 %s 的明文 HTTP 流量", target)
	}
}

// TestHybridPlainHTTPProxy 验证混合端口同样可作为原生 HTTP 正向代理使用，
// 即「同一端口」既接 SOCKS5 也接 HTTP 代理，互不破坏。
func TestHybridPlainHTTPProxy(t *testing.T) {
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("plain-http-proxy"))
		}),
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	defer srv.Close()
	target := ln.Addr().String()

	p, stop := startTestProxy(t)
	defer stop()
	store := p.store

	// 直接以 HTTP 正向代理方式访问（absolute-form 请求行），不使用 SOCKS5
	// 直接用底层 TCP 走 HTTP 正向代理（absolute-form），避免 http.Client 行为干扰排查
	c2, err := net.Dial("tcp", p.Addr())
	if err != nil {
		t.Fatalf("dial hybrid: %v", err)
	}
	defer c2.Close()
	_ = c2.SetDeadline(time.Now().Add(8 * time.Second))
	reqLine := "GET http://" + target + "/ HTTP/1.1\r\nHost: " + target + "\r\nConnection: close\r\n\r\n"
	if _, err := c2.Write([]byte(reqLine)); err != nil {
		t.Fatalf("write proxy req: %v", err)
	}
	buf := make([]byte, 4096)
	n, err := c2.Read(buf)
	if err != nil && n == 0 {
		t.Fatalf("read proxy resp: %v", err)
	}
	body := string(buf[:n])
	if !strings.Contains(body, "plain-http-proxy") {
		t.Fatalf("明文代理响应异常: %q", body)
	}

	th, _, _ := net.SplitHostPort(target)
	if !waitRecord(t, store, th, 3*time.Second) {
		t.Fatalf("未抓到通过 HTTP 代理访问 %s 的流量", target)
	}
}
