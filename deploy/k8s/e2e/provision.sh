#!/bin/bash
# deploy/k8s/e2e/provision.sh
#
# K8s E2E 用 Colima VM (k3s + containerd) の中で実行するプロビジョニングスクリプト。
# ZFS プール作成 → MySQL datadir シード → スナップショット → NFS 共有 →
# zfsagent サービス起動 → operator イメージビルド までを行う。
# operator のデプロイ自体は Helm でホスト側から行う (make e2e-k8s-up)。
#
# 使い方（VM 内, sudo 必須）:
#   sudo REPO_DIR=/path/to/zfs-db bash deploy/k8s/e2e/provision.sh
#
# 前提:
#   - bin/operator-linux と bin/zfsagent-linux が REPO_DIR にクロスコンパイル済み
#   - containerd runtime + k3s 有効 (nerdctl / kubectl が使える)

set -euo pipefail

REPO_DIR="${REPO_DIR:-$(cd "$(dirname "$0")/../../.." && pwd)}"
POOL_NAME="${POOL_NAME:-tank}"
DATASET="${POOL_NAME}/mysql"
BRANCHES_DATASET="${DATASET}/branches"
PG_DATASET="${POOL_NAME}/postgres"
PG_BRANCHES_DATASET="${PG_DATASET}/branches"
ZFS_IMG="${ZFS_IMG:-/var/lib/branchdb-e2e/zfs.img}"
ZFS_IMG_SIZE="${ZFS_IMG_SIZE:-10G}"
SNAPSHOT_NAME="${SNAPSHOT_NAME:-base}"
ZFSAGENT_TOKEN="${ZFSAGENT_TOKEN:-e2e-token}"
ZFSAGENT_PORT="${ZFSAGENT_PORT:-9000}"
MYSQL_IMAGE="${MYSQL_IMAGE:-mysql:8.0}"
POSTGRES_IMAGE="${POSTGRES_IMAGE:-postgres:16}"

NERDCTL="nerdctl -n k8s.io"

log()  { echo "[e2e-provision] $*"; }
step() { echo ""; echo "=== $* ==="; }

if [[ $EUID -ne 0 ]]; then
  echo "エラー: sudo で実行してください" >&2
  exit 1
fi

NODE_IP=$(hostname -I | awk '{print $1}')
log "node IP: ${NODE_IP}"

# ── 0. システムパッケージ (ZFS / NFS) ───────────────────────────────────────
step "0/6 ZFS / NFS パッケージ"
if ! command -v zfs &>/dev/null || ! command -v exportfs &>/dev/null; then
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get install -y zfsutils-linux nfs-kernel-server
fi
modprobe zfs
systemctl enable --now nfs-kernel-server || true

# ── 1. ZFS プール ───────────────────────────────────────────────────────────
step "1/6 ZFS プール作成"
if zpool list "$POOL_NAME" &>/dev/null; then
  log "pool '$POOL_NAME' は既に存在します"
else
  mkdir -p "$(dirname "$ZFS_IMG")"
  truncate -s "$ZFS_IMG_SIZE" "$ZFS_IMG"
  LOOP=$(losetup --find --show "$ZFS_IMG")
  zpool create -f "$POOL_NAME" "$LOOP"
  log "pool '$POOL_NAME' を作成しました (loop=$LOOP)"
fi

if ! zfs list "$DATASET" &>/dev/null; then
  zfs create -o mountpoint="/${DATASET}" "$DATASET"
  log "dataset '$DATASET' を作成しました"
fi
if ! zfs list "$PG_DATASET" &>/dev/null; then
  zfs create -o mountpoint="/${PG_DATASET}" "$PG_DATASET"
  log "dataset '$PG_DATASET' を作成しました"
fi

# ── 2. MySQL datadir シード ─────────────────────────────────────────────────
step "2/6 MySQL datadir シード"
if [[ -f "/${DATASET}/ibdata1" ]]; then
  log "datadir は既にシード済みです"
