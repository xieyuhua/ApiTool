package doc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"apitool/internal/model"
	"apitool/internal/store"

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

// ---------------- 导出 ----------------

// docScopeData 一次导出所需的数据快照。
type docScopeData struct {
	Title     string
	RootID    string
	Project   string // 当前项目名（水印占位符 {{project}} 使用）
	Dirs      []model.Directory
	Apis      []model.ApiInfo
	Common    model.CommonParams
	Watermark model.DocWatermark  // 已补全默认值并展开占位符的水印配置
	Layout    model.DocLayout     // 企业文档排版（封面 / 目录 / 前言 / 规范 / 附录 / 修订记录）
	Envs      []model.Environment // 项目环境列表（生成「接入地址」章节用）
}

// loadDocScope 解析导出范围（标题、根目录、目录、接口、公共参数）与水印配置。
func loadDocScope(s *store.Store, dirID, apiID string) (docScopeData, error) {
	data := s.GetData()
	idx := store.ActiveProjectIndex(data)
	if idx < 0 {
		return docScopeData{}, fmt.Errorf("没有可用的项目")
	}
	project := data.Projects[idx]
	title, dirs, apis := CollectScope(data, dirID, apiID)
	if len(apis) == 0 {
		return docScopeData{}, fmt.Errorf("所选范围内没有接口")
	}
	scope := docScopeData{
		Title:   title,
		Project: project.Name,
		Dirs:    dirs,
		Apis:    apis,
		Common:  project.Common,
		Watermark: ExpandWatermark(
			NormalizeWatermark(data.Settings.Watermark), project.Name, time.Now()),
		Layout: EnrichDocLayout(EffectiveDocLayout(data, project), project.Environments, project.Common),
	}
	if apiID == "" {
		scope.RootID = dirID
	}
	return scope, nil
}

// docFormat 导出格式的落盘参数（扩展名 / 保存对话框过滤器 / 对话框标题）。
type docFormat struct {
	ext    string
	filter string
	dialog string
}

// docFormats 支持的导出格式。word / pdf 会按「文档中心 → 导出水印」配置加水印。
var docFormats = map[string]docFormat{
	"markdown": {".md", "Markdown (*.md)|*.md", "导出接口文档（Markdown）"},
	"html":     {".html", "HTML (*.html)|*.html", "导出接口文档（HTML）"},
	"word":     {".docx", "Word 文档 (*.docx)|*.docx", "导出接口文档（Word）"},
	"pdf":      {".pdf", "PDF 文件 (*.pdf)|*.pdf", "导出接口文档（PDF）"},
	"openapi":  {".json", "OpenAPI JSON (*.json)|*.json", "导出接口文档（OpenAPI）"},
}

// docPayload 渲染产物：data 直接写盘；save 非空时由自定义函数落盘（PDF 交给浏览器打印）。
type docPayload struct {
	title string
	data  []byte
	save  func(path string) error
}

func (p docPayload) writeTo(path string) error {
	if p.save != nil {
		return p.save(path)
	}
	return os.WriteFile(path, p.data, 0o644)
}

// ExportDoc 导出文档，返回保存路径。
// 支持格式见 docFormats：markdown / html / word（.docx，Word 原生排版）/
// pdf（浏览器无头打印）/ openapi。
func ExportDoc(ctx context.Context, s *store.Store, dirID string, apiID string, format string) (string, error) {
	f, ok := docFormats[format]
	if !ok {
		return "", fmt.Errorf("不支持的格式: %s（可选 markdown / html / word / pdf / openapi）", format)
	}
	payload, err := renderDoc(s, dirID, apiID, format)
	if err != nil {
		return "", err
	}
	path, err := runtime.SaveFileDialog(ctx, runtime.SaveDialogOptions{
		Title:           f.dialog,
		DefaultFilename: ExportFileName(payload.title, f.ext),
		Filters:         []runtime.FileFilter{{DisplayName: f.filter, Pattern: "*" + f.ext}},
	})
	if err != nil || path == "" {
		return "", err
	}
	if err := payload.writeTo(path); err != nil {
		return "", err
	}
	return path, nil
}

// renderDoc 按格式渲染导出产物（word / pdf 会写入水印）。
func renderDoc(s *store.Store, dirID, apiID, format string) (docPayload, error) {
	scope, err := loadDocScope(s, dirID, apiID)
	if err != nil {
		return docPayload{}, err
	}
	switch format {
	case "markdown":
		return docPayload{title: scope.Title, data: []byte(buildMarkdown(scope.Title, scope.RootID, scope.Dirs, scope.Apis, scope.Common, scope.Layout))}, nil
	case "html":
		return docPayload{title: scope.Title, data: []byte(BuildHTMLWithLayout(scope.Title, scope.RootID, scope.Dirs, scope.Apis, scope.Common, scope.Layout))}, nil
	case "word":
		b, err := BuildDOCXWithLayout(scope.Title, scope.RootID, scope.Dirs, scope.Apis, scope.Common, scope.Watermark, scope.Layout)
		if err != nil {
			return docPayload{}, err
		}
		return docPayload{title: scope.Title, data: b}, nil
	case "pdf":
		html := BuildHTMLForExportWithLayout(scope.Title, scope.RootID, scope.Dirs, scope.Apis, scope.Common, scope.Watermark, scope.Layout)
		return docPayload{title: scope.Title, save: func(path string) error {
			return WriteHTMLToPDF(html, path)
		}}, nil
	case "openapi":
		content, err := BuildOpenAPI(scope.Title, scope.Dirs, scope.Apis, scope.RootID, scope.Common, "")
		if err != nil {
			return docPayload{}, err
		}
		return docPayload{title: scope.Title, data: []byte(content)}, nil
	}
	return docPayload{}, fmt.Errorf("不支持的格式: %s", format)
}

