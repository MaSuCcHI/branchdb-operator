# 開発バックログ（コード調査 2026-06-11）

コードベース全体（Operator / REST API / ZFS Agent / インフラ層）のレビューで見つかった
バグ・セキュリティ課題・リファクタリング・将来機能をタスクとしてまとめたもの。
ロードマップ（`docs/roadmap.md`）に記載済みの項目は重複を避けて参照のみ。

---

## P0: バグ（データ破損・誤動作につながるもの）

- [x] **PV/PVC のバインディングが固定されていない**
  `internal/infrastructure/k8sdatabase/provider.go:204-246`
  PV に `claimRef`、PVC に `volumeName` のどちらも設定していないため、K8s のバインダーは
  容量・アクセスモードが一致する任意の Available な PV を PVC に束縛しうる。
  ブランチ A の PVC がブランチ B の PV（= 別ブランチのデータ）に bind される可能性がある。
  → PVC に `spec.volumeName: branchdb-pv-<branch>` を設定する（か PV 側に claimRef）。

- [x] **ZFS Agent のデフォルトプロバイダーが非決定的**
  `internal/interface/api/zfsagent/handler.go:43-50`
  `NewHandler` が「map の最初のキー」を defaultType にしているが、Go の map 反復順序は
  ランダム。マルチデータセット構成で `?db_type=` を省略すると、プロセス起動ごとに
  別の dataset が操作対象になる。→ defaultType を明示的に渡す / 省略時は 400 を返す。

- [x] **Operator と API サーバーの環境変数・namespace 既定値の不整合**
  `cmd/operator/main.go:48-51` は `ZFSAGENT_URL` / `ZFSAGENT_TOKEN` を読み、namespace の
  既定値は `branchdb-system`。一方 API サーバー（`cmd/branchdb/main.go`）と CLAUDE.md は
  `ZFSDB_ZFSAGENT_URL` / `ZFSDB_ZFSAGENT_TOKEN`、namespace 既定値 `default`。
  既定のまま動かすと API サーバーは `default` ns の Pod を探し、Operator は
  `branchdb-system` に Pod を作るため `/branches/{name}/pod` が常に失敗する。
  → 変数名を `ZFSDB_*` に統一し、namespace 既定値も揃える。ドキュメントも更新。

- [x] **CRD の scope=Cluster と namespaced な扱いが矛盾**
  `api/v1alpha1/databasebranch_types.go:86`（`+kubebuilder:resource:scope=Cluster`）に対し、
  API ハンドラは CR に Namespace を設定し `client.InNamespace` で List している
  （`internal/interface/api/k8s_branch_handler.go:200,245`）。クラスタスコープのリソースに
  namespace を渡す挙動は未定義に近く、マルチテナント計画とも噛み合わない。
  → CRD を Namespaced に変更するのが妥当（PV 名にも namespace を含める）。

- [x] **`pollForPort` のコンテキストキャンセル処理が壊れている**
  `internal/interface/api/k8s_branch_handler.go:232-236`
  `select` 内の `break` は select を抜けるだけでループは継続する。クライアントが接続を
  切ると `<-ctx.Done()` が即座に返り続け、deadline までビジーループになる。
  → `for { select { case <-ctx.Done(): return ... } }` 形に書き換える。

- [x] **スナップショット作成日時の取得・整形が二重に壊れている**
  1. `internal/infrastructure/zfs/agent_provider.go:366` — `zfs list` の人間可読な
     creation をパースし、エラーを無視（失敗するとゼロ値）。ロケール依存でもある。
     `zfs list -p`（epoch 秒）を使うべき。GC の保持順序（`GCSnapshots`）がこの値に
     依存しているため、パース失敗時は誤った削除順になりうる。
  2. `internal/interface/api/zfsagent/handler.go:147` — ローカル時刻を
     `"2006-01-02T15:04:05Z"` で整形しており、UTC でないのに `Z` を付けている。
     `t.UTC().Format(time.RFC3339)` に修正。

- [x] **`spec.InitSQL` が未実装**
  `api/v1alpha1/databasebranch_types.go:38` で定義されているが、reconciler・provider の
  どこからも参照されていない。実装する（Ready 後に Job などで実行）か、フィールドを削除する。

