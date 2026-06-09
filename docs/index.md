# BranchDB ドキュメント

BranchDB は Kubernetes 上で動作する MySQL ブランチ管理システムです。  
ZFS のコピーオンライトクローンを使い、プルリクエストごとに独立した MySQL 環境を数秒で払い出します。

---

## ドキュメント構成

### Getting Started（はじめに）

| ページ | 内容 |
|--------|------|
| [ローカル開発環境（macOS + Colima）](01_getting-started/local-dev-colima.md) | Mac 1台で ZFS + k3s を動かしてすぐ試す |
| [クイックスタート](01_getting-started/quickstart.md) | Helm で 5 分インストール → ブランチ作成まで |
| [ZFS サーバーセットアップ](01_getting-started/zfs-server-setup.md) | ZFS サーバーに Agent をデプロイする手順 |

### Deploy（デプロイ）

| ページ | 内容 |
|--------|------|
| [Helm チャートリファレンス](02_deploy/helm.md) | 全 values パラメータの詳細と構成例 |
| [Ingress で外部公開](02_deploy/ingress.md) | Nginx / Traefik / ALB での HTTPS 公開 |
| [アップグレードガイド](02_deploy/upgrade.md) | バージョン間のアップグレード手順・破壊的変更 |

### Architecture（アーキテクチャ）

| ページ | 内容 |
|--------|------|
| [システム概要](03_architecture/overview.md) | コンポーネント構成・データフロー |
| [DatabaseBranch CRD リファレンス](03_architecture/crd-spec.md) | spec/status フィールド全定義 |
| [Operator ライフサイクル](03_architecture/operator-lifecycle.md) | Reconciler の状態遷移と処理フロー |

### API（REST API）

| ページ | 内容 |
|--------|------|
| [REST API リファレンス](04_api/rest.md) | 全エンドポイントのリクエスト/レスポンス仕様 |

### Operations（運用）

| ページ | 内容 |
|--------|------|
| [トラブルシューティング](05_operations/troubleshooting.md) | よくある問題と解決方法 |
| [モニタリング](05_operations/monitoring.md) | Prometheus メトリクス・ヘルスチェック |

### その他

| ページ | 内容 |
|--------|------|
| [ロードマップ](roadmap.md) | 計画中の機能（認証・クォータ管理・FSx 対応など）|

---

## 最小構成

```bash
helm upgrade --install branchdb deploy/helm/branchdb \
  --namespace branchdb-system --create-namespace \
  --set installCRDs=true \
  --set zfsAgent.url=http://<zfs-server>:9090 \
  --set zfsAgent.token=<token> \
  --set externalHost=<node-ip>
```

詳細は [クイックスタート](01_getting-started/quickstart.md) を参照してください。
