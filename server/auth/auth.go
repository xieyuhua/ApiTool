// Package auth 服务端鉴权：手写 HMAC-SHA256 JWT（stdlib，零新依赖）+ scrypt 密码哈希。
//
// 双通道令牌：
//   - Authorization: Bearer <accessToken>（RPC/API 请求）
//   - HttpOnly Cookie "apitool_token"（SSE EventSource 无法携带自定义 Header）
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/scrypt"
)

const (
	// AccessTTL accessToken 有效期。
	AccessTTL = 2 * time.Hour
	// RefreshTTL refreshToken 有效期（落库可吊销）。
	RefreshTTL = 30 * 24 * time.Hour
	// CookieName 会话 Cookie 名。
	CookieName = "apitool_token"
)

// Claims JWT 载荷。
type Claims struct {
	UserID   string `json:"uid"`
	Username string `json:"usr"`
	Exp      int64  `json:"exp"`
	IssuedAt int64  `json:"iat"`
}

// Manager JWT 签发与校验。
type Manager struct {
	secret []byte
}

// NewManager 创建（secret 为服务端配置的 JWT 密钥）。
func NewManager(secret string) *Manager {
	return &Manager{secret: []byte(secret)}
}

// sign HMAC-SHA256。
func (m *Manager) sign(data []byte) []byte {
	h := hmac.New(sha256.New, m.secret)
	h.Write(data)
	return h.Sum(nil)
}

// Issue 签发 accessToken。
func (m *Manager) Issue(userID, username string) (string, error) {
	now := time.Now()
	c := Claims{
		UserID:   userID,
		Username: username,
		Exp:      now.Add(AccessTTL).Unix(),
		IssuedAt: now.Unix(),
	}
	payload, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	sig := base64.RawURLEncoding.EncodeToString(m.sign([]byte(body)))
	return body + "." + sig, nil
}

// Verify 校验 accessToken，返回 Claims。
func (m *Manager) Verify(token string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, errors.New("令牌格式错误")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("令牌签名解析失败")
	}
	expect := m.sign([]byte(parts[0]))
	if !hmac.Equal(sig, expect) {
		return nil, errors.New("令牌签名不匹配")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("令牌载荷解析失败")
	}
	var c Claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil, errors.New("令牌载荷损坏")
	}
	if time.Now().Unix() > c.Exp {
		return nil, errors.New("令牌已过期")
	}
	return &c, nil
}

// ---- 密码 ----

// HashPassword 用 scrypt 生成密码哈希（salt 内嵌于结果）。
// 格式：scrypt$N$r$p$salt$hash（全部 hex）。
func HashPassword(password string) (string, string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", "", err
	}
	// N=32768, r=8, p=1（约 64MB 内存，服务端可接受）
	dk, err := scrypt.Key([]byte(password), salt, 32768, 8, 1, 32)
	if err != nil {
		return "", "", err
	}
	s := hex.EncodeToString(salt)
	h := hex.EncodeToString(dk)
	return s, fmt.Sprintf("scrypt$32768$8$1$%s$%s", s, h), nil
}

// VerifyPassword 校验密码（constant-time 比较）。
// salt 参数为 hex 字符串，内部还原为原始字节（与 HashPassword 一致）。
func VerifyPassword(password, salt, stored string) bool {
	raw, err := hex.DecodeString(salt)
	if err != nil {
		return false
	}
	dk, err := scrypt.Key([]byte(password), raw, 32768, 8, 1, 32)
	if err != nil {
		return false
	}
	// 存储格式 scrypt$N$r$p$salt$hash
	parts := strings.Split(stored, "$")
	if len(parts) != 6 {
		return false
	}
	got := hex.EncodeToString(dk)
	return subtle.ConstantTimeCompare([]byte(got), []byte(parts[5])) == 1
}

// RandomToken 生成 URL 安全的随机令牌（refresh token / 分享 token 用）。
func RandomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
