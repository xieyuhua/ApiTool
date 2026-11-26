// Package store 服务端专用存储：多用户 schema（SQLite/MySQL 双后端）。
//
// 与 internal/store（桌面单租户全量覆盖）不同，本包按行存储：
//   - users/tokens：账号与刷新令牌；
//   - teams/team_members：团队与成员角色；
//   - projects：项目本体整包 JSON 列 + version 乐观锁（与前端整包 SaveData
//     契约对齐，冲突返回 409）；
//   - project_members：项目对团队的授权（role: admin/editor/viewer）；
//   - user_prefs：每用户的 settings/plugins/clipboard/agent（JSON 列）；
//   - shares：文档分享（沿用旧 share 语义）。
//
// 每个登录用户经 UserStore（实现 storeapi.Store）访问自己的数据，
// appcore 会话实例不感知底层差异。
package store

import "time"

// UserRow 账号。
type UserRow struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Salt      string    `json:"-"`
	PwdHash   string    `json:"-"` // bcrypt
	CreatedAt time.Time `json:"createdAt"`
}

// TokenRow 刷新令牌（access JWT 不落库）。
type TokenRow struct {
	UserID     string    `json:"userId"`
	RefreshHash string   `json:"-"` // sha256(refreshToken)
	ExpireAt   time.Time `json:"expireAt"`
}

// TeamRow 团队。
type TeamRow struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	OwnerID   string    `json:"ownerId"`
	CreatedAt time.Time `json:"createdAt"`
}

// MemberRow 成员关系（team_members / project_members 共用 role 语义）。
type MemberRow struct {
	TeamID    string `json:"teamId,omitempty"`
	ProjectID string `json:"projectId,omitempty"`
	UserID    string `json:"userId"`
	Username  string `json:"username,omitempty"` // 成员列表展示用（TeamMembers 联表填充）
	Role      string `json:"role"`               // admin | editor | viewer
}

// ProjectRow 项目（data 为 model.Project 整包 JSON）。
type ProjectRow struct {
	ID        string    `json:"id"`
	OwnerID   string    `json:"ownerId"`
	Name      string    `json:"name"`
	Data      string    `json:"-"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ProjectMeta 项目元数据（列表用，不含 data）。
type ProjectMeta struct {
	ID        string    `json:"id"`
	OwnerID   string    `json:"ownerId"`
	Name      string    `json:"name"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updatedAt"`
	Role      string    `json:"role"` // 当前用户在该项目上的角色
}

// ShareRow 分享。
type ShareRow struct {
	Token     string    `json:"token"`
	ProjectID string    `json:"projectId"`
	Title     string    `json:"title"`
	HTML      string    `json:"-"`
	Password  string    `json:"password,omitempty"`
	ExpireAt  time.Time `json:"expireAt"`
	CreatedAt time.Time `json:"createdAt"`
}

// 角色常量。
const (
	RoleAdmin  = "admin"
	RoleEditor = "editor"
	RoleViewer = "viewer"
)

// roleRank 角色权限等级（用于比较）。
func roleRank(r string) int {
	switch r {
	case RoleAdmin:
		return 3
	case RoleEditor:
		return 2
	case RoleViewer:
		return 1
	}
	return 0
}
