package util

import (
	"strings"
	"testing"

	"apitool/internal/model"
)

func TestGenID(t *testing.T) {
	a := GenID()
	b := GenID()
	if a == "" || b == "" {
		t.Fatal("GenID 不应返回空串")
	}
	if a == b {
		t.Fatal("GenID 应返回不同值")
	}
	// UUID v4 形如 8-4-4-4-12
	if !strings.Contains(a, "-") {
		t.Fatalf("GenID 格式异常: %s", a)
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := FirstNonEmpty(); got != "" {
		t.Fatalf("空参数期望空串，实际 %q", got)
	}
	if got := FirstNonEmpty("", "", "x", "y"); got != "x" {
		t.Fatalf("期望首个非空 x，实际 %q", got)
	}
	if got := FirstNonEmpty("", ""); got != "" {
		t.Fatalf("全空期望空串，实际 %q", got)
	}
}

func TestTruncate(t *testing.T) {
	if got := Truncate("hello", 0); got != "hello" {
		t.Fatalf("n<=0 应原样返回，实际 %q", got)
	}
	if got := Truncate("hello", 10); got != "hello" {
		t.Fatalf("短串不应截断，实际 %q", got)
	}
	if got := Truncate("hello世界", 5); got != "hello..." {
		t.Fatalf("按 rune 截断到 5 应得 hello...，实际 %q", got)
	}
	if got := Truncate("hello世界", 3); got != "hel..." {
		t.Fatalf("按 rune 截断到 3 应得 hel...，实际 %q", got)
	}
}

func TestEnabledEnvVars(t *testing.T) {
	vars := []model.EnvVar{
		{Key: "A", Value: "1", Enabled: true},
		{Key: "B", Value: "2", Enabled: false},
		{Key: "", Value: "3", Enabled: true},
		{Key: "C", Value: "4", Enabled: true},
	}
	out := EnabledEnvVars(vars)
	if len(out) != 2 {
		t.Fatalf("期望 2 个启用且非空，实际 %d: %+v", len(out), out)
	}
	if out[0].Key != "A" || out[1].Key != "C" {
		t.Fatalf("期望 A,C，实际 %+v", out)
	}
	for _, kv := range out {
		if !kv.Enabled {
			t.Fatalf("输出项应强制 Enabled=true: %+v", kv)
		}
	}
}

func TestMergeCommon(t *testing.T) {
	// 用例自身同名参数应覆盖公共参数
	common := model.CommonParams{
		Headers: []model.KV{{Key: "Content-Type", Value: "text/plain", Enabled: true}},
		Query:   []model.KV{{Key: "page", Value: "1", Enabled: true}},
	}
	spec := &model.RequestSpec{
		Headers: []model.KV{
			{Key: "Content-Type", Value: "application/json", Enabled: true},
			{Key: "X-Token", Value: "abc", Enabled: true},
		},
		Query: []model.KV{
			{Key: "page", Value: "2", Enabled: true},
			{Key: "size", Value: "10", Enabled: false}, // 禁用不应合并
		},
	}
	MergeCommon(spec, common)

	hm := map[string]string{}
	for _, h := range spec.Headers {
		hm[strings.ToLower(h.Key)] = h.Value
	}
	if hm["content-type"] != "application/json" {
		t.Fatalf("用例同名 Header 应覆盖公共，实际 %q", hm["content-type"])
	}
	if hm["x-token"] != "abc" {
		t.Fatalf("用例独立 Header 应保留，实际 %q", hm["x-token"])
	}
	qm := map[string]string{}
	for _, q := range spec.Query {
		qm[strings.ToLower(q.Key)] = q.Value
	}
	if qm["page"] != "2" {
		t.Fatalf("用例同名 Query 应覆盖公共，实际 %q", qm["page"])
	}
	if _, ok := qm["size"]; ok {
		t.Fatalf("禁用 Query 不应合并，实际 %+v", qm)
	}
}
