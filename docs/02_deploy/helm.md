# Helm チャートリファレンス

BranchDB の Helm チャート `deploy/helm/branchdb` は、CRD・Operator・API サーバーを 1 チャートで管理します。

## インストール

```bash
helm upgrade --install branchdb deploy/helm/branchdb \
  --namespace branchdb-system \
  --create-namespace \
  --set installCRDs=true \
  --set zfsAgent.url=http://<zfs-server>:9090 \
  --set zfsAgent.token=<token> \
  --set externalHost=<node-ip-or-lb>
```

設定ファイルを使う場合（推奨）：

```bash
helm upgrade --install branchdb deploy/helm/branchdb \
  --namespace branchdb-system \
  --create-namespace \
  -f my-values.yaml
```

---

## values.yaml 全パラメータリファレンス

### CRD

| パラメータ | デフォルト | 説明 |
|---|---|---|
| `installCRDs` | `true` | `true` のとき `helm install`/`upgrade` 両方で CRD を作成・更新する。既存 CRD を保護したい場合は `false` に設定。 |

> **注意:** `installCRDs: false` の場合は CRD を事前に手動インストールしてください。  
> `kubectl apply -f deploy/k8s/crd/`

---

### Operator

| パラメータ | デフォルト | 説明 |
|---|---|---|
| `replicaCount` | `1` | Operator の Pod 数。リーダー選出が有効な場合は複数レプリカも可。|
| `image.repository` | `ghcr.io/masucchi/branchdb-operator` | Operator コンテナイメージ |
| `image.tag` | `""` | イメージタグ（空の場合は `Chart.appVersion`）|
| `image.pullPolicy` | `IfNotPresent` | イメージプルポリシー |
| `imagePullSecrets` | `[]` | プライベートレジストリ用 Secret 名のリスト |
| `nameOverride` | `""` | チャート名のオーバーライド |
| `fullnameOverride` | `""` | リソース名のフル上書き |

---

### BranchDB 共通設定

| パラメータ | デフォルト | 説明 |
|---|---|---|
| `externalHost` | `""` | NodePort 経由でブランチデータベースに接続する外部ホスト名 / IP。Operator と API サーバーの両方で使用する。|
| `branchNamespace` | `""` | ブランチ用 Pod/PVC/Service を作成する namespace。空の場合は Release の namespace を使用。|
| `databases.mysql.image` | `""` | MySQL イメージ上書き（空 = `mysql:8.0`）|
| `databases.postgres.image` | `""` | PostgreSQL イメージ上書き（空 = `postgres:16`）|
| `databases.redis.image` | `""` | Redis イメージ上書き（空 = `redis:7`）|

---

### ZFS Agent

| パラメータ | デフォルト | 説明 |
|---|---|---|
| `zfsAgent.url` | `""` | **(必須)** ZFS Agent のベース URL。例: `http://zfs-server.internal:9090` |
| `zfsAgent.token` | `""` | ZFS Agent の Bearer トークン。指定すると Secret が自動作成される。|
| `zfsAgent.existingSecret` | `""` | 既存の Secret を使用する場合の Secret 名（key: `zfsagent-token`）。`token` より優先される。|

`existingSecret` を使う場合の Secret 作成例：

```bash
kubectl create secret generic my-zfsagent-secret \
  --from-literal=zfsagent-token=<token> \
  -n branchdb-system
```

```yaml
# values.yaml
zfsAgent:
  url: "http://zfs-server.internal:9090"
  existingSecret: "my-zfsagent-secret"
```

---

### リーダー選出

| パラメータ | デフォルト | 説明 |
|---|---|---|
| `leaderElection.enabled` | `true` | 複数レプリカ時のリーダー選出。`replicaCount: 1` の場合は `false` でも可。|

---

### サービスアカウント / RBAC

| パラメータ | デフォルト | 説明 |
|---|---|---|
| `serviceAccount.create` | `true` | ServiceAccount を作成するか |
| `serviceAccount.name` | `""` | ServiceAccount 名（空の場合は自動生成）|
| `serviceAccount.annotations` | `{}` | ServiceAccount に付与するアノテーション。IAM Role などに使用。|
| `rbac.create` | `true` | ClusterRole / ClusterRoleBinding を作成するか |

EKS で IAM Roles for Service Accounts (IRSA) を使う場合：

```yaml
serviceAccount:
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::<account-id>:role/branchdb-role
```

---

### Operator ポート / Service

| パラメータ | デフォルト | 説明 |
|---|---|---|
| `ports.metrics` | `8080` | Prometheus メトリクスポート |
| `ports.health` | `8081` | ヘルスチェックポート（`/healthz`, `/readyz`）|
| `service.enabled` | `true` | Operator の metrics 用 Service を作成するか |
| `service.type` | `ClusterIP` | metrics Service の type |

---

### Operator リソース

