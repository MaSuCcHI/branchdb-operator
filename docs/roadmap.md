# ロードマップ

---

## 実装済み ✅

### MySQL ブランチ
ZFS クローン + NFS PV による MySQL 8.0 ブランチの作成・削除・TTL 自動削除。

### PostgreSQL ブランチ
`database_type: postgres` を指定することで PostgreSQL 16 ブランチを作成可能。  
`fsync=off / synchronous_commit=off / full_page_writes=off` で NFS 上でもパフォーマンスを維持。

### マルチデータセット ZFS Agent
`ZFSAGENT_DATASETS=mysql:tank/mysql,postgres:tank/postgres` で1プロセスが複数 dataset を管理。  
`?db_type=` クエリパラメータで操作対象データセットを切り替える。

### Web コンソール
ブランチ一覧・作成・削除・Pod ステータス・メトリクスを操作できる SPA（React + TypeScript）。

---

## 部分実装 🚧

### Redis ブランチ
`dbConfig` の定義は完了済み（`redis:7`、`redis-cli ping` による readiness チェック）。  
ZFS Agent のマルチデータセット設定と E2E テストが未対応。

---

## 計画中 📋

### 認証・認可

**現状:** API サーバーに認証機能はありません。  
ネットワークレベルの制限（K8s NetworkPolicy、Ingress の IP 制限など）で保護してください。

**計画:**
- OIDC / JWT 認証ミドルウェアをインターフェース注入で追加
- GitHub OIDC との統合（GitHub Actions からトークン不要でアクセス）
- `K8sBranchHandler` に `WithAuthMiddleware(...)` を追加する形で実装

---

### マルチテナント

**現状:** 全ユーザーが同一 namespace のブランチを作成できます。

**計画:**
- ユーザー/チーム単位のブランチ分離（namespace または label ベース）
- `DatabaseBranch` CR にオーナー情報を付与

---

### クォータ管理

**現状:** ブランチ数に制限はありません。

**計画:**
- ユーザー/チームごとの最大ブランチ数を設定できる ConfigMap/CRD
- Reconcile ループでクォータチェックを行い、超過時は `Error` フェーズにする

```yaml
# 将来の設定例（未実装）
apiVersion: branchdb.io/v1alpha1
kind: BranchQuota
metadata:
  name: team-payments
spec:
  maxBranches: 10
  selector:
    matchLabels:
      team: payments
```

---

### AWS FSx 対応

**現状:** ZFS Agent（ZFS + NFS サーバー）のみサポートしています。

**計画:**
- `VolumeProvider` インターフェースの FSx 実装（`infrastructure/fsx/`）
- FSx API で ZFS スナップショットをクローンし、EFS/NFS マウントで Pod に提供
- EKS + IRSA での認証フロー

---

### Helm リポジトリ

**現状:** Helm チャートはリポジトリ内のローカルパスからのみインストールできます。

**計画:**
- GitHub Pages で Helm リポジトリを公開
- `helm repo add branchdb https://masucci.github.io/branchdb-operator`
- GitHub Actions でのコンテナイメージ自動ビルド・GHCR へのプッシュ

---

### branchctl CLI

**計画:**
- `branchctl create --wait` でブランチ作成 + 接続待ちをワンコマンドで実行
- `branchctl status <branch>` で Pod 状態・接続数を表示
- `~/.config/branchctl/config.yaml` で接続先を管理
