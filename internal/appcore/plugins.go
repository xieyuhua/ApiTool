package appcore

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"apitool/internal/ai"
	"apitool/internal/cloud"
	"apitool/internal/doc"
	"apitool/internal/model"
	"apitool/internal/plugins"
	"apitool/internal/util"
)

// maxDocTemplates 自定义文档模板数量上限。
const maxDocTemplates = 20

// CallAIArgs CallAI 入参（由前端传入已解析好的配置）。
type CallAIArgs struct {
	BaseURL   string        `json:"baseUrl"`
	APIKey    string        `json:"apiKey"`
	Model     string        `json:"model"`
	Timeout   int           `json:"timeoutSec"`
	MaxTokens int           `json:"maxTokens"`
	Messages  []ai.ChatMessage `json:"messages"`
}

// ChatMessage 简单的聊天消息结构（别名指向 internal/ai）。
type ChatMessage = ai.ChatMessage

// CallAI 调用 OpenAI 兼容接口（转发到 internal/ai）。
func (a *App) CallAI(args CallAIArgs) (string, error) {
	base := strings.TrimSpace(args.BaseURL)
	if base == "" {
		return "", fmt.Errorf("未配置 AI 接口地址（设置 → AI 配置）")
	}
	if args.APIKey == "" {
		return "", fmt.Errorf("未配置 AI API Key（设置 → AI 配置）")
	}
	mdl := strings.TrimSpace(args.Model)
	return ai.ChatRaw(base, args.APIKey, mdl, args.Messages, args.Timeout, args.MaxTokens)
}

// GenerateDescriptions 使用 AI 为字段自动生成描述。
func (a *App) GenerateDescriptions(apiName string, apiDesc string, fields []*model.Field) ([]*model.Field, error) {
	return ai.GenerateDescriptions(a, apiName, apiDesc, fields)
}

// ---- 文档模板 CRUD（读写 settings.docLayoutTemplates）----

// SaveDocLayoutTemplate 把当前文档结构保存为自定义模板（同名覆盖）。
func (a *App) SaveDocLayoutTemplate(name string, layoutJSON string) (model.DocLayoutTemplate, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return model.DocLayoutTemplate{}, fmt.Errorf("模板名称不能为空")
	}
	var layout model.DocLayout
	if err := json.Unmarshal([]byte(layoutJSON), &layout); err != nil {
		return model.DocLayoutTemplate{}, fmt.Errorf("文档结构解析失败: %v", err)
	}
	a.lock()
	defer a.unlock()
	data := a.data.GetData()
	tpl := model.DocLayoutTemplate{ID: util.GenID(), Name: name, Layout: layout}
	replaced := false
	for i := range data.Settings.DocLayoutTemplates {
		if data.Settings.DocLayoutTemplates[i].Name == name {
			tpl.ID = data.Settings.DocLayoutTemplates[i].ID
			data.Settings.DocLayoutTemplates[i] = tpl
			replaced = true
			break
		}
	}
	if !replaced {
		data.Settings.DocLayoutTemplates = append(data.Settings.DocLayoutTemplates, tpl)
		if len(data.Settings.DocLayoutTemplates) > maxDocTemplates {
			return model.DocLayoutTemplate{}, fmt.Errorf("自定义模板最多 %d 个，请先删除不用的模板", maxDocTemplates)
		}
	}
	if err := a.data.SaveData(data); err != nil {
		return model.DocLayoutTemplate{}, err
	}
	return tpl, nil
}

// cloudClient 按当前设置构造云端同步客户端。
func (a *App) cloudClient() *cloud.Client {
	s := a.readData().Settings
	return cloud.New(s.CloudURL, s.CloudToken)
}

