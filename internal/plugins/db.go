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
	_ "github.com/sijms/go-ora/v2"
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
	dbType string // mysql | postgres | oracle
	mysql  *mysqlSession
	pg     *pgSession
	ora    *oraSession
}

func (s *dbSession) Close() error {
	if s.mysql != nil {
		return s.mysql.db.Close()
	}
	if s.pg != nil {
		return s.pg.db.Close()
	}
	if s.ora != nil {
		return s.ora.db.Close()
	}
	return nil
}

func dbFactory(conn model.PluginConn) func() (interface{}, func(), error) {
	return func() (interface{}, func(), error) {
		dsn := buildDBDSN(conn, "")
		var dbType string
		var db *sql.DB
		var err error
		switch conn.DbType {
		case "postgres":
			dbType, db, err = openPostgres(dsn)
		case "oracle":
			dbType, db, err = openOracle(dsn)
		default:
			dbType, db, err = openMysql(conn, dsn)
		}
		if err != nil {
			return nil, nil, err
		}
		db.SetConnMaxLifetime(connPoolTTL)
		db.SetMaxOpenConns(2)
		switch dbType {
		case "postgres":
			return &dbSession{dbType: "postgres", pg: &pgSession{db: db}}, func() { db.Close() }, nil
		case "oracle":
			return &dbSession{dbType: "oracle", ora: &oraSession{db: db}}, func() { db.Close() }, nil
		default:
			return &dbSession{dbType: "mysql", mysql: &mysqlSession{db: db}}, func() { db.Close() }, nil
		}
	}
}

