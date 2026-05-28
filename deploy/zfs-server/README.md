# deploy/zfs-server

ZFS ストレージサーバー（Kubernetes クラスターとは別の Linux マシン）への
ZFS Agent デプロイファイル一式です。

## ファイル構成

```
deploy/zfs-server/
├── setup.sh                     ← シェルスクリプトによるセットアップ
├── systemd/
│   └── zfsagent.service         ← systemd ユニットファイル（手動インストール用）
└── ansible/
    ├── playbook.yml             ← Ansible プレイブック
    ├── inventory.ini            ← localhost インベントリ
    ├── vars.yml.example         ← 変数ファイルのテンプレート
    └── templates/
        └── zfsagent.service.j2  ← systemd ユニットファイル（Ansible テンプレート）
```

## セットアップ方法の選択

| 方法 | 向き不向き |
|---|---|
| [シェルスクリプト](#シェルスクリプト) | 素早く試したい・シンプルな環境 |
| [Ansible プレイブック](#ansible-プレイブック) | 冪等性・複数台管理・再現性が必要な場合 |
| [手動](#手動インストール) | 細かく制御したい場合 |

---

## シェルスクリプト

```bash
sudo ZFS_DEVICE=/dev/sdb \
     K8S_POD_CIDR=10.42.0.0/16 \
     ZFSAGENT_TOKEN=$(openssl rand -hex 32) \
     bash deploy/zfs-server/setup.sh
```

---

## Ansible プレイブック

ZFS サーバー自身に対して `ansible_connection=local` で実行します。  
冪等なので何度実行しても安全です。

### 事前準備

```bash
# ZFS サーバー上で Ansible をインストール
apt install -y ansible

# 変数ファイルを作成
cd deploy/zfs-server/ansible
cp vars.yml.example vars.yml
vi vars.yml   # zfsagent_token / zfs_device / k8s_pod_cidr を設定
```

### 実行

```bash
sudo ansible-playbook -i inventory.ini playbook.yml -e @vars.yml
```

### 主な変数（vars.yml）

| 変数 | デフォルト | 説明 |
|---|---|---|
| `zfs_pool` | `tank` | ZFS プール名 |
| `zfs_dataset` | `mysql` | ZFS dataset 名 |
| `zfs_device` | `/dev/sdb` | ZFS プール作成に使用するディスク |
| `k8s_pod_cidr` | `10.42.0.0/16` | K8s Pod CIDR（NFS アクセス許可範囲）|
| `zfsagent_addr` | `:9090` | ZFS Agent HTTP リッスンアドレス |
| `zfsagent_token` | `changeme` | **必須** — 認証トークン |
| `repo_version` | `main` | ビルドするブランチまたはタグ |

### ドライラン（変更を加えずに確認）

```bash
sudo ansible-playbook -i inventory.ini playbook.yml -e @vars.yml --check
```

---

## 手動インストール

`setup.sh` を使わずに手動でインストールする場合は、
[ZFS サーバーセットアップガイド](../../docs/01_getting-started/zfs-server-setup.md) の手順を参照してください。

systemd ユニットファイルのみ手動で配置する場合：

```bash
cp deploy/zfs-server/systemd/zfsagent.service /etc/systemd/system/
# ZFSAGENT_TOKEN などを編集
systemctl daemon-reload
systemctl enable --now zfsagent
```

---

## 完了後

セットアップ完了後、以下を Helm インストール時に指定してください：

```bash
helm upgrade --install branchdb deploy/helm/branchdb \
  --set zfsAgent.url=http://<zfs-server-ip>:9090 \
  --set zfsAgent.token=<token> \
  --set externalHost=<k8s-node-ip>
  ...
```
