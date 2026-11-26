package agent

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// 基础层：运行日志（写入 / 查询 / 筛选项 / 清空）。
// 日志与 AgentData 同存于 meta.agent，按条数预算裁剪；实时推送只带摘要。

// ============================ 日志 ============================

func (m *Manager) AppendLog(l AgentLog) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.ReadAgentData()
	if l.ID == "" {
		l.ID = agentID("log")
	}
	if l.Timestamp == 0 {
		l.Timestamp = time.Now().UnixMilli()
	}
	if l.Time == "" {
		l.Time = time.Now().Format("2006-01-02 15:04:05.000")
	}
	d.Logs = append(d.Logs, l)
	_ = m.WriteAgentData(d)
	// 实时推送给前端日志面板：只发轻量摘要，不携带 Detail。
	// 单条 Detail 最大可达 200k 字符（完整请求上下文），面板收到事件后
	// 会按需 QueryAgentLogs 重新拉取列表，事件本身无需带全文；
	// 否则每条日志都会在 IPC/SSE 上重复传输大段文本，越用越卡。
	if m.ctx != nil {
		lite := l
		lite.Detail = ""
		m.b.Emit("agent:log", lite)
	}
}

// QueryAgentLogsArgs 日志查询参数。
type QueryAgentLogsArgs struct {
	Keyword  string `json:"keyword"`
	Level    string `json:"level"`
	Category string `json:"category"`
	Limit    int    `json:"limit"`
	// WithDetail 是否在列表里一并返回详情。默认 false：
	// 单条请求内容可能很长（系统提示词 + 上下文 + 工具结果），500 条一起返回会拖垮界面，
	// 因此列表只返回摘要，点击某条时再用 GetAgentLog 取完整详情。
	WithDetail bool `json:"withDetail"`
}

// QueryAgentLogs 按关键词/级别/分类搜索日志，返回最新在前（默认不含详情）。
func (m *Manager) QueryAgentLogs(args QueryAgentLogsArgs) []AgentLog {
	d := m.ReadAgentData()
	kw := strings.ToLower(strings.TrimSpace(args.Keyword))
	out := make([]AgentLog, 0, len(d.Logs))
	for _, l := range d.Logs {
		if args.Level != "" && l.Level != args.Level {
			continue
		}
		if args.Category != "" && l.Category != args.Category {
			continue
		}
		if kw != "" {
			hay := strings.ToLower(l.Title + " " + l.Detail)
			if !strings.Contains(hay, kw) {
				continue
			}
		}
		out = append(out, l)
	}
	// 稳定排序：一次对话常在同一毫秒内产生多条日志（info/request/response/tool），
	// 用 Slice 会出现顺序随机跳动；SliceStable 保持写入顺序（新的在后 → 同毫秒时更晚的在前）
	sort.SliceStable(out, func(i, j int) bool { return out[i].Timestamp > out[j].Timestamp })
	limit := args.Limit
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	if len(out) > limit {
		out = out[:limit]
	}
	if !args.WithDetail {
		// 列表模式：详情清空（点开时再按需取全文），但保留一段摘要，
		// 否则「按关键词搜到的日志」在列表里看不到任何内容线索，会误以为筛选没生效。
		for i := range out {
			out[i].Summary = logSummary(out[i].Detail)
			out[i].Detail = ""
		}
	}
	return out
}

// logSummary 取详情开头作为列表摘要（压掉参数行与分隔线）。
func logSummary(detail string) string {
	s := strings.TrimSpace(strings.ReplaceAll(detail, "\r\n", "\n"))
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "请求参数") {
		if i := strings.Index(s, "\n\n"); i > 0 {
			s = strings.TrimSpace(s[i+2:])
		}
	}
	if n := len([]rune(s)); n > 160 {
		return strings.Join(strings.Fields(string([]rune(s)[:160])), " ") + "…"
	}
	return s
}

// GetAgentLogFacets 返回当前日志中实际存在的级别与分类（含条数）。
// 前端据此动态生成筛选项，避免出现「选项存在但永远查不到数据」的情况
// （例如历史上提供的 plan 级别、skill 分类实际从未产生过）。
func (m *Manager) GetAgentLogFacets() map[string][]map[string]interface{} {
	d := m.ReadAgentData()
	levels := map[string]int{}
	cats := map[string]int{}
	for _, l := range d.Logs {
		levels[l.Level]++
		cats[l.Category]++
	}
	toArr := func(mp map[string]int) []map[string]interface{} {
		keys := make([]string, 0, len(mp))
		for k := range mp {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			if mp[keys[i]] != mp[keys[j]] {
				return mp[keys[i]] > mp[keys[j]]
			}
			return keys[i] < keys[j]
		})
		arr := make([]map[string]interface{}, 0, len(keys))
		for _, k := range keys {
			arr = append(arr, map[string]interface{}{"value": k, "count": mp[k]})
		}
		return arr
	}
	// 汇总全部日志里记录的 LLM 用量（日志面板顶部展示「输入/输出/合计」）
	var total TokenUsage
	for _, l := range d.Logs {
		if l.Usage == nil {
			continue
		}
		total.PromptTokens += l.Usage.PromptTokens
		total.CompletionTokens += l.Usage.CompletionTokens
		total.TotalTokens += l.Usage.TotalTokens
	}
	return map[string][]map[string]interface{}{
		"levels":     toArr(levels),
		"categories": toArr(cats),
		"tokenUsage": {{
			"value":            "total",
			"count":            total.TotalTokens,
			"promptTokens":     total.PromptTokens,
			"completionTokens": total.CompletionTokens,
			"totalTokens":      total.TotalTokens,
		}},
	}
}

// GetAgentLog 按 ID 返回单条日志的完整详情（列表默认不带 detail，用于点击展开时按需加载）。
func (m *Manager) GetAgentLog(id string) (AgentLog, error) {
	d := m.ReadAgentData()
	for _, l := range d.Logs {
		if l.ID == id {
			return l, nil
		}
	}
	return AgentLog{}, fmt.Errorf("日志不存在或已过期")
}

// ClearAgentLogs 清空日志。
func (m *Manager) ClearAgentLogs() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.ReadAgentData()
	d.Logs = []AgentLog{}
	return m.WriteAgentData(d)
}
