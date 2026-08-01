package util

import (
	"strings"

	"apitool/internal/model"
)

// EnabledEnvVars 将环境变量列表转换为 key/value 对，仅保留启用且非空的项。
func EnabledEnvVars(vars []model.EnvVar) []model.KV {
	out := []model.KV{}
	for _, v := range vars {
		if v.Enabled && v.Key != "" {
			out = append(out, model.KV{Key: v.Key, Value: v.Value, Enabled: true})
		}
	}
	return out
}

// MergeCommon 将项目公共参数合并进请求规格（用例/接口自身同名参数优先覆盖公共）。
func MergeCommon(spec *model.RequestSpec, common model.CommonParams) {
	hm := map[string]model.KV{}
	for _, h := range common.Headers {
		if h.Enabled && h.Key != "" {
			hm[strings.ToLower(h.Key)] = h
		}
	}
	for _, h := range spec.Headers {
		if h.Enabled && h.Key != "" {
			hm[strings.ToLower(h.Key)] = h
		}
	}
	spec.Headers = []model.KV{}
	for _, v := range hm {
		spec.Headers = append(spec.Headers, v)
	}
	qm := map[string]model.KV{}
	for _, q := range common.Query {
		if q.Enabled && q.Key != "" {
			qm[strings.ToLower(q.Key)] = q
		}
	}
	for _, q := range spec.Query {
		if q.Enabled && q.Key != "" {
			qm[strings.ToLower(q.Key)] = q
		}
	}
	spec.Query = []model.KV{}
	for _, v := range qm {
		spec.Query = append(spec.Query, v)
	}
}