// PullDocTemplates 从云端拉取模板库并合并（同名以云端为准）。
func (a *App) PullDocTemplates() (cloud.SyncResult, error) {
	a.lock()
	defer a.unlock()
	data := a.data.GetData()
	remote, err := a.cloudClient().PullTemplates()
	if err != nil {
		return cloud.SyncResult{}, err
	}
	merged, changed := cloud.MergeTemplates(data.Settings.DocLayoutTemplates, remote)
	if len(merged) > maxDocTemplates {
		merged = merged[:maxDocTemplates]
	}
	data.Settings.DocLayoutTemplates = merged
	if err := a.data.SaveData(data); err != nil {
		return cloud.SyncResult{}, err
	}
	return cloud.SyncResult{Total: len(merged), Changed: changed}, nil
}

// PushDocTemplates 把本地模板库上传到云端。
func (a *App) PushDocTemplates() (cloud.SyncResult, error) {
	a.lock()
	defer a.unlock()
	data := a.data.GetData()
	tpls := data.Settings.DocLayoutTemplates
	if len(tpls) == 0 {
		return cloud.SyncResult{}, fmt.Errorf("本地没有可上传的模板")
	}
	if err := a.cloudClient().PushTemplates(tpls); err != nil {
		return cloud.SyncResult{}, err
	}
	return cloud.SyncResult{Total: len(tpls), Changed: len(tpls)}, nil
}

// RenameDocLayoutTemplate 重命名自定义模板（重名报错）。
func (a *App) RenameDocLayoutTemplate(id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("模板名称不能为空")
	}
	a.lock()
	defer a.unlock()
	data := a.data.GetData()
	found := false
	for i := range data.Settings.DocLayoutTemplates {
		t := data.Settings.DocLayoutTemplates[i]
		if t.ID == id {
			found = true
			continue
		}
		if t.Name == name {
			return fmt.Errorf("已存在同名模板：%s", name)
		}
	}
	if !found {
		return fmt.Errorf("模板不存在")
	}
	for i := range data.Settings.DocLayoutTemplates {
		if data.Settings.DocLayoutTemplates[i].ID == id {
			data.Settings.DocLayoutTemplates[i].Name = name
		}
	}
	return a.data.SaveData(data)
}

// DuplicateDocLayoutTemplate 复制自定义模板，返回新模板（名称为「原名 副本」）。
func (a *App) DuplicateDocLayoutTemplate(id string) (model.DocLayoutTemplate, error) {
	a.lock()
	defer a.unlock()
	data := a.data.GetData()
	var src *model.DocLayoutTemplate
	for i := range data.Settings.DocLayoutTemplates {
		if data.Settings.DocLayoutTemplates[i].ID == id {
			src = &data.Settings.DocLayoutTemplates[i]
			break
		}
	}
	if src == nil {
		return model.DocLayoutTemplate{}, fmt.Errorf("模板不存在")
	}
	if len(data.Settings.DocLayoutTemplates) >= maxDocTemplates {
		return model.DocLayoutTemplate{}, fmt.Errorf("自定义模板最多 %d 个，请先删除不用的模板", maxDocTemplates)
	}
	name := src.Name + " 副本"
	for i := 2; ; i++ {
		exist := false
		for _, t := range data.Settings.DocLayoutTemplates {
			if t.Name == name {
				exist = true
				break
			}
		}
		if !exist {
			break
		}
		name = fmt.Sprintf("%s 副本 %d", src.Name, i)
	}
	tpl := model.DocLayoutTemplate{ID: util.GenID(), Name: name, Layout: src.Layout}
	tpl.Layout.Name = name
	data.Settings.DocLayoutTemplates = append(data.Settings.DocLayoutTemplates, tpl)
	if err := a.data.SaveData(data); err != nil {
		return model.DocLayoutTemplate{}, err
	}
	return tpl, nil
}

// DeleteDocLayoutTemplate 删除自定义文档结构模板。
func (a *App) DeleteDocLayoutTemplate(id string) error {
	a.lock()
	defer a.unlock()
	data := a.data.GetData()
	kept := make([]model.DocLayoutTemplate, 0, len(data.Settings.DocLayoutTemplates))
	for _, t := range data.Settings.DocLayoutTemplates {
		if t.ID != id {
			kept = append(kept, t)
		}
	}
	data.Settings.DocLayoutTemplates = kept
	return a.data.SaveData(data)
}

