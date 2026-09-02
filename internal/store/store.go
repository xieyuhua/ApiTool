// Package store 负责应用全部数据的磁盘读写与兼容迁移，独立于 Wails 运行时，
// 使业务逻辑模块（agent/plugins 等）无需依赖 App 即可读写数据。
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"apitool/internal/model"
	"apitool/internal/store/db"
)

// Store 数据存储器，支持 JSON 文件（兼容回退）与数据库（SQLite/MySQL）两种后端。
// 默认优先使用 SQLite（与 dataFile 同目录的 apitool.db）；若存在 storage.json 指定
// MySQL，则连接 MySQL。数据库不可用时自动回退到 JSON 文件，保证不崩溃。
type Store struct {
	dataFile   string
	version    string
	updateURL  string
	// mu 保护对后端（dbImpl）的并发访问：读用 RLock（GetData），写用 Lock（SaveData/initBackend）。
	// dbImpl.Read/Write 内部维护原生 map（AppData 含切片/映射），无锁并发会触发 concurrent map panic。
	mu     sync.RWMutex
	dbImpl db.DB // 数据库后端（nil 表示使用 JSON 文件回退）
}

// New 创建存储器。dataFile 为 JSON 数据文件路径（同时作为 SQLite DB 的同级依据）；
// version / updateURL 用于默认值补全与升级地址。
func New(dataFile, version, updateURL string) *Store {
	s := &Store{dataFile: dataFile, version: version, updateURL: updateURL}
	s.initBackend()
	return s
}

// storageConfig 描述可选的外部存储配置（storage.json，与 dataFile 同级）。
type storageConfig struct {
	Type string `json:"type"` // sqlite | mysql
	DSN  string `json:"dsn"`  // sqlite 为文件路径；mysql 为连接串
}

// initBackend 初始化后端：优先 MySQL（storage.json），否则 SQLite，失败回退 JSON。
func (s *Store) initBackend() {
	dir := filepath.Dir(s.dataFile)

	// 1) 若同级 storage.json 指定 mysql，则启用
	cfgPath := filepath.Join(dir, "storage.json")
	if b, err := os.ReadFile(cfgPath); err == nil {
		var cfg storageConfig
		if json.Unmarshal(b, &cfg) == nil && cfg.Type == "mysql" && cfg.DSN != "" {
			if impl, err := db.OpenMySQL(cfg.DSN, s.version, s.updateURL); err == nil {
				s.dbImpl = impl
				return
			}
		}
	}

	// 2) 默认 SQLite（与 dataFile 同目录的 apitool.db）
	dbPath := filepath.Join(dir, "apitool.db")
	impl, err := db.OpenSQLite(dbPath, s.version, s.updateURL)
	if err != nil {
		// 数据库不可用，回退 JSON 文件模式
		return
	}
	s.dbImpl = impl

	// 3) 首次初始化：若库为空且存在旧 data.json，则导入
	if s.isEmptySQLite() {
		if old, ok := loadLegacyJSON(s.dataFile, s.version, s.updateURL); ok {
			// 一并迁移旧 agent.json（字段语义 / 表结构 / 会话等）
			if old.Agent == "" {
				old.Agent = loadLegacyAgentJSON(filepath.Dir(s.dataFile))
			}
			_ = impl.Write(old)
		}
	}
}

// isEmptySQLite 判断库是否尚未写入任何项目（用于首次导入判断）。
func (s *Store) isEmptySQLite() bool {
	rows, err := s.dbImpl.Read()
	if err != nil {
		return true
	}
	return len(rows.Projects) == 0
}

// loadLegacyJSON 读取并修正旧版 JSON 数据文件（复用 JSON 回退逻辑）。
func loadLegacyJSON(dataFile, version, updateURL string) (model.AppData, bool) {
	tmp := &Store{dataFile: dataFile, version: version, updateURL: updateURL}
	data := tmp.readJSON(version, updateURL)
	if len(data.Projects) == 0 {
		return data, false
	}
	return data, true
}

// GetData 读取并修正全部数据（等价于旧 App.readData）。
// 使用读锁，允许多个读协程（capture、stress、cron 等）并发读取而互不阻塞。
func (s *Store) GetData() model.AppData {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.dbImpl != nil {
		data, err := s.dbImpl.Read()
		if err == nil {
			return data
		}
		// DB 读取失败则回退 JSON
	}
	return s.readJSON(s.version, s.updateURL)
}

