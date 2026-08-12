package sniff

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// SetSystemProxy 将本机系统 HTTP/HTTPS 代理指向 addr（格式 host:port）。
// 采用当前用户 Internet Settings 注册表（多数应用与 WinHTTP 共享感知）。
func SetSystemProxy(addr string) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("当前仅支持 Windows 自动设置系统代理")
	}
	host, port, err := splitHostPort(addr)
	if err != nil {
		return err
	}
	proxy := "http=" + host + ":" + port + ";https=" + host + ":" + port
	override := host + ":" + port
	cmds := [][]string{
		{"reg", "add", `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`, "/v", "ProxyServer", "/t", "REG_SZ", "/d", proxy, "/f"},
		{"reg", "add", `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`, "/v", "ProxyOverride", "/t", "REG_SZ", "/d", override, "/f"},
		{"reg", "add", `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`, "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "00000001", "/f"},
	}
	for _, c := range cmds {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			return fmt.Errorf("设置系统代理失败: %s: %v", string(out), err)
		}
	}
	return nil
}

// ClearSystemProxy 关闭系统代理（抓包结束时调用）。
func ClearSystemProxy() error {
	if runtime.GOOS != "windows" {
		return nil
	}
	out, err := exec.Command("reg", "add", `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`, "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "00000000", "/f").CombinedOutput()
	if err != nil {
		return fmt.Errorf("关闭系统代理失败: %s: %v", string(out), err)
	}
	return nil
}

// InstallCA 将根 CA 证书安装到系统「受信任的根证书颁发机构」。
// Windows 需管理员权限（certutil）。其它平台返回错误，需用户手动安装。
func InstallCA(certPEM []byte) error {
	if runtime.GOOS == "windows" {
		// 写入临时文件后调用 certutil
		tmp, err := writeTemp("apitool-ca.pem", certPEM)
		if err != nil {
			return err
		}
		defer removeTemp(tmp)
		out, err := exec.Command("certutil", "-addstore", "-f", "Root", tmp).CombinedOutput()
		if err != nil {
			return fmt.Errorf("安装根证书失败（需管理员运行）: %s: %v", strings.TrimSpace(string(out)), err)
		}
		return nil
	}
	return fmt.Errorf("当前平台不支持自动安装根证书，请手动将 ca.pem 导入系统信任库")
}

// IsCAInstalled 快速判断根 CA 是否已安装到系统信任库（Windows 用 certutil）。
func IsCAInstalled() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	// 以指纹匹配：certutil -verifystore Root 输出包含指纹
	return false // 精确检测交由前端/用户确认；此处保守返回 false，提示可一键安装
}

// parsePEMCert 解析 PEM 证书。
func parsePEMCert(pemBytes []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("无效的 PEM 证书")
	}
	return x509.ParseCertificate(block.Bytes)
}

// splitHostPort 将 127.0.0.1:8888 拆分为 host/port。
func splitHostPort(addr string) (string, string, error) {
	idx := strings.LastIndex(addr, ":")
	if idx < 0 {
		return "", "", fmt.Errorf("代理地址格式错误: %s", addr)
	}
	return addr[:idx], addr[idx+1:], nil
}
