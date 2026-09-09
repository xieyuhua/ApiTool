// Package jsonutil 提供保留原始顺序的 JSON 解析、字段树推断与格式化。
package jsonutil

import (
	"apitool/internal/model"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

type opair struct {
	key string
	val interface{}
}
type omap []opair

// DecodeValue 按原始顺序解析 JSON（对象保留插入顺序）
func DecodeValue(dec *json.Decoder) (interface{}, error) {
	return decodeValue(dec)
}

// FieldFromValue 从解析出的值推断字段结构
func FieldFromValue(name string, v interface{}) *model.Field {
	return fieldFromValue(name, v)
}

// FieldPath 拼接字段路径
func FieldPath(prefix, name string) string {
	return fieldPath(prefix, name)
}

func decodeValue(dec *json.Decoder) (interface{}, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			m := omap{}
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, _ := keyTok.(string)
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				m = append(m, opair{key, v})
			}
			if _, err := dec.Token(); err != nil { // }
				return nil, err
			}
			return m, nil
		case '[':
			arr := []interface{}{}
			for dec.More() {
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, v)
			}
			if _, err := dec.Token(); err != nil { // ]
				return nil, err
			}
			return arr, nil
		}
	}
	return tok, nil
}

func typeOf(v interface{}) string {
	switch t := v.(type) {
	case omap:
		return "object"
	case []interface{}:
		return "array"
	case string:
		return "string"
	case float64:
		if t == math.Trunc(t) && math.Abs(t) < 1e15 {
			return "integer"
		}
		return "number"
	case json.Number:
		if !strings.ContainsAny(t.String(), ".eE") {
			return "integer"
		}
		return "number"
	case bool:
		return "boolean"
	case nil:
		return "null"
	}
	return "string"
}

func exampleOf(v interface{}) string {
	switch t := v.(type) {
	case omap, []interface{}:
		return ""
	case string:
		if len(t) > 60 {
			return t[:60] + "..."
		}
		return t
	case float64:
		if t == math.Trunc(t) && math.Abs(t) < 1e15 {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%v", t)
	case nil:
		return "null"
	default:
		return fmt.Sprintf("%v", v)
	}
}

func fieldFromValue(name string, v interface{}) *model.Field {
	f := &model.Field{Name: name, Type: typeOf(v), Example: exampleOf(v)}
	switch t := v.(type) {
	case omap:
		for _, p := range t {
			f.Children = append(f.Children, fieldFromValue(p.key, p.val))
		}
	case string:
		// 若字符串本身是合法 JSON（对象/数组），展开为结构化字段，
		// 避免整段 JSON 被当成 string 的 example 而截断（如 data="[{...}]" 这类常见用法）。
		if s := strings.TrimSpace(t); s != "" && (strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[")) {
			if sub, err := ParseFields(s, nil); err == nil && len(sub) > 0 {
				return jsonStringToField(name, sub)
			}
		}
		return f
	case []interface{}:
		if len(t) > 0 {
			first := t[0]
			switch ft := first.(type) {
			case omap:
				for _, p := range ft {
					f.Children = append(f.Children, fieldFromValue(p.key, p.val))
				}
				f.Type = "array[object]"
			default:
				f.Type = "array[" + typeOf(first) + "]"
				f.Example = exampleOf(first)
			}
		}
	}
	return f
}

// jsonStringToField 将“字符串本身是 JSON”解析出的子字段，包装成以 name 命名的字段。
// sub 为 ParseFields 对字符串内容的解析结果：
//   - 顶层数组（或标量数组）：ParseFields 返回单个名为 "(root)" 的字段，直接复用并改名；
//   - 顶层对象：返回若干属性字段，包装为 object 的 children。
func jsonStringToField(name string, sub []*model.Field) *model.Field {
	f := &model.Field{Name: name}
	if len(sub) == 1 && sub[0].Name == "(root)" {
		f.Type = sub[0].Type
		f.Children = sub[0].Children
		f.Description = sub[0].Description
		return f
	}
	f.Type = "object"
	f.Children = sub
	return f
}

func fieldPath(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

func collectDesc(fields []*model.Field, prefix string, m map[string]*model.Field) {
	for _, f := range fields {
		p := fieldPath(prefix, f.Name)
		m[p] = f
		collectDesc(f.Children, p, m)
	}
}

func applyDesc(fields []*model.Field, prefix string, m map[string]*model.Field) {
	for _, f := range fields {
		p := fieldPath(prefix, f.Name)
		if old, ok := m[p]; ok {
			if f.Description == "" {
				f.Description = old.Description
			}
			f.Required = old.Required
		}
		applyDesc(f.Children, p, m)
	}
}

// ParseFields 将 JSON 文本解析为字段树，并合并已有字段的描述信息
func ParseFields(jsonStr string, existing []*model.Field) ([]*model.Field, error) {
	dec := json.NewDecoder(strings.NewReader(jsonStr))
	dec.UseNumber()
	v, err := decodeValue(dec)
	if err != nil {
		return nil, fmt.Errorf("JSON 解析失败: %v", err)
	}

	var fields []*model.Field
	switch t := v.(type) {
	case omap:
		for _, p := range t {
			fields = append(fields, fieldFromValue(p.key, p.val))
		}
	case []interface{}:
		root := fieldFromValue("(root)", t)
		if len(root.Children) > 0 {
			fields = root.Children
		} else {
			fields = []*model.Field{root}
		}
	default:
		return nil, fmt.Errorf("请提供 JSON 对象或数组")
	}

	old := map[string]*model.Field{}
	collectDesc(existing, "", old)
	applyDesc(fields, "", old)
	return fields, nil
}

// FormatJSON 格式化 JSON 文本
func FormatJSON(jsonStr string) (string, error) {
	var v interface{}
	dec := json.NewDecoder(strings.NewReader(jsonStr))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return "", fmt.Errorf("JSON 格式错误: %v", err)
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
