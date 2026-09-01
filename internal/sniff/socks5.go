package sniff

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 本文件实现「混合端口代理」：同一个监听端口同时接受
//   - HTTP/HTTPS 代理（明文 absolute-form 请求 + CONNECT 隧道）
//   - SOCKS5 代理（RFC 1928，CONNECT 命令）
//
// 区分方式：Accept 后 peek 首字节。SOCKS5 的首字节固定为版本号 0x05，
// 而 HTTP 请求行以 ASCII 方法名开头（'G' 'P' 'C' ...），两者不可能混淆。
//
// 关键设计：SOCKS5 流量并不自己实现 MITM，而是转换后交给内部 go-mitmproxy 处理，
// 从而完整复用既有的解密、记录、过滤、请求改写能力：
//   - 隧道内是 TLS（HTTPS）     → 转成 HTTP CONNECT 交给 go-mitmproxy 解密抓包
//   - 隧道内是明文 HTTP 请求    → 改写成 absolute-form 交给 go-mitmproxy 记录
//   - 其他 TCP 协议             → 转成 HTTP CONNECT 纯透传（保证连通性）

const (
	socksVer5 = 0x05 // SOCKS 协议版本

	// SOCKS5 响应码（REP 字段）
	socksRepSucceeded        = 0x00
	socksRepGeneralFailure   = 0x01
	socksRepCmdNotSupported  = 0x07
	socksRepAddrNotSupported = 0x08

	socksCmdConnect = 0x01 // 仅支持 CONNECT 命令
	socksMethodNone = 0x00 // 无认证
)

// sniffPeekSize 嗅探时读取的字节数：足够区分 TLS ClientHello(3B) 与最长的
// HTTP 方法名（"OPTIONS " 8B）。
const sniffPeekSize = 8

// sniffReadTimeout 等待客户端首个数据包的超时。SOCKS5 客户端在收到 CONNECT
// 响应后才会发送真实数据，这里给一个宽松上限，避免异常连接长期占用。
const sniffReadTimeout = 10 * time.Second

// handshakeTimeout SOCKS5 握手阶段的读写超时。
const handshakeTimeout = 30 * time.Second

// hybridListener 混合监听器。对外监听 addr，把 HTTP 流量与转换后的 SOCKS5
// 流量统一转发到 upstream（内部 go-mitmproxy 监听地址）。
type hybridListener struct {
	net.Listener
	upstream string // 内部 HTTP 代理地址 host:port
	wg       sync.WaitGroup
}

// startHybridListener 在 addr 上启动混合代理监听。
// 返回的监听器已接管 Accept 循环；调用 Close 即可停止。
func startHybridListener(addr, upstream string) (*hybridListener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	l := &hybridListener{Listener: ln, upstream: upstream}
	l.wg.Add(1)
	go l.serve()
	return l, nil
}

// Addr 返回对外真实监听地址（端口填 0 时为系统分配的实际端口）。
func (l *hybridListener) Addr() net.Addr { return l.Listener.Addr() }

func (l *hybridListener) serve() {
	defer l.wg.Done()
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return // 监听关闭（Stop 时触发）
		}
		l.wg.Add(1)
		go func() {
			defer l.wg.Done()
			l.handleConn(c)
		}()
	}
}

// Close 关闭监听器并等待所有处理中的连接退出。
func (l *hybridListener) Close() error {
	err := l.Listener.Close()
	l.wg.Wait()
	return err
}

// handleConn 依据首字节分发到 SOCKS5 或 HTTP 处理。
func (l *hybridListener) handleConn(c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(handshakeTimeout))

	br := bufio.NewReader(c)
	first, err := br.Peek(1)
	if err != nil {
		return
	}
	if first[0] == socksVer5 {
		l.handleSocks5(c, br)
		return
	}
	// 原生 HTTP 代理：直接转发到内部代理（缓冲数据会一并补发）
	_ = c.SetDeadline(time.Time{})
	l.pipeToUpstream(c, br, "")
}

// ---- SOCKS5 协议处理 ----

// handleSocks5 完成 SOCKS5 握手，然后按隧道内流量类型选择转发方式。
func (l *hybridListener) handleSocks5(c net.Conn, br *bufio.Reader) {
	target, err := l.socks5Handshake(c, br)
	if err != nil {
		return
	}
	// 握手完成，隧道可能是长连接，取消握手超时
	_ = c.SetDeadline(time.Now().Add(sniffReadTimeout))

	// 嗅探隧道内首个数据包以决定处理方式。
	// Peek 在 EOF 时会返回已读到的部分数据，属于正常情况，需兼容。
	head, err := br.Peek(sniffPeekSize)
	_ = c.SetDeadline(time.Time{})
	if err != nil && err != io.EOF {
		return // 客户端迟迟不发数据，放弃该连接
	}

	switch {
	case isTLSClientHello(head):
		// HTTPS：转 HTTP CONNECT，由 go-mitmproxy 做 MITM 解密并记录
		l.connectTunnel(c, br, target)
	case isHTTPRequest(head):
		// 明文 HTTP：改写成 absolute-form，让 go-mitmproxy 正常解析记录
		l.relayHTTP(c, br, target)
	default:
		// 其他 TCP 协议：透传，保证连通（无法解密，与不支持的协议处理一致）
		l.connectTunnel(c, br, target)
	}
}

