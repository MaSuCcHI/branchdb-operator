# BranchDB Documentation

BranchDB is a MySQL branch management system that runs on Kubernetes.
Using ZFS copy-on-write clones, it provisions an isolated MySQL environment for every pull request in seconds.

---

## Documentation Structure

### Getting Started

| Page | Contents |
|--------|------|
| [Local Development (macOS + Colima)](01_getting-started/local-dev-colima.en.md) | Run ZFS + k3s on a single Mac and try it out right away |
| [Quickstart](01_getting-started/quickstart.en.md) | Install with Helm in 5 minutes, then create a branch |
| [ZFS Server Setup](01_getting-started/zfs-server-setup.en.md) | How to deploy the Agent on a ZFS server |

### Deploy

| Page | Contents |
|--------|------|
| [Helm Chart Reference](02_deploy/helm.en.md) | Details of every values parameter and configuration examples |
| [Expose via Ingress](02_deploy/ingress.en.md) | HTTPS exposure with Nginx / Traefik / ALB |
| [Upgrade Guide](02_deploy/upgrade.en.md) | Upgrade procedures between versions and breaking changes |

### Architecture

| Page | Contents |
|--------|------|
| [System Overview](03_architecture/overview.en.md) | Component layout and data flow |
| [DatabaseBranch CRD Reference](03_architecture/crd-spec.en.md) | Full definition of all spec/status fields |
| [Operator Lifecycle](03_architecture/operator-lifecycle.en.md) | Reconciler state transitions and processing flow |

### API (REST API)

| Page | Contents |
|--------|------|
| [REST API Reference](04_api/rest.en.md) | Request/response specification for every endpoint |

### Operations

| Page | Contents |
|--------|------|
| [Troubleshooting](05_operations/troubleshooting.en.md) | Common problems and how to solve them |
| [Monitoring](05_operations/monitoring.en.md) | Prometheus metrics and health checks |

### Other

| Page | Contents |
|--------|------|
| [Roadmap](roadmap.en.md) | Planned features (authentication, quota management, FSx support, etc.) |

---

## Minimal Setup

```bash
helm upgrade --install branchdb deploy/helm/branchdb \
  --namespace branchdb-system --create-namespace \
  --set installCRDs=true \
  --set zfsAgent.url=http://<zfs-server>:9090 \
  --set zfsAgent.token=<token> \
  --set externalHost=<node-ip>
```

See the [Quickstart](01_getting-started/quickstart.en.md) for details.
