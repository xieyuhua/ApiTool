package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type flatField struct {
	Path    string `json:"path"`
	Type    string `json:"type"`
	Example string `json:"example,omitempty"`
}

func flattenFields(fields []*Field, prefix string, out *[]flatField, need map[string]bool) {
	for _, f := range fields {
		p := fieldPath(prefix, f.Name)
		*out = append(*out, flatField{Path: p, Type: f.Type, Example: f.Example})
		if f.Description == "" {
			need[p] = true
		}
		flattenFields(f.Children, p, out, need)
	}
}

func applyAIDesc(fields []*Field, prefix string, m map[string]string, overwrite bool) {
	for _, f := range fields {
		p := fieldPath(prefix, f.Name)
		if d, ok := m[p]; ok && d != "" {
			if overwrite || f.Description == "" {
				f.Description = d
			}
		}
		applyAIDesc(f.Children, p, m, overwrite)
	}
}

// GenerateDescriptions 使用 AI 为字段自动生成描述（仅填充空白描述）
func (a *App) GenerateDescriptions(apiName string, apiDesc string, fields []*Field) ([]*Field, error) {
	data := a.readData()
	s := data.Settings
	if strings.TrimSpace(s.AIKey) == "" {
		return nil, fmt.Errorf("请先在「设置」中配置 AI API Key")
	}
	base := strings.TrimRight(strings.TrimSpace(s.AIBaseURL), "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	model := s.AIModel
	if model == "" {
		model = "gpt-4o-mini"
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

	reqBody := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": "你是一个 API 文档专家，擅长根据字段名推断字段含义。只输出 JSON。"},
			{"role": "user", "content": prompt},
		},
		"temperature": 0.2,
	}
	b, _ := json.Marshal(reqBody)

	req, err := http.NewRequest("POST", base+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.AIKey)

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("AI 请求失败: %v", err)
	}
	defer resp.Body.Close()
	respBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if resp.StatusCode != 200 {
		msg := string(respBytes)
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return nil, fmt.Errorf("AI 接口返回错误(%d): %s", resp.StatusCode, msg)
	}

	var chatResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBytes, &chatResp); err != nil || len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("AI 响应解析失败")
	}
	content := chatResp.Choices[0].Message.Content

	// 提取 JSON 对象
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("AI 未返回有效的 JSON 结果")
	}
	descMap := map[string]string{}
	if err := json.Unmarshal([]byte(content[start:end+1]), &descMap); err != nil {
		return nil, fmt.Errorf("AI 结果解析失败: %v", err)
	}

	applyAIDesc(fields, "", descMap, false)
	return fields, nil
}
