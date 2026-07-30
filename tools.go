package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/des"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash"
	"strings"
)

// ToolResult 通用工具结果（前端统一解析）
type ToolResult struct {
	Ok     bool   `json:"ok"`
	Output string `json:"output"`
	Error  string `json:"error"`
}

func toolOk(s string) ToolResult { return ToolResult{Ok: true, Output: s} }
func toolErr(e string) ToolResult { return ToolResult{Ok: false, Error: e} }

// ToolHash 计算文本摘要，algo: md5|sha1|sha256|sha512，返回十六进制
func (a *App) ToolHash(text, algo string) ToolResult {
	var h hash.Hash
	switch strings.ToLower(algo) {
	case "md5":
		h = md5.New()
	case "sha1":
		h = sha1.New()
	case "sha256":
		h = sha256.New()
	case "sha512":
		h = sha512.New()
	default:
		return toolErr("不支持的哈希算法：" + algo)
	}
	h.Write([]byte(text))
	return toolOk(hex.EncodeToString(h.Sum(nil)))
}

// ToolHmac 计算 HMAC，algo: md5|sha1|sha256|sha512，返回十六进制
func (a *App) ToolHmac(text, key, algo string) ToolResult {
	var mac func() hash.Hash
	switch strings.ToLower(algo) {
	case "md5":
		mac = func() hash.Hash { return hmac.New(md5.New, []byte(key)) }
	case "sha1":
		mac = func() hash.Hash { return hmac.New(sha1.New, []byte(key)) }
	case "sha256":
		mac = func() hash.Hash { return hmac.New(sha256.New, []byte(key)) }
	case "sha512":
		mac = func() hash.Hash { return hmac.New(sha512.New, []byte(key)) }
	default:
		return toolErr("不支持的 HMAC 算法：" + algo)
	}
	hm := mac()
	hm.Write([]byte(text))
	return toolOk(hex.EncodeToString(hm.Sum(nil)))
}

// ToolCipher 对称加解密
// algo: aes|des|3des；mode: ecb|cbc；op: encrypt|decrypt
// outEnc: base64|hex（加密时表示输出编码，解密时表示输入编码）
func (a *App) ToolCipher(algo, mode, op, text, key, iv, outEnc string) ToolResult {
	var newBlock func([]byte) (cipher.Block, error)
	switch strings.ToLower(algo) {
	case "aes":
		newBlock = aes.NewCipher
	case "des":
		newBlock = des.NewCipher
	case "3des":
		newBlock = des.NewTripleDESCipher
	default:
		return toolErr("不支持的对称算法：" + algo)
	}
	block, err := newBlock([]byte(key))
	if err != nil {
		return toolErr("密钥错误（长度不合法）：" + err.Error())
	}
	bs := block.BlockSize()
	mode = strings.ToLower(mode)
	op = strings.ToLower(op)
	outEnc = strings.ToLower(outEnc)
	if outEnc == "" {
		outEnc = "base64"
	}

	if op == "encrypt" {
		data := pkcs7Pad([]byte(text), bs)
		var dst []byte
		if mode == "ecb" {
			dst = ecbCrypt(block, data, true)
		} else {
			if len(iv) != bs {
				return toolErr(fmt.Sprintf("CBC 模式 IV 长度必须为 %d 字节（当前 %d）", bs, len(iv)))
			}
			dst = make([]byte, len(data))
			cipher.NewCBCEncrypter(block, []byte(iv)).CryptBlocks(dst, data)
		}
		return toolOk(encodeOut(dst, outEnc))
	}

	// decrypt
	raw, err := decodeIn(strings.TrimSpace(text), outEnc)
	if err != nil {
		return toolErr("密文解码失败：" + err.Error())
	}
	if len(raw) == 0 {
		return toolErr("密文为空")
	}
	if len(raw)%bs != 0 {
		return toolErr(fmt.Sprintf("密文长度必须是分组大小 %d 的整数倍", bs))
	}
	var plain []byte
	if mode == "ecb" {
		plain = ecbCrypt(block, raw, false)
	} else {
		if len(iv) != bs {
			return toolErr(fmt.Sprintf("CBC 模式 IV 长度必须为 %d 字节（当前 %d）", bs, len(iv)))
		}
		plain = make([]byte, len(raw))
		cipher.NewCBCDecrypter(block, []byte(iv)).CryptBlocks(plain, raw)
	}
	unpadded, err := pkcs7Unpad(plain, bs)
	if err != nil {
		return toolErr("去填充失败：" + err.Error())
	}
	return toolOk(string(unpadded))
}

// ecbCrypt 手动实现 ECB 模式（标准库未提供）
func ecbCrypt(block cipher.Block, src []byte, encrypt bool) []byte {
	bs := block.BlockSize()
	out := make([]byte, len(src))
	for i := 0; i < len(src); i += bs {
		if encrypt {
			block.Encrypt(out[i:i+bs], src[i:i+bs])
		} else {
			block.Decrypt(out[i:i+bs], src[i:i+bs])
		}
	}
	return out
}

// pkcs7Pad PKCS7 填充
func pkcs7Pad(src []byte, blockSize int) []byte {
	padding := blockSize - len(src)%blockSize
	padtext := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(src, padtext...)
}

// pkcs7Unpad PKCS7 去填充
func pkcs7Unpad(src []byte, blockSize int) ([]byte, error) {
	if len(src) == 0 {
		return nil, fmt.Errorf("空数据")
	}
	padding := int(src[len(src)-1])
	if padding < 1 || padding > blockSize || padding > len(src) {
		return nil, fmt.Errorf("填充无效")
	}
	return src[:len(src)-padding], nil
}

func encodeOut(b []byte, enc string) string {
	if enc == "hex" {
		return hex.EncodeToString(b)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func decodeIn(s, enc string) ([]byte, error) {
	if enc == "hex" {
		return hex.DecodeString(s)
	}
	return base64.StdEncoding.DecodeString(s)
}
