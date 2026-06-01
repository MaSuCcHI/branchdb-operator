# branchdb-operator Helm Chart

BranchDB Kubernetes Operator をインストールする Helm チャート。`DatabaseBranch` CRD と
コントローラーをデプロイし、ZFS/FSx スナップショットから MySQL ブランチを払い出せるようにする。

## 前提

- Kubernetes 1.25+
- ZFS Agent が到達可能であること（operator は ZFS 操作を ZFS Agent に HTTP で委譲する）
- ブランチ MySQL は NFS-backed PV を使用するため、ZFS Agent がクローンを NFS エクスポートできること

## インストール

```bash
helm install branchdb-operator ./deploy/helm/branchdb-operator \
  --namespace branchdb-system --create-namespace \
  --set zfsAgent.url=http://zfs-agent.zfs.svc.cluster.local:9090 \
  --set zfsAgent.token=<ZFS_AGENT_TOKEN> \
  --set externalHost=branchdb.example.com
```

CRD は `crds/` ディレクトリに含まれており、インストール時に自動適用される
（Helm の慣習により upgrade では更新されない。CRD の変更時は手動で `kubectl apply` する）。

## 主な values

| キー | 既定値 | 説明 |
|------|--------|------|
| `image.repository` | `ghcr.io/masucchi/branchdb-operator` | operator イメージ |
| `image.tag` | `""`（=Chart.appVersion） | イメージタグ |
| `externalHost` | `""` | 外部クライアントが branch MySQL に接続するホスト名 |
| `portRange.min` / `.max` | `33061` / `34060` | TCP プロキシのポート範囲 |
| `branchNamespace` | `""`（=Release namespace） | branch リソースを作成する namespace |
| `mysqlImage` | `mysql:8.0` | branch MySQL イメージ |
| `zfsAgent.url` | `""`（**必須**） | ZFS Agent のベース URL |
| `zfsAgent.token` | `""` | Bearer トークン（Secret を自動生成） |
| `zfsAgent.existingSecret` | `""` | 既存 Secret 名（key: `zfsagent-token`） |
| `leaderElection.enabled` | `true` | リーダー選出 |
| `rbac.create` | `true` | ClusterRole/Binding を作成 |
| `serviceAccount.create` | `true` | ServiceAccount を作成 |

全項目は `values.yaml` を参照。

## トークンを既存 Secret で渡す

```bash
kubectl -n branchdb-system create secret generic my-zfsagent \
  --from-literal=zfsagent-token=<TOKEN>

helm install branchdb-operator ./deploy/helm/branchdb-operator \
  -n branchdb-system \
  --set zfsAgent.url=http://zfs-agent:9090 \
  --set zfsAgent.existingSecret=my-zfsagent
```

## アンインストール

```bash
helm uninstall branchdb-operator -n branchdb-system
# CRD は残るため、不要なら手動削除:
kubectl delete crd databasebranches.branchdb.io
```
