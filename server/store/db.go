package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// DB 服务端存储句柄（底层单一 sql.DB，方言由 DSN 决定）。
type DB struct {
	db     *sql.DB
	driver string // sqlite | mysql
}

// Open 打开服务端数据库。
// sqliteDSN 形如 "file:path/to/apitool-server.db"；mysqlDSN 为标准连接串。
func Open(driver, dsn string) (*DB, error) {
	var db *sql.DB
	var err error
	switch driver {
	case "sqlite", "":
		db, err = sql.Open("sqlite", dsn)
		if err == nil {
			db.SetMaxOpenConns(1) // SQLite 写并发受限
		}
	case "mysql":
		db, err = sql.Open("mysql", dsn)
		if err == nil {
			db.SetMaxOpenConns(16)
		}
	default:
		return nil, fmt.Errorf("不支持的数据库类型: %s", driver)
	}
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	d := &DB{db: db, driver: driver}
	if err := d.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return d, nil
}

// Close 关闭连接。
func (d *DB) Close() error { return d.db.Close() }

// Driver 返回后端类型。
func (d *DB) Driver() string { return d.driver }

// ddl 返回建表语句（SQLite/MySQL 兼容语法：TEXT 主键 + BIGINT 时间戳毫秒）。
func (d *DB) ddl() []string {
	ts := "BIGINT"  // 时间统一存毫秒时间戳，避免双方言 datetime 差异
	txt := "TEXT"   // 普通文本列
	pk  := "TEXT"   // 主键/索引列：MySQL 的 TEXT 不能直接做主键，需 VARCHAR
	big := "MEDIUMTEXT"
	if d.driver == "sqlite" {
		big = "TEXT"
	} else {
		pk = "VARCHAR(64)"
	}
	return []string{
		`CREATE TABLE IF NOT EXISTS users (
			id ` + pk + ` PRIMARY KEY,
			username ` + pk + ` NOT NULL UNIQUE,
			salt ` + txt + ` NOT NULL,
			pwd_hash ` + txt + ` NOT NULL,
			created_at ` + ts + ` NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tokens (
			refresh_hash ` + pk + ` PRIMARY KEY,
			user_id ` + pk + ` NOT NULL,
			expire_at ` + ts + ` NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS teams (
			id ` + pk + ` PRIMARY KEY,
			name ` + txt + ` NOT NULL,
			owner_id ` + pk + ` NOT NULL,
			created_at ` + ts + ` NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS team_members (
			team_id ` + pk + ` NOT NULL,
			user_id ` + pk + ` NOT NULL,
			role ` + txt + ` NOT NULL,
			PRIMARY KEY (team_id, user_id)
		)`,
		`CREATE TABLE IF NOT EXISTS projects (
			id ` + pk + ` PRIMARY KEY,
			owner_id ` + pk + ` NOT NULL,
			name ` + txt + ` NOT NULL,
			data ` + big + `,
			version BIGINT NOT NULL DEFAULT 1,
			updated_at ` + ts + ` NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS project_members (
			project_id ` + pk + ` NOT NULL,
			team_id ` + pk + ` NOT NULL,
			role ` + txt + ` NOT NULL,
			PRIMARY KEY (project_id, team_id)
		)`,
		`CREATE TABLE IF NOT EXISTS user_prefs (
			user_id ` + pk + ` PRIMARY KEY,
			settings ` + big + `,
			plugins ` + big + `,
			clipboard ` + big + `,
			agent ` + big + `,
			db_schemas ` + big + `
		)`,
		`CREATE TABLE IF NOT EXISTS shares (
			token ` + pk + ` PRIMARY KEY,
			project_id ` + pk + ` NOT NULL,
			title ` + txt + `,
			html ` + big + `,
			password ` + txt + `,
			expire_at ` + ts + ` NOT NULL,
			created_at ` + ts + ` NOT NULL
		)`,
	}
}

func (d *DB) initSchema() error {
	stmts := d.ddl()
	stmts = append(stmts, ddlExtra...)
	// 方言替换：{PK} 在 MySQL 下为 VARCHAR(64)（TEXT 不能做主键），SQLite 下为 TEXT
	pk := "TEXT"
	if d.driver != "sqlite" {
		pk = "VARCHAR(64)"
	}
	for i := range stmts {
		stmts[i] = strings.ReplaceAll(stmts[i], "{PK}", pk)
	}
	for _, s := range stmts {
		if _, err := d.db.Exec(s); err != nil {
			return fmt.Errorf("建表失败: %w", err)
		}
	}
	// 旧库兼容：CREATE IF NOT EXISTS 不会给已存在的 user_prefs 表补新列，
	// 建表后探测补列（列已存在时跳过）。
	return d.migrateAlter()
}

// migrateAlter 旧库补列：建表后探测 user_prefs 是否缺 db_schemas 列，缺则 ALTER。
// 列已存在（新库）时直接返回 nil。
func (d *DB) migrateAlter() error {
	hasColumn := false
	if d.driver == "sqlite" {
		rows, err := d.db.Query(`PRAGMA table_info(user_prefs)`)
		if err == nil {
			for rows.Next() {
				var cid int
				var name, ctype string
				var notNull int
				var dfltValue interface{}
				var pk int
				if rows.Scan(&cid, &name, &ctype, &notNull, &dfltValue, &pk) == nil && name == "db_schemas" {
					hasColumn = true
					break
				}
			}
			_ = rows.Close()
			if err := rows.Err(); err != nil {
				return nil // 探测失败：跳过迁移
			}
		}
	} else {
		rows, err := d.db.Query(`SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='user_prefs' AND COLUMN_NAME='db_schemas'`)
		if err == nil {
			hasColumn = rows.Next()
			_ = rows.Close()
		}
	}
	if hasColumn {
		return nil
	}
	alterBig := "MEDIUMTEXT"
	if d.driver == "sqlite" {
		alterBig = "TEXT"
	}
	if _, err := d.db.Exec(`ALTER TABLE user_prefs ADD COLUMN db_schemas ` + alterBig); err != nil {
		// 并发或已存在：忽略（不影响功能）
		return nil
	}
	return nil
}

// ddlExtra 各文件追加的 DDL（init 时统一执行）。
var ddlExtra []string

// ---- 时间戳辅助 ----

func ms(t time.Time) int64 { return t.UnixMilli() }
func fromMS(v int64) time.Time {
	if v <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(v)
}
