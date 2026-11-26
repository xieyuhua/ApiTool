package agent

// 基础层：数据读写。
//
// AgentData 是「一份 JSON 大对象 + 若干独立表」的混合模型：
//   - 主体（配置/会话/日志/用量）落在 meta.agent 单列；
//   - 技能（agent_skills）与数据库分析快照（db_schemas）是独立表。
// ReadAgentData 负责把两者合并成一份完整视图（含技能迁移与孤儿快照清理），
// WriteAgentData 负责拆分写回。

import (
	"encoding/json"
	"strings"
	"time"

	"apitool/internal/model"
	"apitool/internal/store/db"
)

func defaultAgentData() AgentData {
	return AgentData{
		Config: AgentConfig{
			SystemPrompt:  "你是一个通用智能助手，会严格按照用户给定的角色与任务要求行事（例如数据分析师、运维工程师、客服、开发等），若用户未明确指定角色，则以「贴心、专业、严谨」的通用助手身份回答。\n\n通用准则：\n1. 优先用工具获取真实信息，不要臆测；信息不足时向用户确认，而非编造。\n2. 涉及数据库时：需要结构用 db_schema，需要采样数据用 db_query（仅只读 SELECT，禁止写操作）；当返回结果包含用户维护的「语义」时，优先采用这些语义理解业务含义。\n3. 未显式指定数据库连接时，使用当前已激活的连接；连接未配置或报错时，明确提示用户去「插件 / 数据库连接」配置。\n4. 回答使用简体中文，结论简洁、可验证；执行任何操作或给出 SQL/命令前先说明用途与风险。\n\n【数据分析请求强制流程】当用户要求「分析 / 统计 / 查询 / 看板 / 报表」任何与具体数据相关的任务时，必须且只能基于工具返回的真实数据作答，严禁凭空编造数字或结论。标准流程：\n第 1 步：先用 db_schema 了解相关表的结构、字段含义与可用时间字段；\n第 2 步：用 db_query 编写并执行只读 SQL 取得所需数据（如「最近一周销售」需按时间范围过滤、按维度聚合）；\n第 3 步：基于返回数据做统计与解读，必要时给出趋势/对比/异常说明；\n若已加载了相关的技能（Skill），优先按该技能定义的分析模板执行。每一步都应让用户看到你正在调用哪个工具。",
			Mode:          "react",
			MaxLoops:      6,
			ContextLimit:  20,
			ShowThinking:  true,
			EnablePolish:  false,
			EnableChart:   true,
			Temperature:   0.3,
			Tools:         defaultToolFlags(),
			MaxToolOutput: 4000,
			MaxFileRead:   200000,
			MaxTokens:     8000,
		},
		Skills:  defaultSkills(),
		Servers: []MCPServer{},
		Users:   []AgentUser{},
		Sessions: []AgentSession{
			{ID: "default", Title: "默认会话", CreatedAt: time.Now().Format(time.RFC3339), UpdatedAt: time.Now().Format(time.RFC3339)},
		},
		ActiveSession: "default",
		Messages:      []AgentMsg{},
		Logs:          []AgentLog{},
	}
}

