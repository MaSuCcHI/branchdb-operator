# ロードマップ

現時点で未実装の機能と設計方針のメモです。

---

## 認証・認可

**現状:** API サーバーに認証機能はありません。  
ネットワークレベルの制限（K8s NetworkPolicy、Ingress の IP 制限など）で保護してください。

**計画:**
- OIDC / JWT 認証ミドルウェアをインターフェース注入で追加する
- GitHub OIDC との統合（GitHub Actions からトークン不要でアクセス）
- 実装は `K8sBranchHandler` に `WithAuthMiddleware(...)` を追加する形で行う

---

## マルチテナント

**現状:** 全ユーザーが同一 namespace のブランチを作成できます。

**計画:**
- ユーザー/チーム単位のブランチ分離（namespace または label ベース）
- `DatabaseBranch` CR にオーナー情報を付与する

---

## クォータ管理

**現状:** ブランチ数に制限はありません。

**計画:**
- ユーザー/チームごとの最大ブランチ数を設定できる ConfigMap/CRD
- Operator の Reconcile ループでクォータチェックを行い、超過時は `Error` フェーズにする

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

## AWS FSx 対応

**現状:** ZFS Agent（ZFS + NFS サーバー）のみサポートしています。

**計画:**
- `VolumeProvider` インターフェースの FSx 実装（`infrastructure/fsx/`）
- FSx API で ZFS スナップショットを Clone し、EFS/NFS マウントで Pod に提供
- EKS + IRSA での認証フロー

`deploy/k8s/fsx/manifests/` に参照用マニフェストがあります。

---

## branchctl の K8s モード対応

**現状:** `cmd/branchctl` は非 K8s モード（Docker/VM）向けのみです。

**計画:**
- `branchctl create --wait` でブランチ作成 + MySQL 接続待ちをワンコマンドで実行
- `branchctl status <branch>` で Pod 状態・接続数を表示
- K8s API サーバーに接続する設定（`~/.config/branchctl/config.yaml`）

---

## Helm リポジトリ

**現状:** Helm チャートはリポジトリ内のローカルパスからのみインストールできます。

**計画:**
- GitHub Pages で Helm リポジトリを公開
- `helm repo add branchdb https://masucci.github.io/branchdb-operator`
