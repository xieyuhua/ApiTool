// Package storeapi 定义业务模块与数据存储之间的统一抽象接口。
//
// 2.0 拆分：桌面端数据在本地 SQLite（internal/store.Store 实现），服务端数据
// 在按用户隔离的多行 schema（server/store 实现）。业务模块（agent/capture/
// doc/share/stress）只依赖本接口，不再绑定具体实现。
//
// 语义（与 internal/store.Store 保持一致）：
//   - GetData 返回完整应用数据快照；
//   - SaveData 整体覆盖保存（服务端实现内部做拆行 + 乐观锁）；
//   - Agent/技能/数据库分析等独立域各有专门方法（对应独立表/单列）。
package storeapi

import (
	"apitool/internal/model"
	"apitool/internal/store/db"
)

// Store 数据存储的统一接口。
type Store interface {
	GetData() model.AppData
	SaveData(data model.AppData) error
	// Dir 返回数据目录（图片、导出文件等相对资源的基准目录）。
	Dir() string
	// Path 返回数据文件路径（诊断展示用；服务端实现可返回空）。
	Path() string

	// Agent 数据（meta.agent 单列语义）
	SaveAgentRaw(raw string) error
	LoadAgentRaw() string
	// 剪贴板历史（meta.clipboard 单列语义）
	SaveClipboardRaw(raw string) error
	LoadClipboardRaw() string
	// 技能（agent_skills 独立表）
	LoadSkills() []db.AgentSkill
	SaveSkills(skills []db.AgentSkill) error
	// 数据库连接分析（db_schemas 等独立表）
	LoadDBAnalysis() *db.DBAnalysisSnapshot
	SaveDBAnalysis(snap *db.DBAnalysisSnapshot) error
}
