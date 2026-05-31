// ZFS Agent は ZFS サーバー上で動く薄い HTTP サーバー。
// BranchDB Operator から HTTP で呼び出され、ZFS のスナップショット・クローン操作を実行する。
//
// 環境変数:
//
//	ZFSAGENT_ADDR     リッスンアドレス（デフォルト: :9090）
//	ZFSAGENT_TOKEN    認証トークン（必須）
//	ZFSAGENT_DATASETS DB 種別と dataset のマッピング（例: mysql:tank/mysql,postgres:tank/postgres）
//	                  省略時は ZFSAGENT_POOL / ZFSAGENT_DATASET で単一 dataset を設定する
//	ZFSAGENT_POOL     ZFS pool 名（デフォルト: tank）ZFSAGENT_DATASETS 省略時のみ参照
//	ZFSAGENT_DATASET  ZFS dataset 名（デフォルト: mysql）ZFSAGENT_DATASETS 省略時のみ参照
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
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

	providers := buildProviders(cfg)
	if len(providers) == 0 {
		return fmt.Errorf("no datasets configured")
	}

	handler := zfsagent.NewHandler(providers, cfg.Token)
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
		log.Printf("zfsagent listening on %s (datasets=%v)", cfg.Addr, cfg.Datasets)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("server error: %v", err)
		}
	}()

	<-quit
	log.Println("shutting down...")
	return nil
}

// buildProviders は設定から dbType → AgentVolumeProvider のマップを構築する。
func buildProviders(cfg config) map[string]zfsagent.AgentVolumeProvider {
	result := make(map[string]zfsagent.AgentVolumeProvider, len(cfg.Datasets))
	for dbType, dataset := range cfg.Datasets {
		result[dbType] = zfs.NewAgentProvider(dataset)
	}
	return result
}

type config struct {
	Addr     string
	Token    string
	Datasets map[string]string // dbType → "pool/dataset"
}

func loadConfig() config {
	datasets := parseDatasets(os.Getenv("ZFSAGENT_DATASETS"))
	if len(datasets) == 0 {
		// 後方互換: ZFSAGENT_POOL + ZFSAGENT_DATASET から単一 dataset を構築
		pool := envOr("ZFSAGENT_POOL", "tank")
		dataset := envOr("ZFSAGENT_DATASET", "mysql")
		datasets = map[string]string{"mysql": pool + "/" + dataset}
	}
	return config{
		Addr:     envOr("ZFSAGENT_ADDR", ":9090"),
		Token:    os.Getenv("ZFSAGENT_TOKEN"),
		Datasets: datasets,
	}
}

// parseDatasets は "mysql:tank/mysql,postgres:tank/postgres" 形式の文字列をパースする。
func parseDatasets(raw string) map[string]string {
	if raw == "" {
		return nil
	}
	result := make(map[string]string)
	for _, entry := range strings.Split(raw, ",") {
		parts := strings.SplitN(strings.TrimSpace(entry), ":", 2)
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			result[parts[0]] = parts[1]
		}
	}
	return result
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
