package main

import (
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

// decodeValue 按原始顺序解析 JSON
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

func fieldFromValue(name string, v interface{}) *Field {
	f := &Field{Name: name, Type: typeOf(v), Example: exampleOf(v)}
	switch t := v.(type) {
	case omap:
		for _, p := range t {
			f.Children = append(f.Children, fieldFromValue(p.key, p.val))
		}
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

func fieldPath(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

func collectDesc(fields []*Field, prefix string, m map[string]*Field) {
	for _, f := range fields {
		p := fieldPath(prefix, f.Name)
		m[p] = f
		collectDesc(f.Children, p, m)
	}
}

func applyDesc(fields []*Field, prefix string, m map[string]*Field) {
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
func (a *App) ParseFields(jsonStr string, existing []*Field) ([]*Field, error) {
	dec := json.NewDecoder(strings.NewReader(jsonStr))
	dec.UseNumber()
	v, err := decodeValue(dec)
	if err != nil {
		return nil, fmt.Errorf("JSON 解析失败: %v", err)
	}

	var fields []*Field
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
			fields = []*Field{root}
		}
	default:
		return nil, fmt.Errorf("请提供 JSON 对象或数组")
	}

	old := map[string]*Field{}
	collectDesc(existing, "", old)
	applyDesc(fields, "", old)
	return fields, nil
}

// FormatJSON 格式化 JSON 文本
func (a *App) FormatJSON(jsonStr string) (string, error) {
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
