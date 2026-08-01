package agent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"apitool/internal/ai"
	"apitool/internal/util"
)

// RunAgentArgs 前端发起一次 Agent 对话的入参。
type RunAgentArgs struct {
	Input   string      `json:"input"`
	BaseURL string      `json:"baseUrl"`
	APIKey  string      `json:"apiKey"`
	Model   string      `json:"model"`
	Timeout int         `json:"timeoutSec"`
}

// RunAgentResult 一次对话的最终结果。
type RunAgentResult struct {
	Content  string      `json:"content"`
	Thinking string      `json:"thinking"`
	Steps    []AgentStep `json:"steps"`
	Plan     string      `json:"plan,omitempty"`
	Error    string      `json:"error,omitempty"`
	Usage    TokenUsage  `json:"usage"`
}

// llmCall 复用底层 OpenAI 兼容请求，返回原始文本，并写日志。
func (m *Manager) llmCall(args RunAgentArgs, messages []ai.ChatMessage, temperature float64, tag string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(args.BaseURL), "/")
	if base == "" {
		return "", fmt.Errorf("未配置 AI 接口地址（设置 → AI 配置）")
	}
	if args.APIKey == "" {
		return "", fmt.Errorf("未配置 AI API Key")
	}
	model := strings.TrimSpace(args.Model)
	if model == "" {
		model = "gpt-4o-mini"
	}
	url := base
	if !strings.HasSuffix(url, "/chat/completions") {
		if strings.HasSuffix(url, "/v1") {
			url += "/chat/completions"
		} else {
			url += "/v1/chat/completions"
		}
	}
	payload, _ := json.Marshal(ai.Request{Model: model, Messages: messages, Temperature: temperature, Stream: false})
	timeout := args.Timeout
	if timeout <= 0 {
		timeout = 60
	}
	client := &http.Client{Timeout: time.Duration(timeout) * time.Second}
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+args.APIKey)

	start := time.Now()
	m.appendLog(AgentLog{Level: "request", Category: "llm", Title: "LLM 请求: " + tag, Detail: "模型: " + model + "\n消息数: " + fmt.Sprint(len(messages))})
	resp, err := client.Do(req)
	if err != nil {
		m.appendLog(AgentLog{Level: "error", Category: "llm", Title: "LLM 请求失败: " + tag, Detail: err.Error()})
		return "", fmt.Errorf("AI 请求失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	dur := time.Since(start).Milliseconds()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		m.appendLog(AgentLog{Level: "error", Category: "llm", Title: "LLM 响应异常: " + tag, Detail: fmt.Sprintf("%d: %s", resp.StatusCode, util.Truncate(string(body), 2000)), DurationMs: dur})
		return "", fmt.Errorf("AI 请求失败 %d: %s", resp.StatusCode, string(body))
	}
	var r ai.Result
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}
	if len(r.Choices) == 0 {
		return "", fmt.Errorf("AI 返回内容为空")
	}
	out := r.Choices[0].Message.Content
	m.appendLog(AgentLog{Level: "response", Category: "llm", Title: "LLM 响应: " + tag, Detail: util.Truncate(out, 3000), DurationMs: dur})
	return out, nil
}

// streamDelta 描述一次流式回调的增量：区分是思考区还是正文区。
type streamDelta struct {
	Text     string // 本次增量文本
	Thinking bool   // 是否属于 <thinking> 区段
}

