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
)

// Store 数据存储器，持有数据文件路径与并发锁。
type Store struct {
	dataFile   string
	version    string
	updateURL  string
	mu         sync.Mutex
}

// New 创建存储器。dataFile 为数据文件绝对路径；
// version / updateURL 用于默认值补全与升级地址（避免 store 反向依赖 main 包常量）。
func New(dataFile, version, updateURL string) *Store {
	return &Store{dataFile: dataFile, version: version, updateURL: updateURL}
}

// GetData 读取并修正全部数据（等价于旧 App.readData）。
func (s *Store) GetData() model.AppData {
	return s.Read(s.version, s.updateURL)
}

// SaveData 保存全部数据（等价于旧 App.SaveData）。
func (s *Store) SaveData(data model.AppData) error {
	return s.Write(data)
}

// Path 返回数据文件绝对路径。
func (s *Store) Path() string { return s.dataFile }

// Dir 返回数据文件所在目录（图片等相对资源的基准目录）。
func (s *Store) Dir() string { return filepath.Dir(s.dataFile) }

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

// Read 从磁盘加载并反序列化全部数据，处理旧版本兼容与默认值补全。
// 返回经过迁移/修正后的最新结构，调用方无需再处理兼容逻辑。
func (s *Store) Read(version, updateURL string) model.AppData {
	s.mu.Lock()
	defer s.mu.Unlock()
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

// Write 保存全部数据（原子写：先写临时文件再 rename）。
func (s *Store) Write(data model.AppData) error {
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
