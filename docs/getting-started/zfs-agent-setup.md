# ZFS Agent セットアップ

ZFS Agent は ZFS ストレージサーバー上で動作する軽量な HTTP サーバーです。  
BranchDB Operator からの要求を受けて、ZFS スナップショット・クローン操作を実行します。

## 構成イメージ

```
[K8s Cluster]                    [ZFS Server (Linux)]
  Operator ──── HTTP :9090 ────→  ZFS Agent
                                    └─ zfs snapshot / clone / destroy
```

---

## 前提条件

- Linux サーバー（Ubuntu 22.04 / Debian 12 推奨）
- ZFS カーネルモジュール（`zfsutils-linux`）
- Go 1.21 以上（バイナリビルドする場合）または事前ビルドバイナリ

---

## Step 1: ZFS プールのセットアップ

```bash
# loopback デバイスで検証用プールを作成（本番は実ディスクを使用）
truncate -s 20G /var/lib/branchdb/zfs.img
losetup /dev/loop0 /var/lib/branchdb/zfs.img
zpool create tank /dev/loop0

# MySQL マスターデータ用 dataset を作成
zfs create tank/mysql

# MySQL データを配置し、最初のスナップショットを取得
# （mysql のデータをここに展開する）
zfs snapshot tank/mysql@initial
```

本番環境では実ディスクを直接指定します：

```bash
zpool create tank /dev/sdb
zfs create tank/mysql
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
After=network.target

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
# [{"name":"tank/mysql@initial","created_at":"..."}]
```

---

## 環境変数リファレンス

| 変数 | デフォルト | 必須 | 説明 |
|------|-----------|------|------|
| `ZFSAGENT_ADDR` | `:9090` | | HTTP リッスンアドレス |
| `ZFSAGENT_TOKEN` | | ✅ | Bearer 認証トークン（推奨: 32文字以上のランダム文字列） |
| `ZFSAGENT_POOL` | `tank` | | ZFS プール名 |
| `ZFSAGENT_DATASET` | `mysql` | | ZFS dataset 名（プール名は含めない）|

---

## セキュリティ

- ZFS Agent はルート権限が必要です（ZFS 操作のため）
- Kubernetes クラスターから ZFS サーバーへのポート 9090 だけを開放し、他は全て閉じてください
- トークンは最低 32 文字以上のランダム文字列を使用してください

```bash
# トークン生成例
openssl rand -hex 32
```

- 可能であれば VPN や WireGuard でネットワークを保護してください
- TLS 終端が必要な場合は nginx をリバースプロキシとして前段に置いてください

---

## Kubernetes から ZFS Agent へのネットワーク疎通確認

```bash
# K8s クラスター内から疎通確認
kubectl run -it --rm debug --image=curlimages/curl --restart=Never -- \
  curl -H "Authorization: Bearer <token>" http://<zfs-server-ip>:9090/health
```
