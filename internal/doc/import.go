package doc

import (
	
	"context"
	"apitool/internal/store"
	"apitool/internal/util"
	"apitool/internal/model"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"gopkg.in/yaml.v3"
)

// ImportDoc 选择并导入接口文档（OpenAPI 3 / Swagger 2 / Postman）
func ImportDoc(ctx context.Context, s *store.Store, version, updateURL string, projectID string) (string, error) {
	path, err := runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{
		Title: "导入接口文档",
		Filters: []runtime.FileFilter{
			{DisplayName: "接口文档 (OpenAPI/Swagger/Postman)", Pattern: "*.json;*.yaml;*.yml"},
			{DisplayName: "JSON (*.json)", Pattern: "*.json"},
			{DisplayName: "YAML (*.yaml;*.yml)", Pattern: "*.yaml;*.yml"},
		},
	})
	if err != nil || path == "" {
		return "", err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取文件失败: %v", err)
	}
	doc, err := decodeDoc(raw)
	if err != nil {
		return "", err
	}

	var dirs []model.Directory
	var apis []model.ApiInfo

	switch detectFormat(doc) {
	case "openapi3":
		title := mapStr(doc, "info", "title")
		dirs, apis = parseOpenAPI3(doc, orDefault(title, "导入文档"))
	case "swagger2":
		title := mapStr(doc, "info", "title")
		dirs, apis = parseSwagger2(doc, orDefault(title, "导入文档"))
	case "postman":
		title := mapStr(doc, "info", "name")
		dirs, apis = parsePostman(doc, orDefault(title, "Postman 导入"))
	default:
		return "", fmt.Errorf("无法识别的文档格式，仅支持 OpenAPI 3.0 / Swagger 2.0 / Postman Collection")
	}

	if len(apis) == 0 {
		return "", fmt.Errorf("文档中未解析到任何接口")
	}

	// 外层根目录包裹，避免与现有数据混淆
	rootName := fmt.Sprintf("导入-%s", baseName(path))
	root := model.Directory{ID: util.GenID(), Name: rootName, ParentID: "", Sort: 0}
	// 一级目录指向 root
	for i := range dirs {
		if dirs[i].ParentID == "" {
			dirs[i].ParentID = root.ID
		}
	}
	dirs = append([]model.Directory{root}, dirs...)

	// 兜底：未归类接口挂到根目录
	dirSet := map[string]bool{}
	for _, d := range dirs {
		dirSet[d.ID] = true
	}
	for i := range apis {
		if !dirSet[apis[i].DirID] {
			apis[i].DirID = root.ID
		}
	}

	// 剪枝：删除没有任何接口（含后代）的空目录，避免导入后产生大量无用的空文件夹
	dirs = pruneEmptyDirs(dirs, apis)

	data := s.Read(version, updateURL)
	idx := store.ActiveProjectIndex(data)
	if idx < 0 {
		return "", fmt.Errorf("没有可用的项目")
	}
	data.Projects[idx].Dirs = append(data.Projects[idx].Dirs, dirs...)
	data.Projects[idx].Apis = append(data.Projects[idx].Apis, apis...)
	data.Projects[idx].UpdatedAt = time.Now().Format(time.RFC3339)
	if err := s.Write(data); err != nil {
		return "", err
	}
	return fmt.Sprintf("导入成功：%d 个目录、%d 个接口（已放入项目「%s」的「%s」）", len(dirs), len(apis), data.Projects[idx].Name, rootName), nil
}

// ---------------- 通用工具 ----------------

func decodeDoc(raw []byte) (map[string]interface{}, error) {
	s := strings.TrimSpace(string(raw))
	if strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[") {
		var m map[string]interface{}
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("JSON 解析失败: %v", err)
		}
		return m, nil
	}
	var m map[string]interface{}
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("文档解析失败，文件不是有效的 JSON/YAML: %v", err)
	}
	return m, nil
}

func detectFormat(doc map[string]interface{}) string {
	if _, ok := doc["openapi"]; ok {
		return "openapi3"
	}
	if _, ok := doc["swagger"]; ok {
		return "swagger2"
	}
	if info, ok := doc["info"].(map[string]interface{}); ok {
		if sch, ok := info["schema"].(string); ok && strings.Contains(sch, "getpostman") {
			return "postman"
		}
	}
	if _, ok := doc["_postman_id"]; ok {
		return "postman"
	}
	if _, ok := doc["item"]; ok {
		return "postman"
	}
	return ""
}

