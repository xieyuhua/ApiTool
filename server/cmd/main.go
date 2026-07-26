package main

import (
	"flag"
	"log"
	"net/http"

	"apitool/server"
)

func main() {
	addr := flag.String("addr", ":8080", "监听地址，如 :8080 或 0.0.0.0:8080")
	data := flag.String("data", "apitool-server-data", "数据存储目录")
	flag.Parse()

	s := server.NewStore(*data)
	log.Printf("Apitool 同步服务已启动：%s （数据目录：%s）", *addr, *data)
	log.Fatal(http.ListenAndServe(*addr, s.Handler()))
}
