# モニタリング

## Prometheus メトリクス

Operator は controller-runtime 標準のメトリクスを `:8080/metrics` で公開します。

```bash
# メトリクスの確認
kubectl -n branchdb-system port-forward svc/branchdb-metrics 8080:8080 &
curl http://localhost:8080/metrics | grep branchdb
```

### 主要メトリクス

| メトリクス名 | 説明 |
|---|---|
| `controller_runtime_reconcile_total` | Reconcile の実行回数（ラベル: `controller`, `result`）|
| `controller_runtime_reconcile_errors_total` | Reconcile エラー数 |
| `controller_runtime_reconcile_time_seconds` | Reconcile の実行時間 |
| `workqueue_depth` | 処理待ちのアイテム数 |

### Prometheus でスクレイプする

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

## ヘルスチェック

### Operator

| エンドポイント | ポート | 説明 |
|---|---|---|
| `GET /healthz` | `:8081` | Liveness probe |
| `GET /readyz` | `:8081` | Readiness probe |

### API サーバー

| エンドポイント | ポート | 説明 |
|---|---|---|
| `GET /health` | `:8080` | Liveness / Readiness 兼用 |

---

## Grafana ダッシュボード（例）

よく監視するメトリクス：

```promql
# 現在の Ready ブランチ数（DatabaseBranch から集計）
count(kube_customresource_info{customresource_kind="DatabaseBranch",status_phase="Ready"})

# Reconcile エラー率（直近 5 分）
rate(controller_runtime_reconcile_errors_total{controller="databasebranch"}[5m])

# 平均 Reconcile 時間
histogram_quantile(0.99,
  rate(controller_runtime_reconcile_time_seconds_bucket{controller="databasebranch"}[5m])
)
```

---

## アラート例

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
