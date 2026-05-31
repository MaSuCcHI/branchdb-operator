package e2e

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
)

// waitForBranchClone はブランチの ZFS クローンが作成されるまで待つ（MySQL 起動は待たない）。
// Reconciler がクローンを作成するとステータスが "Pending" から変わる。
func waitForBranchClone(ctx context.Context, t *testing.T, branchName string) {
	t.Helper()
	err := waitFor(ctx, 30*time.Second, "branch clone created", func() (bool, error) {
		b, err := get(ctx, branchURL(branchName))
		if err != nil {
			return false, nil
		}
		status, _ := b["status"].(string)
		return status != "" && status != "Pending", nil
	})
	if err != nil {
		t.Fatalf("ブランチ %s のクローンが作成されませんでした: %v", branchName, err)
	}
}

// waitForBranchDeleted はブランチが 404 になるまで待つ（クローン削除完了を確認）。
func waitForBranchDeleted(ctx context.Context, t *testing.T, branchName string) {
	t.Helper()
	err := waitFor(ctx, 60*time.Second, "branch deleted", func() (bool, error) {
		_, err := get(ctx, branchURL(branchName))
		return err != nil, nil
	})
	if err != nil {
		t.Fatalf("ブランチ %s が削除されませんでした: %v", branchName, err)
	}
}

// TestSnapshotTakeListDelete はブラウザの Snapshots タブ経由で行う
// スナップショット作成→一覧表示→削除の一連フローを検証する。
func TestSnapshotTakeListDelete(t *testing.T) {
	ctx := context.Background()
	snapName := fmt.Sprintf("e2e-snap-%d", time.Now().Unix())

	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = del(cleanCtx, snapshotURL(url.PathEscape(snapName))+"?db_type=mysql")
	})

	// 1. スナップショット作成 (Take Snapshot ボタン相当)
	t.Log("1. スナップショットを作成します:", snapName)
	resp, err := post(ctx, apiURL()+"/snapshots", map[string]any{
		"db_type":   "mysql",
		"name":      snapName,
		"overwrite": false,
	})
	if err != nil {
		t.Fatalf("POST /snapshots 失敗: %v", err)
	}
	if status, _ := resp["status"].(string); status != "ok" {
		t.Fatalf("status=%v, want ok", status)
	}
	t.Logf("   作成完了: name=%s ✓", resp["name"])

	// 2. 一覧で確認し role=current であることを確認 (Snapshots タブ表示相当)
	t.Log("2. 一覧に role=current で表示されることを確認します...")
	snaps, err := listSnapshots(ctx, "mysql")
	if err != nil {
		t.Fatalf("GET /snapshots 失敗: %v", err)
	}
	found := findSnapshot(snaps, snapName)
	if found == nil {
		names := make([]string, len(snaps))
		for i, s := range snaps {
			names[i], _ = s["name"].(string)
		}
		t.Fatalf("スナップショット %s が一覧に見つかりません。存在: %v", snapName, names)
	}
	if role, _ := found["role"].(string); role != "current" {
		t.Fatalf("role=%v, want current", role)
	}
	t.Logf("   確認: name=%s role=%v ✓", snapName, found["role"])

	// 3. 削除 (Delete ボタン相当)
	t.Log("3. スナップショットを削除します...")
	if err := del(ctx, snapshotURL(url.PathEscape(snapName))+"?db_type=mysql"); err != nil {
		t.Fatalf("DELETE /snapshots/%s 失敗: %v", snapName, err)
	}

	// 4. 削除後は一覧から消えていることを確認
	snaps, err = listSnapshots(ctx, "mysql")
	if err != nil {
		t.Fatalf("GET /snapshots 失敗: %v", err)
	}
	if findSnapshot(snaps, snapName) != nil {
		t.Fatalf("削除後もスナップショット %s が残っています", snapName)
	}
	t.Log("   削除確認 ✓")
}

