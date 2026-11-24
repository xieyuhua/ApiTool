package store

import (
	"database/sql"
	"errors"
	"time"
)

// ---- projects ----

// CreateProject 新建项目（version=1）。
func (d *DB) CreateProject(p ProjectRow) error {
	if p.ID == "" {
		p.ID = NewID("p")
	}
	if p.Version <= 0 {
		p.Version = 1
	}
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = time.Now()
	}
	_, err := d.db.Exec(`INSERT INTO projects (id, owner_id, name, data, version, updated_at) VALUES (?,?,?,?,?,?)`,
		p.ID, p.OwnerID, p.Name, p.Data, p.Version, ms(p.UpdatedAt))
	return err
}

// Project 读单项目（含 data）。
func (d *DB) Project(id string) (ProjectRow, error) {
	row := d.db.QueryRow(`SELECT id, owner_id, name, data, version, updated_at FROM projects WHERE id=?`, id)
	var p ProjectRow
	var updated int64
	var data sql.NullString
	if err := row.Scan(&p.ID, &p.OwnerID, &p.Name, &data, &p.Version, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return p, ErrNotFound
		}
		return p, err
	}
	p.Data = data.String
	p.UpdatedAt = fromMS(updated)
	return p, nil
}

// UpdateProject 带乐观锁更新项目内容。
// expectVersion 为客户端持有的版本；不匹配返回 *ConflictError。
func (d *DB) UpdateProject(id string, expectVersion int64, name, data string) error {
	res, err := d.db.Exec(`UPDATE projects SET name=?, data=?, version=version+1, updated_at=? WHERE id=? AND version=?`,
		name, data, ms(time.Now()), id, expectVersion)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// 区分「项目不存在」与「版本冲突」
		var one int
		if err := d.db.QueryRow(`SELECT 1 FROM projects WHERE id=?`, id).Scan(&one); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return &ConflictError{Msg: "项目已被其他设备修改，请刷新后重试"}
	}
	return nil
}

