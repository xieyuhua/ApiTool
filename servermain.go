//go:build serveronly

// ApiTool 2.0 服务端入口。带 serveronly 构建标签编译：
//
//	先构建前端：cd frontend && npm run build
//	构建服务端：go build -tags "serveronly production" -o apitool-server.exe .
//
// 与桌面端（默认无标签 go build .）共用同一 package main 目录但互斥编译：
// serveronly 下不编译桌面入口（main.go 带 !serveronly 标签）。
package main

import (
	"embed"
	"log"

	"apitool/server/app"
)

//go:embed all:frontend/dist
var webAssets embed.FS

func main() {
	cfg, err := app.LoadConfig()
	if err != nil {
		log.Fatalf("读取配置失败: %v", err)
	}
	srv, err := app.New(cfg, "2.0.0", webAssets)
	if err != nil {
		log.Fatalf("启动服务端失败: %v", err)
	}
	defer func() { _ = srv.Close() }()
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
