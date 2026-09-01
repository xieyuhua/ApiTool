package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"apitool/internal/model"
	"apitool/internal/plugins"
)

// builtinServerID 内置工具归属的虚拟服务器 ID（模型通过 server="builtin" 调用）。
const builtinServerID = "builtin"

// collectBuiltinTools 根据开关收集当前启用的内置工具定义。
func collectBuiltinTools(enabled map[string]bool, desc map[string]string) []MCPTool {
	mk := func(name, d string, props map[string]interface{}, required []string) MCPTool {
		schema := map[string]interface{}{"type": "object", "properties": props}
		if len(required) > 0 {
			schema["required"] = required
		}
		b, _ := json.Marshal(schema)
		return MCPTool{Name: name, Description: d, Server: builtinServerID, ServerName: "内置", InputSchema: b}
	}
	// 各工具默认参数 schema（与名称一一对应）
	schemas := map[string]struct {
		props    map[string]interface{}
		required []string
	}{
		"read_file": {map[string]interface{}{
			"path":  map[string]interface{}{"type": "string", "description": "要读取的文件绝对路径，例如 C:\\Users\\me\\a.txt 或 /home/me/a.txt"},
			"limit": map[string]interface{}{"type": "number", "description": "可选，只读取前 N 行（不填则读取全部，受文件读取上限约束）"},
		}, []string{"path"}},
		"write_file": {map[string]interface{}{
			"path":    map[string]interface{}{"type": "string", "description": "要写入的文件绝对路径（不存在则新建，存在则覆盖）"},
			"content": map[string]interface{}{"type": "string", "description": "要写入文件的完整文本内容"},
		}, []string{"path", "content"}},
		"list_dir": {map[string]interface{}{
			"path": map[string]interface{}{"type": "string", "description": "要列出的目录绝对路径；留空或省略则列出当前工作目录"},
		}, nil},
		"make_dir": {map[string]interface{}{
			"path": map[string]interface{}{"type": "string", "description": "要创建的目录绝对路径，可包含多级不存在的父目录"},
			"all":  map[string]interface{}{"type": "boolean", "description": "是否递归创建多级目录，默认 true（即自动创建缺失的父目录）"},
		}, []string{"path"}},
		"remove_dir": {map[string]interface{}{
			"path": map[string]interface{}{"type": "string", "description": "要删除的目录绝对路径"},
			"all":  map[string]interface{}{"type": "boolean", "description": "是否递归删除（含所有子内容）。默认 false，仅能删除空目录；确认要删除非空目录时设为 true"},
		}, []string{"path"}},
		"remove_file": {map[string]interface{}{
			"path": map[string]interface{}{"type": "string", "description": "要删除的文件绝对路径（不是目录）"},
		}, []string{"path"}},
		"rename_path": {map[string]interface{}{
			"src": map[string]interface{}{"type": "string", "description": "原文件或目录的绝对路径"},
			"dst": map[string]interface{}{"type": "string", "description": "新的绝对路径（用于重命名或移动，目录需已存在）"},
		}, []string{"src", "dst"}},
		"web_search": {map[string]interface{}{
			"query": map[string]interface{}{"type": "string", "description": "搜索关键词或问题，例如 \"Go 语言 defer 执行顺序\""},
			"limit": map[string]interface{}{"type": "number", "description": "可选，返回结果条数，默认 5"},
		}, []string{"query"}},
		"system_info": {map[string]interface{}{}, nil},
		"get_time": {map[string]interface{}{
			"timezone": map[string]interface{}{"type": "string", "description": "可选 IANA 时区名，例如 Asia/Shanghai、America/New_York；省略则用本机时区"},
		}, nil},
		"calc": {map[string]interface{}{
			"expr": map[string]interface{}{"type": "string", "description": "算术表达式，仅含数字与运算符 + - * / 和括号 ()，例如 \"(1+2)*3\""},
		}, []string{"expr"}},
		"run_command": {map[string]interface{}{
			"command": map[string]interface{}{"type": "string", "description": "要执行的 shell 命令字符串，例如 \"ls -la\" 或 \"git status\""},
		}, []string{"command"}},
		"db_schema": {map[string]interface{}{
			"connId": map[string]interface{}{"type": "string", "description": "数据库连接 ID（在 Agent「数据连接」管理中配置的 ID，如 mysql-1 / pg-1 / ora-1）"},
			"database": map[string]interface{}{"type": "string", "description": "库名 / 服务名（Oracle 为服务名或 SID）"},
			"tables":   map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "要同步的表名列表；留空则同步该库全部表"},
		}, []string{"connId", "database"}},
		"db_query": {map[string]interface{}{
			"connId":  map[string]interface{}{"type": "string", "description": "数据库连接 ID"},
			"database": map[string]interface{}{"type": "string", "description": "库名 / 服务名"},
			"sql":      map[string]interface{}{"type": "string", "description": "要执行的 SELECT 语句（禁止写操作）"},
			"limit":    map[string]interface{}{"type": "number", "description": "返回行数上限，默认 200"},
		}, []string{"connId", "database", "sql"}},
	}
	var out []MCPTool
	for _, t := range BuiltinToolMeta() {
		if !enabled[t.Name] {
			continue
		}
		d := t.Default
		if v, ok := desc[t.Name]; ok && v != "" {
			d = v
		}
		s := schemas[t.Name]
		out = append(out, mk(t.Name, d, s.props, s.required))
	}
	return out
}

