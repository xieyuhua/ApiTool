package engine

// 引擎层：Agent 对话编排（ReAct / Plan loop）。
//
// 这是整个 Agent 的「大脑」：读数据 → 构建提示词 → 调模型 → 解析工具调用 →
// 执行工具 → 把结果回灌模型 → 循环，直到模型给出最终答案。
// 数据读写与事件推送都走 agent.Runtime 接口，工具执行走 tools.Toolbox，
// 因此本层不直接依赖任何存储实现。

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"apitool/core/agent"
	"apitool/core/agent/tools"
	"apitool/internal/ai"
	"apitool/internal/util"
)

// RunAgent 执行一次完整的 Agent 对话（ReAct / Plan loop）。
func (e *Engine) RunAgent(args agent.RunAgentArgs) agent.RunAgentResult {
	// 全局互斥：桌面端与局域网网页端共享同一份会话，并发执行会写坏数据。
	// 用 TryLock 让第二端立刻拿到明确提示，而不是排队后与第一端交叉写入。
	if !e.runMu.TryLock() {
		return agent.RunAgentResult{Error: "已有对话正在执行（桌面端或局域网网页端），请等它结束后再试"}
	}
	defer e.runMu.Unlock()

	d := e.rt.ReadAgentData()
	cfg := d.Config
	// 回复长度上限统一由配置决定（前端无需单独传），0 表示交给模型服务端默认值
	args.MaxTokens = cfg.MaxTokens
	userCtx := e.rt.MCPUserContext(cfg, d.Users)

	// 收集可用工具（启用的 MCP 服务器 + 启用的内置工具）
	var allTools []agent.MCPTool
	srvByID := map[string]agent.MCPServer{}
	for _, srv := range d.Servers {
		srvByID[srv.ID] = srv
		if !srv.Enabled {
			continue
		}
		ts, err := e.rt.ListMCPTools(srv, userCtx)
		if err != nil {
			e.rt.AppendLog(agent.AgentLog{Level: "error", Category: "mcp", Title: "加载工具失败: " + srv.Name, Detail: err.Error()})
			continue
		}
		allTools = append(allTools, ts...)
	}
	// 内置工具（本地执行，受开关控制）
	builtinTools := tools.Collect(cfg.Tools.Enabled, cfg.Tools.Desc)
	allTools = append(allTools, builtinTools...)

	// 组装系统提示
	sysPrompt := cfg.SystemPrompt + buildToolsPrompt(allTools, d.Skills, cfg.Mode)
	if userCtx != nil {
		sysPrompt += fmt.Sprintf("\n\n## 当前登录用户\n%s（调用工具时会自动携带其身份用于权限校验）", agent.ToJSON(userCtx))
	}
	// 数据库连接分析：注入可用连接信息，避免模型瞎填 connId / database 参数
	if cfg.EnableDBAnalysis {
		var connLines []string
		active := cfg.ActiveDBConn
		for _, c := range e.rt.Connections() {
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
			dbs := e.tb.SyncedDBs(c.ID)
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
	targetSess := agent.FindSession(d, args.SessionID)
	if targetSess == nil {
		targetSess = d.ActiveSessionPtr()
	}
	// 加载该会话的历史上下文（关键：不能用顶层 Messages，否则会串到别的会话）
	hist := []agent.AgentMsg{}
	if targetSess != nil {
		hist = targetSess.Messages
	}
	if cfg.ContextLimit > 0 && len(hist) > cfg.ContextLimit {
		hist = hist[len(hist)-cfg.ContextLimit:]
	}
	for _, msg := range hist {
		if msg.Role == "user" || msg.Role == "assistant" {
			messages = append(messages, ai.ChatMessage{Role: msg.Role, Content: msg.Content})
		}
	}
	messages = append(messages, ai.ChatMessage{Role: "user", Content: args.Input})

	e.rt.AppendLog(agent.AgentLog{Level: "info", Category: "agent", Title: "开始运行 Agent", Detail: fmt.Sprintf("模式=%s 最大轮数=%d 上下文=%d 工具数=%d 技能数=%d\n输入: %s", cfg.Mode, cfg.MaxLoops, cfg.ContextLimit, len(allTools), len(d.Skills), util.Truncate(args.Input, 500)), UserID: cfg.CurrentUserID})

	result := agent.RunAgentResult{}
	var thinkingAll strings.Builder
	var planText string

	// token 累加器（同时累计到全局与当前会话）
	var accUsage agent.TokenUsage
	addUsage := func(u agent.TokenUsage) {
		accUsage.PromptTokens += u.PromptTokens
		accUsage.CompletionTokens += u.CompletionTokens
		accUsage.TotalTokens += u.TotalTokens
		result.Usage = accUsage
		// 实时推送本轮已累计的用量，桌面端与局域网网页端都能在流式过程中看到 token 增长
		e.rt.EmitEvent("agent:usage", map[string]interface{}{
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
				step := agent.AgentStep{
					Type:   "skill",
					Name:   s.Name,
					Input:  "描述：" + desc + "\n\n注入提示词：\n" + s.Prompt,
					Output: "模型在思考中运用了该技能",
					CallID: "skill_" + agent.NewID(""),
				}
				result.Steps = append(result.Steps, step)
				e.rt.EmitEvent("agent:step", step)
				// 写入 skill 分类日志：否则日志面板里「skill」分类永远查不到数据
				e.rt.AppendLog(agent.AgentLog{Level: "info", Category: "skill",
					Title:  "命中技能: " + s.Name,
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
		e.rt.EmitEvent("agent:loop-start", map[string]interface{}{"loop": i + 1, "clientId": args.ClientID})
		out, err := e.llmCallStream(args, messages, cfg.Temperature, fmt.Sprintf("loop-%d", i+1), func(dc streamDelta) {
			// 实时把增量推给前端（区分思考区/正文区），实现打字机效果
			e.rt.EmitEvent("agent:delta", map[string]interface{}{"text": dc.Text, "thinking": dc.Thinking})
		}, func(u agent.TokenUsage) {
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
			e.rt.EmitEvent("agent:thinking", th)
			result.Steps = append(result.Steps, agent.AgentStep{Type: "thought", Name: "思考", Output: th})
		}
		if mm := planRe.FindStringSubmatch(out); len(mm) > 1 {
			planText = strings.TrimSpace(mm[1])
			result.Plan = planText
			e.rt.EmitEvent("agent:plan", planText)
			result.Steps = append(result.Steps, agent.AgentStep{Type: "plan", Name: "计划", Output: planText})
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
				result.Steps = append(result.Steps, agent.AgentStep{
					Type:   "tool-failed",
					Name:   "工具调用未解析",
					CallID: "failed_" + agent.NewID(""),
					Output: "检测到工具调用标记但未能解析为可执行的工具动作。原始片段：\n" + diag + "\n常见原因：标签被截断/转义、参数未闭合、或使用了不被识别的格式（需 <tool_call><function>名</function><parameter name=\"k\">v</parameter></tool_call>）。",
				})
				e.rt.EmitEvent("agent:step", result.Steps[len(result.Steps)-1])
			}
			// 无工具调用 = 最终答案
			result.Content = stripTags(out)
			result.Thinking = strings.TrimSpace(thinkingAll.String())
			break
		}
		// 执行工具。callID 标识本次调用实例：同一次调用的开始/结束事件共用它，
		// 前端据此合并为一张卡片；而重复调用同一工具时各自持有不同 callID，会逐条展示。
		callID := "call_" + agent.NewID("") + strconv.Itoa(i)
		step := agent.AgentStep{Type: "tool", Name: act.Tool, Server: act.Server, Input: agent.ToJSON(act.Arguments), CallID: callID}
		e.rt.EmitEvent("agent:step", agent.AgentStep{Type: "tool", Name: act.Tool, Server: act.Server, Input: step.Input, CallID: callID})
		var toolOut string
		var terr error
		if act.Server == "builtin" {
			// 内置工具：本地执行
			toolOut, terr = e.tb.Exec(act.Tool, act.Arguments, cfg.MaxFileRead)
			// 内置工具此前完全没有日志，导致「按工具/级别筛选」时查不到任何记录，
			// 这里补上：入参、成功结果、报错各一条，与 MCP 工具日志保持一致。
			if terr != nil {
				e.rt.AppendLog(agent.AgentLog{Level: "error", Category: "agent",
					Title:  fmt.Sprintf("调用内置工具失败: %s", act.Tool),
					Detail: "参数: " + agent.ToJSON(act.Arguments) + "\n错误: " + terr.Error(), UserID: cfg.CurrentUserID})
			} else {
				e.rt.AppendLog(agent.AgentLog{Level: "tool", Category: "agent",
					Title:  fmt.Sprintf("调用内置工具: %s", act.Tool),
					Detail: "参数: " + agent.ToJSON(act.Arguments) + "\n结果: " + util.Truncate(toolOut, 4000), UserID: cfg.CurrentUserID})
			}
		} else {
			srv, ok := srvByID[act.Server]
			if !ok {
				// 兼容：按工具名匹配所属服务器
				for _, t := range allTools {
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
			toolOut, terr = e.rt.CallMCPTool(srv, act.Tool, act.Arguments, userCtx)
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
		e.rt.EmitEvent("agent:step", step)
		// 把工具结果回灌给模型
		messages = append(messages, ai.ChatMessage{Role: "assistant", Content: out})
		messages = append(messages, ai.ChatMessage{Role: "user", Content: fmt.Sprintf("工具 %s 返回结果：\n%s\n请基于此继续。", act.Tool, toolOut)})

		// 最后一轮仍在调用工具，强制收尾
		if i == loops-1 {
			finalMsgs := append(messages, ai.ChatMessage{Role: "user", Content: "已达最大轮数，请基于以上信息直接给出最终答案。"})
			e.rt.EmitEvent("agent:loop-start", map[string]interface{}{"loop": loops + 1, "final": true})
			out2, err := e.llmCallStream(args, finalMsgs, cfg.Temperature, "final", func(dc streamDelta) {
				e.rt.EmitEvent("agent:delta", map[string]interface{}{"text": dc.Text, "thinking": dc.Thinking})
			}, func(u agent.TokenUsage) {
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
		step := agent.AgentStep{
			Type:   "skill",
			Name:   "技能匹配",
			Output: fmt.Sprintf("本次已启用 %d 个技能，但未匹配到适用场景（模型未在思考中运用任何技能）", enabledCount),
		}
		result.Steps = append(result.Steps, step)
		e.rt.EmitEvent("agent:step", step)
	}

	if result.Content == "" {
		result.Content = "（未生成有效回答，请检查工具配置或增大 loop 轮数）"
	}

	// AI 润色（可选）
	if cfg.EnablePolish && result.Content != "" {
		polishSys := "你是文字润色助手，请在不改变原意与技术细节的前提下，使下面内容表达更清晰、专业、结构更好。若含代码/表格/图表请保留。直接输出润色后的正文。"
		// 通知前端进入润色，重置正文流
		e.rt.EmitEvent("agent:polish-start", nil)
		// 润色阶段不推送流式增量（onDelta=nil）：否则润色后的全文会被重新「打字」一遍，
		// 用户会看到同一篇回答输出两次。改为只通知前端进入润色状态，完成后一次性替换。
		polished, err := e.llmCallStream(args, []ai.ChatMessage{{Role: "system", Content: polishSys}, {Role: "user", Content: result.Content}}, 0.4, "polish", nil, func(u agent.TokenUsage) {
			addUsage(u)
		})
		if err == nil && strings.TrimSpace(polished) != "" {
			result.Content = strings.TrimSpace(polished)
		}
	}

	result.Thinking = strings.TrimSpace(thinkingAll.String())

	// 保存会话（写入本次对话的目标会话）
	now := time.Now().Format("2006-01-02 15:04:05")
	var sessID string
	_ = e.rt.UpdateData(func(d *agent.AgentData) {
		// 重新按 ID 定位（对话期间该会话可能已被另一端删除），退化时回退到全局激活会话
		sess := agent.FindSession(*d, args.SessionID)
		if sess == nil {
			sess = d.ActiveSessionPtr()
		}
		if sess == nil {
			// 兜底：新建会话后再取一次
			d.Sessions = append(d.Sessions, agent.AgentSession{
				ID: agent.NewID("sess"), Title: "默认会话", CreatedAt: time.Now().Format(time.RFC3339),
			})
			sess = &d.Sessions[len(d.Sessions)-1]
			d.ActiveSession = sess.ID
		}
		sess.Messages = append(sess.Messages,
			agent.AgentMsg{ID: agent.NewID("msg"), Role: "user", Content: args.Input, Time: now},
			agent.AgentMsg{ID: agent.NewID("msg"), Role: "assistant", Content: result.Content, Thinking: result.Thinking, Steps: result.Steps, Time: now,
				// 记录本轮消耗，供前端在气泡上展示「本次 token」
				Usage: &agent.TokenUsage{PromptTokens: accUsage.PromptTokens, CompletionTokens: accUsage.CompletionTokens, TotalTokens: accUsage.TotalTokens}},
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
		sessID = sess.ID
	})

	e.rt.EmitEvent("agent:done", map[string]interface{}{
		"content": result.Content, "thinking": result.Thinking, "usage": accUsage,
		"clientId": args.ClientID, "sessionId": sessID,
	})
	// 结束日志里明确记录本轮「输入 / 输出 / 合计」，便于事后按 token 核对每一次对话的开销
	e.rt.AppendLog(agent.AgentLog{Level: "info", Category: "agent", Title: "Agent 运行结束",
		Detail: fmt.Sprintf("%s\n步骤数=%d 输出长度=%d", usageLine(accUsage), len(result.Steps), len(result.Content)),
		UserID: cfg.CurrentUserID, Usage: usagePtr(accUsage)})
	// 通知所有端（含桌面窗口）会话数据已变更。
	// 这是「局域网网页端提问后桌面端也能看到」的关键：桌面端没有轮询，
	// 必须由后端主动推送，否则它的会话列表与消息永远停留在旧数据（需手动刷新页面）。
	e.rt.NotifySessionsChanged("run", map[string]interface{}{
		"clientId": args.ClientID, "sessionId": sessID,
		"usage": accUsage, "input": util.Truncate(args.Input, 60),
	})
	return result
}

// PolishText 独立的 AI 润色接口（供输入框"润色"按钮使用）。

// PolishText 独立的 AI 润色接口（供输入框"润色"按钮使用）。
func (e *Engine) PolishText(args agent.RunAgentArgs) (string, error) {
	sys := "你是文字润色与提示词优化助手。请优化下面这段用户输入，使其作为给 AI 的指令更清晰、完整、无歧义。直接输出优化后的文本，不要解释。"
	return e.llmCall(args, []ai.ChatMessage{{Role: "system", Content: sys}, {Role: "user", Content: args.Input}}, 0.5, "polish-input", args.MaxTokens)
}
