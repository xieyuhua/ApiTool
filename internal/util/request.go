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

// OverrideByCommon 将项目公共参数合并进请求规格，且「公共参数优先覆盖用例自身同名参数」。
// 仅用于测试执行：环境变量/公共参数应覆盖用例快照（副本），而非被副本覆盖。
// 优先级（低→高）：用例自身 < 公共参数。body / form 等仍由 httpx 的 {{占位}} 替换处理。
func OverrideByCommon(spec *model.RequestSpec, common model.CommonParams) {
	hm := map[string]model.KV{}
	for _, h := range spec.Headers {
		if h.Enabled && h.Key != "" {
			hm[strings.ToLower(h.Key)] = h
		}
	}
	for _, h := range common.Headers {
		if h.Enabled && h.Key != "" {
			hm[strings.ToLower(h.Key)] = h
		}
	}
	spec.Headers = []model.KV{}
	for _, v := range hm {
		spec.Headers = append(spec.Headers, v)
	}
	qm := map[string]model.KV{}
	for _, q := range spec.Query {
		if q.Enabled && q.Key != "" {
			qm[strings.ToLower(q.Key)] = q
		}
	}
	for _, q := range common.Query {
		if q.Enabled && q.Key != "" {
			qm[strings.ToLower(q.Key)] = q
		}
	}
	spec.Query = []model.KV{}
	for _, v := range qm {
		spec.Query = append(spec.Query, v)
	}
}

// OverrideByEnv 用环境变量按 key 覆盖请求规格中同名的 header / query（环境变量优先）。
// 与 httpx 的 {{占位}} 替换互补：此处处理「副本里已写死的值」，
// 例如环境变量 key 与请求头名一致时直接覆盖其值；{{token}} 形式的占位仍由 httpx 在执行时替换。
// 优先级（低→高）：用例自身 < 公共参数 < 环境变量。
func OverrideByEnv(spec *model.RequestSpec, env []model.KV) {
	em := map[string]string{}
	for _, e := range env {
		if e.Enabled && e.Key != "" {
			em[strings.ToLower(e.Key)] = e.Value
		}
	}
	if len(em) == 0 {
		return
	}
	for i := range spec.Headers {
		if v, ok := em[strings.ToLower(spec.Headers[i].Key)]; ok {
			spec.Headers[i].Value = v
		}
	}
	for i := range spec.Query {
		if v, ok := em[strings.ToLower(spec.Query[i].Key)]; ok {
			spec.Query[i].Value = v
		}
	}
}