// execBuiltinTool 本地执行内置工具。fileLimit 为文件读取最大字符数。
func (m *Manager) execBuiltinTool(name string, args map[string]interface{}, fileLimit int) (string, error) {
	switch name {
	case "read_file":
		return builtinReadFile(args, fileLimit)
	case "write_file":
		return builtinWriteFile(args)
	case "list_dir":
		return builtinListDir(args)
	case "make_dir":
		return builtinMakeDir(args)
	case "remove_dir":
		return builtinRemoveDir(args)
	case "remove_file":
		return builtinRemoveFile(args)
	case "rename_path":
		return builtinRenamePath(args)
	case "web_search":
		return builtinWebSearch(args)
	case "system_info":
		return builtinSystemInfo()
	case "get_time":
		return builtinGetTime(args)
	case "calc":
		return builtinCalc(args)
	case "run_command":
		return builtinRunCommand(args)
	case "db_schema":
		return builtinDBSchema(m, args)
	case "db_query":
		return builtinDBQuery(m, args)
	default:
		return "", fmt.Errorf("未知内置工具: %s", name)
	}
}

// findDBConn 从应用数据中按 connId 找到 db 分类的连接配置
func findDBConn(m *Manager, connID string) (model.PluginConn, error) {
	if m == nil || m.host == nil {
		return model.PluginConn{}, fmt.Errorf("agent 未正确初始化")
	}
	for _, c := range m.host.ReadData().Plugins.Connections {
		if c.ID == connID {
			if c.Category != "" && c.Category != "db" {
				return model.PluginConn{}, fmt.Errorf("连接 %s 类型为 %s，不是数据库(db)连接", connID, c.Category)
			}
			return c, nil
		}
	}
	return model.PluginConn{}, fmt.Errorf("未找到数据库连接 %s（请先在「数据连接」管理中配置）", connID)
}

// dbSemantic 读取用户在「数据连接」管理中维护的字段/表语义。
// key = connId|database|table|column（全小写）；column 为空时查表级语义（key 不含 column 段）。
func dbSemantic(m *Manager, connID, database, table, column string) string {
	if m == nil {
		return ""
	}
	cfg := m.LoadAgentData().Config
	sem := cfg.DBSemantics
	if len(sem) == 0 {
		return ""
	}
	if column != "" {
		key := strings.ToLower(fmt.Sprintf("%s|%s|%s|%s", connID, database, table, column))
		if v, ok := sem[key]; ok && strings.TrimSpace(v) != "" {
			return v
		}
		return ""
	}
	key := strings.ToLower(fmt.Sprintf("%s|%s|%s", connID, database, table))
	if v, ok := sem[key]; ok && strings.TrimSpace(v) != "" {
		return v
	}
	return ""
}

