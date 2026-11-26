package engine

// 引擎层测试：工具调用解析。
//
// 这块逻辑最容易回归（模型输出的格式五花八门），因此不依赖存储与网络，
// 纯函数直接测。

import (
	"encoding/json"
	"strings"
	"testing"

	"apitool/core/agent"
)

// TestParseBareToolJSON 验证嵌套 arguments 的裸 JSON 工具调用可被解析。
func TestParseBareToolJSON(t *testing.T) {
	raw := "｜｜DSML｜｜\n{\"action\":\"tool\",\"server\":\"builtin\",\"tool\":\"db_query\",\"arguments\":{\"connId\":\"db_1\",\"database\":\"d\",\"sql\":\"SELECT 1\"}}\n｜｜DSML｜｜"
	act, ok := parseToolAction(raw)
	if !ok {
		t.Fatal("嵌套 arguments 的裸 JSON 工具调用未被解析")
	}
	if act.Tool != "db_query" || act.Server != "builtin" {
		t.Fatalf("解析结果错误: tool=%q server=%q", act.Tool, act.Server)
	}
	if act.Arguments["connId"] != "db_1" {
		t.Fatalf("arguments.connId 错误: %v", act.Arguments["connId"])
	}
	// 最终正文不应残留工具调用 JSON
	if clean := stripTags(raw); strings.Contains(clean, `"action"`) {
		t.Fatalf("stripTags 未移除裸 JSON，残留: %q", clean)
	}
}

// TestParseStandardToolCall 标准 <tool_call> 格式。
func TestParseStandardToolCall(t *testing.T) {
	raw := `<tool_call><function name="read_file"><parameter name="path">/tmp/a.txt</parameter></function></tool_call>`
	act, ok := parseToolAction(raw)
	if !ok {
		t.Fatal("标准格式未被解析")
	}
	if act.Tool != "read_file" {
		t.Fatalf("工具名错误: %q", act.Tool)
	}
	if act.Arguments["path"] != "/tmp/a.txt" {
		t.Fatalf("参数错误: %v", act.Arguments)
	}
}

// TestParseUnclosedFunction 函数标签未闭合（模型常见输出）。
func TestParseUnclosedFunction(t *testing.T) {
	raw := `<tool_call><function name="calc"><parameter name="expr">1+2*3</parameter></tool_call>`
	act, ok := parseToolAction(raw)
	if !ok {
		t.Fatal("未闭合 </function> 的调用未被解析")
	}
	if act.Tool != "calc" {
		t.Fatalf("工具名错误: %q", act.Tool)
	}
	if act.Arguments["expr"] != "1+2*3" {
		t.Fatalf("参数错误: %v", act.Arguments)
	}
}

// TestToolActionUnmarshalArgsAsString arguments 被整体写成 JSON 字符串时应能展开。
// 注意：这是 toolAction 自定义 UnmarshalJSON 的职责，不是 parseToolAction 的。
func TestToolActionUnmarshalArgsAsString(t *testing.T) {
	var act toolAction
	raw := `{"action":"tool","server":"builtin","tool":"db_query","arguments":"{\"database\":\"d\",\"sql\":\"SELECT 1\"}"}`
	if err := json.Unmarshal([]byte(raw), &act); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if act.Arguments["database"] != "d" {
		t.Fatalf("参数未展开: %v", act.Arguments)
	}
}

// TestStripTagsRemovesToolBlocks 正文剥离：所有工具调用标记都应被移除。
func TestStripTagsRemovesToolBlocks(t *testing.T) {
	cases := []string{
		`<tool_call><function name="x"><parameter name="a">1</parameter></function></tool_call>`,
		`<function_calls><function name="x"></function></function_calls>`,
		`<thinking>内部思考</thinking>正文`,
	}
	for _, in := range cases {
		got := stripTags(in)
		// 实现保留「零宽字符占位」的中间形态（<​tool_call>），
		// 用于后续配对删除；这里断言的是真实函数名/思考内容已被清空。
		if strings.Contains(got, "function") || strings.Contains(got, "parameter") || strings.Contains(got, "内部思考") {
			t.Fatalf("未清理干净: in=%q out=%q", in, got)
		}
	}
}

// TestBodyFilterSuppressesToolCall 流式过滤：工具调用不应作为正文推给前端。
func TestBodyFilterSuppressesToolCall(t *testing.T) {
	var out strings.Builder
	f := &bodyFilter{emit: func(s string) { out.WriteString(s) }}
	f.Write("这是正文<tool_call><function name=\"x\">")
	f.Write(`<parameter name="a">1</parameter></function></tool_call>`)
	f.Write("结尾正文")
	f.Flush()
	// 过滤器会输出零宽占位标记（<​tool_call>），但绝不能泄漏真实标签名，
	// 也不能把其中的 function / parameter 暴露成正文。
	if strings.Contains(out.String(), "<tool_call>") || strings.Contains(out.String(), "function") {
		t.Fatalf("工具调用泄漏到正文: %q", out.String())
	}
	if !strings.Contains(out.String(), "这是正文") {
		t.Fatalf("正文被误吞: %q", out.String())
	}
}

// TestBuildToolsPromptWithTools 传入工具时提示词应包含调用格式说明。
func TestBuildToolsPromptWithTools(t *testing.T) {
	tools := []agent.MCPTool{{Name: "read_file", Server: "builtin", Description: "读文件"}}
	p := buildToolsPrompt(tools, nil, "react")
	if !strings.Contains(p, "tool_call") {
		t.Fatalf("有工具时提示词应含调用格式说明: %q", p[:min(300, len(p))])
	}
	if !strings.Contains(p, "read_file") {
		t.Fatalf("提示词应列出工具名: %q", p[:min(300, len(p))])
	}
}

// TestBuildToolsPromptEmpty 无工具时不应出现调用格式说明（模型无工具可用）。
func TestBuildToolsPromptEmpty(t *testing.T) {
	p := buildToolsPrompt(nil, nil, "react")
	if strings.Contains(p, "<tool_call>") {
		t.Fatalf("无工具时不应提示工具调用格式: %q", p)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
