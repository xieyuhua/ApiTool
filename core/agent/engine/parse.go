package engine

// 引擎层：工具调用解析。
//
// 模型返回的工具调用格式极不统一：标准 <tool_call><function>…</function></tool_call>、
// 只闭合一半的标签、裸 JSON、DSML 前缀（｜｜DSML｜｜）等。
// 这里按「从严格到宽松」逐级降级解析，尽量把能看懂的都执行起来；
// 解析失败时由上层把原始片段作为诊断步骤展示给用户。

import (
	"encoding/json"
	"regexp"
	"strings"
)

// errStreamIdle 表示流式响应空闲超时。

var thinkingRe = regexp.MustCompile(`(?s)<thinking>(.*?)</thinking>`)
var planRe = regexp.MustCompile(`(?s)<plan>(.*?)</plan>`)
var toolJSONRe = regexp.MustCompile("(?s)```json\\s*(\\{.*?\\})\\s*```")

// 兼容 OpenAI function-call 风格的 <tool_call>/<function>/<parameter> 标签。
// 支持多种变体：
//   - <tool_call><function>name</function><parameter name="k">v</parameter></tool_call>
//   - <tool_call><function name="name"><parameter .../></function></tool_call>
//   - 裸 <function>name</function> 或 <function name="name">...</function>
var toolCallTagRe = regexp.MustCompile(`(?s)<tool_call>(.*?)</tool_call>`)

// 函数名在标签体内：<function>name</function>
var toolCallFuncRe = regexp.MustCompile(`(?s)<function\s*>\s*(.*?)\s*</function>`)

// 函数名在属性里：<function name="name">...</function>
var toolCallFuncAttrRe = regexp.MustCompile(`(?s)<function\s+name\s*=\s*["']([^"']*)["'](?:\s+[^>]*)?>(.*?)</function>`)
var toolCallParamRe = regexp.MustCompile(`(?s)<parameter\s+name\s*=\s*"([^"]*)"(?:\s+[^>]*)?>(.*?)</parameter>`)

// 外层可能包 <function_calls> ... </function_calls>
var funcCallsRe = regexp.MustCompile(`(?s)<function_calls>(.*?)</function_calls>`)

// looseCallsOpenRe / looseCallsCloseRe 宽松匹配工具调用外层标签的各种畸形变体：
//
//	<tool_calls>  <function_calls>  < calls>  <｜｜DSML｜｜ calls>  <tool_call >
//
// 允许残留的 DSML 片段与任意空白，统一收敛为标准的单数形式。
// 模型输出这类变体时若不能识别，会导致整个工具调用被当成正文丢弃。
var looseCallsOpenRe = regexp.MustCompile(`(?is)<\s*[｜|]*\s*(?:dsml)?[\s|｜]*\s*(?:tool_calls|function_calls|tool_call|calls)\s*>`)
var looseCallsCloseRe = regexp.MustCompile(`(?is)<\s*/\s*[｜|]*\s*(?:dsml)?[\s|｜]*\s*(?:tool_calls|function_calls|tool_call|calls)\s*>`)

// toolCallMarkers 流式输出中可能出现的「工具调用起始标记」。
// hit=true 表示命中即确认为工具调用（标签形式，最终必被 stripTags 剥除）；
// hit=false 表示还需进一步判定（```json 代码块也可能是普通代码，不能误伤）。
var toolCallMarkers = []struct {
	tag string
	hit bool
}{
	{"<tool_call", true},
	{"<function_calls", true},
	{"<function", true},
	{"```json", false},
}

// toolJSONProbeRe 用于判定 ```json 代码块内是否为工具调用。
var toolJSONProbeRe = regexp.MustCompile(`"action"\s*:\s*"tool"`)

// funcLooseNameRe 匹配 <function=NAME> / <function:NAME> / <function NAME>。
var funcLooseNameRe = regexp.MustCompile(`(?is)<\s*function\s*[=:]\s*["']?([A-Za-z0-9_.\-]+)["']?\s*>`)

