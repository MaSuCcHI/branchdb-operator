# Local Development (macOS + Colima)

This guide runs ZFS Agent, k3s, and BranchDB entirely inside a single Colima VM on your Mac.
No real disk required — a loopback device is used for the ZFS pool, keeping the setup as simple as possible.

## Architecture

```
[macOS host]
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

Because k3s and the ZFS Agent share the same VM, NFS communication is entirely local.

---

## Prerequisites

Install the following on your Mac:

```bash
brew install colima kubectl helm
```

---

## Step 1: Start the Colima VM

Start a VM with k3s enabled. Allocate extra resources to accommodate ZFS builds.

```bash
colima start \
  --kubernetes \
  --cpu 4 \
  --memory 8 \
  --disk 40 \
  --kubernetes-version v1.29.0+k3s1
```

Verify the node is ready:

```bash
kubectl get nodes
# NAME      STATUS   ROLES                  AGE   VERSION
# colima    Ready    control-plane,master   1m    v1.29.0+k3s1
```

---

## Step 2: Set Up ZFS Inside the VM

SSH into the Colima VM:

```bash
colima ssh
```

All commands below run inside the VM.

### 2-1. Install ZFS

```bash
sudo apt update
sudo apt install -y zfsutils-linux nfs-kernel-server
```

> DKMS may take a few minutes to build the kernel module.

Verify the installation:

```bash
zfs version
# zfs-2.x.x / zfs-kmod-2.x.x
```

### 2-2. Create a ZFS Pool with a Loopback Device

```bash
sudo mkdir -p /var/lib/branchdb
sudo truncate -s 20G /var/lib/branchdb/zfs.img
sudo losetup /dev/loop0 /var/lib/branchdb/zfs.img

sudo zpool create tank /dev/loop0
sudo zfs create tank/mysql
```

### 2-3. Configure NFS

Allow NFS mounts from the k3s Pod CIDR (default `10.42.0.0/16`):

```bash
sudo systemctl enable --now nfs-server

sudo zfs set sharenfs="rw=@10.42.0.0/16,no_root_squash" tank/mysql

# Verify the export
exportfs -v
# /tank/mysql  10.42.0.0/16(rw,...)
```

### 2-4. Create an Initial Snapshot

Create an empty snapshot to use as the branch base (you can restore real production data later):

```bash
sudo zfs snapshot tank/mysql@initial
sudo zfs list -t snapshot
# NAME                USED  AVAIL  REFER  MOUNTPOINT
# tank/mysql@initial    0B      -     0B  -
```

---

## Step 3: Start the ZFS Agent

### Build the Binary

Install Go if not already available in the VM:

```bash
# Only if Go is not installed
sudo apt install -y golang-go

# Build the ZFS Agent
git clone https://github.com/MaSuCcHI/branchdb-operator.git /tmp/branchdb
cd /tmp/branchdb
go build -o /usr/local/bin/zfsagent ./cmd/zfsagent
```

### Register as a systemd Service

```bash
ZFSAGENT_TOKEN=$(openssl rand -hex 32)
echo "Token (save this for later): $ZFSAGENT_TOKEN"

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

Verify it is running:

```bash
curl -H "Authorization: Bearer ${ZFSAGENT_TOKEN}" http://localhost:9090/health
# {"status":"ok"}

curl -H "Authorization: Bearer ${ZFSAGENT_TOKEN}" http://localhost:9090/snapshots
# [{"name":"initial","created_at":"..."}]
```

Exit the VM:

```bash
exit
```

---

## Step 4: Install BranchDB with Helm

### Get the VM IP Address

```bash
COLIMA_IP=$(colima list -j | jq -r '.[0].address')
echo $COLIMA_IP
# e.g. 192.168.106.2
```

> Install `jq` if needed: `brew install jq`

### Retrieve the ZFS Agent Token

Use the token displayed in Step 3. To look it up again:

```bash
colima ssh -- sudo systemctl cat zfsagent | grep ZFSAGENT_TOKEN
```

### Install with Helm

```bash
ZFSAGENT_TOKEN=<token from Step 3>

helm upgrade --install branchdb \
  ~/sources/branchdb-operator/deploy/helm/branchdb \
  --namespace branchdb-system \
  --create-namespace \
  --set installCRDs=true \
  --set zfsAgent.url=http://${COLIMA_IP}:9090 \
  --set zfsAgent.token=${ZFSAGENT_TOKEN} \
  --set externalHost=${COLIMA_IP}
```

Verify the Pods are running:

```bash
kubectl -n branchdb-system get pods
# NAME                                READY   STATUS    RESTARTS
# branchdb-xxxxxx-xxxxx               1/1     Running   0
# branchdb-api-xxxxxx-xxxxx           1/1     Running   0
```

---

## Step 5: Verify Everything Works

### Access the API Server

```bash
kubectl -n branchdb-system port-forward svc/branchdb-api 8080:8080 &

curl http://localhost:8080/health
# {"status":"ok"}

curl http://localhost:8080/snapshots
# [{"name":"initial","created_at":"..."}]
```

Open the web console in your browser:

```bash
open http://localhost:8080/
```

### Create a Branch

```bash
curl -X POST http://localhost:8080/branches \
  -H 'Content-Type: application/json' \
  -d '{"name":"feature-test","snapshot_ref":"initial","ttl_hours":1}'

# Poll until the port is assigned
until curl -s http://localhost:8080/branches/feature-test | grep -v '"port":0'; do
  sleep 3
done
```

### Connect to MySQL

```bash
DSN=$(curl -s http://localhost:8080/branches/feature-test | jq -r .dsn)
echo $DSN
# root@tcp(192.168.106.2:3XXXX)/

mysql -u root -h ${COLIMA_IP} -P <port from above>
```

### Delete the Branch

```bash
curl -X DELETE http://localhost:8080/branches/feature-test
# 204 No Content
```

---

## Troubleshooting

### ZFS kernel module fails to load

```
cannot open '/dev/zfs': No such file or directory
```

The DKMS build may have failed. Try installing kernel headers manually:

```bash
colima ssh
sudo apt install -y linux-headers-$(uname -r)
sudo modprobe zfs
```

### `/dev/loop0` is already in use

```bash
# Find a free loopback device
colima ssh -- losetup -f
# e.g. /dev/loop1

# Use that device instead of /dev/loop0 in Step 2-2
```

### ZFS pool disappears after VM restart

The loopback device is not persisted across reboots. Re-attach it manually:

```bash
colima ssh
sudo losetup /dev/loop0 /var/lib/branchdb/zfs.img
sudo zpool import tank
sudo systemctl start zfsagent
```

> For automatic recovery on reboot, add the `losetup` command to `/etc/rc.local` or as an `ExecStartPre` in the systemd unit.

---

## Next Steps

- [Quickstart](quickstart.md) — Production-ready setup
- [REST API Reference](../api/rest.md) — Full API specification
- [ZFS Server Setup](zfs-server-setup.md) — Setup with a real disk