// socks5Handshake 执行 SOCKS5 协商与 CONNECT 请求解析，返回目标地址 host:port。
func (l *hybridListener) socks5Handshake(c net.Conn, br *bufio.Reader) (string, error) {
	// 协商：VER(1) NMETHODS(1) METHODS(N)
	ver, err := br.ReadByte()
	if err != nil || ver != socksVer5 {
		return "", errors.New("非 SOCKS5 协议")
	}
	nMethods, err := br.ReadByte()
	if err != nil {
		return "", errors.New("读取方法数失败")
	}
	if nMethods > 0 {
		methods := make([]byte, int(nMethods))
		if _, err := io.ReadFull(br, methods); err != nil {
			return "", errors.New("读取方法列表失败")
		}
		// 仅支持无认证；不支持用户名/密码认证与 GSSAPI
		ok := false
		for _, m := range methods {
			if m == socksMethodNone {
				ok = true
				break
			}
		}
		if !ok {
			_ = writeSocks5Method(c, 0xFF) // 无可接受的方法
			return "", errors.New("不支持的 SOCKS5 认证方式")
		}
	}
	if err := writeSocks5Method(c, socksMethodNone); err != nil {
		return "", err
	}

	// 请求：VER(1) CMD(1) RSV(1) ATYP(1) DST.ADDR(N) DST.PORT(2)
	// 此处只读取前 3 字节（VER/CMD/RSV），ATYP 交由 readSocks5Addr 读取，
	// 避免重复消费导致地址解析错位。
	hdr := make([]byte, 3)
	if _, err := io.ReadFull(br, hdr); err != nil {
		return "", errors.New("读取请求头失败")
	}
	if hdr[0] != socksVer5 {
		return "", errors.New("SOCKS 版本错误")
	}
	// 仅支持 CONNECT（BIND / UDP ASSOCIATE 不提供）
	if hdr[1] != socksCmdConnect {
		_ = writeSocks5Reply(c, socksRepCmdNotSupported)
		return "", fmt.Errorf("不支持的 SOCKS5 命令: 0x%02x", hdr[1])
	}
	host, port, err := readSocks5Addr(br)
	if err != nil {
		_ = writeSocks5Reply(c, socksRepAddrNotSupported)
		return "", err
	}
	if err := writeSocks5Reply(c, socksRepSucceeded); err != nil {
		return "", err
	}
	return net.JoinHostPort(host, port), nil
}

func writeSocks5Method(w io.Writer, method byte) error {
	_, err := w.Write([]byte{socksVer5, method})
	return err
}