// funcNameLooseRes 是「不依赖闭合标签」的函数名提取规则，按优先级排列：
// 模型经常只闭合 <parameter> 与 <tool_call> 而漏掉 </function>，
// 依赖闭合的正则会整体失配，导致工具名取不到、参数全部丢失。
var funcNameLooseRes = []*regexp.Regexp{
	// <function name="x"> / <function name='x'> / <function name=x>
	regexp.MustCompile(`(?is)<\s*function\s+name\s*=\s*["']?([A-Za-z0-9_.\-]+)["']?[^>]*>`),
	// <function=x> / <function:x>
	regexp.MustCompile(`(?is)<\s*function\s*[=:]\s*["']?([A-Za-z0-9_.\-]+)["']?[^>]*>`),
	// <function>x</function>，以及未闭合的 <function>x
	regexp.MustCompile(`(?is)<\s*function\s*>\s*([A-Za-z0-9_.\-]+)\s*(?:</\s*function\s*>|[\r\n]|$)`),
}

// paramOpenRe 只匹配参数**起始**标签（含自闭合形式），参数值另行按边界截断。
var paramOpenRe = regexp.MustCompile(`(?is)<\s*parameter\s+name\s*=\s*["']([^"']+)["'][^>]*>`)

// paramBoundaryRe 标识参数值的结束边界：下一个参数起始、任何闭合或起始标签。
var paramBoundaryRe = regexp.MustCompile(`(?is)<\s*/?\s*(parameter|function|tool_call|tool_calls|invoke)\b`)

// looseTagRe 清理参数值里残留的孤立标签（如 </function>、<parameter name="x">）。
var looseTagRe = regexp.MustCompile(`(?is)<\s*/?\s*(parameter|function|tool_call|tool_calls|invoke)\b[^>]*>`)

// normalizeToolTags 规范化工具调用标签：模型/中间件会输出多种畸形格式，统一还原为
// 标准 <tool_call><function name="x">...</function><parameter name="k">v</parameter></tool_call>。
// 已覆盖的形态：
//  1. 带前缀：<｜｜DSML｜｜tool_call> / <｜｜DSML｜｜function>
//  2. 复数+invoke 风格（Claude 系）：<tool_calls><invoke name="x"><parameter .../></invoke></tool_calls>
//  3. 畸形闭合：<function>名</parameter>（function 被 parameter 错误闭合）
var dsmlStripRe = regexp.MustCompile(`｜｜DSML｜｜`)

// 复数外层 <tool_calls> / </tool_calls> -> <tool_call> / </tool_call>
var dsmlToolCallsOpenRe = regexp.MustCompile(`(?s)<\s*tool_calls\s*>`)
var dsmlToolCallsCloseRe = regexp.MustCompile(`(?s)<\s*/\s*tool_calls\s*>`)

// <invoke name="x"> -> <function name="x">，以及 </invoke> -> </function>
var dsmlInvokeOpenRe = regexp.MustCompile(`(?s)<\s*invoke\s+name\s*=\s*"([^"]*)"\s*>`)
var dsmlInvokeCloseRe = regexp.MustCompile(`(?s)<\s*/\s*invoke\s*>`)

// 修复畸形：<function ...>内容</parameter>  ->  <function ...>内容</function>
var dsmlFuncCloseRe = regexp.MustCompile(`(?s)<function\b([^>]*)>([\s\S]*?)</parameter>`)

// 去除 function 的双闭合残留（畸形修复 + invoke 转换可能叠加产生）
var dsmlDupCloseRe = regexp.MustCompile(`(?s)</function>\s*</function>`)

// extractBareToolJSON 从正文里提取形如 {"action":"tool",...} 的裸 JSON 工具调用对象。
// 相比正则 [^{}]*，这里用括号计数（忽略字符串内的花括号）以支持嵌套对象，
// 例如 {"action":"tool","arguments":{"connId":...}}。返回所有候选 JSON 字符串（待后续 json.Unmarshal 校验）。
type toolAction struct {
	Action    string                 `json:"action"`
	Server    string                 `json:"server"`
	Tool      string                 `json:"tool"`
	Arguments map[string]interface{} `json:"arguments"`
}

// UnmarshalJSON 兼容 arguments 被写成「JSON 字符串」的畸形形态。
//
// 提示词里「参数值如需为对象/数组，请写成 JSON 字符串」常被模型误解成把整个
// arguments 序列化成一个字符串：
func (a *toolAction) UnmarshalJSON(b []byte) error {
	// 先按原始结构解析 arguments 之外的字段，避免递归
	var raw struct {
		Action    string          `json:"action"`
		Server    string          `json:"server"`
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	a.Action, a.Server, a.Tool = raw.Action, raw.Server, raw.Tool
	a.Arguments = map[string]interface{}{}

	argStr := strings.TrimSpace(string(raw.Arguments))
	if argStr == "" || argStr == "null" {
		return nil
	}
	// 形态一：正常对象
	if argStr[0] == '{' {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(argStr), &m); err == nil {
			a.Arguments = m
		}
		return nil
	}
	// 形态二：被序列化成字符串
	var s string
	if err := json.Unmarshal([]byte(argStr), &s); err == nil {
		s = strings.TrimSpace(s)
		if strings.HasPrefix(s, "{") {
			var m map[string]interface{}
			if err := json.Unmarshal([]byte(s), &m); err == nil {
				a.Arguments = m
			}
		}
	}
	return nil
}

