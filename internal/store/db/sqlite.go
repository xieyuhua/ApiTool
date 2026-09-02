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
	if err := initSchema(conn); err != nil {
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

// UpdateAgent 仅更新 meta.agent 单列，避免全量重写整个库。
func (s *SQLiteDB) UpdateAgent(raw string) error {
	return updateAgent(s.db, raw)
}

// ReadAgent 读取 meta.agent 单列。
func (s *SQLiteDB) ReadAgent() (string, error) {
	return readAgent(s.db)
}

// ReadSkills 读取全部技能（独立表）。
func (s *SQLiteDB) ReadSkills() ([]AgentSkill, error) {
	return readSkills(s.db)
}

// SaveSkills 覆盖保存技能列表（独立表）。
func (s *SQLiteDB) SaveSkills(skills []AgentSkill) error {
	return withTx(s.db, func(tx *sql.Tx) error {
		return saveSkills(tx, skills)
	})
}

// ReadDBAnalysis 读取数据库连接分析数据（独立表）。
func (s *SQLiteDB) ReadDBAnalysis() (*DBAnalysisSnapshot, error) {
	return readDBAnalysis(s.db)
}

// SaveDBAnalysis 覆盖保存数据库连接分析数据（独立表）。
func (s *SQLiteDB) SaveDBAnalysis(snap *DBAnalysisSnapshot) error {
	return withTx(s.db, func(tx *sql.Tx) error {
		return saveDBAnalysis(tx, snap)
	})
}
