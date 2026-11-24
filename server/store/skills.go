package store

import (
	"time"

	"apitool/internal/store/db"
)

// timeNow 可测试替换的时间源。
var timeNow = time.Now

// 服务端按用户隔离的技能存储（agent_skills 带 user_id 列）。
// DDL 在 db.go 的 schemaInit 中统一创建（见 ddlExtra）。

func init() {
	ddlExtra = append(ddlExtra, `CREATE TABLE IF NOT EXISTS user_skills (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		name TEXT NOT NULL,
		description TEXT,
		prompt TEXT,
		enabled INT,
		builtin INT,
		updated_at BIGINT
	)`)
}

// userSkills 读取用户技能列表。
func (d *DB) userSkills(userID string) []db.AgentSkill {
	rows, err := d.db.Query(`SELECT id, name, description, prompt, enabled, builtin FROM user_skills WHERE user_id=? ORDER BY updated_at`, userID)
	if err != nil {
		return []db.AgentSkill{}
	}
	defer func() { _ = rows.Close() }()
	out := []db.AgentSkill{}
	for rows.Next() {
		var s db.AgentSkill
		var enabled, builtin int
		if err := rows.Scan(&s.ID, &s.Name, &s.Description, &s.Prompt, &enabled, &builtin); err != nil {
			continue
		}
		s.Enabled = enabled != 0
		s.Builtin = builtin != 0
		out = append(out, s)
	}
	return out
}

// saveUserSkills 覆盖保存用户技能。
func (d *DB) saveUserSkills(userID string, skills []db.AgentSkill) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM user_skills WHERE user_id=?`, userID); err != nil {
		_ = tx.Rollback()
		return err
	}
	now := ms(timeNow())
	for _, s := range skills {
		if _, err := tx.Exec(`INSERT INTO user_skills (id, user_id, name, description, prompt, enabled, builtin, updated_at) VALUES (?,?,?,?,?,?,?,?)`,
			s.ID, userID, s.Name, s.Description, s.Prompt, boolToInt(s.Enabled), boolToInt(s.Builtin), now); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
