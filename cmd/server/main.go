// Command server runs the local field spatial-correction workbench.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"fieldbench/internal/httpapi"
	"fieldbench/internal/store"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:5580", "HTTP listen address")
	dbPath := flag.String("db", "fieldbench.db", "SQLite database path")
	flag.Parse()

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("打开 SQLite 失败: %v", err)
	}
	defer st.Close()

	svc := httpapi.New(st)
	if err := svc.EnsureSeeded(context.Background()); err != nil {
		log.Fatalf("初始化 fixture 失败: %v", err)
	}

	srv := &http.Server{
		Addr:              *listen,
		Handler:           httpapi.NewServer(svc).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	fmt.Printf("田区空间校正台 已启动: http://%s （数据库 %s）\n", *listen, *dbPath)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
