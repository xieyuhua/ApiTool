package main

import "apitool/internal/crypto"

// ToolResult 通用工具结果（前端统一解析）。保留在 main 包以便 Wails 绑定签名与前端兼容。
type ToolResult struct {
	Ok     bool   `json:"ok"`
	Output string `json:"output"`
	Error  string `json:"error"`
}

func toolOk(s string) ToolResult        { return ToolResult{Ok: true, Output: s} }
func toolErr(e string) ToolResult       { return ToolResult{Ok: false, Error: e} }

// ToolHash 计算文本摘要，algo: md5|sha1|sha256|sha512，返回十六进制
func (a *App) ToolHash(text, algo string) ToolResult {
	r := crypto.Hash(text, algo)
	return mapResult(r)
}

// ToolHmac 计算 HMAC，algo: md5|sha1|sha256|sha512，返回十六进制
func (a *App) ToolHmac(text, key, algo string) ToolResult {
	r := crypto.Hmac(text, key, algo)
	return mapResult(r)
}

// ToolCipher 对称加解密
// algo: aes|des|3des；mode: ecb|cbc；op: encrypt|decrypt
// outEnc: base64|hex（加密时表示输出编码，解密时表示输入编码）
func (a *App) ToolCipher(algo, mode, op, text, key, iv, outEnc string) ToolResult {
	r := crypto.Cipher(algo, mode, op, text, key, iv, outEnc)
	return mapResult(r)
}

// mapResult 将 crypto.Result 转换为前端依赖的 ToolResult 结构
func mapResult(r crypto.Result) ToolResult {
	return ToolResult{Ok: r.Ok, Output: r.Output, Error: r.Error}
}