// buildDBDSN 根据连接信息构造 DSN（database 为空时不指定库，用于连接测试）
func buildDBDSN(conn model.PluginConn, database string) string {
	switch conn.DbType {
	case "postgres":
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
	case "oracle":
		port := portOrDefault(conn.Port, 1521)
		// go-ora 使用 oracle://user:pass@host:port/service 形式（database 作为服务名/SID）
		svc := database
		if svc == "" {
			svc = conn.Database
		}
		return fmt.Sprintf("oracle://%s:%s@%s:%d/%s", conn.Username, conn.Password, conn.Host, port, svc)
	default: // mysql
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

func openOracle(dsn string) (string, *sql.DB, error) {
	db, err := sql.Open("oracle", dsn)
	if err != nil {
		return "", nil, err
	}
	if e := db.Ping(); e != nil {
		db.Close()
		return "", nil, e
	}
	return "oracle", db, nil
}

type mysqlSession struct{ db *sql.DB }
type pgSession struct{ db *sql.DB }
type oraSession struct{ db *sql.DB }

func (m *mysqlSession) query(q string) (*DBRow, error) { return genericQuery(m.db, q) }
func (m *mysqlSession) exec(q string) (int64, error)   { return genericExec(m.db, q) }
func (p *pgSession) query(q string) (*DBRow, error)    { return genericQuery(p.db, q) }
func (p *pgSession) exec(q string) (int64, error)      { return genericExec(p.db, q) }
func (o *oraSession) query(q string) (*DBRow, error)    { return genericQuery(o.db, q) }
func (o *oraSession) exec(q string) (int64, error)     { return genericExec(o.db, q) }

// dbSession 转发：供 PluginDBSchema 等统一调用
func (s *dbSession) query(q string) (*DBRow, error) {
	switch s.dbType {
	case "postgres":
		return s.pg.query(q)
	case "oracle":
		return s.ora.query(q)
	default:
		return s.mysql.query(q)
	}
}
func (s *dbSession) exec(q string) (int64, error) {
	switch s.dbType {
	case "postgres":
		return s.pg.exec(q)
	case "oracle":
		return s.ora.exec(q)
	default:
		return s.mysql.exec(q)
	}
}

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
		switch s.dbType {
		case "postgres":
			res, e = s.pg.query("SELECT 1")
		case "oracle":
			res, e = s.ora.query("SELECT 1 FROM DUAL")
		default:
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
		switch s.dbType {
		case "postgres":
			res, e = s.pg.query("SELECT datname FROM pg_database WHERE datistemplate=false ORDER BY datname")
		case "oracle":
			// Oracle 无"数据库"概念，列出当前用户可访问的 schema（OWNER）
			res, e = s.ora.query("SELECT DISTINCT OWNER FROM ALL_TABLES ORDER BY OWNER")
		default:
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
		var res *DBRow
		var e error
		switch s.dbType {
		case "postgres":
			q := fmt.Sprintf("SELECT table_name, (SELECT reltuples::bigint FROM pg_class WHERE relname=table_name) FROM information_schema.tables WHERE table_schema='public' AND table_catalog='%s' ORDER BY table_name", escapeQuote(database))
			res, e = s.pg.query(q)
		case "oracle":
			q := fmt.Sprintf("SELECT table_name, num_rows FROM user_tables ORDER BY table_name")
			res, e = s.ora.query(q)
		default: // mysql
			if _, e2 := s.mysql.exec("USE " + quoteIdent(s.dbType, database)); e2 != nil {
				return e2
			}
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
			switch s.dbType {
			case "postgres":
				if len(r) >= 2 {
					t.Rows, _ = strconv.ParseInt(r[1], 10, 64)
				}
			case "oracle":
				if len(r) >= 2 && r[1] != "" {
					t.Rows, _ = strconv.ParseInt(r[1], 10, 64)
				}
			default:
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
		var res *DBRow
		switch s.dbType {
		case "postgres":
			res, e = s.pg.query(q)
		case "oracle":
			res, e = s.ora.query(q)
		default: // mysql
			res, e = s.mysql.query(q)
		}
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

// quoteIdent 为标识符（库名/表名）加引号
func quoteIdent(dbType, ident string) string {
	if dbType == "postgres" || dbType == "oracle" {
		return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
	}
	return "`" + strings.ReplaceAll(ident, "`", "``") + "`"
}

// escapeQuote 转义单引号（简单场景）
func escapeQuote(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// PluginDBColumns 读取指定表的字段定义（名称/类型/可空/默认值/注释）。
// database 指定库名；table 指定表名。
func PluginDBColumns(conn model.PluginConn, database, table string) ([]DBColumn, error) {
	var cols []DBColumn
	err := withConn(connKey(conn), dbFactory(conn), func(v interface{}) error {
		s := v.(*dbSession)
		var q string
		switch s.dbType {
		case "postgres":
			q = fmt.Sprintf(`SELECT column_name, data_type, is_nullable, column_default, '' FROM information_schema.columns WHERE table_schema='public' AND table_name=%s ORDER BY ordinal_position`, quoteStr(table))
		case "oracle":
			owner := strings.ToUpper(database)
			if owner == "" {
				owner = "USER"
			} else {
				owner = quoteStr(owner)
			}
			q = fmt.Sprintf(`SELECT c.COLUMN_NAME, c.DATA_TYPE, c.NULLABLE, c.DATA_DEFAULT, n.COMMENTS FROM ALL_TAB_COLUMNS c LEFT JOIN ALL_COL_COMMENTS n ON n.TABLE_NAME=c.TABLE_NAME AND n.COLUMN_NAME=c.COLUMN_NAME AND n.OWNER=c.OWNER WHERE c.TABLE_NAME=%s AND c.OWNER=%s ORDER BY c.COLUMN_ID`, quoteStr(strings.ToUpper(table)), owner)
		default: // mysql
			q = fmt.Sprintf("SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_DEFAULT, COLUMN_COMMENT FROM information_schema.columns WHERE table_schema=%s AND table_name=%s ORDER BY ORDINAL_POSITION", quoteStr(database), quoteStr(table))
		}
		res, e := s.query(q)
		if e != nil {
			return e
		}
		for _, r := range res.Rows {
			col := DBColumn{}
			if len(r) > 0 {
				col.Name = r[0]
			}
			if len(r) > 1 {
				col.Type = r[1]
			}
			if len(r) > 2 {
				col.Nullable = r[2]
			}
			if len(r) > 3 {
				col.Default = r[3]
			}
			if len(r) > 4 {
				col.Comment = strings.TrimSpace(r[4])
			}
			cols = append(cols, col)
		}
		return nil
	})
	return cols, err
}

// PluginDBSchema 读取一张或多张表的结构（含字段、类型、注释、行数），
// 用于把数据库结构同步给大模型做数据分析。tables 为空时返回该库全部表的结构。
func PluginDBSchema(conn model.PluginConn, database string, tables []string) ([]DBSchema, error) {
	tables, err := resolveTables(conn, database, tables)
	if err != nil {
		return nil, err
	}
	var out []DBSchema
	for _, t := range tables {
		cols, e := PluginDBColumns(conn, database, t)
		if e != nil {
			return nil, fmt.Errorf("读取表 %s 结构失败: %w", t, e)
		}
		rows := int64(0)
		if r, e2 := tableRowCount(conn, database, t); e2 == nil {
			rows = r
		}
		out = append(out, DBSchema{Database: database, Table: t, Rows: rows, Columns: cols})
	}
	return out, nil
}

// resolveTables 根据传入的表清单决定实际要分析的表：为空则列出该库全部表。
func resolveTables(conn model.PluginConn, database string, tables []string) ([]string, error) {
	if len(tables) > 0 {
		return tables, nil
	}
	ts, err := PluginDBTables(conn, database)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, t := range ts {
		names = append(names, t.Name)
	}
	return names, nil
}

// tableRowCount 取表行数（用于结构描述中标注规模）
func tableRowCount(conn model.PluginConn, database, table string) (int64, error) {
	res, err := PluginDBQuery(conn, DBQueryReq{Database: database, SQL: "SELECT COUNT(*) FROM " + quoteIdent(conn.DbType, table), Limit: 0})
	if err != nil {
		return 0, err
	}
	if len(res.Rows) > 0 && len(res.Rows[0]) > 0 {
		n, _ := strconv.ParseInt(strings.TrimSpace(res.Rows[0][0]), 10, 64)
		return n, nil
	}
	return 0, nil
}

// quoteStr 为字符串字面量加单引号并转义
func quoteStr(s string) string {
	return "'" + escapeQuote(s) + "'"
}