// builtinDBSchema 读取表结构（含字段、类型、注释、行数），整理为便于大模型理解的文本
func builtinDBSchema(m *Manager, args map[string]interface{}) (string, error) {
	connID, _ := args["connId"].(string)
	database, _ := args["database"].(string)
	if connID == "" {
		connID = m.LoadAgentData().Config.ActiveDBConn // 未指定则使用当前激活的连接
	}
	if connID == "" || database == "" {
		return "", fmt.Errorf("缺少 connId 或 database 参数（可在「插件 / 数据库连接」中启用一个连接作为分析连接）")
	}
	conn, err := findDBConn(m, connID)
	if err != nil {
		return "", err
	}
	var tables []string
	if t, ok := args["tables"].([]interface{}); ok {
		for _, x := range t {
			if s, ok := x.(string); ok && s != "" {
				tables = append(tables, s)
			}
		}
	}
	schemas, err := plugins.PluginDBSchema(conn, database, tables)
	if err != nil {
		return "", err
	}
	if len(schemas) == 0 {
		return fmt.Sprintf("库 %s 中没有表或未能读取到表结构。", database), nil
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# 数据库结构：%s（库 %s，共 %d 张表）\n\n", conn.DbType, database, len(schemas)))
	for _, s := range schemas {
		sb.WriteString(fmt.Sprintf("## 表 `%s`", s.Table))
		if s.Rows > 0 {
			sb.WriteString(fmt.Sprintf("（约 %d 行）", s.Rows))
		}
		sb.WriteString("\n")
		// 叠加用户在「数据连接」管理中维护的表级语义
		if tsem := dbSemantic(m, connID, database, s.Table, ""); tsem != "" {
			sb.WriteString(fmt.Sprintf("> 表语义：%s\n\n", tsem))
		}
		if len(s.Columns) == 0 {
			sb.WriteString("_（无字段信息）_\n")
		} else {
			sb.WriteString("| 字段 | 类型 | 可空 | 默认值 | 注释/语义 |\n")
			sb.WriteString("| --- | --- | --- | --- | --- |\n")
			for _, c := range s.Columns {
				cmt := c.Comment
				if cmt == "" {
					cmt = "—"
				}
				// 叠加用户在「数据连接」管理中维护的字段语义（优先于数据库自带注释）
				if sem := dbSemantic(m, connID, database, s.Table, c.Name); sem != "" {
					if cmt == "" || cmt == "—" {
						cmt = sem
					} else {
						cmt = cmt + "；" + sem
					}
				}
				sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s |\n", c.Name, c.Type, c.Nullable, c.Default, cmt))
			}
		}
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

// builtinDBQuery 在连接上执行只读 SELECT 并返回结果（限制行数）
func builtinDBQuery(m *Manager, args map[string]interface{}) (string, error) {
	connID, _ := args["connId"].(string)
	database, _ := args["database"].(string)
	sql, _ := args["sql"].(string)
	if connID == "" {
		connID = m.LoadAgentData().Config.ActiveDBConn
	}
	if connID == "" || database == "" || sql == "" {
		return "", fmt.Errorf("缺少 connId / database / sql 参数（可在「插件 / 数据库连接」中启用一个连接作为分析连接）")
	}
	upper := strings.ToUpper(strings.TrimSpace(sql))
	if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "WITH") {
		return "", fmt.Errorf("仅允许 SELECT / WITH 查询，禁止写操作")
	}
	conn, err := findDBConn(m, connID)
	if err != nil {
		return "", err
	}
	limit := 200
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}
	res, err := plugins.PluginDBQuery(conn, plugins.DBQueryReq{Database: database, SQL: sql, Limit: limit})
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("查询返回 %d 行 %d 列：\n", len(res.Rows), len(res.Columns)))
	sb.WriteString(strings.Join(res.Columns, "\t") + "\n")
	for _, row := range res.Rows {
		sb.WriteString(strings.Join(row, "\t") + "\n")
	}
	return sb.String(), nil
}

func builtinReadFile(args map[string]interface{}, fileLimit int) (string, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return "", fmt.Errorf("缺少 path 参数")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	content := string(b)
	if limit, ok := args["limit"].(float64); ok && limit > 0 {
		lines := strings.Split(content, "\n")
		if int(limit) < len(lines) {
			content = strings.Join(lines[:int(limit)], "\n")
		}
		return content, nil
	}
	// 防止超大文件（fileLimit 可配置，<=0 使用默认）
	if fileLimit <= 0 {
		fileLimit = 200000
	}
	r := []rune(content)
	if len(r) > fileLimit {
		content = string(r[:fileLimit]) + "\n... (内容已截断，可在设置中调大上限)"
	}
	return content, nil
}

