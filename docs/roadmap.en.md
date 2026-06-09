# Roadmap

---

## Implemented ✅

### MySQL Branches
Create, delete, and TTL-based auto-deletion of MySQL 8.0 branches via ZFS clones + NFS PVs.

### PostgreSQL Branches
PostgreSQL 16 branches can be created by specifying `database_type: postgres`.
Performance over NFS is maintained with `fsync=off / synchronous_commit=off / full_page_writes=off`.

### Multi-dataset ZFS Agent
A single process manages multiple datasets via `ZFSAGENT_DATASETS=mysql:tank/mysql,postgres:tank/postgres`.
The target dataset for an operation is switched with the `?db_type=` query parameter.

### Web Console
A SPA (React + TypeScript) for listing, creating, and deleting branches, and viewing Pod status and metrics.

---

## Partially Implemented 🚧

### Redis Branches
The `dbConfig` definition is complete (`redis:7`, readiness check via `redis-cli ping`).
Multi-dataset configuration for the ZFS Agent and E2E tests are not yet supported.

---

## Planned 📋

### Authentication & Authorization

**Current state:** The API server has no authentication.
Protect it with network-level restrictions (K8s NetworkPolicy, IP restrictions on the Ingress, etc.).

**Plan:**
- Add OIDC / JWT authentication middleware via interface injection
- Integration with GitHub OIDC (token-free access from GitHub Actions)
- Implemented by adding `WithAuthMiddleware(...)` to `K8sBranchHandler`

---

### Multi-tenancy

**Current state:** All users can create branches in the same namespace.

**Plan:**
- Branch isolation per user/team (namespace- or label-based)
- Attach owner information to the `DatabaseBranch` CR

---

### Quota Management

**Current state:** There is no limit on the number of branches.

**Plan:**
- A ConfigMap/CRD to configure the maximum number of branches per user/team
- Perform a quota check in the Reconcile loop and set the `Error` phase when exceeded

```yaml
# Example future configuration (not yet implemented)
apiVersion: branchdb.io/v1alpha1
kind: BranchQuota
metadata:
  name: team-payments
spec:
  maxBranches: 10
  selector:
    matchLabels:
      team: payments
```

---

### AWS FSx Support

**Current state:** Only the ZFS Agent (ZFS + NFS server) is supported.

**Plan:**
- An FSx implementation of the `VolumeProvider` interface (`infrastructure/fsx/`)
- Clone ZFS snapshots via the FSx API and serve them to Pods over EFS/NFS mounts
- Authentication flow with EKS + IRSA

---

### Helm Repository

**Current state:** On tag push (`v*.*.*`), the Helm chart is automatically published to GHCR as an OCI artifact.

```bash
helm install branchdb oci://ghcr.io/masucchi/charts/branchdb --version <version>
```

Installation from the local path `deploy/helm/branchdb` is still possible.

---

### branchctl CLI

**Plan:**
- `branchctl create --wait` to create a branch and wait for the connection in a single command
- `branchctl status <branch>` to display Pod status and connection count
- Manage connection targets in `~/.config/branchctl/config.yaml`