func extractBareToolJSON(text string) []string {
	var out []string
	for _, loc := range toolJSONProbeRe.FindAllStringIndex(text, -1) {
		// 从 "action":"tool" 位置向前找匹配的起始 '{'
		start := strings.LastIndex(text[:loc[0]], "{")
		if start < 0 {
			continue
		}
		// 从 start 向右括号计数，匹配到与起始 '{' 对应的 '}'
		var depth int
		inStr := false
		var esc bool
		matched := -1
		for i := start; i < len(text); i++ {
			c := text[i]
			if esc {
				esc = false
				continue
			}
			if inStr {
				if c == '\\' {
					esc = true
				} else if c == '"' {
					inStr = false
				}
				continue
			}
			switch c {
			case '"':
				inStr = true
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					matched = i
					goto done
				}
			}
		}
	done:
		if matched < 0 {
			continue
		}
		out = append(out, text[start:matched+1])
	}
	return out
}

// 兼容 OpenAI function-call 风格的 <tool_call>/<function>/<parameter> 标签。
// 支持多种变体：
//   - <tool_call><function>name</function><parameter name="k">v</parameter></tool_call>
//   - <tool_call><function name="name"><parameter .../></function></tool_call>
//   - 裸 <function>name</function> 或 <function name="name">...</function>

// normalizeLooseCalls 宽松收敛工具调用外层标签并补齐缺失的闭合标签。
// 单独抽成函数，避免在调用点直接书写带零宽字符的标签字面量。
func normalizeLooseCalls(text string) string {
	text = looseCallsCloseRe.ReplaceAllString(text, "</\u200btool_call>")
	text = looseCallsOpenRe.ReplaceAllString(text, "<\u200btool_call>")
	return closeUnclosedToolCall2(text)
}

// looseCallsOpenRe / looseCallsCloseRe 宽松匹配工具调用外层标签的各种畸形变体：
//
//	<tool_calls>  <function_calls>  < calls>  <｜｜DSML｜｜ calls>  <tool_call >
//
// 允许残留的 DSML 片段与任意空白，统一收敛为标准的单数形式。
// 模型输出这类变体时若不能识别，会导致整个工具调用被当成正文丢弃。

// closeUnclosedToolCall2 为未闭合的外层标签补上收尾。
// 模型常常只写开标签就结束输出（流式截断尤其常见），缺闭合会让后续参数被当成正文。
func closeUnclosedToolCall2(text string) string {
	const open = "<\u200btool_call>"
	const close = "</\u200btool_call>"
	o, c := strings.Count(text, open), strings.Count(text, close)
	if o <= c {
		return text
	}
	return text + strings.Repeat(close, o-c)
}

// toolCallMarkers 流式输出中可能出现的「工具调用起始标记」。
// hit=true 表示命中即确认为工具调用（标签形式，最终必被 stripTags 剥除）；
// hit=false 表示还需进一步判定（```json 代码块也可能是普通代码，不能误伤）。

// toolCallPartialTail 返回 s 末尾可能是某个工具调用起始标记前缀的字符数。
// 流式分片可能把 "<tool_call" 切成 "<tool_" + "call>"，这里把不完整的尾巴
// 留给下一个分片再判定，避免把半个标记当成正文推送出去。
func toolCallPartialTail(s string) int {
	best := 0
	for _, m := range toolCallMarkers {
		maxK := len(m.tag) - 1
		if maxK > len(s) {
			maxK = len(s)
		}
		for k := maxK; k > 0; k-- {
			if strings.HasSuffix(s, m.tag[:k]) {
				if k > best {
					best = k
				}
				break
			}
		}
	}
	return best
}

// fenceBufMax ```json 块缓冲上限：超过仍无法判定为工具调用时按普通内容补发，
// 避免异常输出把正文一直憋住不显示。