func builtinWriteFile(args map[string]interface{}) (string, error) {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	if path == "" {
		return "", fmt.Errorf("缺少 path 参数")
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("已写入 %d 字节到 %s", len(content), path), nil
}

func builtinListDir(args map[string]interface{}) (string, error) {
	path, _ := args["path"].(string)
	if path == "" {
		path = "."
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, e := range entries {
		if e.IsDir() {
			sb.WriteString("[DIR]  ")
		} else {
			sb.WriteString("[FILE] ")
		}
		sb.WriteString(e.Name())
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

func builtinMakeDir(args map[string]interface{}) (string, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return "", fmt.Errorf("缺少 path 参数")
	}
	// all 缺省按 true 处理（递归创建多级目录），避免常见失败
	all := true
	if v, ok := args["all"].(bool); ok {
		all = v
	}
	if all {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return "", err
		}
	} else {
		if err := os.Mkdir(path, 0o755); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("已创建目录: %s", path), nil
}

func builtinRemoveDir(args map[string]interface{}) (string, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return "", fmt.Errorf("缺少 path 参数")
	}
	all := false
	if v, ok := args["all"].(bool); ok {
		all = v
	}
	if all {
		if err := os.RemoveAll(path); err != nil {
			return "", err
		}
		return fmt.Sprintf("已递归删除目录: %s", path), nil
	}
	// 默认仅删除空目录
	if err := os.Remove(path); err != nil {
		return "", fmt.Errorf("删除失败（目录非空？可传入 all=true 递归删除）: %v", err)
	}
	return fmt.Sprintf("已删除空目录: %s", path), nil
}

func builtinRemoveFile(args map[string]interface{}) (string, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return "", fmt.Errorf("缺少 path 参数")
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return fmt.Sprintf("已删除文件: %s", path), nil
}

func builtinRenamePath(args map[string]interface{}) (string, error) {
	src, _ := args["src"].(string)
	dst, _ := args["dst"].(string)
	if src == "" || dst == "" {
		return "", fmt.Errorf("缺少 src 或 dst 参数")
	}
	if err := os.Rename(src, dst); err != nil {
		return "", err
	}
	return fmt.Sprintf("已移动/重命名: %s -> %s", src, dst), nil
}

func builtinWebSearch(args map[string]interface{}) (string, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return "", fmt.Errorf("缺少 query 参数")
	}
	limit := 5
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}
	// 使用 DuckDuckGo HTML 接口（无需 API Key）
	url := "https://html.duckduckgo.com/html/?q=" + urlEncode(query)
	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) ApitoolAgent")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := readAllString(resp)
	results := parseDuckDuckGo(body, limit)
	if len(results) == 0 {
		return "未找到相关结果。", nil
	}
	var sb strings.Builder
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("%d. %s\n   %s\n   %s\n\n", i+1, r.title, r.snippet, r.url))
	}
	return sb.String(), nil
}

func builtinSystemInfo() (string, error) {
	host, _ := os.Hostname()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	wd, _ := os.Getwd()
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("操作系统: %s\n", runtime.GOOS))
	sb.WriteString(fmt.Sprintf("架构: %s\n", runtime.GOARCH))
	sb.WriteString(fmt.Sprintf("CPU 核心数: %d\n", runtime.NumCPU()))
	sb.WriteString(fmt.Sprintf("主机名: %s\n", host))
	sb.WriteString(fmt.Sprintf("当前时间: %s\n", time.Now().Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("工作目录: %s\n", wd))
	sb.WriteString(fmt.Sprintf("Go 堆内存使用: %d MB\n", m.Alloc/1024/1024))
	return sb.String(), nil
}

func builtinGetTime(args map[string]interface{}) (string, error) {
	tz, _ := args["timezone"].(string)
	now := time.Now()
	if tz != "" {
		loc, err := time.LoadLocation(tz)
		if err == nil {
			now = now.In(loc)
		}
	}
	return now.Format("2006-01-02 15:04:05 MST -0700"), nil
}

func builtinCalc(args map[string]interface{}) (string, error) {
	expr, _ := args["expr"].(string)
	if expr == "" {
		return "", fmt.Errorf("缺少 expr 参数")
	}
	v, err := evalExpr(expr)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s = %s", expr, formatNum(v)), nil
}

func builtinRunCommand(args map[string]interface{}) (string, error) {
	command, _ := args["command"].(string)
	if command == "" {
		return "", fmt.Errorf("缺少 command 参数")
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", command)
	} else {
		cmd = exec.Command("sh", "-c", command)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out) + "\n[命令执行出错: " + err.Error() + "]", nil
	}
	return string(out), nil
}

// ============================ 辅助：搜索结果解析 ============================

type ddgResult struct {
	title  string
	snippet string
	url    string
}

func parseDuckDuckGo(html string, limit int) []ddgResult {
	var out []ddgResult
	// 提取 result__a（标题+链接）与 result__snippet（摘要）
	reA := regexpTitle()
	reS := regexpSnippet()
	titles := reA.FindAllStringSubmatch(html, -1)
	snips := reS.FindAllStringSubmatch(html, -1)
	for i := 0; i < len(titles) && len(out) < limit; i++ {
		url := extractAttr(titles[i][1], "href")
		title := stripTagsInline(titles[i][2])
		title = strings.TrimSpace(decodeHTML(title))
		if title == "" {
			continue
		}
		snippet := ""
		if i < len(snips) {
			snippet = strings.TrimSpace(decodeHTML(stripTagsInline(snips[i][1])))
		}
		out = append(out, ddgResult{title: title, snippet: snippet, url: url})
	}
	return out
}

