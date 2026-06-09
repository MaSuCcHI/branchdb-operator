# Troubleshooting

## Common Problems

---

### A branch stays stuck in Creating

**Diagnosis steps**

```bash
# 1. Check the Operator logs
kubectl -n branchdb-system logs deploy/branchdb -f

# 2. Check the CR status
kubectl describe databasebranch <branch-name>

# 3. Check the Pod status
kubectl -n branchdb-system get pods
kubectl -n branchdb-system describe pod branchdb-mysql-<branch-name>
```

**Causes and solutions**

| Symptom | Cause | Solution |
|---|---|---|
| `status.message: "create clone: ..."` | Connection to the ZFS Agent failed | Check connectivity to `zfsAgent.url` |
| `status.message: "start mysql: ..."` | Pod/PVC creation failed | Check details with `kubectl describe` |
| Pod stays `Pending` | Insufficient node resources | Check events with `kubectl describe pod` |
| Pod is `CrashLoopBackOff` | MySQL failed to start (NFS mount error, etc.) | Check the Pod logs |

---

### Connection to the ZFS Agent fails

```bash
# Connectivity check from the K8s cluster to the ZFS Agent
kubectl run -it --rm debug --image=curlimages/curl --restart=Never -- \
  curl -v http://<zfs-server-ip>:9090/health

# Check directly from the Operator Pod
kubectl -n branchdb-system exec deploy/branchdb -- \
  wget -q -O- http://<zfs-server-ip>:9090/health
```

Things to check:
- Whether the ZFS Agent systemd service is running: `systemctl status zfsagent`
- Whether port 9090 is open in the firewall: `ufw status`
- Whether the value of `zfsAgent.url` is correct: `kubectl -n branchdb-system get deploy branchdb -o yaml | grep ZFSAGENT`

---

### Cannot connect to the NodePort

```bash
# Check the NodePort
kubectl -n branchdb-system get svc branchdb-mysql-<branch-name>
# Shown in the PORT(S) column in a form like 3306:31234/TCP

# Check the node's external IP
kubectl get nodes -o wide

# Test the connection to MySQL
mysql -u root -h <node-ip> -P <nodeport> -e "SELECT 1"
```

Things to check:
- Whether `externalHost` is set to a hostname/IP that is actually reachable
- In cloud environments, whether the NodePort range (30000-32767) is open in the security group/firewall

---

### The Operator does not start

```bash
kubectl -n branchdb-system logs deploy/branchdb
```

| Error message | Cause | Solution |
|---|---|---|
| `ZFSAGENT_URL is required` | `zfsAgent.url` is not set | Add `--set zfsAgent.url=...` |
| `get kubeconfig: ...` | Failed to obtain the kubeconfig | Check the RBAC configuration |
| `register CRD scheme: ...` | The CRD is not installed | Install with `--set installCRDs=true` |

---

### The API server returns 500

```bash
kubectl -n branchdb-system logs deploy/branchdb-api
```

Common causes:
- Insufficient RBAC permissions for the K8s API → `kubectl get clusterrolebinding | grep branchdb`
- `ZFSDB_NAMESPACE` does not match the actual namespace

---

### The CRD is not found

```bash
kubectl get crd databasebranches.branchdb.io
# Error from server (NotFound): ...
```

```bash
# Install the CRD manually
kubectl apply -f deploy/k8s/crd/

# Or install it automatically via helm upgrade
helm upgrade branchdb deploy/helm/branchdb \
  --namespace branchdb-system \
  --set installCRDs=true \
  -f my-values.yaml
```

---

### Branch deletion gets stuck (stays Terminating)

This happens when the finalizer remains. Deletion cannot complete while the Operator is stopped.

```bash
# Check whether the Operator is running
kubectl -n branchdb-system get pods

# Force delete (only when the Operator cannot be recovered)
kubectl patch databasebranch <branch-name> \
  -p '{"metadata":{"finalizers":[]}}' \
  --type=merge
```

> **Note:** Force deletion may leave the ZFS clone or MySQL Pod behind. Manual cleanup is required.

---

## Checking Logs

```bash
# Operator logs (detailed)
kubectl -n branchdb-system logs deploy/branchdb -f --timestamps

# API server logs
kubectl -n branchdb-system logs deploy/branchdb-api -f

# Logs from a previous crash
kubectl -n branchdb-system logs deploy/branchdb --previous

# ZFS Agent logs (on the ZFS server)
journalctl -u zfsagent -f
```

---

## Diagnostic Command Summary

```bash
# Check the state of all resources
kubectl -n branchdb-system get all

# List DatabaseBranches (with phase and NodePort)
kubectl get databasebranches

# Operator Lease (check the leader)
kubectl get lease -n branchdb-system

# Events (recent events)
kubectl -n branchdb-system get events --sort-by='.lastTimestamp' | tail -20
```