// ReadAgentData 从应用主库（meta.agent 列）读取并补全 Agent 数据。
// 主库不可用时由 Store 回退到旧 agent.json 文件（兼容迁移）。调用方需持有 m.mu。
func (m *Manager) ReadAgentData() AgentData {
	data := defaultAgentData()
	raw := m.host.Store().LoadAgentRaw()
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &data)
	}
	if data.Config.MaxLoops <= 0 {
		data.Config.MaxLoops = 6
	}
	if data.Config.ContextLimit <= 0 {
		data.Config.ContextLimit = 20
	}
	if data.Config.Mode == "" {
		data.Config.Mode = "react"
	}
	// 技能独立存储于 agent_skills 表：优先从独立表读取。
	// 先保留 meta.agent 中解析出的旧 skills（用于首次迁移判断），不可提前清空。
	metaSkills := data.Skills
	if metaSkills == nil {
		metaSkills = []AgentSkill{}
	}
	dbSkills := m.host.Store().LoadSkills()
	if len(dbSkills) == 0 && len(metaSkills) > 0 {
		// 独立表为空但旧 meta.agent JSON 中含有 skills：首次迁移，写入独立表。
		dbSkills = metaSkills
		_ = m.host.Store().SaveSkills(dbSkills)
	}
	// 合并预置技能（不覆盖用户已有的同 ID 技能），并持久化新增的预置技能。
	merged := mergeDefaultSkills(dbSkills)
	if len(merged) != len(dbSkills) {
		_ = m.host.Store().SaveSkills(merged)
	}
	data.Skills = merged
	if data.Skills == nil {
		data.Skills = []AgentSkill{}
	}
	// 数据库连接分析数据（表结构同步 / 字段语义维护）独立存储于
	// db_schemas / db_semantics / db_last_db 表，优先从独立表读取。
	snap := m.host.Store().LoadDBAnalysis()
	oldSchemas := data.Config.DBSchemas
	oldSem := data.Config.DBSemantics
	oldLast := data.Config.DBLastDB
	if len(snap.Schemas) == 0 && len(oldSchemas) > 0 {
		// 独立表为空但旧 meta.agent JSON 中含有：首次迁移，写入独立表。
		snap.Schemas = schemasToJSONMap(oldSchemas)
		snap.Semantics = oldSem
		snap.LastDB = oldLast
		_ = m.host.Store().SaveDBAnalysis(snap)
	}
	// 仅当独立表确有数据时才覆盖，避免「独立表空 / 回退模式」时用空值清掉 meta.agent 中已同步的数据。
	if len(snap.Schemas) > 0 {
		data.Config.DBSchemas = schemasFromJSONMap(snap.Schemas)
	}
	if len(snap.Semantics) > 0 {
		data.Config.DBSemantics = snap.Semantics
	}
	if len(snap.LastDB) > 0 {
		data.Config.DBLastDB = snap.LastDB
	}
	if data.Config.DBSchemas == nil {
		data.Config.DBSchemas = map[string]DBSyncedTable{}
	}
	if data.Config.DBSemantics == nil {
		data.Config.DBSemantics = map[string]string{}
	}
	if data.Config.DBLastDB == nil {
		data.Config.DBLastDB = map[string]string{}
	}
	// 清理「连接已删除但快照仍残留」的脏数据，并校准分析连接。
	//
	// 表结构快照以 connId|database|table 为键存放在独立表里，删除连接时不会级联清除。
	// 于是：已删除连接的快照会继续出现在 Agent 提示词中，模型据此填出一个
	// 根本不存在的 connId，调用时才发现失败；更麻烦的是新连接的 id 与旧 id
	// 往往只差几位数字（如 db_1788254096769 与 db_1788254178513），
	// 极易被误认为同一个连接，出现「我选的是 MySQL，报错却来自 Oracle」这类困惑。
	data.Config = m.pruneOrphanDBAnalysis(data.Config, m.host.ReadData().Plugins.Connections)
	if data.Servers == nil {
		data.Servers = []MCPServer{}
	}
	if data.Users == nil {
		data.Users = []AgentUser{}
	}
	if data.Logs == nil {
		data.Logs = []AgentLog{}
	}
	if data.Sessions == nil {
		// 兼容旧版本：把顶层 Messages 迁移为默认会话
		data.Sessions = []AgentSession{
			{ID: "default", Title: "默认会话", CreatedAt: time.Now().Format(time.RFC3339), UpdatedAt: time.Now().Format(time.RFC3339), Messages: data.Messages},
		}
		data.ActiveSession = "default"
	}
	if data.ActiveSession == "" && len(data.Sessions) > 0 {
		data.ActiveSession = data.Sessions[0].ID
	}
	// 确保当前会话存在
	if !sessionExists(data, data.ActiveSession) && len(data.Sessions) > 0 {
		data.ActiveSession = data.Sessions[0].ID
	}
	// 内置工具开关迁移与兜底
	data.Config.Tools = migrateToolFlags(data.Config.Tools)
	for i := range data.Sessions {
		if data.Sessions[i].Messages == nil {
			data.Sessions[i].Messages = []AgentMsg{}
		}
	}
	return data
}

func sessionExists(d AgentData, id string) bool {
	for _, s := range d.Sessions {
		if s.ID == id {
			return true
		}
	}
	return false
}

// findSession 按 ID 返回会话指针（找不到返回 nil）。
// RunAgent 用它把「读历史」和「写结果」绑定到同一个会话：
// 多端各自浏览不同会话时，后端那个全局 ActiveSession 游标不能作为依据。
func FindSession(d AgentData, id string) *AgentSession {
	if id == "" {
		return nil
	}
	for i := range d.Sessions {
		if d.Sessions[i].ID == id {
			return &d.Sessions[i]
		}
	}
	return nil
}