// ============================ 辅助：安全四则运算 ============================

func evalExpr(expr string) (float64, error) {
	expr = strings.ReplaceAll(expr, " ", "")
	if expr == "" {
		return 0, fmt.Errorf("空表达式")
	}
	// 仅允许数字与运算符
	for _, r := range expr {
		if !strings.ContainsRune("0123456789.+-*/()", r) {
			return 0, fmt.Errorf("表达式含非法字符: %c", r)
		}
	}
	return parseExpr(expr)
}

// 简易递归下降解析：表达式 -> 项 (+|-) 项 ...
func parseExpr(s string) (float64, error) {
	pos := 0
	var parse func() (float64, error)
	parse = func() (float64, error) {
		lhs, err := parseTerm(s, &pos)
		if err != nil {
			return 0, err
		}
		for pos < len(s) && (s[pos] == '+' || s[pos] == '-') {
			op := s[pos]
			pos++
			rhs, err := parseTerm(s, &pos)
			if err != nil {
				return 0, err
			}
			if op == '+' {
				lhs += rhs
			} else {
				lhs -= rhs
			}
		}
		return lhs, nil
	}
	return parse()
}

func parseTerm(s string, pos *int) (float64, error) {
	lhs, err := parseFactor(s, pos)
	if err != nil {
		return 0, err
	}
	for *pos < len(s) && (s[*pos] == '*' || s[*pos] == '/') {
		op := s[*pos]
		*pos++
		rhs, err := parseFactor(s, pos)
		if err != nil {
			return 0, err
		}
		if op == '*' {
			lhs *= rhs
		} else {
			if rhs == 0 {
				return 0, fmt.Errorf("除零错误")
			}
			lhs /= rhs
		}
	}
	return lhs, nil
}

func parseFactor(s string, pos *int) (float64, error) {
	if *pos < len(s) && s[*pos] == '(' {
		*pos++
		v, err := parseExprInline(s, pos)
		if err != nil {
			return 0, err
		}
		if *pos < len(s) && s[*pos] == ')' {
			*pos++
		}
		return v, nil
	}
	start := *pos
	for *pos < len(s) && (isDigit(s[*pos]) || s[*pos] == '.') {
		*pos++
	}
	if start == *pos {
		return 0, fmt.Errorf("解析失败于位置 %d", *pos)
	}
	v, err := strconv.ParseFloat(s[start:*pos], 64)
	if err != nil {
		return 0, err
	}
	return v, nil
}

// parseExprInline 供括号内部调用（避免递归类型声明问题）。
func parseExprInline(s string, pos *int) (float64, error) {
	lhs, err := parseTerm(s, pos)
	if err != nil {
		return 0, err
	}
	for *pos < len(s) && (s[*pos] == '+' || s[*pos] == '-') {
		op := s[*pos]
		*pos++
		rhs, err := parseTerm(s, pos)
		if err != nil {
			return 0, err
		}
		if op == '+' {
			lhs += rhs
		} else {
			lhs -= rhs
		}
	}
	return lhs, nil
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func formatNum(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func readAllString(resp *http.Response) (string, error) {
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func urlEncode(s string) string {
	return url.QueryEscape(s)
}

func regexpTitle() *regexp.Regexp {
	return regexp.MustCompile(`<a[^>]*class="result__a"[^>]*href="([^"]*)"[^>]*>(.*?)</a>`)
}

func regexpSnippet() *regexp.Regexp {
	return regexp.MustCompile(`<a[^>]*class="result__snippet"[^>]*>(.*?)</a>`)
}

func extractAttr(s, attr string) string {
	re := regexp.MustCompile(attr + `="([^"]*)"`)
	if m := re.FindStringSubmatch(s); len(m) > 1 {
		return m[1]
	}
	return ""
}

func stripTagsInline(s string) string {
	re := regexp.MustCompile(`<[^>]+>`)
	return re.ReplaceAllString(s, "")
}

func decodeHTML(s string) string {
	repls := []struct {
		from string
		to   string
	}{
		{"&amp;", "&"}, {"&lt;", "<"}, {"&gt;", ">"}, {"&quot;", "\""},
		{"&#x27;", "'"}, {"&apos;", "'"}, {"&nbsp;", " "},
	}
	for _, r := range repls {
		s = strings.ReplaceAll(s, r.from, r.to)
	}
	return s
}
