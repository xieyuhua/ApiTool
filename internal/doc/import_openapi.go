package doc

// OpenAPI 3 / Swagger 2 的公共解析： 解析、schema → 字段树、示例生成、multipart 表单转换，
// 以及 OpenAPI 3 的 paths / parameters / requestBody 装配。

import (
	"encoding/json"
	"sort"
	"strings"

	"apitool/internal/model"
	"apitool/internal/util"
)

// ---------------- OpenAPI 3 / Swagger 2 公共 ----------------

func resolveRef(ref string, doc map[string]interface{}) map[string]interface{} {
	if !strings.HasPrefix(ref, "#/") {
		return nil
	}
	parts := strings.Split(strings.TrimPrefix(ref, "#/"), "/")
	var cur interface{} = doc
	for _, p := range parts {
		m, ok := cur.(map[string]interface{})
		if !ok {
			return nil
		}
		cur = m[p]
	}
	if m, ok := cur.(map[string]interface{}); ok {
		return m
	}
	return nil
}

func resolveRefSchema(s map[string]interface{}, doc map[string]interface{}) map[string]interface{} {
	if ref, ok := s["$ref"].(string); ok {
		if resolved := resolveRef(ref, doc); resolved != nil {
			return resolved
		}
	}
	return s
}

func schemaToFields(name string, schema map[string]interface{}, doc map[string]interface{}, depth int) *model.Field {
	if depth > 20 {
		return nil
	}
	schema = resolveRefSchema(schema, doc)
	if schema == nil {
		return nil
	}
	field := &model.Field{Name: name}
	if t, ok := schema["type"].(string); ok {
		field.Type = t
	} else {
		field.Type = "object"
	}
	if ex, ok := schema["example"]; ok {
		field.Example = strVal(ex)
	}
	if d, ok := schema["description"].(string); ok {
		field.Description = d
	}
	if req, ok := schema["required"].(bool); ok {
		field.Required = req
	}
	switch field.Type {
	case "array":
		if items, ok := schema["items"].(map[string]interface{}); ok {
			items = resolveRefSchema(items, doc)
			ch := schemaToFields("", items, doc, depth+1)
			if ch != nil {
				if ch.Type == "object" || len(ch.Children) > 0 {
					field.Children = ch.Children
					field.Type = "array[object]"
				} else {
					field.Type = "array[" + ch.Type + "]"
					field.Example = ch.Example
				}
			}
		} else {
			field.Type = "array"
		}
	case "object":
		props, _ := schema["properties"].(map[string]interface{})
		reqList, _ := schema["required"].([]interface{})
		reqSet := map[string]bool{}
		for _, r := range reqList {
			if s, ok := r.(string); ok {
				reqSet[s] = true
			}
		}
		var names []string
		for n := range props {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			sub, _ := props[n].(map[string]interface{})
			if sub == nil {
				continue
			}
			cf := schemaToFields(n, sub, doc, depth+1)
			if cf != nil {
				cf.Required = reqSet[n]
				field.Children = append(field.Children, cf)
			}
		}
	}
	if field.Type == "" {
		field.Type = "object"
	}
	return field
}

func schemaToExample(s map[string]interface{}, doc map[string]interface{}) string {
	v := schemaExampleValue(s, doc)
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}

func schemaExampleValue(s map[string]interface{}, doc map[string]interface{}) interface{} {
	s = resolveRefSchema(s, doc)
	if s == nil {
		return nil
	}
	if ex, ok := s["example"]; ok {
		return ex
	}
	switch strVal(s["type"]) {
	case "object":
		m := map[string]interface{}{}
		props, _ := s["properties"].(map[string]interface{})
		for k, p := range props {
			if pm, ok := p.(map[string]interface{}); ok {
				m[k] = schemaExampleValue(pm, doc)
			}
		}
		return m
	case "array":
		if items, ok := s["items"].(map[string]interface{}); ok {
			return []interface{}{schemaExampleValue(items, doc)}
		}
		return []interface{}{}
	case "integer":
		return 0
	case "number":
		return 0.0
	case "boolean":
		return false
	case "string":
		if d, ok := s["description"].(string); ok && d != "" {
			return d
		}
		return "string"
	}
	return nil
}

func contentSchema(obj map[string]interface{}, doc map[string]interface{}) map[string]interface{} {
	content, ok := obj["content"].(map[string]interface{})
	if !ok {
		return nil
	}
	if cm, ok := content["application/json"].(map[string]interface{}); ok {
		if s, ok := cm["schema"].(map[string]interface{}); ok {
			return resolveRefSchema(s, doc)
		}
	}
	for _, v := range content {
		if cm, ok := v.(map[string]interface{}); ok {
			if s, ok := cm["schema"].(map[string]interface{}); ok {
				return resolveRefSchema(s, doc)
			}
		}
	}
	return nil
}

// isMultipartBody 判断 requestBody 是否为 multipart/form-data
func isMultipartBody(rb map[string]interface{}) bool {
	content, ok := rb["content"].(map[string]interface{})
	if !ok {
		return false
	}
	_, ok = content["multipart/form-data"].(map[string]interface{})
	return ok
}

