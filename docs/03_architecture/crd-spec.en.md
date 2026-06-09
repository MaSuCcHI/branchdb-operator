# DatabaseBranch CRD Reference

## Overview

`DatabaseBranch` is a Kubernetes custom resource that represents a database branch managed by BranchDB.

```
apiVersion: branchdb.io/v1alpha1
kind: DatabaseBranch
```

- **Scope:** Cluster-scoped (no namespace)
- **Group:** `branchdb.io`
- **Version:** `v1alpha1`

---

## spec

| Field | Type | Required | Description |
|---|---|---|---|
| `snapshotRef` | string | ✅ | Name of the base snapshot (the name of a snapshot managed by the ZFS Agent) |
| `ttlHours` | integer | | Lifetime of the branch (hours). `0` or omitted means no expiry. |
| `databaseType` | string | | Type of database to start. `mysql` (default) / `postgres` / `redis`. |
| `databaseVersion` | string | | Override of the container image tag. When omitted, the Operator's default is used (`mysql:8.0`, `postgres:16`, `redis:7`). |
| `initSQL` | string | | Initialization SQL run after the branch MySQL starts (planned) |

---

## status

| Field | Type | Description |
|---|---|---|
| `phase` | string | The current phase (see below) |
| `clusterHost` | string | Internal cluster DNS name (e.g. `feat-x.branchdb.svc.cluster.local`) |
| `clusterPort` | integer | Internal cluster port (always `3306`) |
| `externalHost` | string | Hostname for external connections (the value of the `externalHost` values) |
| `externalPort` | integer | The NodePort assigned by K8s (`30000`–`32767`) |
| `message` | string | Detailed message during the error phase |
| `expiresAt` | datetime | Expiry based on TTL (set only when `ttlHours > 0`) |

### phase Values

| phase | Meaning |
|---|---|
| `Pending` | Just after the CR is created, before the Operator starts processing |
| `Creating` | Creating the Volume clone and the MySQL Pod |
| `Ready` | MySQL has started and is connectable |
| `Error` | An error occurred during creation/deletion |
| `Deleting` | The CR is being deleted (the finalizer is processing) |

---

## Sample Manifests

### MySQL (default)

```yaml
apiVersion: branchdb.io/v1alpha1
kind: DatabaseBranch
metadata:
  name: feature-payment-v2
spec:
  snapshotRef: snap-20260101
  ttlHours: 24
  # databaseType: mysql  ← mysql is used even when omitted
```

### PostgreSQL

```yaml
apiVersion: branchdb.io/v1alpha1
kind: DatabaseBranch
metadata:
  name: feature-pg-branch
spec:
  snapshotRef: snap-20260101
  databaseType: postgres
  databaseVersion: "16"   # postgres:16 when omitted
  ttlHours: 24
```

### Redis

```yaml
apiVersion: branchdb.io/v1alpha1
kind: DatabaseBranch
metadata:
  name: feature-redis-cache
spec:
  snapshotRef: snap-20260101
  databaseType: redis
  ttlHours: 8
```

### Pinning a Version

```yaml
spec:
  snapshotRef: snap-20260101
  databaseType: mysql
  databaseVersion: "8.4"   # uses mysql:8.4
```

### No TTL (kept until manual deletion)

```yaml
apiVersion: branchdb.io/v1alpha1
kind: DatabaseBranch
metadata:
  name: staging-persistent
spec:
  snapshotRef: snap-20260101
  ttlHours: 0
```

---

## kubectl Commands

```bash
# List (shows Phase and ExternalPort)
kubectl get databasebranches

# Show details
kubectl describe databasebranch feature-payment-v2

# Check the status
kubectl get databasebranch feature-payment-v2 -o jsonpath='{.status}'

# Get the DSN
kubectl get databasebranch feature-payment-v2 \
  -o jsonpath='root@tcp({.status.externalHost}:{.status.externalPort})/'

# Delete
kubectl delete databasebranch feature-payment-v2
```

---

## finalizer

BranchDB attaches a finalizer named `branchdb.io/finalizer` to the CR.
When a delete request for the CR arrives, the Operator runs the cleanup in the following order and then removes the finalizer:

1. `status.phase = Deleting`
2. `BranchMySQLProvider.Stop()` — delete the Pod/PVC/Service
3. `VolumeProvider.DeleteClone()` — delete the ZFS clone
4. Remove the finalizer → the CR disappears from etcd

If the Operator stops while the finalizer remains, Reconcile resumes after it restarts.
