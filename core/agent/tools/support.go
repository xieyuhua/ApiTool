package tools

// 数据库查询链路的公共类型与连接池。
//
// 复制自 internal/plugins/plugins.go 的对应片段（仅取 dbquery.go 需要的部分），
// 使独立 Agent 服务不必依赖插件管理（后者内含 SSH/SFTP/Redis 等与 Agent 无关的能力）。

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"apitool/internal/model"
)

// DBRow 查询结果（列名 + 二维字符串数据）。
type DBRow struct {
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
}

func portOrDefault(p, d int) int {
	if p <= 0 {
		return d
	}
	return p
}

// ===================== 连接池：按连接指纹复用，避免每次操作都重新建连 =====================

type pooledConn struct {
	val    interface{}
	close  func()
	mu     sync.Mutex
	expire time.Time
}

var (
	poolMu   sync.Mutex
	connPool = map[string]*pooledConn{}
)

const connPoolTTL = 90 * time.Second

// connKey 用连接的关键字段生成指纹；任一字段变化（如改密码）即视为新连接。
func connKey(c model.PluginConn) string {
	return fmt.Sprintf("%s|%s|%d|%s|%s|%s|%s", c.Category, c.Host, c.Port, c.Username, c.Password, c.DbType, c.Database)
}

// withConn 取/建与 key 关联的连接，并串行执行 fn（同一连接同一时刻仅一个操作在跑）。
func withConn(key string, factory func() (interface{}, func(), error), fn func(interface{}) error) error {
	poolMu.Lock()
	p := connPool[key]
	if p != nil && time.Now().After(p.expire) {
		go p.close()
		delete(connPool, key)
		p = nil
	}
	if p == nil {
		v, closeFn, err := factory()
		if err != nil {
			poolMu.Unlock()
			return err
		}
		p = &pooledConn{val: v, close: closeFn, expire: time.Now().Add(connPoolTTL)}
		connPool[key] = p
	}
	p.expire = time.Now().Add(connPoolTTL)
	poolMu.Unlock()

	p.mu.Lock()
	defer p.mu.Unlock()
	return fn(p.val)
}

// quoteIdent 为标识符（库名/表名）加引号。
func quoteIdent(dbType, ident string) string {
	if dbType == "postgres" || dbType == "oracle" {
		return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
	}
	return "`" + strings.ReplaceAll(ident, "`", "``") + "`"
}
