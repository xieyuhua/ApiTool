// Package tool 提供前端调用的通用工具（Hash / HMAC / 对称加解密）。
// 实现与 Wails 绑定根（main.App）分离：App 通过嵌入 Service 把方法提升到绑定层，
// 便于独立测试与复用。结果统一使用 crypto.Result（与前端约定字段 ok/output/error）。
package tool

import "apitool/internal/crypto"

// Service 通用工具服务，由 main.App 嵌入以暴露给 Wails 前端。
type Service struct{}

// ToolHash 计算文本摘要，algo: md5|sha1|sha256|sha512，返回十六进制
func (s *Service) ToolHash(text, algo string) crypto.Result {
	return crypto.Hash(text, algo)
}

// ToolHmac 计算 HMAC，algo: md5|sha1|sha256|sha512，返回十六进制
func (s *Service) ToolHmac(text, key, algo string) crypto.Result {
	return crypto.Hmac(text, key, algo)
}

// ToolCipher 对称加解密
// algo: aes|des|3des；mode: ecb|cbc；op: encrypt|decrypt
// outEnc: base64|hex（加密时表示输出编码，解密时表示输入编码）
func (s *Service) ToolCipher(algo, mode, op, text, key, iv, outEnc string) crypto.Result {
	return crypto.Cipher(algo, mode, op, text, key, iv, outEnc)
}
