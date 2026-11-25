// Package app 组装 ApiTool 服务端：鉴权、多用户会话、RPC 反射、SSE 分频道、
// 项目 REST API 与内嵌网页端静态资源。
//
// 部署形态：单二进制（go build ./server/app/cmd），SQLite（个人）或 MySQL（团队）。
package app

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config 服务端配置。
type Config struct {
	Addr          string // 监听地址（:8080）
	DataDir       string // 数据根目录（SQLite 文件/导出文件）
	DBType        string // sqlite | mysql
	DSN           string // SQLite 文件路径或 MySQL 连接串
	JWTSecret     string // JWT 密钥（空则首次生成并落盘）
	AllowRegister bool   // 是否开放注册
	EnablePlugins bool   // 是否允许网页端调用 Plugin*（DB/SSH/SFTP 等）
	EnableAgent   bool   // 是否允许网页端运行 AI Agent
	SessionTTL    time.Duration
	// 安全相关（P5）
	RateLimitRPS   float64 // 每 IP 每秒请求数（0=关闭限流）
	RateLimitBurst float64 // 突发上限
	LoginMaxFails  int           // 连续登录失败上限（0=不锁定）
	LoginLockFor   time.Duration // 锁定窗口
	MaxBodyMB      int64         // 请求体上限（MB，0=用默认 32MB）
	// Agent 每用户每日调用配额（0=不限）
	AgentDailyQuota int
}

// LoadConfig 从命令行 flag 装配配置。
func LoadConfig() (*Config, error) {
	cfg := &Config{}
	flag.StringVar(&cfg.Addr, "addr", ":8080", "监听地址")
	flag.StringVar(&cfg.DataDir, "data", "apitool-server-data", "数据目录")
	flag.StringVar(&cfg.DBType, "db", "sqlite", "数据库类型：sqlite | mysql")
	flag.StringVar(&cfg.DSN, "dsn", "", "数据库连接串（sqlite 为文件路径；mysql 为 DSN）；空则用 DataDir/server.db")
	flag.StringVar(&cfg.JWTSecret, "jwt-secret", "", "JWT 密钥；空则首次生成并落盘到数据目录")
	flag.BoolVar(&cfg.AllowRegister, "allow-register", true, "是否开放注册")
	flag.BoolVar(&cfg.EnablePlugins, "enable-plugins", false, "是否允许网页端调用插件（DB/SSH/SFTP，注意 SSRF 风险）")
	flag.BoolVar(&cfg.EnableAgent, "enable-agent", true, "是否允许网页端运行 AI Agent")
	ttl := flag.Duration("session-ttl", 30*time.Minute, "用户会话空闲淘汰时间")
	flag.Float64Var(&cfg.RateLimitRPS, "rate-limit", 20, "每 IP 每秒请求数（0=关闭）")
	flag.Float64Var(&cfg.RateLimitBurst, "rate-burst", 40, "每 IP 突发请求上限")
	flag.IntVar(&cfg.LoginMaxFails, "login-max-fails", 5, "连续登录失败次数上限（0=不锁定）")
	flag.DurationVar(&cfg.LoginLockFor, "login-lock-for", 10*time.Minute, "登录失败锁定窗口")
	flag.Int64Var(&cfg.MaxBodyMB, "max-body-mb", 32, "请求体大小上限（MB）")
	flag.IntVar(&cfg.AgentDailyQuota, "agent-daily-quota", 200, "每用户每日 Agent 调用配额（0=不限）")
	flag.Parse()
	cfg.SessionTTL = *ttl

	if cfg.DSN == "" {
		_ = os.MkdirAll(cfg.DataDir, 0o755)
		cfg.DSN = filepath.Join(cfg.DataDir, "server.db")
	}
	if cfg.DBType == "sqlite" && !strings.Contains(cfg.DSN, ":") {
		_ = os.MkdirAll(filepath.Dir(cfg.DSN), 0o755)
		cfg.DSN = "file:" + cfg.DSN
	}
	secret, err := loadOrCreateSecret(cfg.DataDir, cfg.JWTSecret)
	if err != nil {
		return nil, err
	}
	cfg.JWTSecret = secret
	return cfg, nil
}

// loadOrCreateSecret 读取或首次生成 JWT 密钥（落盘 jwt_secret.txt）。
func loadOrCreateSecret(dataDir, explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	path := filepath.Join(dataDir, "jwt_secret.txt")
	if b, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(b))) >= 32 {
		return strings.TrimSpace(string(b)), nil
	}
	secret := randomSecret()
	if err := os.WriteFile(path, []byte(secret), 0o600); err != nil {
		return "", fmt.Errorf("写入 jwt secret 失败: %w", err)
	}
	return secret, nil
}

func randomSecret() string {
	// 复用 auth.RandomToken（64 字节 → ~86 字符 base64）
	return randomToken(64)
}

var errConfig = errors.New("配置错误")