// defaultToolFlags 返回内置工具默认开关与描述（全部开启，描述为默认值）。
func defaultToolFlags() ToolFlags {
	enabled := map[string]bool{}
	desc := map[string]string{}
	for _, t := range BuiltinToolMeta() {
		enabled[t.Name] = true
		desc[t.Name] = t.Default
	}
	return ToolFlags{Enabled: enabled, Desc: desc}
}

// defaultSkills 预置的数据分析模板技能。用户可在前端「技能」中编辑、新增或关闭，
// 启动时通过 mergeDefaultSkills 合并，不会覆盖用户已有配置。
func defaultSkills() []AgentSkill {
	now := time.Now().Format(time.RFC3339)
	return []AgentSkill{
		{
			ID:          "skill_sales_analysis",
			Name:        "销售数据分析",
			Description: "当用户要求分析销售额、销量、订单、营收、客单价、最近一周/月/季度销售等与销售业绩相关的话题时使用。",
			Prompt:      "你是销售数据分析专家。分析销售相关问题时遵循：\n1. 先用 db_schema 找到订单/销售/商品相关表，确认金额、数量、时间、状态等字段。\n2. 用 db_query 执行只读 SQL，按时间(最近一周/月/季度)、品类、地区、渠道等维度聚合，计算销售额、销量、订单数、客单价、环比/同比。\n3. 识别Top商品、异常波动、退货/退款影响。\n4. 给出结论与可执行建议，并用表格或趋势描述呈现关键指标。",
			Enabled:     true,
			Builtin:     true,
			UpdatedAt:   now,
		},
		{
			ID:          "skill_user_retention",
			Name:        "用户留存分析",
			Description: "当用户要求分析新增用户、活跃用户、留存率、流失率、复购、用户生命周期等与用户行为相关的话题时使用。",
			Prompt:      "你是用户增长分析专家。分析用户留存/活跃相关问题时遵循：\n1. 用 db_schema 定位用户表、注册表、登录/行为表、订单表。\n2. 用 db_query 计算：日/周/月新增、活跃(DAU/WAU/MAU)、次日/7日/30日留存、复购率、流失率。\n3. 按渠道、注册月份做同期群(Cohort)对比。\n4. 输出留存曲线结论与提升建议。",
			Enabled:     true,
			Builtin:     true,
			UpdatedAt:   now,
		},
		{
			ID:          "skill_traffic_conversion",
			Name:        "流量转化分析",
			Description: "当用户要求分析访问量、转化率、漏斗、加购、下单转化、渠道效果等与流量和转化相关的话题时使用。",
			Prompt:      "你是流量与转化分析专家。分析转化相关问题时遵循：\n1. 用 db_schema 定位曝光/点击/访问、加购、下单等各漏斗环节表。\n2. 用 db_query 计算各环节量级与转化率，构建漏斗(曝光→点击→加购→下单→支付)。\n3. 拆解各渠道/落地页的转化差异，定位流失最大环节。\n4. 输出漏斗图结论与优化建议。",
			Enabled:     true,
			Builtin:     true,
			UpdatedAt:   now,
		},
		{
			ID:          "skill_anomaly_diagnosis",
			Name:        "指标异常诊断",
			Description: "当用户要求排查某指标突增/突降、数据异常、波动原因、对比上期差异时使用。",
			Prompt:      "你是指标异常诊断专家。分析异常波动时遵循：\n1. 用 db_schema 确认指标所属表与维度字段。\n2. 用 db_query 拉取异常期与对照期(环比/同比)的分维度数据。\n3. 通过下钻(地区/渠道/品类/新老用户)定位主要贡献因子。\n4. 给出根因结论与验证 SQL。",
			Enabled:     true,
			Builtin:     true,
			UpdatedAt:   now,
		},
	}
}

// mergeDefaultSkills 将预置技能与用户已有技能合并：用户已存在同 ID 的技能以用户的为准，
// 用户未配置过的预置技能追加进列表，保证升级后模板技能自动出现且不覆盖用户修改。
func mergeDefaultSkills(user []AgentSkill) []AgentSkill {
	have := map[string]bool{}
	for _, s := range user {
		if s.ID != "" {
			have[s.ID] = true
		}
	}
	merged := make([]AgentSkill, 0, len(user)+len(defaultSkills()))
	merged = append(merged, user...)
	for _, s := range defaultSkills() {
		if !have[s.ID] {
			merged = append(merged, s)
		}
	}
	return merged
}