// stripLooseTags 去掉字符串中残留的工具调用标签，返回干净的值。
func stripLooseTags(s string) string {
	if !strings.Contains(s, "<") {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(looseTagRe.ReplaceAllString(s, ""))
}

// extractTagParams 从工具调用标签体中提取参数。
// 与「必须匹配 </parameter>」的严格写法不同，这里只依赖起始标签 + 边界截断，
// 因此能兼容参数标签未闭合、自闭合、以及值里混入后续标签残片等情况。

// extractTagParams 从工具调用标签体中提取参数。
// 与「必须匹配 </parameter>」的严格写法不同，这里只依赖起始标签 + 边界截断，
// 因此能兼容参数标签未闭合、自闭合、以及值里混入后续标签残片等情况。
func extractTagParams(block string) map[string]interface{} {
	args := map[string]interface{}{}
	locs := paramOpenRe.FindAllStringSubmatchIndex(block, -1)
	for i, loc := range locs {
		name := strings.TrimSpace(block[loc[2]:loc[3]])
		if name == "" {
			continue
		}
		valStart := loc[1]
		valEnd := len(block)
		if i+1 < len(locs) {
			// 下一个参数标签即为本次值的边界
			valEnd = locs[i+1][0]
		} else if m := paramBoundaryRe.FindStringIndex(block[valStart:]); m != nil {
			valEnd = valStart + m[0]
		}
		applyArg(args, name, stripLooseTags(block[valStart:valEnd]))
	}
	return args
}

// parseToolFromToolCall 解析 <tool_call>/<function>/<parameter> 标签格式。
func parseToolFromToolCall(text string) (*toolAction, bool) {
	// 先定位最内层可解析块：优先 <tool_call>，其次 <function_calls>，再次整段
	block := text
	if m := toolCallTagRe.FindStringSubmatch(text); len(m) >= 2 {
		block = m[1]
	} else if m := funcCallsRe.FindStringSubmatch(text); len(m) >= 2 {
		block = m[1]
	}

	// 提取函数名（兼容 name 在属性内、在标签体内、以及 <function=NAME> 等号/冒号形式）
	name := ""
	var funcBody string
	if m := toolCallFuncAttrRe.FindStringSubmatch(block); len(m) >= 3 {
		name = strings.TrimSpace(m[1])
		funcBody = m[2]
	} else if m := toolCallFuncRe.FindStringSubmatch(block); len(m) >= 2 {
		name = strings.TrimSpace(m[1])
		funcBody = block
	} else if m := funcLooseNameRe.FindStringSubmatch(block); len(m) >= 2 {
		// <function=db_query> / <function:db_query>：等号/冒号形式，函数名不带引号
		name = strings.TrimSpace(m[1])
		funcBody = block
	} else {
		// 兜底：模型常常不闭合 </function>（只闭合 parameter 与 tool_call），
		// 上面几个依赖 </function> 的写法会全部失配，这里按「起始标签即取名」解析。
		for _, re := range funcNameLooseRes {
			if m := re.FindStringSubmatch(block); len(m) >= 2 {
				name = strings.TrimSpace(m[1])
				break
			}
		}
		funcBody = block
	}
	if name == "" {
		// 模型有时整个漏掉函数名标签，只给出一串 <parameter name="...">。
		// 各工具的参数签名区分度很高，按参数推断比直接判失败可靠得多
		//（否则整轮工具调用作废，用户白等一轮还得重试）。
		if args := extractTagParams(block); len(args) > 0 {
			if tool := inferToolFromArgs(args); tool != "" {
				return &toolAction{Action: "tool", Server: "builtin", Tool: tool, Arguments: args}, true
			}
		}
		return nil, false
	}

	// 提取参数：容错解析（起始标签 + 边界截断），能救回未闭合 / 串味的参数。
	args := extractTagParams(block)
	if len(args) == 0 {
		// 无 parameter 标签时，函数体本身可能是 JSON 对象（如 <function>{"path":"/x"}</function>）
		if fb := strings.TrimSpace(funcBody); fb != "" {
			var jv map[string]interface{}
			if err := json.Unmarshal([]byte(fb), &jv); err == nil {
				for k, v := range jv {
					args[k] = v
				}
			}
		}
	}
	act := &toolAction{Action: "tool", Server: "builtin", Tool: name, Arguments: args}
	return act, true
}

// inferToolFromArgs 在缺失函数名标签时，按参数签名推断工具名。
//
// 各内置工具的参数组合区分度很高（如 sql+database 只会是 db_query），
// 因此推断的可靠性较高。判定顺序即特异性由高到低，命中即返回；
// 无法判定时返回空，由调用方按解析失败处理。

// inferToolFromArgs 在缺失函数名标签时，按参数签名推断工具名。
//
// 各内置工具的参数组合区分度很高（如 sql+database 只会是 db_query），
// 因此推断的可靠性较高。判定顺序即特异性由高到低，命中即返回；
// 无法判定时返回空，由调用方按解析失败处理。
func inferToolFromArgs(args map[string]interface{}) string {
	if len(args) == 0 {
		return ""
	}
	has := func(keys ...string) bool {
		for _, k := range keys {
			if _, ok := args[k]; !ok {
				return false
			}
		}
		return true
	}
	// hasAny：任一参数存在即可（用于同一概念的多种别名，如 data/rows/csv）
	hasAny := func(keys ...string) bool {
		for _, k := range keys {
			if _, ok := args[k]; !ok {
				continue
			}
			// 参数存在但值为空时不算命中，避免空标签误判
			if s, isStr := args[k].(string); !isStr || strings.TrimSpace(s) != "" {
				return true
			}
		}
		return false
	}
	switch {
	// 数据库：sql 是 db_query 的强特征；tables 是 db_schema 的强特征
	case has("sql"):
		return "db_query"
	case has("tables"):
		return "db_schema"
	// 导出表格（data/rows/csv/markdown 互为别名，任一即可）
	case hasAny("data", "rows", "csv", "markdown"):
		return "export_table"
	// 文件类：content+path 是写文件；仅 path 先按读文件
	case has("path", "content"):
		return "write_file"
	case has("src", "dst"):
		return "rename_path"
	case has("path"):
		return "read_file"
	case has("query"):
		return "web_search"
	case has("expr"):
		return "calc"
	case has("command"):
		return "run_command"
	case has("timezone"):
		return "get_time"
	}
	// 无参数工具无法从参数区分，交由上层按名称匹配，这里不猜
	return ""
}

// applyArg 将单个参数值按 JSON 解析后写入 args。
//
// 关于对象值的处理：模型有时会把整包参数塞进一个容器型参数里
// （如 <parameter name="arguments">{"path":"/x"}</parameter>），需要展开合并。
// 但若对**所有**对象值都展开，正常传对象的参数会被拆散——
// 例如 <parameter name="content">{"k":1}</parameter> 会变成 args["k"]=1 而丢掉 content。
// 因此只对容器型参数名展开，其余保持原样。

// applyArg 将单个参数值按 JSON 解析后写入 args。
//
// 关于对象值的处理：模型有时会把整包参数塞进一个容器型参数里
// （如 <parameter name="arguments">{"path":"/x"}</parameter>），需要展开合并。
// 但若对**所有**对象值都展开，正常传对象的参数会被拆散——
// 例如 <parameter name="content">{"k":1}</parameter> 会变成 args["k"]=1 而丢掉 content。
// 因此只对容器型参数名展开，其余保持原样。
func applyArg(args map[string]interface{}, pname, pval string) {
	if pname == "" {
		return
	}
	var jv interface{}
	if err := json.Unmarshal([]byte(pval), &jv); err == nil {
		if m, ok := jv.(map[string]interface{}); ok && isContainerArg(pname) {
			// 容器参数（arguments/args）整体是 JSON 对象时展开合并
			for k, v := range m {
				args[k] = v
			}
			return
		}
		args[pname] = jv
		return
	}
	args[pname] = pval
}

// isContainerArg 判断参数名是否是「整包参数容器」。

// isContainerArg 判断参数名是否是「整包参数容器」。
func isContainerArg(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "arguments", "args", "参数", "arguments_json":
		return true
	}
	return false
}

