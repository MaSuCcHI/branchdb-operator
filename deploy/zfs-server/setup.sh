#!/bin/bash
# BranchDB ZFS Agent セットアップスクリプト
#
# ZFS ストレージサーバー（Kubernetes クラスターとは別の Linux マシン）で実行します。
# 以下をまとめてセットアップします:
#   1. 依存パッケージのインストール（zfsutils-linux, nfs-kernel-server）
#   2. ZFS プール・dataset の作成
#   3. NFS 共有の設定（sharenfs プロパティ）
#   4. ZFS Agent バイナリのビルドとインストール
#   5. systemd サービスの登録・起動
#
# 使い方:
#   sudo ZFS_DEVICE=/dev/sdb \
#        K8S_POD_CIDR=10.42.0.0/16 \
#        ZFSAGENT_TOKEN=$(openssl rand -hex 32) \
#        bash setup.sh
#
set -euo pipefail

POOL_NAME="${POOL_NAME:-tank}"
DATASET_NAME="${DATASET_NAME:-mysql}"
ZFS_DEVICE="${ZFS_DEVICE:-}"
K8S_POD_CIDR="${K8S_POD_CIDR:-10.0.0.0/8}"
ZFSAGENT_ADDR="${ZFSAGENT_ADDR:-:9090}"
ZFSAGENT_TOKEN="${ZFSAGENT_TOKEN:-}"
ZFSAGENT_INSTALL_DIR="${ZFSAGENT_INSTALL_DIR:-/usr/local/bin}"
REPO_URL="https://github.com/MaSuCcHI/branchdb-operator.git"

echo "=== BranchDB ZFS Agent セットアップ ==="
echo "  プール        : ${POOL_NAME}"
echo "  dataset       : ${DATASET_NAME}"
echo "  K8s Pod CIDR  : ${K8S_POD_CIDR}"
echo "  Agent アドレス: ${ZFSAGENT_ADDR}"
echo ""

if [ -z "$ZFSAGENT_TOKEN" ]; then
  echo "ERROR: ZFSAGENT_TOKEN を指定してください"
  echo "  例: ZFSAGENT_TOKEN=\$(openssl rand -hex 32)"
  exit 1
fi

# ── 1. 依存パッケージ ─────────────────────────────────────────────────────────
echo "[1/5] パッケージのインストール..."
apt-get update -y
apt-get install -y zfsutils-linux nfs-kernel-server golang-go

# ── 2. ZFS pool / dataset ─────────────────────────────────────────────────────
echo "[2/5] ZFS セットアップ..."
MASTER_DATASET="${POOL_NAME}/${DATASET_NAME}"

if ! zpool list "$POOL_NAME" &>/dev/null; then
  if [ -z "$ZFS_DEVICE" ]; then
    echo "ERROR: ZFS_DEVICE を指定してください（例: ZFS_DEVICE=/dev/sdb）"
    exit 1
  fi
  zpool create -f "$POOL_NAME" "$ZFS_DEVICE"
  echo "  プール ${POOL_NAME} を作成しました"
fi

zfs list "$MASTER_DATASET" &>/dev/null || zfs create "$MASTER_DATASET"
echo "  dataset ${MASTER_DATASET} OK"

# ── 3. NFS 共有（sharenfs プロパティ）────────────────────────────────────────
echo "[3/5] NFS 共有の設定..."
# sharenfs プロパティを設定すると、作成されるクローンが自動的に NFS エクスポートされる。
# no_root_squash: MySQL initContainer の chown（root 権限）に必要。
CURRENT_SHARENFS=$(zfs get -H -o value sharenfs "$MASTER_DATASET")
if [ "$CURRENT_SHARENFS" = "off" ] || [ "$CURRENT_SHARENFS" = "-" ]; then
  zfs set sharenfs="rw=@${K8S_POD_CIDR},no_root_squash" "$MASTER_DATASET"
  echo "  sharenfs を設定しました: rw=@${K8S_POD_CIDR},no_root_squash"
else
  echo "  sharenfs は既に設定されています: ${CURRENT_SHARENFS}"
fi

systemctl enable nfs-server
systemctl restart nfs-server
echo "  nfs-server を起動しました"

# ── 4. ZFS Agent バイナリのビルド ─────────────────────────────────────────────
echo "[4/5] ZFS Agent のビルド..."
TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

git clone --depth=1 "$REPO_URL" "$TMPDIR/repo"
cd "$TMPDIR/repo"
go build -o "${ZFSAGENT_INSTALL_DIR}/zfsagent" ./cmd/zfsagent
echo "  バイナリを ${ZFSAGENT_INSTALL_DIR}/zfsagent にインストールしました"

# ── 5. systemd サービスの登録 ─────────────────────────────────────────────────
echo "[5/5] systemd サービスの設定..."
cat > /etc/systemd/system/zfsagent.service <<EOF
[Unit]
Description=BranchDB ZFS Agent
After=network.target nfs-server.service
Requires=nfs-server.service

[Service]
Type=simple
User=root
Environment=ZFSAGENT_ADDR=${ZFSAGENT_ADDR}
Environment=ZFSAGENT_TOKEN=${ZFSAGENT_TOKEN}
Environment=ZFSAGENT_POOL=${POOL_NAME}
Environment=ZFSAGENT_DATASET=${DATASET_NAME}
ExecStart=${ZFSAGENT_INSTALL_DIR}/zfsagent
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now zfsagent
echo "  zfsagent サービスを起動しました"

# ── 完了 ─────────────────────────────────────────────────────────────────────
SERVER_IP=$(hostname -I | awk '{print $1}')
echo ""
echo "=== セットアップ完了 ==="
echo ""
echo "Helm インストール時に以下を指定してください:"
echo "  --set zfsAgent.url=http://${SERVER_IP}:${ZFSAGENT_ADDR#:}"
echo "  --set zfsAgent.token=<上記のトークン>"
echo "  --set externalHost=<K8sノードIPまたはLBのホスト名>"
echo ""
echo "動作確認:"
echo "  curl -H 'Authorization: Bearer ${ZFSAGENT_TOKEN}' http://localhost:${ZFSAGENT_ADDR#:}/health"