| パラメータ | デフォルト | 説明 |
|---|---|---|
| `resources.requests.cpu` | `100m` | CPU リクエスト |
| `resources.requests.memory` | `128Mi` | メモリリクエスト |
| `resources.limits.cpu` | `500m` | CPU 上限 |
| `resources.limits.memory` | `512Mi` | メモリ上限 |
| `podAnnotations` | `{}` | Pod アノテーション |
| `podLabels` | `{}` | 追加の Pod ラベル |
| `nodeSelector` | `{}` | ノードセレクター |
| `tolerations` | `[]` | テイント Toleration |
| `affinity` | `{}` | アフィニティルール |
| `extraEnv` | `[]` | 追加の環境変数（`name`/`value` 形式）|

---

### API サーバー

| パラメータ | デフォルト | 説明 |
|---|---|---|
| `apiServer.enabled` | `true` | API サーバーを有効化するか |
| `apiServer.image.repository` | `ghcr.io/masucchi/branchdb` | API サーバーイメージ |
| `apiServer.image.tag` | `""` | イメージタグ（空の場合は `Chart.appVersion`）|
| `apiServer.image.pullPolicy` | `IfNotPresent` | イメージプルポリシー |
| `apiServer.replicaCount` | `1` | レプリカ数 |
| `apiServer.listenPort` | `8080` | HTTP リッスンポート |
| `apiServer.service.enabled` | `true` | Service を作成するか |
| `apiServer.service.type` | `ClusterIP` | Service type（`ClusterIP` / `LoadBalancer` / `NodePort`）|
| `apiServer.service.nodePort` | `""` | NodePort 番号（`type: NodePort` のとき。空の場合は自動割当）|
| `apiServer.resources` | *(see below)* | リソースリクエスト/上限 |
| `apiServer.podAnnotations` | `{}` | Pod アノテーション |
| `apiServer.nodeSelector` | `{}` | ノードセレクター |
| `apiServer.tolerations` | `[]` | Toleration |
| `apiServer.affinity` | `{}` | アフィニティ |

API サーバーのデフォルトリソース：

```yaml
apiServer:
  resources:
    requests:
      cpu: 100m
      memory: 128Mi
    limits:
      cpu: 500m
      memory: 256Mi
```

---

## 構成例

### 最小構成（開発・検証）

```yaml
# dev-values.yaml
installCRDs: true
externalHost: "192.168.1.100"
zfsAgent:
  url: "http://192.168.1.200:9090"
  token: "dev-token"
leaderElection:
  enabled: false
apiServer:
  service:
    type: NodePort
```

### 本番構成（EKS + LoadBalancer）

```yaml
# prod-values.yaml
installCRDs: true
externalHost: "branchdb.example.com"
zfsAgent:
  url: "http://zfs-agent.internal:9090"
  existingSecret: "zfsagent-credentials"
replicaCount: 2
leaderElection:
  enabled: true
serviceAccount:
  annotations:
    eks.amazonaws.com/role-arn: "arn:aws:iam::123456789:role/branchdb"
resources:
  requests:
    cpu: 200m
    memory: 256Mi
  limits:
    cpu: 1000m
    memory: 1Gi
apiServer:
  replicaCount: 2
  service:
    type: LoadBalancer
  resources:
    requests:
      cpu: 200m
      memory: 256Mi
    limits:
      cpu: 500m
      memory: 512Mi
```

### Ingress を使う構成

```yaml
# ingress-values.yaml
installCRDs: true
externalHost: "branchdb.example.com"
zfsAgent:
  url: "http://zfs-agent.internal:9090"
  token: "secret"
apiServer:
  service:
    type: ClusterIP   # Ingress がルーティングするので ClusterIP で十分
```

Ingress の設定は [Ingress での外部公開](ingress.md) を参照してください。

---

## テンプレートの確認（ドライラン）

```bash
# リソース名の一覧確認
helm template branchdb deploy/helm/branchdb \
  --set zfsAgent.url=http://zfs:9090 \
  --set zfsAgent.token=token \
  | grep -E "^kind:|^  name:"

# 特定リソースの詳細確認
helm template branchdb deploy/helm/branchdb \
  --set zfsAgent.url=http://zfs:9090 \
  -s templates/apiserver.yaml

# lint
helm lint deploy/helm/branchdb
```

---

## アップグレード

詳細は [アップグレードガイド](upgrade.md) を参照してください。

```bash
# CRD を含めてアップグレード
helm upgrade branchdb deploy/helm/branchdb \
  --namespace branchdb-system \
  --set installCRDs=true \
  -f my-values.yaml
```

## アンインストール

```bash
helm uninstall branchdb -n branchdb-system
```

> **注意:** `helm.sh/resource-policy: keep` アノテーションにより、アンインストール後も CRD は残ります。  
> CRD を削除する場合は手動で実行してください：  
> `kubectl delete crd databasebranches.branchdb.io`  
> **CRD を削除すると全ての DatabaseBranch リソースも失われます。**
