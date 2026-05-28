# アップグレードガイド

## 基本手順

```bash
# 1. 現在のバージョン確認
helm list -n branchdb-system

# 2. CRD を含めてアップグレード
helm upgrade branchdb deploy/helm/branchdb \
  --namespace branchdb-system \
  --set installCRDs=true \
  -f my-values.yaml

# 3. ロールアウト確認
kubectl -n branchdb-system rollout status deploy/branchdb
kubectl -n branchdb-system rollout status deploy/branchdb-api
```

---

## CRD のアップグレードについて

BranchDB は `installCRDs: true`（cert-manager パターン）を採用しています。  
`helm upgrade --set installCRDs=true` で CRD も同時にアップグレードされます。

```bash
# CRD の現在のバージョン確認
kubectl get crd databasebranches.branchdb.io -o jsonpath='{.metadata.annotations}'
```

> **注意:** CRD のアップグレード中は既存の `DatabaseBranch` リソースの削除は行われません。  
> フィールドの追加は後方互換性があります。フィールドの削除や型変更は破壊的変更となります。

---

## ロールバック

```bash
# 前のバージョンに戻す
helm rollback branchdb -n branchdb-system

# 特定リビジョンに戻す
helm history branchdb -n branchdb-system
helm rollback branchdb <revision> -n branchdb-system
```

> **注意:** CRD はロールバックされません。CRD を前バージョンに戻す必要がある場合は手動で適用してください。

---

## ゼロダウンタイムアップグレード

API サーバーは `RollingUpdate` 戦略を採用しているため、レプリカ数を 2 以上にするとゼロダウンタイムでアップグレードできます。

```yaml
# values.yaml
apiServer:
  replicaCount: 2
```

Operator はリーダー選出により複数レプリカでも安全に動作します。

---

## バージョン間の破壊的変更

### v0.1.x → v0.2.x

- Helm チャート名が `branchdb-operator` → `branchdb` に変更されました
- 既存インストールを移行する場合：

```bash
# 旧チャートをアンインストール（CRD は残る）
helm uninstall branchdb-operator -n branchdb-system

# 新チャートをインストール（CRD は既に存在するので installCRDs=false）
helm upgrade --install branchdb deploy/helm/branchdb \
  --namespace branchdb-system \
  --set installCRDs=false \
  -f my-values.yaml
```
