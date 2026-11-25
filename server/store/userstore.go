package store

import (
	"encoding/json"
	"errors"
	"fmt"

	"apitool/internal/model"
	"apitool/internal/store/db"
	"apitool/internal/storeapi"
)

// UserStore 单用户存储视图：实现 storeapi.Store，把 appcore 的
// GetData/SaveData（AppData 整包语义）映射到服务端多行 schema。
//
// 组装/拆分规则：
//   - GetData：可见项目行 → AppData.Projects（data JSON 反序列化）；
//     user_prefs 四列 → Settings/Plugins/Clipboard/Agent。
//   - SaveData：Settings/Plugins/Clipboard → user_prefs 对应列；
//     Projects → 逐项目行（version 乐观锁；本地 AppData 里没有版本号，
//     这里采用「读最新版本→直接覆盖」策略，真正的并发保护由 /rpc 层的
//     会话互斥 + 冲突检测 API（PUT /api/projects/{id}）承担；
//     服务端会话是单用户实例，会话内的整包保存不存在并发窗口）。
type UserStore struct {
	db     *DB
	userID string
	// dataDir 用户数据根目录（导出文件等），由服务端配置注入。
	dataDir string
	// cachedDBAnalysis 最近一次读/写的 DB 分析 JSON（增量保存）。
	cachedDBAnalysis string
	// seen 本次会话通过 GetData 见过的项目 ID。
	// 用于区分「客户端新建的项目」与「客户端持有陈旧数据」：
	// 未见过 + 无版本 = 新建；见过 + 无版本 = 陈旧（要求刷新）。
	seen map[string]bool
	// versions 读取时记录的项目版本快照（projectID -> version）。
	// SaveData 用它做乐观锁：若服务端版本已前进，说明他人先保存过，
	// 返回冲突错误而不是静默覆盖。零值表示"未读过"（新项目）。
	versions map[string]int64
}

// NewUserStore 创建用户存储视图。
func NewUserStore(db *DB, userID, dataDir string) *UserStore {
	return &UserStore{db: db, userID: userID, dataDir: dataDir}
}

// Dir 返回用户数据目录。
func (u *UserStore) Dir() string { return u.dataDir }

// Path 返回空（诊断用，服务端无单数据文件）。
func (u *UserStore) Path() string { return "" }

// GetData 组装用户的 AppData 视图。
func (u *UserStore) GetData() model.AppData {
	data := model.AppData{}
	metas, err := u.db.ProjectsOfUser(u.userID)
	if err == nil {
		u.versions = make(map[string]int64, len(metas))
		u.seen = make(map[string]bool, len(metas))
		for _, m := range metas {
			row, err := u.db.Project(m.ID)
			if err != nil {
				continue
			}
			u.versions[row.ID] = row.Version
			u.seen[row.ID] = true
			// 版本随数据下发：客户端保存时回传，实现跨设备/跨会话的乐观锁
			// （服务端每用户一个会话实例，不能只依赖实例内快照）。
			var p model.Project
			if row.Data != "" {
				if err := json.Unmarshal([]byte(row.Data), &p); err != nil {
					continue
				}
			} else {
				p = model.Project{ID: row.ID, Name: row.Name}
			}
			// 行上的 name/version 为准（data 内的 name 可能滞后）
			p.ID = row.ID
			p.Name = row.Name
			p.Version = row.Version
			data.Projects = append(data.Projects, p)
		}
	}
	settingsJSON, pluginsJSON, clipJSON, agentJSON, dbSchemasJSON, err := u.db.Prefs(u.userID)
	if err == nil {
		if settingsJSON != "" {
			_ = json.Unmarshal([]byte(settingsJSON), &data.Settings)
		}
		if pluginsJSON != "" {
			_ = json.Unmarshal([]byte(pluginsJSON), &data.Plugins)
		}
		if clipJSON != "" {
			_ = json.Unmarshal([]byte(clipJSON), &data.Clipboard)
		}
		if agentJSON != "" {
			data.Agent = agentJSON
		}
		if dbSchemasJSON != "" {
			u.cachedDBAnalysis = dbSchemasJSON
		}
	}
	// 默认值兜底（与 internal/store 的兼容逻辑一致）。
	// 注意：兜底项目的 ID 必须按用户唯一（default_<userID>）——固定 "default"
	// 会与库中其它用户的同名行主键冲突，导致该用户的保存被静默跳过。
	if len(data.Projects) == 0 {
		data.Projects = []model.Project{{ID: "default_" + u.userID, Name: "默认项目"}}
	}
	if data.CurrentProjectID == "" {
		data.CurrentProjectID = data.Projects[0].ID
	}
	if data.Settings.Version == "" {
		data.Settings.Version = "2.0.0"
	}
	return data
}

