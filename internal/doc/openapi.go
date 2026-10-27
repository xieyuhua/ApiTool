package doc

// OpenAPI 3.0 导出：把抓包/整理出的接口还原为标准 OpenAPI 文档，便于导入其他平台。

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"apitool/internal/jsonutil"
	"apitool/internal/model"
)

func schemaFromFields(fields []*model.Field) map[string]interface{} {
	props := map[string]interface{}{}
	var required []string
	for _, f := range fields {
		props[f.Name] = schemaFromField(f)
		if f.Required {
			required = append(required, f.Name)
		}
	}
	schema := map[string]interface{}{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func schemaFromField(f *model.Field) map[string]interface{} {
	t := f.Type
	desc := f.Description
	base := func(typ string) map[string]interface{} {
		m := map[string]interface{}{"type": typ}
		if desc != "" {
			m["description"] = desc
		}
		if f.Example != "" && typ != "object" && typ != "array" {
			m["example"] = f.Example
		}
		return m
	}
	switch {
	case t == "object":
		m := schemaFromFields(f.Children)
		if desc != "" {
			m["description"] = desc
		}
		return m
	case strings.HasPrefix(t, "array"):
		var items map[string]interface{}
		if t == "array[object]" || len(f.Children) > 0 {
			items = schemaFromFields(f.Children)
		} else {
			inner := strings.TrimSuffix(strings.TrimPrefix(t, "array["), "]")
			if inner == "" || inner == "array" {
				inner = "string"
			}
			items = map[string]interface{}{"type": inner}
		}
		m := map[string]interface{}{"type": "array", "items": items}
		if desc != "" {
			m["description"] = desc
		}
		return m
	case t == "integer", t == "number", t == "boolean", t == "string":
		return base(t)
	case t == "null":
		return base("string")
	default:
		return base("string")
	}
}

// parseFormBodyToKVs 将表单请求体解析为表单字段（兼容 JSON 对象与 urlencoded 两种格式）。
func parseFormBodyToKVs(body string) []model.KV {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}
	kvs := []model.KV{}
	if strings.HasPrefix(body, "{") {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(body), &m); err == nil {
			for k, v := range m {
				kvs = append(kvs, model.KV{Enabled: true, Key: k, Value: fmt.Sprint(v)})
			}
			return kvs
		}
	}
	vals, err := url.ParseQuery(body)
	if err != nil {
		return nil
	}
	for k := range vals {
		kvs = append(kvs, model.KV{Enabled: true, Key: k, Value: vals.Get(k)})
	}
	return kvs
}

// formFieldValueSchema 若表单字段值是 JSON 对象/数组，则转换为结构化 schema，
// 避免把整段 JSON 作为字符串 example 导出（如 data=[{...}] / payload={...} 这类常见用法）。
func formFieldValueSchema(val string) map[string]interface{} {
	val = strings.TrimSpace(val)
	if val == "" || !(strings.HasPrefix(val, "{") || strings.HasPrefix(val, "[")) {
		return nil
	}
	fields, err := jsonutil.ParseFields(val, nil)
	if err != nil || len(fields) == 0 {
		return nil
	}
	if strings.HasPrefix(val, "[") {
		// 顶层为数组：包装成 array[items=对象]，避免丢失数组外壳
		return map[string]interface{}{
			"type":  "array",
			"items": schemaFromFields(fields),
		}
	}
	return schemaFromFields(fields)
}