// PreviewWatermark 按当前水印配置渲染 HTML 文档并在浏览器打开，返回临时文件路径。
// 与 PDF 导出共用同一套 HTML 水印层，用于导出前核对水印效果。
func PreviewWatermark(ctx context.Context, s *store.Store, dirID string, apiID string) (string, error) {
	scope, err := loadDocScope(s, dirID, apiID)
	if err != nil {
		return "", err
	}
	if !WatermarkActive(scope.Watermark) {
		return "", fmt.Errorf("水印未启用：请先在「导出水印」中开启并填写水印文字")
	}
	html := BuildHTMLForExportWithLayout(scope.Title, scope.RootID, scope.Dirs, scope.Apis, scope.Common, scope.Watermark, scope.Layout)
	path := filepath.Join(os.TempDir(), "apitool-watermark-preview-"+SanitizeFilename(scope.Title)+".html")
	if err := os.WriteFile(path, []byte(html), 0o644); err != nil {
		return "", err
	}
	runtime.BrowserOpenURL(ctx, "file:///"+filepath.ToSlash(path))
	return path, nil
}

// PreviewHTML 生成完整文档 HTML（含所选范围内的全部接口），供前端弹窗预览。
// layoutJSON 非空时使用该结构（前端当前编辑中的模板，含未落库的改动），否则用当前生效结构。
func PreviewHTML(s *store.Store, dirID string, apiID string, layoutJSON string) (string, error) {
	scope, err := loadDocScope(s, dirID, apiID)
	if err != nil {
		return "", err
	}
	layout := scope.Layout
	if strings.TrimSpace(layoutJSON) != "" {
		data := s.GetData()
		proj := model.Project{}
		if idx := store.ActiveProjectIndex(data); idx >= 0 {
			proj = data.Projects[idx]
		}
		var l model.DocLayout
		if err := json.Unmarshal([]byte(layoutJSON), &l); err != nil {
			return "", fmt.Errorf("文档结构解析失败: %v", err)
		}
		// 前端传入的是原始结构（未注入环境/公共头/验签等自动章节），这里按项目数据补齐
		layout = EnrichDocLayout(l, proj.Environments, proj.Common)
	}
	return withPreviewScript(BuildHTMLForExportWithLayout(scope.Title, scope.RootID, scope.Dirs, scope.Apis,
		scope.Common, scope.Watermark, layout)), nil
}

// PreviewTemplateHTML 生成「模板结构预览」HTML（不含具体接口），供前端弹窗内嵌展示。
// layoutJSON 为空时使用当前生效结构（全局配置或项目级覆盖）。
func PreviewTemplateHTML(s *store.Store, layoutJSON string) (string, error) {
	html, _, err := templatePreviewHTML(s, layoutJSON)
	return html, err
}

// templatePreviewHTML 解析生效结构 + 水印，渲染模板预览 HTML（返回 HTML 与模板名）。
func templatePreviewHTML(s *store.Store, layoutJSON string) (string, string, error) {
	data := s.GetData()
	proj := model.Project{}
	if idx := store.ActiveProjectIndex(data); idx >= 0 {
		proj = data.Projects[idx]
	}
	layout := EffectiveDocLayout(data, proj)
	if strings.TrimSpace(layoutJSON) != "" {
		// 传入了结构就以它为准（从零值解析，避免与库里的旧结构混合）
		layout = model.DocLayout{}
		if err := json.Unmarshal([]byte(layoutJSON), &layout); err != nil {
			return "", "", fmt.Errorf("文档结构解析失败: %v", err)
		}
	}
	layout = EnrichDocLayout(layout, proj.Environments, proj.Common)
	wm := ExpandWatermark(NormalizeWatermark(data.Settings.Watermark), proj.Name, time.Now())
	title := strings.TrimSpace(layout.Name)
	if title == "" {
		title = "文档模板预览"
	} else {
		title = "文档模板预览 · " + title
	}
	name := layout.Name
	if strings.TrimSpace(name) == "" {
		name = "current"
	}
	return withPreviewScript(BuildTemplatePreviewHTML(title, proj.Common, wm, layout)), name, nil
}

// ShareDoc 生成 HTML 文档并用浏览器打开（可将文件发送给他人分享）
func ShareDoc(ctx context.Context, s *store.Store, dirID string, apiID string) (string, error) {
	payload, err := renderDoc(s, dirID, apiID, "html")
	if err != nil {
		return "", err
	}
	path := filepath.Join(os.TempDir(), "apitool-share-"+SanitizeFilename(payload.title)+".html")
	if err := os.WriteFile(path, payload.data, 0o644); err != nil {
		return "", err
	}
	runtime.BrowserOpenURL(ctx, "file:///"+filepath.ToSlash(path))
	return path, nil
}

// CopyDocMarkdown 复制 Markdown 文档到剪贴板（用于快速分享）
func CopyDocMarkdown(ctx context.Context, s *store.Store, dirID string, apiID string) error {
	payload, err := renderDoc(s, dirID, apiID, "markdown")
	if err != nil {
		return err
	}
	return runtime.ClipboardSetText(ctx, string(payload.data))
}

// SanitizeFilename 清理文件名中的非法字符，空结果回退为 api-doc。
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
