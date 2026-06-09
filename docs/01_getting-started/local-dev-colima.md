# ローカル開発環境（macOS + Colima）

Mac 上の Colima VM 1台で ZFS Agent・k3s・BranchDB をすべて動かすガイドです。
実ディスク不要・loopback デバイスで ZFS プールを作るため、試すまでの手順を最小限に抑えています。

## アーキテクチャ

```
[macOS ホスト]
  └─ Colima VM (Ubuntu 22.04)
       ├─ k3s (Kubernetes)
       │   └─ branchdb-operator Pod
       │   └─ branchdb-api Pod
       │   └─ MySQL Branch Pod ─── NFS mount → /tank/mysql/branches/<name>
       └─ ZFS Agent (:9090)
            └─ ZFS pool: tank (loopback device /var/lib/branchdb/zfs.img)
                └─ dataset: tank/mysql
                └─ clone:   tank/mysql/branches/<branch-name> ← NFS export
```

k3s と ZFS Agent が同じ VM 内にいるため、NFS は localhost 経由で完結します。

---

## 前提条件

macOS に以下をインストールしてください。

```bash
brew install colima kubectl helm
```

---

## Step 1: Colima VM を起動する

k3s 付きで VM を起動します。ZFS のビルドに備えてリソースは多めに取ります。

```bash
colima start \
  --kubernetes \
  --cpu 4 \
  --memory 8 \
  --disk 40 \
  --kubernetes-version v1.29.0+k3s1
```

起動確認:

```bash
kubectl get nodes
# NAME      STATUS   ROLES                  AGE   VERSION
# colima    Ready    control-plane,master   1m    v1.29.0+k3s1
```

---

## Step 2: VM 内で ZFS をセットアップする

Colima VM に SSH して ZFS を構築します。

```bash
colima ssh
```

以降はVM内での操作です。

### 2-1. ZFS をインストールする

```bash
sudo apt update
sudo apt install -y zfsutils-linux nfs-kernel-server
```

> DKMS によるカーネルモジュールのビルドに数分かかることがあります。

インストール確認:

```bash
zfs version
# zfs-2.x.x / zfs-kmod-2.x.x
```

### 2-2. ZFS プールを loopback デバイスで作成する

```bash
sudo mkdir -p /var/lib/branchdb
sudo truncate -s 20G /var/lib/branchdb/zfs.img
sudo losetup /dev/loop0 /var/lib/branchdb/zfs.img

sudo zpool create tank /dev/loop0
sudo zfs create tank/mysql
```

### 2-3. NFS を設定する

k3s の Pod CIDR（デフォルト `10.42.0.0/16`）から NFS マウントできるように設定します。

```bash
sudo systemctl enable --now nfs-server

# k3s Pod CIDR からの NFS アクセスを許可
sudo zfs set sharenfs="rw=@10.42.0.0/16,no_root_squash" tank/mysql

# NFS エクスポートの確認
exportfs -v
# /tank/mysql  10.42.0.0/16(rw,...)
```

### 2-4. スナップショットを作成する

空のスナップショットをベースとして作成します（後で本番データを流し込めます）。

```bash
sudo zfs snapshot tank/mysql@initial
sudo zfs list -t snapshot
# NAME                USED  AVAIL  REFER  MOUNTPOINT
# tank/mysql@initial    0B      -     0B  -
```

---

## Step 3: ZFS Agent を起動する

### バイナリをビルドする

VM 内で Go が使えない場合はまずインストールします。

```bash
# Go が未インストールの場合
sudo apt install -y golang-go

# ZFS Agent をビルド
git clone https://github.com/MaSuCcHI/branchdb-operator.git /tmp/branchdb
cd /tmp/branchdb
go build -o /usr/local/bin/zfsagent ./cmd/zfsagent
```

### systemd サービスとして起動する

```bash
ZFSAGENT_TOKEN=$(openssl rand -hex 32)
echo "トークン（後で使います）: $ZFSAGENT_TOKEN"

sudo tee /etc/systemd/system/zfsagent.service <<EOF
[Unit]
Description=BranchDB ZFS Agent
After=network.target nfs-server.service

[Service]
Type=simple
User=root
Environment=ZFSAGENT_ADDR=:9090
Environment=ZFSAGENT_TOKEN=${ZFSAGENT_TOKEN}
Environment=ZFSAGENT_POOL=tank
Environment=ZFSAGENT_DATASET=mysql
ExecStart=/usr/local/bin/zfsagent
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now zfsagent
```

