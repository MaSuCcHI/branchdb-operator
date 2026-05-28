# トラブルシューティング

## よくある問題

---

### ブランチが Creating のまま進まない

**確認手順**

```bash
# 1. Operator のログ確認
kubectl -n branchdb-system logs deploy/branchdb -f

# 2. CR の status 確認
kubectl describe databasebranch <branch-name>

# 3. Pod の状態確認
kubectl -n branchdb-system get pods
kubectl -n branchdb-system describe pod branchdb-mysql-<branch-name>
```

**原因と解決策**

| 症状 | 原因 | 解決策 |
|---|---|---|
| `status.message: "create clone: ..."` | ZFS Agent への接続失敗 | `zfsAgent.url` の疎通確認 |
| `status.message: "start mysql: ..."` | Pod/PVC 作成失敗 | `kubectl describe` で詳細確認 |
| Pod が `Pending` のまま | ノードのリソース不足 | `kubectl describe pod` で events 確認 |
| Pod が `CrashLoopBackOff` | MySQL 起動失敗（NFS マウントエラー等）| Pod のログ確認 |

---

### ZFS Agent への接続失敗

```bash
# K8s クラスターから ZFS Agent への疎通確認
kubectl run -it --rm debug --image=curlimages/curl --restart=Never -- \
  curl -v http://<zfs-server-ip>:9090/health

# Operator Pod から直接確認
kubectl -n branchdb-system exec deploy/branchdb -- \
  wget -q -O- http://<zfs-server-ip>:9090/health
```

確認ポイント:
- ZFS Agent の systemd サービスが動いているか: `systemctl status zfsagent`
- ファイアウォールでポート 9090 が開いているか: `ufw status`
- `zfsAgent.url` の値が正しいか: `kubectl -n branchdb-system get deploy branchdb -o yaml | grep ZFSAGENT`

---

### NodePort に接続できない

```bash
# NodePort の確認
kubectl -n branchdb-system get svc branchdb-mysql-<branch-name>
# PORT(S) の列に 3306:31234/TCP のような形式で表示される

# ノードの外部 IP 確認
kubectl get nodes -o wide

# MySQL への接続テスト
mysql -u root -h <node-ip> -P <nodeport> -e "SELECT 1"
```

確認ポイント:
- `externalHost` が実際にアクセス可能なホスト名/IPになっているか
- クラウド環境ではセキュリティグループ/ファイアウォールで NodePort 範囲（30000-32767）が開いているか

---

### Operator が起動しない

```bash
kubectl -n branchdb-system logs deploy/branchdb
```

| エラーメッセージ | 原因 | 解決策 |
|---|---|---|
| `ZFSAGENT_URL is required` | `zfsAgent.url` が未設定 | `--set zfsAgent.url=...` を追加 |
| `get kubeconfig: ...` | kubeconfig の取得失敗 | RBAC 設定を確認 |
| `register CRD scheme: ...` | CRD が未インストール | `--set installCRDs=true` でインストール |

---

### API サーバーが 500 を返す

```bash
kubectl -n branchdb-system logs deploy/branchdb-api
```

よくある原因:
- K8s API への RBAC 権限不足 → `kubectl get clusterrolebinding | grep branchdb`
- `ZFSDB_NAMESPACE` が実際の名前空間と一致していない

---

### CRD が見つからない

```bash
kubectl get crd databasebranches.branchdb.io
# Error from server (NotFound): ...
```

```bash
# CRD を手動インストール
kubectl apply -f deploy/k8s/crd/

# または helm upgrade で自動インストール
helm upgrade branchdb deploy/helm/branchdb \
  --namespace branchdb-system \
  --set installCRDs=true \
  -f my-values.yaml
```

---

### ブランチ削除がスタックする（Terminating のまま）

finalizer が残っている場合に起きます。Operator が停止していると削除が完了しません。

```bash
# Operator が起動しているか確認
kubectl -n branchdb-system get pods

# 強制削除（Operator が復旧できない場合のみ）
kubectl patch databasebranch <branch-name> \
  -p '{"metadata":{"finalizers":[]}}' \
  --type=merge
```

> **注意:** 強制削除すると ZFS クローンや MySQL Pod が残る場合があります。手動でクリーンアップが必要です。

---

## ログの確認

```bash
# Operator のログ（詳細）
kubectl -n branchdb-system logs deploy/branchdb -f --timestamps

# API サーバーのログ
kubectl -n branchdb-system logs deploy/branchdb-api -f

# 過去のクラッシュログ
kubectl -n branchdb-system logs deploy/branchdb --previous

# ZFS Agent のログ（ZFS サーバー上で）
journalctl -u zfsagent -f
```

---

## 診断コマンドまとめ

```bash
# 全リソースの状態確認
kubectl -n branchdb-system get all

# DatabaseBranch の一覧（phase と NodePort 付き）
kubectl get databasebranches

# Operator の Lease（リーダー確認）
kubectl get lease -n branchdb-system

# Events（直近のイベント）
kubectl -n branchdb-system get events --sort-by='.lastTimestamp' | tail -20
```