// SaveData 保存全部数据（等价于旧 App.SaveData）。
func (s *Store) SaveData(data model.AppData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dbImpl != nil {
		// agent 数据由 SaveAgentRaw 独立管理，避免此处全量写覆盖其最新值。
		if raw, err := s.dbImpl.ReadAgent(); err == nil && raw != "" {
			data.Agent = raw
		}
		if err := s.dbImpl.Write(data); err == nil {
			return nil
		}
		// DB 写入失败则回退 JSON
	}
	return s.writeJSON(data)
}

// SaveAgentRaw 仅持久化 agent 数据（meta.agent 单列），不触碰其他表，
// 避免每次保存 agent 配置/会话/日志时全量重建整个库。回退模式下写旧 agent.json。
func (s *Store) SaveAgentRaw(raw string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dbImpl != nil {
		if err := s.dbImpl.UpdateAgent(raw); err == nil {
			return nil
		}
		// DB 写入失败则回退 JSON
	}
	return os.WriteFile(filepath.Join(filepath.Dir(s.dataFile), "agent.json"), []byte(raw), 0o644)
}

// LoadAgentRaw 读取 agent 原始 JSON 串（meta.agent 单列）。回退模式下读旧 agent.json。
func (s *Store) LoadAgentRaw() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dbImpl != nil {
		if raw, err := s.dbImpl.ReadAgent(); err == nil {
			return raw
		}
	}
	if b, err := os.ReadFile(filepath.Join(filepath.Dir(s.dataFile), "agent.json")); err == nil {
		return string(b)
	}
	return ""
}

// LoadSkills 读取全部技能（独立表 agent_skills）。回退模式下无外部表，返回空切片。
func (s *Store) LoadSkills() []db.AgentSkill {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dbImpl != nil {
		if sk, err := s.dbImpl.ReadSkills(); err == nil {
			return sk
		}
	}
	return []db.AgentSkill{}
}

// SaveSkills 覆盖保存技能列表（独立表 agent_skills）。
func (s *Store) SaveSkills(skills []db.AgentSkill) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dbImpl != nil {
		return s.dbImpl.SaveSkills(skills)
	}
	return nil
}

// LoadDBAnalysis 读取数据库连接分析数据（独立表）。
func (s *Store) LoadDBAnalysis() *db.DBAnalysisSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dbImpl != nil {
		if snap, err := s.dbImpl.ReadDBAnalysis(); err == nil {
			return snap
		}
	}
	return &db.DBAnalysisSnapshot{Schemas: map[string]string{}, Semantics: map[string]string{}, LastDB: map[string]string{}}
}

// SaveDBAnalysis 覆盖保存数据库连接分析数据（独立表）。
func (s *Store) SaveDBAnalysis(snap *db.DBAnalysisSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dbImpl != nil {
		return s.dbImpl.SaveDBAnalysis(snap)
	}
	return nil
}

// Path 返回数据文件绝对路径。
func (s *Store) Path() string { return s.dataFile }

// Dir 返回数据文件所在目录（图片等相对资源的基准目录）。
func (s *Store) Dir() string { return filepath.Dir(s.dataFile) }

// Close 释放底层数据库连接（若存在）。应用在退出时应调用，避免文件句柄残留。
func (s *Store) Close() error {
	if s.dbImpl != nil {
		return s.dbImpl.Close()
	}
	return nil
}

// defaultData 构造默认数据。version/updateURL 由调用方（App）注入，
// 避免 store 包反向依赖 main 包的常量。
func defaultData(version, updateURL string) model.AppData {
	data := model.AppData{
		Projects: []model.Project{
			{
				ID:        "default",
				Name:      "默认项目",
				Dirs:      []model.Directory{},
				Apis:      []model.ApiInfo{},
				Environments: []model.Environment{},
				UpdatedAt: time.Now().Format(time.RFC3339),
			},
		},
		CurrentProjectID: "default",
		Settings: model.Settings{
			AIBaseURL:  "https://api.openai.com/v1",
			AIModel:    "gpt-4o-mini",
			TimeoutSec: 30,
		},
		Plugins:   model.PluginsData{Connections: []model.PluginConn{}},
		Clipboard: model.ClipData{History: []model.ClipItem{}},
	}
	data.Settings.Version = version
	data.Settings.UpdateURL = updateURL
	data.Settings.Clipboard = model.ClipSettings{Monitor: true, MaxItems: 200}
	return data
}

