# ZFS Server Setup

The ZFS server is the storage foundation of BranchDB. It serves branch data using ZFS copy-on-write clones over NFS.
The ZFS Agent (`cmd/zfsagent`) runs as an HTTP server and accepts operation requests (clone, snapshot) from the Operator.

## Big Picture

```
[K8s Cluster]                    [ZFS Server (Linux)]
  Operator ──── HTTP :9090 ────→  ZFS Agent
  │                                 └─ zfs clone / snapshot / destroy
  │                                 └─ zfs share  ← NFS export
  │
  │  ← PV(NFS) → Pod mounts the ZFS clone
  │
  Pod(MySQL/PG/Redis)
    └── /var/lib/mysql
         └── NFS mount: zfs-server:/tank/mysql/branches/<branch-name>
```

**Flow when creating a branch:**
1. Operator → ZFS Agent: `POST /clones {"snapshot":"snap-1","name":"feat-x"}`
2. ZFS Agent: `zfs clone tank/mysql@snap-1 tank/mysql/branches/feat-x`
3. ZFS Agent: `zfs share tank/mysql/branches/feat-x` ← enables the NFS export
4. ZFS Agent → Operator: `{"nfs_server":"10.0.0.1","nfs_path":"/tank/mysql/branches/feat-x"}`
5. Operator: creates an NFS PersistentVolume in K8s and mounts it on the Pod

> **NFS is required.** Install `nfs-kernel-server` on the ZFS server and configure the `sharenfs` property (see Step 1 for the procedure).

## Choosing a Setup Method

