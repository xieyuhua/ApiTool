package doc

// 文档导入入口：选择文件 → 识别格式（OpenAPI 3 / Swagger 2 / Postman）→ 解析为目录与接口 → 落库。
// 导入内容统一包裹在「导入-<文件名>」根目录下，并自动剪掉没有任何接口的空目录。

import (
	"context"
	"fmt"
	"os"
	"time"

	"apitool/internal/model"
	"apitool/internal/store"
	"apitool/internal/util"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ImportDoc 选择并导入接口文档（OpenAPI 3 / Swagger 2 / Postman）
func ImportDoc(ctx context.Context, s *store.Store, projectID string) (string, error) {
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

	data := s.GetData()
	idx := store.ActiveProjectIndex(data)
	if idx < 0 {
		return "", fmt.Errorf("没有可用的项目")
	}
	data.Projects[idx].Dirs = append(data.Projects[idx].Dirs, dirs...)
	data.Projects[idx].Apis = append(data.Projects[idx].Apis, apis...)
	data.Projects[idx].UpdatedAt = time.Now().Format(time.RFC3339)
	if err := s.SaveData(data); err != nil {
		return "", err
	}
	return fmt.Sprintf("导入成功：%d 个目录、%d 个接口（已放入项目「%s」的「%s」）", len(dirs), len(apis), data.Projects[idx].Name, rootName), nil
}