// parseToolAction 从模型输出中提取工具调用。
// 兼容三种形式：
//  1. ```json {"action":"tool",...} ``` 代码块
//  2. 裸 JSON（含 "action":"tool"）
//  3. <tool_call><function>name</function><parameter name="x">v</parameter></tool_call> 标签（OpenAI 风格）

// parseToolAction 从模型输出中提取工具调用。
// 兼容三种形式：
//  1. ```json {"action":"tool",...} ``` 代码块
//  2. 裸 JSON（含 "action":"tool"）
//  3. <tool_call><function>name</function><parameter name="x">v</parameter></tool_call> 标签（OpenAI 风格）
func parseToolAction(text string) (*toolAction, bool) {
	text = normalizeToolTags(text)
	// 仅当文本确实包含工具调用标签时才走标签解析，避免普通文本误判。
	//
	// 这里必须用正则而非 strings.Contains("<tool_call")：标准标签里含零宽字符
	//（U+200B），字面量匹配永远不成立，会导致所有标签形式的调用都被跳过，
	// 进而误报「检测到工具调用标记但未能解析」。
	if paramOpenRe.MatchString(text) ||
		toolCallTagRe.MatchString(text) ||
		funcCallsRe.MatchString(text) ||
		funcLooseNameRe.MatchString(text) ||
		strings.Contains(text, "<function") ||
		strings.Contains(text, "<tool_call") {
		if act, ok := parseToolFromToolCall(text); ok {
			return act, true
		}
	}
	candidates := []string{}
	if m := toolJSONRe.FindStringSubmatch(text); len(m) >= 2 {
		candidates = append(candidates, m[1])
	}
	candidates = append(candidates, extractBareToolJSON(text)...)
	for _, c := range candidates {
		var act toolAction
		if err := json.Unmarshal([]byte(c), &act); err != nil {
			continue
		}
		// arguments 为字符串形态的兼容已由 toolAction.UnmarshalJSON 处理
		if act.Action != "tool" || act.Tool == "" {
			continue
		}
		if act.Server == "" {
			act.Server = "builtin" // 缺省归为内置（兼容旧模型）
		}
		return &act, true
	}
	return nil, false
}

