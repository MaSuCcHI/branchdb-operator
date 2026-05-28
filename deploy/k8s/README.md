# Kubernetes デプロイ（BranchDB Pro）

Kubernetes 環境で BranchDB Pro を動かす手順です。  
**Pro ライセンスキーが必要です。** [https://branchdb.io/pro](https://branchdb.io/pro) で取得してください。

## ストレージ構成の選択

| | `zfs-nfs/` | `fsx/` |
|---|---|---|
| **ストレージ** | 自前 Linux サーバー上の ZFS を NFS でエクスポート | Amazon FSx for OpenZFS |
| **環境** | オンプレミス / 任意のクラウド | AWS EKS |
| **コスト** | サーバー運用コストのみ | FSx 利用料金（GB/月）|
| **スナップショット** | `zfs snapshot` CLI | FSx API（自動） |
| **推奨規模** | 中規模（開発者 10〜50 人） | 大規模・エンタープライズ |

---

## shared-vm からの移行

```
shared-vm → k8s への移行パス:

1. ZFS スナップショットを取得
   curl -X POST http://<shared-vm>:8080/snapshots

2. スナップショットを NFS サーバー or FSx に転送
   zfs send tank/mysql@<snapshot> | ssh <nfs-server> zfs recv tank/mysql@<snapshot>
   # FSx の場合は AWS DataSync を使用

3. k8s クラスターに Operator をデプロイ（下記手順参照）

4. shared-vm を停止
```

---

## zfs-nfs: 自前 ZFS+NFS サーバー構成

### アーキテクチャ

```
[k8s Cluster]
  ├─ branchdb-operator (Deployment)
  │   └─ DatabaseBranch CR を監視・調停
  ├─ mysql-master (StatefulSet)
  │   └─ NFS mount: /tank/mysql
  └─ branch-<name> (Pod × N)
       └─ NFS mount: /tank/mysql/branches/<name>
           │
           │ NFS over TCP
           ▼
[ZFS+NFS Server (Linux)]
  └─ ZFS pool: tank
      ├─ tank/mysql          ← マスター MySQL データ
      └─ tank/mysql/branches
          ├─ feature-a       ← ZFS clone of tank/mysql@auto-xxx
          └─ feature-b
```

### セットアップ

#### 1. ZFS+NFS サーバーの準備

```bash
# 別途 Linux サーバーを用意し、セットアップスクリプトを実行
sudo ZFS_DEVICE=/dev/sdb bash deploy/k8s/zfs-nfs/zfs-server/setup.sh

# 出力された NFS サーバー IP とパスを控える
# NFS サーバー情報:
#   エンドポイント : 192.168.1.100
#   エクスポートパス: /tank/mysql/branches
```

#### 2. マニフェストの設定

```bash
# NFS サーバーの IP を各マニフェストに設定
# 変更が必要なファイル:
#   - manifests/operator.yaml  (ConfigMap の NFS_SERVER)
#   - manifests/mysql-master.yaml  (PersistentVolume の nfs.server)
```

#### 3. ライセンスキーの設定

```bash
# manifests/operator.yaml の Secret を編集
kubectl create secret generic branchdb-license \
  --from-literal=license-key="<your-pro-license-key>" \
  --namespace branchdb-system \
  --dry-run=client -o yaml | kubectl apply -f -
```

#### 4. デプロイ

```bash
kubectl apply -f deploy/k8s/zfs-nfs/manifests/namespace.yaml
kubectl apply -f deploy/k8s/zfs-nfs/manifests/crds.yaml
kubectl apply -f deploy/k8s/zfs-nfs/manifests/rbac.yaml
kubectl apply -f deploy/k8s/zfs-nfs/manifests/operator.yaml
kubectl apply -f deploy/k8s/zfs-nfs/manifests/mysql-master.yaml

# Operator の起動確認
kubectl rollout status deployment/branchdb-operator -n branchdb-system
```

#### 5. 動作確認

```bash
# スナップショット取得
kubectl exec -n branchdb-system deployment/branchdb-operator -- \
  curl -X POST http://localhost:8080/snapshots

# ブランチ作成（CR 経由）
kubectl apply -f deploy/k8s/zfs-nfs/manifests/sample-databasebranch.yaml

# ステータス確認
kubectl get databasebranches -n branchdb-system
# NAME                 STATUS    PORT    AGE
# feature-payment-v2  Running   33101   2m

# MySQL 接続確認
kubectl port-forward -n branchdb-system svc/branch-feature-payment-v2 33101:33101
mysql -h 127.0.0.1 -P 33101 -u root -p
```

---

## fsx: Amazon FSx for OpenZFS 構成

### アーキテクチャ

```
[EKS Cluster]
  ├─ branchdb-operator (Deployment, IRSA)
  │   └─ DatabaseBranch CR を監視
  │       ├─ FSx API で Volume をクローン（CoW、即座）
  │       └─ PV/PVC を作成して MySQL Pod を起動
  ├─ mysql-master (StatefulSet)
  │   └─ NFS mount: FSx Volume (fsvol-xxx)
  └─ branch-<name> (Pod × N)
       └─ NFS mount: FSx cloned Volume
           │
           │ NFS over TCP（VPC 内）
           ▼
[Amazon FSx for OpenZFS]
  ├─ Root Volume
  │   └─ /fsx/mysql          ← マスター MySQL データ
  └─ Branch Volumes (cloned)
      ├─ /fsx/mysql/branch-a
      └─ /fsx/mysql/branch-b
```

### セットアップ

#### 1. FSx for OpenZFS ファイルシステムの作成

```bash
# AWS コンソール or CLI で FSx for OpenZFS ファイルシステムを作成
aws fsx create-file-system \
  --file-system-type OPENZFS \
  --storage-capacity 1024 \
  --subnet-ids subnet-xxxxxxxxxx \
  --open-zfs-configuration '{
    "DeploymentType": "SINGLE_AZ_1",
    "ThroughputCapacity": 64,
    "RootVolumeConfiguration": {
      "DataCompressionType": "LZ4",
      "NfsExports": [{"ClientConfigurations": [{"Clients": "*", "Options": ["rw","crossmnt","no_root_squash"]}]}]
    }
  }'

# マスター MySQL 用 Volume を作成
aws fsx create-volume \
  --volume-type OPENZFS \
  --name mysql-master \
  --open-zfs-configuration '{"ParentVolumeId":"fsvol-xxxx","StorageCapacityReservationGiB":100}'
```

#### 2. IRSA の設定

```bash
# IAM ポリシーの作成
aws iam create-policy \
  --policy-name BranchDBOperatorPolicy \
  --policy-document file://deploy/k8s/fsx/manifests/fsx-iam-policy.json

# IRSA ロールの作成（eksctl 使用の場合）
eksctl create iamserviceaccount \
  --cluster <cluster-name> \
  --namespace branchdb-system \
  --name branchdb-operator \
  --attach-policy-arn arn:aws:iam::<account-id>:policy/BranchDBOperatorPolicy \
  --approve
```

#### 3. マニフェストの設定

```bash
# manifests/operator.yaml の ConfigMap を編集:
#   FSX_REGION, FSX_FILE_SYSTEM_ID, FSX_MASTER_VOLUME_ID, FSX_DNS_NAME

# manifests/mysql-master.yaml の PV を編集:
#   nfs.server  → FSx DNS 名
#   nfs.path    → NFS エクスポートパス（/fsx/mysql）
```

#### 4. デプロイ

```bash
kubectl apply -f deploy/k8s/fsx/manifests/namespace.yaml
kubectl apply -f deploy/k8s/fsx/manifests/crds.yaml
kubectl apply -f deploy/k8s/fsx/manifests/rbac.yaml
kubectl apply -f deploy/k8s/fsx/manifests/operator.yaml
kubectl apply -f deploy/k8s/fsx/manifests/mysql-master.yaml

kubectl rollout status deployment/branchdb-operator -n branchdb-system
```

#### 5. 動作確認

```bash
# スナップショット取得（FSx スナップショット API を呼び出す）
kubectl exec -n branchdb-system deployment/branchdb-operator -- \
  curl -X POST http://localhost:8080/snapshots

# ブランチ作成
kubectl apply -f deploy/k8s/fsx/manifests/sample-databasebranch.yaml

kubectl get databasebranches -n branchdb-system
```

---

## CI/CD からの利用（GitHub Actions）

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
            -H 'Authorization: Bearer ${{ secrets.BRANCHDB_TOKEN }}' \
            -d '{"name":"pr-${{ github.event.number }}","ttl_hours":2}')
          echo "port=$(echo $RESP | jq -r .port)" >> $GITHUB_OUTPUT

      - name: Run tests
        env:
          DATABASE_URL: mysql://root:${{ secrets.MYSQL_ROOT_PASSWORD }}@${{ secrets.BRANCHDB_HOST }}:${{ steps.db.outputs.port }}/
        run: go test ./...

      - name: Delete DB branch
        if: always()
        run: |
          curl -sf -X DELETE \
            -H 'Authorization: Bearer ${{ secrets.BRANCHDB_TOKEN }}' \
            https://${{ secrets.BRANCHDB_HOST }}/branches/pr-${{ github.event.number }}
