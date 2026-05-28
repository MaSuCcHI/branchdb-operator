# deploy/zfs-server

ZFS ストレージサーバー（Kubernetes クラスターとは別の Linux マシン）への
ZFS Agent デプロイファイル一式です。

## ファイル構成

```
deploy/zfs-server/
├── setup.sh                 ← セットアップスクリプト（ワンライナー実行）
└── systemd/
    └── zfsagent.service     ← systemd ユニットファイル（手動インストール用）
```

## クイックセットアップ

```bash
sudo ZFS_DEVICE=/dev/sdb \
     K8S_POD_CIDR=10.42.0.0/16 \
     ZFSAGENT_TOKEN=$(openssl rand -hex 32) \
     bash deploy/zfs-server/setup.sh
```

完了後に表示される Helm インストールコマンドを使用してください。

## 詳細手順

[ZFS Agent セットアップガイド](../../docs/getting-started/zfs-agent-setup.md) を参照してください。

## 手動インストール

`setup.sh` を使わずに手動でインストールする場合は、
systemd ユニットファイルをコピーして編集してください：

```bash
cp deploy/zfs-server/systemd/zfsagent.service /etc/systemd/system/
# /etc/systemd/system/zfsagent.service を編集して ZFSAGENT_TOKEN などを設定
systemctl daemon-reload
systemctl enable --now zfsagent
```
