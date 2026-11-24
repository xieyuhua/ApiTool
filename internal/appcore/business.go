package appcore

import (
	"fmt"

	"apitool/internal/bus"
	"apitool/internal/doc"
	"apitool/internal/jsonutil"
	"apitool/internal/model"
	"apitool/internal/stress"
	"apitool/internal/testing"
	"apitool/internal/store"
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
