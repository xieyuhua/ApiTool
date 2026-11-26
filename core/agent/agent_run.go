package agent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"apitool/internal/ai"
	"apitool/internal/util"
)

// RunAgentArgs 前端发起一次 Agent 对话的入参。
type RunAgentArgs struct {
	Input     string `json:"input"`
	BaseURL   string `json:"baseUrl"`
	APIKey    string `json:"apiKey"`
	Model     string `json:"model"`
	Timeout   int    `json:"timeoutSec"`
	MaxTokens int    `json:"maxTokens"` // 模型回复长度上限（0=模型默认）
	// ClientID 发起端标识（桌面端或局域网网页端各自随机生成并保持不变）。
	// 后端会把它回传到 agent:done / agent:sessions-changed 等事件里，
	// 前端据此判断「这次对话是不是我自己发起的」——
	// 自己发起的不重复刷新，别人（如网页端）发起的才刷新会话并提示，
	// 否则两端会互相覆盖对方正在展示的流式内容。
	ClientID string `json:"clientId,omitempty"`
	// SessionID 本次对话所属会话。桌面端与局域网网页端各自浏览不同会话时，
	// 若仍依赖后端那个全局共享的 ActiveSession 游标，读到的历史与写入的结果
	// 就会落到不同会话上（表现为「刚发的内容不见了」/「串到别的会话」）。
	// 前端传自己正在浏览的会话 ID，为空时才回退到全局游标。
	SessionID string `json:"sessionId,omitempty"`
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

// messagesLogText 把发往模型的请求消息序列化为可读文本，写入 request 级日志，
// 用于核对「到底把什么发给了模型」：系统提示词、历史上下文、本轮输入、
// 工具结果回灌的内容全部原样保留，不做静默截断；仅在整体超过 logDetailHardLimit
// （极端的超长工具结果）时才截断，并在末尾明确标注原始长度，避免"看着完整其实被截了"。
func messagesLogText(msgs []ai.ChatMessage) string {
	var sb strings.Builder
	for i, m := range msgs {
		role := m.Role
		if role == "" {
			role = "user"
		}
		label := role
		switch role {
		case "system":
			label = "system（系统提示词）"
		case "assistant":
			label = "assistant（模型上一轮输出 / 工具调用）"
		case "user":
			label = "user（用户输入 / 工具结果回灌）"
		}
		head := "【" + strconv.Itoa(i+1) + "/" + strconv.Itoa(len(msgs)) + "】" + label
		sb.WriteString(head + "\n")
		sb.WriteString(strings.Repeat("─", runeCount(head)+4) + "\n")
		if strings.TrimSpace(m.Content) == "" {
			sb.WriteString("(空内容)")
		} else {
			sb.WriteString(m.Content)
		}
		sb.WriteString("\n\n")
	}
	out := sb.String()
	if n := runeCount(out); n > logDetailHardLimit {
		r := []rune(out)
		out = string(r[:logDetailHardLimit]) +
			"\n\n…（内容过长已截断，完整长度 " + strconv.Itoa(n) + " 字符）"
	}
	return out
}

// requestLogHeader 生成 request 日志的参数摘要行，让「完整请求」具备上下文。
func requestLogHeader(model string, temperature float64, maxTokens int, stream bool, msgCount int) string {
	mt := "模型默认"
	if maxTokens > 0 {
		mt = strconv.Itoa(maxTokens)
	}
	streamTxt := "否"
	if stream {
		streamTxt = "是"
	}
	head := "请求参数　model=" + model +
		"　temperature=" + strconv.FormatFloat(temperature, 'f', -1, 64) +
		"　max_tokens=" + mt +
		"　stream=" + streamTxt +
		"　消息数=" + strconv.Itoa(msgCount) +
		"　总字符=" + "见下方正文"
	return head + "\n" + strings.Repeat("═", 60) + "\n\n"
}
// runeCount 按 Unicode 字符数统计（避免中文按字节被算成两倍）。
func runeCount(s string) int { return len([]rune(s)) }

// usagePtr 把本次调用的用量挂到日志上；全为 0（服务端未返回 usage）时返回 nil，
// 以便前端区分「没有消耗」与「服务端没给数据」。
func usagePtr(u TokenUsage) *TokenUsage {
	if u.PromptTokens == 0 && u.CompletionTokens == 0 && u.TotalTokens == 0 {
		return nil
	}
	c := u
	return &c
}

// usageLine 生成日志详情里的用量摘要行。
func usageLine(u TokenUsage) string {
	return fmt.Sprintf("Token 用量　输入 %d　输出 %d　合计 %d", u.PromptTokens, u.CompletionTokens, u.TotalTokens)
}

// streamReadErrHint 把流式读取的中断原因翻译成可操作的提示。
func streamReadErrHint(err error, timeout int) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, errStreamIdle) {
		return fmt.Sprintf("连接在 %d 秒内没有收到任何数据，判定为中断。请检查网络/代理是否稳定，或在「设置 → AI 配置」把超时调大。", timeout)
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "context deadline exceeded"),
		strings.Contains(msg, "Client.Timeout"),
		strings.Contains(msg, "timeout awaiting response headers"):
		return fmt.Sprintf("等待 AI 接口响应超时（%d 秒）。请检查接口地址是否可达、代理是否正常，或在「设置 → AI 配置」把超时调大。", timeout)
	case strings.Contains(msg, "unexpected EOF"),
		strings.Contains(msg, "connection reset by peer"),
		strings.Contains(msg, "broken pipe"):
		return "与 AI 接口的连接被中断（网络不稳定或服务端提前断开），请重试。"
	case strings.Contains(msg, "EOF"):
		return "AI 接口提前关闭了连接（服务端可能拒绝了该请求或发生错误），请查看日志中的响应内容。"
	}
	return msg
}

