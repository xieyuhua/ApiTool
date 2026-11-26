package appcore

import (
	"fmt"
	"time"

	"apitool/internal/bus"
	"apitool/internal/capture"
	"apitool/internal/doc"
	"apitool/internal/jsonutil"
	"apitool/internal/model"
	"apitool/internal/store"
	"apitool/internal/stress"
	"apitool/internal/testing"
	"apitool/internal/wailsrt"
)

// DialogOptions / FileFilter 直接复用 wailsrt 的类型（bus.Bus 契约使用它们），
// 避免业务核心再定义一套转换。
type (
	// DialogOptions 文件对话框选项（对齐 bus.Bus 契约）。
	DialogOptions = wailsrt.DialogOptions
	// FileFilter 文件类型过滤器。
	FileFilter = wailsrt.FileFilter
)

// toBusDialog 显式转换（类型别名下恒等，保留以说明边界）。
func toBusDialog(o DialogOptions) wailsrt.DialogOptions { return o }

// sendRequest 转发到 httpx（独立小函数便于 mock）。
func sendRequest(spec model.RequestSpec) model.ResponseData {
	return httpxSend(spec)
}

// ---- 接口调试工具 ----

// ParseFields 将 JSON 文本解析为字段树。
func (a *App) ParseFields(jsonStr string, existing []*model.Field) ([]*model.Field, error) {
	return jsonutil.ParseFields(jsonStr, existing)
}

// FormatJSON 格式化 JSON 文本。
func (a *App) FormatJSON(jsonStr string) (string, error) {
	return jsonutil.FormatJSON(jsonStr)
}

// ---- 测试数据 ----

// ClearTestData 一键清空当前项目测试数据。
// scope: cases | plans | reports | all；返回被清空条数。
func (a *App) ClearTestData(projectID string, scope string) (int, error) {
	a.lock()
	defer a.unlock()
	data := a.data.GetData()
	idx := store.ActiveProjectIndex(data)
	if idx < 0 {
		return 0, fmt.Errorf("没有可用的项目")
	}
	if projectID != "" {
		found := false
		for i, p := range data.Projects {
			if p.ID == projectID {
				idx = i
				found = true
				break
			}
		}
		if !found {
			return 0, fmt.Errorf("项目不存在：%s", projectID)
		}
	}
	if scope == "" {
		scope = "all"
	}
	removed := 0
	proj := &data.Projects[idx]
	switch scope {
	case "cases":
		removed = len(proj.TestCases)
		proj.TestCases = []model.TestCase{}
	case "plans":
		removed = len(proj.TestPlans)
		proj.TestPlans = []model.TestPlan{}
	case "reports":
		removed = len(proj.TestReports)
		proj.TestReports = []model.TestReport{}
	case "all":
		removed = len(proj.TestCases) + len(proj.TestPlans) + len(proj.TestReports)
		proj.TestCases = []model.TestCase{}
		proj.TestPlans = []model.TestPlan{}
		proj.TestReports = []model.TestReport{}
	default:
		return 0, fmt.Errorf("未知的清空范围：%s", scope)
	}
	if err := a.data.SaveData(data); err != nil {
		return 0, err
	}
	return removed, nil
}

// DefaultTestCasePrompt 返回 AI 生成用例默认提示词。
func (a *App) DefaultTestCasePrompt() string { return testing.DefaultTestCaseUserPrompt() }

// ---- 文档（渲染/预览可下沉；对话框导出由桌面壳覆盖，服务端走 RenderExport+下载）----

// RenderExportContent 渲染导出产物但不落盘，返回（文件名，内容）。
// 服务端 /export 接口的基石；桌面端仍走对话框版 ExportDoc。
func (a *App) RenderExportContent(dirID, apiID, format string) (string, []byte, error) {
	a.lock()
	defer a.unlock()
	return doc.RenderExport(a.data, dirID, apiID, format)
}

// PreviewDocHTML 生成整份文档预览 HTML（含接口，按当前结构与水印）。
func (a *App) PreviewDocHTML(dirID, apiID, layoutJSON string) (string, error) {
	a.lock()
	defer a.unlock()
	return doc.PreviewHTML(a.data, dirID, apiID, layoutJSON)
}

// PreviewDocTemplateHTML 生成模板结构预览 HTML（不含具体接口）。
func (a *App) PreviewDocTemplateHTML(layoutJSON string) (string, error) {
	a.lock()
	defer a.unlock()
	return doc.PreviewTemplateHTML(a.data, layoutJSON)
}

// DocMarkdown 返回 Markdown 文档内容（网页端没有系统剪贴板，由浏览器复制）。
func (a *App) DocMarkdown(dirID, apiID string) (string, error) {
	name, data, err := a.RenderExportContent(dirID, apiID, "markdown")
	if err != nil {
		return "", err
	}
	_ = name
	return string(data), nil
}

// ShareDocHTML 返回分享用 HTML 网页源码（供网页端"网页代码"页与分享链接）。
// 结构跟随「文档模板」当前配置，水印按「分享文档加导出水印」开关。
func (a *App) ShareDocHTML(dirID, apiID string) (string, error) {
	_, html, err := a.shareDocContent(dirID, apiID)
	return html, err
}

