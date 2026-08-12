package sniff

import (
	"bytes"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lqqyt2423/go-mitmproxy/cert"
)

// caBundle 包装 go-mitmproxy 的自签根 CA，负责加载/生成并暴露给系统安装。
type caBundle struct {
	ca       cert.CA
	certFile string // 纯证书 PEM 文件路径（供系统信任库安装）
	rootCert *x509.Certificate
	dir      string // CA 文件目录（用于导入覆盖）
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
		dir:      dir,
	}
	return b, nil
}

// ImportCA 用用户提供的根证书（证书 + 私钥 PEM）替换当前 CA，并写入
// go-mitmproxy 兼容的文件（mitmproxy-ca.pem / mitmproxy-ca-cert.pem），
// 便于直接复用 Fiddler 等现有根证书。传入的私钥支持 PKCS1 / PKCS8。
func (c *caBundle) ImportCA(certPEM, keyPEM []byte) error {
	if c.dir == "" {
		return errInvalidCA
	}
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return errInvalidCA
	}
	rootCert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return err
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return errInvalidCA
	}
	var priv *rsa.PrivateKey
	if k, e := x509.ParsePKCS1PrivateKey(keyBlock.Bytes); e == nil {
		priv = k
	} else if pkcs8, e := x509.ParsePKCS8PrivateKey(keyBlock.Bytes); e == nil {
		if rk, ok := pkcs8.(*rsa.PrivateKey); ok {
			priv = rk
		} else {
			return fmt.Errorf("仅支持 RSA 私钥")
		}
	} else {
		return errInvalidCA
	}

	// 写成 go-mitmproxy 的加载格式：mitmproxy-ca.pem = PKCS8 私钥 + 证书；mitmproxy-ca-cert.pem = 证书
	pkcs8, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return err
	}
	pemBuf := &bytes.Buffer{}
	pem.Encode(pemBuf, &pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})
	pem.Encode(pemBuf, &pem.Block{Type: "CERTIFICATE", Bytes: rootCert.Raw})
	if err := os.WriteFile(filepath.Join(c.dir, "mitmproxy-ca.pem"), pemBuf.Bytes(), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(c.certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootCert.Raw}), 0o600); err != nil {
		return err
	}

	// 重新加载以生效
	nw, err := cert.NewSelfSignCA(c.dir)
	if err != nil {
		return err
	}
	c.ca = nw
	c.rootCert = nw.GetRootCA()
	return nil
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