// writeSocks5Reply 回复 CONNECT 结果。绑定地址统一填 0.0.0.0:0（客户端通常忽略）。
func writeSocks5Reply(w io.Writer, rep byte) error {
	_, err := w.Write([]byte{socksVer5, rep, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	return err
}

// readSocks5Addr 解析 ATYP + DST.ADDR + DST.PORT。
func readSocks5Addr(br *bufio.Reader) (host, port string, err error) {
	atyp, err := br.ReadByte()
	if err != nil {
		return "", "", err
	}
	var addr string
	switch atyp {
	case 0x01: // IPv4
		b := make([]byte, net.IPv4len)
		if _, err = io.ReadFull(br, b); err != nil {
			return "", "", err
		}
		addr = net.IP(b).String()
	case 0x03: // 域名
		n, err := br.ReadByte()
		if err != nil {
			return "", "", err
		}
		if n == 0 {
			return "", "", errors.New("空域名")
		}
		b := make([]byte, int(n))
		if _, err = io.ReadFull(br, b); err != nil {
			return "", "", err
		}
		addr = string(b)
	case 0x04: // IPv6
		b := make([]byte, net.IPv6len)
		if _, err = io.ReadFull(br, b); err != nil {
			return "", "", err
		}
		addr = net.IP(b).String()
	default:
		return "", "", fmt.Errorf("不支持的地址类型: 0x%02x", atyp)
	}
	pb := make([]byte, 2)
	if _, err = io.ReadFull(br, pb); err != nil {
		return "", "", err
	}
	return addr, strconv.Itoa(int(binary.BigEndian.Uint16(pb))), nil
}

// ---- 流量嗅探判定 ----

// isTLSClientHello 判断是否为 TLS 握手首包：ContentType=0x16(Handshake) 且版本 0x03xx。
func isTLSClientHello(b []byte) bool {
	return len(b) >= 3 && b[0] == 0x16 && b[1] == 0x03 && b[2] <= 0x04
}

// isHTTPRequest 判断是否为 HTTP 请求行（以标准方法名开头）。
func isHTTPRequest(b []byte) bool {
	for _, m := range []string{"GET ", "POST ", "PUT ", "DELETE ", "HEAD ", "OPTIONS ", "PATCH ", "TRACE ", "CONNECT "} {
		if bytes.HasPrefix(b, []byte(m)) {
			return true
		}
	}
	return false
}

// ---- 转发实现 ----

// connectTunnel 以 HTTP CONNECT 的方式把连接接入内部代理。
// go-mitmproxy 收到 CONNECT 后会按 shouldIntercept 决定解密（HTTPS）或透传。
func (l *hybridListener) connectTunnel(c net.Conn, br *bufio.Reader, target string) {
	up, err := net.Dial("tcp", l.upstream)
	if err != nil {
		return
	}
	defer up.Close()

	// 构造 CONNECT 请求（目标地址来自 SOCKS5，协议转换的关键一步）
	req := "CONNECT " + target + " HTTP/1.1\r\nHost: " + target + "\r\n\r\n"
	if _, err := io.WriteString(up, req); err != nil {
		return
	}
	// 读到响应头结束，即隧道已建立（后续为裸 TCP）
	ubr := bufio.NewReader(up)
	if _, err := ubr.ReadString('\n'); err != nil {
		return
	}
	for {
		line, err := ubr.ReadString('\n')
		if err != nil {
			return
		}
		if line == "\r\n" || line == "\n" {
			break
		}
	}
	// 隧道建立后纯转发（自动补发 br 中已缓冲的数据）
	l.pipe(c, br, up)
}

// relayHTTP 把 SOCKS5 隧道内的明文 HTTP 请求改写成 absolute-form 后交给内部代理。
// 这样 go-mitmproxy 能像处理普通 HTTP 代理请求一样解析、记录并转发。
func (l *hybridListener) relayHTTP(c net.Conn, br *bufio.Reader, target string) {
	for {
		req, err := http.ReadRequest(br)
		if err != nil {
			return // 连接关闭或协议异常
		}
		if !relayOneHTTP(c, req, target, l.upstream) {
			return
		}
	}
}

// relayOneHTTP 转发单个请求，返回是否应继续复用该连接。
func relayOneHTTP(c net.Conn, req *http.Request, target, upstream string) bool {
	up, err := net.Dial("tcp", upstream)
	if err != nil {
		return false
	}
	defer up.Close()

	// 补全目标：SOCKS5 客户端发的是 relative-form（GET /path），
	// 需改写成代理要求的 absolute-form（GET http://host/path）。
	if !req.URL.IsAbs() {
		req.URL.Scheme = "http"
		req.URL.Host = target
	}
	if req.Host == "" {
		req.Host = target
	}
	// WriteProxy 会输出 absolute-form 的请求行
	if err := req.WriteProxy(up); err != nil {
		return false
	}
	resp, err := http.ReadResponse(bufio.NewReader(up), req)
	if err != nil {
		return false
	}
	closeConn := resp.Close || req.Close
	if err := resp.Write(c); err != nil {
		return false
	}
	_ = resp.Body.Close()
	return !closeConn
}

// pipeToUpstream 建立到内部代理的连接并双向转发；prefix 为需先发送的数据。
func (l *hybridListener) pipeToUpstream(c net.Conn, br *bufio.Reader, prefix string) {
	up, err := net.Dial("tcp", l.upstream)
	if err != nil {
		return
	}
	defer up.Close()
	if prefix != "" {
		if _, err := io.WriteString(up, prefix); err != nil {
			return
		}
	}
	l.pipe(c, br, up)
}

// pipe 双向转发。src 中已缓冲的数据先补发给 dst，之后直接透传底层连接。
func (l *hybridListener) pipe(src net.Conn, br *bufio.Reader, dst net.Conn) {
	if br != nil {
		if n := br.Buffered(); n > 0 {
			buf, err := br.Peek(n)
			if err != nil {
				return
			}
			if _, err := dst.Write(buf); err != nil {
				return
			}
			br.Reset(src) // 缓冲已消费，后续直接读底层连接
		}
	}
	transferConns(src, dst)
}

// transferConns 双向拷贝，任一端结束即半关闭对端，避免连接悬挂。
func transferConns(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(b, a)
		closeWrite(b)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(a, b)
		closeWrite(a)
	}()
	wg.Wait()
}

// closeWrite 半关闭 TCP 写方向（非 TCP 连接忽略）。
func closeWrite(c net.Conn) {
	if tc, ok := c.(*net.TCPConn); ok {
		_ = tc.CloseWrite()
	}
}

// pickFreePort 获取一个当前空闲的本地端口，用于内部 go-mitmproxy 监听。
// 存在极小的竞态（获取后被占用），故调用方应重试若干次。
func pickFreePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port, nil
}

// joinHostPortSafely 拼接 host:port，host 已含端口时不重复添加。
func joinHostPortSafely(host, port string) string {
	if host == "" || port == "" {
		return host
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return host
	}
	return net.JoinHostPort(host, port)
}
