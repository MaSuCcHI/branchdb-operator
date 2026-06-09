# Quickstart

This guide takes you from installing BranchDB on a Kubernetes cluster to creating your first branch in 5 minutes.

## Prerequisites

- Kubernetes 1.25 or later (k3s / EKS / GKE / AKS, etc.)
- Helm 3.10 or later
- A ZFS server (complete the [ZFS Server Setup](zfs-server-setup.en.md) first)

> If you use AWS FSx for OpenZFS, the ZFS Agent is not required, but the current version only supports access via the ZFS Agent.

---

## Step 1: Install with Helm

Install the CRD, Operator, and API server with a single command.

```bash
helm upgrade --install branchdb deploy/helm/branchdb \
  --namespace branchdb-system \
  --create-namespace \
  --set installCRDs=true \
  --set zfsAgent.url=http://<zfs-server-ip>:9090 \
  --set zfsAgent.token=<your-token> \
  --set externalHost=<k8s-node-ip-or-lb>
```

| Parameter | Description |
|---|---|
| `zfsAgent.url` | URL of the ZFS Agent (required) |
| `zfsAgent.token` | Authentication token for the ZFS Agent (required) |
| `externalHost` | Hostname / IP used to connect to the branch MySQL via NodePort |

Verify the installation:

```bash
kubectl -n branchdb-system get pods
# NAME                                READY   STATUS    RESTARTS
# branchdb-xxxxxx-xxxxx               1/1     Running   0    ← Operator
# branchdb-api-xxxxxx-xxxxx           1/1     Running   0    ← API server
```

---

## Step 2: Verify Access to the API Server

By default a `ClusterIP` Service is created. Access it with port-forward.

```bash
kubectl -n branchdb-system port-forward svc/branchdb-api 8080:8080 &

curl http://localhost:8080/health
# {"status":"ok"}
```

To access it directly from outside the cluster, see [Expose via Ingress](../02_deploy/ingress.en.md).

---

## Step 3: Check Snapshots

Check that a snapshot to base the branch on exists.

```bash
curl http://localhost:8080/snapshots
# [{"name":"snap-20260101","created_at":"2026-01-01T00:00:00Z"}]
```

If there is no snapshot, you can take one immediately:

```bash
curl -X POST http://localhost:8080/snapshots
```

---

## Step 4: Create a Branch

```bash
curl -X POST http://localhost:8080/branches \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "feature-payment",
    "snapshot_ref": "snap-20260101",
    "ttl_hours": 24
  }'
```

Example response (202 Accepted):

```json
{
  "name": "feature-payment",
  "status": "creating",
  "host": "<node-ip>",
  "port": 0,
  "created_at": "2026-05-28T10:00:00Z",
  "expires_at": "2026-05-29T10:00:00Z"
}
```

While `port: 0`, MySQL is still starting up. Poll until the port is assigned:

```bash
until curl -s http://localhost:8080/branches/feature-payment | grep -v '"port":0'; do
  sleep 3
done
```

---

## Step 5: Connect to MySQL

```bash
# Get the DSN
DSN=$(curl -s http://localhost:8080/branches/feature-payment | jq -r .dsn)
echo $DSN
# root@tcp(<node-ip>:31234)/

# Test the connection
mysql -u root -h <node-ip> -P 31234
```

---

## Step 6: Delete the Branch

```bash
curl -X DELETE http://localhost:8080/branches/feature-payment
# 204 No Content
```

If you set a TTL, the branch is automatically deleted at `expires_at`.

---

## Web Console

Access `/` on the API server to use the management console in your browser.

```bash
open http://localhost:8080/
```

---

## Next Steps

- [Helm Chart Reference](../02_deploy/helm.en.md) — details of every parameter
- [REST API Reference](../04_api/rest.en.md) — the complete API specification
- [Expose via Ingress](../02_deploy/ingress.en.md) — setup for production environments
