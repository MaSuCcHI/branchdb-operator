# deploy/k8s

Kubernetes へのデプロイに関するファイル一式です。

## ディレクトリ構成

```
deploy/k8s/
├── crd/                     ← DatabaseBranch CRD マニフェスト
│   └── branchdb.io_databasebranches.yaml
├── e2e/                     ← E2E テスト設定（Colima / k3s）
├── fsx/manifests/           ← AWS FSx 用 raw マニフェスト（参照用）
└── zfs-nfs/manifests/       ← ZFS+NFS 用 raw マニフェスト（参照用）
```

> **推奨:** raw マニフェストの代わりに [Helm chart](../helm/branchdb/) を使用してください。
> CRD・Operator・API サーバーをまとめてインストールできます。

---

## Helm でインストール（推奨）

```bash
helm upgrade --install branchdb deploy/helm/branchdb \
  --namespace branchdb-system \
  --create-namespace \
  --set installCRDs=true \
  --set zfsAgent.url=http://<zfs-server>:9090 \
  --set zfsAgent.token=<token> \
  --set externalHost=<node-ip-or-lb>
```

詳細は [Helm チャートリファレンス](../../docs/deploy/helm.md) を参照してください。

---

## CRD のみをインストールする場合

Helm を使わずに CRD だけをインストールしたい場合：

```bash
kubectl apply -f deploy/k8s/crd/
```

---

## CI/CD との連携（GitHub Actions 例）

```yaml
jobs:
  test:
    runs-on: ubuntu-latest
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

## 関連ドキュメント

| ガイド | 内容 |
|---|---|
| [クイックスタート](../../docs/getting-started/quickstart.md) | 5分インストール手順 |
| [ZFS サーバーセットアップ](../../docs/getting-started/zfs-server-setup.md) | ZFS サーバーの準備 |
| [Helm チャートリファレンス](../../docs/deploy/helm.md) | 全 values パラメータ |
| [Ingress での外部公開](../../docs/deploy/ingress.md) | HTTPS 公開手順 |
| [トラブルシューティング](../../docs/operations/troubleshooting.md) | よくある問題と解決策 |
