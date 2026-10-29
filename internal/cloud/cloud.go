// Package cloud 提供「文档模板库」与自建云同步服务的同步能力。
//
// 同步复用现有的项目同步接口（PUT/GET /api/projects/{id}），把模板库承载在一个保留的
// 「模板库项目」里：服务端只需按原样存取 data JSON，无需新增接口即可跨设备共享模板。
package cloud

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"apitool/internal/model"
)

// TemplateProjectID 云端承载模板库的保留项目 ID（请勿在项目列表中出现）。
const TemplateProjectID = "__doc_templates__"

// TemplateProjectName 云端保留项目的显示名。
const TemplateProjectName = "文档模板库（勿删）"

// SyncResult 一次模板同步的结果（用于前端提示）。
type SyncResult struct {
	Total   int `json:"total"`   // 同步后本地模板总数
	Changed int `json:"changed"` // 本次新增 / 更新的条数
}

// Client 云端同步客户端。
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// New 创建客户端；baseURL 未带协议时补 http://，并去掉结尾斜杠。
func New(baseURL, token string) *Client {
	u := strings.TrimSpace(baseURL)
	if u != "" && !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		u = "http://" + u
	}
	u = strings.TrimRight(u, "/")
	return &Client{BaseURL: u, Token: strings.TrimSpace(token), HTTP: &http.Client{Timeout: 20 * time.Second}}
}

// templatePayload 云端保留项目的数据结构：既是合法项目对象，又承载模板库。
type templatePayload struct {
	ID           string                    `json:"id"`
	Name         string                    `json:"name"`
	Dirs         []model.Directory         `json:"dirs"`
	Apis         []model.ApiInfo           `json:"apis"`
	Environments []model.Environment       `json:"environments"`
	Common       model.CommonParams        `json:"common"`
	UpdatedAt    string                    `json:"updatedAt"`
	DocTemplates []model.DocLayoutTemplate `json:"docTemplates"`
}

func newPayload(tpls []model.DocLayoutTemplate, updatedAt string) templatePayload {
	if tpls == nil {
		tpls = []model.DocLayoutTemplate{}
	}
	return templatePayload{
		ID: TemplateProjectID, Name: TemplateProjectName,
		Dirs: []model.Directory{}, Apis: []model.ApiInfo{}, Environments: []model.Environment{},
		UpdatedAt: updatedAt, DocTemplates: tpls,
	}
}

// do 发起一次云端请求；成功时把响应体解析到 out（可为 nil）。
func (c *Client) do(method, path string, body any, out any) error {
	if c.BaseURL == "" {
		return errors.New("未配置云服务器地址（设置 → 云同步）")
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.BaseURL+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("连接云服务器失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		if e.Error != "" {
			return fmt.Errorf("%s", e.Error)
		}
		return fmt.Errorf("云服务器返回 %d", resp.StatusCode)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("解析云服务器响应失败: %w", err)
		}
	}
	return nil
}

// remoteProject 云端项目响应（data 为任意 JSON）。
type remoteProject struct {
	UpdatedAt string          `json:"updatedAt"`
	Data      json.RawMessage `json:"data"`
}

// PullTemplates 拉取云端模板库；云端尚未建立模板库时返回空切片（不视为错误）。
func (c *Client) PullTemplates() ([]model.DocLayoutTemplate, error) {
	var resp remoteProject
	if err := c.do(http.MethodGet, "/api/projects/"+TemplateProjectID, nil, &resp); err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(resp.Data) == 0 {
		return nil, nil
	}
	var p templatePayload
	if err := json.Unmarshal(resp.Data, &p); err != nil {
		return nil, fmt.Errorf("云端模板库格式不正确: %w", err)
	}
	return p.DocTemplates, nil
}

// PushTemplates 用本地模板库覆盖云端模板库（云端版本落后时服务端可能要求先拉取）。
func (c *Client) PushTemplates(tpls []model.DocLayoutTemplate) error {
	now := time.Now().Format(time.RFC3339)
	p := newPayload(tpls, now)
	body := map[string]any{"name": TemplateProjectName, "updatedAt": now, "data": p}
	err := c.do(http.MethodPut, "/api/projects/"+TemplateProjectID, body, nil)
	if err != nil && isConflict(err) {
		// 服务端要求先拉取（避免覆盖），拉一次后重试
		if _, perr := c.PullTemplates(); perr != nil {
			return perr
		}
		return c.do(http.MethodPut, "/api/projects/"+TemplateProjectID, body, nil)
	}
	return err
}

func isNotFound(err error) bool {
	s := err.Error()
	return strings.Contains(s, "404") || strings.Contains(s, "不存在")
}

func isConflict(err error) bool {
	s := err.Error()
	return strings.Contains(s, "409") || strings.Contains(s, "请先拉取")
}

// MergeTemplates 合并模板库：以名称为键，remote 覆盖同名本地模板，保留本地独有项，
// 顺序为「本地顺序在前，新增的远端模板追加在后」。返回合并结果与变更数量。
func MergeTemplates(local, remote []model.DocLayoutTemplate) (merged []model.DocLayoutTemplate, changed int) {
	merged = append([]model.DocLayoutTemplate{}, local...)
	idx := make(map[string]int, len(merged))
	for i, t := range merged {
		idx[t.Name] = i
	}
	for _, r := range remote {
		name := strings.TrimSpace(r.Name)
		if name == "" {
			continue
		}
		r.Name = name
		if i, ok := idx[name]; ok {
			merged[i] = r
			changed++
			continue
		}
		idx[name] = len(merged)
		merged = append(merged, r)
		changed++
	}
	return merged, changed
}
