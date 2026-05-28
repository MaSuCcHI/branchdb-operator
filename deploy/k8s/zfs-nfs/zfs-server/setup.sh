#!/bin/bash
# ZFS + NFS サーバーの初期セットアップ
# Kubernetes クラスターとは別のサーバー（Linux）で実行します
set -euo pipefail

POOL_NAME="${POOL_NAME:-tank}"
DATASET_NAME="${DATASET_NAME:-mysql}"
ZFS_DEVICE="${ZFS_DEVICE:-}"
NFS_ALLOWED_NETWORK="${NFS_ALLOWED_NETWORK:-10.0.0.0/8}"  # k8s Node のネットワーク

echo "=== ZFS + NFS サーバーセットアップ ==="

# ── 依存パッケージ ───────────────────────────────────────────────────────────
echo "[1/5] パッケージのインストール..."
apt-get update -y
apt-get install -y zfsutils-linux nfs-kernel-server

# ── ZFS pool ─────────────────────────────────────────────────────────────────
echo "[2/5] ZFS pool の作成..."
if ! zpool list "$POOL_NAME" &>/dev/null; then
  if [ -z "$ZFS_DEVICE" ]; then
    echo "ERROR: ZFS_DEVICE を指定してください（例: ZFS_DEVICE=/dev/sdb）"
    exit 1
  fi
  zpool create -f "$POOL_NAME" "$ZFS_DEVICE"
fi

# ── ZFS dataset ───────────────────────────────────────────────────────────────
echo "[3/5] ZFS dataset の作成..."
MASTER_DATASET="${POOL_NAME}/${DATASET_NAME}"
BRANCHES_DATASET="${MASTER_DATASET}/branches"

zfs list "$MASTER_DATASET"  &>/dev/null || zfs create "$MASTER_DATASET"
zfs list "$BRANCHES_DATASET" &>/dev/null || zfs create "$BRANCHES_DATASET"

# NFS エクスポート用のマウントポイントを確認
echo "  マウントポイント: /$(zfs get -H -o value mountpoint "$BRANCHES_DATASET")"

# ── NFS エクスポート設定 ──────────────────────────────────────────────────────
echo "[4/5] NFS エクスポートの設定..."
BRANCHES_MOUNTPOINT=$(zfs get -H -o value mountpoint "$BRANCHES_DATASET")

# /etc/exports への追記（重複チェック付き）
EXPORT_LINE="${BRANCHES_MOUNTPOINT} ${NFS_ALLOWED_NETWORK}(rw,sync,no_subtree_check,no_root_squash,fsid=0)"
if ! grep -qF "$BRANCHES_MOUNTPOINT" /etc/exports; then
  echo "$EXPORT_LINE" >> /etc/exports
fi

# 個別のブランチ作成時にエクスポートを追加するスクリプト
cat > /usr/local/bin/branchdb-nfs-export.sh <<'SCRIPT'
#!/bin/bash
# 使い方: branchdb-nfs-export.sh add <branch-name>
#         branchdb-nfs-export.sh remove <branch-name>
POOL="${POOL:-tank}"
DATASET="${DATASET:-mysql}"
ALLOWED="${ALLOWED:-10.0.0.0/8}"
ACTION=$1
BRANCH=$2

MOUNTPOINT="/${POOL}/${DATASET}/branches/${BRANCH}"

case "$ACTION" in
  add)
    zfs clone "${POOL}/${DATASET}@$(zfs list -t snapshot -o name -s createtxg "${POOL}/${DATASET}" | tail -1 | cut -d@ -f2)" \
      "${POOL}/${DATASET}/branches/${BRANCH}" || true
    echo "${MOUNTPOINT} ${ALLOWED}(rw,sync,no_subtree_check,no_root_squash)" >> /etc/exports
    exportfs -ra
    ;;
  remove)
    sed -i "\|${MOUNTPOINT}|d" /etc/exports
    exportfs -ra
    zfs destroy "${POOL}/${DATASET}/branches/${BRANCH}" || true
    ;;
esac
SCRIPT
chmod +x /usr/local/bin/branchdb-nfs-export.sh

exportfs -ra
systemctl enable nfs-kernel-server
systemctl restart nfs-kernel-server

# ── 完了 ─────────────────────────────────────────────────────────────────────
echo "[5/5] 完了"
SERVER_IP=$(hostname -I | awk '{print $1}')
echo ""
echo "NFS サーバー情報:"
echo "  エンドポイント : ${SERVER_IP}"
echo "  エクスポートパス: ${BRANCHES_MOUNTPOINT}"
echo ""
echo "k8s の StorageClass values.yaml に以下を設定してください:"
echo "  nfsServer: ${SERVER_IP}"
echo "  nfsBasePath: ${BRANCHES_MOUNTPOINT}"