## P1: バグ（運用で踏むもの）

- [x] **ブランチ名のバリデーションがない / Service 名が衝突しうる**
  `handleCreate` は空チェックのみ（`k8s_branch_handler.go:172`）。DNS-1035 非準拠の名前は
  K8s API のエラーがそのままユーザーに返る。また Service 名がブランチ名そのまま
  （`k8sdatabase/provider.go:348`）なので、namespace 内の既存 Service と衝突しうる。
  → 名前の正規表現バリデーション + Service 名に `branchdb-` プレフィックスを付ける。

- [x] **`Provider.Stop` が全エラーを握りつぶす**
  `internal/infrastructure/k8sdatabase/provider.go:155-172`
  削除失敗（NotFound 以外）も無視して nil を返すため、リソースリークが finalizer 突破後に
  発覚しない。NotFound のみ無視し、それ以外は集約して返す（`errors.Join`）。

- [x] **作成した K8s リソースに OwnerReference がない**
  PV/PVC/Pod/Service/ConfigMap に DatabaseBranch への ownerRef がなく、Operator 停止中の
  CR 削除や Stop の失敗でリソースが孤児化する。ownerRef を設定して K8s GC に拾わせる。

- [x] **`handleResetDataset` の削除レース**
  `k8s_branch_handler.go:494-530`
  CR の Delete は finalizer による非同期クリーンアップなのに、直後に Agent の
  `ResetDataset`（全クローン破棄）を呼ぶ。稼働中の DB Pod の下からボリュームが消える。
  → CR の削除完了（finalizer 消化）を待ってから Reset する。

- [x] **TTL の挙動が不正確**
  `internal/interface/operator/reconciler.go:48-56,70-72`
  - Ready 後は早期 return するため、`spec.ttlHours` を後から変更しても反映されない。
  - 失効チェックは固定 10 分間隔の requeue 依存なので、最大 10 分超過する。
    → `RequeueAfter` を `min(残り時間, requeueInterval)` にする。

- [x] **Graceful shutdown が未実装**
  `cmd/branchdb/main.go:88-92` / `cmd/zfsagent/main.go:57-69`
  シグナル受信後 `srv.Shutdown(ctx)` を呼ばずに main を抜けるため、処理中のリクエスト
  （ZFS 操作を含む）が切断される。

- [x] **`queryMySQLThreads` が無結果を 0 件として成功扱い**
  `k8s_branch_handler.go:604-630`
  行が無い場合・`Sscanf` 失敗時に `(0, nil)` を返す。エラーを返すよう修正。

- [x] **`handleCreate` のスナップショット検証が Agent 障害時に素通り**
  `k8s_branch_handler.go:182`（`if snaps, err := ...; err == nil` のときのみ検証）。
  Agent がダウンしていると存在しないスナップショット名でも CR が作られ、ブランチは
  Error フェーズで失敗する。検証エラー時は 503 を返す方が親切。

## P2: セキュリティ

- [ ] **ブランチ DB が無認証で NodePort 公開される**
  MySQL: `MYSQL_ALLOW_EMPTY_PASSWORD=yes`、PostgreSQL: `trust`（`k8sdatabase/provider.go:60,74`）。
  開発用途とはいえ NodePort で外部公開されるため、最低限「ブランチごとに生成した
  パスワードを Status / API レスポンスで返す」オプションを追加し、README に現状の
  リスクを明記する。

- [ ] **Bearer トークン比較がタイミング攻撃可能**
  `internal/interface/api/zfsagent/handler.go:95` — `crypto/subtle.ConstantTimeCompare` を使う。

- [ ] **branchdb API が完全無認証（破壊的操作含む）**
  `/snapshots/reset`・`/gc`・`DELETE /branches` が誰でも叩ける。OIDC はロードマップの
  Pro 機能だが、暫定で静的トークンのミドルウェア（`WithAuthMiddleware` の注入口）を
  先に用意しておくと Pro 境界の設計も進む。

