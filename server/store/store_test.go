package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"apitool/internal/store/db"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "test.db")
	d, err := Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestUsersAndTokens(t *testing.T) {
	d := newTestDB(t)
	if err := d.CreateUser(UserRow{ID: "u1", Username: "alice", Salt: "s", PwdHash: "h"}); err != nil {
		t.Fatal(err)
	}
	// 重名拒绝
	if err := d.CreateUser(UserRow{ID: "u2", Username: "alice", Salt: "s", PwdHash: "h"}); err == nil {
		t.Fatal("重名用户创建应当失败")
	}
	u, err := d.UserByName("alice")
	if err != nil || u.ID != "u1" {
		t.Fatalf("查询用户失败: %v", err)
	}
	if _, err := d.UserByName("nobody"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在用户应返回 ErrNotFound: %v", err)
	}
	// 刷新令牌
	if err := d.SaveRefreshToken("u1", "rt-1", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	uid, err := d.LookupRefreshToken("rt-1")
	if err != nil || uid != "u1" {
		t.Fatalf("lookup: %v", err)
	}
	// 轮换后旧令牌失效
	if err := d.SaveRefreshToken("u1", "rt-2", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := d.LookupRefreshToken("rt-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("旧令牌应失效: %v", err)
	}
	if err := d.RevokeRefreshToken("rt-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.LookupRefreshToken("rt-2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("吊销后应失效: %v", err)
	}
}

func TestProjectOptimisticLock(t *testing.T) {
	d := newTestDB(t)
	if err := d.CreateProject(ProjectRow{ID: "p1", OwnerID: "u1", Name: "项目A", Data: `{"name":"项目A"}`, Version: 1}); err != nil {
		t.Fatal(err)
	}
	row, err := d.Project("p1")
	if err != nil || row.Version != 1 {
		t.Fatalf("读项目: %v", err)
	}
	// 版本匹配更新成功
	if err := d.UpdateProject("p1", 1, "项目A2", `{"name":"项目A2"}`); err != nil {
		t.Fatalf("更新: %v", err)
	}
	row, _ = d.Project("p1")
	if row.Version != 2 || row.Name != "项目A2" {
		t.Fatalf("版本应+1: %+v", row)
	}
	// 旧版本更新 → 冲突
	var ce *ConflictError
	err = d.UpdateProject("p1", 1, "x", `{}`)
	if !errors.As(err, &ce) {
		t.Fatalf("应返回冲突: %v", err)
	}
	// 不存在的项目
	err = d.UpdateProject("nope", 1, "x", `{}`)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("应返回 NotFound: %v", err)
	}
}

func TestProjectIsolation(t *testing.T) {
	d := newTestDB(t)
	_ = d.CreateProject(ProjectRow{ID: "pa", OwnerID: "alice", Name: "A的项目"})
	_ = d.CreateProject(ProjectRow{ID: "pb", OwnerID: "bob", Name: "B的项目"})

	ps, err := d.ProjectsOfUser("alice")
	if err != nil || len(ps) != 1 || ps[0].ID != "pa" {
		t.Fatalf("alice 只应看到自己的项目: %+v err=%v", ps, err)
	}

	// 授权 bob 所在团队只读访问 pa
	if _, err := d.CreateTeam(TeamRow{ID: "t1", Name: "B团队", OwnerID: "bob"}); err != nil {
		t.Fatal(err)
	}
	_ = d.AddTeamMember("t1", "bob", RoleAdmin)
	_ = d.GrantProject("pa", "t1", RoleViewer)

	ps, _ = d.ProjectsOfUser("bob")
	if len(ps) != 2 {
		t.Fatalf("bob 应看到 2 个项目: %+v", ps)
	}
	role, _ := d.ProjectRole("pa", "bob")
	if role != RoleViewer {
		t.Fatalf("bob 对 pa 应是 viewer: %q", role)
	}
	role, _ = d.ProjectRole("pa", "alice")
	if role != RoleAdmin {
		t.Fatalf("owner 应是 admin: %q", role)
	}
	role, _ = d.ProjectRole("pb", "alice")
	if role != "" {
		t.Fatalf("alice 对未授权项目应无角色: %q", role)
	}
}

func TestUserStoreRoundTrip(t *testing.T) {
	d := newTestDB(t)
	us := NewUserStore(d, "u1", t.TempDir())

	// 首次读：默认数据
	data := us.GetData()
	if len(data.Projects) == 0 || data.CurrentProjectID == "" {
		t.Fatalf("默认视图: %+v", data)
	}
	// 写入
	data.Projects[0].Name = "改名项目"
	data.Settings.Theme = "dark"
	if err := us.SaveData(data); err != nil {
		t.Fatal(err)
	}
	// 回读
	got := us.GetData()
	if got.Projects[0].Name != "改名项目" {
		t.Fatalf("项目名未持久化: %+v", got.Projects[0])
	}
	if got.Settings.Theme != "dark" {
		t.Fatalf("设置未持久化: %+v", got.Settings)
	}
	// 用户隔离
	us2 := NewUserStore(d, "u2", t.TempDir())
	got2 := us2.GetData()
	if got2.Projects[0].Name == "改名项目" {
		t.Fatal("u2 不应看到 u1 的项目数据")
	}
}

func TestSkillsPerUser(t *testing.T) {
	d := newTestDB(t)
	us1 := NewUserStore(d, "u1", "")
	us2 := NewUserStore(d, "u2", "")
	if err := us1.SaveSkills([]db.AgentSkill{{ID: "s1", Name: "技能1", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if len(us1.LoadSkills()) != 1 {
		t.Fatal("u1 技能应保存")
	}
	if len(us2.LoadSkills()) != 0 {
		t.Fatal("u2 不应看到 u1 的技能")
	}
}
