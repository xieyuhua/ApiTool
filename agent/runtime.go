package agent

// 单租户运行时：把 core/agent.Manager 需要的数据与事件能力，绑定到
// 本服务的存储（internal/store：SQLite 或 MySQL）与 SSE 广播上。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"apitool/internal/model"
	"apitool/internal/store"
	"apitool/internal/storeapi"
)

// Runtime 实现 core/agent.Host 与 core/agent.Emitter。
type Runtime struct {
	store *store.Store
	hub   *hub
	ver   string
}

// newRuntime 初始化数据目录与存储后端。
//
// internal/store 的后端选择规则（不可改，桌面端共用同一实现）：
//   - 同目录存在 storage.json 且 type=mysql → 用 MySQL；
//   - 否则固定使用 <dataDir>/apitool.db 的 SQLite。
//
// 因此这里在 -db mysql 时写 storage.json，其余情况直接交给 store。
func newRuntime(cfg *Config, h *hub, version string) (*Runtime, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}
	if cfg.DBType == "mysql" {
		if cfg.DSN == "" {
			return nil, fmt.Errorf("使用 MySQL 时必须提供 -dsn")
		}
		raw, _ := json.Marshal(map[string]string{"type": "mysql", "dsn": cfg.DSN})
		if err := os.WriteFile(filepath.Join(cfg.DataDir, "storage.json"), raw, 0o600); err != nil {
			return nil, fmt.Errorf("写入 storage.json 失败: %w", err)
		}
	}
	dataFile := filepath.Join(cfg.DataDir, "agent.json")
	st := store.New(dataFile, version, "")
	return &Runtime{store: st, hub: h, ver: version}, nil
}

// Store 返回存储句柄（core/agent.Host）。
func (r *Runtime) Store() storeapi.Store { return r.store }

// ReadData 返回完整应用数据（core/agent.Host）。
func (r *Runtime) ReadData() model.AppData { return r.store.GetData() }

// AppVersion 返回服务版本。
func (r *Runtime) AppVersion() string { return r.ver }

// Emit 把 Agent 运行事件推给所有 SSE 订阅者（core/agent.Emitter）。
func (r *Runtime) Emit(event string, data ...interface{}) {
	if r.hub != nil {
		r.hub.broadcast(event, data...)
	}
}

// Close 释放存储。
func (r *Runtime) Close() error { return r.store.Close() }
