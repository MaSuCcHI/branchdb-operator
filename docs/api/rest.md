# REST API リファレンス

ベース URL: `http://<api-server>:8080`

すべてのレスポンスは JSON。エラーは `{"error": "<message>"}` 形式。

---

## ブランチ操作

### POST /branches — ブランチ作成

```
POST /branches
Content-Type: application/json
```

**リクエスト**

| フィールド | 型 | 必須 | 説明 |
|---|---|---|---|
| `name` | string | ✅ | ブランチ名（`[a-z0-9-]` のみ使用可）|
| `snapshot_ref` | string | ✅ | ベーススナップショット名 |
| `ttl_hours` | integer | | 有効期間（時間）。`0` または省略で無期限。|

**レスポンス: 202 Accepted**

```json
{
  "name":         "feature-payment",
  "status":       "creating",
  "host":         "192.168.1.100",
  "port":         31234,
  "dsn":          "root@tcp(192.168.1.100:31234)/",
  "cluster_host": "",
  "cluster_port": 0,
  "snapshot_ref": "snap-20260101",
  "ttl_hours":    24,
  "created_at":   "2026-05-28T10:00:00Z",
  "expires_at":   "2026-05-29T10:00:00Z"
}
```

`port: 0` の間は NodePort がまだ割り当てられていません。  
API サーバーは CR 作成後に最大 5 秒ポーリングして NodePort が確定した段階でレスポンスを返します。

**エラー**

| コード | 条件 |
|---|---|
| `400 Bad Request` | リクエストボディが不正、または `name` が空 |
| `500 Internal Server Error` | K8s API への接続エラー |

---

### GET /branches — ブランチ一覧

```
GET /branches
```

**レスポンス: 200 OK** — BranchResponse の配列

```json
[
  {
    "name":         "feature-payment",
    "status":       "ready",
    "host":         "192.168.1.100",
    "port":         31234,
    "dsn":          "root@tcp(192.168.1.100:31234)/",
    "cluster_host": "feature-payment.branchdb-system.svc.cluster.local",
    "cluster_port": 3306,
    "snapshot_ref": "snap-20260101",
    "ttl_hours":    24,
    "created_at":   "2026-05-28T10:00:00Z",
    "expires_at":   "2026-05-29T10:00:00Z"
  }
]
```

---

### GET /branches/{name} — ブランチ状態取得

```
GET /branches/{name}
```

**レスポンス: 200 OK**

BranchResponse（上記と同じ構造）

| フィールド | 説明 |
|---|---|
| `status` | `pending` / `creating` / `ready` / `error` / `deleting` |
| `host` | 外部接続用ホスト名（`externalHost` の値）|
| `port` | NodePort（0 の場合はまだ未割当）|
| `dsn` | `root@tcp(<host>:<port>)/`（port > 0 のときのみ設定）|
| `cluster_host` | クラスター内部 DNS（同一クラスター内の Pod から接続する場合）|
| `cluster_port` | クラスター内部ポート（常に `3306`）|
| `message` | `status: error` のとき詳細メッセージが入る |

**エラー**

| コード | 条件 |
|---|---|
| `404 Not Found` | ブランチが存在しない |
| `500 Internal Server Error` | K8s API エラー（Not Found 以外）|

---

### DELETE /branches/{name} — ブランチ削除

```
DELETE /branches/{name}
```

**レスポンス: 204 No Content**

`DatabaseBranch` CR に削除マークを付けます。実際のリソース（Pod/PVC/ZFS クローン）は  
Operator が非同期でクリーンアップします。

**エラー**

| コード | 条件 |
|---|---|
| `404 Not Found` | ブランチが存在しない |

---

## ブランチ詳細情報

### GET /branches/{name}/pod — Pod 状態取得

```
GET /branches/{name}/pod
```

MySQL Pod（`branchdb-mysql-<name>`）の状態を返します。

**レスポンス: 200 OK**

```json
{
  "phase":   "Running",
  "ready":   true,
  "message": ""
}
```

| フィールド | 説明 |
|---|---|
| `phase` | Pod の phase（`Pending` / `Running` / `Succeeded` / `Failed` / `Unknown`）|
| `ready` | Pod の Ready 条件が True かどうか |
| `message` | エラー時の詳細メッセージ |

**エラー**

| コード | 条件 |
|---|---|
| `503 Service Unavailable` | Pod が存在しない |

---

### GET /branches/{name}/metrics — MySQL メトリクス取得

```
GET /branches/{name}/metrics
```

ブランチ MySQL に直接接続して `Threads_connected` を取得します。

**レスポンス: 200 OK**（MySQL 接続成功時）

```json
{
  "threads_connected": 3,
  "available":         true
}
```

**レスポンス: 200 OK**（MySQL 接続失敗時）

```json
{
  "threads_connected": 0,
  "available":         false,
  "error":             "dial tcp 192.168.1.100:3306: connect: connection refused"
}
```

> **注意:** HTTP ステータスは常に `200` です。`available: false` でエラーを判定してください。

---

## 統計

### GET /stats — フェーズ別集計

```
GET /stats
```

**レスポンス: 200 OK**

```json
{
  "total":    10,
  "ready":    7,
  "creating": 2,
  "error":    1,
  "pending":  0,
  "deleting": 0
}
```

---

## スナップショット

> `ZFSDB_ZFSAGENT_URL` が設定されている場合のみ有効。  
> 未設定時は `501 Not Implemented` を返します。

### GET /snapshots — スナップショット一覧

```
GET /snapshots
```

**レスポンス: 200 OK**

```json
[
  {
    "name":       "snap-20260101",
    "created_at": "2026-01-01T00:00:00Z"
  }
]
```

---

### POST /snapshots — スナップショット即時取得

```
POST /snapshots
```

**レスポンス: 201 Created**

```json
{
  "name":       "snap-20260528T120000",
  "created_at": "2026-05-28T12:00:00Z"
}
```

---

## ヘルスチェック

### GET /health

```
GET /health
```

**レスポンス: 200 OK**

```json
{"status": "ok"}
```

---

## WebSocket

### GET /ws — リアルタイムイベントストリーム

WebSocket 接続でブランチの状態変化をリアルタイムに受信できます。

```javascript
const ws = new WebSocket('ws://<api-server>:8080/ws');
ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  console.log(msg.type, msg.data);
};
```

**イベント形式**

```json
{
  "type": "branch_updated",
  "data": {
    "name":   "feature-payment",
    "status": "ready",
    "port":   31234
  }
}
```

| type | タイミング |
|---|---|
| `branch_updated` | ブランチの status が変化したとき（Creating → Ready など）|
| `branch_deleted` | ブランチが削除されたとき |

---

## Web コンソール

### GET /

ブラウザ向けの管理 UI（SPA）を返します。  
上記 API と同じホスト・ポートでアクセスできます。

```
http://<api-server>:8080/
```
