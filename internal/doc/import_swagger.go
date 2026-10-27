package doc

// Swagger 2.0 解析：把 definitions / parameters / produces 还原为目录与接口。

import (
	"strings"

	"apitool/internal/model"
	"apitool/internal/util"
)

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
				ftype := "text"
				if strVal(p["type"]) == "file" {
					ftype = "file"
				}
				api.FormItems = append(api.FormItems, model.KV{Enabled: true, Key: strVal(p["name"]), Value: "", Type: ftype, Description: strVal(p["description"])})
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
