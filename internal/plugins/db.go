package plugins

import (
	"crypto/tls"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"apitool/internal/model"
	"github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
)

// ===================== 数据库插件（MySQL / PostgreSQL 最小客户端） =====================
//
// 这里不依赖第三方数据库驱动链路的复杂事务管理，仅实现：连接测试、列出数据库、
// 列出表、执行查询、执行更新四类操作，结果以二维字符串形式返回给前端展示。

// DBQueryReq 数据库查询/执行请求
type DBQueryReq struct {
	Database string `json:"database"`
	SQL      string `json:"sql"`
	Limit    int    `json:"limit"`
}

// DBExecReq 数据库执行请求（DML/DDL）
type DBExecReq struct {
	Database string `json:"database"`
	SQL      string `json:"sql"`
}

type dbSession struct {
	dbType string // mysql | postgres
	mysql  *mysqlSession
	pg     *pgSession
}

func (s *dbSession) Close() error {
	if s.mysql != nil {
		return s.mysql.db.Close()
	}
	if s.pg != nil {
		return s.pg.db.Close()
	}
	return nil
}

func dbFactory(conn model.PluginConn) func() (interface{}, func(), error) {
	return func() (interface{}, func(), error) {
		dsn := buildDBDSN(conn, "")
		var dbType string
		var db *sql.DB
		var err error
		if conn.DbType == "postgres" {
			dbType, db, err = openPostgres(dsn)
		} else {
			dbType, db, err = openMysql(conn, dsn)
		}
		if err != nil {
			return nil, nil, err
		}
		db.SetConnMaxLifetime(connPoolTTL)
		db.SetMaxOpenConns(2)
		if dbType == "postgres" {
			return &dbSession{dbType: "postgres", pg: &pgSession{db: db}}, func() { db.Close() }, nil
		}
		return &dbSession{dbType: "mysql", mysql: &mysqlSession{db: db}}, func() { db.Close() }, nil
	}
}

// buildDBDSN 根据连接信息构造 JDBC 风格的 DSN（database 为空时不指定库，用于连接测试）
func buildDBDSN(conn model.PluginConn, database string) string {
	if conn.DbType == "postgres" {
		host := conn.Host
		port := portOrDefault(conn.Port, 5432)
		user := conn.Username
		pass := conn.Password
		dbname := database
		if dbname == "" {
			dbname = "postgres"
		}
		return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable connect_timeout=10",
			host, port, user, pass, dbname)
	}
	// MySQL: 默认端口 3306
	port := portOrDefault(conn.Port, 3306)
	cfg := mysql.NewConfig()
	cfg.User = conn.Username
	cfg.Passwd = conn.Password
	cfg.Net = "tcp"
	cfg.Addr = fmt.Sprintf("%s:%d", conn.Host, port)
	cfg.DBName = database
	cfg.Timeout = 10 * time.Second
	cfg.ParseTime = true
	if conn.UseTLS {
		_ = mysql.RegisterTLSConfig("apitoolTLS", &tls.Config{InsecureSkipVerify: true})
		cfg.TLSConfig = "apitoolTLS"
	}
	return cfg.FormatDSN()
}

func openMysql(conn model.PluginConn, dsn string) (string, *sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return "", nil, err
	}
	if e := db.Ping(); e != nil {
		db.Close()
		return "", nil, e
	}
	return "mysql", db, nil
}

func openPostgres(dsn string) (string, *sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return "", nil, err
	}
	if e := db.Ping(); e != nil {
		db.Close()
		return "", nil, e
	}
	return "postgres", db, nil
}

type mysqlSession struct{ db *sql.DB }
type pgSession struct{ db *sql.DB }

func (m *mysqlSession) query(q string) (*DBRow, error) { return genericQuery(m.db, q) }
func (m *mysqlSession) exec(q string) (int64, error)   { return genericExec(m.db, q) }
func (p *pgSession) query(q string) (*DBRow, error)    { return genericQuery(p.db, q) }
func (p *pgSession) exec(q string) (int64, error)      { return genericExec(p.db, q) }

