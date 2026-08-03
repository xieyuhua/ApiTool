// Package ai 提供与 OpenAI 兼容大模型的通用调用能力，供 agent 运行、文档字段描述生成
// （GenerateDescriptions）等模块复用。这里只放「与 App 实例无关」的纯逻辑与类型，
// 避免 main 包与 internal 包之间的循环依赖。
package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"apitool/internal/jsonutil"
	"apitool/internal/model"
)

// Host 是 ai 包所需的宿主能力（由 main 包 *App 实现）。
type Host interface {
	// ReadData 返回当前全部应用数据（含 AI 设置）。
	ReadData() model.AppData
}

// AppVersion 应用版本号，供 Agent 的 MCP stdio 客户端作为 userAgent 上报。
const AppVersion = "1.0.0"

// ChatMessage 简单的聊天消息结构（role/content）。
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Request / Result / StreamOptions / Choice 对应 OpenAI 兼容接口的请求与响应结构。
type Request struct {
	Model         string        `json:"model"`
	Messages      []ChatMessage `json:"messages"`
	Temperature   float64       `json:"temperature"`
	Stream        bool          `json:"stream"`
	StreamOptions *StreamOptions `json:"stream_options,omitempty"`
}

type StreamOptions struct {
	Probe        int `json:"probe,omitempty"`
	IncludeUsage bool `json:"include_usage"`
}

type Choice struct {
	Message ChatMessage `json:"message"`
}

type Result struct {
	Choices []Choice     `json:"choices"`
	Error   interface{}  `json:"error"`
}

// ExtractJSON 从可能夹杂说明文字的 AI 返回中提取第一个 JSON 片段（对象或数组）。
func ExtractJSON(s string) string {
	startObj := strings.Index(s, "{")
	endObj := strings.LastIndex(s, "}")
	startArr := strings.Index(s, "[")
	endArr := strings.LastIndex(s, "]")
	var start, end int
	if startObj < 0 && startArr < 0 {
		return ""
	}
	if startArr >= 0 && (startObj < 0 || startArr < startObj) {
		start, end = startArr, endArr
	} else {
		start, end = startObj, endObj
	}
	if start < 0 || end <= start {
		return ""
	}
	return s[start : end+1]
}

// ChatRaw 调用 OpenAI 兼容接口，返回助手消息文本。
// 兼容以 /v1 结尾或已含 /chat/completions 的 base；超时单位秒（<=0 时回退 30s）。
// 错误时优先提取响应体中的 error.message，否则回退原始错误文本。
// CallAI 与 Chat 均委托此函数，避免重复实现 HTTP 调用。
func ChatRaw(base, apiKey, model string, messages []ChatMessage, timeoutSec int) (string, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "gpt-4o-mini"
	}
	url := base
	if !strings.HasSuffix(url, "/chat/completions") {
		if strings.HasSuffix(url, "/v1") {
			url = url + "/chat/completions"
		} else {
			url = url + "/v1/chat/completions"
		}
	}
	if timeoutSec <= 0 {
		timeoutSec = 30
	}

	payload, err := json.Marshal(Request{
		Model:       model,
		Messages:    messages,
		Temperature: 0.3,
		Stream:      false,
	})
	if err != nil {
		return "", fmt.Errorf("构造请求失败: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("AI 请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var r Result
		_ = json.Unmarshal(body, &r)
		if r.Error != nil {
			if m, ok := r.Error.(map[string]interface{}); ok {
				if msg, ok := m["message"].(string); ok {
					return "", fmt.Errorf("AI 请求失败: %s", msg)
				}
			}
		}
		return "", fmt.Errorf("AI 请求失败 %d: %s", resp.StatusCode, string(body))
	}

	var r Result
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}
	if len(r.Choices) == 0 {
		return "", fmt.Errorf("AI 返回内容为空")
	}
	return r.Choices[0].Message.Content, nil
}

// Chat 调用 OpenAI 兼容接口，返回助手消息文本。
// 内部处理鉴权、模型、超时与错误响应。system 为系统提示，user 为用户提示。
// 实现上委托 ChatRaw，与 CallAI 共用同一套 HTTP 逻辑。
func Chat(s model.Settings, system, user string) (string, error) {
	base := s.AIBaseURL
	if strings.TrimSpace(base) == "" {
		base = "https://api.openai.com/v1"
	}
	model := s.AIModel
	if model == "" {
		model = "gpt-4o-mini"
	}
	messages := []ChatMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}
	return ChatRaw(base, s.AIKey, model, messages, 180)
}

// ---------------- 字段描述生成 ----------------

type flatField struct {
	Path    string `json:"path"`
	Type    string `json:"type"`
	Example string `json:"example,omitempty"`
}

func flattenFields(fields []*model.Field, prefix string, out *[]flatField, need map[string]bool) {
	for _, f := range fields {
		p := jsonutil.FieldPath(prefix, f.Name)
		*out = append(*out, flatField{Path: p, Type: f.Type, Example: f.Example})
		if f.Description == "" {
			need[p] = true
		}
		flattenFields(f.Children, p, out, need)
	}
}

func applyAIDesc(fields []*model.Field, prefix string, m map[string]string, overwrite bool) {
	for _, f := range fields {
		p := jsonutil.FieldPath(prefix, f.Name)
		if d, ok := m[p]; ok && d != "" {
			if overwrite || f.Description == "" {
				f.Description = d
			}
		}
		applyAIDesc(f.Children, p, m, overwrite)
	}
}

// GenerateDescriptions 使用 AI 为字段自动生成描述（仅填充空白描述）。
// 通过 host 读取 AI 设置，复用 Chat 调用 OpenAI 兼容接口。
func GenerateDescriptions(host Host, apiName string, apiDesc string, fields []*model.Field) ([]*model.Field, error) {
	s := host.ReadData().Settings
	if strings.TrimSpace(s.AIKey) == "" {
		return nil, fmt.Errorf("请先在「设置」中配置 AI API Key")
	}

	var flat []flatField
	need := map[string]bool{}
	flattenFields(fields, "", &flat, need)
	if len(need) == 0 {
		return fields, nil
	}

	fieldsJSON, _ := json.Marshal(flat)
	prompt := fmt.Sprintf(`接口名称：%s
接口说明：%s
以下是接口的参数字段列表（JSON 数组，包含字段路径、类型、示例值）：
%s

请根据字段名、类型和示例值，推断每个字段的中文含义描述。描述要简洁专业，不超过 30 个字。
只返回一个 JSON 对象，键为字段路径，值为中文描述，不要输出其他任何内容。`, apiName, apiDesc, string(fieldsJSON))

	system := "你是一个 API 文档专家，擅长根据字段名推断字段含义。只输出 JSON。"
	content, err := Chat(s, system, prompt)
	if err != nil {
		return nil, err
	}
	jsonStr := ExtractJSON(content)
	if jsonStr == "" {
		return nil, fmt.Errorf("AI 未返回有效的 JSON 结果")
	}
	descMap := map[string]string{}
	if err := json.Unmarshal([]byte(jsonStr), &descMap); err != nil {
		return nil, fmt.Errorf("AI 结果解析失败: %v", err)
	}

	applyAIDesc(fields, "", descMap, false)
	return fields, nil
}
