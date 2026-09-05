package doc

import (
	"apitool/internal/model"
	"apitool/internal/store"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// collectScope 计算导出/分享范围内的目录与接口（限定当前项目）。
// 返回 (标题, 目录列表, 接口列表)；单接口时标题取接口名，目录范围时取目录名，否则为"接口文档"。
func CollectScope(data model.AppData, dirID string, apiID string) (title string, dirs []model.Directory, apis []model.ApiInfo) {
	idx := store.ActiveProjectIndex(data)
	if idx < 0 {
		return "接口文档", nil, nil
	}
	proj := data.Projects[idx]
	if apiID != "" {
		for _, api := range proj.Apis {
			if api.ID == apiID {
				return api.Name, nil, []model.ApiInfo{api}
			}
		}
		return "接口文档", nil, nil
	}
	if dirID == "" {
		// 全项目
		return "接口文档", proj.Dirs, proj.Apis
	}
	// 目录范围：该目录及其所有子目录
	desc := map[string]bool{dirID: true}
	for {
		grew := false
		for _, d := range proj.Dirs {
			if desc[d.ParentID] && !desc[d.ID] {
				desc[d.ID] = true
				grew = true
			}
		}
		if !grew {
			break
		}
	}
	for _, d := range proj.Dirs {
		if desc[d.ID] {
			dirs = append(dirs, d)
		}
	}
	for _, api := range proj.Apis {
		if desc[api.DirID] {
			apis = append(apis, api)
		}
	}
	if len(dirs) > 0 {
		for _, d := range dirs {
			if d.ID == dirID {
				title = d.Name
				break
			}
		}
	}
	return title, dirs, apis
}

// buildDocContent 按指定格式（markdown/html/word/openapi）生成文档内容。
func buildDocContent(ctx context.Context, s *store.Store, version, updateURL, dirID, apiID, format string) (content string, title string, err error) {
	data := s.GetData()
	idx := store.ActiveProjectIndex(data)
	if idx < 0 {
		return "", "", fmt.Errorf("没有可用的项目")
	}
	proj := data.Projects[idx]
	title, dirs, apis := CollectScope(data, dirID, apiID)
	if len(apis) == 0 {
		return "", "", fmt.Errorf("所选范围内没有接口")
	}
	rootID := ""
	if apiID == "" {
		rootID = dirID
	}
	switch format {
	case "markdown":
		content = buildMarkdown(title, rootID, dirs, apis, proj.Common)
	case "html", "word":
		content = BuildHTML(title, rootID, dirs, apis, proj.Common)
	case "openapi":
		content, err = BuildOpenAPI(title, dirs, apis, rootID, proj.Common, "")
	default:
		return "", "", fmt.Errorf("不支持的格式: %s", format)
	}
	return content, title, err
}

// ExportDoc 导出文档，返回保存路径
func ExportDoc(ctx context.Context, s *store.Store, version, updateURL, dirID string, apiID string, format string) (string, error) {
	content, title, err := buildDocContent(ctx, s, version, updateURL, dirID, apiID, format)
	if err != nil {
		return "", err
	}
	ext, filter := ".md", "Markdown (*.md)|*.md"
	switch format {
	case "html":
		ext, filter = ".html", "HTML (*.html)|*.html"
	case "word":
		ext, filter = ".doc", "Word (*.doc)|*.doc"
	case "openapi":
		ext, filter = ".json", "OpenAPI JSON (*.json)|*.json"
	}
	path, err := runtime.SaveFileDialog(ctx, runtime.SaveDialogOptions{
		Title:           "导出接口文档",
		DefaultFilename: SanitizeFilename(title) + ext,
		Filters: []runtime.FileFilter{
			{DisplayName: filter, Pattern: "*" + ext},
		},
	})
	if err != nil || path == "" {
		return "", err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// ShareDoc 生成 HTML 文档并用浏览器打开（可将文件发送给他人分享）
func ShareDoc(ctx context.Context, s *store.Store, version, updateURL, dirID string, apiID string) (string, error) {
	content, title, err := buildDocContent(ctx, s, version, updateURL, dirID, apiID, "html")
	if err != nil {
		return "", err
	}
	path := filepath.Join(os.TempDir(), "apitool-share-"+SanitizeFilename(title)+".html")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	runtime.BrowserOpenURL(ctx, "file:///"+filepath.ToSlash(path))
	return path, nil
}

// CopyDocMarkdown 复制 Markdown 文档到剪贴板（用于快速分享）
func CopyDocMarkdown(ctx context.Context, s *store.Store, version, updateURL, dirID string, apiID string) error {
	content, _, err := buildDocContent(ctx, s, version, updateURL, dirID, apiID, "markdown")
	if err != nil {
		return err
	}
	return runtime.ClipboardSetText(ctx, content)
}

func SanitizeFilename(s string) string {
	out := []rune{}
	for _, r := range s {
		switch r {
		case '\\', '/', ':', '*', '?', '"', '<', '>', '|':
			out = append(out, '_')
		default:
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return "api-doc"
	}
	return string(out)
}