// TestSnapshotOverwriteInPlace は依存クローンが無いときの上書きを検証する。
// 依存なし → 旧スナップショットを削除して新規作成（アーカイブは作られない）。
func TestSnapshotOverwriteInPlace(t *testing.T) {
	ctx := context.Background()
	snapName := fmt.Sprintf("e2e-overwrite-%d", time.Now().Unix())

	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		snaps, _ := listSnapshots(cleanCtx, "mysql")
		for _, s := range snaps {
			name, _ := s["name"].(string)
			if name == snapName || strings.HasPrefix(name, snapName+"-") {
				_ = del(cleanCtx, snapshotURL(url.PathEscape(name))+"?db_type=mysql")
			}
		}
	})

	// 1. 初回スナップショット作成
	t.Log("1. 初回スナップショットを作成します:", snapName)
	if _, err := post(ctx, apiURL()+"/snapshots", map[string]any{
		"db_type":   "mysql",
		"name":      snapName,
		"overwrite": false,
	}); err != nil {
		t.Fatalf("初回 POST /snapshots 失敗: %v", err)
	}

	time.Sleep(time.Second)

	// 2. 上書き (overwrite=true, 依存クローンなし → その場で置き換え)
	t.Log("2. 依存ブランチなしで上書きします (overwrite=true)...")
	if _, err := post(ctx, apiURL()+"/snapshots", map[string]any{
		"db_type":   "mysql",
		"name":      snapName,
		"overwrite": true,
	}); err != nil {
		t.Fatalf("上書き POST /snapshots 失敗: %v", err)
	}

	// 3. 依存クローンがなかったので archived は作られず、同名の current だけが残る
	t.Log("3. archived なし、current のみが残ることを確認します...")
	snaps, err := listSnapshots(ctx, "mysql")
	if err != nil {
		t.Fatalf("GET /snapshots 失敗: %v", err)
	}

	currentCount, archivedCount := 0, 0
	for _, s := range snaps {
		name, _ := s["name"].(string)
		role, _ := s["role"].(string)
		if name == snapName && role == "current" {
			currentCount++
		}
		if strings.HasPrefix(name, snapName+"-") && role == "archived" {
			archivedCount++
		}
	}
	if currentCount != 1 {
		t.Fatalf("current スナップショット数=%d, want 1", currentCount)
	}
	if archivedCount != 0 {
		t.Fatalf("依存クローンなしなのに archived が %d 件作成されています", archivedCount)
	}
	t.Logf("   current=%d, archived=%d ✓", currentCount, archivedCount)
}

// TestSnapshotOverwriteWithBranch はブランチ（依存クローン）がある状態での上書きを検証する。
// ユースケースA: リフレッシュウィンドウ内でのスナップショット更新。
// 依存あり → 旧スナップショットをリネームしてアーカイブ化し新しい current を作成する。
func TestSnapshotOverwriteWithBranch(t *testing.T) {
	ctx := context.Background()
	ts := time.Now().Unix()
	snapName := fmt.Sprintf("e2e-arch-%d", ts)
	branchName := fmt.Sprintf("e2e-arch-br-%d", ts)

	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		// ブランチ削除（先にクローンを消してからスナップショット削除）
		_ = del(cleanCtx, branchURL(branchName))
		waitForBranchDeleted(cleanCtx, t, branchName)
		// スナップショット削除
		snaps, _ := listSnapshots(cleanCtx, "mysql")
		for _, s := range snaps {
			name, _ := s["name"].(string)
			if name == snapName || strings.HasPrefix(name, snapName+"-") {
				_ = del(cleanCtx, snapshotURL(url.PathEscape(name))+"?db_type=mysql")
			}
		}
	})

	// 1. スナップショット作成
	t.Log("1. スナップショットを作成します:", snapName)
	if _, err := post(ctx, apiURL()+"/snapshots", map[string]any{
		"db_type":   "mysql",
		"name":      snapName,
		"overwrite": false,
	}); err != nil {
		t.Fatalf("POST /snapshots 失敗: %v", err)
	}

	// 2. ブランチを作成して依存クローンを作る（ZFS クローンができるまで待つ）
	t.Log("2. ブランチを作成します（依存クローン作成）:", branchName)
	if _, err := post(ctx, apiURL()+"/branches", map[string]any{
		"name":          branchName,
		"snapshot_ref":  snapName,
		"ttl_hours":     1,
		"database_type": "mysql",
	}); err != nil {
		t.Fatalf("POST /branches 失敗: %v", err)
	}
	waitForBranchClone(ctx, t, branchName)
	t.Log("   ZFS クローン作成確認 ✓")

	time.Sleep(time.Second)

	// 3. 上書き (overwrite=true, 依存クローンあり → リネームしてアーカイブ)
	t.Log("3. 依存ブランチありで上書きします (overwrite=true)...")
	if _, err := post(ctx, apiURL()+"/snapshots", map[string]any{
		"db_type":   "mysql",
		"name":      snapName,
		"overwrite": true,
	}); err != nil {
		t.Fatalf("上書き POST /snapshots 失敗: %v", err)
	}

	// 4. current + archived の両方が存在することを確認
	t.Log("4. current + archived の両方が存在することを確認します...")
	snaps, err := listSnapshots(ctx, "mysql")
	if err != nil {
		t.Fatalf("GET /snapshots 失敗: %v", err)
	}

	var currentSnap, archivedSnap map[string]any
	for _, s := range snaps {
		name, _ := s["name"].(string)
		role, _ := s["role"].(string)
		if name == snapName && role == "current" {
			currentSnap = s
		}
		if strings.HasPrefix(name, snapName+"-") && role == "archived" {
			archivedSnap = s
		}
	}
	if currentSnap == nil {
		t.Fatalf("current スナップショット %s が見つかりません", snapName)
	}
	if archivedSnap == nil {
		t.Fatalf("archived スナップショット (%s-YYYYMMDD-HHMMSS) が見つかりません", snapName)
	}
	archivedName, _ := archivedSnap["name"].(string)
	t.Logf("   current=%s ✓  archived=%s ✓", snapName, archivedName)

	// 5. archived スナップショットは Delete ボタンで削除できることを確認
	//    （先にブランチを削除してから。ブランチの origin は archived に移っているため）
	t.Log("5. ブランチを削除してから archived を削除します...")
	if err := del(ctx, branchURL(branchName)); err != nil {
		t.Fatalf("DELETE /branches/%s 失敗: %v", branchName, err)
	}
	waitForBranchDeleted(ctx, t, branchName)

	if err := del(ctx, snapshotURL(url.PathEscape(archivedName))+"?db_type=mysql"); err != nil {
		t.Fatalf("archived 削除失敗: %v", err)
	}
	snaps, _ = listSnapshots(ctx, "mysql")
	if findSnapshot(snaps, archivedName) != nil {
		t.Fatalf("削除後も archived %s が残っています", archivedName)
	}
	t.Log("   archived 削除確認 ✓")
}

