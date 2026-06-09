# Operator Lifecycle

## State Transition Diagram

```
                    CR created
                      │
                      ▼
                  [Pending]
                      │ Reconcile starts
                      ▼
              add finalizer
                      │
                      ▼
                  [Creating]
                 /          \
       success  /            \  failure
              ▼                ▼
           [Ready]           [Error]
              │                │ re-Reconcile (auto retry)
              │ TTL expired    │
              ▼                │
       CR delete request ←─────┘
              │
              ▼
           [Deleting]
              │ cleanup complete
              ▼
        CR deleted (etcd)
```

---

## Reconcile Loop Details

Implemented by the `Reconcile()` method in `internal/interface/operator/reconciler.go`.

### 1. Fetch the CR

```go
if err := r.Get(ctx, req.NamespacedName, branch); err != nil {
    return ctrl.Result{}, client.IgnoreNotFound(err)
}
```

If the CR does not exist (already deleted), finish normally.

### 2. Deletion Handling (when DeletionTimestamp is set)

Call `handleDeletion()`:

1. `status.phase = Deleting`
2. `MySQLProvider.Stop()` — delete the Pod/PVC/Service/ConfigMap
3. `VolumeProvider.DeleteClone()` — delete the ZFS clone
4. Remove the finalizer → the CR disappears from etcd

If any step fails, set `status.phase = Error` and return the error.
controller-runtime retries with exponential backoff.

### 3. TTL Check

```go
if branch.Status.ExpiresAt != nil && branch.Status.Phase == Ready {
    if time.Now().After(branch.Status.ExpiresAt.Time) {
        r.Delete(ctx, branch)   // → DeletionTimestamp gets set
    }
}
```

Ready branches whose TTL has expired are deleted automatically.

### 4. Add the finalizer

If the CR does not have `branchdb.io/finalizer`, add it and re-fetch (to update the resource version).

### 5. Ready Check (skip)

If `status.phase == Ready`, schedule the TTL check with `RequeueAfter: 10 minutes` and finish.

### 6. Creation Flow

For a new CR (or recovery from Error):

```
status.phase = Creating
    ↓
VolumeProvider.CreateClone(snapshotRef, branchName)
    ↓ failure → status.phase = Error, return error
    ↓ success → VolumeInfo{NFSServer, NFSPath}
    ↓
MySQLProvider.Start(branchName, volumeInfo)
    ↓ failure → status.phase = Error, return error
    ↓ success → BranchEndpoint{Host, Port, ExternalPort}
    ↓
status.clusterHost = endpoint.Host
status.clusterPort = endpoint.Port
status.externalHost = r.ExternalHost
status.externalPort = endpoint.ExternalPort
status.expiresAt = now + ttlHours
status.phase = Ready
```

---

## Retry Strategy

When Reconcile returns an error, controller-runtime retries with exponential backoff:

- 1st: immediately
- 2nd: after 1 second
- 3rd: after 2 seconds
- Thereafter: doubling up to a maximum of 10 minutes

`status.phase = Error` is set, but because the Operator retries automatically,
transient network errors and the like recover on their own.

---

## Leader Election

With `--leader-elect=true` (the default), only one of the Operator Pods is actively running
even when multiple are started. It is managed with a Lease resource (`branchdb.io`).

```bash
# Check the leader
kubectl get lease branchdb.io -n branchdb-system -o yaml
```
