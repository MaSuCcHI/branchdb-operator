# Helm Chart Reference

The BranchDB Helm chart `deploy/helm/branchdb` manages the CRD, Operator, and API server in a single chart.

## Installation

The chart is published on an OCI registry (GitHub Packages).

```bash
helm upgrade --install branchdb oci://ghcr.io/masucchi/charts/branchdb \
  --version <version> \
  --namespace branchdb-system \
  --create-namespace \
  --set installCRDs=true \
  --set zfsAgent.url=http://<zfs-server>:9090 \
  --set zfsAgent.token=<token> \
  --set externalHost=<node-ip-or-lb>
```

> If you omit `--version`, the latest stable version is used. For available versions, see
> [Packages](https://github.com/MaSuCcHI/branchdb-operator/pkgs/container/charts%2Fbranchdb).

Using a values file (recommended):

```bash
helm upgrade --install branchdb oci://ghcr.io/masucchi/charts/branchdb \
  --version <version> \
  --namespace branchdb-system \
  --create-namespace \
  -f my-values.yaml
```

You can also install directly from a local source tree:

```bash
helm upgrade --install branchdb deploy/helm/branchdb \
  --namespace branchdb-system --create-namespace -f my-values.yaml
```

---

## Full values.yaml Parameter Reference

### CRD

| Parameter | Default | Description |
|---|---|---|
| `installCRDs` | `true` | When `true`, create/update the CRD on both `helm install` and `upgrade`. Set to `false` to protect an existing CRD. |

> **Note:** When `installCRDs: false`, install the CRD manually in advance.
> `kubectl apply -f deploy/k8s/crd/`

---

### Operator

| Parameter | Default | Description |
|---|---|---|
| `replicaCount` | `1` | Number of Operator Pods. Multiple replicas are allowed when leader election is enabled. |
| `image.repository` | `ghcr.io/masucchi/branchdb-operator` | Operator container image |
| `image.tag` | `""` | Image tag (empty = `Chart.appVersion`) |
| `image.pullPolicy` | `IfNotPresent` | Image pull policy |
| `imagePullSecrets` | `[]` | List of Secret names for a private registry |
| `nameOverride` | `""` | Override the chart name |
| `fullnameOverride` | `""` | Fully override resource names |

---

### Common BranchDB Settings

| Parameter | Default | Description |
|---|---|---|
| `externalHost` | `""` | External hostname / IP used to connect to branch databases via NodePort. Used by both the Operator and the API server. |
| `branchNamespace` | `""` | Namespace in which to create branch Pods/PVCs/Services. Empty uses the Release namespace. |
| `databases.mysql.image` | `""` | MySQL image override (empty = `mysql:8.0`) |
| `databases.postgres.image` | `""` | PostgreSQL image override (empty = `postgres:16`) |
| `databases.redis.image` | `""` | Redis image override (empty = `redis:7`) |

---

### ZFS Agent

| Parameter | Default | Description |
|---|---|---|
| `zfsAgent.url` | `""` | **(required)** Base URL of the ZFS Agent. Example: `http://zfs-server.internal:9090` |
| `zfsAgent.token` | `""` | Bearer token for the ZFS Agent. A Secret is created automatically when specified. |
| `zfsAgent.existingSecret` | `""` | Name of an existing Secret to use (key: `zfsagent-token`). Takes precedence over `token`. |

Example of creating a Secret when using `existingSecret`:

```bash
kubectl create secret generic my-zfsagent-secret \
  --from-literal=zfsagent-token=<token> \
  -n branchdb-system
```

```yaml
# values.yaml
zfsAgent:
  url: "http://zfs-server.internal:9090"
  existingSecret: "my-zfsagent-secret"
```

---

### Leader Election

| Parameter | Default | Description |
|---|---|---|
| `leaderElection.enabled` | `true` | Leader election for multiple replicas. May be `false` when `replicaCount: 1`. |

---

### Service Account / RBAC

| Parameter | Default | Description |
|---|---|---|
| `serviceAccount.create` | `true` | Whether to create a ServiceAccount |
| `serviceAccount.name` | `""` | ServiceAccount name (auto-generated when empty) |
| `serviceAccount.annotations` | `{}` | Annotations applied to the ServiceAccount. Used for IAM roles, etc. |
| `rbac.create` | `true` | Whether to create the ClusterRole / ClusterRoleBinding |

When using IAM Roles for Service Accounts (IRSA) on EKS:

```yaml
serviceAccount:
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::<account-id>:role/branchdb-role
```

---

### Operator Ports / Service

| Parameter | Default | Description |
|---|---|---|
| `ports.metrics` | `8080` | Prometheus metrics port |
| `ports.health` | `8081` | Health check port (`/healthz`, `/readyz`) |
| `service.enabled` | `true` | Whether to create a Service for the Operator's metrics |
| `service.type` | `ClusterIP` | Type of the metrics Service |

---

### Operator Resources

| Parameter | Default | Description |
|---|---|---|
| `resources.requests.cpu` | `100m` | CPU request |
| `resources.requests.memory` | `128Mi` | Memory request |
| `resources.limits.cpu` | `500m` | CPU limit |
| `resources.limits.memory` | `512Mi` | Memory limit |
| `podAnnotations` | `{}` | Pod annotations |
| `podLabels` | `{}` | Additional Pod labels |
| `nodeSelector` | `{}` | Node selector |
| `tolerations` | `[]` | Taint tolerations |
| `affinity` | `{}` | Affinity rules |
| `extraEnv` | `[]` | Additional environment variables (`name`/`value` form) |

---

### API Server

| Parameter | Default | Description |
|---|---|---|
| `apiServer.enabled` | `true` | Whether to enable the API server |
| `apiServer.image.repository` | `ghcr.io/masucchi/branchdb` | API server image |
| `apiServer.image.tag` | `""` | Image tag (empty = `Chart.appVersion`) |
| `apiServer.image.pullPolicy` | `IfNotPresent` | Image pull policy |
| `apiServer.replicaCount` | `1` | Number of replicas |
| `apiServer.listenPort` | `8080` | HTTP listen port |
| `apiServer.service.enabled` | `true` | Whether to create a Service |
| `apiServer.service.type` | `ClusterIP` | Service type (`ClusterIP` / `LoadBalancer` / `NodePort`) |
| `apiServer.service.nodePort` | `""` | NodePort number (when `type: NodePort`; auto-assigned when empty) |
| `apiServer.resources` | *(see below)* | Resource requests/limits |
| `apiServer.podAnnotations` | `{}` | Pod annotations |
| `apiServer.nodeSelector` | `{}` | Node selector |
| `apiServer.tolerations` | `[]` | Tolerations |
| `apiServer.affinity` | `{}` | Affinity |

Default resources for the API server:

```yaml
apiServer:
  resources:
    requests:
      cpu: 100m
      memory: 128Mi
    limits:
      cpu: 500m
      memory: 256Mi
```

---

## Configuration Examples

### Minimal Setup (development / testing)

```yaml
# dev-values.yaml
installCRDs: true
externalHost: "192.168.1.100"
zfsAgent:
  url: "http://192.168.1.200:9090"
  token: "dev-token"
leaderElection:
  enabled: false
apiServer:
  service:
    type: NodePort
```

### Production Setup (EKS + LoadBalancer)

```yaml
# prod-values.yaml
installCRDs: true
externalHost: "branchdb.example.com"
zfsAgent:
  url: "http://zfs-agent.internal:9090"
  existingSecret: "zfsagent-credentials"
replicaCount: 2
leaderElection:
  enabled: true
serviceAccount:
  annotations:
    eks.amazonaws.com/role-arn: "arn:aws:iam::123456789:role/branchdb"
resources:
  requests:
    cpu: 200m
    memory: 256Mi
  limits:
    cpu: 1000m
    memory: 1Gi
apiServer:
  replicaCount: 2
  service:
    type: LoadBalancer
  resources:
    requests:
      cpu: 200m
      memory: 256Mi
    limits:
      cpu: 500m
      memory: 512Mi
```

### Setup Using Ingress

```yaml
# ingress-values.yaml
installCRDs: true
externalHost: "branchdb.example.com"
zfsAgent:
  url: "http://zfs-agent.internal:9090"
  token: "secret"
apiServer:
  service:
    type: ClusterIP   # ClusterIP is enough because the Ingress handles routing
```

For Ingress configuration, see [Expose via Ingress](ingress.en.md).

---

## Inspecting Templates (Dry Run)

```bash
# List resource names
helm template branchdb deploy/helm/branchdb \
  --set zfsAgent.url=http://zfs:9090 \
  --set zfsAgent.token=token \
  | grep -E "^kind:|^  name:"

# Inspect a specific resource in detail
helm template branchdb deploy/helm/branchdb \
  --set zfsAgent.url=http://zfs:9090 \
  -s templates/apiserver.yaml

# lint
helm lint deploy/helm/branchdb
```

---

## Upgrade

See the [Upgrade Guide](upgrade.en.md) for details.

```bash
# Upgrade including the CRD
helm upgrade branchdb deploy/helm/branchdb \
  --namespace branchdb-system \
  --set installCRDs=true \
  -f my-values.yaml
```

## Uninstall

```bash
helm uninstall branchdb -n branchdb-system
```

> **Note:** Because of the `helm.sh/resource-policy: keep` annotation, the CRD remains after uninstall.
> To remove the CRD, run it manually:
> `kubectl delete crd databasebranches.branchdb.io`
> **Deleting the CRD also loses all DatabaseBranch resources.**