else
  $NERDCTL rm -f mysql-seed &>/dev/null || true
  log "mysql:8.0 で datadir を初期化します..."
  $NERDCTL run -d --name mysql-seed \
    -e MYSQL_ALLOW_EMPTY_PASSWORD=yes \
    -v "/${DATASET}:/var/lib/mysql" \
    "$MYSQL_IMAGE"

  log "MySQL の起動を待機中..."
  for i in $(seq 1 60); do
    if $NERDCTL exec mysql-seed mysqladmin ping -h localhost --silent &>/dev/null; then
      log "MySQL ready (${i}回目)"
      break
    fi
    sleep 2
  done

  $NERDCTL exec mysql-seed mysql -uroot -e \
    "CREATE DATABASE IF NOT EXISTS e2e_seed; \
     CREATE TABLE IF NOT EXISTS e2e_seed.marker (id INT PRIMARY KEY); \
     INSERT IGNORE INTO e2e_seed.marker VALUES (1);"
  log "シードデータ (e2e_seed.marker) を投入しました"

  $NERDCTL stop mysql-seed
  $NERDCTL rm mysql-seed
fi

# ── 2b. PostgreSQL datadir シード ────────────────────────────────────────────
step "2b/6 PostgreSQL datadir シード"
if [[ -f "/${PG_DATASET}/PG_VERSION" ]]; then
  log "postgres datadir は既にシード済みです"
else
  $NERDCTL rm -f pg-seed &>/dev/null || true
  log "${POSTGRES_IMAGE} で datadir を初期化します..."
  $NERDCTL run -d --name pg-seed \
    -e POSTGRES_HOST_AUTH_METHOD=trust \
    -v "/${PG_DATASET}:/var/lib/postgresql/data" \
    "$POSTGRES_IMAGE"

  log "PostgreSQL の起動を待機中..."
  for i in $(seq 1 60); do
    if $NERDCTL exec pg-seed pg_isready -U postgres &>/dev/null; then
      log "PostgreSQL ready (${i}回目)"
      break
    fi
    sleep 2
  done

  $NERDCTL exec pg-seed psql -U postgres -c \
    "CREATE DATABASE e2e_seed;" || true
  $NERDCTL exec pg-seed psql -U postgres -d e2e_seed -c \
    "CREATE TABLE IF NOT EXISTS marker (id INT PRIMARY KEY); \
     INSERT INTO marker VALUES (1) ON CONFLICT DO NOTHING;"
  log "シードデータ (e2e_seed.marker) を投入しました"

  $NERDCTL stop pg-seed
  $NERDCTL rm pg-seed
fi

# ── 3. ベーススナップショット ───────────────────────────────────────────────
step "3/6 スナップショット作成"
if zfs list -t snapshot "${DATASET}@${SNAPSHOT_NAME}" &>/dev/null; then
  log "snapshot '${DATASET}@${SNAPSHOT_NAME}' は既に存在します"
else
  zfs snapshot "${DATASET}@${SNAPSHOT_NAME}"
  log "snapshot '${DATASET}@${SNAPSHOT_NAME}' を作成しました"
fi
if zfs list -t snapshot "${PG_DATASET}@${SNAPSHOT_NAME}" &>/dev/null; then
  log "snapshot '${PG_DATASET}@${SNAPSHOT_NAME}' は既に存在します"
else
  zfs snapshot "${PG_DATASET}@${SNAPSHOT_NAME}"
  log "snapshot '${PG_DATASET}@${SNAPSHOT_NAME}' を作成しました"
fi

# ── 4. branches データセット + NFS 共有 ──────────────────────────────────────
step "4/6 branches データセットと NFS 共有"
if ! zfs list "$BRANCHES_DATASET" &>/dev/null; then
  zfs create -o mountpoint="/${BRANCHES_DATASET}" "$BRANCHES_DATASET"
  log "dataset '$BRANCHES_DATASET' を作成しました"
fi
# クローン (子データセット) は sharenfs を継承し、作成時に自動エクスポートされる。
# no_root_squash: kubelet/mysql(uid=999) が NFS 経由で datadir を読み書きできるようにする。
zfs set sharenfs='rw,no_root_squash,no_subtree_check,insecure' "$BRANCHES_DATASET"
systemctl restart nfs-kernel-server || true
log "branches データセットに sharenfs を設定しました"