```

---

## クォータ管理（Pro 機能）

```bash
# ユーザーごとの最大ブランチ数を変更
kubectl patch configmap branchdb-operator-config -n branchdb-system \
  --patch '{"data":{"DEFAULT_MAX_BRANCHES_PER_USER":"10"}}'

# チームごとの設定はラベルで上書き可能（GUI から設定）
```

---

## API サーバー環境変数

| 変数 | デフォルト | 説明 |
|---|---|---|
| `ZFSDB_LISTEN_ADDR` | `:8080` | HTTP リッスンアドレス |
| `ZFSDB_EXTERNAL_HOST` | `localhost` | NodePort の外部ホスト名（ノード IP or LB DNS） |
| `ZFSDB_NAMESPACE` | `default` | DatabaseBranch CR を管理する名前空間 |
| `ZFSDB_ZFSAGENT_URL` | *(未設定)* | ZFS Agent の URL。設定時のみスナップショット API が有効 |
| `ZFSDB_ZFSAGENT_TOKEN` | *(未設定)* | ZFS Agent の認証トークン |
| `ZFSDB_MODE` | `oss` | `k8s` を指定すると K8s モードで起動 |

---

## 管理コンソール

API サーバーの `GET /` でブラウザベースの管理コンソール（K8s SPA）が利用できる。

- **ブランチ一覧** — フェーズ別カラーバッジ・経過時間・NodePort・MySQL 接続数
- **ブランチ作成** — name / snapshot_ref / ttl_hours を指定
- **詳細パネル** — DSN コピー・Pod ステータス・エラーメッセージ（行クリックで展開）
- **スナップショットタブ** — 一覧・即時取得（`ZFSDB_ZFSAGENT_URL` 設定時のみ）
- **Stats バー** — Total / Ready / Creating / Error のリアルタイムカウント

---

## トラブルシューティング

```bash
# Operator のログ確認
kubectl logs -n branchdb-system deployment/branchdb-operator -f

# DatabaseBranch のイベント確認
kubectl describe databasebranch feature-payment-v2 -n branchdb-system

# NFS マウント確認（ブランチ Pod 内）
kubectl exec -n branchdb-system <branch-pod-name> -- df -h /var/lib/mysql
```