func mapStr(m map[string]interface{}, keys ...string) string {
	var cur interface{} = m
	for _, k := range keys {
		mm, ok := cur.(map[string]interface{})
		if !ok {
			return ""
		}
		cur = mm[k]
	}
	return strVal(cur)
}

func strVal(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func baseName(p string) string {
	b := filepath.Base(p)
	return strings.TrimSuffix(b, filepath.Ext(b))
}

// ensureTagPath 将形如 "A/B/C" 的标签名拆成多级嵌套目录，
// 返回末级目录 ID。兼容 Apipost 等多级文件夹用 "/" 拼接在 tag 名里的导出方式。
func ensureTagPath(tagPath string, dirs *[]model.Directory, tagDir map[string]string) string {
	tagPath = strings.Trim(tagPath, "/")
	if tagPath == "" {
		return ""
	}
	parent := ""
	curKey := ""
	for _, seg := range strings.Split(tagPath, "/") {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		if curKey == "" {
			curKey = seg
		} else {
			curKey = curKey + "/" + seg
		}
		if id, ok := tagDir[curKey]; ok {
			parent = id
			continue
		}
		id := util.GenID()
		*dirs = append(*dirs, model.Directory{ID: id, Name: seg, ParentID: parent, Sort: len(*dirs)})
		tagDir[curKey] = id
		parent = id
	}
	return parent
}

// pruneEmptyDirs 删除没有任何接口（含后代接口）归属的空目录。
// 保留规则：一个目录被保留，当且仅当它本身直接挂载了接口，或它的某个后代目录被保留。
func pruneEmptyDirs(dirs []model.Directory, apis []model.ApiInfo) []model.Directory {
	// 收集所有“被接口直接使用”的目录 ID
	used := map[string]bool{}
	for _, a := range apis {
		if a.DirID != "" {
			used[a.DirID] = true
		}
	}
	// 由子到父传递“被使用”标记
	changed := true
	for changed {
		changed = false
		for _, d := range dirs {
			if used[d.ID] && d.ParentID != "" && !used[d.ParentID] {
				used[d.ParentID] = true
				changed = true
			}
		}
	}
	out := dirs[:0]
	for _, d := range dirs {
		if used[d.ID] {
			out = append(out, d)
		}
	}
	return out
}

func joinURL(base, p string) string {
	if p == "" {
		return base
	}
	if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
		return p
	}
	if base == "" {
		return p
	}
	base = strings.TrimRight(base, "/")
	p = strings.TrimLeft(p, "/")
	return base + "/" + p
}

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
		if s := contentSchema(rb, doc); s != nil {
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

// ---------------- Swagger 2 ----------------

func parseSwagger2(doc map[string]interface{}, title string) ([]model.Directory, []model.ApiInfo) {
	var dirs []model.Directory
	var apis []model.ApiInfo

	base := ""
	if host, ok := doc["host"].(string); ok && host != "" {
		scheme := "https"
		if sl, ok := doc["schemes"].([]interface{}); ok && len(sl) > 0 {
			if s, ok := sl[0].(string); ok {
				scheme = s
			}
		}
		base = scheme + "://" + host
	}
	if bp, ok := doc["basePath"].(string); ok {
		base += bp
	}

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
			apis = append(apis, buildSwaggerOp(api, op, doc))
		}
	}
	return dirs, apis
}

