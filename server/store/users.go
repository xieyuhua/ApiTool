package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound 记录不存在。
var ErrNotFound = errors.New("记录不存在")

// ErrConflict 乐观锁冲突。
type ConflictError struct{ Msg string }

func (e *ConflictError) Error() string { return e.Msg }

// NewID 生成实体 ID。
func NewID(prefix string) string {
	return prefix + "_" + uuid.NewString()
}

// hashRefresh 对刷新令牌做 SHA-256（落库只存哈希，泄漏库无法还原令牌）。
func hashRefresh(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ---- users ----

// CreateUser 创建账号。
func (d *DB) CreateUser(u UserRow) error {
	if u.ID == "" {
		u.ID = NewID("u")
	}
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now()
	}
	_, err := d.db.Exec(`INSERT INTO users (id, username, salt, pwd_hash, created_at) VALUES (?,?,?,?,?)`,
		u.ID, u.Username, u.Salt, u.PwdHash, ms(u.CreatedAt))
	if err != nil {
		return fmt.Errorf("创建用户失败: %w", err)
	}
	return nil
}

// UserByName 按用户名查账号。
func (d *DB) UserByName(username string) (UserRow, error) {
	row := d.db.QueryRow(`SELECT id, username, salt, pwd_hash, created_at FROM users WHERE username=?`, username)
	return scanUser(row)
}

// UserByID 按 ID 查账号。
func (d *DB) UserByID(id string) (UserRow, error) {
	row := d.db.QueryRow(`SELECT id, username, salt, pwd_hash, created_at FROM users WHERE id=?`, id)
	return scanUser(row)
}

func scanUser(row *sql.Row) (UserRow, error) {
	var u UserRow
	var created int64
	if err := row.Scan(&u.ID, &u.Username, &u.Salt, &u.PwdHash, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return u, ErrNotFound
		}
		return u, err
	}
	u.CreatedAt = fromMS(created)
	return u, nil
}

// ---- tokens ----

// SaveRefreshToken 保存（轮换）刷新令牌：先删旧哈希再插新。
func (d *DB) SaveRefreshToken(userID, refreshToken string, expire time.Time) error {
	h := hashRefresh(refreshToken)
	if _, err := d.db.Exec(`DELETE FROM tokens WHERE user_id=?`, userID); err != nil {
		return err
	}
	_, err := d.db.Exec(`INSERT INTO tokens (refresh_hash, user_id, expire_at) VALUES (?,?,?)`,
		h, userID, ms(expire))
	return err
}

// LookupRefreshToken 校验刷新令牌，返回 userID。
func (d *DB) LookupRefreshToken(refreshToken string) (string, error) {
	h := hashRefresh(refreshToken)
	var userID string
	var expire int64
	err := d.db.QueryRow(`SELECT user_id, expire_at FROM tokens WHERE refresh_hash=?`, h).Scan(&userID, &expire)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if time.Now().UnixMilli() > expire {
		_, _ = d.db.Exec(`DELETE FROM tokens WHERE refresh_hash=?`, h)
		return "", ErrNotFound
	}
	return userID, nil
}

// RevokeRefreshToken 吊销刷新令牌（登出）。
func (d *DB) RevokeRefreshToken(refreshToken string) error {
	_, err := d.db.Exec(`DELETE FROM tokens WHERE refresh_hash=?`, hashRefresh(refreshToken))
	return err
}

// ---- teams ----

// CreateTeam 创建团队并把 owner 设为 admin，返回生成的团队 ID。
func (d *DB) CreateTeam(t TeamRow) (string, error) {
	if t.ID == "" {
		t.ID = NewID("t")
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now()
	}
	tx, err := d.db.Begin()
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(`INSERT INTO teams (id, name, owner_id, created_at) VALUES (?,?,?,?)`,
		t.ID, t.Name, t.OwnerID, ms(t.CreatedAt)); err != nil {
		_ = tx.Rollback()
		return "", err
	}
	if _, err := tx.Exec(`INSERT INTO team_members (team_id, user_id, role) VALUES (?,?,?)`,
		t.ID, t.OwnerID, RoleAdmin); err != nil {
		_ = tx.Rollback()
		return "", err
	}
	return t.ID, tx.Commit()
}

// TeamsOfUser 返回用户所在的团队列表。
func (d *DB) TeamsOfUser(userID string) ([]TeamRow, error) {
	rows, err := d.db.Query(`SELECT t.id, t.name, t.owner_id, t.created_at FROM teams t
		JOIN team_members m ON m.team_id=t.id WHERE m.user_id=?`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []TeamRow
	for rows.Next() {
		var t TeamRow
		var created int64
		if err := rows.Scan(&t.ID, &t.Name, &t.OwnerID, &created); err != nil {
			return nil, err
		}
		t.CreatedAt = fromMS(created)
		out = append(out, t)
	}
	return out, rows.Err()
}

// TeamRole 返回用户在团队中的角色（不在则空串）。
func (d *DB) TeamRole(teamID, userID string) (string, error) {
	var role string
	err := d.db.QueryRow(`SELECT role FROM team_members WHERE team_id=? AND user_id=?`, teamID, userID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return role, err
}

// AddTeamMember 添加/更新团队成员。
func (d *DB) AddTeamMember(teamID, userID, role string) error {
	_, err := d.db.Exec(`INSERT INTO team_members (team_id, user_id, role) VALUES (?,?,?)
		ON CONFLICT (team_id, user_id) DO UPDATE SET role=excluded.role`, teamID, userID, role)
	if err != nil && d.driver == "mysql" {
		// MySQL 的 upsert 语法不同
		_, err = d.db.Exec(`INSERT INTO team_members (team_id, user_id, role) VALUES (?,?,?)
			ON DUPLICATE KEY UPDATE role=VALUES(role)`, teamID, userID, role)
	}
	return err
}

// RemoveTeamMember 移除成员。
func (d *DB) RemoveTeamMember(teamID, userID string) error {
	_, err := d.db.Exec(`DELETE FROM team_members WHERE team_id=? AND user_id=?`, teamID, userID)
	return err
}

// TeamMembers 返回团队成员（联表带出用户名，供前端展示）。
func (d *DB) TeamMembers(teamID string) ([]MemberRow, error) {
	rows, err := d.db.Query(`SELECT m.team_id, m.user_id, m.role, u.username FROM team_members m
		LEFT JOIN users u ON u.id=m.user_id WHERE m.team_id=?`, teamID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []MemberRow
	for rows.Next() {
		var m MemberRow
		var uname sql.NullString
		if err := rows.Scan(&m.TeamID, &m.UserID, &m.Role, &uname); err != nil {
			return nil, err
		}
		m.Username = uname.String
		out = append(out, m)
	}
	return out, rows.Err()
}
