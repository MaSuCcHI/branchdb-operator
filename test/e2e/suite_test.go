// Package e2e はブランチ作成〜MySQL接続〜削除のライフサイクルを実機で検証する。
//
// 環境変数:
//
//	BRANCHDB_API_URL   API サーバーの URL（例: http://localhost:8080）
//	BRANCHDB_SNAPSHOT  使用するスナップショット名（デフォルト: base）
//	KUBECONFIG         k3s の kubeconfig パス（デフォルト: ~/.kube/config）
package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

const (
	defaultAPIURL    = "http://localhost:8080"
	defaultSnapshot  = "base"
	defaultTimeout   = 3 * time.Minute
	pollInterval     = 3 * time.Second
)

func apiURL() string {
	if v := os.Getenv("BRANCHDB_API_URL"); v != "" {
		return v
	}
	return defaultAPIURL
}

func snapshotRef() string {
	if v := os.Getenv("BRANCHDB_SNAPSHOT"); v != "" {
		return v
	}
	return defaultSnapshot
}

// waitFor は predicate が true になるか timeout に達するまでポーリングする。
func waitFor(ctx context.Context, timeout time.Duration, desc string, predicate func() (bool, error)) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ok, err := predicate()
		if err != nil {
			return fmt.Errorf("%s: %w", desc, err)
		}
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: context cancelled", desc)
		case <-time.After(pollInterval):
		}
	}
	return fmt.Errorf("%s: timed out after %s", desc, timeout)
}

// TestMain はテスト前に API サーバーへの疎通を確認する。
func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := waitFor(ctx, 10*time.Second, "API サーバーへの疎通確認", func() (bool, error) {
		resp, err := get(ctx, apiURL()+"/health")
		if err != nil {
			return false, nil // retry
		}
		return resp["status"] == "ok", nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "E2E セットアップ失敗: %v\n", err)
		fmt.Fprintf(os.Stderr, "BRANCHDB_API_URL=%s が正しいか確認してください\n", apiURL())
		os.Exit(1)
	}

	os.Exit(m.Run())
}
