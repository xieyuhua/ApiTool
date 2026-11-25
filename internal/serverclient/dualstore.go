package serverclient

import (
	"encoding/json"
	"fmt"
	"sync"

	"apitool/internal/model"
	"apitool/internal/store/db"
	"apitool/internal/storeapi"
)

// DualStore 双模式存储：未登录 → 直通本地 SQLite（local）；已登录 → 数据读写
// 经服务端 /rpc，本地 SQLite 作为**写穿镜像**（保证 RunTest/ExportDoc 等
// App 内部读路径的一致性）。
//
// 路由表：
//
//	方法                未登录        已登录
//	GetData             local        /rpc/LoadData + 写穿本地镜像
//	SaveData            local        /rpc/SaveData + 写穿本地镜像
//	SaveAgentRaw 等     local        local（agent/技能/DB分析暂留本地，P3.5 合并）
//	ClipboardRaw        local        local（桌面剪贴板采集始终本地）
//	Dir/Path            local        local
type DualStore struct {
	local  storeapi.Store // 本地 SQLite（internal/store.Store）
	client *Client
	mu     sync.RWMutex
}

// NewDualStore 创建双模式存储。
func NewDualStore(local storeapi.Store, client *Client) *DualStore {
	return &DualStore{local: local, client: client}
}

// Client 返回底层客户端（登录状态检查用）。
func (d *DualStore) Client() *Client { return d.client }

// ---- storeapi.Store 实现 ----

// GetData 读数据：已登录时从服务端拉取并写穿到本地镜像。
func (d *DualStore) GetData() model.AppData {
	if !d.client.LoggedIn() {
		return d.local.GetData()
	}
	var data model.AppData
	// LoadData 无参
	if err := d.client.CallRPC("LoadData", []json.RawMessage{}, &data); err != nil {
		// 服务端不可达：降级读本地镜像（离线可用），由上层提示
		return d.local.GetData()
	}
	if data.CurrentProjectID == "" || len(data.Projects) == 0 {
		// 服务端为空（全新用户）：返回一份空的默认视图，但不写穿本地镜像——
		// 否则本地旧项目（可能是另一账号/另一服务器的数据）会被当成该用户
		// 的数据推上服务端，产生游离项目。
		return model.AppData{
			CurrentProjectID: "default_" + d.client.UserID(),
			Projects:         []model.Project{{ID: "default_" + d.client.UserID(), Name: "默认项目"}},
		}
	}
	// 写穿本地镜像（离线降级数据源）
	_ = d.local.SaveData(data)
	return data
}

// SaveData 写数据：已登录时先推服务端（失败抛错，由上层处理冲突/401），
// 成功后写穿本地镜像。
func (d *DualStore) SaveData(data model.AppData) error {
	if !d.client.LoggedIn() {
		return d.local.SaveData(data)
	}
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	// CallRPC 的参数是 JSON 数组的每个元素：[data]
	args := []json.RawMessage{b}
	var unused json.RawMessage
	if err := d.client.CallRPC("SaveData", args, &unused); err != nil {
		return err
	}
	return d.local.SaveData(data)
}

// Dir 本地数据目录（图片/导出文件始终落本地磁盘）。
func (d *DualStore) Dir() string { return d.local.Dir() }

// Path 本地数据文件路径（诊断展示）。
func (d *DualStore) Path() string { return d.local.Path() }

// ---- agent 数据：暂留本地（P3.5 再上服务端，避免一次迁移过多）----

func (d *DualStore) SaveAgentRaw(raw string) error { return d.local.SaveAgentRaw(raw) }
func (d *DualStore) LoadAgentRaw() string          { return d.local.LoadAgentRaw() }
func (d *DualStore) SaveClipboardRaw(raw string) error {
	return d.local.SaveClipboardRaw(raw)
}
func (d *DualStore) LoadClipboardRaw() string { return d.local.LoadClipboardRaw() }
func (d *DualStore) LoadSkills() []db.AgentSkill {
	return d.local.LoadSkills()
}
func (d *DualStore) SaveSkills(skills []db.AgentSkill) error {
	return d.local.SaveSkills(skills)
}

// LoadDBAnalysis / SaveDBAnalysis：数据库连接分析暂留本地（与 agent 数据同策略）。
func (d *DualStore) LoadDBAnalysis() *db.DBAnalysisSnapshot { return d.local.LoadDBAnalysis() }
func (d *DualStore) SaveDBAnalysis(snap *db.DBAnalysisSnapshot) error {
	return d.local.SaveDBAnalysis(snap)
}

// 编译期确认。
var _ storeapi.Store = (*DualStore)(nil)

// FetchProjectsMeta 拉取服务端项目元数据（角色/版本），供前端 role 表填充。
func (d *DualStore) FetchProjectsMeta() ([]ProjectMeta, error) {
	var metas []ProjectMeta
	if err := d.client.post("/api/projects", nil, &metas, true); err != nil {
		return nil, err
	}
	return metas, nil
}

// ProjectMeta 服务端项目元数据（对齐 server/store.ProjectMeta）。
type ProjectMeta struct {
	ID        string `json:"id"`
	OwnerID   string `json:"ownerId"`
	Name      string `json:"name"`
	Version   int64  `json:"version"`
	UpdatedAt string `json:"updatedAt"`
	Role      string `json:"role"`
}

var _ = fmt.Sprintf // 保持 fmt 引用（错误信息构造在 client.go）
