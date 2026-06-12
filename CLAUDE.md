# zfs-db-k8s Development Guide

このファイルは [Claude Code](https://claude.ai/code) が本リポジトリで作業する際に参照する開発ガイドです。

## 開発プロセス: t-wada式TDD

実装は Red → Green → Refactor のサイクルを厳守する。

1. **Red** — 失敗するテストを1つだけ書く
2. **Green** — そのテストを通過させる最小限のコードを書く
3. **Refactor** — テストがグリーンのまま重複・命名・構造を整理する

**カバレッジは常に95%以上を維持する。** `go test ./internal/... -cover` で確認し、95%を下回るパッケージがあればテストを追加してから次に進む。

---

## アーキテクチャ: クリーンアーキテクチャ

依存の方向は外側から内側（インフラ → インターフェース → ドメイン）。

```
domain/          # VolumeProvider / BranchDatabaseProvider インターフェース。外部依存ゼロ。
infrastructure/  # k8sdatabase / zfsagent / zfs の実装。
interface/       # operator (Reconciler) / api (REST + SPA) の入出力アダプタ。
```

---

## コマンドリファレンス

```bash
# テスト
go test ./internal/... -count=1          # ユニットテスト全体
go test ./internal/... -cover            # カバレッジ確認
make test                                # 全体確認

# ビルド
make build                               # バイナリ → bin/

# SPA コンソール
make web-build                           # SPA を internal/interface/api/k8s-dist/ にビルド
make web-dev                             # 開発サーバー起動 (hot reload, :5173)

# CRD 生成
make manifests                           # deploy/k8s/crd/ の YAML を再生成
```

---

## OSS / Pro の境界

- **OSS**: Operator、REST API、SPA コンソール、ZFS Agent
- **Pro**: OIDC 認証・マルチテナント・クォータ管理

Pro 機能はインターフェース注入で追加する。`cmd/` のエントリポイントで Pro プラグインを差し込む。ビルドタグは使わない。

```
interface/
  api/
    k8s_branch_handler.go  ← AuthMiddleware interface を受け付ける (将来)
  operator/
    reconciler.go          ← QuotaEnforcer interface を受け付ける (将来)
```

---

## 環境変数

| 変数 | デフォルト | 説明 |
|---|---|---|
| `ZFSDB_LISTEN_ADDR` | `:8080` | API サーバーのリッスンアドレス |
| `ZFSDB_EXTERNAL_HOST` | `localhost` | NodePort の外部ホスト名 |
| `ZFSDB_NAMESPACE` | `default` | DatabaseBranch CR の名前空間 |
| `ZFSDB_ZFSAGENT_URL` | *(未設定)* | ZFS Agent URL（設定時のみスナップショット API 有効） |
| `ZFSDB_ZFSAGENT_TOKEN` | *(未設定)* | ZFS Agent 認証トークン |
| `ZFSDB_API_TOKEN` | *(未設定)* | branchdb API の静的 Bearer トークン（設定時のみ認証有効、未設定は後方互換の無認証） |
| `ZFSDB_BRANCH_AUTH` | *(未設定)* | `generated` を設定するとブランチごとにランダムパスワードを生成して Secret に保存する。デフォルトは無認証。 |
| `ZFSAGENT_ADDR` | `:9090` | ZFS Agent のリッスンアドレス |
| `ZFSAGENT_TOKEN` | *(必須)* | ZFS Agent 認証トークン |
| `ZFSAGENT_POOL` | `tank` | ZFS pool 名（シングルデータセット時） |
| `ZFSAGENT_DATASET` | `mysql` | ZFS dataset 名（シングルデータセット時） |
| `ZFSAGENT_DATASETS` | *(未設定)* | マルチデータセット設定 例: `mysql:tank/mysql,postgres:tank/postgres` |