// TestGCOrphanClonesAndSnapshots は GC エンドポイントを検証する。
// アーカイブスナップショットが keep_snapshots を超えた場合に自動削除されることを確認する。
func TestGCOrphanClonesAndSnapshots(t *testing.T) {
	ctx := context.Background()
	ts := time.Now().Unix()
	snapName := fmt.Sprintf("e2e-gc-%d", ts)
	branchNames := []string{
		fmt.Sprintf("e2e-gc-br1-%d", ts),
		fmt.Sprintf("e2e-gc-br2-%d", ts),
	}

	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		for _, br := range branchNames {
			_ = del(cleanCtx, branchURL(br))
		}
		for _, br := range branchNames {
			waitForBranchDeleted(cleanCtx, t, br)
		}
		snaps, _ := listSnapshots(cleanCtx, "mysql")
		for _, s := range snaps {
			name, _ := s["name"].(string)
			if name == snapName || strings.HasPrefix(name, snapName+"-") {
				_ = del(cleanCtx, snapshotURL(url.PathEscape(name))+"?db_type=mysql")
			}
		}
	})

	// archived スナップショットを 2 件作る
	// 方法: snap作成 → branch作成(clone) → overwrite(rename archive) × 2
	t.Log("1. アーカイブスナップショットを 2 件作成します...")
	for i, branchName := range branchNames {
		// スナップショット取得
		if _, err := post(ctx, apiURL()+"/snapshots", map[string]any{
			"db_type":   "mysql",
			"name":      snapName,
			"overwrite": i > 0, // 2回目以降は上書き（i=0は新規, i=1は上書き）
		}); err != nil {
			t.Fatalf("POST /snapshots (i=%d) 失敗: %v", i, err)
		}

		// ブランチ作成（依存クローンを作る）
		if _, err := post(ctx, apiURL()+"/branches", map[string]any{
			"name":          branchName,
			"snapshot_ref":  snapName,
			"ttl_hours":     1,
			"database_type": "mysql",
		}); err != nil {
			t.Fatalf("POST /branches %s 失敗: %v", branchName, err)
		}
		waitForBranchClone(ctx, t, branchName)

		time.Sleep(time.Second) // アーカイブ名のタイムスタンプを分ける
	}
	// もう1回上書き（2つ目のアーカイブを作る）
	if _, err := post(ctx, apiURL()+"/snapshots", map[string]any{
		"db_type":   "mysql",
		"name":      snapName,
		"overwrite": true,
	}); err != nil {
		t.Fatalf("POST /snapshots (final) 失敗: %v", err)
	}

	// archived が 2 件あることを確認
	snaps, err := listSnapshots(ctx, "mysql")
	if err != nil {
		t.Fatalf("GET /snapshots 失敗: %v", err)
	}
	archivedCount := 0
	for _, s := range snaps {
		name, _ := s["name"].(string)
		role, _ := s["role"].(string)
		if strings.HasPrefix(name, snapName+"-") && role == "archived" {
			archivedCount++
		}
	}
	if archivedCount < 2 {
		t.Fatalf("archived スナップショットが %d 件です (期待: ≥2件)", archivedCount)
	}
	t.Logf("   archived %d 件確認 ✓", archivedCount)

	// ブランチを削除（クローンを消してから GC 実行）
	t.Log("2. ブランチを削除します...")
	for _, br := range branchNames {
		_ = del(ctx, branchURL(br))
	}
	for _, br := range branchNames {
		waitForBranchDeleted(ctx, t, br)
	}
	t.Log("   ブランチ削除 ✓")

	// 3. GC (keep_snapshots=5) → archived は保持される
	t.Log("3. GC (keep_snapshots=5) → レスポンス形式を確認します...")
	gcResp, err := post(ctx, apiURL()+"/gc", map[string]any{
		"db_type":        "mysql",
		"keep_snapshots": 5,
	})
	if err != nil {
		t.Fatalf("POST /gc 失敗: %v", err)
	}
	if _, ok := gcResp["deleted_orphan_clones"]; !ok {
		t.Fatalf("GC レスポンスに deleted_orphan_clones がありません: %v", gcResp)
	}
	if _, ok := gcResp["deleted_snapshots"]; !ok {
		t.Fatalf("GC レスポンスに deleted_snapshots がありません: %v", gcResp)
	}
	t.Logf("   GC レスポンス形式 OK ✓: %v", gcResp)

	// 4. GC (keep_snapshots=1) → 古い archived が削除される
	t.Log("4. GC (keep_snapshots=1) → archived が 1 件まで削減されることを確認します...")
	gcResp2, err := post(ctx, apiURL()+"/gc", map[string]any{
		"db_type":        "mysql",
		"keep_snapshots": 1,
	})
	if err != nil {
		t.Fatalf("POST /gc (keep=1) 失敗: %v", err)
	}
	deleted, _ := gcResp2["deleted_snapshots"].([]any)
	if len(deleted) == 0 {
		t.Fatalf("keep_snapshots=1 で GC しても archived が削除されませんでした。レスポンス: %v", gcResp2)
	}
	t.Logf("   GC で %d 件削除 ✓: %v", len(deleted), deleted)

	// 5. archived が 1 件以下になっていることを確認
	snaps, err = listSnapshots(ctx, "mysql")
	if err != nil {
		t.Fatalf("GET /snapshots 失敗: %v", err)
	}
	archivedAfter := 0
	for _, s := range snaps {
		name, _ := s["name"].(string)
		role, _ := s["role"].(string)
		if strings.HasPrefix(name, snapName+"-") && role == "archived" {
			archivedAfter++
		}
	}
	if archivedAfter > 1 {
		t.Fatalf("GC (keep=1) 後も archived が %d 件残っています", archivedAfter)
	}
	t.Logf("   GC 後 archived %d 件 (keep=1 なので ≤1) ✓", archivedAfter)
}