// DocSignTemplates 返回预置验签模板清单。
func (a *App) DocSignTemplates() []doc.DocSignTemplate { return doc.SignTemplateList() }

// ---- 升级检测 ----

// UpdateInfo 升级服务返回的版本信息。
type UpdateInfo struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	Notes   string `json:"notes"`
}

// CheckUpdateResult 升级检测结果。
type CheckUpdateResult struct {
	Current string `json:"current"`
	Latest  string `json:"latest"`
	HasNew  bool   `json:"hasNew"`
	URL     string `json:"url"`
	Notes   string `json:"notes"`
	Error   string `json:"error"`
}

// GetVersion 返回客户端版本号。
func (a *App) GetVersion() string { return a.info.Version }

// CheckUpdate 调用升级服务 /version 接口检测新版本。
func (a *App) CheckUpdate() CheckUpdateResult {
	data := a.readData()
	res := CheckUpdateResult{Current: data.Settings.Version}
	if data.Settings.Version == "" {
		res.Current = a.info.Version
	}
	res.Latest = res.Current

	base := strings.TrimSpace(data.Settings.UpdateURL)
	if base == "" {
		base = "http://127.0.0.1:8080"
	}
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	base = strings.TrimRight(base, "/")

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(base + "/version")
	if err != nil {
		res.Error = "无法连接升级服务：" + err.Error()
		return res
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		res.Error = fmt.Sprintf("升级服务返回状态 %d", resp.StatusCode)
		return res
	}
	var info UpdateInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		res.Error = "升级服务返回数据解析失败：" + err.Error()
		return res
	}
	if info.Version == "" {
		res.Error = "升级服务未返回版本号"
		return res
	}
	res.Latest = info.Version
	res.URL = info.URL
	res.Notes = info.Notes
	res.HasNew = compareVersion(info.Version, res.Current) > 0
	return res
}

// compareVersion 比较语义化版本（仅数值段），a>b 返回 1，相等 0，a<b 返回 -1。
func compareVersion(a, b string) int {
	as := strings.Split(strings.TrimSpace(a), ".")
	bs := strings.Split(strings.TrimSpace(b), ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		ai, bi := 0, 0
		if i < len(as) {
			ai = atoiSafe(as[i])
		}
		if i < len(bs) {
			bi = atoiSafe(bs[i])
		}
		if ai > bi {
			return 1
		}
		if ai < bi {
			return -1
		}
	}
	return 0
}

func atoiSafe(s string) int {
	v := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		v = v*10 + int(c-'0')
	}
	return v
}

// ---- 插件转发（Redis/ES/SSH/SFTP/FTP/DB，全部无状态）----

// PluginConnect 建立并测试连接。
func (a *App) PluginConnect(conn model.PluginConn) plugins.PluginOpResult {
	return plugins.PluginTest(conn)
}

// PluginDisconnect 断开连接（TTL 连接池自动回收）。
func (a *App) PluginDisconnect(conn model.PluginConn) plugins.PluginOpResult {
	return plugins.PluginOpResult{Ok: true, Info: "已断开"}
}

// PluginTest 连接测试。
func (a *App) PluginTest(conn model.PluginConn) plugins.PluginOpResult {
	return plugins.PluginTest(conn)
}

// PluginRedisKeys 列举匹配模式的 Redis 键。
func (a *App) PluginRedisKeys(conn model.PluginConn, pattern string, db int) ([]plugins.RedisKey, error) {
	return plugins.PluginRedisKeys(conn, pattern, db)
}

// PluginRedisValue 获取 Redis 键的值。
func (a *App) PluginRedisValue(conn model.PluginConn, key string, db int) (plugins.RedisValue, error) {
	return plugins.PluginRedisValue(conn, key, db)
}

