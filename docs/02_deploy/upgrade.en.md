# Upgrade Guide

## Basic Procedure

```bash
# 1. Check the current version
helm list -n branchdb-system

# 2. Upgrade including the CRD
helm upgrade branchdb deploy/helm/branchdb \
  --namespace branchdb-system \
  --set installCRDs=true \
  -f my-values.yaml

# 3. Check the rollout
kubectl -n branchdb-system rollout status deploy/branchdb
kubectl -n branchdb-system rollout status deploy/branchdb-api
```

---

## About CRD Upgrades

BranchDB adopts `installCRDs: true` (the cert-manager pattern).
`helm upgrade --set installCRDs=true` upgrades the CRD at the same time.

```bash
# Check the current version of the CRD
kubectl get crd databasebranches.branchdb.io -o jsonpath='{.metadata.annotations}'
```

> **Note:** Existing `DatabaseBranch` resources are not deleted during a CRD upgrade.
> Adding fields is backward compatible. Removing fields or changing types is a breaking change.

---

## Rollback

```bash
# Roll back to the previous version
helm rollback branchdb -n branchdb-system

# Roll back to a specific revision
helm history branchdb -n branchdb-system
helm rollback branchdb <revision> -n branchdb-system
```

> **Note:** The CRD is not rolled back. If you need to revert the CRD to a previous version, apply it manually.

---

## Zero-downtime Upgrades

Because the API server uses the `RollingUpdate` strategy, you can upgrade with zero downtime by setting the replica count to 2 or more.

```yaml
# values.yaml
apiServer:
  replicaCount: 2
```

The Operator runs safely with multiple replicas thanks to leader election.

---

## Breaking Changes Between Versions

### v0.1.x → v0.2.x

- The Helm chart name changed from `branchdb-operator` to `branchdb`
- To migrate an existing installation:

```bash
# Uninstall the old chart (the CRD remains)
helm uninstall branchdb-operator -n branchdb-system

# Install the new chart (use installCRDs=false since the CRD already exists)
helm upgrade --install branchdb deploy/helm/branchdb \
  --namespace branchdb-system \
  --set installCRDs=false \
  -f my-values.yaml
```