# 重要: branches サブツリーの mount propagation を private にする。
# k3s/containerd は / を rshared にしているため、ここを shared のままにすると
# zfs clone のマウントが全 Pod のマウント名前空間に伝播し、Pod 削除後も他 Pod が
# データセットを掴んで "dataset is busy" で zfs destroy が失敗する。
# Pod は NFS 経由でデータにアクセスするため、ホスト側マウントの伝播は不要。
mount --make-rprivate "/${BRANCHES_DATASET}"
log "branches マウントを private propagation に設定しました"

if ! zfs list "$PG_BRANCHES_DATASET" &>/dev/null; then
  zfs create -o mountpoint="/${PG_BRANCHES_DATASET}" "$PG_BRANCHES_DATASET"
  log "dataset '$PG_BRANCHES_DATASET' を作成しました"
fi
zfs set sharenfs='rw,no_root_squash,no_subtree_check,insecure' "$PG_BRANCHES_DATASET"
systemctl restart nfs-kernel-server || true
mount --make-rprivate "/${PG_BRANCHES_DATASET}"
log "postgres branches データセットを NFS 共有 + private propagation に設定しました"

# ── 5. zfsagent サービス ─────────────────────────────────────────────────────
step "5/6 zfsagent サービス起動"
cat > /etc/systemd/system/branchdb-zfsagent.service <<EOF
[Unit]
Description=BranchDB ZFS Agent (E2E)
After=zfs.target nfs-kernel-server.service

[Service]
ExecStart=${REPO_DIR}/bin/zfsagent-linux
Environment=ZFSAGENT_ADDR=:${ZFSAGENT_PORT}
Environment=ZFSAGENT_TOKEN=${ZFSAGENT_TOKEN}
Environment=ZFSAGENT_DATASETS=mysql:${DATASET},postgres:${PG_DATASET}
Restart=on-failure
RestartSec=2

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable branchdb-zfsagent.service
# enable --now は既に起動中だと再起動しないため、設定・バイナリ更新を確実に反映する
systemctl restart branchdb-zfsagent.service
sleep 2
if curl -sf -H "Authorization: Bearer ${ZFSAGENT_TOKEN}" \
     "http://${NODE_IP}:${ZFSAGENT_PORT}/snapshots" >/dev/null; then
  log "zfsagent が応答しました (http://${NODE_IP}:${ZFSAGENT_PORT})"
else
  log "警告: zfsagent の応答確認に失敗しました。journalctl -u branchdb-zfsagent を確認してください。"
fi

# ── 6. イメージビルド (operator + API サーバー) ──────────────────────────────
# E2E 用 Dockerfile は FROM scratch を使用するためインターネット不要。
# bin/operator-linux と bin/branchdb-linux は make build-linux でホスト側（Mac）で
# クロスコンパイル済みである必要がある（GOOS=linux GOARCH=amd64）。
step "6/6 イメージビルド (nerdctl -> k3s containerd)"
cd "$REPO_DIR"
if [[ ! -f "bin/operator-linux" ]]; then
  log "ERROR: bin/operator-linux が存在しません。ホスト側で make build-linux を実行してください。" >&2
  exit 1
fi
if [[ ! -f "bin/branchdb-linux" ]]; then
  log "ERROR: bin/branchdb-linux が存在しません。ホスト側で make build-linux を実行してください。" >&2
  exit 1
fi
$NERDCTL build -f Dockerfile.operator.e2e -t branchdb-operator:e2e .
log "イメージ branchdb-operator:e2e をビルドしました"
$NERDCTL build -f Dockerfile.branchdb.e2e -t branchdb:e2e .
log "イメージ branchdb:e2e をビルドしました"

# operator のデプロイは Helm でホスト側から行う (make e2e-k8s-up が helm install を実行)。
# こうすることで E2E のたびに本番と同じ Helm インストール経路が検証される。

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo " K8s E2E インフラの準備が完了しました (operator は Helm で導入)"
echo "   NODE_IP      : ${NODE_IP}"
echo "   snapshot     : ${DATASET}@${SNAPSHOT_NAME}"
echo "   zfsagent     : http://${NODE_IP}:${ZFSAGENT_PORT}"
echo "   image        : branchdb-operator:e2e"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
