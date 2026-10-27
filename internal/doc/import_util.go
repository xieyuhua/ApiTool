package doc

// 导入公共工具：文件解码（JSON / YAML）、格式识别、目录树辅助、URL 拼接与空目录剪枝。

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"apitool/internal/model"
	"apitool/internal/util"

	"gopkg.in/yaml.v3"
)

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