// readJSON 从磁盘加载并反序列化全部数据（JSON 文件回退模式），处理旧版本兼容与默认值补全。
// 调用方负责加锁（GetData 已持锁；loadLegacyJSON 使用临时 Store 无需并发保护）。
func (s *Store) readJSON(version, updateURL string) model.AppData {
	data := defaultData(version, updateURL)
	b, err := os.ReadFile(s.dataFile)
	if err != nil {
		return data
	}
	_ = json.Unmarshal(b, &data)
	if data.Settings.TimeoutSec <= 0 {
		data.Settings.TimeoutSec = 30
	}
	// 兼容旧版本配置（无 version / updateURL 字段）
	if data.Settings.Version == "" {
		data.Settings.Version = version
	}
	if data.Settings.UpdateURL == "" {
		data.Settings.UpdateURL = updateURL
	}
	// 兼容旧版数据（顶层 dirs/apis/environments）：反序列化到新结构失败时，
	// 通过 migrateLegacy 额外解析旧结构并迁移进默认项目。
	if len(data.Projects) == 0 {
		proj := model.Project{ID: "default", Name: "默认项目", UpdatedAt: time.Now().Format(time.RFC3339)}
		if migrated := migrateLegacy(b, &proj); migrated {
			data.Projects = []model.Project{proj}
			data.CurrentProjectID = proj.ID
		}
	}
	if len(data.Projects) == 0 {
		data = defaultData(version, updateURL)
	}
	if data.CurrentProjectID == "" || !hasProject(data, data.CurrentProjectID) {
		data.CurrentProjectID = data.Projects[0].ID
	}
	// 兼容旧版：若主库/文件均无 agent 数据，则尝试从同目录旧 agent.json 迁移
	if data.Agent == "" {
		if legacy := loadLegacyAgentJSON(filepath.Dir(s.dataFile)); legacy != "" {
			data.Agent = legacy
		}
	}
	return data
}

// migrateLegacy 兼容旧版数据（顶层 dirs/apis/environments）
func migrateLegacy(b []byte, proj *model.Project) bool {
	var legacy struct {
		Dirs         []model.Directory   `json:"dirs"`
		Apis         []model.ApiInfo     `json:"apis"`
		Environments []model.Environment `json:"environments"`
		ActiveEnvID  string              `json:"activeEnvId"`
	}
	if err := json.Unmarshal(b, &legacy); err != nil {
		return false
	}
	if len(legacy.Dirs) == 0 && len(legacy.Apis) == 0 && len(legacy.Environments) == 0 {
		return false
	}
	proj.Dirs = legacy.Dirs
	proj.Apis = legacy.Apis
	proj.Environments = legacy.Environments
	proj.ActiveEnvID = legacy.ActiveEnvID
	return true
}

func hasProject(data model.AppData, id string) bool {
	for _, p := range data.Projects {
		if p.ID == id {
			return true
		}
	}
	return false
}

// loadLegacyAgentJSON 读取旧版 agent.json（独立文件）兼容到 AppData.Agent。
// 仅在数据库/主库为空、且代理数据尚未迁移时调用。
func loadLegacyAgentJSON(dataDir string) string {
	p := filepath.Join(dataDir, "agent.json")
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return string(b)
}

// writeJSON 保存全部数据到 JSON 文件（原子写：先写临时文件再 rename）。
func (s *Store) writeJSON(data model.AppData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.dataFile + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.dataFile)
}

// ActiveProjectIndex 返回当前项目在切片中的索引；无效时回退到第一个，仍无效返回 -1。
func ActiveProjectIndex(data model.AppData) int {
	if idx, ok := projectIndex(data, data.CurrentProjectID); ok {
		return idx
	}
	if len(data.Projects) > 0 {
		return 0
	}
	return -1
}

func projectIndex(data model.AppData, id string) (int, bool) {
	for i, p := range data.Projects {
		if p.ID == id {
			return i, true
		}
	}
	return 0, false
}