// llmCallStream 以流式（SSE）方式请求 LLM。
// onDelta 会在收到每个增量文本时被调用（已根据 <thinking> 标签拆分区段），
// 返回累计的完整原始文本（含标签），供上层解析工具调用/思考/正文。
func (m *Manager) llmCallStream(args RunAgentArgs, messages []ai.ChatMessage, temperature float64, tag string, onDelta func(streamDelta), onUsage func(TokenUsage)) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(args.BaseURL), "/")
	if base == "" {
		return "", fmt.Errorf("未配置 AI 接口地址（设置 → AI 配置）")
	}
	if args.APIKey == "" {
		return "", fmt.Errorf("未配置 AI API Key")
	}
	model := strings.TrimSpace(args.Model)
	if model == "" {
		model = "gpt-4o-mini"
	}
	url := base
	if !strings.HasSuffix(url, "/chat/completions") {
		if strings.HasSuffix(url, "/v1") {
			url += "/chat/completions"
		} else {
			url += "/v1/chat/completions"
		}
	}
	payload, _ := json.Marshal(ai.Request{
		Model:         model,
		Messages:      messages,
		Temperature:   temperature,
		Stream:        true,
		StreamOptions: &ai.StreamOptions{IncludeUsage: true},
	})
	timeout := args.Timeout
	if timeout <= 0 {
		timeout = 120
	}
	client := &http.Client{Timeout: time.Duration(timeout) * time.Second}
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+args.APIKey)
	req.Header.Set("Accept", "text/event-stream")

	start := time.Now()
	m.appendLog(AgentLog{Level: "request", Category: "llm", Title: "LLM 流式请求: " + tag, Detail: "模型: " + model + "\n消息数: " + fmt.Sprint(len(messages))})
	resp, err := client.Do(req)
	if err != nil {
		m.appendLog(AgentLog{Level: "error", Category: "llm", Title: "LLM 流式请求失败: " + tag, Detail: err.Error()})
		return "", fmt.Errorf("AI 请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		m.appendLog(AgentLog{Level: "error", Category: "llm", Title: "LLM 流式响应异常: " + tag, Detail: fmt.Sprintf("%d: %s", resp.StatusCode, util.Truncate(string(body), 2000))})
		return "", fmt.Errorf("AI 请求失败 %d: %s", resp.StatusCode, string(body))
	}

	var full strings.Builder    // 完整原始文本（含标签）
	inThinking := false         // 当前是否在 <thinking> 内
	var pending strings.Builder // 用于跨分片检测标签的缓冲

	// flush 将 pending 中已可确定归属的文本按区段推送给 onDelta。
	// 逐字符扫描，遇到 <thinking>/</thinking> 切换区段。
	emit := func(s string, thinking bool) {
		if s == "" || onDelta == nil {
			return
		}
		onDelta(streamDelta{Text: s, Thinking: thinking})
	}
	processChunk := func(chunk string) {
		full.WriteString(chunk)
		pending.WriteString(chunk)
		buf := pending.String()
		pending.Reset()
		for len(buf) > 0 {
			var marker string
			if inThinking {
				marker = "</thinking>"
			} else {
				marker = "<thinking>"
			}
			idx := strings.Index(buf, marker)
			if idx >= 0 {
				// 输出标记之前的内容
				emit(buf[:idx], inThinking)
				buf = buf[idx+len(marker):]
				inThinking = !inThinking
				continue
			}
			// 没有完整标记：检查是否有可能是被截断的标记前缀，若有则留到下次
			keep := partialTailLen(buf, marker)
			if keep > 0 {
				emit(buf[:len(buf)-keep], inThinking)
				pending.WriteString(buf[len(buf)-keep:])
			} else {
				emit(buf, inThinking)
			}
			break
		}
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		// 注意：OpenAI/兼容服务返回的 usage 字段名为蛇形（prompt_tokens 等），
		// 与 TokenUsage 的驼峰 tag 不同，这里用专用 struct 解析后再映射。
		var ev struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
			Usage struct {
				PromptTokens     int64 `json:"prompt_tokens"`
				CompletionTokens int64 `json:"completion_tokens"`
				TotalTokens      int64 `json:"total_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}
		// usage 可能出现在任意分片（OpenAI 通常放在最后一个带 choices/finish_reason 的事件里，
		// 部分兼容服务则放在独立的空 choices 事件），只要解析到有效 usage 就回调。
		if ev.Usage.TotalTokens > 0 && onUsage != nil {
			onUsage(TokenUsage{
				PromptTokens:     ev.Usage.PromptTokens,
				CompletionTokens: ev.Usage.CompletionTokens,
				TotalTokens:      ev.Usage.TotalTokens,
			})
		}
		if len(ev.Choices) == 0 {
			continue
		}
		if c := ev.Choices[0].Delta.Content; c != "" {
			processChunk(c)
		}
	}
	// 收尾：把 pending 剩余内容输出
	if pending.Len() > 0 {
		emit(pending.String(), inThinking)
	}
	if err := scanner.Err(); err != nil {
		m.appendLog(AgentLog{Level: "error", Category: "llm", Title: "LLM 流式读取中断: " + tag, Detail: err.Error()})
		if full.Len() == 0 {
			return "", fmt.Errorf("AI 流式读取失败: %w", err)
		}
	}
	out := full.String()
	dur := time.Since(start).Milliseconds()
	m.appendLog(AgentLog{Level: "response", Category: "llm", Title: "LLM 流式响应: " + tag, Detail: util.Truncate(out, 3000), DurationMs: dur})
	return out, nil
}

// partialTailLen 返回 buf 末尾可能是 marker 前缀的长度（用于跨分片保留半个标记）。
func partialTailLen(buf, marker string) int {
	max := len(marker) - 1
	if max > len(buf) {
		max = len(buf)
	}
	for n := max; n > 0; n-- {
		if strings.HasPrefix(marker, buf[len(buf)-n:]) {
			return n
		}
	}
	return 0
}

// buildToolsPrompt 根据可用 MCP 工具与技能构造系统提示，指导模型用 JSON 协议调用工具。
func buildToolsPrompt(tools []MCPTool, skills []AgentSkill, mode string) string {
	var sb strings.Builder
	if len(skills) > 0 {
		sb.WriteString("\n\n## 可用技能(Skill)\n遇到匹配场景时，请在思考中说明将使用哪个技能，并遵循其指引：\n")
		for _, s := range skills {
			if !s.Enabled {
				continue
			}
			sb.WriteString(fmt.Sprintf("- [%s] %s\n", s.Name, s.Description))
		}
	}
	if len(tools) > 0 {
		sb.WriteString("\n\n## 可用工具(Tool)\n你可以调用以下工具。调用工具时，请使用如下两种格式之一（任选，不要混用、且每轮最多一个工具调用）：\n")
		sb.WriteString("格式A（标签，推荐）：\n")
		sb.WriteString("<tool_call>\n<function>工具名</function>\n<parameter name=\"参数名\">参数值</parameter>\n</tool_call>\n")
		sb.WriteString("格式B（JSON）：\n")
		sb.WriteString("```json\n{\"action\":\"tool\",\"server\":\"<服务器ID>\",\"tool\":\"<工具名>\",\"arguments\":{...}}\n```\n")
		sb.WriteString("说明：内置工具 server 固定为 \"builtin\"（无需 MCP 服务器，本地直接执行）。参数值如需为对象/数组，请写成 JSON 字符串。\n")
		sb.WriteString("工具列表：\n")
		for _, t := range tools {
			sb.WriteString(fmt.Sprintf("- server=%s tool=%s: %s\n", t.Server, t.Name, t.Description))
			if len(t.InputSchema) > 0 {
				sb.WriteString(fmt.Sprintf("  parameters: %s\n", string(t.InputSchema)))
			}
		}
		sb.WriteString("\n当你获得足够信息、无需继续调用工具时，直接输出最终答案（不要再输出工具调用）。\n")
	}
	sb.WriteString("\n## 输出要求\n")
	sb.WriteString("- 请先在 <thinking>...</thinking> 标签内写出你的思考过程，然后再给出正式回答。\n")
	if mode == "plan" {
		sb.WriteString("- 你处于 Plan 模式：先在 <plan>...</plan> 中列出分步计划，再逐步执行。\n")
	}
	return sb.String()
}

var thinkingRe = regexp.MustCompile(`(?s)<thinking>(.*?)</thinking>`)
var planRe = regexp.MustCompile(`(?s)<plan>(.*?)</plan>`)
var toolJSONRe = regexp.MustCompile("(?s)```json\\s*(\\{.*?\\})\\s*```")
var toolJSONReBare = regexp.MustCompile(`(?s)\{[^{}]*"action"\s*:\s*"tool"[^{}]*\}`)
// 兼容 OpenAI function-call 风格的 <tool_call>/<function>/<parameter> 标签。
// 支持多种变体：
//   - <tool_call><function>name</function><parameter name="k">v</parameter></tool_call>
//   - <tool_call><function name="name"><parameter .../></function></tool_call>
//   - 裸 <function>name</function> 或 <function name="name">...</function>
var toolCallTagRe = regexp.MustCompile(`(?s)<tool_call>(.*?)</tool_call>`)
// 函数名在标签体内：<function>name</function>
var toolCallFuncRe = regexp.MustCompile(`(?s)<function\s*>\s*(.*?)\s*</function>`)
// 函数名在属性里：<function name="name">...</function>
var toolCallFuncAttrRe = regexp.MustCompile(`(?s)<function\s+name\s*=\s*"([^"]*)"\s*>(.*?)</function>`)
var toolCallParamRe = regexp.MustCompile(`(?s)<parameter\s+name\s*=\s*"([^"]*)"\s*>(.*?)</parameter>`)
// 外层可能包 <function_calls> ... </function_calls>
var funcCallsRe = regexp.MustCompile(`(?s)<function_calls>(.*?)</function_calls>`)

type toolAction struct {
	Action    string                 `json:"action"`
	Server    string                 `json:"server"`
	Tool      string                 `json:"tool"`
	Arguments map[string]interface{} `json:"arguments"`
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

	// 提取函数名（兼容 name 在属性内 与 在标签体内两种写法）
	name := ""
	var funcBody string
	if m := toolCallFuncAttrRe.FindStringSubmatch(block); len(m) >= 3 {
		name = strings.TrimSpace(m[1])
		funcBody = m[2]
	} else if m := toolCallFuncRe.FindStringSubmatch(block); len(m) >= 2 {
		name = strings.TrimSpace(m[1])
		funcBody = block
	}
	if name == "" {
		return nil, false
	}

	// 提取参数。优先从 <parameter> 标签解析；若无 parameter 标签但函数体里有
	// 类似 JSON 的内容，则直接解析函数体。
	args := map[string]interface{}{}
	params := toolCallParamRe.FindAllStringSubmatch(block, -1)
	if len(params) > 0 {
		for _, pm := range params {
			pname := strings.TrimSpace(pm[1])
			pval := strings.TrimSpace(pm[2])
			applyArg(args, pname, pval)
		}
	} else if fb := strings.TrimSpace(funcBody); fb != "" {
		// 函数体本身可能是 JSON 对象（如 <function>{"path":"/x"}</function>）
		var jv map[string]interface{}
		if err := json.Unmarshal([]byte(fb), &jv); err == nil {
			for k, v := range jv {
				args[k] = v
			}
		}
	}
	act := &toolAction{Action: "tool", Server: "builtin", Tool: name, Arguments: args}
	return act, true
}

// applyArg 将单个参数值按 JSON 解析后写入 args；若解析为对象则展开合并，否则标量。
func applyArg(args map[string]interface{}, pname, pval string) {
	if pname == "" {
		return
	}
	var jv interface{}
	if err := json.Unmarshal([]byte(pval), &jv); err == nil {
		if m, ok := jv.(map[string]interface{}); ok {
			// 参数整体是 JSON 对象（如 arguments={...}）时展开合并
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

// parseToolAction 从模型输出中提取工具调用。
// 兼容三种形式：
//  1. ```json {"action":"tool",...} ``` 代码块
//  2. 裸 JSON（含 "action":"tool"）
//  3. <tool_call><function>name</function><parameter name="x">v</parameter></tool_call> 标签（OpenAI 风格）
func parseToolAction(text string) (*toolAction, bool) {
	// 仅当文本确实包含工具调用标签时才走标签解析，避免普通文本误判
	if strings.Contains(text, "<tool_call") || strings.Contains(text, "<function") {
		if act, ok := parseToolFromToolCall(text); ok {
			return act, true
		}
	}
	candidates := []string{}
	if m := toolJSONRe.FindStringSubmatch(text); len(m) >= 2 {
		candidates = append(candidates, m[1])
	}
	if m := toolJSONReBare.FindString(text); m != "" {
		candidates = append(candidates, m)
	}
	for _, c := range candidates {
		var act toolAction
		if err := json.Unmarshal([]byte(c), &act); err != nil {
			continue
		}
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

func stripTags(text string) string {
	text = thinkingRe.ReplaceAllString(text, "")
	text = planRe.ReplaceAllString(text, "")
	text = toolJSONRe.ReplaceAllString(text, "")
	text = funcCallsRe.ReplaceAllString(text, "")        // 移除 <function_calls>...</function_calls>
	text = toolCallTagRe.ReplaceAllString(text, "")      // 移除 <tool_call>...</tool_call> 标签
	text = toolCallFuncAttrRe.ReplaceAllString(text, "") // 移除 <function name="x">...</function>
	text = toolCallFuncRe.ReplaceAllString(text, "")      // 移除残留 <function>...</function>
	text = toolCallParamRe.ReplaceAllString(text, "")    // 移除残留 <parameter>...</parameter>
	return strings.TrimSpace(text)
}

// emitEvent 向前端推送 agent 运行事件（实时展示步骤/思考）。
func (m *Manager) emitEvent(name string, payload interface{}) {
	if m.ctx != nil {
		m.b.Emit( name, payload)
	}
}

// RunAgent 执行一次完整的 Agent 对话（ReAct / Plan loop）。
func (m *Manager) RunAgent(args RunAgentArgs) RunAgentResult {
	d := m.readAgentData()
	cfg := d.Config
	userCtx := m.mcpUserContext(cfg, d.Users)

	// 收集可用工具（启用的 MCP 服务器 + 启用的内置工具）
	var tools []MCPTool
	srvByID := map[string]MCPServer{}
	for _, srv := range d.Servers {
		srvByID[srv.ID] = srv
		if !srv.Enabled {
			continue
		}
		ts, err := m.listMCPTools(srv, userCtx)
		if err != nil {
			m.appendLog(AgentLog{Level: "error", Category: "mcp", Title: "加载工具失败: " + srv.Name, Detail: err.Error()})
			continue
		}
		tools = append(tools, ts...)
	}
	// 内置工具（本地执行，受开关控制）
	builtinTools := collectBuiltinTools(cfg.Tools.Enabled, cfg.Tools.Desc)
	tools = append(tools, builtinTools...)

	// 组装系统提示
	sysPrompt := cfg.SystemPrompt + buildToolsPrompt(tools, d.Skills, cfg.Mode)
	if userCtx != nil {
		sysPrompt += fmt.Sprintf("\n\n## 当前登录用户\n%s（调用工具时会自动携带其身份用于权限校验）", toJSON(userCtx))
	}

	messages := []ai.ChatMessage{{Role: "system", Content: sysPrompt}}
	// 加载当前激活会话的历史上下文（关键：必须用当前会话，而非顶层 Messages，否则会串到别的会话）
	hist := []AgentMsg{}
	if sess := d.activeSession(); sess != nil {
		hist = sess.Messages
	}
	if cfg.ContextLimit > 0 && len(hist) > cfg.ContextLimit {
		hist = hist[len(hist)-cfg.ContextLimit:]
	}
	for _, m := range hist {
		if m.Role == "user" || m.Role == "assistant" {
			messages = append(messages, ai.ChatMessage{Role: m.Role, Content: m.Content})
		}
	}
	messages = append(messages, ai.ChatMessage{Role: "user", Content: args.Input})

	m.appendLog(AgentLog{Level: "info", Category: "agent", Title: "开始运行 Agent", Detail: fmt.Sprintf("模式=%s 最大轮数=%d 上下文=%d 工具数=%d 技能数=%d\n输入: %s", cfg.Mode, cfg.MaxLoops, cfg.ContextLimit, len(tools), len(d.Skills), util.Truncate(args.Input, 500)), UserID: cfg.CurrentUserID})

	result := RunAgentResult{}
	var thinkingAll strings.Builder
	var planText string

	// token 累加器（同时累计到全局与当前会话）
	var accUsage TokenUsage
	addUsage := func(u TokenUsage) {
		accUsage.PromptTokens += u.PromptTokens
		accUsage.CompletionTokens += u.CompletionTokens
		accUsage.TotalTokens += u.TotalTokens
		result.Usage = accUsage
	}

	// 记录哪些技能被提及（简单命中：模型思考中出现技能名）
	markSkills := func(text string) {
		for _, s := range d.Skills {
			if s.Enabled && s.Name != "" && strings.Contains(text, s.Name) {
				result.Steps = append(result.Steps, AgentStep{Type: "skill", Name: s.Name})
				m.b.Emit("agent:step", AgentStep{Type: "skill", Name: s.Name})
			}
		}
	}

	loops := cfg.MaxLoops
	if loops <= 0 {
		loops = 6
	}
	for i := 0; i < loops; i++ {
		// 通知前端：新一轮流式输出开始
		m.b.Emit("agent:loop-start", map[string]interface{}{"loop": i + 1})
		out, err := m.llmCallStream(args, messages, cfg.Temperature, fmt.Sprintf("loop-%d", i+1), func(dc streamDelta) {
			// 实时把增量推给前端（区分思考区/正文区），实现打字机效果
			m.b.Emit("agent:delta", map[string]interface{}{"text": dc.Text, "thinking": dc.Thinking})
		}, func(u TokenUsage) {
			addUsage(u)
		})
		if err != nil {
			result.Error = err.Error()
			return result
		}
		// 抽取思考 / 计划
		if mm := thinkingRe.FindStringSubmatch(out); len(mm) > 1 {
			th := strings.TrimSpace(mm[1])
			thinkingAll.WriteString(th + "\n")
			markSkills(th)
			m.b.Emit("agent:thinking", th)
			result.Steps = append(result.Steps, AgentStep{Type: "thought", Name: "思考", Output: th})
		}
		if mm := planRe.FindStringSubmatch(out); len(mm) > 1 {
			planText = strings.TrimSpace(mm[1])
			result.Plan = planText
			m.b.Emit("agent:plan", planText)
			result.Steps = append(result.Steps, AgentStep{Type: "plan", Name: "计划", Output: planText})
		}

		// 是否有工具调用
		act, has := parseToolAction(out)
		if !has {
			// 无工具调用 = 最终答案
			result.Content = stripTags(out)
			result.Thinking = strings.TrimSpace(thinkingAll.String())
			break
		}
		// 执行工具
		step := AgentStep{Type: "tool", Name: act.Tool, Server: act.Server, Input: toJSON(act.Arguments)}
		m.b.Emit("agent:step", AgentStep{Type: "tool", Name: act.Tool, Server: act.Server, Input: step.Input})
		var toolOut string
		var terr error
		if act.Server == "builtin" {
			// 内置工具：本地执行
			toolOut, terr = m.execBuiltinTool(act.Tool, act.Arguments, cfg.MaxFileRead)
		} else {
			srv, ok := srvByID[act.Server]
			if !ok {
				// 兼容：按工具名匹配所属服务器
				for _, t := range tools {
					if t.Name == act.Tool {
						srv = srvByID[t.Server]
						ok = true
						break
					}
				}
			}
			if !ok {
				step.Error = "未找到工具所属服务器"
				result.Steps = append(result.Steps, step)
				messages = append(messages, ai.ChatMessage{Role: "assistant", Content: out})
				messages = append(messages, ai.ChatMessage{Role: "user", Content: "工具调用失败：未找到服务器 " + act.Server + "，请直接给出答案或换用其他方式。"})
				continue
			}
			step.Server = srv.Name
			toolOut, terr = m.callMCPTool(srv, act.Tool, act.Arguments, userCtx)
		}
		if terr != nil {
			step.Error = terr.Error()
			result.Steps = append(result.Steps, step)
			messages = append(messages, ai.ChatMessage{Role: "assistant", Content: out})
			messages = append(messages, ai.ChatMessage{Role: "user", Content: "工具执行出错：" + terr.Error() + "。请调整或直接回答。"})
			continue
		}
		step.Output = util.Truncate(toolOut, cfg.MaxToolOutput)
		result.Steps = append(result.Steps, step)
		m.b.Emit("agent:step", step)
		// 把工具结果回灌给模型
		messages = append(messages, ai.ChatMessage{Role: "assistant", Content: out})
		messages = append(messages, ai.ChatMessage{Role: "user", Content: fmt.Sprintf("工具 %s 返回结果：\n%s\n请基于此继续。", act.Tool, toolOut)})

		// 最后一轮仍在调用工具，强制收尾
		if i == loops-1 {
			finalMsgs := append(messages, ai.ChatMessage{Role: "user", Content: "已达最大轮数，请基于以上信息直接给出最终答案。"})
			m.b.Emit("agent:loop-start", map[string]interface{}{"loop": loops + 1, "final": true})
			out2, err := m.llmCallStream(args, finalMsgs, cfg.Temperature, "final", func(dc streamDelta) {
				m.b.Emit("agent:delta", map[string]interface{}{"text": dc.Text, "thinking": dc.Thinking})
			}, func(u TokenUsage) {
				addUsage(u)
			})
			if err == nil {
				result.Content = stripTags(out2)
			}
		}
	}

	if result.Content == "" {
		result.Content = "（未生成有效回答，请检查工具配置或增大 loop 轮数）"
	}

	// AI 润色（可选）
	if cfg.EnablePolish && result.Content != "" {
		polishSys := "你是文字润色助手，请在不改变原意与技术细节的前提下，使下面内容表达更清晰、专业、结构更好。若含代码/表格/图表请保留。直接输出润色后的正文。"
		// 通知前端进入润色，重置正文流
		m.b.Emit("agent:polish-start", nil)
		polished, err := m.llmCallStream(args, []ai.ChatMessage{{Role: "system", Content: polishSys}, {Role: "user", Content: result.Content}}, 0.4, "polish", func(dc streamDelta) {
			m.b.Emit("agent:delta", map[string]interface{}{"text": dc.Text, "thinking": dc.Thinking})
		}, func(u TokenUsage) {
			addUsage(u)
		})
		if err == nil && strings.TrimSpace(polished) != "" {
			result.Content = strings.TrimSpace(polished)
		}
	}

	result.Thinking = strings.TrimSpace(thinkingAll.String())

	// 保存会话（写入当前激活会话）
	now := time.Now().Format("2006-01-02 15:04:05")
	d = m.readAgentData()
	sess := d.activeSession()
	if sess == nil {
		// 兜底：新建
		id := m.CreateAgentSession("默认会话")
		d = m.readAgentData()
		sess = d.activeSession()
		_ = id
	}
	sess.Messages = append(sess.Messages,
		AgentMsg{ID: agentID("msg"), Role: "user", Content: args.Input, Time: now},
		AgentMsg{ID: agentID("msg"), Role: "assistant", Content: result.Content, Thinking: result.Thinking, Steps: result.Steps, Time: now},
	)
	if sess.Title == "" || sess.Title == "新会话" || sess.Title == "默认会话" {
		sess.Title = util.Truncate(args.Input, 30)
	}
	sess.UpdatedAt = time.Now().Format(time.RFC3339)
	// 累计 token 到会话与全局
	sess.Usage.PromptTokens += accUsage.PromptTokens
	sess.Usage.CompletionTokens += accUsage.CompletionTokens
	sess.Usage.TotalTokens += accUsage.TotalTokens
	d.Usage.PromptTokens += accUsage.PromptTokens
	d.Usage.CompletionTokens += accUsage.CompletionTokens
	d.Usage.TotalTokens += accUsage.TotalTokens
	_ = m.writeAgentData(d)

	m.b.Emit("agent:done", map[string]interface{}{"content": result.Content, "thinking": result.Thinking, "usage": accUsage})
	m.appendLog(AgentLog{Level: "info", Category: "agent", Title: "Agent 运行结束", Detail: fmt.Sprintf("步骤数=%d 输出长度=%d token=%d", len(result.Steps), len(result.Content), accUsage.TotalTokens), UserID: cfg.CurrentUserID})
	return result
}

// PolishText 独立的 AI 润色接口（供输入框"润色"按钮使用）。
func (m *Manager) PolishText(args RunAgentArgs) (string, error) {
	sys := "你是文字润色与提示词优化助手。请优化下面这段用户输入，使其作为给 AI 的指令更清晰、完整、无歧义。直接输出优化后的文本，不要解释。"
	return m.llmCall(args, []ai.ChatMessage{{Role: "system", Content: sys}, {Role: "user", Content: args.Input}}, 0.5, "polish-input")
}