func genericQuery(db *sql.DB, q string) (*DBRow, error) {
	rows, err := db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := &DBRow{Columns: cols, Rows: [][]string{}}
	for rows.Next() {
		cells := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range cells {
			ptrs[i] = &cells[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make([]string, len(cols))
		for i, c := range cells {
			row[i] = cellToString(c)
		}
		out.Rows = append(out.Rows, row)
	}
	return out, rows.Err()
}

func genericExec(db *sql.DB, q string) (int64, error) {
	res, err := db.Exec(q)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// cellToString 将任意数据库单元值转为可读字符串（NULL 显示为 null）
func cellToString(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case []byte:
		return string(x)
	case string:
		return x
	case time.Time:
		return x.Format("2006-01-02 15:04:05")
	default:
		return fmt.Sprintf("%v", x)
	}
}

// PluginDBTest 连接测试（不指定数据库）
func PluginDBTest(conn model.PluginConn) PluginOpResult {
	err := withConn(connKey(conn), dbFactory(conn), func(v interface{}) error {
		s := v.(*dbSession)
		var res *DBRow
		var e error
		if s.dbType == "postgres" {
			res, e = s.pg.query("SELECT 1")
		} else {
			res, e = s.mysql.query("SELECT 1")
		}
		if e != nil {
			return e
		}
		if len(res.Rows) == 0 {
			return fmt.Errorf("SELECT 1 无返回")
		}
		return nil
	})
	if err != nil {
		return opErr(err)
	}
	return PluginOpResult{Ok: true, Info: "数据库连接成功"}
}

// PluginDBDatabases 列出所有数据库
func PluginDBDatabases(conn model.PluginConn) (DBInfo, error) {
	var info DBInfo
	err := withConn(connKey(conn), dbFactory(conn), func(v interface{}) error {
		s := v.(*dbSession)
		var res *DBRow
		var e error
		if s.dbType == "postgres" {
			res, e = s.pg.query("SELECT datname FROM pg_database WHERE datistemplate=false ORDER BY datname")
		} else {
			res, e = s.mysql.query("SHOW DATABASES")
		}
		if e != nil {
			return e
		}
		for _, r := range res.Rows {
			if len(r) > 0 {
				info.Databases = append(info.Databases, r[0])
			}
		}
		info.Ok = true
		return nil
	})
	return info, err
}

// PluginDBTables 列出某库的表
func PluginDBTables(conn model.PluginConn, database string) ([]DBTable, error) {
	var tables []DBTable
	err := withConn(connKey(conn), dbFactory(conn), func(v interface{}) error {
		s := v.(*dbSession)
		if s.dbType != "postgres" {
			// MySQL 需要先 USE
			if _, e := s.mysql.exec("USE " + quoteIdent(s.dbType, database)); e != nil {
				return e
			}
		}
		var res *DBRow
		var e error
		if s.dbType == "postgres" {
			q := fmt.Sprintf("SELECT table_name, (SELECT reltuples::bigint FROM pg_class WHERE relname=table_name) FROM information_schema.tables WHERE table_schema='public' AND table_catalog='%s' ORDER BY table_name", escapeQuote(database))
			res, e = s.pg.query(q)
		} else {
			res, e = s.mysql.query("SHOW TABLE STATUS")
		}
		if e != nil {
			return e
		}
		for _, r := range res.Rows {
			t := DBTable{}
			if len(r) >= 1 {
				t.Name = r[0]
			}
			if s.dbType == "postgres" {
				if len(r) >= 2 {
					t.Rows, _ = strconv.ParseInt(r[1], 10, 64)
				}
			} else {
				if len(r) >= 2 {
					t.Engine = r[1]
				}
				if len(r) >= 5 {
					t.Rows, _ = strconv.ParseInt(r[4], 10, 64)
				}
			}
			tables = append(tables, t)
		}
		return nil
	})
	return tables, err
}

// PluginDBQuery 执行查询（SELECT）
func PluginDBQuery(conn model.PluginConn, req DBQueryReq) (*DBRow, error) {
	var result *DBRow
	err := withConn(connKey(conn), dbFactory(conn), func(v interface{}) error {
		s := v.(*dbSession)
		q := req.SQL
		if req.Limit > 0 && s.dbType != "postgres" {
			if !strings.Contains(strings.ToUpper(q), " LIMIT ") {
				q = fmt.Sprintf("%s LIMIT %d", q, req.Limit)
			}
		}
		if s.dbType != "postgres" && req.Database != "" {
			if _, e := s.mysql.exec("USE " + quoteIdent(s.dbType, req.Database)); e != nil {
				return e
			}
		}
		var e error
		dsnDB := s.mysql
		if s.dbType == "postgres" {
			// postgres 用 search_path 切换库（在 DSN 中已指定 database）
			res, e2 := s.pg.query(q)
			result = res
			return e2
		}
		_ = dsnDB
		res, e := s.mysql.query(q)
		result = res
		return e
	})
	return result, err
}

// PluginDBExec 执行 DML/DDL（INSERT/UPDATE/DELETE/CREATE...）
func PluginDBExec(conn model.PluginConn, req DBExecReq) (int64, error) {
	var affected int64
	err := withConn(connKey(conn), dbFactory(conn), func(v interface{}) error {
		s := v.(*dbSession)
		if s.dbType != "postgres" && req.Database != "" {
			if _, e := s.mysql.exec("USE " + quoteIdent(s.dbType, req.Database)); e != nil {
				return e
			}
		}
		var e error
		if s.dbType == "postgres" {
			affected, e = s.pg.exec(req.SQL)
		} else {
			affected, e = s.mysql.exec(req.SQL)
		}
		return e
	})
	return affected, err
}

// quoteIdent 为标识符（库名/表名）加反引号（MySQL）
func quoteIdent(dbType, ident string) string {
	if dbType == "postgres" {
		return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
	}
	return "`" + strings.ReplaceAll(ident, "`", "``") + "`"
}

// escapeQuote 转义单引号（简单场景）
func escapeQuote(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
