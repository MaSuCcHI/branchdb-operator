package e2e

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// TestPostgresBranchLifecycle は PostgreSQL ブランチ作成→接続→データ確認→削除を検証する。
func TestPostgresBranchLifecycle(t *testing.T) {
	ctx := context.Background()
	branchName := fmt.Sprintf("pg-e2e-%d", time.Now().Unix())

	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = del(cleanCtx, branchURL(branchName))
	})

	// 1. ブランチ作成
	t.Log("1. PostgreSQL ブランチを作成します:", branchName)
	resp, err := post(ctx, apiURL()+"/branches", map[string]any{
		"name":          branchName,
		"snapshot_ref":  snapshotRef(),
		"ttl_hours":     1,
		"database_type": "postgres",
	})
	if err != nil {
		t.Fatalf("POST /branches 失敗: %v", err)
	}
	t.Logf("   レスポンス status=%v port=%v", resp["status"], resp["port"])

	// 2. Ready + NodePort 確定待ち
	t.Log("2. ブランチが Ready になるまで待機します...")
	var nodePort float64
	err = waitFor(ctx, defaultTimeout, "branch ready", func() (bool, error) {
		b, err := get(ctx, branchURL(branchName))
		if err != nil {
			return false, nil
		}
		status, _ := b["status"].(string)
		if strings.EqualFold(status, "error") {
			msg, _ := b["message"].(string)
			return false, fmt.Errorf("branch error: %s", msg)
		}
		port, ok := b["port"].(float64)
		if !ok || port == 0 {
			return false, nil
		}
		nodePort = port
		t.Logf("   status=%s port=%.0f", status, port)
		return strings.EqualFold(status, "ready"), nil
	})
	if err != nil {
		t.Fatalf("ブランチが Ready になりませんでした: %v", err)
	}

	// 3. PostgreSQL 接続待ち
	host := mysqlHost() // NodePort ホストは MySQL と同じ VM IP
	t.Logf("3. PostgreSQL (port=%.0f) の接続を待ちます...", nodePort)
	dsn := fmt.Sprintf("postgres://postgres@%s:%.0f/e2e_seed?sslmode=disable", host, nodePort)
	var db *sql.DB
	err = waitFor(ctx, defaultTimeout, "postgres ready", func() (bool, error) {
		var connErr error
		db, connErr = sql.Open("postgres", dsn)
		if connErr != nil {
			return false, nil
		}
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if pingErr := db.PingContext(pingCtx); pingErr != nil {
			db.Close()
			db = nil
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		t.Fatalf("PostgreSQL に接続できませんでした (dsn=%s): %v", dsn, err)
	}
	defer db.Close()
	t.Log("   PostgreSQL 接続成功")

	// 4. シードデータの確認
	t.Log("4. シードデータ e2e_seed.marker を確認します...")
	var count int
	row := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM marker")
	if err := row.Scan(&count); err != nil {
		t.Fatalf("e2e_seed.marker の確認失敗: %v", err)
	}
	if count != 1 {
		t.Fatalf("e2e_seed.marker の行数が期待と異なります: got=%d, want=1", count)
	}
	t.Logf("   e2e_seed.marker: %d 行 ✓", count)

	// 5. データ独立性確認
	t.Log("5. データ独立性を確認します（別ブランチを作成）...")
	branch2 := branchName + "-iso"
	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = del(cleanCtx, branchURL(branch2))
	})

	_, err = post(ctx, apiURL()+"/branches", map[string]any{
		"name":          branch2,
		"snapshot_ref":  snapshotRef(),
		"ttl_hours":     1,
		"database_type": "postgres",
	})
	if err != nil {
		t.Fatalf("2つ目のブランチ作成失敗: %v", err)
	}

	var nodePort2 float64
	err = waitFor(ctx, defaultTimeout, "branch2 ready", func() (bool, error) {
		b, err := get(ctx, branchURL(branch2))
		if err != nil {
			return false, nil
		}
		port, ok := b["port"].(float64)
		if !ok || port == 0 {
			return false, nil
		}
		nodePort2 = port
		status, _ := b["status"].(string)
		return strings.EqualFold(status, "ready"), nil
	})
	if err != nil {
		t.Fatalf("2つ目のブランチが Ready になりませんでした: %v", err)
	}

	// branch1 に書き込み
	if _, err := db.ExecContext(ctx, "INSERT INTO marker VALUES (2)"); err != nil {
		t.Fatalf("branch1 への書き込み失敗: %v", err)
	}

	// branch2 に接続して branch1 の書き込みが見えないことを確認
	dsn2 := fmt.Sprintf("postgres://postgres@%s:%.0f/e2e_seed?sslmode=disable", host, nodePort2)
	var db2 *sql.DB
	err = waitFor(ctx, defaultTimeout, "branch2 postgres ready", func() (bool, error) {
		var connErr error
		db2, connErr = sql.Open("postgres", dsn2)
		if connErr != nil {
			return false, nil
		}
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if pingErr := db2.PingContext(pingCtx); pingErr != nil {
			db2.Close()
			db2 = nil
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		t.Fatalf("branch2 の PostgreSQL に接続できませんでした: %v", err)
	}
	defer db2.Close()

	var count2 int
	row2 := db2.QueryRowContext(ctx, "SELECT COUNT(*) FROM marker")
	if err := row2.Scan(&count2); err != nil {
		t.Fatalf("branch2 の marker 確認失敗: %v", err)
	}
	if count2 != 1 {
		t.Fatalf("branch2 に branch1 の書き込みが伝播しています: got=%d, want=1", count2)
	}
	t.Logf("   データ独立性 OK: branch1=%d行, branch2=%d行 ✓", count+1, count2)

	// 6. 削除
	t.Log("6. ブランチを削除します...")
	if err := del(ctx, branchURL(branchName)); err != nil {
		t.Fatalf("DELETE /branches/%s 失敗: %v", branchName, err)
	}
	if err := del(ctx, branchURL(branch2)); err != nil {
		t.Fatalf("DELETE /branches/%s 失敗: %v", branch2, err)
	}

	err = waitFor(ctx, 60*time.Second, "branch deleted", func() (bool, error) {
		_, err := get(ctx, branchURL(branchName))
		if err != nil {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		t.Fatalf("ブランチが削除されませんでした: %v", err)
	}
	t.Log("   ブランチ削除完了 ✓")
}