動作確認:

```bash
curl -H "Authorization: Bearer ${ZFSAGENT_TOKEN}" http://localhost:9090/health
# {"status":"ok"}

curl -H "Authorization: Bearer ${ZFSAGENT_TOKEN}" http://localhost:9090/snapshots
# [{"name":"initial","created_at":"..."}]
```

確認できたら VM から抜けます。

```bash
exit
```

---

## Step 4: BranchDB を Helm でインストールする

### VM の IP を確認する

```bash
COLIMA_IP=$(colima list -j | jq -r '.[0].address')
echo $COLIMA_IP
# 例: 192.168.106.2
```

> `jq` が未インストールの場合: `brew install jq`

### ZFS Agent のトークンを確認する

Step 3 で表示したトークンを使います。確認したい場合は:

```bash
colima ssh -- sudo systemctl cat zfsagent | grep ZFSAGENT_TOKEN
```

### Helm でインストールする

```bash
ZFSAGENT_TOKEN=<Step3で表示されたトークン>

helm upgrade --install branchdb \
  ~/sources/branchdb-operator/deploy/helm/branchdb \
  --namespace branchdb-system \
  --create-namespace \
  --set installCRDs=true \
  --set zfsAgent.url=http://${COLIMA_IP}:9090 \
  --set zfsAgent.token=${ZFSAGENT_TOKEN} \
  --set externalHost=${COLIMA_IP}
```

Pod の起動確認:

```bash
kubectl -n branchdb-system get pods
# NAME                                READY   STATUS    RESTARTS
# branchdb-xxxxxx-xxxxx               1/1     Running   0
# branchdb-api-xxxxxx-xxxxx           1/1     Running   0
```

---

## Step 5: 動作確認

### API サーバーへのアクセス

```bash
kubectl -n branchdb-system port-forward svc/branchdb-api 8080:8080 &

curl http://localhost:8080/health
# {"status":"ok"}

curl http://localhost:8080/snapshots
# [{"name":"initial","created_at":"..."}]
```

ブラウザで Web コンソールを開く:

```bash
open http://localhost:8080/
```

### ブランチを作成する

```bash
curl -X POST http://localhost:8080/branches \
  -H 'Content-Type: application/json' \
  -d '{"name":"feature-test","snapshot_ref":"initial","ttl_hours":1}'

# ポートが確定するまで待つ
until curl -s http://localhost:8080/branches/feature-test | grep -v '"port":0'; do
  sleep 3
done
```

### MySQL に接続する

```bash
DSN=$(curl -s http://localhost:8080/branches/feature-test | jq -r .dsn)
echo $DSN
# root@tcp(192.168.106.2:3XXXX)/

mysql -u root -h ${COLIMA_IP} -P <上記のポート番号>
```

### ブランチを削除する

```bash
curl -X DELETE http://localhost:8080/branches/feature-test
# 204 No Content
```

---

## トラブルシューティング

### ZFS カーネルモジュールが読み込めない

```
cannot open '/dev/zfs': No such file or directory
```

DKMS のビルドが失敗している可能性があります。

```bash
colima ssh
sudo apt install -y linux-headers-$(uname -r)
sudo modprobe zfs
```

### loopback デバイスが /dev/loop0 で使われている

```bash
# 空きの loopback デバイスを探す
colima ssh -- losetup -f
# /dev/loop1 など

# setup.sh の losetup コマンドのデバイス名を変更する
```

### VM 再起動後に ZFS プールが消える

Colima の VM を再起動すると loopback デバイスが消えます。再マウントが必要です。

```bash
colima ssh
sudo losetup /dev/loop0 /var/lib/branchdb/zfs.img
sudo zpool import tank
sudo systemctl start zfsagent
```

> 永続化したい場合は `/etc/rc.local` か systemd の `ExecStartPre` で losetup を実行してください。

---

## 次のステップ

- [クイックスタート](quickstart.md) — 本番環境向けセットアップ
- [REST API リファレンス](../api/rest.md) — API の完全仕様
- [ZFS サーバーセットアップ](zfs-server-setup.md) — 実ディスクを使った本格構成
