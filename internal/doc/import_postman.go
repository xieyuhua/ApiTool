package doc

// Postman Collection 解析：文件夹层级还原为多级目录，request 还原为接口与请求体。

import (
	"strings"

	"apitool/internal/model"
	"apitool/internal/util"
)

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
