package sniff

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"

	"github.com/lqqyt2423/go-mitmproxy/cert"
)

// caBundle 包装 go-mitmproxy 的自签根 CA，负责加载/生成并暴露给系统安装。
type caBundle struct {
	ca       cert.CA
	certFile string // 纯证书 PEM 文件路径（供系统信任库安装）
	rootCert *x509.Certificate
}

// loadOrCreateCA 优先从 dir 加载 go-mitmproxy 已生成的根 CA；
// 若不存在则用 cert.NewSelfSignCA 生成并落盘（mitmproxy-ca.pem 等）。
func loadOrCreateCA(dir string) (*caBundle, error) {
	c, err := cert.NewSelfSignCA(dir)
	if err != nil {
		return nil, err
	}
	b := &caBundle{
		ca:       c,
		certFile: filepath.Join(dir, "mitmproxy-ca-cert.pem"),
		rootCert: c.GetRootCA(),
	}
	return b, nil
}

// CAPath 返回根 CA 证书文件路径（供用户手动安装/查看）。
func (c *caBundle) CAPath() string {
	return c.certFile
}

// CertPEM 返回根 CA 证书 PEM 文本。
func (c *caBundle) CertPEM() []byte {
	if data, err := os.ReadFile(c.certFile); err == nil && len(data) > 0 {
		return data
	}
	if c.rootCert != nil {
		return pemEncodeCert(c.rootCert)
	}
	return nil
}

// FingerprintSHA1 返回根 CA 的 SHA1 指纹（冒号分隔，大写）。
func (c *caBundle) FingerprintSHA1() string {
	if c.rootCert == nil {
		return ""
	}
	return formatHex(sha1Sum(c.rootCert.Raw))
}

// newCA 返回 go-mitmproxy 的 cert.CA（供 proxy 使用）。
func (c *caBundle) newCA() cert.CA {
	return c.ca
}

func pemEncodeCert(cert *x509.Certificate) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
}
