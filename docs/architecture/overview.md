# システムアーキテクチャ概要

## コンポーネント構成

```
┌─────────────────────────────────────────────────────────────┐
│  クライアント                                                │
│  CI/CD (GitHub Actions 等) / branchctl / ブラウザ           │
└──────────────┬──────────────────────────────────────────────┘
               │ HTTP :8080
               ▼
┌─────────────────────────────────────────────────────────────┐
│  branchdb（API サーバー）           Kubernetes Cluster      │
│                                                             │
│  REST API + Web コンソール (SPA)                            │
│  DatabaseBranch CR の CRUD ← → K8s API サーバー            │
└──────────────┬──────────────────────────────────────────────┘
               │ watch / CRUD
               ▼
┌─────────────────────────────────────────────────────────────┐
│  K8s API サーバー                                           │
│  DatabaseBranch CRD を保持                                  │
└──────────────┬──────────────────────────────────────────────┘
               │ watch
               ▼
┌─────────────────────────────────────────────────────────────┐
│  Operator (controller-runtime)                              │
│                                                             │
│  Reconciler                                                 │
│    ├─ VolumeProvider ──────────────→ ZFS Agent (HTTP)      │
│    │    └─ ZFSAgentProvider              ↓                 │
│    │                               zfs clone / snapshot    │
│    └─ BranchMySQLProvider                                   │
│         └─ K8sMySQLProvider ──→ Pod + PVC + NodePort Svc   │
└─────────────────────────────────────────────────────────────┘
               │ NodePort
               ▼
┌─────────────────────────────────────────────────────────────┐
│  ブランチ MySQL（各ブランチに 1 Pod）                        │
│  mysql:8.0 + NFS マウント (ZFS clone)                       │
└─────────────────────────────────────────────────────────────┘
```

---

## 各コンポーネントの責務

### branchdb（API サーバー）

- `cmd/branchdb/main.go`
- ユーザーの HTTP リクエストを `DatabaseBranch` CR の CRUD に変換する
- ZFS Agent に接続してスナップショット操作を行う（`ZFSDB_ZFSAGENT_URL` が設定されている場合）
- Web コンソール（SPA）を `/` で配信する
- WebSocket で CR の変更をリアルタイムにブロードキャストする

### Operator

- `cmd/operator/main.go`
- `DatabaseBranch` CR を watch し、実際のリソース（Volume・Pod・Service）を管理する
- controller-runtime を使用し、宣言的な状態管理（Reconcile ループ）で動作する
- **VolumeProvider** と **BranchMySQLProvider** の 2 つのインターフェースを介して実装を差し替え可能

### ZFS Agent

- `cmd/zfsagent/main.go`
- ZFS ストレージサーバー上で動作する軽量 HTTP サーバー
- Operator からの HTTP リクエストを受け、`zfs` コマンドを実行する
- 認証はシンプルな Bearer トークン方式

---

## データフロー：ブランチ作成

```
1. POST /branches {"name":"feat-x","snapshot_ref":"snap-1","ttl_hours":24}
       ↓ API サーバー
2. DatabaseBranch CR を作成
       ↓ K8s API サーバー (etcd に保存)
       ↓ Operator が watch
3. Reconciler 起動
   a. finalizer を追加
   b. status.phase = Creating
   c. VolumeProvider.CreateClone("snap-1", "feat-x")
         → ZFS Agent: POST /clones {"snapshot":"tank/mysql@snap-1","clone":"tank/mysql/branches/feat-x"}
         → ZFS サーバー: zfs clone tank/mysql@snap-1 tank/mysql/branches/feat-x
         ← VolumeInfo{NFSServer, NFSPath}
   d. BranchMySQLProvider.Start("feat-x", volumeInfo)
         → PersistentVolume (NFS) 作成
         → PersistentVolumeClaim 作成
         → Pod 作成 (mysql:8.0 + fix-permissions initContainer)
         → NodePort Service 作成
         → K8s が NodePort を自動割当
   e. status.externalPort = <NodePort>
   f. status.phase = Ready
       ↓
4. GET /branches/feat-x → {status:"ready", port:31234, dsn:"root@tcp(...:31234)/"}
```

---

## インターフェース設計（Pro 拡張ポイント）

```go
// internal/domain/volume.go
type VolumeProvider interface {
    TakeSnapshot(ctx, name) error
    CreateClone(ctx, snapshot, clone) (VolumeInfo, error)
    DeleteClone(ctx, clone) error
    ListSnapshots(ctx) ([]SnapshotInfo, error)
}

// internal/domain/branchmysql.go
type BranchMySQLProvider interface {
    Start(ctx, branch, volumeInfo) (BranchEndpoint, error)
    Stop(ctx, branch) error
}
```

将来の Pro 機能（OIDC 認証、マルチテナント、クォータ管理）は、これらのインターフェースまたは HTTP ミドルウェアとして注入します。ビルドタグは使いません。

---

## 関連ドキュメント

- [DatabaseBranch CRD リファレンス](crd-spec.md)
- [Operator ライフサイクル](operator-lifecycle.md)
