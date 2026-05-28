# Operator ライフサイクル

## 状態遷移図

```
                    CR 作成
                      │
                      ▼
                  [Pending]
                      │ Reconcile 開始
                      ▼
              finalizer 追加
                      │
                      ▼
                  [Creating]
                 /          \
          成功 /              \ 失敗
              ▼                ▼
           [Ready]           [Error]
              │                │ 再 Reconcile（自動リトライ）
              │ TTL 期限切れ    │
              ▼                │
       CR 削除リクエスト ←─────┘
              │
              ▼
           [Deleting]
              │ cleanup 完了
              ▼
        CR 削除（etcd）
```

---

## Reconcile ループの詳細

`internal/interface/operator/reconciler.go` の `Reconcile()` メソッドが実装しています。

### 1. CR の取得

```go
if err := r.Get(ctx, req.NamespacedName, branch); err != nil {
    return ctrl.Result{}, client.IgnoreNotFound(err)
}
```

CR が存在しない場合（削除済み）は正常終了。

### 2. 削除処理（DeletionTimestamp が設定されている場合）

`handleDeletion()` を呼び出す:

1. `status.phase = Deleting`
2. `MySQLProvider.Stop()` — Pod/PVC/Service/ConfigMap を削除
3. `VolumeProvider.DeleteClone()` — ZFS クローンを削除
4. finalizer を除去 → CR が etcd から消える

いずれかのステップで失敗した場合は `status.phase = Error` にセットしてエラーを返す。  
controller-runtime が exponential backoff でリトライする。

### 3. TTL チェック

```go
if branch.Status.ExpiresAt != nil && branch.Status.Phase == Ready {
    if time.Now().After(branch.Status.ExpiresAt.Time) {
        r.Delete(ctx, branch)   // → DeletionTimestamp がセットされる
    }
}
```

TTL が切れた Ready ブランチは自動削除される。

### 4. finalizer の追加

CR に `branchdb.io/finalizer` がない場合は追加してから再取得（リソースバージョンの更新のため）。

### 5. Ready チェック（skip）

`status.phase == Ready` の場合は `RequeueAfter: 10 minutes` で TTL チェックをスケジュールして終了。

### 6. 作成フロー

新規 CR（または Error からのリカバリー）の場合：

```
status.phase = Creating
    ↓
VolumeProvider.CreateClone(snapshotRef, branchName)
    ↓ 失敗 → status.phase = Error, return error
    ↓ 成功 → VolumeInfo{NFSServer, NFSPath}
    ↓
MySQLProvider.Start(branchName, volumeInfo)
    ↓ 失敗 → status.phase = Error, return error
    ↓ 成功 → BranchEndpoint{Host, Port, ExternalPort}
    ↓
status.clusterHost = endpoint.Host
status.clusterPort = endpoint.Port
status.externalHost = r.ExternalHost
status.externalPort = endpoint.ExternalPort
status.expiresAt = now + ttlHours
status.phase = Ready
```

---

## リトライ戦略

controller-runtime は Reconcile がエラーを返すと exponential backoff でリトライします：

- 初回: 即時
- 2 回目: 1 秒後
- 3 回目: 2 秒後
- 以降: 最大 10 分まで倍増

`status.phase = Error` にはなりますが、Operator が自動的にリトライするため、  
一時的なネットワークエラー等は自然に回復します。

---

## リーダー選出

`--leader-elect=true`（デフォルト）の場合、複数の Operator Pod が起動していても  
1 つだけがアクティブに動作します。Lease リソース（`branchdb.io`）で管理されます。

```bash
# リーダーの確認
kubectl get lease branchdb.io -n branchdb-system -o yaml
```
