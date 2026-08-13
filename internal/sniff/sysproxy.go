package sniff

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
)

// hiddenCmd 构造一个在 Windows 上不弹出控制台窗口的命令。
// 抓包启动/停止时会调用 reg/certutil，若不隐藏会闪现黑窗口。
func hiddenCmd(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	}
	return cmd
}

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
		if out, err := hiddenCmd(c[0], c[1:]...).CombinedOutput(); err != nil {
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
	out, err := hiddenCmd("reg", "add", `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`, "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "00000000", "/f").CombinedOutput()
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
		out, err := hiddenCmd("certutil", "-addstore", "-f", "Root", tmp).CombinedOutput()
		if err != nil {
			return fmt.Errorf("安装根证书失败（需管理员运行）: %s: %v", strings.TrimSpace(string(out)), err)
		}
		return nil
	}
	return fmt.Errorf("当前平台不支持自动安装根证书，请手动将 ca.pem 导入系统信任库")
}

// IsCAInstalled 判断指定指纹的根 CA 是否已安装到系统「受信任的根证书颁发机构」。
// fingerprint 需为冒号分隔的大写 SHA1（如 "AA:BB:...")；为空时返回 false。
func IsCAInstalled(fingerprint string) bool {
	if runtime.GOOS != "windows" || fingerprint == "" {
		return false
	}
	// 归一化指纹：去除冒号、空格，转小写，用于匹配 certutil 输出
	want := normalizeFingerprint(fingerprint)
	out, err := hiddenCmd("certutil", "-store", "Root").CombinedOutput()
	if err != nil {
		return false
	}
	return containsFingerprint(string(out), want)
}

// normalizeFingerprint 去除冒号与空白并转小写。
func normalizeFingerprint(fp string) string {
	s := strings.ToLower(fp)
	s = strings.ReplaceAll(s, ":", "")
	return strings.ReplaceAll(s, " ", "")
}

// containsFingerprint 在 certutil 输出中查找指纹（可能带空格/冒号/大小写差异）。
func containsFingerprint(output, want string) bool {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		norm := normalizeFingerprint(line)
		if norm != "" && strings.Contains(norm, want) {
			return true
		}
	}
	return false
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