// TestFullRefreshWorkflow はブラウザの "Full Refresh" ボタン相当を検証する。
// ユースケースB: 定期リフレッシュ時にすべてのブランチ・スナップショットを破棄して
// クリーンな状態に戻し、新しいスナップショットを取得できることを確認する。
func TestFullRefreshWorkflow(t *testing.T) {
	ctx := context.Background()
	snapName := fmt.Sprintf("e2e-refresh-%d", time.Now().Unix())
	branchName := fmt.Sprintf("e2e-refresh-br-%d", time.Now().Unix())
	const dbType = "mysql"

	// テスト後に base スナップショットを必ず復元する
	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		snaps, _ := listSnapshots(cleanCtx, dbType)
		for _, s := range snaps {
			if n, _ := s["name"].(string); n == "base" {
				return
			}
		}
		_, _ = post(cleanCtx, apiURL()+"/snapshots", map[string]any{
			"db_type":   dbType,
			"name":      "base",
			"overwrite": false,
		})
	})

	// 1. テスト用スナップショットとブランチを作成
	t.Log("1. テスト用スナップショットを作成します:", snapName)
	if _, err := post(ctx, apiURL()+"/snapshots", map[string]any{
		"db_type":   dbType,
		"name":      snapName,
		"overwrite": false,
	}); err != nil {
		t.Fatalf("POST /snapshots 失敗: %v", err)
	}

	t.Log("2. ブランチを作成します:", branchName)
	if _, err := post(ctx, apiURL()+"/branches", map[string]any{
		"name":          branchName,
		"snapshot_ref":  snapName,
		"ttl_hours":     1,
		"database_type": dbType,
	}); err != nil {
		t.Fatalf("POST /branches 失敗: %v", err)
	}

	// 3. Full Refresh 実行 (確認モーダル → "実行" ボタン相当)
	t.Log("3. Full Refresh を実行します (POST /snapshots/reset)...")
	resetResp, err := post(ctx, apiURL()+"/snapshots/reset", map[string]any{
		"db_type": dbType,
	})
	if err != nil {
		t.Fatalf("POST /snapshots/reset 失敗: %v", err)
	}
	msg, _ := resetResp["message"].(string)
	if !strings.Contains(msg, "ready for new data") {
		t.Fatalf("リセットメッセージが期待と異なります: %v", msg)
	}
	t.Logf("   リセット完了: %v ✓", msg)

	// 4. スナップショットが空になることを確認
	t.Log("4. スナップショットが空になることを確認します...")
	snaps, err := listSnapshots(ctx, dbType)
	if err != nil {
		t.Fatalf("GET /snapshots 失敗: %v", err)
	}
	if len(snaps) != 0 {
		names := make([]string, len(snaps))
		for i, s := range snaps {
			names[i], _ = s["name"].(string)
		}
		t.Fatalf("リセット後もスナップショットが残っています: %v", names)
	}
	t.Log("   スナップショット 0件 ✓")

	// 5. ブランチが削除されていることを確認
	t.Log("5. ブランチが削除されていることを確認します...")
	err = waitFor(ctx, 60*time.Second, "branch deleted after reset", func() (bool, error) {
		_, err := get(ctx, branchURL(branchName))
		return err != nil, nil
	})
	if err != nil {
		t.Fatalf("Full Refresh 後もブランチ %s が残っています: %v", branchName, err)
	}
	t.Log("   ブランチ削除確認 ✓")

	// 6. 新しいスナップショット（base）を作成できることを確認
	//    (Full Refresh 後の「Take Snapshot でリストア」UI フロー相当)
	t.Log("6. リセット後に新しいスナップショット (base) を作成します...")
	if _, err := post(ctx, apiURL()+"/snapshots", map[string]any{
		"db_type":   dbType,
		"name":      "base",
		"overwrite": false,
	}); err != nil {
		t.Fatalf("リセット後の POST /snapshots 失敗: %v", err)
	}
	snaps, err = listSnapshots(ctx, dbType)
	if err != nil {
		t.Fatalf("GET /snapshots 失敗: %v", err)
	}
	base := findSnapshot(snaps, "base")
	if base == nil {
		t.Fatal("base スナップショットの再作成に失敗しました")
	}
	if role, _ := base["role"].(string); role != "current" {
		t.Fatalf("base の role=%v, want current", role)
	}
	t.Logf("   base スナップショット復元完了: role=%v ✓", base["role"])
}

// TestBranchCreateRequiresSnapshotRef はブランチ作成 UI でのバリデーションを検証する。
// 存在しないスナップショット名を指定した場合に 4xx が返ることを確認する。
func TestBranchCreateRequiresSnapshotRef(t *testing.T) {
	ctx := context.Background()
	branchName := fmt.Sprintf("e2e-invalid-%d", time.Now().Unix())

	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = del(cleanCtx, branchURL(branchName))
	})

	t.Log("存在しないスナップショット名でブランチ作成を試みます...")
	_, err := post(ctx, apiURL()+"/branches", map[string]any{
		"name":          branchName,
		"snapshot_ref":  "nonexistent-snapshot-e2e",
		"ttl_hours":     1,
		"database_type": "mysql",
	})
	if err == nil {
		t.Fatal("存在しないスナップショットでもブランチが作成されてしまいました")
	}
	t.Logf("   期待通りエラーが返りました: %v ✓", err)
}