// llmCall 复用底层 OpenAI 兼容请求，返回原始文本，并写日志。
func (m *Manager) llmCall(args RunAgentArgs, messages []ai.ChatMessage, temperature float64, tag string, maxTokens int) (string, error) {
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
	payload, _ := json.Marshal(ai.Request{Model: model, Messages: messages, Temperature: temperature, MaxTokens: maxTokens, Stream: false})
	timeout := args.Timeout
	if timeout <= 0 {
		timeout = 60
	}
	client := &http.Client{Timeout: time.Duration(timeout) * time.Second}
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+args.APIKey)

	start := time.Now()
	m.appendLog(AgentLog{Level: "request", Category: "llm", Title: "LLM 请求: " + tag, Detail: requestLogHeader(model, temperature, maxTokens, false, len(messages)) + messagesLogText(messages)})
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
	// 非流式响应同样带 usage，一并记入日志，保证「每次调用的输入/输出」都可查
	var callUsage TokenUsage
	var u struct {
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
			TotalTokens      int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(body, &u) == nil {
		callUsage = TokenUsage{PromptTokens: u.Usage.PromptTokens, CompletionTokens: u.Usage.CompletionTokens, TotalTokens: u.Usage.TotalTokens}
	}
	detail := util.Truncate(out, 3000)
	if p := usagePtr(callUsage); p != nil {
		detail = usageLine(callUsage) + "\n\n" + detail
	}
	m.appendLog(AgentLog{Level: "response", Category: "llm", Title: "LLM 响应: " + tag, Detail: detail, DurationMs: dur, Usage: usagePtr(callUsage)})
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
// idleTimeoutBody 给流式响应体套一层「空闲超时」。
//
// 为什么不能用 http.Client.Timeout：它的计时覆盖「发请求 → 读完响应体」全过程。
// LLM 流式输出是持续很久的（模型思考 + 逐 token 输出长回答），
// 用它做总时限就必然报
//
//	context deadline exceeded (Client.Timeout or context cancellation while reading body)
//
// 而此时模型其实一切正常、正在输出。该错误只在「一段时间内一个字节都没收到」
// （服务端卡住、代理挂起、网络中断）时才有意义 —— 即空闲超时。
//
// 实现上每次 Read 都起一个带超时的等待；超时时主动 Close 底层 body，
// 让阻塞中的 goroutine 立刻返回，避免泄漏。
type idleTimeoutBody struct {
	r    io.ReadCloser
	idle time.Duration
}

// errStreamIdle 表示流式响应空闲超时。
var errStreamIdle = errors.New("流式响应空闲超时")

func (b *idleTimeoutBody) Read(p []byte) (int, error) {
	type result struct {
		n   int
		err error
	}
	ch := make(chan result, 1)
	go func() {
		n, err := b.r.Read(p)
		ch <- result{n, err}
	}()
	timer := time.NewTimer(b.idle)
	defer timer.Stop()
	select {
	case r := <-ch:
		return r.n, r.err
	case <-timer.C:
		// 关闭底层连接，使阻塞中的 Read 立刻返回，goroutine 得以退出
		_ = b.r.Close()
		return 0, fmt.Errorf("%w（%d 秒内未收到任何数据；可在「设置 → AI 配置」调大超时）",
			errStreamIdle, int(b.idle.Seconds()))
	}
}

func (b *idleTimeoutBody) Close() error { return b.r.Close() }

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
		MaxTokens:     args.MaxTokens,
		Stream:        true,
		StreamOptions: &ai.StreamOptions{IncludeUsage: true},
	})
	timeout := args.Timeout
	if timeout <= 0 {
		timeout = 180
	}
	// 流式请求不能用 http.Client.Timeout 限制总时长（它计时到读完响应体为止，
	// 而模型输出本来就可能持续好几分钟），否则正常输出也会被判超时。
	// 改为：Timeout=0 不限总时长，用 ResponseHeaderTimeout 限制「等待首字节」，
	// 再由 idleTimeoutBody 限制「两个数据块之间的静默间隔」。
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout: 15 * time.Second,
			}).DialContext,
			ResponseHeaderTimeout: time.Duration(timeout) * time.Second,
		},
	}
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+args.APIKey)
	req.Header.Set("Accept", "text/event-stream")

	start := time.Now()
	m.appendLog(AgentLog{Level: "request", Category: "llm", Title: "LLM 流式请求: " + tag, Detail: requestLogHeader(model, temperature, args.MaxTokens, true, len(messages)) + messagesLogText(messages)})
	resp, err := client.Do(req)
	if err != nil {
		m.appendLog(AgentLog{Level: "error", Category: "llm", Title: "LLM 流式请求失败: " + tag, Detail: err.Error()})
		return "", fmt.Errorf("AI 请求失败: %w", err)
	}
	defer resp.Body.Close()
	// 套上空闲超时：只要模型还在持续输出就一直正常，卡住（服务端无响应 /
	// 代理挂起 / 网络中断）超过 timeout 秒才报错，且报错带上可操作提示。
	resp.Body = &idleTimeoutBody{r: resp.Body, idle: time.Duration(timeout) * time.Second}
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

	// 正文流经 bodyFilter 过滤掉工具调用区段后再推送给前端
	bf := &bodyFilter{emit: func(s string) { emit(s, false) }}
	emitBody := bf.Write

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
				if inThinking {
					emit(buf[:idx], true)
				} else {
					emitBody(buf[:idx])
				}
				buf = buf[idx+len(marker):]
				inThinking = !inThinking
				continue
			}
			// 没有完整标记：检查是否有可能是被截断的标记前缀，若有则留到下次
			keep := partialTailLen(buf, marker)
			if keep > 0 {
				if inThinking {
					emit(buf[:len(buf)-keep], true)
				} else {
					emitBody(buf[:len(buf)-keep])
				}
				pending.WriteString(buf[len(buf)-keep:])
			} else {
				if inThinking {
					emit(buf, true)
				} else {
					emitBody(buf)
				}
			}
			break
		}
	}

	// callUsage 记录本次调用的用量（供日志展示）；跨分片时以最后一条有效 usage 为准。
	var callUsage TokenUsage
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
		if ev.Usage.TotalTokens > 0 {
			callUsage = TokenUsage{
				PromptTokens:     ev.Usage.PromptTokens,
				CompletionTokens: ev.Usage.CompletionTokens,
				TotalTokens:      ev.Usage.TotalTokens,
			}
			if onUsage != nil {
				onUsage(callUsage)
			}
		}
		if len(ev.Choices) == 0 {
			continue
		}
		if c := ev.Choices[0].Delta.Content; c != "" {
			processChunk(c)
		}
	}
	// 收尾：把 pending 剩余内容输出（工具调用区段的屏蔽状态同样生效）
	if pending.Len() > 0 {
		rest := pending.String()
		pending.Reset()
		if inThinking {
			emit(rest, true)
		} else {
			emitBody(rest)
		}
	}
	// 补发过滤器中残留的正文（未闭合的 ```json 等），避免丢失内容
	bf.Flush()
	if err := scanner.Err(); err != nil {
		// 原文 "context deadline exceeded (Client.Timeout or context cancellation
		// while reading body)" 对用户毫无意义，这里翻译成能据以行动的提示。
		hint := streamReadErrHint(err, timeout)
		m.appendLog(AgentLog{Level: "error", Category: "llm", Title: "LLM 流式读取中断: " + tag,
			Detail: fmt.Sprintf("%v\n已收到 %d 字符", err, full.Len()), DurationMs: time.Since(start).Milliseconds()})
		if full.Len() == 0 {
			return "", fmt.Errorf("AI 流式读取失败: %s", hint)
		}
		// 已输出部分内容：保留它继续走完本轮（用户至少拿到半截答案，
		// 思考与工具链也不被丢弃），中断原因记入日志。
		m.appendLog(AgentLog{Level: "info", Category: "llm", Title: "LLM 流式提前结束: " + tag,
			Detail: "已保留中断前生成的内容继续处理。原因：" + hint})
	}
	out := full.String()
	dur := time.Since(start).Milliseconds()
	detail := util.Truncate(out, 3000)
	if p := usagePtr(callUsage); p != nil {
		detail = usageLine(callUsage) + "\n\n" + detail
	}
	m.appendLog(AgentLog{Level: "response", Category: "llm", Title: "LLM 流式响应: " + tag, Detail: detail, DurationMs: dur, Usage: usagePtr(callUsage)})
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
		sb.WriteString("\n\n## 可用技能(Skill)\n遇到匹配场景时，请在思考中说明将使用哪个技能，并严格遵循其指引作答（可叠加多个相关技能）：\n")
		for _, s := range skills {
			if !s.Enabled {
				continue
			}
			sb.WriteString(fmt.Sprintf("- [%s] %s\n", s.Name, s.Description))
			if strings.TrimSpace(s.Prompt) != "" {
				sb.WriteString(fmt.Sprintf("  技能指引：\n%s\n", s.Prompt))
			}
		}
	}
	if len(tools) > 0 {
		sb.WriteString("\n\n## 可用工具(Tool)\n你可以调用以下工具。调用工具时，请使用如下两种格式之一（任选，不要混用、且每轮最多一个工具调用）：\n")
		sb.WriteString("格式A（标签，推荐）：\n")
		sb.WriteString("<tool_call>\n<function>工具名</function>\n<parameter name=\"参数名\">参数值</parameter>\n</tool_call>\n")
		sb.WriteString("格式B（JSON）：\n")
		sb.WriteString("```json\n{\"action\":\"tool\",\"server\":\"<服务器ID>\",\"tool\":\"<工具名>\",\"arguments\":{...}}\n```\n")
		sb.WriteString("说明：内置工具 server 固定为 \"builtin\"（无需 MCP 服务器，本地直接执行）。参数值如需为对象/数组，请写成 JSON 字符串。\n")
		sb.WriteString("\n严格遵守格式（下列写法都会导致调用失败）：\n")
		sb.WriteString("- 函数名只能写成 <function>工具名</function>，不要用 <function=工具名>、<function name=工具名>（不加引号）等变体。\n")
		sb.WriteString("- 每个 <parameter> 都必须有对应的 </parameter> 闭合。\n")
		sb.WriteString("- 一个工具调用的所有内容必须放在同一个 <tool_call>...</tool_call> 内，不要提前闭合。\n")
		sb.WriteString("- 每个需要的参数都必须给全，缺参数会直接失败（如 db_query 必须同时给 database 与 sql）。\n")
		sb.WriteString("\n正确示例：\n")
		sb.WriteString("<tool_call>\n<function>db_query</function>\n<parameter name=\"database\">diygw</parameter>\n<parameter name=\"sql\">SELECT * FROM orders WHERE ROWNUM <= 10</parameter>\n</tool_call>\n")
		sb.WriteString("注意：Oracle 没有 LIMIT 子句，行数限制请用 ROWNUM 或 FETCH FIRST n ROWS ONLY。\n")
		sb.WriteString("\n表格导出：当你给出的回答中包含数据表格（db_query 查询结果、统计汇总、明细清单等）时，" +
			"如用户可能需要表格数据，请调用 export_table 把该表格导出为文件（xlsx 默认，Excel 可直接打开），" +
			"并把生成的文件路径告知用户。data 参数可直接复制你刚输出的 Markdown 表格原文。\n")
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

// extractBareToolJSON 从正文里提取形如 {"action":"tool",...} 的裸 JSON 工具调用对象。
// 相比正则 [^{}]*，这里用括号计数（忽略字符串内的花括号）以支持嵌套对象，
// 例如 {"action":"tool","arguments":{"connId":...}}。返回所有候选 JSON 字符串（待后续 json.Unmarshal 校验）。
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
var toolCallTagRe = regexp.MustCompile(`(?s)<tool_call>(.*?)</tool_call>`)
// 函数名在标签体内：<function>name</function>
var toolCallFuncRe = regexp.MustCompile(`(?s)<function\s*>\s*(.*?)\s*</function>`)
// 函数名在属性里：<function name="name">...</function>
var toolCallFuncAttrRe = regexp.MustCompile(`(?s)<function\s+name\s*=\s*["']([^"']*)["'](?:\s+[^>]*)?>(.*?)</function>`)
var toolCallParamRe = regexp.MustCompile(`(?s)<parameter\s+name\s*=\s*"([^"]*)"(?:\s+[^>]*)?>(.*?)</parameter>`)
// 外层可能包 <function_calls> ... </function_calls>
var funcCallsRe = regexp.MustCompile(`(?s)<function_calls>(.*?)</function_calls>`)

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
var looseCallsOpenRe = regexp.MustCompile(`(?is)<\s*[｜|]*\s*(?:dsml)?[\s|｜]*\s*(?:tool_calls|function_calls|tool_call|calls)\s*>`)
var looseCallsCloseRe = regexp.MustCompile(`(?is)<\s*/\s*[｜|]*\s*(?:dsml)?[\s|｜]*\s*(?:tool_calls|function_calls|tool_call|calls)\s*>`)

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
const fenceBufMax = 4096

// bodyFilter 流式正文过滤器。
// 模型输出工具调用时，工具调用本身不应作为正文实时展示——它最终会被 stripTags 剥除，
// 若实时推送，用户会先看到一坨 JSON，下一轮 loop-start 又被清空，产生「回答了两次」的错觉。
// 该过滤器只影响推送给前端的增量，不影响返回给上层的完整文本（完整文本仍需用于解析工具调用）。
type bodyFilter struct {
	suppressed bool          // 已确认进入工具调用区段：本轮剩余内容全部丢弃
	inFence    bool          // 正在缓冲 ```json 块以判定是否为工具调用
	fenceBuf   strings.Builder
	pending    strings.Builder // 跨分片暂存（不完整的工具调用标记前缀）
	emit       func(string)    // 推送已确认的正文
}

// Write 接收一段正文增量，过滤掉工具调用区段后推送剩余部分。
func (f *bodyFilter) Write(s string) {
	if s == "" {
		return
	}
	// 规范化模型可能输出的 ｜｜DSML｜｜ 前缀标签，使其能被后续标记判定与剥离识别
	s = normalizeToolTags(s)
	// 拼接上一分片留下的不完整标记尾巴
	if f.pending.Len() > 0 {
		s = f.pending.String() + s
		f.pending.Reset()
	}
	for len(s) > 0 {
		if f.suppressed {
			return // 已进入工具调用区段，本轮剩余内容全部丢弃
		}
		if f.inFence {
			// 在 ```json 块内：等块结束（或已能判定）再决定是工具调用还是普通代码
			endIdx := strings.Index(s, "```")
			if endIdx >= 0 {
				f.fenceBuf.WriteString(s[:endIdx])
				s = s[endIdx+3:]
				f.inFence = false
				body := f.fenceBuf.String()
				f.fenceBuf.Reset()
				if toolJSONProbeRe.MatchString(body) {
					f.suppressed = true // 确认是工具调用，丢弃
					return
				}
				// 普通代码块：连同围栏标记一并补发，保证 markdown 结构完整
				f.emit("```json" + body + "```")
				continue
			}
			f.fenceBuf.WriteString(s)
			body := f.fenceBuf.String()
			if toolJSONProbeRe.MatchString(body) {
				f.fenceBuf.Reset()
				f.suppressed = true
				return
			}
			if f.fenceBuf.Len() > fenceBufMax {
				// 超长仍无法判定：按普通内容补发，避免吞掉正文
				f.fenceBuf.Reset()
				f.inFence = false
				f.emit("```json" + body)
			}
			return
		}
		// 查找最近的工具调用起始标记
		cut, cutTag, cutHit := -1, "", false
		for _, m := range toolCallMarkers {
			if i := strings.Index(s, m.tag); i >= 0 && (cut < 0 || i < cut) {
				cut, cutTag, cutHit = i, m.tag, m.hit
			}
		}
		if cut >= 0 {
			f.emit(s[:cut]) // 标记之前的正文正常推送
			s = s[cut+len(cutTag):]
			if cutHit {
				f.suppressed = true // 标签形式：确认是工具调用
				return
			}
			f.inFence = true // ```json：进入缓冲判定
			f.fenceBuf.Reset()
			continue
		}
		// 可能是被截断的标记前缀，留给下一个分片判定
		if keep := toolCallPartialTail(s); keep > 0 {
			f.emit(s[:len(s)-keep])
			f.pending.WriteString(s[len(s)-keep:])
			return
		}
		f.emit(s)
		return
	}
}

// Flush 流结束时输出残留：未闭合的 ```json 缓冲按普通内容补发，避免丢失正文。
func (f *bodyFilter) Flush() {
	if f.suppressed {
		return
	}
	if f.pending.Len() > 0 {
		f.emit(f.pending.String())
		f.pending.Reset()
	}
	if f.inFence && f.fenceBuf.Len() > 0 {
		f.emit("```json" + f.fenceBuf.String())
		f.fenceBuf.Reset()
		f.inFence = false
	}
}

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
//
//	"arguments":"{\"database\":\"diygw\",\"sql\":\"SELECT 1 FROM dual\"}"
//
// 标准反序列化会直接失败（字符串不能解到 map），于是一个参数都取不到，
// 最终报「缺少 connId / database / sql 参数」。这里做二次解析。
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

// ---------------- 畸形标签工具调用的容错解析 ----------------
//
// 实测模型（Qwen / DeepSeek / GLM 等）对工具调用的输出极不稳定，常见破坏形式：
//  1. 函数名用等号或冒号：<function=db_query>（无引号、无 name= 属性）
//  2. 参数标签不闭合：<parameter name="connId">pl_1
//     <parameter name="database">diygw     —— 没有 </parameter>
//  3. 参数值里混入后续标签残片，例如 connId 的值变成
//     "pl_1</function>\n<parameter name=\"database\">diygw"
//  4. 参数写成自闭合：<parameter name="limit"/>
//
// 这些都会被原先「严格闭合」的正则漏掉，结果是参数串味 + 关键参数（sql）丢失，
// 最终报「缺少 connId / database / sql 参数」——而模型其实已经给出了。
// 下面按「起始标签定位 + 值遇到任意标签即截断」的思路解析，可救回绝大部分。

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
	text = dsmlStripRe.ReplaceAllString(text, "")                                  // 移除所有 ｜｜DSML｜｜ 前缀
	text = dsmlToolCallsOpenRe.ReplaceAllString(text, "<tool_call>")               // <tool_calls> -> <tool_call>
	text = dsmlToolCallsCloseRe.ReplaceAllString(text, "</tool_call>")             // </tool_calls> -> </tool_call>
	text = dsmlInvokeOpenRe.ReplaceAllString(text, "<function name=\"$1\">")        // <invoke name="x"> -> <function name="x">
	text = dsmlInvokeCloseRe.ReplaceAllString(text, "</function>")                  // </invoke> -> </function>
	text = dsmlFuncCloseRe.ReplaceAllString(text, "<function$1>$2</function>")       // 修复 <function>..</parameter> 畸形
	text = dsmlDupCloseRe.ReplaceAllString(text, "</function>")                     // 去掉重复的 </function>
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
	// 全局互斥：桌面端与局域网网页端共享同一份会话，并发执行会写坏数据。
	// 用 TryLock 让第二端立刻拿到明确提示，而不是排队后与第一端交叉写入。
	if !m.runMu.TryLock() {
		return RunAgentResult{Error: "已有对话正在执行（桌面端或局域网网页端），请等它结束后再试"}
	}
	defer m.runMu.Unlock()

	d := m.readAgentData()
	cfg := d.Config
	// 回复长度上限统一由配置决定（前端无需单独传），0 表示交给模型服务端默认值
	args.MaxTokens = cfg.MaxTokens
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
	// 数据库连接分析：注入可用连接信息，避免模型瞎填 connId / database 参数
	if cfg.EnableDBAnalysis {
		var connLines []string
		active := cfg.ActiveDBConn
		for _, c := range m.host.Store().GetData().Plugins.Connections {
			if c.Category != "db" {
				continue
			}
			mark := ""
			if c.ID == active {
				mark = "（当前激活分析连接）"
			}
			// 库名以「该连接已同步过表结构的库」为准，而不是连接配置里的默认库：
			// Oracle 的默认库填的是服务名（与 schema 不是一回事），MySQL/PG 的默认库
			// 也未必是用户实际要分析的库。只列出真正可用于查询的库，避免模型填错。
			dbs := m.syncedDBsOfConn(c.ID)
			dbLabel := strings.Join(dbs, "、")
			if dbLabel == "" {
				dbLabel = "（该库尚未同步表结构，请先在「插件 / 数据库连接」中同步）"
			}
			connLines = append(connLines, fmt.Sprintf("- connId=%s 名称=%s 类型=%s 可用库=%s%s",
				c.ID, c.Name, c.DbType, dbLabel, mark))
		}
		if len(connLines) > 0 {
			sysPrompt += "\n\n## 可用数据库连接（db 类型）\n调用 db_schema / db_query 时请使用下列 connId 与 database：" +
				"\n" + strings.Join(connLines, "\n") +
				"\n说明：database 必须是上面「可用库」中的值（已同步过表结构的库）。" +
				"若未显式指定 connId，工具会使用当前激活的分析连接。" +
				"\n注意：Oracle 连接配置里的「默认库」填的是**服务名**（用于建立连接），不是 schema；" +
				"实际查询用的库（schema）必须取自上面的「可用库」。"
		}
	}

	messages := []ai.ChatMessage{{Role: "system", Content: sysPrompt}}
	// 确定本次对话的目标会话：优先用前端传来的 SessionID（本端正在浏览的会话），
	// 为空或不存在时回退到全局激活会话。
	// 读历史与写结果必须用同一个会话，否则会出现「上下文来自 A、结果写进 B」。
	targetSess := findSession(d, args.SessionID)
	if targetSess == nil {
		targetSess = d.activeSession()
	}
	// 加载该会话的历史上下文（关键：不能用顶层 Messages，否则会串到别的会话）
	hist := []AgentMsg{}
	if targetSess != nil {
		hist = targetSess.Messages
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
		// 实时推送本轮已累计的用量，桌面端与局域网网页端都能在流式过程中看到 token 增长
		m.emitEvent("agent:usage", map[string]interface{}{
			"promptTokens":     accUsage.PromptTokens,
			"completionTokens": accUsage.CompletionTokens,
			"totalTokens":      accUsage.TotalTokens,
		})
	}

	// 技能匹配：仅展示被模型在思考中实际运用到的「已启用」技能（不展示未启用的）。
	// 用 markedSkills 记录已展示过的技能名避免重复，enabledCount 统计已启用的技能数。
	markedSkills := map[string]bool{}
	enabledCount := 0
	for _, s := range d.Skills {
		if s.Enabled {
			enabledCount++
		}
	}
	// 记录哪些技能在思考中被模型提及（简单命中：思考文本出现技能名）即展示。
	markSkills := func(text string) {
		for _, s := range d.Skills {
			if !s.Enabled || s.Name == "" || markedSkills[s.Name] {
				continue
			}
			if strings.Contains(text, s.Name) {
				markedSkills[s.Name] = true
				desc := s.Description
				if desc == "" {
					desc = "(无描述)"
				}
				step := AgentStep{
									Type:   "skill",
									Name:   s.Name,
									Input:  "描述：" + desc + "\n\n注入提示词：\n" + s.Prompt,
									Output: "模型在思考中运用了该技能",
									CallID: "skill_" + agentID(""),
								}
							result.Steps = append(result.Steps, step)
							m.b.Emit("agent:step", step)
							// 写入 skill 分类日志：否则日志面板里「skill」分类永远查不到数据
							m.appendLog(AgentLog{Level: "info", Category: "skill",
								Title: "命中技能: " + s.Name,
								Detail: "描述：" + desc + "\n\n注入提示词：\n" + s.Prompt, UserID: cfg.CurrentUserID})
			}
		}
	}

	loops := cfg.MaxLoops
	if loops <= 0 {
		loops = 6
	}
	for i := 0; i < loops; i++ {
		// 通知前端：新一轮流式输出开始（带 clientId，便于识别是不是自己发起的）
		m.b.Emit("agent:loop-start", map[string]interface{}{"loop": i + 1, "clientId": args.ClientID})
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
			// 模型输出看似包含工具调用标记却未能解析（如标签被改写/截断/格式异常）时，
			// 给出诊断步骤，便于在前端直接看到「为什么没执行工具」，而不是默默当成正文。
			if strings.Contains(out, "<tool_call") || strings.Contains(out, "<function") || strings.Contains(out, "｜｜DSML｜｜") {
				diag := strings.TrimSpace(out)
				if len(diag) > 600 {
					diag = diag[:600] + " …(已截断)"
				}
				result.Steps = append(result.Steps, AgentStep{
					Type:   "tool-failed",
					Name:   "工具调用未解析",
					CallID: "failed_" + agentID(""),
					Output: "检测到工具调用标记但未能解析为可执行的工具动作。原始片段：\n" + diag + "\n常见原因：标签被截断/转义、参数未闭合、或使用了不被识别的格式（需 <tool_call><function>名</function><parameter name=\"k\">v</parameter></tool_call>）。",
				})
				m.b.Emit("agent:step", result.Steps[len(result.Steps)-1])
			}
			// 无工具调用 = 最终答案
			result.Content = stripTags(out)
			result.Thinking = strings.TrimSpace(thinkingAll.String())
			break
		}
		// 执行工具。callID 标识本次调用实例：同一次调用的开始/结束事件共用它，
		// 前端据此合并为一张卡片；而重复调用同一工具时各自持有不同 callID，会逐条展示。
		callID := "call_" + agentID("") + strconv.Itoa(i)
			step := AgentStep{Type: "tool", Name: act.Tool, Server: act.Server, Input: toJSON(act.Arguments), CallID: callID}
			m.b.Emit("agent:step", AgentStep{Type: "tool", Name: act.Tool, Server: act.Server, Input: step.Input, CallID: callID})
		var toolOut string
		var terr error
		if act.Server == "builtin" {
			// 内置工具：本地执行
			toolOut, terr = m.execBuiltinTool(act.Tool, act.Arguments, cfg.MaxFileRead)
			// 内置工具此前完全没有日志，导致「按工具/级别筛选」时查不到任何记录，
			// 这里补上：入参、成功结果、报错各一条，与 MCP 工具日志保持一致。
			if terr != nil {
				m.appendLog(AgentLog{Level: "error", Category: "agent",
					Title: fmt.Sprintf("调用内置工具失败: %s", act.Tool),
					Detail: "参数: " + toJSON(act.Arguments) + "\n错误: " + terr.Error(), UserID: cfg.CurrentUserID})
			} else {
				m.appendLog(AgentLog{Level: "tool", Category: "agent",
					Title: fmt.Sprintf("调用内置工具: %s", act.Tool),
					Detail: "参数: " + toJSON(act.Arguments) + "\n结果: " + util.Truncate(toolOut, 4000), UserID: cfg.CurrentUserID})
			}
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
		// 数据库结构/查询结果需要完整呈现给用户核对，不做截断；其余工具仍按上限截断
		if act.Tool == "db_schema" || act.Tool == "db_query" {
			step.Output = toolOut
		} else {
			step.Output = util.Truncate(toolOut, cfg.MaxToolOutput)
		}
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

	// 技能匹配过程反馈：本次启用了技能但未匹配到任何适用场景时，明确提示匹配机制在运行，
	// 让用户知道「有匹配过程，只是没命中」，而不是以为技能没生效。
	if enabledCount > 0 && len(markedSkills) == 0 {
		step := AgentStep{
			Type:   "skill",
			Name:   "技能匹配",
			Output: fmt.Sprintf("本次已启用 %d 个技能，但未匹配到适用场景（模型未在思考中运用任何技能）", enabledCount),
		}
		result.Steps = append(result.Steps, step)
		m.b.Emit("agent:step", step)
	}

	if result.Content == "" {
		result.Content = "（未生成有效回答，请检查工具配置或增大 loop 轮数）"
	}

	// AI 润色（可选）
	if cfg.EnablePolish && result.Content != "" {
		polishSys := "你是文字润色助手，请在不改变原意与技术细节的前提下，使下面内容表达更清晰、专业、结构更好。若含代码/表格/图表请保留。直接输出润色后的正文。"
		// 通知前端进入润色，重置正文流
		m.b.Emit("agent:polish-start", nil)
		// 润色阶段不推送流式增量（onDelta=nil）：否则润色后的全文会被重新「打字」一遍，
		// 用户会看到同一篇回答输出两次。改为只通知前端进入润色状态，完成后一次性替换。
		polished, err := m.llmCallStream(args, []ai.ChatMessage{{Role: "system", Content: polishSys}, {Role: "user", Content: result.Content}}, 0.4, "polish", nil, func(u TokenUsage) {
			addUsage(u)
		})
		if err == nil && strings.TrimSpace(polished) != "" {
			result.Content = strings.TrimSpace(polished)
		}
	}

	result.Thinking = strings.TrimSpace(thinkingAll.String())

	// 保存会话（写入本次对话的目标会话）
	now := time.Now().Format("2006-01-02 15:04:05")
	m.mu.Lock()
	d = m.readAgentData()
	// 重新按 ID 定位（对话期间该会话可能已被另一端删除），退化时回退到全局激活会话
	sess := findSession(d, args.SessionID)
	if sess == nil {
		sess = d.activeSession()
	}
	if sess == nil {
		// 兜底：新建
		id := m.CreateAgentSession("默认会话")
		d = m.readAgentData()
		sess = d.activeSession()
		_ = id
	}
	sess.Messages = append(sess.Messages,
		AgentMsg{ID: agentID("msg"), Role: "user", Content: args.Input, Time: now},
		AgentMsg{ID: agentID("msg"), Role: "assistant", Content: result.Content, Thinking: result.Thinking, Steps: result.Steps, Time: now,
			// 记录本轮消耗，供前端在气泡上展示「本次 token」
			Usage: &TokenUsage{PromptTokens: accUsage.PromptTokens, CompletionTokens: accUsage.CompletionTokens, TotalTokens: accUsage.TotalTokens}},
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
	sessID := sess.ID
	m.mu.Unlock()

	m.b.Emit("agent:done", map[string]interface{}{
		"content": result.Content, "thinking": result.Thinking, "usage": accUsage,
		"clientId": args.ClientID, "sessionId": sessID,
	})
	// 结束日志里明确记录本轮「输入 / 输出 / 合计」，便于事后按 token 核对每一次对话的开销
	m.appendLog(AgentLog{Level: "info", Category: "agent", Title: "Agent 运行结束",
		Detail: fmt.Sprintf("%s\n步骤数=%d 输出长度=%d", usageLine(accUsage), len(result.Steps), len(result.Content)),
		UserID: cfg.CurrentUserID, Usage: usagePtr(accUsage)})
	// 通知所有端（含桌面窗口）会话数据已变更。
	// 这是「局域网网页端提问后桌面端也能看到」的关键：桌面端没有轮询，
	// 必须由后端主动推送，否则它的会话列表与消息永远停留在旧数据（需手动刷新页面）。
	m.notifySessionsChanged("run", map[string]interface{}{
		"clientId": args.ClientID, "sessionId": sessID,
		"usage": accUsage, "input": util.Truncate(args.Input, 60),
	})
	return result
}

// PolishText 独立的 AI 润色接口（供输入框"润色"按钮使用）。
func (m *Manager) PolishText(args RunAgentArgs) (string, error) {
	sys := "你是文字润色与提示词优化助手。请优化下面这段用户输入，使其作为给 AI 的指令更清晰、完整、无歧义。直接输出优化后的文本，不要解释。"
	return m.llmCall(args, []ai.ChatMessage{{Role: "system", Content: sys}, {Role: "user", Content: args.Input}}, 0.5, "polish-input", args.MaxTokens)
}