| Method | Command |
|---|---|
| **Ansible playbook** (recommended) | [→ Build with Ansible](#build-with-the-ansible-playbook) |
| Shell script | `bash deploy/zfs-server/setup.sh` |
| Manual (the steps on this page) | Run Steps 1–3 below in order |

---

## Prerequisites

| Requirement | Description |
|---|---|
| Linux server | Ubuntu 22.04 / Debian 12 recommended |
| `zfsutils-linux` | ZFS kernel module |
| `nfs-kernel-server` | **Required** — used by the ZFS server to export clones over NFS |
| Connectivity from K8s → ZFS server | Port 9090 (ZFS Agent HTTP) and 2049 (NFS) must be reachable |

---

## Step 1: Configure the ZFS Pool and NFS

### 1-1. Create the ZFS pool and dataset

```bash
# Production: use a real disk
zpool create tank /dev/sdb
zfs create tank/mysql

# Test environment (loopback device)
truncate -s 20G /var/lib/branchdb/zfs.img
losetup /dev/loop0 /var/lib/branchdb/zfs.img
zpool create tank /dev/loop0
zfs create tank/mysql
```

### 1-2. Install the NFS kernel server

```bash
apt install -y nfs-kernel-server
systemctl enable --now nfs-server
```

### 1-3. Configure ZFS NFS sharing (important)

**Branches cannot be created without this configuration.**

By setting the `sharenfs` property, every clone created under `tank/mysql`
is automatically exported over NFS.

```bash
# <k8s-pod-cidr> is the Pod CIDR you can check with kubectl cluster-info
# Example: the EKS default is 192.168.0.0/16, the k3s default is 10.42.0.0/16
zfs set sharenfs="rw=@<k8s-pod-cidr>,no_root_squash" tank/mysql

# Verify the setting
zfs get sharenfs tank/mysql
# NAME        PROPERTY  VALUE                          SOURCE
# tank/mysql  sharenfs  rw=@10.42.0.0/16,no_root_squash  local

# Verify the NFS export
exportfs -v
# /tank/mysql  10.42.0.0/16(rw,...)
```

> **Security note:** `no_root_squash` lets root inside the Pod operate on files
> as root on the NFS server. It is required by the MySQL initContainer (`chown`).
> Restrict the range to your K8s Pod CIDR.

### 1-4. Prepare data and take a snapshot

```bash
# Place the MySQL data directory (e.g. rsync existing MySQL data)
# rsync -a /var/lib/mysql/ /tank/mysql/

# Take a snapshot
zfs snapshot tank/mysql@initial

# Verify
zfs list -t snapshot
# NAME                   USED  AVAIL  REFER  MOUNTPOINT
# tank/mysql@initial      1.2G     -  1.2G   -
```

---

## Step 2: Install the ZFS Agent

### Build the binary

```bash
git clone https://github.com/MaSuCcHI/branchdb-operator.git
cd branchdb-operator
go build -o /usr/local/bin/zfsagent ./cmd/zfsagent
```

### Register as a systemd service

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

## Step 3: Verify Operation

```bash
# Health check
curl -H "Authorization: Bearer <token>" http://localhost:9090/health
# {"status":"ok"}

# List snapshots
curl -H "Authorization: Bearer <token>" http://localhost:9090/snapshots
# [{"name":"initial","created_at":"..."}]

# Clone creation test (verify it works all the way through the NFS share)
curl -X POST -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"snapshot":"initial","name":"test-clone"}' \
  http://localhost:9090/clones

# Verify it appears in the NFS exports
exportfs -v | grep test-clone
# /tank/mysql/branches/test-clone ...

# Delete the clone after the test
curl -X DELETE -H "Authorization: Bearer <token>" \
  http://localhost:9090/clones/test-clone
```

---

## Environment Variable Reference

| Variable | Default | Required | Description |
|------|-----------|------|------|
| `ZFSAGENT_ADDR` | `:9090` | | HTTP listen address |
| `ZFSAGENT_TOKEN` | | ✅ | Bearer authentication token (recommended: random string of 32+ characters) |
| `ZFSAGENT_POOL` | `tank` | | ZFS pool name |
| `ZFSAGENT_DATASET` | `mysql` | | ZFS dataset name (do not include the pool name) |

---

## Security

```bash
# Example token generation
openssl rand -hex 32
```

- The ZFS Agent requires root privileges (for ZFS and NFS operations)
- Allow connections only from the K8s cluster
  - Port 9090 (ZFS Agent HTTP): from K8s node IPs
  - Port 2049 (NFS): from the K8s Pod CIDR
- If possible, protect the network with a VPN or WireGuard
- If you need TLS termination, place nginx in front as a reverse proxy

---

## Common Errors

### `zfs share` fails

```
zfs share tank/mysql/branches/feat-x: ZFS file system sharing not set
```

**Cause:** The `sharenfs` property is not configured.

```bash
# Fix
zfs set sharenfs="rw=@<k8s-pod-cidr>,no_root_squash" tank/mysql
```

### A Pod cannot mount NFS

```
mount.nfs: Connection timed out
```

**Things to check:**
- Whether port 2049 is allowed from the K8s Pod CIDR in the ZFS server's iptables
- Whether the path appears in `exportfs -v`
- Whether `showmount -e <zfs-server-ip>` succeeds from a Pod

```bash
# Connectivity check from the K8s cluster
kubectl run -it --rm debug --image=busybox --restart=Never -- \
  showmount -e <zfs-server-ip>
```

### `zfs clone` succeeds but `zfs share` fails

`nfs-server` may not be running.

```bash
systemctl status nfs-server
systemctl start nfs-server
```

---

## Connectivity Check from Kubernetes to the ZFS Server

```bash
kubectl run -it --rm debug --image=curlimages/curl --restart=Never -- \
  curl -H "Authorization: Bearer <token>" http://<zfs-server-ip>:9090/health
```

---

## Build with the Ansible Playbook

This Ansible playbook fully automates everything in manual Steps 1–3.
It is idempotent, so it is safe to run any number of times. Run it against the ZFS server itself with `ansible_connection=local`.

### Preparation

```bash
# Install Ansible on the ZFS server
apt install -y ansible

# Clone the repository
git clone https://github.com/MaSuCcHI/branchdb-operator.git
cd branchdb-operator/deploy/zfs-server/ansible

# Create the variables file
cp vars.yml.example vars.yml
```

Edit `vars.yml` to set values for your environment:

```yaml
# vars.yml (the minimum items you need to change)
zfs_device: /dev/sdb         # disk to use for the ZFS pool
k8s_pod_cidr: 10.42.0.0/16  # check with kubectl cluster-info dump | grep podCIDR
zfsagent_token: "$(openssl rand -hex 32)"  # ★ always change to a strong token
```

### Run

```bash
# Dry run (only shows the changes, does not actually change anything)
sudo ansible-playbook -i inventory.ini playbook.yml -e @vars.yml --check

# Run
sudo ansible-playbook -i inventory.ini playbook.yml -e @vars.yml
```

After completion, output like the following is shown:

```
TASK [Display setup completion message]
ok: [localhost] => {
  "msg": [
    "=== BranchDB ZFS server setup complete ===",
    "",
    "Specify the following when installing with Helm:",
    "  --set zfsAgent.url=http://10.0.0.1:9090",
    "  --set zfsAgent.token=<token>",
    "  --set externalHost=<K8s node IP or LB hostname>"
  ]
}
```

### What Ansible Does

1. Installs `zfsutils-linux`, `nfs-kernel-server`, `git`, and `golang-go`
2. Runs `zpool create` if the ZFS pool does not exist yet
3. Runs `zfs create` if the ZFS dataset does not exist yet
4. Sets `zfs set sharenfs=...` if the `sharenfs` property is not set yet
5. Enables and starts `nfs-server`
6. Clones the repository, then builds and installs the `zfsagent` binary
7. Installs the systemd unit file and enables/starts `zfsagent`

> **Note:** If `zfsagent_token` is set to `changeme`, the playbook continues to run,
> but in production you must always use a strong token generated with `openssl rand -hex 32`.
