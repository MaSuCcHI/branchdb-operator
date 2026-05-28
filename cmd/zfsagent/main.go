// ZFS Agent は ZFS サーバー上で動く薄い HTTP サーバー。
// BranchDB Operator から HTTP で呼び出され、ZFS のスナップショット・クローン操作を実行する。
//
// 環境変数:
//
//	ZFSAGENT_ADDR    リッスンアドレス（デフォルト: :9090）
//	ZFSAGENT_TOKEN   認証トークン（必須）
//	ZFSAGENT_POOL    ZFS pool 名（デフォルト: tank）
//	ZFSAGENT_DATASET ZFS dataset 名（デフォルト: mysql）
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/MaSuCcHI/branchdb-operator/internal/infrastructure/zfs"
	"github.com/MaSuCcHI/branchdb-operator/internal/interface/api/zfsagent"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := loadConfig()

	if cfg.Token == "" {
		return fmt.Errorf("ZFSAGENT_TOKEN is required")
	}

	dataset := cfg.Pool + "/" + cfg.Dataset
	zfsProvider := zfs.NewAgentProvider(dataset)

	handler := zfsagent.NewHandler(zfsProvider, cfg.Token)
	router := zfsagent.NewRouter(handler)

	srv := &http.Server{
		Addr:         cfg.Addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("zfsagent listening on %s (pool=%s, dataset=%s)", cfg.Addr, cfg.Pool, cfg.Dataset)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("server error: %v", err)
		}
	}()

	<-quit
	log.Println("shutting down...")
	return nil
}

type config struct {
	Addr    string
	Token   string
	Pool    string
	Dataset string
}

func loadConfig() config {
	return config{
		Addr:    envOr("ZFSAGENT_ADDR", ":9090"),
		Token:   os.Getenv("ZFSAGENT_TOKEN"),
		Pool:    envOr("ZFSAGENT_POOL", "tank"),
		Dataset: envOr("ZFSAGENT_DATASET", "mysql"),
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
