# ZFS サーバーセットアップ

ZFS サーバーは BranchDB のストレージ基盤です。ZFS のコピーオンライトクローンと NFS でブランチのデータを提供します。  
ZFS Agent（`cmd/zfsagent`）が HTTP サーバーとして稼働し、Operator からの操作要求（クローン・スナップショット）を受け付けます。

## 全体像

```
[K8s Cluster]                    [ZFS Server (Linux)]
  Operator ──── HTTP :9090 ────→  ZFS Agent
  │                                 └─ zfs clone / snapshot / destroy
  │                                 └─ zfs share  ← NFS エクスポート
  │
  │  ← PV(NFS) → Pod が ZFS clone をマウント
  │
  Pod(MySQL/PG/Redis)
    └── /var/lib/mysql
         └── NFS mount: zfs-server:/tank/mysql/branches/<branch-name>
```

**ブランチ作成時のフロー:**
1. Operator → ZFS Agent: `POST /clones {"snapshot":"snap-1","name":"feat-x"}`
2. ZFS Agent: `zfs clone tank/mysql@snap-1 tank/mysql/branches/feat-x`
3. ZFS Agent: `zfs share tank/mysql/branches/feat-x` ← NFS エクスポートを有効化
4. ZFS Agent → Operator: `{"nfs_server":"10.0.0.1","nfs_path":"/tank/mysql/branches/feat-x"}`
5. Operator: K8s に NFS PersistentVolume を作成し、Pod をマウント

> **NFS は必須です。** ZFS サーバーに `nfs-kernel-server` をインストールし、`sharenfs` プロパティを設定してください（手順は Step 1 を参照）。

## セットアップ方法の選択