- [ ] **WebSocket の `CheckOrigin` が常に true**
  `internal/interface/api/ws_hub.go:12-14` — 同一オリジン検証を入れる。

- [ ] **DB Pod に resource requests/limits・securityContext がない**
  `k8sdatabase/provider.go:295-329` — ノイジーネイバー対策と PodSecurity 対応。

## P3: リファクタリング・デッドコード

- [ ] **デッドコードの削除**
  - `internal/infrastructure/zfs/client.go` — `Client`（Clone/Promote/Rollback 等）は
    どこからも使われていない（`run()` のみ agent_provider が使用）。
  - `WSHub`（`ws_hub.go`）— `cmd/branchdb` で配線されておらず、イベントを Publish する
    箇所も無い。使うなら P4 のタスクとして配線、使わないなら削除。
  - `domain.BranchMySQLProvider`（Deprecated エイリアス）— 参照が無くなり次第削除。

- [ ] **アーカイブ名パターン判定ロジックの重複**
  `api.InferSnapshotRole`（`k8s_branch_handler.go:82` の `isDigits`）と
  `zfs.GCSnapshots`（`agent_provider.go:273` の `isAllDigits`）に同じ
  `-YYYYMMDD-HHMMSS` 判定が二重実装されている。domain 層に共通関数を抽出する。

- [ ] **zfsagent.Provider の HTTP 呼び出しの重複**
  `internal/infrastructure/zfsagent/provider.go` — 8 メソッドでリクエスト生成・認証
  ヘッダ・エラーデコードがコピペされている。`doRequest(ctx, method, path, body, out)`
  ヘルパーに集約する。

- [ ] **`sortSnapshotsByCreatedAtDesc` のバブルソート**
  `agent_provider.go:315-323` — `sort.Slice` に置き換える。

- [ ] **`localIP()` が Linux 専用**
  `agent_provider.go:376`（`hostname -I`）。失敗時 `127.0.0.1` にフォールバックすると
  K8s ノードからマウントできない NFS アドレスを返してしまう。
  `ZFSAGENT_NFS_HOST` 環境変数で明示設定できるようにし、未設定+検出失敗は起動エラーにする。

- [ ] **PV サイズ 10Gi がハードコード**
  `k8sdatabase/provider.go:210,236` — ZFS クローンの実サイズと無関係な値。設定可能にする。

- [ ] **`internal/infrastructure/zfs` のカバレッジ 14.7%**
  CLAUDE.md の「95% 以上」ポリシーに対し大きく不足（os/exec 依存のため除外と
  コメントされている）。コマンド実行を `runner` インターフェースとして注入できるよう
  リファクタし、パース・分岐ロジック（TakeSnapshot の overwrite 分岐、GC、
  releaseNFSAndDestroy）をテスト可能にする。少なくとも CI 上で除外パッケージを明示する。

## P4: 機能追加（ロードマップ連動含む）

- [ ] **CR への Conditions / K8s Events の記録**
  現状 `status.message` のみ。`status.conditions` と `record.EventRecorder` を導入して
  `kubectl describe` で原因が追えるようにする。

- [ ] **WebSocket イベントの配線（または SPA のポーリング明示化）**
  Reconcile/API 操作時に `WSHub.Publish` を呼び、`cmd/branchdb` で hub を起動・注入する。

- [ ] **メトリクスエンドポイント（branchdb API）**
  ブランチ数・作成失敗数・ZFS Agent 呼び出しレイテンシの Prometheus メトリクス。

- [ ] **PostgreSQL / Redis のメトリクス対応**
  `handleGetMetrics` は MySQL のみ（`k8s_branch_handler.go:359`）。pg_stat_activity /
  `INFO clients` で他エンジンにも対応する。

- [ ] **スナップショット検証ゲート・自動再クローン・Redis AOF**
  → `docs/roadmap.md` の「部分実装 🚧」セクション参照。

- [ ] **認証・マルチテナント・クォータ（Pro 境界の注入口づくり）**
  → `docs/roadmap.md` の「計画中 📋」参照。OSS 側タスクは
  `WithAuthMiddleware` / `QuotaEnforcer` インターフェースの定義と注入ポイントの実装。