func buildSwaggerOp(api model.ApiInfo, op map[string]interface{}, doc map[string]interface{}) model.ApiInfo {
	if params, ok := op["parameters"].([]interface{}); ok {
		for _, pv := range params {
			p, ok := pv.(map[string]interface{})
			if !ok {
				continue
			}
			in := strVal(p["in"])
			switch in {
			case "body":
				if s, ok := p["schema"].(map[string]interface{}); ok {
					s = resolveRefSchema(s, doc)
					if f := schemaToFields("", s, doc, 0); f != nil {
						api.ReqFields = f.Children
					}
					api.Body = schemaToExample(s, doc)
					api.BodyType = "json"
				}
			case "formData":
				api.BodyType = "form"
				api.FormItems = append(api.FormItems, model.KV{Enabled: true, Key: strVal(p["name"]), Value: strVal(p["type"]), Description: strVal(p["description"])})
			case "query":
				api.Query = append(api.Query, paramToKV(p))
			case "header":
				api.Headers = append(api.Headers, paramToKV(p))
			case "path":
				api.Query = append(api.Query, paramToKV(p))
			}
		}
	}
	if resp, ok := op["responses"].(map[string]interface{}); ok {
		for _, code := range []string{"200", "201", "default"} {
			if rv, ok := resp[code]; ok {
				if rm, ok := rv.(map[string]interface{}); ok {
					if s, ok := rm["schema"].(map[string]interface{}); ok {
						s = resolveRefSchema(s, doc)
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

// ---------------- Postman Collection ----------------

func parsePostman(doc map[string]interface{}, title string) ([]model.Directory, []model.ApiInfo) {
	var dirs []model.Directory
	var apis []model.ApiInfo
	if items, ok := doc["item"].([]interface{}); ok {
		parsePostmanItems(items, &dirs, &apis, "", 0)
	}
	return dirs, apis
}

func parsePostmanItems(items []interface{}, dirs *[]model.Directory, apis *[]model.ApiInfo, parentID string, sort int) {
	for _, it := range items {
		im, ok := it.(map[string]interface{})
		if !ok {
			continue
		}
		name := strVal(im["name"])
		if sub, ok := im["item"].([]interface{}); ok {
			id := util.GenID()
			*dirs = append(*dirs, model.Directory{ID: id, Name: name, ParentID: parentID, Sort: sort})
			parsePostmanItems(sub, dirs, apis, id, 0)
		} else if req, ok := im["request"]; ok {
			*apis = append(*apis, postmanRequestToApi(name, req, parentID))
		}
	}
}

func postmanRequestToApi(name string, req interface{}, parentID string) model.ApiInfo {
	api := model.ApiInfo{ID: util.GenID(), Name: name, DirID: parentID, BodyType: "json"}
	rm, ok := req.(map[string]interface{})
	if !ok {
		return api
	}
	method := strVal(rm["method"])
	if method == "" {
		method = "GET"
	}
	api.Method = strings.ToUpper(method)
	if u, ok := rm["url"]; ok {
		api.URL = postmanURL(u)
	}
	if hl, ok := rm["header"].([]interface{}); ok {
		for _, h := range hl {
			hm, ok := h.(map[string]interface{})
			if !ok {
				continue
			}
			api.Headers = append(api.Headers, model.KV{
				Enabled: true, Key: strVal(hm["key"]), Value: strVal(hm["value"]), Description: strVal(hm["description"]),
			})
		}
	}
	if b, ok := rm["body"]; ok {
		postmanBody(b, &api)
	}
	return api
}

func postmanURL(u interface{}) string {
	switch t := u.(type) {
	case string:
		return t
	case map[string]interface{}:
		if raw, ok := t["raw"].(string); ok && raw != "" {
			return raw
		}
		var host, path string
		if hl, ok := t["host"].([]interface{}); ok {
			parts := []string{}
			for _, h := range hl {
				parts = append(parts, strVal(h))
			}
			host = strings.Join(parts, "")
		}
		if pl, ok := t["path"].([]interface{}); ok {
			parts := []string{}
			for _, p := range pl {
				parts = append(parts, strVal(p))
			}
			path = "/" + strings.Join(parts, "/")
		}
		url := host + path
		if ql, ok := t["query"].([]interface{}); ok && len(ql) > 0 {
			q := []string{}
			for _, qv := range ql {
				if qm, ok := qv.(map[string]interface{}); ok {
					q = append(q, strVal(qm["key"])+"="+strVal(qm["value"]))
				}
			}
			if len(q) > 0 {
				url += "?" + strings.Join(q, "&")
			}
		}
		return url
	}
	return ""
}

func postmanBody(b interface{}, api *model.ApiInfo) {
	bm, ok := b.(map[string]interface{})
	if !ok {
		return
	}
	switch strVal(bm["mode"]) {
	case "raw":
		raw := strVal(bm["raw"])
		api.Body = raw
		trimmed := strings.TrimSpace(raw)
		if trimmed != "" && (strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) {
			api.BodyType = "json"
		} else {
			api.BodyType = "text"
		}
	case "urlencoded", "formdata":
		api.BodyType = "form"
		key := "urlencoded"
		if strVal(bm["mode"]) == "formdata" {
			key = "formdata"
		}
		if fl, ok := bm[key].([]interface{}); ok {
			for _, f := range fl {
				if fm, ok := f.(map[string]interface{}); ok {
					api.FormItems = append(api.FormItems, model.KV{
						Enabled: true, Key: strVal(fm["key"]), Value: strVal(fm["value"]), Description: strVal(fm["description"]),
					})
				}
			}
		}
	case "file":
		api.BodyType = "text"
	}
}
