package agent

import (
	"crypto/tls"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"apitool/internal/model"
	"github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/sijms/go-ora/v2"
)

// 数据库查询实现（自 internal/plugins/db.go 复制，供独立 Agent 服务使用）。
// 与本仓库的插件管理解耦：仅保留只读查询链路。
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

func dbFactory(conn model.PluginConn, database string) func() (interface{}, func(), error) {
	return func() (interface{}, func(), error) {
		dsn := buildDBDSN(conn, database)
		var dbType string
		var db *sql.DB
		var err error
		switch conn.DbType {
		case "postgres":
			dbType, db, err = openPostgres(dsn)
		case "oracle":
			// 服务名 / SID 依次回退尝试，避免用户反复手改配置。
			// 注意：服务名只取连接配置的 database，传入的 database 参数是 schema，
			// 混用会导致「测试连接成功但工具执行报 ORA-12514」。
			port := portOrDefault(conn.Port, 1521)
			svc := strings.TrimSpace(conn.Database)
			var odb *sql.DB
			odb, err = tryOpenOracle(conn, port, svc)
			db, dbType = odb, "oracle"
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
		// Oracle 的「服务名」与「schema」是两个不同的概念，绝不能混用：
		//   - 服务名（SERVICE_NAME/SID）：网络层，由监听器注册，决定能否连上；
		//   - schema：登录后的命名空间，查询时才需要。
		// database 参数在 Oracle 语义下是 schema（用户在「选择数据库/Schema」里选的），
		// 若拿它当服务名就会把 HYDEE 之类的 schema 名发给监听器 → ORA-12514，
		// 表现为「测试连接成功，但一执行工具就报错」。因此服务名只取连接配置本身。
		svc := strings.TrimSpace(conn.Database)
		return buildOracleDSN(conn, port, svc)
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

func openOracle(dsn string, conn model.PluginConn) (string, *sql.DB, error) {
	db, err := sql.Open("oracle", dsn)
	if err != nil {
		return "", nil, err
	}
	if e := db.Ping(); e != nil {
		db.Close()
		return "", nil, oracleConnErrHint(conn, e)
	}
	return "oracle", db, nil
}

// oracleDSNVariants 按「可能性从高到低」给出 Oracle DSN 候选。
//
// 背景：Oracle 连接里的「服务名」指的是网络层服务（SERVICE_NAME / SID），
// 由监听器（Listener）注册；而用户经常把 schema 名、用户名当成服务名填进来
// （例如填 hydee），于是必然 ORA-12514。
// 同一个标识符在不同部署里既可能是 SERVICE_NAME 也可能是 SID，
// 单次尝试很容易失败，因此这里给出多个候选依次回退：
//
//  1. 作为 SERVICE_NAME 连接（标准做法）
//  2. 作为 SID 连接（老库常见，listener 注册的是 SID）
//
// 注意：不能提供「不指定服务名」的候选 —— go-ora 要求 SID 与 ServiceName
// 至少有一个（见 newConnectionStringFromUrl），那样的 DSN 会直接报
// "empty SID and service name"，既连不上又会掩盖前面候选的真实错误。
// 服务名缺失时改为直接给出可操作的提示。
func oracleDSNVariants(conn model.PluginConn, port int, svc string) []string {
	name := strings.TrimSpace(svc)
	if name == "" || strings.EqualFold(name, "MISSING_SERVICE_NAME") {
		return nil
	}
	// 显式 sid: 前缀：优先按 SID 尝试
	up := strings.ToUpper(name)
	if strings.HasPrefix(up, "SID:") || strings.HasPrefix(up, "SID=") {
		s := strings.TrimSpace(name[4:])
		if s == "" {
			return nil
		}
		return []string{
			buildOracleDSNWith(conn, port, "", s), // SID
			buildOracleDSNWith(conn, port, s, ""), // 兜底：当服务名
		}
	}
	return []string{
		buildOracleDSNWith(conn, port, name, ""), // SERVICE_NAME
		buildOracleDSNWith(conn, port, "", name), // SID
	}
}

// errEmptyOracleService 服务名缺失时的明确提示（Oracle 必须有 SERVICE_NAME 或 SID）。
var errEmptyOracleService error = fmt.Errorf(
	"Oracle 连接必须指定服务名（SERVICE_NAME）或 SID：请在「插件 / 数据库连接」的「默认库/Schema」中填写" +
		"（常见 ORCL / XE / ORCLPDB1）。注意它不是 schema 名、也不是用户名；" +
		"可在数据库服务器上执行 lsnrctl status 查看监听器已注册的服务。")

// tryOpenOracle 按候选顺序尝试连接，返回首个成功的连接。
// 全部失败时返回最有诊断价值的那个错误（见 bestErr 处说明）。
func tryOpenOracle(conn model.PluginConn, port int, svc string) (*sql.DB, error) {
	variants := oracleDSNVariants(conn, port, svc)
	if len(variants) == 0 {
		return nil, errEmptyOracleService
	}
	// bestErr 保留最有诊断价值的错误：优先第一个候选（标准 SERVICE_NAME 尝试，
	// 典型是 ORA-12514），后面的候选只在它没产出错误时才顶上。
	var bestErr error
	for i, dsn := range variants {
		db, err := sql.Open("oracle", dsn)
		if err != nil {
			if bestErr == nil || i == 0 {
				bestErr = err
			}
			continue
		}
		if e := db.Ping(); e != nil {
			_ = db.Close()
			if bestErr == nil || i == 0 {
				bestErr = e
			}
			continue
		}
		return db, nil
	}
	if bestErr == nil {
		bestErr = errEmptyOracleService
	}
	return nil, oracleConnErrHint(conn, bestErr)
}

// oracleConnErrHint 把 Oracle 常见连接错误码翻译成可操作的提示。
// 光把 ORA-12514 原样抛给用户没有意义，必须说明去哪里改。
func oracleConnErrHint(conn model.PluginConn, err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	svc := strings.TrimSpace(conn.Database)
	var hint string
	switch {
	case strings.Contains(msg, "ORA-12514"):
		hint = "监听器不认识该服务名。注意「服务名」不是 schema 名、也不是用户名，而是监听器注册的服务" +
			"（常见 ORCL / XE / ORCLPDB1）。请在数据库服务器上执行 lsnrctl status 查看可用服务，" +
			"或用 tnsping <服务名> 验证；若监听器注册的是 SID，请写成 sid:XXX。"
	case strings.Contains(msg, "ORA-01017"):
		hint = "用户名或密码无效。请确认用户名与密码（注意首尾空格与大小写）。"
	case strings.Contains(msg, "ORA-28000"):
		hint = "账号已被锁定（密码过期或多次登录失败），需先用 DBA 解锁。"
	case strings.Contains(msg, "ORA-12541"):
		hint = "未找到监听器。请确认地址/端口正确，且目标库的 Oracle Listener 已启动。"
	case strings.Contains(msg, "ORA-12154"), strings.Contains(msg, "ORA-12170"):
		hint = "无法建立网络连接。请检查主机、端口（默认 1521）以及防火墙/网络连通性。"
	case strings.Contains(msg, "ORA-01034"), strings.Contains(msg, "ORA-01019"):
		hint = "数据库未启动（未挂载）。请先启动该实例。"
	case strings.Contains(msg, "ORA-28001"):
		hint = "密码已过期，需先修改密码。"
	}
	if hint == "" {
		return err
	}
	if svc != "" {
		return fmt.Errorf("Oracle 连接失败（服务名 %s）：%v\n提示：%s", svc, err, hint)
	}
	return fmt.Errorf("Oracle 连接失败：%v\n提示：%s", err, hint)
}

// connectTimeoutSec 数据库连接超时（秒）。
const connectTimeoutSec = 10

// buildOracleDSN 构造 go-ora 的 oracle:// URL。
//
// 关键点（都曾导致「连不上」）：
//  1. 必须用 url.URL 构造而非 fmt.Sprintf 拼接。Oracle 密码里出现 @ # / : 等
//     字符时，手工拼接会被 go-ora 的 url.Parse 误判为 host / 片段分隔符，
//     表现为「用户名或密码错误」或解析失败。url.UserPassword 会做百分号编码。
//  2. 支持 SID：数据库填 "sid:XXX" 时改用?SID= 参数，
//     因为很多老库注册的是 SID 而非 ServiceName，只按 ServiceName 连会 ORA-12514。
//  3. 设 connect_timeout，避免连接测试时长时间挂起。
func buildOracleDSN(conn model.PluginConn, port int, svc string) string {
	svc = strings.TrimSpace(svc)

	// sid:XXX / SID=XXX 前缀表示这是 SID 而不是服务名
	sid := ""
	name := svc
	up := strings.ToUpper(svc)
	if strings.HasPrefix(up, "SID:") || strings.HasPrefix(up, "SID=") {
		sid = strings.TrimSpace(svc[4:])
		name = ""
	}
	return buildOracleDSNWith(conn, port, name, sid)
}

// buildOracleDSNWith 是底层构造：service 与 sid 显式指定，互斥使用。
// service 写进 URL 路径，SID 写进 ?SID= 参数。
func buildOracleDSNWith(conn model.PluginConn, port int, service, sid string) string {
	service = strings.TrimSpace(service)
	sid = strings.TrimSpace(sid)

	u := &url.URL{
		Scheme: "oracle",
		Host:   net.JoinHostPort(strings.TrimSpace(conn.Host), strconv.Itoa(port)),
		Path:   "/",
	}
	if conn.Username != "" {
		if conn.Password != "" {
			u.User = url.UserPassword(conn.Username, conn.Password)
		} else {
			u.User = url.User(conn.Username)
		}
	}
	if service != "" {
		u.Path = "/" + strings.TrimPrefix(service, "/")
	}

	q := u.Query()
	if sid != "" {
		q.Set("SID", sid)
	}
	// 超时参数名必须是 go-ora 认识的 "CONNECT TIMEOUT"（空格分隔、整数秒）。
	// 之前误用了 MySQL/libpq 的 connect_timeout，go-ora 解析 URL 时只做大写化、
	// 不会把下划线转成空格，于是落入 default 分支直接报
	// "unknown URL option: connect_timeout"，导致任何 Oracle 连接都失败。
	q.Set("CONNECT TIMEOUT", strconv.Itoa(connectTimeoutSec))
	u.RawQuery = q.Encode()

	return u.String()
}

// oracleServiceName 从「服务名/SID」输入中取出服务名部分（去掉 sid: 前缀）。
func oracleServiceName(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 4 {
		up := strings.ToUpper(s)
		if strings.HasPrefix(up, "SID:") || strings.HasPrefix(up, "SID=") {
			return ""
		}
	}
	return s
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

// switchSchema 把当前库/schema 切换到 database 指定的值。
//
// 各库语法完全不同，用错不只是语法问题。历史上 Oracle 因为走到 mysql 分支去
// 执行 USE 而直接 nil panic（Oracle 会话里 s.mysql 为 nil）：
//   - MySQL：USE `db`
//   - Oracle：没有 USE，用 ALTER SESSION SET CURRENT_SCHEMA（会话级，立即生效）
//   - PostgreSQL：没有当前库概念，schema 由连接串 / search_path 决定，不处理
func (s *dbSession) switchSchema(database string) error {
	if s.dbType == "postgres" || database == "" {
		return nil
	}
	if s.dbType == "oracle" {
		// Oracle 的 database 参数是 schema（OWNER），用 ALTER SESSION 切换。
		// 若它带 sid: 前缀说明填的是服务名而非 schema，此时不做切换。
		sch := strings.TrimSpace(database)
		if sch == "" || oracleServiceName(sch) == "" {
			return nil
		}
		_, err := s.ora.exec("ALTER SESSION SET CURRENT_SCHEMA=" + quoteIdent("oracle", strings.ToUpper(sch)))
		return err
	}
	_, err := s.mysql.exec("USE " + quoteIdent(s.dbType, database))
	return err
}

// applyLimit 给查询追加行数上限。
//
// Oracle 不支持 LIMIT n：在子查询 / UNION / 集合运算后使用会直接 ORA-00933
// 语法错误，必须用 FETCH FIRST n ROWS ONLY（12c+）。
func (s *dbSession) applyLimit(q string, limit int) string {
	if limit <= 0 {
		return q
	}
	upper := strings.ToUpper(q)
	if s.dbType == "oracle" {
		if strings.Contains(upper, "FETCH FIRST") || strings.Contains(upper, "ROWNUM") {
			return q
		}
		return fmt.Sprintf("%s FETCH FIRST %d ROWS ONLY", q, limit)
	}
	if strings.Contains(upper, " LIMIT ") {
		return q
	}
	return fmt.Sprintf("%s LIMIT %d", q, limit)
}

// probeSQL 返回连通性探活语句（Oracle 没有 SELECT 1，必须配 FROM DUAL）。
func (s *dbSession) probeSQL() string {
	if s.dbType == "oracle" {
		return "SELECT 1 FROM DUAL"
	}
	return "SELECT 1"
}

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

func PluginDBQuery(conn model.PluginConn, req DBQueryReq) (*DBRow, error) {
	var result *DBRow
	err := withConn(connKey(conn)+"|"+req.Database, dbFactory(conn, req.Database), func(v interface{}) error {
		s := v.(*dbSession)
		q := req.SQL
		// 行数上限按方言适配：Oracle 不能用 LIMIT（ORA-00933），要用 FETCH FIRST
		q = s.applyLimit(q, req.Limit)
		// 切库按方言适配：Oracle 没有 USE，用 ALTER SESSION SET CURRENT_SCHEMA。
		// 原来这里对 Oracle 走了 s.mysql 分支，s.mysql 为 nil 会直接 panic。
		if e := s.switchSchema(req.Database); e != nil {
			return e
		}
		res, e := s.query(q)
		result = res
		return e
	})
	return result, err
}
