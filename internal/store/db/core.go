package db

import (
	"database/sql"
	"encoding/json"
	"fmt"

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
			clipboard TEXT
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
			dir_name TEXT
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
	var curPID, settingsJSON, pluginsJSON, clipboardJSON sql.NullString
	_ = db.QueryRow(`SELECT current_project_id, settings, plugins, clipboard FROM meta WHERE key='global'`).Scan(
		&curPID, &settingsJSON, &pluginsJSON, &clipboardJSON)
	if settingsJSON.Valid && settingsJSON.String != "" {
		_ = json.Unmarshal([]byte(settingsJSON.String), &data.Settings)
	}
	if pluginsJSON.Valid && pluginsJSON.String != "" {
		_ = json.Unmarshal([]byte(pluginsJSON.String), &data.Plugins)
	}
	if clipboardJSON.Valid && clipboardJSON.String != "" {
		_ = json.Unmarshal([]byte(clipboardJSON.String), &data.Clipboard)
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
	rows, err := db.Query(`SELECT id, project_id, api_id, dir_id, category, name, description, method, url, body_type, body, content_type, headers, query, form_items, assertions, enabled, created_at, dir_name FROM test_cases`)
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
		var pid, headers, query, formItems, assertionsJSON, createdAt, dirName sql.NullString
		var enabled int
		if err := rows.Scan(&c.ID, &pid, &c.ApiID, &c.DirID, &c.Category, &c.Name, &c.Description, &c.Method, &c.URL, &c.BodyType, &c.Body, &c.ContentType, &headers, &query, &formItems, &assertionsJSON, &enabled, &createdAt, &dirName); err != nil {
			return err
		}
		_ = scanJSON(headers.String, &c.Headers)
		_ = scanJSON(query.String, &c.Query)
		_ = scanJSON(formItems.String, &c.FormItems)
		_ = scanJSON(assertionsJSON.String, &c.Assertions)
		c.Enabled = enabled != 0
		c.CreatedAt = createdAt.String
		c.DirName = dirName.String
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
		"test_cases", "test_plans", "test_reports", "plugin_conns", "clip_items"}
	for _, t := range tables {
		if _, err := tx.Exec(fmt.Sprintf("DELETE FROM %s", t)); err != nil {
			return fmt.Errorf("清空表 %s 失败: %w", t, err)
		}
	}

	settingsJSON, _ := json.Marshal(data.Settings)
	pluginsJSON, _ := json.Marshal(data.Plugins)
	clipboardJSON, _ := json.Marshal(data.Clipboard)
	if _, err := tx.Exec(`INSERT INTO meta (key, current_project_id, settings, plugins, clipboard) VALUES ('global', ?, ?, ?, ?)`,
		data.CurrentProjectID, string(settingsJSON), string(pluginsJSON), string(clipboardJSON)); err != nil {
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
			if _, err := tx.Exec(`INSERT INTO test_cases (id, project_id, api_id, dir_id, category, name, description, method, url, body_type, body, content_type, headers, query, form_items, assertions, enabled, created_at, dir_name) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				c.ID, p.ID, c.ApiID, c.DirID, c.Category, c.Name, c.Description, c.Method, c.URL, c.BodyType, c.Body,
				c.ContentType, jsonCol(c.Headers), jsonCol(c.Query), jsonCol(c.FormItems), jsonCol(c.Assertions),
				boolToInt(c.Enabled), c.CreatedAt, c.DirName); err != nil {
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
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