// ShareDocTitle 返回分享文档标题。
func (a *App) ShareDocTitle(dirID, apiID string) (string, error) {
	title, _, err := a.shareDocContent(dirID, apiID)
	return title, err
}

// ShareDocContent 返回分享标题与 HTML（服务端创建分享链接时落库用）。
func (a *App) ShareDocContent(dirID, apiID string) (string, string, error) {
	return a.shareDocContent(dirID, apiID)
}

// CurrentProjectID 返回当前活动项目 ID（服务端用于校验分享权限）。
func (a *App) CurrentProjectID() string {
	a.lock()
	defer a.unlock()
	data := a.data.GetData()
	if idx := store.ActiveProjectIndex(data); idx >= 0 {
		return data.Projects[idx].ID
	}
	return ""
}

// shareDocContent 计算范围内的分享标题与 HTML（与 internal/share 的语义一致）。
func (a *App) shareDocContent(dirID, apiID string) (string, string, error) {
	a.lock()
	defer a.unlock()
	data := a.data.GetData()
	title, dirs, apis := doc.CollectScope(data, dirID, apiID)
	if len(apis) == 0 {
		return "", "", fmt.Errorf("所选范围内没有接口")
	}
	proj := model.Project{}
	if idx := store.ActiveProjectIndex(data); idx >= 0 {
		proj = data.Projects[idx]
	}
	layout := doc.EnrichDocLayout(doc.EffectiveDocLayout(data, proj), proj.Environments, proj.Common)
	var wm model.DocWatermark
	if data.Settings.ShareDocWatermark {
		wm = doc.ExpandWatermark(doc.NormalizeWatermark(data.Settings.Watermark), proj.Name, time.Now())
	}
	return title, doc.BuildHTMLForExportWithLayout(title, dirID, dirs, apis, proj.Common, wm, layout), nil
}

// ImportDocContent 从上传的文档内容导入接口（OpenAPI3/Swagger2/Postman）。
// 网页端无法弹文件对话框，由浏览器读取文件后把内容传上来。
func (a *App) ImportDocContent(name, content string) (string, error) {
	a.lock()
	defer a.unlock()
	return doc.ImportDocContent(a.data, name, content)
}

// PreviewWatermarkHTML 返回带水印的文档预览 HTML（网页端在新标签页打开，
// 桌面端仍走 PreviewDocWatermark 的临时文件 + 系统浏览器）。
func (a *App) PreviewWatermarkHTML(dirID, apiID string) (string, error) {
	a.lock()
	defer a.unlock()
	return doc.PreviewWatermarkHTML(a.data, dirID, apiID)
}

// DocPresets 返回预置文档模板列表。
func (a *App) DocPresets() []doc.DocPreset { return doc.DocPresetList() }

// DocPresetLayout 返回预置模板结构。
func (a *App) DocPresetLayout(key string) model.DocLayout { return doc.DocPresetLayout(key) }

// DefaultDocQuickStart 返回接入指引默认内容。
func (a *App) DefaultDocQuickStart() string { return doc.QuickStartDefaultText() }

// DefaultDocConvention 返回统一定义规范默认内容。
func (a *App) DefaultDocConvention() string { return doc.DefaultConventionTemplate() }

// DefaultDocGlossary 返回默认术语表。
func (a *App) DefaultDocGlossary() []model.DocTerm { return doc.DefaultGlossary() }

// DefaultDocEnums 返回默认数据字典。
func (a *App) DefaultDocEnums() []model.DocEnum { return doc.DefaultEnums() }

// DefaultDocBusinessCodes 返回默认业务错误码。
func (a *App) DefaultDocBusinessCodes() []model.DocCode { return doc.DefaultBusinessCodes() }

// DefaultDocSign 返回加密与验签默认内容。
func (a *App) DefaultDocSign() string { return doc.DefaultSignTemplate() }

// RenderTestReportContent 渲染测试报告（网页/App 端下载用）。
func (a *App) RenderTestReportContent(reportJSON, format string) (string, []byte, error) {
	return a.Engine.RenderTestReport(reportJSON, format)
}

// RenderStressReportContent 渲染压测报告（网页/App 端下载用）。
func (a *App) RenderStressReportContent(reportJSON, format string) (string, []byte, error) {
	return stress.RenderReport(reportJSON, format)
}

// ExportCapturedOpenAPI 将选中的捕获记录生成 OpenAPI 文档并返回（供下载）。
func (a *App) RenderCapturedOpenAPI(ids []string, title string) (string, error) {
	return capture.BuildOpenAPI(ids, title)
}

// ---- 压测 ----

// RunStressTest 对给定目标发起并发压测。
func (a *App) RunStressTest(targets []stress.StressTarget, config stress.StressConfig) (stress.StressReport, error) {
	if a.bus == nil {
		return stress.StressReport{}, fmt.Errorf("当前环境不支持压测（缺少事件通道）")
	}
	a.lock()
	defer a.unlock()
	return stress.Run(targets, config, a.data, a.bus)
}

// _ = bus 保持导入（toBusDialog 在部分构建路径下可能未直接引用）
var _ = bus.Bus(nil)