// normalizeToolTags 规范化工具调用标签：模型/中间件会输出多种畸形格式，统一还原为
// 标准 <tool_call><function name="x">...</function><parameter name="k">v</parameter></tool_call>。
// 已覆盖的形态：
//  1. 带前缀：<｜｜DSML｜｜tool_call> / <｜｜DSML｜｜function>
//  2. 复数+invoke 风格（Claude 系）：<tool_calls><invoke name="x"><parameter .../></invoke></tool_calls>
//  3. 畸形闭合：<function>名</parameter>（function 被 parameter 错误闭合）

func normalizeToolTags(text string) string {
	// 先宽松收敛外层标签：<｜｜DSML｜｜ calls> / < calls> / <function_calls> 等
	// 一律规范为标准单数形式，后续规则才能正确识别。
	text = normalizeLooseCalls(text)
	if !strings.Contains(text, "DSML") &&
		!strings.Contains(text, "tool_calls") &&
		!strings.Contains(text, "tool_call") &&
		!strings.Contains(text, "<invoke") {
		return text
	}
	text = dsmlStripRe.ReplaceAllString(text, "")                              // 移除所有 ｜｜DSML｜｜ 前缀
	text = dsmlToolCallsOpenRe.ReplaceAllString(text, "<tool_call>")           // <tool_calls> -> <tool_call>
	text = dsmlToolCallsCloseRe.ReplaceAllString(text, "</tool_call>")         // </tool_calls> -> </tool_call>
	text = dsmlInvokeOpenRe.ReplaceAllString(text, "<function name=\"$1\">")   // <invoke name="x"> -> <function name="x">
	text = dsmlInvokeCloseRe.ReplaceAllString(text, "</function>")             // </invoke> -> </function>
	text = dsmlFuncCloseRe.ReplaceAllString(text, "<function$1>$2</function>") // 修复 <function>..</parameter> 畸形
	text = dsmlDupCloseRe.ReplaceAllString(text, "</function>")                // 去掉重复的 </function>
	return text
}

func stripTags(text string) string {
	text = normalizeToolTags(text)
	text = closeUnclosedToolCall2(text)
	text = thinkingRe.ReplaceAllString(text, "")
	text = planRe.ReplaceAllString(text, "")
	text = toolJSONRe.ReplaceAllString(text, "")
	// 移除裸 JSON 工具调用（如 ｜｜DSML｜｜ 包裹的 {"action":"tool",...}），避免最终正文残留
	for _, j := range extractBareToolJSON(text) {
		text = strings.Replace(text, j, "", 1)
	}
	text = funcCallsRe.ReplaceAllString(text, "")        // 移除 <function_calls>...</function_calls>
	text = toolCallTagRe.ReplaceAllString(text, "")      // 移除 <tool_call>...</tool_call> 标签
	text = toolCallFuncAttrRe.ReplaceAllString(text, "") // 移除 <function name="x">...</function>
	text = toolCallFuncRe.ReplaceAllString(text, "")     // 移除残留 <function>...</function>
	text = toolCallParamRe.ReplaceAllString(text, "")    // 移除残留 <parameter>...</parameter>
	return strings.TrimSpace(text)
}

// RunAgent 执行一次完整的 Agent 对话（ReAct / Plan loop）。