// PluginRedisSet 设置 Redis 字符串键。
func (a *App) PluginRedisSet(conn model.PluginConn, key, value string, ttl, db int) error {
	return plugins.PluginRedisSet(conn, key, value, ttl, db)
}

// PluginRedisDel 删除 Redis 键。
func (a *App) PluginRedisDel(conn model.PluginConn, key string, db int) error {
	return plugins.PluginRedisDel(conn, key, db)
}

// PluginRedisTTL 返回 Redis 键过期秒数。
func (a *App) PluginRedisTTL(conn model.PluginConn, key string, db int) (int, error) {
	return plugins.PluginRedisTTL(conn, key, db)
}

// PluginESIndices 列出 ES 索引。
func (a *App) PluginESIndices(conn model.PluginConn) ([]plugins.ESIndex, error) {
	return plugins.PluginESIndices(conn)
}

// PluginESSearch 执行 ES 查询。
func (a *App) PluginESSearch(conn model.PluginConn, index, query string) (string, error) {
	return plugins.PluginESSearch(conn, index, query)
}

// PluginSSHExec 在远端执行命令。
func (a *App) PluginSSHExec(conn model.PluginConn, command string) (string, error) {
	return plugins.PluginSSHExec(conn, command)
}

// PluginSSHOpen 建立带 PTY 的持久 SSH 会话（输出经事件推送前端）。
func (a *App) PluginSSHOpen(conn model.PluginConn) (string, error) {
	return plugins.PluginSSHOpen(a, conn)
}

// PluginSSHInput 向 SSH 会话写入数据。
func (a *App) PluginSSHInput(sessionID, data string) error {
	return plugins.PluginSSHInput(sessionID, data)
}

// PluginSSHResize 同步 PTY 尺寸。
func (a *App) PluginSSHResize(sessionID string, rows, cols int) error {
	return plugins.PluginSSHResize(sessionID, rows, cols)
}

// PluginSSHClose 关闭 SSH 会话。
func (a *App) PluginSSHClose(sessionID string) error {
	return plugins.PluginSSHClose(sessionID)
}

// PluginSetClipboard 写入系统剪贴板（XShell 风格粘贴）。
func (a *App) PluginSetClipboard(text string) error {
	return plugins.PluginSetClipboard(a, text)
}

// PluginSFTPList 列出目录。
func (a *App) PluginSFTPList(conn model.PluginConn, path string) ([]plugins.FileInfo, error) {
	return plugins.PluginSFTPList(conn, path)
}

// PluginSFTPRead 读文件（base64）。
func (a *App) PluginSFTPRead(conn model.PluginConn, path string) (string, error) {
	return plugins.PluginSFTPRead(conn, path)
}

// PluginSFTPWrite 写文件（base64）。
func (a *App) PluginSFTPWrite(conn model.PluginConn, path, contentB64 string) error {
	return plugins.PluginSFTPWrite(conn, path, contentB64)
}

// PluginSFTPUploadB64 上传本地文件（base64）。
func (a *App) PluginSFTPUploadB64(conn model.PluginConn, remoteDir, name, b64 string) error {
	return plugins.PluginSFTPUploadB64(conn, remoteDir, name, b64)
}

// PluginSFTPRename 重命名。
func (a *App) PluginSFTPRename(conn model.PluginConn, oldPath, newPath string) error {
	return plugins.PluginSFTPRename(conn, oldPath, newPath)
}

// PluginSFTPDownload 下载远端文件到本地，返回保存路径。
func (a *App) PluginSFTPDownload(conn model.PluginConn, path, name string) (string, error) {
	return plugins.PluginSFTPDownload(a, conn, path, name)
}

// PluginSFTPMkdir 新建目录。
func (a *App) PluginSFTPMkdir(conn model.PluginConn, path string) error {
	return plugins.PluginSFTPMkdir(conn, path)
}

