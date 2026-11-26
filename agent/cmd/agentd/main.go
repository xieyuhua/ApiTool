// agentd 是 AI Agent 的独立部署入口。
//
// 与桌面端、主服务端完全独立：一个进程、一份数据、一个访问令牌。
//
//	go build -o agentd ./agent/cmd/agentd
//	./agentd -addr :8090 -data ./agent-data -token <随机串>
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"apitool/agent"
)

// version 由构建脚本通过 -ldflags "-X main.version=..." 注入。
var version = "1.0.0"

func main() {
	cfg, err := agent.LoadConfig(os.Args[1:])
	if err != nil {
		log.Fatalf("配置解析失败: %v", err)
	}
	if cfg.Token == "" && !cfg.IsLoopback() {
		log.Fatalf("拒绝启动：监听地址 %s 未绑定本机，但未配置 -token。\n"+
			"Agent 具备文件读写与命令执行能力，对外提供服务必须设置访问令牌。", cfg.Addr)
	}

	srv, err := agent.New(cfg, version)
	if err != nil {
		log.Fatalf("初始化失败: %v", err)
	}
	defer func() { _ = srv.Close() }()

	// 优雅退出：容器 stop / Ctrl+C 时释放数据库连接
	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
		<-ch
		log.Println("正在退出…")
		_ = srv.Close()
		os.Exit(0)
	}()

	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("服务退出: %v", err)
	}
}
