package db

import (
	"database/sql"

	"apitool/internal/model"

	_ "github.com/go-sql-driver/mysql"
)

// MySQLDB 基于 go-sql-driver/mysql 的远程/共享存储后端，schema 与 SQLite 完全一致。
// dsn 为 MySQL 连接串，形如 user:pass@tcp(host:3306)/dbname?parseTime=true&charset=utf8mb4。
type MySQLDB struct {
	db  *sql.DB
	dsn string
	ver string
	url string
}

// OpenMySQL 打开（或创建）MySQL 数据库并完成建表。
func OpenMySQL(dsn, version, updateURL string) (*MySQLDB, error) {
	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	if err := conn.Ping(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := initSchema(conn, "mysql"); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &MySQLDB{db: conn, dsn: dsn, ver: version, url: updateURL}, nil
}

func (m *MySQLDB) Driver() string { return "mysql" }

func (m *MySQLDB) Read() (model.AppData, error) {
	return readAll(m.db, m.ver, m.url)
}

func (m *MySQLDB) Write(data model.AppData) error {
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	if err := writeAll(tx, data); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (m *MySQLDB) Close() error {
	return m.db.Close()
}
