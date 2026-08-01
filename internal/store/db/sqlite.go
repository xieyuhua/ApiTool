package db

import (
	"database/sql"

	"apitool/internal/model"

	_ "modernc.org/sqlite"
)

// SQLiteDB 基于现代纯 Go SQLite 驱动（modernc.org/sqlite）的存储后端。
// dsn 为数据库文件路径。
type SQLiteDB struct {
	db   *sql.DB
	dsn  string
	ver  string
	url  string
}

// OpenSQLite 打开（或创建）SQLite 数据库并完成建表。
func OpenSQLite(dsn, version, updateURL string) (*SQLiteDB, error) {
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(1) // SQLite 写并发受限，单连接最稳
	if err := conn.Ping(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := initSchema(conn, "sqlite"); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &SQLiteDB{db: conn, dsn: dsn, ver: version, url: updateURL}, nil
}

func (s *SQLiteDB) Driver() string { return "sqlite" }

func (s *SQLiteDB) Read() (model.AppData, error) {
	return readAll(s.db, s.ver, s.url)
}

func (s *SQLiteDB) Write(data model.AppData) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := writeAll(tx, data); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *SQLiteDB) Close() error {
	return s.db.Close()
}
