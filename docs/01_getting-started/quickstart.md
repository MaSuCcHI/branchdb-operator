# クイックスタート

このガイドでは、BranchDB を Kubernetes クラスターにインストールし、最初のブランチを作成するまでを 5 分で完了します。

## 前提条件

- Kubernetes 1.25 以上（k3s / EKS / GKE / AKS など）
- Helm 3.10 以上
- ZFS サーバー（[ZFS サーバーセットアップ](zfs-server-setup.md) を先に完了してください）

> AWS FSx for OpenZFS を使う場合は ZFS Agent は不要ですが、現バージョンでは ZFS Agent 経由のみサポートしています。

---

## Step 1: Helm でインストール

CRD・Operator・API サーバーを 1 コマンドでインストールします。

```bash
helm upgrade --install branchdb deploy/helm/branchdb \
  --namespace branchdb-system \
  --create-namespace \
  --set installCRDs=true \
  --set zfsAgent.url=http://<zfs-server-ip>:9090 \
  --set zfsAgent.token=<your-token> \
  --set externalHost=<k8s-node-ip-or-lb>
```

| パラメータ | 説明 |
|---|---|
| `zfsAgent.url` | ZFS Agent の URL（必須）|
| `zfsAgent.token` | ZFS Agent の認証トークン（必須）|
| `externalHost` | NodePort 経由でブランチ MySQL に接続するホスト名 / IP |

インストールの確認：

```bash
kubectl -n branchdb-system get pods
# NAME                                READY   STATUS    RESTARTS
# branchdb-xxxxxx-xxxxx               1/1     Running   0    ← Operator
# branchdb-api-xxxxxx-xxxxx           1/1     Running   0    ← API サーバー
```

---

## Step 2: API サーバーへのアクセス確認

デフォルトでは `ClusterIP` Service が作成されます。ポートフォワードでアクセスします。

```bash
kubectl -n branchdb-system port-forward svc/branchdb-api 8080:8080 &

curl http://localhost:8080/health
# {"status":"ok"}
```

外部から直接アクセスしたい場合は [Ingress での外部公開](../deploy/ingress.md) を参照してください。

---

## Step 3: スナップショットの確認

ブランチのベースとなるスナップショットが存在するか確認します。

```bash
curl http://localhost:8080/snapshots
# [{"name":"snap-20260101","created_at":"2026-01-01T00:00:00Z"}]
```

スナップショットがない場合は即時取得できます：

```bash
curl -X POST http://localhost:8080/snapshots
```

---

## Step 4: ブランチを作成する

```bash
curl -X POST http://localhost:8080/branches \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "feature-payment",
    "snapshot_ref": "snap-20260101",
    "ttl_hours": 24
  }'
```

レスポンス例（202 Accepted）:

```json
{
  "name": "feature-payment",
  "status": "creating",
  "host": "<node-ip>",
  "port": 0,
  "created_at": "2026-05-28T10:00:00Z",
  "expires_at": "2026-05-29T10:00:00Z"
}
```

`port: 0` の間は MySQL が起動中です。ポートが確定するまでポーリングします：

```bash
until curl -s http://localhost:8080/branches/feature-payment | grep -v '"port":0'; do
  sleep 3
done
```

---

## Step 5: MySQL に接続する

```bash
# DSN の取得
DSN=$(curl -s http://localhost:8080/branches/feature-payment | jq -r .dsn)
echo $DSN
# root@tcp(<node-ip>:31234)/

# 接続確認
mysql -u root -h <node-ip> -P 31234
```

---

## Step 6: ブランチを削除する

```bash
curl -X DELETE http://localhost:8080/branches/feature-payment
# 204 No Content
```

TTL を設定した場合は `expires_at` に自動削除されます。

---

## Web コンソール

API サーバーの `/` にアクセスするとブラウザで管理コンソールが使えます。

```bash
open http://localhost:8080/
```

---

## 次のステップ

- [Helm チャートリファレンス](../deploy/helm.md) — 全パラメータの詳細
- [REST API リファレンス](../api/rest.md) — API の完全仕様
- [Ingress での外部公開](../deploy/ingress.md) — 本番環境向けセットアップ