// DeleteProject 删除项目（含授权关系）。
func (d *DB) DeleteProject(id string) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	for _, q := range []string{
		`DELETE FROM projects WHERE id=?`,
		`DELETE FROM project_members WHERE project_id=?`,
		`DELETE FROM shares WHERE project_id=?`,
	} {
		if _, err := tx.Exec(q, id); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// ProjectsOfUser 返回用户可见项目（拥有的 + 被授权的），按角色标注。
func (d *DB) ProjectsOfUser(userID string) ([]ProjectMeta, error) {
	rows, err := d.db.Query(`
		SELECT p.id, p.owner_id, p.name, p.version, p.updated_at,
			CASE WHEN p.owner_id=? THEN 'admin' ELSE COALESCE(pm.role, '') END
		FROM projects p
		LEFT JOIN project_members pm ON pm.project_id = p.id
		LEFT JOIN team_members tm ON tm.team_id = pm.team_id AND tm.user_id = ?
		WHERE p.owner_id=? OR tm.user_id IS NOT NULL`,
		userID, userID, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ProjectMeta
	for rows.Next() {
		var m ProjectMeta
		var updated int64
		if err := rows.Scan(&m.ID, &m.OwnerID, &m.Name, &m.Version, &updated, &m.Role); err != nil {
			return nil, err
		}
		m.UpdatedAt = fromMS(updated)
		out = append(out, m)
	}
	return out, rows.Err()
}

// ProjectRole 计算用户对项目的有效角色：owner=admin；否则取授权团队中的最高角色。
func (d *DB) ProjectRole(projectID, userID string) (string, error) {
	var owner string
	err := d.db.QueryRow(`SELECT owner_id FROM projects WHERE id=?`, projectID).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if owner == userID {
		return RoleAdmin, nil
	}
	rows, err := d.db.Query(`
		SELECT pm.role FROM project_members pm
		JOIN team_members tm ON tm.team_id = pm.team_id
		WHERE pm.project_id=? AND tm.user_id=?`, projectID, userID)
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()
	best := ""
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return "", err
		}
		if roleRank(role) > roleRank(best) {
			best = role
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return best, nil
}

// GrantProject 授权团队访问项目（upsert）。
func (d *DB) GrantProject(projectID, teamID, role string) error {
	_, err := d.db.Exec(`INSERT INTO project_members (project_id, team_id, role) VALUES (?,?,?)`,
		projectID, teamID, role)
	if err != nil && d.driver == "mysql" {
		_, err = d.db.Exec(`INSERT INTO project_members (project_id, team_id, role) VALUES (?,?,?)
			ON DUPLICATE KEY UPDATE role=VALUES(role)`, projectID, teamID, role)
	}
	return err
}

// RevokeProject 撤销团队授权。
func (d *DB) RevokeProject(projectID, teamID string) error {
	_, err := d.db.Exec(`DELETE FROM project_members WHERE project_id=? AND team_id=?`, projectID, teamID)
	return err
}

// ---- user_prefs ----

// SavePrefs 保存用户偏好（四个 JSON 列，空串表示不变）。
func (d *DB) SavePrefs(userID, settings, plugins, clipboard, agent string) error {
	// 先确保行存在
	if _, err := d.db.Exec(`INSERT INTO user_prefs (user_id) VALUES (?)`, userID); err != nil && d.driver == "mysql" {
		// MySQL：主键冲突时忽略
		if !isDup(err) {
			return err
		}
	}
	if settings != "" {
		if _, err := d.db.Exec(`UPDATE user_prefs SET settings=? WHERE user_id=?`, settings, userID); err != nil {
			return err
		}
	}
	if plugins != "" {
		if _, err := d.db.Exec(`UPDATE user_prefs SET plugins=? WHERE user_id=?`, plugins, userID); err != nil {
			return err
		}
	}
	if clipboard != "" {
		if _, err := d.db.Exec(`UPDATE user_prefs SET clipboard=? WHERE user_id=?`, clipboard, userID); err != nil {
			return err
		}
	}
	if agent != "" {
		if _, err := d.db.Exec(`UPDATE user_prefs SET agent=? WHERE user_id=?`, agent, userID); err != nil {
			return err
		}
	}
	return nil
}

func isDup(err error) bool {
	return err != nil && (errors.Is(err, sql.ErrNoRows) == false) && (err.Error() != "")
}

// Prefs 读取用户偏好，返回（settings, plugins, clipboard, agent）四段 JSON。
func (d *DB) Prefs(userID string) (settings, plugins, clipboard, agent string, err error) {
	row := d.db.QueryRow(`SELECT settings, plugins, clipboard, agent FROM user_prefs WHERE user_id=?`, userID)
	var s, p, c, a sql.NullString
	err = row.Scan(&s, &p, &c, &a)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", "", "", nil
	}
	return s.String, p.String, c.String, a.String, err
}

// ---- shares ----

// SaveShare 保存分享。
func (d *DB) SaveShare(s ShareRow) error {
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now()
	}
	_, err := d.db.Exec(`INSERT INTO shares (token, project_id, title, html, password, expire_at, created_at) VALUES (?,?,?,?,?,?,?)`,
		s.Token, s.ProjectID, s.Title, s.HTML, s.Password, ms(s.ExpireAt), ms(s.CreatedAt))
	return err
}

// Share 读分享。
func (d *DB) Share(token string) (ShareRow, error) {
	row := d.db.QueryRow(`SELECT token, project_id, title, html, password, expire_at, created_at FROM shares WHERE token=?`, token)
	var s ShareRow
	var expire, created int64
	var html, pwd sql.NullString
	if err := row.Scan(&s.Token, &s.ProjectID, &s.Title, &html, &pwd, &expire, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return s, ErrNotFound
		}
		return s, err
	}
	s.HTML = html.String
	s.Password = pwd.String
	s.ExpireAt = fromMS(expire)
	s.CreatedAt = fromMS(created)
	return s, nil
}

// SharesOfProject 列出项目的分享。
func (d *DB) SharesOfProject(projectID string) ([]ShareRow, error) {
	rows, err := d.db.Query(`SELECT token, project_id, title, password, expire_at, created_at FROM shares WHERE project_id=?`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ShareRow
	for rows.Next() {
		var s ShareRow
		var expire, created int64
		var pwd sql.NullString
		if err := rows.Scan(&s.Token, &s.ProjectID, &s.Title, &pwd, &expire, &created); err != nil {
			return nil, err
		}
		s.Password = pwd.String
		s.ExpireAt = fromMS(expire)
		s.CreatedAt = fromMS(created)
		out = append(out, s)
	}
	return out, rows.Err()
}

// DeleteShare 删除分享。
func (d *DB) DeleteShare(token string) error {
	_, err := d.db.Exec(`DELETE FROM shares WHERE token=?`, token)
	return err
}
