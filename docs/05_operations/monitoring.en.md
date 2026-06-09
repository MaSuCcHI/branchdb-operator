# Monitoring

## Prometheus Metrics

The Operator exposes the standard controller-runtime metrics at `:8080/metrics`.

```bash
# Check the metrics
kubectl -n branchdb-system port-forward svc/branchdb-metrics 8080:8080 &
curl http://localhost:8080/metrics | grep branchdb
```

### Key Metrics

| Metric Name | Description |
|---|---|
| `controller_runtime_reconcile_total` | Number of Reconcile executions (labels: `controller`, `result`) |
| `controller_runtime_reconcile_errors_total` | Number of Reconcile errors |
| `controller_runtime_reconcile_time_seconds` | Reconcile execution time |
| `workqueue_depth` | Number of items waiting to be processed |

### Scraping with Prometheus

```yaml
# prometheus scrape config
scrape_configs:
  - job_name: branchdb-operator
    kubernetes_sd_configs:
      - role: endpoints
        namespaces:
          names: [branchdb-system]
    relabel_configs:
      - source_labels: [__meta_kubernetes_service_name]
        regex: branchdb-metrics
        action: keep
```

---

## Health Checks

### Operator

| Endpoint | Port | Description |
|---|---|---|
| `GET /healthz` | `:8081` | Liveness probe |
| `GET /readyz` | `:8081` | Readiness probe |

### API Server

| Endpoint | Port | Description |
|---|---|---|
| `GET /health` | `:8080` | Combined Liveness / Readiness |

---

## Grafana Dashboard (Example)

Commonly monitored metrics:

```promql
# Current number of Ready branches (aggregated from DatabaseBranch)
count(kube_customresource_info{customresource_kind="DatabaseBranch",status_phase="Ready"})

# Reconcile error rate (last 5 minutes)
rate(controller_runtime_reconcile_errors_total{controller="databasebranch"}[5m])

# Average Reconcile time
histogram_quantile(0.99,
  rate(controller_runtime_reconcile_time_seconds_bucket{controller="databasebranch"}[5m])
)
```

---

## Alert Examples

```yaml
# Prometheus alerting rules
groups:
  - name: branchdb
    rules:
      - alert: BranchDBOperatorReconcileErrors
        expr: rate(controller_runtime_reconcile_errors_total{controller="databasebranch"}[5m]) > 0.1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "BranchDB Operator reconcile error rate is high"

      - alert: BranchDBAPIServerDown
        expr: up{job="branchdb-api"} == 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "BranchDB API server is down"
```
