package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"time"

	"apitool/internal/appcore"
)

// rpcBlocked 网页端禁用的桌面专属方法（与 webui.go 的 webRPCBlocked 同源并扩充）。
// 调用这些方法返回可读错误而非晦涩报错。
var rpcBlocked = map[string]string{
	// 文件对话框
	"SaveFileDialog":      "网页端不支持保存对话框：导出内容将通过浏览器下载",
	"OpenFileDialog":      "网页端不支持文件对话框",
	"OpenDirectoryDialog": "网页端不支持目录对话框",
	// 窗口控制
	"WindowShow":           "该操作仅适用于桌面端窗口",
	"WindowHide":           "该操作仅适用于桌面端窗口",
	"WindowUnminimise":     "该操作仅适用于桌面端窗口",
	"WindowCenter":         "该操作仅适用于桌面端窗口",
	"WindowSetAlwaysOnTop": "该操作仅适用于桌面端窗口",
	"Quit":                 "该操作仅适用于桌面端",
	// 桌面剪贴板浮层
	"ShowClipboardWindow":   "网页端不支持剪贴板浮层",
	"CloseClipboardWindow":  "网页端不支持剪贴板浮层",
	"ToggleClipboardWindow": "网页端不支持剪贴板浮层",
	"StartClipboardCapture": "网页端不支持系统剪贴板采集",
	"StopClipboardCapture":  "网页端不支持系统剪贴板采集",
	"NotifyUpdated":         "该操作仅适用于桌面端",
	// MITM 抓包（依赖系统代理与 CA，桌面专属）
	"SniffStart":          "网络抓包仅支持桌面端（需设置系统代理与安装证书）",
	"SniffStop":           "网络抓包仅支持桌面端",
	"SniffSetSystemProxy": "网络抓包仅支持桌面端",
	"SniffInstallCA":      "根证书安装仅支持桌面端",
	"SniffImportCA":       "根证书导入仅支持桌面端",
	"SniffPickCAFile":     "网页端不支持文件选择",
	"SniffCAPath":         "该能力仅适用于桌面端",
	"SniffExportOpenAPI":  "网页端不支持保存对话框",
	// 数据文件路径（本地概念）
	"GetDataFilePath": "该操作仅适用于桌面端",
	// 托盘/本地服务转发
	"StartSyncServer":    "本地同步服务仅适用于桌面端",
	"StopSyncServer":     "本地同步服务仅适用于桌面端",
	"StartShareServer":   "本地分享服务仅适用于桌面端",
	"StopShareServer":    "本地分享服务仅适用于桌面端",
	"OpenShareDoc":       "该操作仅适用于桌面端",
	"StartWebChat":       "该操作仅适用于桌面端",
	"StopWebChat":        "该操作仅适用于桌面端",
	"OpenExportFile":     "网页端请直接下载文件",
	"OpenExportFolder":   "网页端请直接下载文件",
	"OpenInBrowser":      "该操作仅适用于桌面端",
	"OpenCADialog":       "该操作仅适用于桌面端",
	"CopyClipItem":       "网页端请使用浏览器剪贴板",
	"GetClipImageData":   "网页端暂不支持图片历史",
	// 桌面端新增/遗留的专属方法（P5 收口）
	"ServerLogin":          "该操作仅适用于桌面端（网页端已登录）",
	"ServerRegister":       "该操作仅适用于桌面端（网页端已登录）",
	"ServerLogout":         "该操作仅适用于桌面端",
	"ServerStatus":         "该操作仅适用于桌面端",
	"ServerRefreshProjects": "该操作仅适用于桌面端",
	"LocalStore":           "该操作仅适用于桌面端",
	"ClipMonitorEnabled":   "该操作仅适用于桌面端",
	"SaveClipText":         "系统剪贴板采集仅适用于桌面端",
	"SaveClipImage":        "系统剪贴板采集仅适用于桌面端",
	"SetClipboardText":     "网页端请使用浏览器剪贴板",
	"ClipboardSetText":     "网页端请使用浏览器剪贴板",
	"ExportStressReport":   "网页端不支持保存对话框，请使用导出下载",
	"DocExport":            "网页端不支持保存对话框，请使用导出下载",
}

// agentSemaphore 服务端级 Agent 并发护栏（防滥用）。
var agentSemaphore = make(chan struct{}, 3)

