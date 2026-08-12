package sniff

import (
	"bufio"
	"bytes"
	"net/http"
	"strings"
)

// Protocol 流量协议类型。
type Protocol string

const (
	ProtoHTTP  Protocol = "HTTP"
	ProtoHTTPS Protocol = "HTTPS"
	ProtoTLS   Protocol = "TLS"
	ProtoSSH   Protocol = "SSH"
	ProtoFTP   Protocol = "FTP"
	ProtoSMTP  Protocol = "SMTP"
	ProtoOther Protocol = "OTHER"
	ProtoUnknown Protocol = "UNKNOWN"
)

// ProtoInfo 协议识别结果。
type ProtoInfo struct {
	Protocol Protocol
	// Decryptable 表示抓包层能否解密并解析明文（仅 HTTP/HTTPS 可）。
	Decryptable bool
	// Note 对无法解密协议的说明。
	Note string
}

// IdentifyProtocol 根据握手字节嗅探对端协议类型。
// 传入的是从客户端读取的前若干个字节（通常为前 1-8 字节）。
func IdentifyProtocol(head []byte) ProtoInfo {
	if len(head) == 0 {
		return ProtoInfo{Protocol: ProtoUnknown, Decryptable: false}
	}

	// SSH：以 "SSH-" 开头
	if bytes.HasPrefix(head, []byte("SSH-")) {
		return ProtoInfo{Protocol: ProtoSSH, Decryptable: false, Note: "SSH 为加密协议，无法解密内容，仅识别连接"}
	}

	// TLS：ContentType 0x16(Handshake) 且 Version 0x03xx
	if head[0] == 0x16 && len(head) >= 3 && head[1] == 0x03 && (head[2] == 0x01 || head[2] == 0x03 || head[2] == 0x00) {
		// HTTPS 是 TLS 上承载 HTTP；此处仅识别为 TLS/HTTPS，待后续按请求特征细化
		return ProtoInfo{Protocol: ProtoTLS, Decryptable: false, Note: "TLS 加密流量；若为目标 HTTPS 站点将在代理层解密"}
	}

	// SMTP：服务端与客户端握手常见 "220 " 或客户端 "HELO"/"EHLO"/"MAIL FROM"
	line, _ := firstLine(head)
	up := strings.ToUpper(line)
	switch {
	case strings.HasPrefix(up, "220 "), strings.HasPrefix(up, "220-"),
		strings.HasPrefix(up, "EHLO "), strings.HasPrefix(up, "HELO "),
		strings.HasPrefix(up, "MAIL FROM"), strings.HasPrefix(up, "RCPT TO"):
		return ProtoInfo{Protocol: ProtoSMTP, Decryptable: false, Note: "SMTP 明文邮件协议，已识别"}
	}

	// FTP：服务端 "220 " + "FTP" 或客户端 "USER "/"PASS "/"PORT "
	switch {
	case strings.Contains(up, "220") && strings.Contains(up, "FTP"),
		strings.HasPrefix(up, "USER "), strings.HasPrefix(up, "PASS "),
		strings.HasPrefix(up, "PORT "), strings.HasPrefix(up, "PASV"),
		strings.HasPrefix(up, "STOR "), strings.HasPrefix(up, "RETR "):
		return ProtoInfo{Protocol: ProtoFTP, Decryptable: false, Note: "FTP 文件传输协议，已识别"}
	}

	// HTTP：方法词开头
	if isHTTPMethod(line) {
		return ProtoInfo{Protocol: ProtoHTTP, Decryptable: true}
	}

	return ProtoInfo{Protocol: ProtoUnknown, Decryptable: false}
}

func firstLine(b []byte) (string, bool) {
	r := bufio.NewReader(bytes.NewReader(b))
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return string(b), false
	}
	return strings.TrimRight(line, "\r\n"), true
}

func isHTTPMethod(line string) bool {
	up := strings.ToUpper(strings.TrimSpace(line))
	for _, m := range []string{"GET ", "POST ", "PUT ", "DELETE ", "HEAD ", "OPTIONS ", "PATCH ", "CONNECT ", "TRACE "} {
		if strings.HasPrefix(up, m) {
			return true
		}
	}
	// 形如 "GET / HTTP/1.1" 无空格的兜底
	if idx := strings.Index(up, " /"); idx > 0 {
		verb := up[:idx]
		for _, m := range []string{"GET", "POST", "PUT", "DELETE", "HEAD", "OPTIONS", "PATCH", "CONNECT", "TRACE"} {
			if verb == m {
				return true
			}
		}
	}
	return false
}

// classifyRequest 根据请求特征判定协议标签（MITM 解密后的均为 HTTP/HTTPS）。
func classifyRequest(req *http.Request) string {
	if req == nil {
		return "UNKNOWN"
	}
	if req.TLS != nil {
		return "HTTPS"
	}
	return "HTTP"
}

// bodyTypeOf 依据 Content-Type 与内容推断 body 类型标签。
func bodyTypeOf(contentType string, body []byte) string {
	ct := strings.ToLower(contentType)
	switch {
	case ct == "" || len(body) == 0:
		return "none"
	case strings.Contains(ct, "image/"):
		return "image"
	case strings.Contains(ct, "json"):
		return "json"
	case strings.Contains(ct, "x-www-form-urlencoded"):
		return "form"
	case strings.Contains(ct, "multipart/form-data"):
		return "form"
	case strings.Contains(ct, "xml"):
		return "xml"
	}
	// 无明确类型时按内容启发式判断
	s := string(body)
	if len(s) > 0 && (s[0] == '{' || s[0] == '[') {
		return "json"
	}
	return "text"
}