// SaveData 把 AppData 整包拆写回服务端。
func (u *UserStore) SaveData(data model.AppData) error {
	// 1) 非项目部分 → user_prefs
	settingsJSON, _ := json.Marshal(data.Settings)
	pluginsJSON, _ := json.Marshal(data.Plugins)
	clipJSON, _ := json.Marshal(data.Clipboard)
	if err := u.db.SavePrefs(u.userID, string(settingsJSON), string(pluginsJSON), string(clipJSON), data.Agent, ""); err != nil {
		return fmt.Errorf("保存用户设置失败: %w", err)
	}
	// 2) 项目 → 逐行 upsert（带乐观锁）
	for _, p := range data.Projects {
		b, err := json.Marshal(p)
		if err != nil {
			continue
		}
		row, err := u.db.Project(p.ID)
		if errors.Is(err, ErrNotFound) {
			if err := u.db.CreateProject(ProjectRow{ID: p.ID, OwnerID: u.userID, Name: p.Name, Data: string(b), Version: 1}); err != nil {
				return fmt.Errorf("创建项目 %s 失败: %w", p.Name, err)
			}
			if u.versions == nil {
				u.versions = map[string]int64{}
			}
			u.versions[p.ID] = 1
			if u.seen == nil {
				u.seen = map[string]bool{}
			}
			u.seen[p.ID] = true
			continue
		}
		if err != nil {
			return err
		}
		if row.OwnerID != u.userID {
			// 被共享的项目：仅 owner 可整包覆盖（角色校验在 RPC 层做编辑权限，
			// 这里兜底拒绝非 owner 的整包写，防止共享读者覆盖他人项目）。
			continue
		}
		// 乐观锁：只以「客户端回传的 Version」为准。
		// 不用会话快照兜底——会话快照记录的就是服务端最新版本，
		// 拿它做基准会让陈旧客户端永远"看起来最新"，失去并发保护。
		if p.Version > 0 {
			if p.Version != row.Version {
				return &ConflictError{Msg: fmt.Sprintf(
					"项目「%s」已有更新版本（你基于 v%d，服务端已是 v%d），请刷新后重试",
					p.Name, p.Version, row.Version)}
			}
		} else if u.seen != nil && u.seen[p.ID] {
			// 本会话读过该项目（读时必然带过版本），客户端却没带回版本：
			// 说明它持有的是更早的陈旧副本 → 冲突，要求刷新。
			return &ConflictError{Msg: fmt.Sprintf(
				"项目「%s」的数据已过期（服务端为 v%d），请刷新后重试",
				p.Name, row.Version)}
		}
		// 未带版本且本会话没见过该项目 → 客户端新建，允许落库。
		if err := u.db.UpdateProject(p.ID, row.Version, p.Name, string(b)); err != nil {
			return fmt.Errorf("保存项目 %s 失败: %w", p.Name, err)
		}
		if u.versions == nil {
			u.versions = map[string]int64{}
		}
		u.versions[p.ID] = row.Version + 1
	}
	return nil
}

// ---- 独立域转发（实现 storeapi.Store 其余方法）----

func (u *UserStore) SaveAgentRaw(raw string) error {
	return u.db.SavePrefs(u.userID, "", "", "", raw, "")
}
func (u *UserStore) LoadAgentRaw() string {
	_, _, _, agent, _, err := u.db.Prefs(u.userID)
	if err != nil {
		return ""
	}
	return agent
}
func (u *UserStore) SaveClipboardRaw(raw string) error {
	return u.db.SavePrefs(u.userID, "", "", raw, "", "")
}
func (u *UserStore) LoadClipboardRaw() string {
	_, _, clip, _, _, err := u.db.Prefs(u.userID)
	if err != nil {
		return ""
	}
	return clip
}
func (u *UserStore) LoadSkills() []db.AgentSkill { return u.db.userSkills(u.userID) }
func (u *UserStore) SaveSkills(skills []db.AgentSkill) error {
	return u.db.saveUserSkills(u.userID, skills)
}

// 编译期确认实现完整接口。
var _ storeapi.Store = (*UserStore)(nil)

// LoadDBAnalysis / SaveDBAnalysis：数据库连接分析（user_prefs.db_schemas JSON 列）。
func (u *UserStore) LoadDBAnalysis() *db.DBAnalysisSnapshot {
	if u.cachedDBAnalysis == "" {
		_, _, _, _, dsc, err := u.db.Prefs(u.userID)
		if err == nil {
			u.cachedDBAnalysis = dsc
		}
	}
	if u.cachedDBAnalysis == "" {
		return &db.DBAnalysisSnapshot{Schemas: map[string]string{}, Semantics: map[string]string{}, LastDB: map[string]string{}}
	}
	var snap db.DBAnalysisSnapshot
	if json.Unmarshal([]byte(u.cachedDBAnalysis), &snap) != nil {
		return &db.DBAnalysisSnapshot{Schemas: map[string]string{}, Semantics: map[string]string{}, LastDB: map[string]string{}}
	}
	return &snap
}

// SaveDBAnalysis 序列化快照并增量写 user_prefs.db_schemas。
func (u *UserStore) SaveDBAnalysis(snap *db.DBAnalysisSnapshot) error {
	b, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	u.cachedDBAnalysis = string(b)
	return u.db.SavePrefs(u.userID, "", "", "", "", string(b))
}
