// Package util 提供跨模块共享的通用工具函数（如 ID 生成、本机 IP 解析）。
package util

import (
	"net"

	"github.com/google/uuid"
)

// GenID 生成全局唯一 ID（UUID v4 字符串）
func GenID() string {
	return uuid.NewString()
}

// LocalIP 返回本机第一个非回环 IPv4 地址（用于生成局域网可访问的分享/同步地址）。
// 无可用地址时回退 127.0.0.1。
func LocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		if v4 := ipnet.IP.To4(); v4 != nil {
			return v4.String()
		}
	}
	return "127.0.0.1"
}

// FirstNonEmpty 返回参数列表中第一个非空字符串，全空时返回空串。
// capture / agent 等模块共用，避免重复定义。
func FirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// Truncate 将字符串按字符数截断到最多 n 个 rune，超出部分以 "..." 结尾。
// 用于日志/输出裁剪，避免超长内容撑爆前端或日志。
func Truncate(s string, n int) string {
	if n <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