| 方法 | コマンド |
|---|---|
| **Ansible プレイブック**（推奨）| [→ Ansible で構築する](#ansible-プレイブックで構築する) |
| シェルスクリプト | `bash deploy/zfs-server/setup.sh` |
| 手動（本ページの手順）| 以下の Step 1〜3 を順に実行 |

---

## 前提条件

| 要件 | 説明 |
|---|---|
| Linux サーバー | Ubuntu 22.04 / Debian 12 推奨 |
| `zfsutils-linux` | ZFS カーネルモジュール |
| `nfs-kernel-server` | **必須** — ZFS サーバーがクローンを NFS でエクスポートするために使用 |
| K8s → ZFS サーバー間の疎通 | ポート 9090（ZFS Agent HTTP）と 2049（NFS）が到達可能であること |

---

## Step 1: ZFS プールと NFS の設定

### 1-1. ZFS プールと dataset の作成

```bash
# 本番環境は実ディスクを使用
zpool create tank /dev/sdb
zfs create tank/mysql

# 検証環境（loopback デバイス）
truncate -s 20G /var/lib/branchdb/zfs.img
losetup /dev/loop0 /var/lib/branchdb/zfs.img
zpool create tank /dev/loop0
zfs create tank/mysql
```

### 1-2. NFS カーネルサーバーのインストール

```bash
apt install -y nfs-kernel-server
systemctl enable --now nfs-server
```

### 1-3. ZFS NFS 共有の設定（重要）

**この設定をしないとブランチが作成できません。**

`sharenfs` プロパティを設定することで、`tank/mysql` 以下に作成されるすべてのクローンが
自動的に NFS エクスポートされます。

```bash
# <k8s-pod-cidr> は kubectl cluster-info で確認できる Pod CIDR
# 例: EKS のデフォルトは 192.168.0.0/16、k3s のデフォルトは 10.42.0.0/16
zfs set sharenfs="rw=@<k8s-pod-cidr>,no_root_squash" tank/mysql

# 設定確認
zfs get sharenfs tank/mysql
# NAME        PROPERTY  VALUE                          SOURCE
# tank/mysql  sharenfs  rw=@10.42.0.0/16,no_root_squash  local

# NFS エクスポートの確認
exportfs -v
# /tank/mysql  10.42.0.0/16(rw,...)
```

> **セキュリティ注意:** `no_root_squash` は Pod 内の root が NFS サーバーの root として
> ファイルを操作できるようにします。MySQL の initContainer（`chown`）に必要です。
> 範囲は K8s Pod CIDR に限定してください。

### 1-4. データの準備とスナップショット

```bash
# MySQL データディレクトリを配置（例：既存 MySQL データを rsync で転送）
# rsync -a /var/lib/mysql/ /tank/mysql/

# スナップショットを取得
zfs snapshot tank/mysql@initial

# 確認
zfs list -t snapshot
# NAME                   USED  AVAIL  REFER  MOUNTPOINT
# tank/mysql@initial      1.2G     -  1.2G   -
```

---

## Step 2: ZFS Agent のインストール

### バイナリをビルドする

```bash
git clone https://github.com/MaSuCcHI/branchdb-operator.git
cd branchdb-operator
go build -o /usr/local/bin/zfsagent ./cmd/zfsagent
```

### systemd サービスとして登録

```bash
cat > /etc/systemd/system/zfsagent.service <<'EOF'
[Unit]
Description=BranchDB ZFS Agent
After=network.target nfs-server.service

[Service]
Type=simple
User=root
Environment=ZFSAGENT_ADDR=:9090
Environment=ZFSAGENT_TOKEN=<strong-random-token>
Environment=ZFSAGENT_POOL=tank
Environment=ZFSAGENT_DATASET=mysql
ExecStart=/usr/local/bin/zfsagent
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now zfsagent
systemctl status zfsagent
```

---

## Step 3: 動作確認

```bash
# ヘルスチェック
curl -H "Authorization: Bearer <token>" http://localhost:9090/health
# {"status":"ok"}

# スナップショット一覧
curl -H "Authorization: Bearer <token>" http://localhost:9090/snapshots
# [{"name":"initial","created_at":"..."}]

# クローン作成テスト（NFS share まで正常に動作するか確認）
curl -X POST -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"snapshot":"initial","name":"test-clone"}' \
  http://localhost:9090/clones

# NFS エクスポートに現れることを確認
exportfs -v | grep test-clone
# /tank/mysql/branches/test-clone ...

# テスト後にクローンを削除
curl -X DELETE -H "Authorization: Bearer <token>" \
  http://localhost:9090/clones/test-clone
```

---

## 環境変数リファレンス

| 変数 | デフォルト | 必須 | 説明 |
|------|-----------|------|------|
| `ZFSAGENT_ADDR` | `:9090` | | HTTP リッスンアドレス |
| `ZFSAGENT_TOKEN` | | ✅ | Bearer 認証トークン（推奨: 32文字以上のランダム文字列）|
| `ZFSAGENT_POOL` | `tank` | | ZFS プール名 |
| `ZFSAGENT_DATASET` | `mysql` | | ZFS dataset 名（プール名は含めない）|

---

## セキュリティ

```bash
# トークン生成例
openssl rand -hex 32
```

- ZFS Agent はルート権限が必要（ZFS・NFS 操作のため）
- K8s クラスターからの接続のみを許可してください
  - ポート 9090（ZFS Agent HTTP）: K8s ノード IP から
  - ポート 2049（NFS）: K8s Pod CIDR から
- 可能であれば VPN や WireGuard でネットワークを保護してください
- TLS 終端が必要な場合は nginx をリバースプロキシとして前段に配置してください

---

## よくあるエラー

### `zfs share` が失敗する

```
zfs share tank/mysql/branches/feat-x: ZFS file system sharing not set
```

**原因:** `sharenfs` プロパティが設定されていません。

```bash
# 修正
zfs set sharenfs="rw=@<k8s-pod-cidr>,no_root_squash" tank/mysql
```

### Pod が NFS をマウントできない

```
mount.nfs: Connection timed out
```

**確認ポイント:**
- ZFS サーバーの iptables でポート 2049 が K8s Pod CIDR から許可されているか
- `exportfs -v` でパスが表示されているか
- `showmount -e <zfs-server-ip>` が Pod から成功するか

```bash
# K8s クラスターから疎通確認
kubectl run -it --rm debug --image=busybox --restart=Never -- \
  showmount -e <zfs-server-ip>
```

### `zfs clone` は成功するが `zfs share` で失敗する

`nfs-server` が起動していない可能性があります。

```bash
systemctl status nfs-server
systemctl start nfs-server
```

---

## Kubernetes から ZFS サーバーへの疎通確認

```bash
kubectl run -it --rm debug --image=curlimages/curl --restart=Never -- \
  curl -H "Authorization: Bearer <token>" http://<zfs-server-ip>:9090/health
```

---

## Ansible プレイブックで構築する

手動の Step 1〜3 の内容をすべて自動化した Ansible プレイブックです。  
冪等なので何度実行しても安全です。ZFS サーバー自身に対して `ansible_connection=local` で実行します。

### 事前準備

```bash
# ZFS サーバー上で Ansible をインストール
apt install -y ansible

# リポジトリをクローン
git clone https://github.com/MaSuCcHI/branchdb-operator.git
cd branchdb-operator/deploy/zfs-server/ansible

# 変数ファイルを作成
cp vars.yml.example vars.yml
```

`vars.yml` を編集して環境に合わせた値を設定します：

```yaml
# vars.yml（最低限変更が必要な項目）
zfs_device: /dev/sdb         # ZFS プールに使うディスク
k8s_pod_cidr: 10.42.0.0/16  # kubectl cluster-info dump | grep podCIDR で確認
zfsagent_token: "$(openssl rand -hex 32)"  # ★ 必ず強いトークンに変更
```

### 実行

```bash
# ドライラン（変更内容の確認のみ、実際には変更しない）
sudo ansible-playbook -i inventory.ini playbook.yml -e @vars.yml --check

# 実行
sudo ansible-playbook -i inventory.ini playbook.yml -e @vars.yml
```

実行完了後、以下のような出力が表示されます：

```
TASK [セットアップ完了メッセージを表示]
ok: [localhost] => {
  "msg": [
    "=== BranchDB ZFS サーバーセットアップ完了 ===",
    "",
    "Helm インストール時に以下を指定してください:",
    "  --set zfsAgent.url=http://10.0.0.1:9090",
    "  --set zfsAgent.token=<token>",
    "  --set externalHost=<K8s ノード IP または LB ホスト名>"
  ]
}
```

### Ansible が行う処理

1. `zfsutils-linux`・`nfs-kernel-server`・`git`・`golang-go` をインストール
2. ZFS プールが未作成なら `zpool create` を実行
3. ZFS dataset が未作成なら `zfs create` を実行
4. `sharenfs` プロパティが未設定なら `zfs set sharenfs=...` を設定
5. `nfs-server` を有効化・起動
6. リポジトリをクローンして `zfsagent` バイナリをビルド・配置
7. systemd ユニットファイルを配置して `zfsagent` を有効化・起動

> **注意:** `zfsagent_token` に `changeme` が設定されている場合、プレイブックは実行を継続しますが、
> 本番環境では必ず `openssl rand -hex 32` で生成した強いトークンを使用してください。
