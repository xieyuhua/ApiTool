package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"apitool/internal/model"
)

// schemaDDL 返回建表语句。
// 统一使用兼容 SQLite 与 MySQL 的语法：主键为 TEXT 类型（ID 由应用生成 UUID），
// 避免依赖数据库自增方言差异，因此两种驱动共用同一份 DDL。
func schemaDDL() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS meta (
			key TEXT PRIMARY KEY,
			current_project_id TEXT,
			settings TEXT,
			plugins TEXT,
			clipboard TEXT,
			agent TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS projects (
			id TEXT PRIMARY KEY,
			name TEXT,
			active_env_id TEXT,
			common TEXT,
			updated_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS directories (
			id TEXT PRIMARY KEY,
			project_id TEXT,
			parent_id TEXT,
			name TEXT,
			sort INT
		)`,
		`CREATE TABLE IF NOT EXISTS apis (
			id TEXT PRIMARY KEY,
			project_id TEXT,
			dir_id TEXT,
			name TEXT,
			method TEXT,
			url TEXT,
			description TEXT,
			content_type TEXT,
			body_type TEXT,
			body TEXT,
			form_items TEXT,
			headers TEXT,
			query TEXT,
			req_fields TEXT,
			resp_fields TEXT,
			pre_script TEXT,
			post_script TEXT,
			last_response TEXT,
			updated_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS environments (
			id TEXT PRIMARY KEY,
			project_id TEXT,
			name TEXT,
			vars TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS test_cases (
			id TEXT PRIMARY KEY,
			project_id TEXT,
			api_id TEXT,
			dir_id TEXT,
			category TEXT,
			name TEXT,
			description TEXT,
			method TEXT,
			url TEXT,
			body_type TEXT,
			body TEXT,
			content_type TEXT,
			headers TEXT,
			query TEXT,
			form_items TEXT,
			assertions TEXT,
			enabled INT,
			created_at TEXT,
			dir_name TEXT,
			source TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS test_plans (
			id TEXT PRIMARY KEY,
			project_id TEXT,
			name TEXT,
			case_ids TEXT,
			env_id TEXT,
			concurrency INT,
			created_at TEXT,
			updated_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS test_reports (
			id TEXT PRIMARY KEY,
			project_id TEXT,
			plan_id TEXT,
			plan_name TEXT,
			created_at TEXT,
			total INT,
			passed INT,
			failed INT,
			duration_ms BIGINT,
			results TEXT,
			summary TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS plugin_conns (
			id TEXT PRIMARY KEY,
			category TEXT,
			name TEXT,
			host TEXT,
			port INT,
			username TEXT,
			password TEXT,
			database_name TEXT,
			db_type TEXT,
			db_index INT,
			encoding TEXT,
			use_tls INT,
			remark TEXT,
			updated_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS clip_items (
			id TEXT PRIMARY KEY,
			type TEXT,
			text TEXT,
			image_path TEXT,
			width INT,
			height INT,
			time TEXT,
			timestamp BIGINT
		)`,
		`CREATE TABLE IF NOT EXISTS agent_skills (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT,
			prompt TEXT,
			enabled INT NOT NULL DEFAULT 1,
			builtin INT NOT NULL DEFAULT 0,
			updated_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS db_schemas (
			id TEXT PRIMARY KEY,
			conn_id TEXT NOT NULL,
			database TEXT NOT NULL,
			table_name TEXT NOT NULL,
			rows INT NOT NULL DEFAULT 0,
			columns TEXT,
			updated_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS db_semantics (
			key TEXT PRIMARY KEY,
			value TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS db_last_db (
			conn_id TEXT PRIMARY KEY,
			database TEXT
		)`,
	}
}

func initSchema(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if err := execTx(tx, schemaDDL()...); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("建表失败: %w", err)
	}
	// 兼容旧库：meta 表可能已存在但缺少 agent 列，追加该列（已存在则忽略错误）。
	_, _ = tx.Exec(`ALTER TABLE meta ADD COLUMN agent TEXT`)
	// 兼容旧库：test_cases 表可能缺少 source 列（用例来源），追加该列（已存在则忽略错误）。
	_, _ = tx.Exec(`ALTER TABLE test_cases ADD COLUMN source TEXT`)
	return tx.Commit()
}

// readAll 从数据库读取全部应用数据。
func readAll(db *sql.DB, version, updateURL string) (model.AppData, error) {
	data := model.AppData{
		Settings: model.Settings{
			AIBaseURL:  "https://api.openai.com/v1",
			AIModel:    "gpt-4o-mini",
			TimeoutSec: 30,
			Clipboard:  model.ClipSettings{Monitor: true, MaxItems: 200},
		},
	}

	// meta
	var curPID, settingsJSON, pluginsJSON, clipboardJSON, agentJSON sql.NullString
	_ = db.QueryRow(`SELECT current_project_id, settings, plugins, clipboard, agent FROM meta WHERE key='global'`).Scan(
		&curPID, &settingsJSON, &pluginsJSON, &clipboardJSON, &agentJSON)
	if settingsJSON.Valid && settingsJSON.String != "" {
		_ = json.Unmarshal([]byte(settingsJSON.String), &data.Settings)
	}
	if pluginsJSON.Valid && pluginsJSON.String != "" {
		_ = json.Unmarshal([]byte(pluginsJSON.String), &data.Plugins)
	}
	if clipboardJSON.Valid && clipboardJSON.String != "" {
		_ = json.Unmarshal([]byte(clipboardJSON.String), &data.Clipboard)
	}
	if agentJSON.Valid && agentJSON.String != "" {
		data.Agent = agentJSON.String
	}
	if data.Settings.TimeoutSec <= 0 {
		data.Settings.TimeoutSec = 30
	}
	if data.Settings.Version == "" {
		data.Settings.Version = version
	}
	if data.Settings.UpdateURL == "" {
		data.Settings.UpdateURL = updateURL
	}

	// projects
	projRows, err := db.Query(`SELECT id, name, active_env_id, common, updated_at FROM projects ORDER BY updated_at`)
	if err != nil {
		return data, err
	}
	for projRows.Next() {
		var p model.Project
		var commonJSON, updatedAt sql.NullString
		if err := projRows.Scan(&p.ID, &p.Name, &p.ActiveEnvID, &commonJSON, &updatedAt); err != nil {
			_ = projRows.Close()
			return data, err
		}
		_ = scanJSON(commonJSON.String, &p.Common)
		p.UpdatedAt = updatedAt.String
		data.Projects = append(data.Projects, p)
	}
	if err := projRows.Err(); err != nil {
		_ = projRows.Close()
		return data, err
	}
	_ = projRows.Close()

	// 关联子表
	if err := readDirectories(db, &data); err != nil {
		return data, err
	}
	if err := readApis(db, &data); err != nil {
		return data, err
	}
	if err := readEnvironments(db, &data); err != nil {
		return data, err
	}
	if err := readTestCases(db, &data); err != nil {
		return data, err
	}
	if err := readTestPlans(db, &data); err != nil {
		return data, err
	}
	if err := readTestReports(db, &data); err != nil {
		return data, err
	}

	data.CurrentProjectID = curPID.String
	if data.CurrentProjectID == "" && len(data.Projects) > 0 {
		data.CurrentProjectID = data.Projects[0].ID
	}
	return data, nil
}

func readDirectories(db *sql.DB, data *model.AppData) error {
	rows, err := db.Query(`SELECT id, project_id, parent_id, name, sort FROM directories`)
	if err != nil {
		return err
	}
	defer rows.Close()
	byProj := map[string]*model.Project{}
	for i := range data.Projects {
		byProj[data.Projects[i].ID] = &data.Projects[i]
	}
	for rows.Next() {
		var d model.Directory
		var pid sql.NullString
		if err := rows.Scan(&d.ID, &pid, &d.ParentID, &d.Name, &d.Sort); err != nil {
			return err
		}
		if p, ok := byProj[pid.String]; ok {
			p.Dirs = append(p.Dirs, d)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return nil
}

func readApis(db *sql.DB, data *model.AppData) error {
	rows, err := db.Query(`SELECT id, project_id, dir_id, name, method, url, description, content_type, body_type, body, form_items, headers, query, req_fields, resp_fields, pre_script, post_script, last_response, updated_at FROM apis`)
	if err != nil {
		return err
	}
	defer rows.Close()
	byProj := map[string]*model.Project{}
	for i := range data.Projects {
		byProj[data.Projects[i].ID] = &data.Projects[i]
	}
	for rows.Next() {
		var a model.ApiInfo
		var pid, formItems, headers, query, reqFields, respFields, lastResp, updatedAt sql.NullString
		if err := rows.Scan(&a.ID, &pid, &a.DirID, &a.Name, &a.Method, &a.URL, &a.Description, &a.ContentType, &a.BodyType, &a.Body, &formItems, &headers, &query, &reqFields, &respFields, &a.PreScript, &a.PostScript, &lastResp, &updatedAt); err != nil {
			return err
		}
		_ = scanJSON(formItems.String, &a.FormItems)
		_ = scanJSON(headers.String, &a.Headers)
		_ = scanJSON(query.String, &a.Query)
		_ = scanJSON(reqFields.String, &a.ReqFields)
		_ = scanJSON(respFields.String, &a.RespFields)
		if lastResp.String != "" {
			a.LastResponse = &model.ResponseData{}
			_ = scanJSON(lastResp.String, a.LastResponse)
		}
		a.UpdatedAt = updatedAt.String
		if p, ok := byProj[pid.String]; ok {
			p.Apis = append(p.Apis, a)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return nil
}

func readEnvironments(db *sql.DB, data *model.AppData) error {
	rows, err := db.Query(`SELECT id, project_id, name, vars FROM environments`)
	if err != nil {
		return err
	}
	defer rows.Close()
	byProj := map[string]*model.Project{}
	for i := range data.Projects {
		byProj[data.Projects[i].ID] = &data.Projects[i]
	}
	for rows.Next() {
		var e model.Environment
		var pid, varsJSON sql.NullString
		if err := rows.Scan(&e.ID, &pid, &e.Name, &varsJSON); err != nil {
			return err
		}
		_ = scanJSON(varsJSON.String, &e.Vars)
		if p, ok := byProj[pid.String]; ok {
			p.Environments = append(p.Environments, e)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return nil
}

func readTestCases(db *sql.DB, data *model.AppData) error {
	rows, err := db.Query(`SELECT id, project_id, api_id, dir_id, category, name, description, method, url, body_type, body, content_type, headers, query, form_items, assertions, enabled, created_at, dir_name, source FROM test_cases`)
	if err != nil {
		return err
	}
	defer rows.Close()
	byProj := map[string]*model.Project{}
	for i := range data.Projects {
		byProj[data.Projects[i].ID] = &data.Projects[i]
	}
	for rows.Next() {
		var c model.TestCase
		var pid, headers, query, formItems, assertionsJSON, createdAt, dirName, source sql.NullString
		var enabled int
		if err := rows.Scan(&c.ID, &pid, &c.ApiID, &c.DirID, &c.Category, &c.Name, &c.Description, &c.Method, &c.URL, &c.BodyType, &c.Body, &c.ContentType, &headers, &query, &formItems, &assertionsJSON, &enabled, &createdAt, &dirName, &source); err != nil {
			return err
		}
		_ = scanJSON(headers.String, &c.Headers)
		_ = scanJSON(query.String, &c.Query)
		_ = scanJSON(formItems.String, &c.FormItems)
		_ = scanJSON(assertionsJSON.String, &c.Assertions)
		c.Enabled = enabled != 0
		c.CreatedAt = createdAt.String
		c.DirName = dirName.String
		c.Source = source.String
		if p, ok := byProj[pid.String]; ok {
			p.TestCases = append(p.TestCases, c)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return nil
}

func readTestPlans(db *sql.DB, data *model.AppData) error {
	rows, err := db.Query(`SELECT id, project_id, name, case_ids, env_id, concurrency, created_at, updated_at FROM test_plans`)
	if err != nil {
		return err
	}
	defer rows.Close()
	byProj := map[string]*model.Project{}
	for i := range data.Projects {
		byProj[data.Projects[i].ID] = &data.Projects[i]
	}
	for rows.Next() {
		var pl model.TestPlan
		var pid, caseIDsJSON, envID, createdAt, updatedAt sql.NullString
		if err := rows.Scan(&pl.ID, &pid, &pl.Name, &caseIDsJSON, &envID, &pl.Concurrency, &createdAt, &updatedAt); err != nil {
			return err
		}
		_ = scanJSON(caseIDsJSON.String, &pl.CaseIDs)
		pl.EnvID = envID.String
		pl.CreatedAt = createdAt.String
		pl.UpdatedAt = updatedAt.String
		if p, ok := byProj[pid.String]; ok {
			p.TestPlans = append(p.TestPlans, pl)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return nil
}

func readTestReports(db *sql.DB, data *model.AppData) error {
	rows, err := db.Query(`SELECT id, project_id, plan_id, plan_name, created_at, total, passed, failed, duration_ms, results, summary FROM test_reports`)
	if err != nil {
		return err
	}
	defer rows.Close()
	byProj := map[string]*model.Project{}
	for i := range data.Projects {
		byProj[data.Projects[i].ID] = &data.Projects[i]
	}
	for rows.Next() {
		var r model.TestReport
		var pid, resultsJSON, planName, createdAt sql.NullString
		if err := rows.Scan(&r.ID, &pid, &r.PlanID, &planName, &createdAt, &r.Total, &r.Passed, &r.Failed, &r.DurationMs, &resultsJSON, &r.Summary); err != nil {
			return err
		}
		_ = scanJSON(resultsJSON.String, &r.Results)
		r.PlanName = planName.String
		r.CreatedAt = createdAt.String
		if p, ok := byProj[pid.String]; ok {
			p.TestReports = append(p.TestReports, r)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return nil
}

// writeAll 在事务中全量覆盖写入（清空各表后重新插入）。
func writeAll(tx *sql.Tx, data model.AppData) error {
	tables := []string{"meta", "projects", "directories", "apis", "environments",
		"test_cases", "test_plans", "test_reports", "plugin_conns", "clip_items",
		"agent_skills", "db_schemas", "db_semantics", "db_last_db"}
	for _, t := range tables {
		if _, err := tx.Exec(fmt.Sprintf("DELETE FROM %s", t)); err != nil {
			return fmt.Errorf("清空表 %s 失败: %w", t, err)
		}
	}

	settingsJSON, _ := json.Marshal(data.Settings)
	pluginsJSON, _ := json.Marshal(data.Plugins)
	clipboardJSON, _ := json.Marshal(data.Clipboard)
	if _, err := tx.Exec(`INSERT INTO meta (key, current_project_id, settings, plugins, clipboard, agent) VALUES ('global', ?, ?, ?, ?, ?)`,
		data.CurrentProjectID, string(settingsJSON), string(pluginsJSON), string(clipboardJSON), data.Agent); err != nil {
		return err
	}

	for _, p := range data.Projects {
		if _, err := tx.Exec(`INSERT INTO projects (id, name, active_env_id, common, updated_at) VALUES (?,?,?,?,?)`,
			p.ID, p.Name, p.ActiveEnvID, jsonCol(p.Common), p.UpdatedAt); err != nil {
			return err
		}
		for _, d := range p.Dirs {
			if _, err := tx.Exec(`INSERT INTO directories (id, project_id, parent_id, name, sort) VALUES (?,?,?,?,?)`,
				d.ID, p.ID, d.ParentID, d.Name, d.Sort); err != nil {
				return err
			}
		}
		for _, a := range p.Apis {
			lastResp := ""
			if a.LastResponse != nil {
				b, _ := json.Marshal(a.LastResponse)
				lastResp = string(b)
			}
			if _, err := tx.Exec(`INSERT INTO apis (id, project_id, dir_id, name, method, url, description, content_type, body_type, body, form_items, headers, query, req_fields, resp_fields, pre_script, post_script, last_response, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				a.ID, p.ID, a.DirID, a.Name, a.Method, a.URL, a.Description, a.ContentType, a.BodyType, a.Body,
				jsonCol(a.FormItems), jsonCol(a.Headers), jsonCol(a.Query), jsonCol(a.ReqFields), jsonCol(a.RespFields),
				a.PreScript, a.PostScript, lastResp, a.UpdatedAt); err != nil {
				return err
			}
		}
		for _, e := range p.Environments {
			if _, err := tx.Exec(`INSERT INTO environments (id, project_id, name, vars) VALUES (?,?,?,?)`,
				e.ID, p.ID, e.Name, jsonCol(e.Vars)); err != nil {
				return err
			}
		}
		for _, c := range p.TestCases {
			if _, err := tx.Exec(`INSERT INTO test_cases (id, project_id, api_id, dir_id, category, name, description, method, url, body_type, body, content_type, headers, query, form_items, assertions, enabled, created_at, dir_name, source) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				c.ID, p.ID, c.ApiID, c.DirID, c.Category, c.Name, c.Description, c.Method, c.URL, c.BodyType, c.Body,
				c.ContentType, jsonCol(c.Headers), jsonCol(c.Query), jsonCol(c.FormItems), jsonCol(c.Assertions),
				boolToInt(c.Enabled), c.CreatedAt, c.DirName, c.Source); err != nil {
				return err
			}
		}
		for _, pl := range p.TestPlans {
			if _, err := tx.Exec(`INSERT INTO test_plans (id, project_id, name, case_ids, env_id, concurrency, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?)`,
				pl.ID, p.ID, pl.Name, jsonCol(pl.CaseIDs), pl.EnvID, pl.Concurrency, pl.CreatedAt, pl.UpdatedAt); err != nil {
				return err
			}
		}
		for _, r := range p.TestReports {
			if _, err := tx.Exec(`INSERT INTO test_reports (id, project_id, plan_id, plan_name, created_at, total, passed, failed, duration_ms, results, summary) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
				r.ID, p.ID, r.PlanID, r.PlanName, r.CreatedAt, r.Total, r.Passed, r.Failed, r.DurationMs, jsonCol(r.Results), r.Summary); err != nil {
				return err
			}
		}
	}

	for _, conn := range data.Plugins.Connections {
		if _, err := tx.Exec(`INSERT INTO plugin_conns (id, category, name, host, port, username, password, database_name, db_type, db_index, encoding, use_tls, remark, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			conn.ID, conn.Category, conn.Name, conn.Host, conn.Port, conn.Username, conn.Password, conn.Database, conn.DbType, conn.DbIndex, conn.Encoding, boolToInt(conn.UseTLS), conn.Remark, conn.UpdatedAt); err != nil {
			return err
		}
	}
	for _, it := range data.Clipboard.History {
		if _, err := tx.Exec(`INSERT INTO clip_items (id, type, text, image_path, width, height, time, timestamp) VALUES (?,?,?,?,?,?,?,?)`,
			it.ID, it.Type, it.Text, it.ImagePath, it.Width, it.Height, it.Time, it.Timestamp); err != nil {
			return err
		}
	}
	// 技能与数据库连接分析数据独立存储，全量备份时从 meta.agent 解析出来一并写入对应表。
	if data.Agent != "" {
		var ad agentDataSkills
		if _ = json.Unmarshal([]byte(data.Agent), &ad); ad.Skills != nil {
			if err := saveSkills(tx, ad.Skills); err != nil {
				return err
			}
		}
		if ad.Config.DBSchemas != nil || ad.Config.DBSemantics != nil || ad.Config.DBLastDB != nil {
			schemas := map[string]string{}
			for k, v := range ad.Config.DBSchemas {
				schemas[k] = string(v)
			}
			snap := &DBAnalysisSnapshot{
				Schemas:   schemas,
				Semantics: ad.Config.DBSemantics,
				LastDB:    ad.Config.DBLastDB,
			}
			if err := saveDBAnalysis(tx, snap); err != nil {
				return err
			}
		}
	}
	return nil
}

// agentDataSkills 仅用于从 meta.agent JSON 中提取 skills 与数据库连接分析字段（全量备份场景）。
type agentDataSkills struct {
	Skills []AgentSkill `json:"skills"`
	Config struct {
		DBSchemas   map[string]json.RawMessage `json:"dbSchemas"`
		DBSemantics map[string]string          `json:"dbSemantics"`
		DBLastDB    map[string]string          `json:"dbLastDB"`
	} `json:"config"`
}

// withTx 开启事务并执行 fn，fn 内出错则回滚，否则提交。
func withTx(db *sql.DB, fn func(tx *sql.Tx) error) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// readAgent 读取 meta.agent 单列（agent 全部数据）。
func readAgent(db *sql.DB) (string, error) {
	var raw sql.NullString
	err := db.QueryRow(`SELECT agent FROM meta WHERE key='global'`).Scan(&raw)
	if err != nil {
		return "", err
	}
	return raw.String, nil
}

// updateAgent 仅更新 meta.agent 单列，避免全量重写整个库。
// 采用"先 UPDATE，若未命中则 INSERT"的跨方言兼容写法（SQLite/MySQL 通用）。
func updateAgent(db *sql.DB, raw string) error {
	res, err := db.Exec(`UPDATE meta SET agent=? WHERE key='global'`, raw)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	_, err = db.Exec(`INSERT INTO meta (key, agent) VALUES ('global', ?)`, raw)
	return err
}

// readSkills 读取全部技能（独立表 agent_skills）。
func readSkills(db *sql.DB) ([]AgentSkill, error) {
	rows, err := db.Query(`SELECT id, name, description, prompt, enabled, builtin, updated_at FROM agent_skills ORDER BY updated_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgentSkill
	for rows.Next() {
		var s AgentSkill
		var desc, prompt, updatedAt sql.NullString
		var enabled, builtin int
		if err := rows.Scan(&s.ID, &s.Name, &desc, &prompt, &enabled, &builtin, &updatedAt); err != nil {
			return nil, err
		}
		s.Description = desc.String
		s.Prompt = prompt.String
		s.Enabled = enabled != 0
		s.Builtin = builtin != 0
		s.UpdatedAt = updatedAt.String
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []AgentSkill{}
	}
	return out, nil
}

// saveSkills 覆盖保存技能列表（独立表 agent_skills）。
// 在事务内先清空再全量插入，桌面场景数据量小，跨方言通用。
func saveSkills(tx *sql.Tx, skills []AgentSkill) error {
	if _, err := tx.Exec(`DELETE FROM agent_skills`); err != nil {
		return fmt.Errorf("清空 agent_skills 失败: %w", err)
	}
	for _, s := range skills {
		if _, err := tx.Exec(`INSERT INTO agent_skills (id, name, description, prompt, enabled, builtin, updated_at) VALUES (?,?,?,?,?,?,?)`,
			s.ID, s.Name, s.Description, s.Prompt, boolToInt(s.Enabled), boolToInt(s.Builtin), s.UpdatedAt); err != nil {
			return err
		}
	}
	return nil
}

// DBAnalysisSnapshot 数据库连接分析数据的快照，跨包传递用通用类型避免循环依赖。
// Schemas 的 value 为 DBSyncedTable 的 JSON；Semantics/LastDB 为 map[string]string。
type DBAnalysisSnapshot struct {
	Schemas   map[string]string // key=connId|database|table(小写) -> DBSyncedTable JSON
	Semantics map[string]string // key=connId|database|table|column(小写) -> 语义文本
	LastDB    map[string]string // connId -> database
}

// readDBAnalysis 读取数据库连接分析数据（独立表）。
func readDBAnalysis(db *sql.DB) (*DBAnalysisSnapshot, error) {
	snap := &DBAnalysisSnapshot{Schemas: map[string]string{}, Semantics: map[string]string{}, LastDB: map[string]string{}}
	rows, err := db.Query(`SELECT id, columns, rows FROM db_schemas`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, cols sql.NullString
		var rowsN int
		if err := rows.Scan(&id, &cols, &rowsN); err != nil {
			rows.Close()
			return nil, err
		}
		snap.Schemas[id.String] = cols.String // 列 JSON 已含 conn/database/table/rows 冗余，便于回写
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	srows, err := db.Query(`SELECT key, value FROM db_semantics`)
	if err != nil {
		return nil, err
	}
	for srows.Next() {
		var k, v string
		if err := srows.Scan(&k, &v); err != nil {
			srows.Close()
			return nil, err
		}
		snap.Semantics[k] = v
	}
	srows.Close()
	if err := srows.Err(); err != nil {
		return nil, err
	}
	drows, err := db.Query(`SELECT conn_id, database FROM db_last_db`)
	if err != nil {
		return nil, err
	}
	for drows.Next() {
		var cid, dbName string
		if err := drows.Scan(&cid, &dbName); err != nil {
			drows.Close()
			return nil, err
		}
		snap.LastDB[cid] = dbName
	}
	drows.Close()
	if err := drows.Err(); err != nil {
		return nil, err
	}
	return snap, nil
}

// saveDBAnalysis 覆盖保存数据库连接分析数据（独立表）。
func saveDBAnalysis(tx *sql.Tx, snap *DBAnalysisSnapshot) error {
	if _, err := tx.Exec(`DELETE FROM db_schemas`); err != nil {
		return fmt.Errorf("清空 db_schemas 失败: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM db_semantics`); err != nil {
		return fmt.Errorf("清空 db_semantics 失败: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM db_last_db`); err != nil {
		return fmt.Errorf("清空 db_last_db 失败: %w", err)
	}
	for k, colsJSON := range snap.Schemas {
		// 从 JSON 中提取元信息；列 JSON 形如 {"connId":..,"database":..,"table":..,"rows":..,"columns":[..]}
		var meta struct {
			ConnID   string `json:"connId"`
			Database string `json:"database"`
			Table    string `json:"table"`
			Rows     int    `json:"rows"`
		}
		_ = json.Unmarshal([]byte(colsJSON), &meta)
		if _, err := tx.Exec(`INSERT INTO db_schemas (id, conn_id, database, table_name, rows, columns, updated_at) VALUES (?,?,?,?,?,?,?)`,
			k, meta.ConnID, meta.Database, meta.Table, meta.Rows, colsJSON, time.Now().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	for k, v := range snap.Semantics {
		if _, err := tx.Exec(`INSERT INTO db_semantics (key, value) VALUES (?,?)`, k, v); err != nil {
			return err
		}
	}
	for cid, dbName := range snap.LastDB {
		if _, err := tx.Exec(`INSERT INTO db_last_db (conn_id, database) VALUES (?,?)`, cid, dbName); err != nil {
			return err
		}
	}
	return nil
}