// migrateToolFlags 将旧版分组开关（fileOp/webSearch/sysInfo/common）迁移为各工具独立开关。
func migrateToolFlags(old ToolFlags) ToolFlags {
	if old.Enabled != nil && len(old.Enabled) > 0 {
		return old
	}
	enabled := map[string]bool{}
	desc := map[string]string{}
	for _, t := range BuiltinToolMeta() {
		enabled[t.Name] = true
		desc[t.Name] = t.Default
	}
	return ToolFlags{Enabled: enabled, Desc: desc}
}

// pruneOrphanDBAnalysis 剔除已删除连接遗留的表结构 / 语义 / 记忆库，并校准分析连接。
//
// 背景：db_schemas / db_semantics / db_last_db 三张表以 connId 为键，
// 但删除数据库连接时不会级联清除，于是孤立快照会长期残留并混入
// Agent 提示词的可用表结构列表，模型据此填出并不存在的 connId；
// 又因新旧连接 id 常常只差几位数字，人眼极难分辨，
// 最终表现为「明明选了 MySQL，报错却来自 Oracle」。
//
// 同时把 activeDBConn 校准为真实存在的 db 连接：
// 指向已删除连接时应回退到某个有效连接，避免工具全盘失败。
func (m *Manager) pruneOrphanDBAnalysis(cfg AgentConfig, conns []model.PluginConn) AgentConfig {
	if len(cfg.DBSchemas) == 0 && len(cfg.DBSemantics) == 0 && len(cfg.DBLastDB) == 0 && cfg.ActiveDBConn == "" {
		return cfg
	}
	// 现有 db 连接及其 id
	live := make(map[string]bool, len(conns))
	for _, c := range conns {
		if c.Category == "db" {
			live[c.ID] = true
		}
	}
	changed := false
	if len(cfg.DBSchemas) > 0 {
		kept := make(map[string]DBSyncedTable, len(cfg.DBSchemas))
		for k, v := range cfg.DBSchemas {
			id := v.ConnID
			if id == "" {
				id = dbAnalysisKeyConnID(k)
			}
			if id != "" && !live[id] {
				continue // 孤立快照，丢弃
			}
			kept[k] = v
		}
		if len(kept) != len(cfg.DBSchemas) {
			cfg.DBSchemas = kept
			changed = true
		}
	}
	if len(cfg.DBLastDB) > 0 {
		kept := make(map[string]string, len(cfg.DBLastDB))
		for id, v := range cfg.DBLastDB {
			if !live[id] {
				continue
			}
			kept[id] = v
		}
		if len(kept) != len(cfg.DBLastDB) {
			cfg.DBLastDB = kept
			changed = true
		}
	}
	// 字段语义键：connId|database|table 或 connId|database|field
	if len(cfg.DBSemantics) > 0 {
		kept := make(map[string]string, len(cfg.DBSemantics))
		for k, v := range cfg.DBSemantics {
			id := dbAnalysisKeyConnID(k)
			if id != "" && !live[id] {
				continue
			}
			kept[k] = v
		}
		if len(kept) != len(cfg.DBSemantics) {
			cfg.DBSemantics = kept
			changed = true
		}
	}
	// 分析连接校准：仅在其指向已删除的连接时清空。
	//
	// 刻意**不**自动回退到「第一个 db 连接」：连接顺序取决于插入顺序，用户看到的
	// 「第一个」可能是任意一个（实测就曾把 Oracle 当成 MySQL 显示），
	// 静默替用户挑选会导致「我明明选了 MySQL，怎么变成 Oracle 了」。
	// 清空后由用户在「插件 / 数据库连接」里显式启用，符合「连接选择权归用户」。
	if cfg.ActiveDBConn != "" && !live[cfg.ActiveDBConn] {
		cfg.ActiveDBConn = ""
		changed = true
	}
	if changed {
		// 清理结果落库，避免每次启动都重复剔除
		_ = m.host.Store().SaveDBAnalysis(&db.DBAnalysisSnapshot{
			Schemas:   schemasToJSONMap(cfg.DBSchemas),
			Semantics: cfg.DBSemantics,
			LastDB:    cfg.DBLastDB,
		})
	}
	return cfg
}