// handleRPC 处理 POST /rpc/{Method}：反射调用当前用户会话的 appcore 方法。
// 协议与桌面端 webui.go 一致：body 为 JSON 参数数组，返回 {"ok":true,"result":...}。
func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "仅支持 POST")
		return
	}
	name := trimPrefix(r.URL.Path, "/rpc/")
	if msg, blocked := rpcBlocked[name]; blocked {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": msg})
		return
	}
	// 插件开关（SSRF 风险面，公网部署默认关）
	if !s.cfg.EnablePlugins && len(name) > 6 && name[:6] == "Plugin" {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": "该服务端未启用插件功能（-enable-plugins）"})
		return
	}
	// Agent 开关
	if !s.cfg.EnableAgent && (name == "RunAgent" || len(name) > 7 && name[:7] == "PolishText") {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": "该服务端未启用 AI Agent（-enable-agent）"})
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "读取请求体失败")
		return
	}
	var args []json.RawMessage
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			// 容错：允许直接传单个对象（无参数数组）
			args = []json.RawMessage{json.RawMessage(raw)}
		}
	}
	uid := userIDOf(r.Context())
	uname := usernameOf(r.Context())

	// Agent 每日配额（RunAgent / PolishText 均计入）
	if name == "RunAgent" || name == "PolishText" {
		if qerr := s.agentQuota.take(uid); qerr != nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": qerr.Error()})
			return
		}
	}

	// 在会话互斥下反射调用该用户的 appcore 实例
	out, callErr := s.sessions.withLock(uid, uname, func(core *appcore.App) (interface{}, error) {
		if name == "RunAgent" {
			agentSemaphore <- struct{}{}
			defer func() { <-agentSemaphore }()
		}
		return callCoreMethod(core, name, args)
	})
	if callErr != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": callErr.Error()})
		return
	}
	// 多端同步：保存成功后通知同一用户的其它端（clientId 由前端携带，用于过滤自己）
	if name == "SaveData" {
		clientID := strings.TrimSpace(r.Header.Get("X-Client-Id"))
		s.hub.pushUserEvent(uid, "apitool:data-changed", []interface{}{
			map[string]interface{}{"clientId": clientID, "at": time.Now().UnixMilli()},
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "result": out})
}

// trimPrefix strings.TrimPrefix 别名（避免多个 import 行）。
func trimPrefix(s, prefix string) string {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):]
	}
	return s
}

// callCoreMethod 反射调用 appcore.App 的导出方法。
// 移植自 webui.go callWebMethod：签名处理 (T, error)/(T)/(error)/()，
// 首参 context.Context 的方法不对网页开放（依赖窗口上下文），panic 转错误。
func callCoreMethod(core interface{}, name string, args []json.RawMessage) (result interface{}, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			result, err = nil, fmt.Errorf("调用 %s 失败: %v", name, rec)
		}
	}()
	v := reflect.ValueOf(core)
	m := v.MethodByName(name)
	if !m.IsValid() {
		// appcore.App 嵌入的 agent.Manager/testing.Engine 方法在 reflect 上不可直达
		// （reflect 只看顶层方法集），嵌入类型的提升方法其实可见；到不了说明不存在。
		return nil, fmt.Errorf("方法不存在: %s", name)
	}
	mt := m.Type()
	if mt.NumIn() > 0 && mt.In(0) == reflect.TypeOf((*context.Context)(nil)).Elem() {
		return nil, fmt.Errorf("方法 %s 仅适用于桌面端", name)
	}
	// 变参方法（Emit）：网页端无需反向发事件
	if mt.IsVariadic() {
		return nil, nil
	}
	if mt.NumIn() != len(args) {
		return nil, fmt.Errorf("参数个数不匹配: %s 需要 %d 个，收到 %d 个", name, mt.NumIn(), len(args))
	}
	in := make([]reflect.Value, mt.NumIn())
	for i := 0; i < mt.NumIn(); i++ {
		pv := reflect.New(mt.In(i))
		if len(args[i]) > 0 {
			if err := json.Unmarshal(args[i], pv.Interface()); err != nil {
				return nil, fmt.Errorf("参数 %d 解析失败: %v", i+1, err)
			}
		}
		in[i] = pv.Elem()
	}
	out := m.Call(in)
	// 剥离尾部 error
	errType := reflect.TypeOf((*error)(nil)).Elem()
	n := len(out)
	for n > 0 && out[n-1].Type() == errType {
		if !out[n-1].IsNil() {
			return nil, out[n-1].Interface().(error)
		}
		n--
	}
	res := make([]interface{}, 0, n)
	for i := 0; i < n; i++ {
		res = append(res, out[i].Interface())
	}
	switch len(res) {
	case 0:
		return nil, nil
	case 1:
		return res[0], nil
	default:
		return res, nil
	}
}

