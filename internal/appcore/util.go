package appcore

import (
	"apitool/internal/httpx"
	"apitool/internal/model"
)

// httpxSend 独立转发（便于测试替换）。
func httpxSend(spec model.RequestSpec) model.ResponseData {
	return httpx.SendRequest(spec)
}
