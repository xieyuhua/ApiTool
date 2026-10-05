// Package util 提供跨模块共享的通用工具函数（如 ID 生成、本机 IP 解析）。
package util

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"strings"

	"github.com/google/uuid"
)

// GenID 生成全局唯一 ID（UUID v4 字符串）
func GenID() string {
	return uuid.NewString()
}

// Token 生成 32 字符的随机十六进制令牌（128bit 熵），用于对外服务鉴权，
// 例如 capture 捕获服务的访问 token。统一收口避免各模块重复实现 crypto/rand。
func Token() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// 极端情况下回退 UUID，保证永远能产出可用令牌
		return strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	return hex.EncodeToString(buf)
}

// LocalIP 返回本机用于「局域网访问」的 IPv4 地址（分享 / 同步 / 局域网聊天地址生成共用）。
//
// 选择策略（分数由高到低）：
//  1. 私有网段 192.168.x.x —— 家用 / 办公 Wi-Fi 最常见，优先；
//  2. 私有网段 10.x.x.x、172.16-31.x.x；
//  3. 其它可路由地址；
//  4. 169.254.x.x（APIPA 自动地址，通常来自虚拟网卡，仅作兜底）。
//
// 同时跳过未启用、以及名字像虚拟网卡的接口（Hyper-V / VMware / VirtualBox / WSL / Docker 等），
// 避免拿到一个手机根本连不上的地址。全部不可用时回退 127.0.0.1。
func LocalIP() string {
	if ip := bestLANIPv4(true); ip != "" {
		return ip
	}
	if ip := bestLANIPv4(false); ip != "" {
		return ip
	}
	return "127.0.0.1"
}

// bestLANIPv4 遍历网卡选出得分最高的 IPv4 地址；skipVirtual 为 true 时跳过虚拟网卡。
func bestLANIPv4(skipVirtual bool) string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	best, bestScore := "", -1
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 {
			continue
		}
		if skipVirtual && isVirtualIfName(ifi.Name) {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			v4 := ipnet.IP.To4()
			if v4 == nil || v4.IsLoopback() {
				continue
			}
			if s := scoreIPv4(v4); s > bestScore {
				best, bestScore = v4.String(), s
			}
		}
	}
	return best
}

// scoreIPv4 给局域网可用性打分：私有网段优先，APIPA 自动地址垫底。
func scoreIPv4(v4 net.IP) int {
	switch {
	case v4[0] == 192 && v4[1] == 168:
		return 30
	case v4[0] == 10:
		return 20
	case v4[0] == 172 && v4[1] >= 16 && v4[1] <= 31:
		return 15
	case v4[0] == 169 && v4[1] == 254:
		return 5
	default:
		return 10
	}
}

// isVirtualIfName 判断网卡名是否属于常见虚拟网卡。
func isVirtualIfName(name string) bool {
	n := strings.ToLower(name)
	for _, k := range []string{"loopback", "virtual", "vethernet", "vmware", "virtualbox", "vbox", "docker", "veth", "wsl", "tap", "tun", "teredo", "isatap"} {
		if strings.Contains(n, k) {
			return true
		}
	}
	return false
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
