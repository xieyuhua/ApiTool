package sniff

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// errInvalidCA 表示 CA 证书/私钥文件损坏或无法解析。
var errInvalidCA = errors.New("invalid CA certificate or key")

// netParseIP 解析 IP（支持 IPv4/IPv6），失败返回 nil。
func netParseIP(s string) net.IP {
	return net.ParseIP(s)
}

// netIP 仅为类型别名，方便在 Sign 中统一书写。
type netIP = net.IP

// sha1Sum 计算 SHA1。
func sha1Sum(b []byte) []byte {
	s := sha1.Sum(b)
	return s[:]
}

// formatHex 将字节切片格式化为大写、冒号分隔的十六进制串（指纹展示）。
func formatHex(b []byte) string {
	parts := make([]string, len(b))
	for i, x := range b {
		parts[i] = hex.EncodeToString([]byte{x})
	}
	return strings.ToUpper(strings.Join(parts, ":"))
}

// randHex 返回 n 字节的十六进制随机串（用于生成 ID）。
func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte(time.Now().String()))
	}
	return hex.EncodeToString(b)
}

// writeTemp 写临时文件，返回路径。
func writeTemp(name string, data []byte) (string, error) {
	f, err := os.CreateTemp("", name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return "", err
	}
	return f.Name(), nil
}

// removeTemp 删除临时文件（忽略错误）。
func removeTemp(path string) {
	_ = os.Remove(path)
}

// atoiSafe 将字符串安全转换为整数，失败返回 0。
func atoiSafe(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

// containsStr 判断 sub 是否包含在 s 中（不区分大小写）。
func containsStr(s, sub string) bool {
	if sub == "" {
		return true
	}
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