// dbAnalysisKeyConnID 从 "connId|database|table" 形式的键里取出 connId。
func dbAnalysisKeyConnID(key string) string {
	if i := strings.Index(key, "|"); i > 0 {
		return key[:i]
	}
	return ""
}

// schemasToJSONMap 将表结构快照 map 转为 value 为 JSON 的通用 map，便于跨包存独立表。
//
// 为什么要转：表结构快照存在独立表 db_schemas（一列一行、值为 JSON 文本），
// 而 storeapi.Store 的接口只接受 map[string]string。这里做一层序列化，
// 与 schemasFromJSONMap 成对使用。
func schemasToJSONMap(schemas map[string]DBSyncedTable) map[string]string {
	out := map[string]string{}
	if schemas == nil {
		return out
	}
	for k, v := range schemas {
		if b, err := json.Marshal(v); err == nil {
			out[k] = string(b)
		}
	}
	return out
}

// schemasFromJSONMap 将独立表中 value 为 JSON 的 map 转回表结构快照 map。
// 单行 JSON 解析失败时跳过该行（结构演进/手工改库都可能留下坏行），
// 不能让一行坏数据导致整份 Agent 数据读不出来。
func schemasFromJSONMap(m map[string]string) map[string]DBSyncedTable {
	out := map[string]DBSyncedTable{}
	if m == nil {
		return out
	}
	for k, v := range m {
		var t DBSyncedTable
		if err := json.Unmarshal([]byte(v), &t); err == nil {
			out[k] = t
		}
	}
	return out
}

// activeSession 返回当前激活会话指针（不存在时返回 nil）。
func (d *AgentData) ActiveSessionPtr() *AgentSession {
	for i := range d.Sessions {
		if d.Sessions[i].ID == d.ActiveSession {
			return &d.Sessions[i]
		}
	}
	return nil
}

// maxAgentLogBytes 日志总字符预算（约 8MB）。超过后丢弃最旧的日志，
// 避免 meta.agent 单列随使用时间无限膨胀、每次日志追加都全量读写。
const maxAgentLogBytes = 8 << 20

// trimLogsByBudget 从最旧（切片头部）开始丢弃，直到日志总字符数不超过预算。
// 只统计主要字段（Detail/Title/Summary），足以反映体积。
func trimLogsByBudget(logs []AgentLog, budget int) []AgentLog {
	total := 0
	for i := range logs {
		total += len(logs[i].Detail) + len(logs[i].Title) + len(logs[i].Summary)
	}
	if total <= budget {
		return logs
	}
	cut := 0
	for cut < len(logs) && total > budget {
		total -= len(logs[cut].Detail) + len(logs[cut].Title) + len(logs[cut].Summary)
		cut++
	}
	return logs[cut:]
}

// WriteAgentData 将 Agent 数据序列化并持久化到应用主库（meta.agent 列）。
// 调用方需持有 m.mu；内部会读取当前 AppData 并覆盖 Agent 字段后整体保存。
func (m *Manager) WriteAgentData(data AgentData) error {
	// 限制日志与消息数量，避免无限增长。
	// 另加一层「总字符数」上限：单条 Detail 最大 200k 字符，2000 条日志理论上可达
	// 数百 MB，每次追加日志都要把整份 JSON 读写一遍（且携带全部会话），
	// 会让 Agent 用久了之后越来越卡。超限时从最旧的日志开始丢弃。
	if len(data.Logs) > 2000 {
		data.Logs = data.Logs[len(data.Logs)-2000:]
	}
	data.Logs = trimLogsByBudget(data.Logs, maxAgentLogBytes)
	if len(data.Messages) > 500 {
		data.Messages = data.Messages[len(data.Messages)-500:]
	}
	// 限制每个会话消息数量，避免无限增长
	for i := range data.Sessions {
		if len(data.Sessions[i].Messages) > 500 {
			data.Sessions[i].Messages = data.Sessions[i].Messages[len(data.Sessions[i].Messages)-500:]
		}
	}
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if err := m.host.Store().SaveAgentRaw(string(b)); err != nil {
		return err
	}
	// 同步数据库连接分析数据到独立表（与 meta.agent 冗余，但支持行级读写与查询）。
	snap := &db.DBAnalysisSnapshot{
		Schemas:   schemasToJSONMap(data.Config.DBSchemas),
		Semantics: data.Config.DBSemantics,
		LastDB:    data.Config.DBLastDB,
	}
	return m.host.Store().SaveDBAnalysis(snap)
}
