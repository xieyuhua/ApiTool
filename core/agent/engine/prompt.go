package engine

// 引擎层：提示词构建。

import (
	"fmt"
	"strings"

	"apitool/core/agent"
)

// buildToolsPrompt 根据可用 MCP 工具与技能构造系统提示，指导模型用 JSON 协议调用工具。
func buildToolsPrompt(tools []agent.MCPTool, skills []agent.AgentSkill, mode string) string {
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
