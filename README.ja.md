# zfs-db-k8s

[![CI](https://github.com/MaSuCcHI/branchdb-operator/actions/workflows/ci.yml/badge.svg)](https://github.com/MaSuCcHI/branchdb-operator/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26-00ADD8.svg)](go.mod)

**プルリクエストごとに、数秒で専用データベースブランチを作る Kubernetes Operator**

`DatabaseBranch` CR を作るだけで、ZFS クローンをベースにした独立した MySQL または PostgreSQL インスタンスが起動します。CI/CD パイプラインは本番と同じデータを持つデータベースをすぐ使えます。シードスクリプト不要、共有フィクスチャ不要。

```
git push origin feature/payment-v2
→ DatabaseBranch CR が作成される
→ Operator が ZFS スナップショットをクローン → MySQL/PostgreSQL Pod を起動
→ NodePort が割り当てられる
→ mysql://branchdb.company.com:31234/ に対してテスト実行
→ PR マージ → CR 削除 → リソース自動クリーンアップ
```

---

## 特徴

- **CR 1つ = DB 1つ** — `DatabaseBranch` CRD が Pod + PVC + NodePort Service に直接対応
- **MySQL・PostgreSQL 対応** — `database_type: mysql` または `database_type: postgres` をブランチごとに指定可能
- **ZFS クローン** — コピーオンライトによる瞬時のブランチ作成
- **TTL 自動削除** — `ttlHours` 経過後にブランチが自動削除。手動クリーンアップ不要
- **Web コンソール** — ブランチ管理・Pod ステータス・メトリクスをブラウザで確認（`/`）
- **REST API** — `POST /branches`、`GET /branches/{name}`、`GET /stats` など
- **WebSocket** — `/ws` でリアルタイムイベントストリーム

---

## クイックスタート

> **まず Mac で試したい方へ**
> [ローカル開発環境ガイド（macOS + Colima）](docs/01_getting-started/local-dev-colima.md) — 実ディスクもクラウドも不要で動作確認できます。

### 前提条件

- Kubernetes クラスター（k3s / EKS / GKE など）
- ZFS サーバー（[ZFS Agent](#zfs-agent) 経由）

### 1. Helm でインストール

CRD・Operator・API サーバーの3つを **1コマンド** でインストールできます:

```bash
helm upgrade --install branchdb deploy/helm/branchdb \
  --namespace branchdb-system --create-namespace \
  --set installCRDs=true \
  --set zfsAgent.url=http://<zfs-server>:9090 \
  --set zfsAgent.token=<token> \
  --set externalHost=<node-ip-or-lb>
```

API サーバーを外部公開する場合（クラスター内で Ingress を使う場合は不要）:

```bash
# クラウド環境（LoadBalancer）
--set apiServer.service.type=LoadBalancer

# オンプレミス / ローカル（NodePort）
--set apiServer.service.type=NodePort
```

### 2. ブランチを作成する

```bash
# MySQL ブランチ（デフォルト）
curl -X POST http://<api-server>:8080/branches \
  -H 'Content-Type: application/json' \
  -d '{"name":"feature-payment","snapshot_ref":"base","ttl_hours":24}'

# PostgreSQL ブランチ
curl -X POST http://<api-server>:8080/branches \
  -H 'Content-Type: application/json' \
  -d '{"name":"feature-payment","snapshot_ref":"base","ttl_hours":24,"database_type":"postgres"}'
```

---

## しくみ

```
[CI/CD / branchctl]
        │ HTTP :8080
        ▼
┌─────────────────────────┐
│   API サーバー           │  REST API + Web コンソール
│   (cmd/branchdb)         │  DatabaseBranch CR の CRUD
└──────────┬──────────────┘
           │ watch / CRUD
           ▼
┌─────────────────────────┐
│   K8s API サーバー       │
└──────────┬──────────────┘
           │ watch
           ▼
┌─────────────────────────┐
│   Operator               │  controller-runtime Reconciler
│   (cmd/operator)         │
│   ├─ VolumeProvider      │──→ ZFS Agent
│   └─ DatabaseProvider    │──→ Pod + PVC + NodePort Service
└─────────────────────────┘
```

### ZFS Agent

ZFS ストレージサーバー上で動く軽量な HTTP サーバー（`cmd/zfsagent`）。Operator から HTTP 経由で呼び出され、スナップショット・クローン操作を実行します。

```bash
# MySQL のみ（シングルデータセット）
ZFSAGENT_TOKEN=secret ZFSAGENT_POOL=tank ZFSAGENT_DATASET=mysql \
  ./bin/zfsagent

# MySQL + PostgreSQL（マルチデータセット）
ZFSAGENT_TOKEN=secret \
  ZFSAGENT_DATASETS=mysql:tank/mysql,postgres:tank/postgres \
  ./bin/zfsagent
```

---

## REST API

| メソッド | パス | 説明 |
|---------|------|------|
| `POST` | `/branches` | ブランチ作成（即座に 202 を返す） |
| `GET` | `/branches` | ブランチ一覧 |
| `GET` | `/branches/{name}` | ブランチ状態・DSN 取得 |
| `DELETE` | `/branches/{name}` | ブランチ削除 |
| `GET` | `/branches/{name}/pod` | Pod フェーズ・Ready 状態 |
| `GET` | `/branches/{name}/metrics` | MySQL 接続数（`Threads_connected`） |
| `GET` | `/stats` | フェーズ別カウント（total/ready/creating/error） |
| `GET` | `/snapshots?db_type=<type>` | スナップショット一覧 |
| `POST` | `/snapshots` | スナップショット即時取得 |
| `GET` | `/health` | ヘルスチェック |
| `GET` | `/` | Web コンソール（SPA） |

### レスポンス例（GET /branches/{name}）

```json
{
  "name":          "feature-payment",
  "status":        "ready",
  "database_type": "mysql",
  "host":          "branchdb.company.com",
  "port":          31234,
  "dsn":           "root@tcp(branchdb.company.com:31234)/",
  "cluster_host":  "feature-payment.branchdb.svc.cluster.local",
  "cluster_port":  3306,
  "snapshot_ref":  "base",
  "ttl_hours":     24,
  "created_at":    "2026-05-28T10:00:00Z",
  "expires_at":    "2026-05-29T10:00:00Z"
}
```

---

## GitHub Actions との連携例

```yaml
jobs:
  test:
    steps:
      - name: Create DB branch
        id: db
        run: |
          RESP=$(curl -sf -X POST https://${{ secrets.BRANCHDB_HOST }}/branches \
            -H 'Content-Type: application/json' \
            -d '{"name":"pr-${{ github.event.number }}","ttl_hours":2}')
          echo "dsn=$(echo $RESP | jq -r .dsn)" >> $GITHUB_OUTPUT

      - name: Run tests
        env:
          DATABASE_URL: ${{ steps.db.outputs.dsn }}
        run: go test ./...

      - name: Delete DB branch
        if: always()
        run: |
          curl -sf -X DELETE \
            https://${{ secrets.BRANCHDB_HOST }}/branches/pr-${{ github.event.number }}
```

---

## 開発

```bash
# テスト実行
make test

# カバレッジ確認（95% 以上を維持）
make cover

# バイナリビルド → bin/
make build

# Web コンソールビルド → internal/interface/api/k8s-dist/
make web-build

# 開発サーバー起動（hot reload, :5173）
make web-dev

# CRD マニフェスト再生成
make manifests
```

### 環境変数

| 変数 | デフォルト | 説明 |
|------|-----------|------|
| `ZFSDB_LISTEN_ADDR` | `:8080` | API サーバーのリッスンアドレス |
| `ZFSDB_EXTERNAL_HOST` | `localhost` | NodePort の外部ホスト名 / IP |
| `ZFSDB_NAMESPACE` | `default` | DatabaseBranch CR の名前空間 |
| `ZFSDB_ZFSAGENT_URL` | *(未設定)* | ZFS Agent URL（設定時のみスナップショット API 有効） |
| `ZFSDB_ZFSAGENT_TOKEN` | *(未設定)* | ZFS Agent 認証トークン |
| `ZFSDB_API_TOKEN` | *(未設定)* | branchdb REST API の静的 Bearer トークン。設定すると `/health` 以外の全 API ルートで `Authorization: Bearer <token>` が必要になる。未設定は後方互換の無認証。 |
| `ZFSDB_BRANCH_AUTH` | *(未設定)* | `generated` を設定すると Operator がブランチごとにランダムパスワードを生成し `branchdb-cred-<branch>` Secret に保存する。API レスポンスの DSN にパスワードが含まれる。デフォルトは無認証（`MYSQL_ALLOW_EMPTY_PASSWORD`、PostgreSQL は `trust`）。 |
| `ZFSAGENT_ADDR` | `:9090` | ZFS Agent のリッスンアドレス |
| `ZFSAGENT_TOKEN` | *(必須)* | ZFS Agent 認証トークン |
| `ZFSAGENT_POOL` | `tank` | ZFS pool 名（シングルデータセット時） |
| `ZFSAGENT_DATASET` | `mysql` | ZFS dataset 名（シングルデータセット時） |
| `ZFSAGENT_DATASETS` | *(未設定)* | マルチデータセット設定 例: `mysql:tank/mysql,postgres:tank/postgres` |

### セキュリティについて

デフォルトではブランチ DB は **NodePort でパスワードなしで外部公開** されます（MySQL は `MYSQL_ALLOW_EMPTY_PASSWORD=yes`、PostgreSQL は `trust` 認証）。開発用途での手軽さを優先した設計ですが、NodePort に到達可能なホストであれば **認証なしで接続できる** ことを意味します。

**共有環境や半パブリックなネットワークでの推奨設定:**

- Operator に `ZFSDB_BRANCH_AUTH=generated` を設定してブランチごとのランダムパスワードを有効化する。パスワードは `branchdb-cred-<branch>` Secret（ownerRef 付き、削除時に自動クリーンアップ）に保存され、API レスポンスの接続 DSN に含まれます。
- branchdb API サーバーに `ZFSDB_API_TOKEN=<強力なランダムトークン>` を設定して REST API を保護する。設定しない場合、API エンドポイントに到達できれば誰でもブランチの作成・削除やスナップショットリセットなどの破壊的操作が可能です。
- NetworkPolicy で NodePort へのアクセスを信頼できる CIDR に制限することを検討する。

---

## ライセンス

MIT — [LICENSE](LICENSE) を参照してください。