// BuildOpenAPI 生成标准 OpenAPI 3.0.3 文档。
// hostMode:
//   - "original"：接口地址使用抓包实际完整地址（含 host/path/query），不附加 servers，保证导入后地址不被替换；
//   - "env"：将 host 替换为环境变量 {{host}}，servers=[{url:"{{host}}"}]，path 仅保留路径（Query 以 parameters 形式导出，标准做法）。
func BuildOpenAPI(title string, dirs []model.Directory, apis []model.ApiInfo, rootID string, common model.CommonParams, hostMode string) (string, error) {
	paths := map[string]map[string]interface{}{}
	usedTags := []string{}
	envHost := hostMode == "env"
	for _, api := range apis {
		var p string
		if envHost {
			// 环境变量模式：host 抽成 {{host}}，path 仅保留路径（Query 以 parameters 形式导出）
			if u, err := url.Parse(api.URL); err == nil && (u.Path != "" || u.RawQuery != "") {
				base := u.Path
				if base == "" {
					base = "/"
				}
				p = base
			} else {
				p = api.URL
			}
		} else {
			// 原地址模式：保留抓包实际完整地址（host+path+query），导入其他平台即真实地址
			p = api.URL
		}
		if p == "" {
			p = "/"
		}
		if paths[p] == nil {
			paths[p] = map[string]interface{}{}
		}
		// 目录分组：取接口所属目录的完整层级路径（如 "用户中心 / 用户管理"），无目录则归入"未分类"
		tag := dirPathOf(dirs, api.DirID)
		if tag == "" {
			tag = "未分类"
		}
		usedTags = append(usedTags, tag)
		op := map[string]interface{}{
			"tags":        []string{tag},
			"summary":     p,
			"description": api.Description,
			"responses": map[string]interface{}{
				"200": map[string]interface{}{
					"description": "成功",
					"content": map[string]interface{}{
						"application/json": map[string]interface{}{
							"schema": schemaFromFields(api.RespFields),
						},
					},
				},
			},
		}
		var params []map[string]interface{}
		// 原地址模式下 Query 已包含在路径完整 URL 中，不再重复；env 模式按标准以 parameters 导出
		if envHost {
			for _, kv := range EnabledKVs(api.Query) {
				params = append(params, map[string]interface{}{
					"name": kv.Key, "in": "query", "description": kv.Description,
					"schema": map[string]string{"type": "string"}, "example": kv.Value,
				})
			}
		}
		for _, kv := range EnabledKVs(api.Headers) {
			params = append(params, map[string]interface{}{
				"name": kv.Key, "in": "header", "description": kv.Description,
				"schema": map[string]string{"type": "string"}, "example": kv.Value,
			})
		}
		// 公共参数：项目级自动附加（接口同名覆盖公共），仅追加接口未定义的
		defined := map[string]bool{}
		for _, kv := range append(append([]model.KV{}, api.Headers...), api.Query...) {
			if kv.Enabled && kv.Key != "" {
				defined[kv.Key] = true
			}
		}
		for _, kv := range EnabledKVs(common.Query) {
			if !defined[kv.Key] {
				params = append(params, map[string]interface{}{
					"name": kv.Key, "in": "query", "description": "[公共参数] " + kv.Description,
					"schema": map[string]string{"type": "string"}, "example": kv.Value,
				})
			}
		}
		for _, kv := range EnabledKVs(common.Headers) {
			if !defined[kv.Key] {
				params = append(params, map[string]interface{}{
					"name": kv.Key, "in": "header", "description": "[公共参数] " + kv.Description,
					"schema": map[string]string{"type": "string"}, "example": kv.Value,
				})
			}
		}
		if len(params) > 0 {
			op["parameters"] = params
		}
		// 请求体：优先使用结构化字段；字段为空但有原始 Body 时，实时解析兜底，避免 POST 参数丢失。
		// 非 form 类型的 Body 均尝试按 JSON 解析（扩展若把 JSON 体误标为 text/none 也能兜住）。
		reqFields := api.ReqFields
		if len(reqFields) == 0 && api.BodyType != "form" && strings.TrimSpace(api.Body) != "" {
			if f, err := jsonutil.ParseFields(api.Body, nil); err == nil && len(f) > 0 {
				reqFields = f
			}
		}
		if len(reqFields) > 0 {
			op["requestBody"] = map[string]interface{}{
				"content": map[string]interface{}{
					"application/json": map[string]interface{}{
						"schema": schemaFromFields(reqFields),
					},
				},
			}
		}
		// 表单参数（Form / 文件上传）：生成 multipart/form-data requestBody
		// 若 FormItems 为空但有 form 类型 Body，则尝试从 Body 解析兜底
		formKVs := EnabledKVs(api.FormItems)
		if len(formKVs) == 0 && api.BodyType == "form" && strings.TrimSpace(api.Body) != "" {
			formKVs = EnabledKVs(parseFormBodyToKVs(api.Body))
		}
		if len(formKVs) > 0 {
			props := map[string]interface{}{}
			required := []string{}
			for _, kv := range formKVs {
				if kv.Type == model.FormTypeFile {
					props[kv.Key] = map[string]interface{}{
						"type":        "string",
						"format":      "binary",
						"description": kv.Description,
					}
				} else if sch := formFieldValueSchema(kv.Value); sch != nil {
					// 字段值本身是 JSON（对象/数组）：以结构化 schema 表达，避免整段 JSON 作为 example
					if kv.Description != "" {
						sch["description"] = kv.Description
					}
					props[kv.Key] = sch
				} else {
					props[kv.Key] = map[string]interface{}{
						"type":        "string",
						"description": kv.Description,
						"example":     kv.Value,
					}
				}
				required = append(required, kv.Key)
			}
			op["requestBody"] = map[string]interface{}{
				"content": map[string]interface{}{
					"multipart/form-data": map[string]interface{}{
						"schema": map[string]interface{}{
							"type":       "object",
							"properties": props,
							"required":   required,
						},
					},
				},
			}
		}
		method := strings.ToLower(api.Method)
		if method == "" {
			method = "get"
		}
		paths[p][method] = op
	}
	servers := []map[string]interface{}{}
	if envHost {
		servers = append(servers, map[string]interface{}{"url": "{{host}}"})
	}
	doc := map[string]interface{}{
		"openapi": "3.0.3",
		"info": map[string]interface{}{
			"title":   title,
			"version": "1.0.0",
		},
		"servers": servers,
		"tags":    collectTags(usedTags),
		"paths":   paths,
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// dirPathOf 根据目录列表，返回从根到该目录的完整层级路径（如 "用户中心 / 用户管理"）。
func dirPathOf(dirs []model.Directory, id string) string {
	if id == "" {
		return ""
	}
	byID := map[string]model.Directory{}
	for _, d := range dirs {
		byID[d.ID] = d
	}
	chain := []string{}
	cur := id
	guard := 0
	for cur != "" {
		d, ok := byID[cur]
		if !ok {
			break
		}
		chain = append([]string{d.Name}, chain...)
		cur = d.ParentID
		if guard++; guard > 50 {
			break
		}
	}
	return strings.Join(chain, " / ")
}

// collectTags 根据出现的目录路径收集根 tags 声明（含每一层级），便于 Swagger/Postman 按目录分组展示。
func collectTags(usedPaths []string) []map[string]interface{} {
	exists := map[string]bool{}
	var ordered []string
	for _, p := range usedPaths {
		parts := strings.Split(p, " / ")
		prefix := ""
		for _, part := range parts {
			if prefix == "" {
				prefix = part
			} else {
				prefix = prefix + " / " + part
			}
			if !exists[prefix] {
				exists[prefix] = true
				ordered = append(ordered, prefix)
			}
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if strings.Count(ordered[i], " / ") != strings.Count(ordered[j], " / ") {
			return strings.Count(ordered[i], " / ") < strings.Count(ordered[j], " / ")
		}
		return ordered[i] < ordered[j]
	})
	tags := make([]map[string]interface{}, 0, len(ordered))
	for _, p := range ordered {
		tags = append(tags, map[string]interface{}{"name": p})
	}
	return tags
}