// multipartToFormItems 将 multipart/form-data 的 schema 还原为表单字段
func multipartToFormItems(rb map[string]interface{}) []model.KV {
	var items []model.KV
	content, ok := rb["content"].(map[string]interface{})
	if !ok {
		return items
	}
	cm, ok := content["multipart/form-data"].(map[string]interface{})
	if !ok {
		return items
	}
	schema, ok := cm["schema"].(map[string]interface{})
	if !ok {
		return items
	}
	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		return items
	}
	for name, pv := range props {
		pm, ok := pv.(map[string]interface{})
		if !ok {
			items = append(items, model.KV{Enabled: true, Key: name, Type: "text"})
			continue
		}
		typ := "text"
		if fmt, ok := pm["format"].(string); ok && fmt == "binary" {
			typ = "file"
		} else if t, ok := pm["type"].(string); ok && t == "string" && fmt == "" {
			typ = "text"
		}
		item := model.KV{Enabled: true, Key: name, Type: typ, Value: "", Description: strVal(pm["description"])}
		if ex, ok := pm["example"]; ok && typ != "file" {
			item.Value = strVal(ex)
		}
		items = append(items, item)
	}
	return items
}

func paramToKV(p map[string]interface{}) model.KV {
	kv := model.KV{Enabled: true, Key: strVal(p["name"]), Description: strVal(p["description"])}
	if req, ok := p["required"].(bool); ok && req {
		kv.Description = "必填。" + kv.Description
	}
	if ex, ok := p["example"]; ok {
		kv.Value = strVal(ex)
	}
	if kv.Value == "" {
		if sch, ok := p["schema"].(map[string]interface{}); ok {
			if ex, ok := sch["example"]; ok {
				kv.Value = strVal(ex)
			}
		}
		// 无 example 时不再用类型名兜底，Value 保持为空
	}
	return kv
}

// ---------------- OpenAPI 3 ----------------

func parseOpenAPI3(doc map[string]interface{}, title string) ([]model.Directory, []model.ApiInfo) {
	var dirs []model.Directory
	var apis []model.ApiInfo

	base := ""
	if servers, ok := doc["servers"].([]interface{}); ok && len(servers) > 0 {
		if s, ok := servers[0].(map[string]interface{}); ok {
			base = strVal(s["url"])
		}
	}
	// 目录：优先用顶层 tags 声明；兼容 Apipost 等「接口自带 tags 但未在顶层声明」的导出，
	// 在遍历接口时若遇到未声明的 tag 也补建目录，保证按目录分组。
	// tag 名中含 "/" 时拆成多级嵌套目录（如 "用户管理/账号"）。
	tagDir := map[string]string{}
	if tl, ok := doc["tags"].([]interface{}); ok {
		for _, t := range tl {
			if tm, ok := t.(map[string]interface{}); ok {
				ensureTagPath(strVal(tm["name"]), &dirs, tagDir)
			}
		}
	}

	paths, _ := doc["paths"].(map[string]interface{})
	for path, pm := range paths {
		pmap, ok := pm.(map[string]interface{})
		if !ok {
			continue
		}
		for method, opv := range pmap {
			op, ok := opv.(map[string]interface{})
			if !ok {
				continue
			}
			method = strings.ToUpper(method)
			if method == "" || method == "PARAMETERS" {
				continue
			}
			api := model.ApiInfo{
				ID: util.GenID(), Name: strVal(op["summary"]), Method: method,
				URL: joinURL(base, path), Description: strVal(op["description"]), BodyType: "json",
			}
			if api.Name == "" {
				api.Name = method + " " + path
			}
			if tl, ok := op["tags"].([]interface{}); ok && len(tl) > 0 {
				for _, t := range tl {
					if name, ok := t.(string); ok {
						api.DirID = ensureTagPath(name, &dirs, tagDir)
					}
				}
			}
			apis = append(apis, buildOpenAPIOp(api, op, doc))
		}
	}
	return dirs, apis
}

func buildOpenAPIOp(api model.ApiInfo, op map[string]interface{}, doc map[string]interface{}) model.ApiInfo {
	if params, ok := op["parameters"].([]interface{}); ok {
		for _, pv := range params {
			p, ok := pv.(map[string]interface{})
			if !ok {
				continue
			}
			kv := paramToKV(p)
			switch strVal(p["in"]) {
			case "query":
				api.Query = append(api.Query, kv)
			case "header":
				api.Headers = append(api.Headers, kv)
			case "path":
				api.Query = append(api.Query, kv)
			}
		}
	}
	if rb, ok := op["requestBody"].(map[string]interface{}); ok {
		// multipart/form-data → 表单参数
		if isMultipartBody(rb) {
			api.BodyType = "form"
			api.FormItems = multipartToFormItems(rb)
		} else if s := contentSchema(rb, doc); s != nil {
			if f := schemaToFields("", s, doc, 0); f != nil {
				api.ReqFields = f.Children
			}
			api.Body = schemaToExample(s, doc)
			api.BodyType = "json"
		}
	}
	if resp, ok := op["responses"].(map[string]interface{}); ok {
		for _, code := range []string{"200", "201", "default"} {
			if rv, ok := resp[code]; ok {
				if rm, ok := rv.(map[string]interface{}); ok {
					if s := contentSchema(rm, doc); s != nil {
						if f := schemaToFields("", s, doc, 0); f != nil {
							api.RespFields = f.Children
						}
					}
					break
				}
			}
		}
	}
	return api
}
