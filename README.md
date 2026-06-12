# BranchDB

[![CI](https://github.com/MaSuCcHI/branchdb-operator/actions/workflows/ci.yml/badge.svg)](https://github.com/MaSuCcHI/branchdb-operator/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26-00ADD8.svg)](go.mod)
[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/branch-db)](https://artifacthub.io/packages/search?repo=branch-db)

**Kubernetes operator that gives every pull request its own database branch in seconds.**

Each `DatabaseBranch` CR spins up an isolated database instance (MySQL or PostgreSQL) backed by a ZFS clone. CI/CD pipelines get a fresh database with real production data — no shared fixtures, no seed scripts.

```
git push origin feature/payment-v2
→ DatabaseBranch CR created
→ Operator clones ZFS snapshot → starts MySQL/PostgreSQL Pod
→ NodePort assigned
→ Tests run against mysql://branchdb.company.com:31234/
→ PR merged → CR deleted → resources cleaned up
```

---

## Features

- **One CR = one database** — `DatabaseBranch` CRD maps directly to a Pod + PVC + NodePort Service
- **MySQL & PostgreSQL** — specify `database_type: mysql` or `database_type: postgres` per branch
- **ZFS clones** — instant branch creation via copy-on-write (ZFS Agent)
- **TTL** — branches self-destruct after `ttlHours`; no manual cleanup
- **Web console** — browser UI at `/` for branch management, Pod status, metrics
- **REST API** — `POST /branches`, `GET /branches/{name}`, `GET /stats`, etc.
- **WebSocket** — real-time event stream at `/ws`

---

## Quick Start

> **Want to try it on your Mac right now?**
> See [Local Development Guide (macOS + Colima)](docs/01_getting-started/local-dev-colima.en.md) — no real disk or cloud account required.

### Prerequisites

- Kubernetes cluster (k3s, EKS, GKE, …)
- ZFS server running [ZFS Agent](#zfs-agent)

### 1. Install with Helm

CRD, Operator, and API server are all installed with a single command:

```bash
helm upgrade --install branchdb deploy/helm/branchdb \
  --namespace branchdb-system --create-namespace \
  --set installCRDs=true \
  --set zfsAgent.url=http://<zfs-server>:9090 \
  --set zfsAgent.token=<token> \
  --set externalHost=<node-ip-or-lb>
```

To expose the API server externally (skip for in-cluster Ingress setups):

```bash
# LoadBalancer (cloud)
--set apiServer.service.type=LoadBalancer

# NodePort (bare-metal / local)
--set apiServer.service.type=NodePort
```

### 2. Create a branch

```bash
# MySQL branch (default)
curl -X POST http://<api-server>:8080/branches \
  -H 'Content-Type: application/json' \
  -d '{"name":"feature-payment","snapshot_ref":"base","ttl_hours":24}'

# PostgreSQL branch
curl -X POST http://<api-server>:8080/branches \
  -H 'Content-Type: application/json' \
  -d '{"name":"feature-payment","snapshot_ref":"base","ttl_hours":24,"database_type":"postgres"}'
```

---

## Architecture

```
[CI/CD / branchctl]
        │ HTTP :8080
        ▼
┌─────────────────────┐
│   API Server         │  REST API + Web Console
│   (cmd/branchdb)     │  DatabaseBranch CR CRUD
└──────────┬──────────┘
           │ watch / CRUD
           ▼
┌─────────────────────┐
│   K8s API Server     │
└──────────┬──────────┘
           │ watch
           ▼
┌─────────────────────┐
│   Operator           │  controller-runtime Reconciler
│   (cmd/operator)     │
│   ├─ VolumeProvider      │──→ ZFS Agent
│   └─ DatabaseProvider   │──→ Pod + PVC + NodePort Service
└─────────────────────┘
```

### ZFS Agent

A lightweight HTTP server (`cmd/zfsagent`) that runs on the ZFS storage server and executes snapshot/clone operations on behalf of the Operator.

```bash
# Single dataset (MySQL only)
ZFSAGENT_TOKEN=secret ZFSAGENT_POOL=tank ZFSAGENT_DATASET=mysql \
  ./bin/zfsagent

# Multi-dataset (MySQL + PostgreSQL)
ZFSAGENT_TOKEN=secret \
  ZFSAGENT_DATASETS=mysql:tank/mysql,postgres:tank/postgres \
  ./bin/zfsagent
```

---

## REST API

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/branches` | Create branch (returns 202 immediately) |
| `GET` | `/branches` | List all branches |
| `GET` | `/branches/{name}` | Get branch status + DSN |
| `DELETE` | `/branches/{name}` | Delete branch |
| `GET` | `/branches/{name}/pod` | Pod phase / Ready |
| `GET` | `/branches/{name}/metrics` | MySQL `Threads_connected` |
| `GET` | `/stats` | Phase counts (total/ready/creating/error) |
| `GET` | `/snapshots` | List snapshots |
| `POST` | `/snapshots` | Take snapshot now |
| `GET` | `/health` | Health check |
| `GET` | `/` | Web console (SPA) |

---

## Development

```bash
# Run tests
make test

# Check coverage (≥95% required)
make cover

# Build binaries → bin/
make build

# Build web console → internal/interface/api/k8s-dist/
make web-build

# Regenerate CRD manifests
make manifests
```

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `ZFSDB_LISTEN_ADDR` | `:8080` | API server listen address |
| `ZFSDB_EXTERNAL_HOST` | `localhost` | NodePort external hostname / IP |
| `ZFSDB_NAMESPACE` | `default` | Namespace for DatabaseBranch CRs |
| `ZFSDB_ZFSAGENT_URL` | *(unset)* | ZFS Agent URL — enables snapshot API |
| `ZFSDB_ZFSAGENT_TOKEN` | *(unset)* | ZFS Agent auth token |
| `ZFSDB_API_TOKEN` | *(unset)* | Static Bearer token for the branchdb REST API. When set, all API routes (except `/health`) require `Authorization: Bearer <token>`. Omit for backwards-compatible unauthenticated mode. |
| `ZFSDB_BRANCH_AUTH` | *(unset)* | Set to `generated` to have the Operator create a random password per branch and store it in a `branchdb-cred-<branch>` Secret. The password appears in the API response DSN. Defaults to unauthenticated (`MYSQL_ALLOW_EMPTY_PASSWORD`, PostgreSQL `trust`). |
| `ZFSAGENT_ADDR` | `:9090` | ZFS Agent listen address |
| `ZFSAGENT_TOKEN` | *(required)* | ZFS Agent auth token |
| `ZFSAGENT_POOL` | `tank` | ZFS pool name (single-dataset mode) |
| `ZFSAGENT_DATASET` | `mysql` | ZFS dataset name (single-dataset mode) |
| `ZFSAGENT_DATASETS` | *(unset)* | Multi-dataset map, e.g. `mysql:tank/mysql,postgres:tank/postgres` |

### Security Considerations

By default, branch databases are exposed via NodePort **without a password** (MySQL uses `MYSQL_ALLOW_EMPTY_PASSWORD=yes`, PostgreSQL uses `trust` authentication). This is intentional for frictionless development workflows, but it means **any host that can reach the NodePort can connect without credentials**.

**Recommendations for shared or semi-public environments:**

- Set `ZFSDB_BRANCH_AUTH=generated` on the Operator to enable per-branch random passwords. The password is stored in a `branchdb-cred-<branch>` Kubernetes Secret (with ownerRef for automatic cleanup) and is returned as part of the connection DSN in the API response.
- Set `ZFSDB_API_TOKEN=<strong-random-token>` on the branchdb API server to protect the REST API (branch create/delete, snapshot reset, GC). Without this, anyone who can reach the API endpoint can perform destructive operations.
- Consider using a NetworkPolicy to restrict NodePort access to trusted CIDR ranges.

---

## Sponsors

BranchDB is free and open source. If it saves you time, consider sponsoring to keep it maintained and growing.

**[❤️ Sponsor on GitHub](https://github.com/sponsors/MaSuCcHI)**

<!-- sponsors -->
<!-- sponsors -->

---

## License

MIT — see [LICENSE](LICENSE).