// PluginSFTPDelete 删除（递归）。
func (a *App) PluginSFTPDelete(conn model.PluginConn, path string) error {
	return plugins.PluginSFTPDelete(conn, path)
}

// PluginFTPList 列出目录。
func (a *App) PluginFTPList(conn model.PluginConn, path string) ([]plugins.FileInfo, error) {
	return plugins.PluginFTPList(conn, path)
}

// PluginFTPRead 读文件。
func (a *App) PluginFTPRead(conn model.PluginConn, path string) (string, error) {
	return plugins.PluginFTPRead(conn, path)
}

// PluginFTPWrite 写文件。
func (a *App) PluginFTPWrite(conn model.PluginConn, path, content string) error {
	return plugins.PluginFTPWrite(conn, path, content)
}

// PluginFTPUploadB64 上传本地文件（base64）。
func (a *App) PluginFTPUploadB64(conn model.PluginConn, remoteDir, name, b64 string) error {
	return plugins.PluginFTPUploadB64(conn, remoteDir, name, b64)
}

// PluginFTPDownload 下载 FTP 文件到本地，返回保存路径。
func (a *App) PluginFTPDownload(conn model.PluginConn, path, name string) (string, error) {
	return plugins.PluginFTPDownload(a, conn, path, name)
}

// PluginFTPRename 重命名。
func (a *App) PluginFTPRename(conn model.PluginConn, oldPath, newPath string) error {
	return plugins.PluginFTPRename(conn, oldPath, newPath)
}

// PluginFTPMkdir 新建目录。
func (a *App) PluginFTPMkdir(conn model.PluginConn, path string) error {
	return plugins.PluginFTPMkdir(conn, path)
}

// PluginFTPDelete 删除。
func (a *App) PluginFTPDelete(conn model.PluginConn, path string) error {
	return plugins.PluginFTPDelete(conn, path)
}

// PluginDBTest 数据库连接测试。
func (a *App) PluginDBTest(conn model.PluginConn) plugins.PluginOpResult {
	return plugins.PluginDBTest(conn)
}

// PluginDBDatabases 列出数据库。
func (a *App) PluginDBDatabases(conn model.PluginConn) (plugins.DBInfo, error) {
	return plugins.PluginDBDatabases(conn)
}

// PluginDBTables 列出表。
func (a *App) PluginDBTables(conn model.PluginConn, database string) ([]plugins.DBTable, error) {
	return plugins.PluginDBTables(conn, database)
}

// PluginDBColumns 查看表结构。
func (a *App) PluginDBColumns(conn model.PluginConn, database, table string) ([]plugins.DBColumn, error) {
	return plugins.PluginDBColumns(conn, database, table)
}

// PluginDBSchema 批量读取表结构（表 + 字段）。
func (a *App) PluginDBSchema(conn model.PluginConn, database string, tables []string) ([]plugins.DBSchema, error) {
	return plugins.PluginDBSchema(conn, database, tables)
}

// PluginDBQuery 执行查询。
func (a *App) PluginDBQuery(conn model.PluginConn, req plugins.DBQueryReq) (*plugins.DBRow, error) {
	return plugins.PluginDBQuery(conn, req)
}

// PluginDBExec 执行 DML/DDL。
func (a *App) PluginDBExec(conn model.PluginConn, req plugins.DBExecReq) (int64, error) {
	return plugins.PluginDBExec(conn, req)
}

// ExportFileInDir 判断 path 是否位于导出目录内（局域网下载校验）。
func (a *App) ExportFileInDir(path string) bool {
	if a == nil || a.data == nil {
		return false
	}
	return exportPathAllowed(a.data.Dir(), path)
}

// ExportDirPath 返回导出目录绝对路径。
func (a *App) ExportDirPath() string {
	if a == nil || a.data == nil {
		return ""
	}
	return joinExportDir(a.data.Dir())
}
