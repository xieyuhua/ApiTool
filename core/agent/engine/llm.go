package engine

// 引擎层：LLM 调用（非流式 / 流式）与请求日志。

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"apitool/core/agent"
	"apitool/internal/ai"
	"apitool/internal/util"
)

func messagesLogText(msgs []ai.ChatMessage) string {
	var sb strings.Builder
	for i, msg := range msgs {
		role := msg.Role
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
		if strings.TrimSpace(msg.Content) == "" {
			sb.WriteString("(空内容)")
		} else {
			sb.WriteString(msg.Content)
		}
		sb.WriteString("\n\n")
	}
	out := sb.String()
	if n := runeCount(out); n > agent.LogDetailHardLimit {
		r := []rune(out)
		out = string(r[:agent.LogDetailHardLimit]) +
			"\n\n…（内容过长已截断，完整长度 " + strconv.Itoa(n) + " 字符）"
	}
	return out
}

// requestLogHeader 生成 request 日志的参数摘要行，让「完整请求」具备上下文。

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

// runeCount 按 Unicode 字符数统计（避免中文按字节被算成两倍）。
func runeCount(s string) int { return len([]rune(s)) }

// usagePtr 把本次调用的用量挂到日志上；全为 0（服务端未返回 usage）时返回 nil，
// 以便前端区分「没有消耗」与「服务端没给数据」。

// usagePtr 把本次调用的用量挂到日志上；全为 0（服务端未返回 usage）时返回 nil，
// 以便前端区分「没有消耗」与「服务端没给数据」。
func usagePtr(u agent.TokenUsage) *agent.TokenUsage {
	if u.PromptTokens == 0 && u.CompletionTokens == 0 && u.TotalTokens == 0 {
		return nil
	}
	c := u
	return &c
}

// usageLine 生成日志详情里的用量摘要行。

// usageLine 生成日志详情里的用量摘要行。
func usageLine(u agent.TokenUsage) string {
	return fmt.Sprintf("Token 用量　输入 %d　输出 %d　合计 %d", u.PromptTokens, u.CompletionTokens, u.TotalTokens)
}

// streamReadErrHint 把流式读取的中断原因翻译成可操作的提示。

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

// errStreamIdle 表示流式响应空闲超时。
var errStreamIdle = errors.New("流式响应空闲超时")

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

// llmCall 复用底层 OpenAI 兼容请求，返回原始文本，并写日志。
func (e *Engine) llmCall(args agent.RunAgentArgs, messages []ai.ChatMessage, temperature float64, tag string, maxTokens int) (string, error) {
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
	e.rt.AppendLog(agent.AgentLog{Level: "request", Category: "llm", Title: "LLM 请求: " + tag, Detail: requestLogHeader(model, temperature, maxTokens, false, len(messages)) + messagesLogText(messages)})
	resp, err := client.Do(req)
	if err != nil {
		e.rt.AppendLog(agent.AgentLog{Level: "error", Category: "llm", Title: "LLM 请求失败: " + tag, Detail: err.Error()})
		return "", fmt.Errorf("AI 请求失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	dur := time.Since(start).Milliseconds()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		e.rt.AppendLog(agent.AgentLog{Level: "error", Category: "llm", Title: "LLM 响应异常: " + tag, Detail: fmt.Sprintf("%d: %s", resp.StatusCode, util.Truncate(string(body), 2000)), DurationMs: dur})
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
	var callUsage agent.TokenUsage
	var u struct {
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
			TotalTokens      int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(body, &u) == nil {
		callUsage = agent.TokenUsage{PromptTokens: u.Usage.PromptTokens, CompletionTokens: u.Usage.CompletionTokens, TotalTokens: u.Usage.TotalTokens}
	}
	detail := util.Truncate(out, 3000)
	if p := usagePtr(callUsage); p != nil {
		detail = usageLine(callUsage) + "\n\n" + detail
	}
	e.rt.AppendLog(agent.AgentLog{Level: "response", Category: "llm", Title: "LLM 响应: " + tag, Detail: detail, DurationMs: dur, Usage: usagePtr(callUsage)})
	return out, nil
}

// streamDelta 描述一次流式回调的增量：区分是思考区还是正文区。

func (e *Engine) llmCallStream(args agent.RunAgentArgs, messages []ai.ChatMessage, temperature float64, tag string, onDelta func(streamDelta), onUsage func(agent.TokenUsage)) (string, error) {
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
	e.rt.AppendLog(agent.AgentLog{Level: "request", Category: "llm", Title: "LLM 流式请求: " + tag, Detail: requestLogHeader(model, temperature, args.MaxTokens, true, len(messages)) + messagesLogText(messages)})
	resp, err := client.Do(req)
	if err != nil {
		e.rt.AppendLog(agent.AgentLog{Level: "error", Category: "llm", Title: "LLM 流式请求失败: " + tag, Detail: err.Error()})
		return "", fmt.Errorf("AI 请求失败: %w", err)
	}
	defer resp.Body.Close()
	// 套上空闲超时：只要模型还在持续输出就一直正常，卡住（服务端无响应 /
	// 代理挂起 / 网络中断）超过 timeout 秒才报错，且报错带上可操作提示。
	resp.Body = &idleTimeoutBody{r: resp.Body, idle: time.Duration(timeout) * time.Second}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		e.rt.AppendLog(agent.AgentLog{Level: "error", Category: "llm", Title: "LLM 流式响应异常: " + tag, Detail: fmt.Sprintf("%d: %s", resp.StatusCode, util.Truncate(string(body), 2000))})
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
	var callUsage agent.TokenUsage
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
		// 与 agent.TokenUsage 的驼峰 tag 不同，这里用专用 struct 解析后再映射。
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
			callUsage = agent.TokenUsage{
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
		e.rt.AppendLog(agent.AgentLog{Level: "error", Category: "llm", Title: "LLM 流式读取中断: " + tag,
			Detail: fmt.Sprintf("%v\n已收到 %d 字符", err, full.Len()), DurationMs: time.Since(start).Milliseconds()})
		if full.Len() == 0 {
			return "", fmt.Errorf("AI 流式读取失败: %s", hint)
		}
		// 已输出部分内容：保留它继续走完本轮（用户至少拿到半截答案，
		// 思考与工具链也不被丢弃），中断原因记入日志。
		e.rt.AppendLog(agent.AgentLog{Level: "info", Category: "llm", Title: "LLM 流式提前结束: " + tag,
			Detail: "已保留中断前生成的内容继续处理。原因：" + hint})
	}
	out := full.String()
	dur := time.Since(start).Milliseconds()
	detail := util.Truncate(out, 3000)
	if p := usagePtr(callUsage); p != nil {
		detail = usageLine(callUsage) + "\n\n" + detail
	}
	e.rt.AppendLog(agent.AgentLog{Level: "response", Category: "llm", Title: "LLM 流式响应: " + tag, Detail: detail, DurationMs: dur, Usage: usagePtr(callUsage)})
	return out, nil
}

// partialTailLen 返回 buf 末尾可能是 marker 前缀的长度（用于跨分片保留半个标记）。
