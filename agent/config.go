// Package agent 是可独立部署的 AI Agent 服务。
//
// 与桌面端、主服务端的关系：三者各自运行、互不依赖。业务核心位于
// core/agent（自 internal/agent 复制的去桌面化版本），本包只负责
// 单租户运行时（SQLite + SSE）与 HTTP API。
//
// 部署形态：单二进制（内嵌网页端 UI），一个实例一份数据。
package agent

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

// Config 独立 Agent 服务配置。
type Config struct {
	Addr            string        // 监听地址（:8090）
	DataDir         string        // 数据目录（SQLite / 导出文件）
	DBType          string        // sqlite | mysql
	DSN             string        // SQLite 文件或 MySQL 连接串
	Token           string        // 访问令牌；为空表示仅本机可访问
	TokenTTL        time.Duration // 令牌 Cookie 有效期
	AllowLocalNoTok bool          // 无令牌时是否允许（仅绑定回环地址）
}

// LoadConfig 从命令行 flag 与环境变量装配配置。
// 环境变量优先于默认值、低于命令行显式传参（flag 解析后覆盖）。
func LoadConfig(args []string) (*Config, error) {
	cfg := &Config{}
	fs := flag.NewFlagSet("agentd", flag.ContinueOnError)
	fs.StringVar(&cfg.Addr, "addr", envOr("AGENT_ADDR", ":8090"), "监听地址")
	fs.StringVar(&cfg.DataDir, "data", envOr("AGENT_DATA", "./agent-data"), "数据目录")
	fs.StringVar(&cfg.DBType, "db", envOr("AGENT_DB", "sqlite"), "数据库类型：sqlite|mysql")
	fs.StringVar(&cfg.DSN, "dsn", envOr("AGENT_DSN", ""), "连接串（空则使用 <data>/agent.db）")
	fs.StringVar(&cfg.Token, "token", envOr("AGENT_TOKEN", ""), "访问令牌（空则仅本机可访问）")
	fs.DurationVar(&cfg.TokenTTL, "token-ttl", 720*time.Hour, "令牌 Cookie 有效期")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	cfg.AllowLocalNoTok = true
	cfg.DBType = strings.ToLower(strings.TrimSpace(cfg.DBType))
	if cfg.DBType != "sqlite" && cfg.DBType != "mysql" {
		return nil, fmt.Errorf("不支持的 -db：%s（可选 sqlite / mysql）", cfg.DBType)
	}
	return cfg, nil
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// IsLoopback 判断监听地址是否只绑定本机（无令牌时仅允许这种绑定）。
func (c *Config) IsLoopback() bool {
	addr := strings.TrimSpace(c.Addr)
	host := addr
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		host = addr[:i]
	}
	host = strings.Trim(host, "[]")
	switch host {
	case "", "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}
