# DatabaseBranch CRD リファレンス

## 概要

`DatabaseBranch` は BranchDB が管理するデータベースブランチを表す Kubernetes カスタムリソースです。

```
apiVersion: branchdb.io/v1alpha1
kind: DatabaseBranch
```

- **スコープ:** Cluster-scoped（namespace なし）
- **グループ:** `branchdb.io`
- **バージョン:** `v1alpha1`

---

## spec

| フィールド | 型 | 必須 | 説明 |
|---|---|---|---|
| `snapshotRef` | string | ✅ | ベースとなるスナップショット名（ZFS Agent が管理するスナップショットの名前）|
| `ttlHours` | integer | | ブランチの有効期間（時間）。`0` または省略で無期限。|
| `initSQL` | string | | ブランチ MySQL 起動後に実行する初期化 SQL（将来対応予定）|

---

## status

| フィールド | 型 | 説明 |
|---|---|---|
| `phase` | string | 現在のフェーズ（後述）|
| `clusterHost` | string | クラスター内部の DNS 名（例: `feat-x.branchdb.svc.cluster.local`）|
| `clusterPort` | integer | クラスター内部ポート（常に `3306`）|
| `externalHost` | string | 外部接続用ホスト名（`externalHost` values の値）|
| `externalPort` | integer | K8s が割り当てた NodePort（`30000`–`32767`）|
| `message` | string | エラーフェーズ時の詳細メッセージ |
| `expiresAt` | datetime | TTL に基づく有効期限（`ttlHours > 0` のときのみ設定）|

### phase の値

| phase | 意味 |
|---|---|
| `Pending` | CR 作成直後、Operator が処理を開始する前 |
| `Creating` | Volume クローンと MySQL Pod の作成中 |
| `Ready` | MySQL が起動済みで接続可能 |
| `Error` | 作成・削除処理でエラーが発生した |
| `Deleting` | CR 削除中（finalizer が処理している）|

---

## サンプルマニフェスト

### 基本

```yaml
apiVersion: branchdb.io/v1alpha1
kind: DatabaseBranch
metadata:
  name: feature-payment-v2
spec:
  snapshotRef: snap-20260101
  ttlHours: 24
```

### TTL なし（手動削除まで保持）

```yaml
apiVersion: branchdb.io/v1alpha1
kind: DatabaseBranch
metadata:
  name: staging-persistent
spec:
  snapshotRef: snap-20260101
  ttlHours: 0
```

---

## kubectl コマンド

```bash
# 一覧表示（Phase と ExternalPort が表示される）
kubectl get databasebranches

# 詳細表示
kubectl describe databasebranch feature-payment-v2

# status の確認
kubectl get databasebranch feature-payment-v2 -o jsonpath='{.status}'

# DSN の取得
kubectl get databasebranch feature-payment-v2 \
  -o jsonpath='root@tcp({.status.externalHost}:{.status.externalPort})/'

# 削除
kubectl delete databasebranch feature-payment-v2
```

---

## finalizer

BranchDB は `branchdb.io/finalizer` というファイナライザーを CR に付与します。  
CR の削除リクエストが来ると、Operator は以下の順序でクリーンアップを実行した後にファイナライザーを除去します：

1. `status.phase = Deleting`
2. `BranchMySQLProvider.Stop()` — Pod/PVC/Service を削除
3. `VolumeProvider.DeleteClone()` — ZFS クローンを削除
4. ファイナライザーを除去 → CR が etcd から消える

ファイナライザーが残った状態で Operator が停止した場合、再起動後に Reconcile が再開されます。
