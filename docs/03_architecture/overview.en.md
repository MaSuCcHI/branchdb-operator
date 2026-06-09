# System Architecture Overview

## Component Layout

```
┌─────────────────────────────────────────────────────────────┐
│  Client                                                      │
│  CI/CD (GitHub Actions, etc.) / branchctl / browser         │
└──────────────┬──────────────────────────────────────────────┘
               │ HTTP :8080
               ▼
┌─────────────────────────────────────────────────────────────┐
│  branchdb (API server)              Kubernetes Cluster      │
│                                                             │
│  REST API + Web console (SPA)                               │
│  CRUD of DatabaseBranch CR ← → K8s API server               │
└──────────────┬──────────────────────────────────────────────┘
               │ watch / CRUD
               ▼
┌─────────────────────────────────────────────────────────────┐
│  K8s API server                                             │
│  Holds the DatabaseBranch CRD                               │
└──────────────┬──────────────────────────────────────────────┘
               │ watch
               ▼
┌─────────────────────────────────────────────────────────────┐
│  Operator (controller-runtime)                              │
│                                                             │
│  Reconciler                                                 │
│    ├─ VolumeProvider ──────────────→ ZFS Agent (HTTP)      │
│    │    └─ ZFSAgentProvider              ↓                 │
│    │                               zfs clone / snapshot    │
│    └─ BranchMySQLProvider                                   │
│         └─ K8sMySQLProvider ──→ Pod + PVC + NodePort Svc   │
└─────────────────────────────────────────────────────────────┘
               │ NodePort
               ▼
┌─────────────────────────────────────────────────────────────┐
│  Branch MySQL (one Pod per branch)                          │
│  mysql:8.0 + NFS mount (ZFS clone)                          │
└─────────────────────────────────────────────────────────────┘
```

---

## Responsibilities of Each Component

### branchdb (API server)

- `cmd/branchdb/main.go`
- Translates the user's HTTP requests into CRUD on the `DatabaseBranch` CR
- Connects to the ZFS Agent to perform snapshot operations (when `ZFSDB_ZFSAGENT_URL` is set)
- Serves the Web console (SPA) at `/`
- Broadcasts CR changes in real time over WebSocket

### Operator

- `cmd/operator/main.go`
- Watches the `DatabaseBranch` CR and manages the actual resources (Volume, Pod, Service)
- Uses controller-runtime and operates with declarative state management (the Reconcile loop)
- Implementations are swappable through two interfaces: **VolumeProvider** and **BranchMySQLProvider**

### ZFS Agent

- `cmd/zfsagent/main.go`
- A lightweight HTTP server running on the ZFS storage server
- Receives HTTP requests from the Operator and runs `zfs` commands
- Authentication uses a simple Bearer token scheme

---

## Data Flow: Creating a Branch

```
1. POST /branches {"name":"feat-x","snapshot_ref":"snap-1","ttl_hours":24}
       ↓ API server
2. Create the DatabaseBranch CR
       ↓ K8s API server (stored in etcd)
       ↓ Operator watches
3. Reconciler starts
   a. Add finalizer
   b. status.phase = Creating
   c. VolumeProvider.CreateClone("snap-1", "feat-x")
         → ZFS Agent: POST /clones {"snapshot":"tank/mysql@snap-1","clone":"tank/mysql/branches/feat-x"}
         → ZFS server: zfs clone tank/mysql@snap-1 tank/mysql/branches/feat-x
         ← VolumeInfo{NFSServer, NFSPath}
   d. BranchMySQLProvider.Start("feat-x", volumeInfo)
         → Create PersistentVolume (NFS)
         → Create PersistentVolumeClaim
         → Create Pod (mysql:8.0 + fix-permissions initContainer)
         → Create NodePort Service
         → K8s auto-assigns the NodePort
   e. status.externalPort = <NodePort>
   f. status.phase = Ready
       ↓
4. GET /branches/feat-x → {status:"ready", port:31234, dsn:"root@tcp(...:31234)/"}
```

---

## Interface Design (Pro Extension Points)

```go
// internal/domain/volume.go
type VolumeProvider interface {
    TakeSnapshot(ctx, name) error
    CreateClone(ctx, snapshot, clone) (VolumeInfo, error)
    DeleteClone(ctx, clone) error
    ListSnapshots(ctx) ([]SnapshotInfo, error)
}

// internal/domain/branchmysql.go
type BranchMySQLProvider interface {
    Start(ctx, branch, volumeInfo) (BranchEndpoint, error)
    Stop(ctx, branch) error
}
```

Future Pro features (OIDC authentication, multi-tenancy, quota management) are injected through these interfaces or as HTTP middleware. No build tags are used.

---

## Related Documents

- [DatabaseBranch CRD Reference](crd-spec.en.md)
- [Operator Lifecycle](operator-lifecycle.en.md)
