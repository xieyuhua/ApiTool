package store

import (
	"database/sql"
	"fmt"
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
	ts := "BIGINT" // 时间统一存毫秒时间戳，避免双方言 datetime 差异
	txt := "TEXT"
	big := "MEDIUMTEXT"
	if d.driver == "sqlite" {
		big = "TEXT"
	}
	return []string{
		`CREATE TABLE IF NOT EXISTS users (
			id ` + txt + ` PRIMARY KEY,
			username ` + txt + ` NOT NULL UNIQUE,
			salt ` + txt + ` NOT NULL,
			pwd_hash ` + txt + ` NOT NULL,
			created_at ` + ts + ` NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tokens (
			refresh_hash ` + txt + ` PRIMARY KEY,
			user_id ` + txt + ` NOT NULL,
			expire_at ` + ts + ` NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS teams (
			id ` + txt + ` PRIMARY KEY,
			name ` + txt + ` NOT NULL,
			owner_id ` + txt + ` NOT NULL,
			created_at ` + ts + ` NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS team_members (
			team_id ` + txt + ` NOT NULL,
			user_id ` + txt + ` NOT NULL,
			role ` + txt + ` NOT NULL,
			PRIMARY KEY (team_id, user_id)
		)`,
		`CREATE TABLE IF NOT EXISTS projects (
			id ` + txt + ` PRIMARY KEY,
			owner_id ` + txt + ` NOT NULL,
			name ` + txt + ` NOT NULL,
			data ` + big + `,
			version BIGINT NOT NULL DEFAULT 1,
			updated_at ` + ts + ` NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS project_members (
			project_id ` + txt + ` NOT NULL,
			team_id ` + txt + ` NOT NULL,
			role ` + txt + ` NOT NULL,
			PRIMARY KEY (project_id, team_id)
		)`,
		`CREATE TABLE IF NOT EXISTS user_prefs (
			user_id ` + txt + ` PRIMARY KEY,
			settings ` + big + `,
			plugins ` + big + `,
			clipboard ` + big + `,
			agent ` + big + `
		)`,
		`CREATE TABLE IF NOT EXISTS shares (
			token ` + txt + ` PRIMARY KEY,
			project_id ` + txt + ` NOT NULL,
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
	for _, s := range stmts {
		if _, err := d.db.Exec(s); err != nil {
			return fmt.Errorf("建表失败: %w", err)
		}
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
