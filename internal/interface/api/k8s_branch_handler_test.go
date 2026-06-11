package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	v1alpha1 "github.com/MaSuCcHI/branchdb-operator/api/v1alpha1"
	"github.com/MaSuCcHI/branchdb-operator/internal/domain"
	"github.com/MaSuCcHI/branchdb-operator/internal/interface/api"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newK8sTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(s)
	return s
}

func newK8sTestSchemeWithCore() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(s)
	_ = clientgoscheme.AddToScheme(s)
	return s
}

func TestK8sPostBranches_CRが作成される(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com").
		WithPortWaitTimeout(10 * time.Millisecond)
	router := api.NewK8sRouter(handler)

	body, _ := json.Marshal(map[string]any{"name": "feature-k8s"})
	req := httptest.NewRequest(http.MethodPost, "/branches", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("got status %d, want 202", w.Code)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["name"] != "feature-k8s" {
		t.Errorf("got name %q, want feature-k8s", resp["name"])
	}
	if resp["status"] != "creating" {
		t.Errorf("got status %q, want creating", resp["status"])
	}
}

func TestK8sGetBranches_CR一覧が返る(t *testing.T) {
	scheme := newK8sTestScheme()
	now := metav1.NewTime(time.Now())
	cr := v1alpha1.DatabaseBranch{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "feat-a",
			Namespace:         "default",
			CreationTimestamp: now,
		},
		Status: v1alpha1.DatabaseBranchStatus{
			Phase:        "ready",
			ClusterHost:  "feat-a-svc.default.svc",
			ExternalPort: 30001,
		},
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(&cr).
		Build()

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/branches", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}

	var resp []map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp) != 1 {
		t.Fatalf("got %d branches, want 1", len(resp))
	}
	if resp[0]["name"] != "feat-a" {
		t.Errorf("got name %q, want feat-a", resp[0]["name"])
	}
}

func TestK8sGetBranch_CRのstatusが返る(t *testing.T) {
	scheme := newK8sTestScheme()
	now := metav1.NewTime(time.Now())
	cr := v1alpha1.DatabaseBranch{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "feat-b",
			Namespace:         "default",
			CreationTimestamp: now,
		},
		Status: v1alpha1.DatabaseBranchStatus{
			Phase:        "ready",
			ClusterHost:  "feat-b-svc.default.svc",
			ExternalPort: 30002,
		},
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(&cr).
		Build()

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/branches/feat-b", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["name"] != "feat-b" {
		t.Errorf("got name %q, want feat-b", resp["name"])
	}
	if resp["status"] != "ready" {
		t.Errorf("got status %q, want ready", resp["status"])
	}
}

func TestK8sGetBranch_存在しないブランチは404を返す(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/branches/nonexistent", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("got status %d, want 404", w.Code)
	}
}

func TestK8sPostBranches_ボディが不正なとき400を返す(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodPost, "/branches", bytes.NewReader([]byte("invalid-json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want 400", w.Code)
	}
}

func TestK8sPostBranches_ブランチ名が空のとき400を返す(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com").
		WithPortWaitTimeout(10 * time.Millisecond)
	router := api.NewK8sRouter(handler)

	body, _ := json.Marshal(map[string]any{"name": ""})
	req := httptest.NewRequest(http.MethodPost, "/branches", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want 400", w.Code)
	}
}

func TestK8sDeleteBranch_存在しないブランチは404を返す(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodDelete, "/branches/nonexistent", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("got status %d, want 404", w.Code)
	}
}

func TestK8sDeleteBranch_CRが削除される(t *testing.T) {
	scheme := newK8sTestScheme()
	now := metav1.NewTime(time.Now())
	cr := v1alpha1.DatabaseBranch{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "feat-del",
			Namespace:         "default",
			CreationTimestamp: now,
		},
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(&cr).
		Build()

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodDelete, "/branches/feat-del", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("got status %d, want 204", w.Code)
	}
}

func TestK8sGetBranches_namespace指定が反映される(t *testing.T) {
	scheme := newK8sTestScheme()
	crDefault := v1alpha1.DatabaseBranch{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "in-default",
			Namespace: "default",
		},
	}
	crOther := v1alpha1.DatabaseBranch{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "in-other",
			Namespace: "other",
		},
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(&crDefault, &crOther).
		Build()

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com").
		WithNamespace("other")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/branches", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}
	var resp []map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp) != 1 {
		t.Fatalf("got %d branches, want 1 (only in-other)", len(resp))
	}
	if resp[0]["name"] != "in-other" {
		t.Errorf("got name %q, want in-other", resp[0]["name"])
	}
}

func TestK8sPostBranches_コンテキストキャンセル時も202を返す(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com").
		WithPortWaitTimeout(200 * time.Millisecond)
	router := api.NewK8sRouter(handler)

	body, _ := json.Marshal(map[string]any{"name": "ctx-cancel-branch"})
	req := httptest.NewRequest(http.MethodPost, "/branches", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("got status %d, want 202", w.Code)
	}
}

func TestK8sPostBranches_事前キャンセル済みコンテキストのときpollForPortのctxDoneパスを通る(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	// fake client はコンテキストをチェックしないため Create は成功する。
	// pollForPort 内の select で ctx.Done() が即座に fire する。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com").
		WithPortWaitTimeout(10 * time.Millisecond)
	router := api.NewK8sRouter(handler)

	body, _ := json.Marshal(map[string]any{"name": "ctx-done-branch"})
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/branches", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("got status %d, want 202", w.Code)
	}
}

func TestK8sPostBranches_ポーリング中にExternalPortが設定されたとき即座に返す(t *testing.T) {
	// mockK8sClient は Create を無視して Get でポート付き CR を返す。
	// pollForPort が ExternalPort > 0 の早期 return パスを通る。
	cr := &v1alpha1.DatabaseBranch{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "feat-port-set",
			Namespace: "default",
		},
		Status: v1alpha1.DatabaseBranchStatus{
			ExternalPort: 30001,
			ClusterHost:  "feat-port-set.svc.default",
		},
	}
	mock := &mockK8sClient{getCR: cr}

	handler := api.NewK8sBranchHandler(mock, "branchdb.example.com").
		WithPortWaitTimeout(100 * time.Millisecond)
	router := api.NewK8sRouter(handler)

	body, _ := json.Marshal(map[string]any{"name": "feat-port-set"})
	req := httptest.NewRequest(http.MethodPost, "/branches", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("got status %d, want 202", w.Code)
	}
}

func TestK8sRouter_HealthEndpointが正常に応答する(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}
}

func TestK8sGetBranch_portが確定済みのときDSNが返る(t *testing.T) {
	scheme := newK8sTestScheme()
	cr := v1alpha1.DatabaseBranch{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "feat-dsn",
			Namespace: "default",
		},
		Status: v1alpha1.DatabaseBranchStatus{
			Phase:        "ready",
			ClusterHost:  "feat-dsn-svc.default.svc",
			ExternalPort: 30099,
		},
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(&cr).
		Build()

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/branches/feat-dsn", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	dsn, _ := resp["dsn"].(string)
	if dsn == "" {
		t.Error("expected non-empty DSN when port is assigned")
	}
	wantDSN := "root@tcp(branchdb.example.com:30099)/"
	if dsn != wantDSN {
		t.Errorf("got DSN %q, want %q", dsn, wantDSN)
	}
}

func TestK8sGetStats_フェーズ別カウントが返る(t *testing.T) {
	scheme := newK8sTestScheme()
	branches := []v1alpha1.DatabaseBranch{
		{ObjectMeta: metav1.ObjectMeta{Name: "b1", Namespace: "default"}, Status: v1alpha1.DatabaseBranchStatus{Phase: v1alpha1.BranchPhaseReady}},
		{ObjectMeta: metav1.ObjectMeta{Name: "b2", Namespace: "default"}, Status: v1alpha1.DatabaseBranchStatus{Phase: v1alpha1.BranchPhaseReady}},
		{ObjectMeta: metav1.ObjectMeta{Name: "b3", Namespace: "default"}, Status: v1alpha1.DatabaseBranchStatus{Phase: v1alpha1.BranchPhaseCreating}},
		{ObjectMeta: metav1.ObjectMeta{Name: "b4", Namespace: "default"}, Status: v1alpha1.DatabaseBranchStatus{Phase: v1alpha1.BranchPhaseError}},
		{ObjectMeta: metav1.ObjectMeta{Name: "b5", Namespace: "default"}, Status: v1alpha1.DatabaseBranchStatus{Phase: v1alpha1.BranchPhasePending}},
		{ObjectMeta: metav1.ObjectMeta{Name: "b6", Namespace: "default"}, Status: v1alpha1.DatabaseBranchStatus{Phase: v1alpha1.BranchPhaseDeleting}},
	}
	objs := make([]runtime.Object, len(branches))
	for i := range branches {
		objs[i] = &branches[i]
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}

	var stats api.K8sStats
	if err := json.NewDecoder(w.Body).Decode(&stats); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	if stats.Total != 6 {
		t.Errorf("Total: got %d, want 6", stats.Total)
	}
	if stats.Ready != 2 {
		t.Errorf("Ready: got %d, want 2", stats.Ready)
	}
	if stats.Creating != 1 {
		t.Errorf("Creating: got %d, want 1", stats.Creating)
	}
	if stats.Error != 1 {
		t.Errorf("Error: got %d, want 1", stats.Error)
	}
	if stats.Pending != 1 {
		t.Errorf("Pending: got %d, want 1", stats.Pending)
	}
	if stats.Deleting != 1 {
		t.Errorf("Deleting: got %d, want 1", stats.Deleting)
	}
}

func TestK8sGetStats_ブランチが0件のとき空カウントを返す(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}
	var stats api.K8sStats
	json.NewDecoder(w.Body).Decode(&stats)
	if stats.Total != 0 {
		t.Errorf("Total: got %d, want 0", stats.Total)
	}
}

func TestK8sGetPod_Podが存在するとき情報を返す(t *testing.T) {
	scheme := newK8sTestSchemeWithCore()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "branchdb-db-feat-pod",
			Namespace: "default",
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/branches/feat-pod/pod", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}

	var info api.PodInfo
	if err := json.NewDecoder(w.Body).Decode(&info); err != nil {
		t.Fatalf("decode pod info: %v", err)
	}
	if info.Phase != "Running" {
		t.Errorf("Phase: got %q, want Running", info.Phase)
	}
	if !info.Ready {
		t.Error("Ready: got false, want true")
	}
}

func TestK8sGetPod_Podが存在しないとき503を返す(t *testing.T) {
	scheme := newK8sTestSchemeWithCore()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/branches/no-pod/pod", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("got status %d, want 503", w.Code)
	}
}

func TestK8sGetMetrics_CRが存在しないとき利用不可を返す(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/branches/no-such-branch/metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}
	var m api.BranchMetrics
	json.NewDecoder(w.Body).Decode(&m)
	if m.Available {
		t.Error("expected Available=false when CR not found")
	}
}

func TestK8sGetMetrics_ClusterHostが未設定のとき利用不可を返す(t *testing.T) {
	scheme := newK8sTestScheme()
	cr := v1alpha1.DatabaseBranch{
		ObjectMeta: metav1.ObjectMeta{Name: "feat-no-host", Namespace: "default"},
		Status:     v1alpha1.DatabaseBranchStatus{Phase: v1alpha1.BranchPhaseCreating},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&cr).Build()

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/branches/feat-no-host/metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}
	var m api.BranchMetrics
	json.NewDecoder(w.Body).Decode(&m)
	if m.Available {
		t.Error("expected Available=false when ClusterHost is empty")
	}
	if m.ErrorMsg == "" {
		t.Error("expected non-empty ErrorMsg")
	}
}

func TestK8sGetMetrics_MySQL接続失敗時に利用不可を返す(t *testing.T) {
	scheme := newK8sTestScheme()
	cr := v1alpha1.DatabaseBranch{
		ObjectMeta: metav1.ObjectMeta{Name: "feat-no-mysql", Namespace: "default"},
		Status: v1alpha1.DatabaseBranchStatus{
			Phase:       v1alpha1.BranchPhaseReady,
			ClusterHost: "192.0.2.1", // TEST-NET: unreachable
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&cr).Build()

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/branches/feat-no-mysql/metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}
	var m api.BranchMetrics
	json.NewDecoder(w.Body).Decode(&m)
	if m.Available {
		t.Error("expected Available=false when MySQL connection fails")
	}
}

func TestK8sListSnapshots_VolumeProviderが未設定のとき501を返す(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/snapshots", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("got status %d, want 501", w.Code)
	}
}

func TestK8sListSnapshots_VolumeProviderが設定済みのときスナップショット一覧を返す(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	now := time.Now().Truncate(time.Second)
	mockVP := &mockVolumeProvider{
		listSnapshotsFunc: func(ctx context.Context, _ string) ([]domain.SnapshotInfo, error) {
			return []domain.SnapshotInfo{
				{Name: "snap-1", CreatedAt: now, DatabaseType: "mysql"},
				{Name: "snap-2", CreatedAt: now.Add(-time.Hour), DatabaseType: "mysql"},
			}, nil
		},
	}

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com").
		WithVolumeProvider(mockVP)
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/snapshots?db_type=mysql", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}

	var snaps []api.K8sSnapshotResponse
	if err := json.NewDecoder(w.Body).Decode(&snaps); err != nil {
		t.Fatalf("decode snapshots: %v", err)
	}
	if len(snaps) != 2 {
		t.Errorf("got %d snapshots, want 2", len(snaps))
	}
	if snaps[0].Name != "snap-1" {
		t.Errorf("got name %q, want snap-1", snaps[0].Name)
	}
	if snaps[0].DatabaseType != "mysql" {
		t.Errorf("got database_type %q, want mysql", snaps[0].DatabaseType)
	}
}

func TestK8sCreate_snapshot_refが空のときVolumeProvider設定済みなら400を返す(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	mockVP := &mockVolumeProvider{}
	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com").
		WithVolumeProvider(mockVP).
		WithPortWaitTimeout(0)
	router := api.NewK8sRouter(handler)

	body, _ := json.Marshal(map[string]any{"name": "feature-x", "database_type": "mysql"})
	req := httptest.NewRequest(http.MethodPost, "/branches", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want 400", w.Code)
	}
}

func TestK8sCreate_ListSnapshotsエラー時に503を返す(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	mockVP := &mockVolumeProvider{
		listSnapshotsFunc: func(ctx context.Context, dbType string) ([]domain.SnapshotInfo, error) {
			return nil, errors.New("agent unavailable")
		},
	}
	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com").
		WithVolumeProvider(mockVP).
		WithPortWaitTimeout(0)
	router := api.NewK8sRouter(handler)

	body, _ := json.Marshal(map[string]any{"name": "new-branch", "database_type": "mysql", "snapshot_ref": "base"})
	req := httptest.NewRequest(http.MethodPost, "/branches", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("got status %d, want 503 when ListSnapshots errors", w.Code)
	}
}

func TestK8sCreate_指定dbTypeにsnapshotが存在しないとき400を返す(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	mockVP := &mockVolumeProvider{
		listSnapshotsFunc: func(ctx context.Context, dbType string) ([]domain.SnapshotInfo, error) {
			// postgres にはスナップショットが存在しない
			if dbType == "postgres" {
				return []domain.SnapshotInfo{}, nil
			}
			return []domain.SnapshotInfo{{Name: "base", DatabaseType: "mysql"}}, nil
		},
	}
	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com").
		WithVolumeProvider(mockVP).
		WithPortWaitTimeout(0)
	router := api.NewK8sRouter(handler)

	body, _ := json.Marshal(map[string]any{"name": "pg-branch", "database_type": "postgres", "snapshot_ref": "base"})
	req := httptest.NewRequest(http.MethodPost, "/branches", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want 400", w.Code)
	}
}

func TestK8sCreate_VolumeProviderが設定済みで正しいsnapshotなら202を返す(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	mockVP := &mockVolumeProvider{
		listSnapshotsFunc: func(ctx context.Context, dbType string) ([]domain.SnapshotInfo, error) {
			return []domain.SnapshotInfo{{Name: "base", DatabaseType: dbType}}, nil
		},
	}
	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com").
		WithVolumeProvider(mockVP).
		WithPortWaitTimeout(0)
	router := api.NewK8sRouter(handler)

	body, _ := json.Marshal(map[string]any{"name": "mysql-branch", "database_type": "mysql", "snapshot_ref": "base"})
	req := httptest.NewRequest(http.MethodPost, "/branches", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("got status %d, want 202", w.Code)
	}
}

func TestK8sTakeSnapshot_VolumeProviderが未設定のとき501を返す(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodPost, "/snapshots", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("got status %d, want 501", w.Code)
	}
}

func TestK8sTakeSnapshot_VolumeProviderが設定済みのとき200を返す(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	called := false
	mockVP := &mockVolumeProvider{
		takeSnapshotFunc: func(ctx context.Context, dbType, name string, overwrite bool) error {
			called = true
			return nil
		},
	}

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com").
		WithVolumeProvider(mockVP)
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodPost, "/snapshots", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}
	if !called {
		t.Error("TakeSnapshot was not called")
	}
}

func TestK8sGetBranch_新フィールドが返る(t *testing.T) {
	scheme := newK8sTestScheme()
	expiresAt := metav1.NewTime(time.Now().Add(24 * time.Hour))
	cr := v1alpha1.DatabaseBranch{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "feat-fields",
			Namespace: "default",
		},
		Spec: v1alpha1.DatabaseBranchSpec{
			SnapshotRef: "base-snap",
			TTLHours:    24,
		},
		Status: v1alpha1.DatabaseBranchStatus{
			Phase:       v1alpha1.BranchPhaseReady,
			ClusterHost: "feat-fields.default.svc.cluster.local",
			ClusterPort: 3306,
			Message:     "all good",
			ExpiresAt:   &expiresAt,
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&cr).Build()

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/branches/feat-fields", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["cluster_host"] != "feat-fields.default.svc.cluster.local" {
		t.Errorf("cluster_host: got %v", resp["cluster_host"])
	}
	if resp["cluster_port"] != float64(3306) {
		t.Errorf("cluster_port: got %v", resp["cluster_port"])
	}
	if resp["message"] != "all good" {
		t.Errorf("message: got %v", resp["message"])
	}
	if resp["snapshot_ref"] != "base-snap" {
		t.Errorf("snapshot_ref: got %v", resp["snapshot_ref"])
	}
	if resp["ttl_hours"] != float64(24) {
		t.Errorf("ttl_hours: got %v", resp["ttl_hours"])
	}
	if resp["expires_at"] == nil {
		t.Error("expires_at: expected non-nil")
	}
}

// mockVolumeProvider はテスト用の VolumeProvider 実装。
type mockVolumeProvider struct {
	takeSnapshotFunc   func(ctx context.Context, dbType, name string, overwrite bool) error
	deleteSnapshotFunc func(ctx context.Context, dbType, name string) error
	listSnapshotsFunc  func(ctx context.Context, dbType string) ([]domain.SnapshotInfo, error)
	listClonesFunc     func(ctx context.Context, dbType string) ([]string, error)
	gcSnapshotsFunc    func(ctx context.Context, dbType string, keep int) ([]string, error)
	resetDatasetFunc   func(ctx context.Context, dbType string) error
}

func (m *mockVolumeProvider) TakeSnapshot(ctx context.Context, dbType, name string, overwrite bool) error {
	if m.takeSnapshotFunc != nil {
		return m.takeSnapshotFunc(ctx, dbType, name, overwrite)
	}
	return nil
}

func (m *mockVolumeProvider) DeleteSnapshot(ctx context.Context, dbType, name string) error {
	if m.deleteSnapshotFunc != nil {
		return m.deleteSnapshotFunc(ctx, dbType, name)
	}
	return nil
}

func (m *mockVolumeProvider) ListClones(ctx context.Context, dbType string) ([]string, error) {
	if m.listClonesFunc != nil {
		return m.listClonesFunc(ctx, dbType)
	}
	return nil, nil
}

func (m *mockVolumeProvider) GCSnapshots(ctx context.Context, dbType string, keep int) ([]string, error) {
	if m.gcSnapshotsFunc != nil {
		return m.gcSnapshotsFunc(ctx, dbType, keep)
	}
	return nil, nil
}

func (m *mockVolumeProvider) ResetDataset(ctx context.Context, dbType string) error {
	if m.resetDatasetFunc != nil {
		return m.resetDatasetFunc(ctx, dbType)
	}
	return nil
}

func (m *mockVolumeProvider) CreateClone(ctx context.Context, _ string, snapshot, cloneName string) (domain.VolumeInfo, error) {
	return domain.VolumeInfo{}, nil
}

func (m *mockVolumeProvider) DeleteClone(ctx context.Context, _ string, cloneName string) error {
	return nil
}

func (m *mockVolumeProvider) ListSnapshots(ctx context.Context, dbType string) ([]domain.SnapshotInfo, error) {
	if m.listSnapshotsFunc != nil {
		return m.listSnapshotsFunc(ctx, dbType)
	}
	return []domain.SnapshotInfo{}, nil
}

func TestK8sListSnapshots_Listエラーのとき500を返す(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	mockVP := &mockVolumeProvider{
		listSnapshotsFunc: func(ctx context.Context, _ string) ([]domain.SnapshotInfo, error) {
			return nil, errors.New("storage failure")
		},
	}

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com").
		WithVolumeProvider(mockVP)
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/snapshots", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("got status %d, want 500", w.Code)
	}
}

func TestK8sTakeSnapshot_エラーのとき500を返す(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	mockVP := &mockVolumeProvider{
		takeSnapshotFunc: func(ctx context.Context, dbType, name string, overwrite bool) error {
			return errors.New("storage failure")
		},
	}

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com").
		WithVolumeProvider(mockVP)
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodPost, "/snapshots", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("got status %d, want 500", w.Code)
	}
}

func TestK8sGetStats_listエラー時500を返す(t *testing.T) {
	mock := &mockK8sClient{listErr: errK8sFailed}

	handler := api.NewK8sBranchHandler(mock, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("got status %d, want 500", w.Code)
	}
}

func TestK8sRouter_SPAルートが200を返す(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// The k8s-dist directory has a built k8s.html (or placeholder index.html).
	// Either 200 (built) or 404 (no console) is acceptable, but not 500.
	if w.Code == http.StatusInternalServerError {
		t.Errorf("got status 500, want 200 or 404")
	}
}

func TestK8sGetMetrics_branchが存在しないとき利用不可を返す(t *testing.T) {
	mock := &mockK8sClient{getErr: errK8sFailed}

	handler := api.NewK8sBranchHandler(mock, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/branches/no-branch/metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}

	var m api.BranchMetrics
	json.NewDecoder(w.Body).Decode(&m)
	if m.Available {
		t.Error("Available should be false when branch not found")
	}
}

func TestK8sGetMetrics_clusterHostが未設定のとき利用不可を返す(t *testing.T) {
	cr := &v1alpha1.DatabaseBranch{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "no-host",
			Namespace: "default",
		},
		Status: v1alpha1.DatabaseBranchStatus{
			Phase:       v1alpha1.BranchPhaseReady,
			ClusterHost: "", // not set yet
		},
	}
	mock := &mockK8sClient{getCR: cr}

	handler := api.NewK8sBranchHandler(mock, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/branches/no-host/metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}

	var m api.BranchMetrics
	json.NewDecoder(w.Body).Decode(&m)
	if m.Available {
		t.Error("Available should be false when cluster host not set")
	}
}

func TestK8sGetMetrics_postgres種別のときメトリクス未サポートを返す(t *testing.T) {
	cr := &v1alpha1.DatabaseBranch{
		ObjectMeta: metav1.ObjectMeta{Name: "pg-branch", Namespace: "default"},
		Spec:       v1alpha1.DatabaseBranchSpec{DatabaseType: "postgres"},
		Status: v1alpha1.DatabaseBranchStatus{
			Phase:       v1alpha1.BranchPhaseReady,
			ClusterHost: "pg-branch.default.svc.cluster.local",
		},
	}
	mock := &mockK8sClient{getCR: cr}

	handler := api.NewK8sBranchHandler(mock, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/branches/pg-branch/metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}
	var m api.BranchMetrics
	json.NewDecoder(w.Body).Decode(&m)
	if m.Available {
		t.Error("Available should be false for postgres")
	}
	if m.ErrorMsg == "" {
		t.Error("ErrorMsg should be set for unsupported db type")
	}
}

func TestK8sRouter_WSHubあり時にwsルートが登録される(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com")
	hub := api.NewWSHub()
	router := api.NewK8sRouter(handler, hub)

	// WS エンドポイントが登録されていることを確認（HTTP ではアップグレード失敗で 400 を返す）
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	// 400 Bad Request（WebSocket ハンドシェイクなし）
	if w.Code == http.StatusNotFound {
		t.Error("expected /ws to be registered, got 404")
	}
}

func TestK8sGetMetrics_MySQL接続成功時にスレッド数を返す(t *testing.T) {
	cr := &v1alpha1.DatabaseBranch{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-ok", Namespace: "default"},
		Status: v1alpha1.DatabaseBranchStatus{
			Phase:       v1alpha1.BranchPhaseReady,
			ClusterHost: "mysql.cluster.local",
		},
	}
	mock := &mockK8sClient{getCR: cr}

	handler := api.NewK8sBranchHandler(mock, "branchdb.example.com").
		WithMySQLQuerier(func(_ context.Context, _ string, _ string) (int, error) {
			return 5, nil
		})
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/branches/mysql-ok/metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}
	var m api.BranchMetrics
	json.NewDecoder(w.Body).Decode(&m)
	if !m.Available {
		t.Error("Available should be true on success")
	}
	if m.ThreadsConnected != 5 {
		t.Errorf("ThreadsConnected = %d, want 5", m.ThreadsConnected)
	}
}

func TestK8sGetMetrics_MySQLクエリエラー時に利用不可を返す(t *testing.T) {
	cr := &v1alpha1.DatabaseBranch{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-err", Namespace: "default"},
		Status: v1alpha1.DatabaseBranchStatus{
			Phase:       v1alpha1.BranchPhaseReady,
			ClusterHost: "mysql.cluster.local",
		},
	}
	mock := &mockK8sClient{getCR: cr}

	handler := api.NewK8sBranchHandler(mock, "branchdb.example.com").
		WithMySQLQuerier(func(_ context.Context, _ string, _ string) (int, error) {
			return 0, errors.New("connection refused")
		})
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/branches/mysql-err/metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}
	var m api.BranchMetrics
	json.NewDecoder(w.Body).Decode(&m)
	if m.Available {
		t.Error("Available should be false on error")
	}
}

func TestK8sGetMetrics_MySQLに接続できないとき利用不可を返す(t *testing.T) {
	cr := &v1alpha1.DatabaseBranch{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-branch", Namespace: "default"},
		Spec:       v1alpha1.DatabaseBranchSpec{DatabaseType: "mysql"},
		Status: v1alpha1.DatabaseBranchStatus{
			Phase:       v1alpha1.BranchPhaseReady,
			ClusterHost: "127.0.0.1", // no MySQL server running
		},
	}
	mock := &mockK8sClient{getCR: cr}

	handler := api.NewK8sBranchHandler(mock, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/branches/mysql-branch/metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}
	var m api.BranchMetrics
	json.NewDecoder(w.Body).Decode(&m)
	if m.Available {
		t.Error("Available should be false when MySQL is unreachable")
	}
}

func TestK8sCreateBranch_database_typeがCRSpecに反映される(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com").
		WithPortWaitTimeout(0)
	router := api.NewK8sRouter(handler)

	body := `{"name":"pg-branch","database_type":"postgres","snapshot_ref":"base"}`
	req := httptest.NewRequest(http.MethodPost, "/branches", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("got status %d, want 202", w.Code)
	}

	var cr v1alpha1.DatabaseBranch
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "pg-branch", Namespace: "default"}, &cr); err != nil {
		t.Fatalf("CR not found: %v", err)
	}
	if cr.Spec.DatabaseType != "postgres" {
		t.Errorf("DatabaseType = %q, want postgres", cr.Spec.DatabaseType)
	}
}

func TestK8sDeleteSnapshot_VolumeProviderが未設定のとき501を返す(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodDelete, "/snapshots/snap-001", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("got status %d, want 501", w.Code)
	}
}

func TestK8sDeleteSnapshot_正常に削除できる(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	var deletedName, deletedDBType string
	mockVP := &mockVolumeProvider{
		deleteSnapshotFunc: func(ctx context.Context, dbType, name string) error {
			deletedDBType = dbType
			deletedName = name
			return nil
		},
	}
	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com").
		WithVolumeProvider(mockVP)
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodDelete, "/snapshots/snap-001?db_type=mysql", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("got status %d, want 204", w.Code)
	}
	if deletedName != "snap-001" {
		t.Errorf("deleted name = %q, want snap-001", deletedName)
	}
	if deletedDBType != "mysql" {
		t.Errorf("deleted dbType = %q, want mysql", deletedDBType)
	}
}

func TestInferSnapshotRole(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"base", "current"},
		{"v1", "current"},
		{"base-20260531-175740", "archived"},
		{"v1-20260101-000000", "archived"},
		{"auto-20260531-150405", "auto"},
		{"auto", "current"},
		{"base-notdate-123456", "current"},
		{"base-20260531", "current"}, // 日付部分のみ（時刻なし）
	}
	for _, c := range cases {
		got := api.InferSnapshotRole(c.name)
		if got != c.want {
			t.Errorf("inferSnapshotRole(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestK8sGC_VolumeProviderが未設定のとき501を返す(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodPost, "/gc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("got status %d, want 501", w.Code)
	}
}

func TestK8sGC_孤立クローンとアーカイブスナップショットを削除する(t *testing.T) {
	scheme := newK8sTestScheme()
	// CR には "active-branch" だけ存在
	cr := v1alpha1.DatabaseBranch{
		ObjectMeta: metav1.ObjectMeta{Name: "active-branch", Namespace: "default"},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&cr).Build()

	mockVP := &mockVolumeProvider{
		listClonesFunc: func(_ context.Context, _ string) ([]string, error) {
			// ZFS には "active-branch"（CR あり）と "orphan"（CR なし）が存在
			return []string{"active-branch", "orphan"}, nil
		},
	}
	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com").
		WithVolumeProvider(mockVP)
	router := api.NewK8sRouter(handler)

	body, _ := json.Marshal(map[string]any{"db_type": "mysql", "keep_snapshots": 5})
	req := httptest.NewRequest(http.MethodPost, "/gc", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	orphans, _ := resp["deleted_orphan_clones"].([]any)
	if len(orphans) != 1 || orphans[0] != "orphan" {
		t.Errorf("deleted_orphan_clones = %v, want [orphan]", orphans)
	}
}

func TestK8sReset_VolumeProviderが未設定のとき501を返す(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodPost, "/snapshots/reset", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("got status %d, want 501", w.Code)
	}
}

func TestK8sReset_CRの削除完了を待ってからResetDatasetを呼ぶ(t *testing.T) {
	// このテストでは、fake クライアントが即座に削除を完了するため、
	// ResetDataset が CR削除「後」に呼ばれることを確認する。
	scheme := newK8sTestScheme()
	cr := v1alpha1.DatabaseBranch{
		ObjectMeta: metav1.ObjectMeta{Name: "some-branch", Namespace: "default"},
		Spec:       v1alpha1.DatabaseBranchSpec{DatabaseType: "mysql"},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&cr).Build()

	resetCalled := false
	mockVP := &mockVolumeProvider{
		resetDatasetFunc: func(_ context.Context, _ string) error {
			resetCalled = true
			return nil
		},
	}
	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com").
		WithVolumeProvider(mockVP).
		WithResetWaitTimeout(100 * time.Millisecond)
	router := api.NewK8sRouter(handler)

	body, _ := json.Marshal(map[string]string{"db_type": "mysql"})
	req := httptest.NewRequest(http.MethodPost, "/snapshots/reset", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}
	if !resetCalled {
		t.Error("ResetDataset must be called after CRs are gone")
	}
}

func TestK8sReset_CRとZFSデータを削除する(t *testing.T) {
	scheme := newK8sTestScheme()
	cr := v1alpha1.DatabaseBranch{
		ObjectMeta: metav1.ObjectMeta{Name: "some-branch", Namespace: "default"},
		Spec:       v1alpha1.DatabaseBranchSpec{DatabaseType: "mysql"},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&cr).Build()

	resetCalled := false
	mockVP := &mockVolumeProvider{
		resetDatasetFunc: func(_ context.Context, _ string) error {
			resetCalled = true
			return nil
		},
	}
	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com").
		WithVolumeProvider(mockVP)
	router := api.NewK8sRouter(handler)

	body, _ := json.Marshal(map[string]string{"db_type": "mysql"})
	req := httptest.NewRequest(http.MethodPost, "/snapshots/reset", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}
	if !resetCalled {
		t.Error("ResetDataset was not called")
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["deleted_branches"] != float64(1) {
		t.Errorf("deleted_branches = %v, want 1", resp["deleted_branches"])
	}
}

func TestK8sGC_ListClonesエラーのとき500を返す(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	mockVP := &mockVolumeProvider{
		listClonesFunc: func(_ context.Context, _ string) ([]string, error) {
			return nil, errors.New("list error")
		},
	}
	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com").WithVolumeProvider(mockVP)
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodPost, "/gc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("got %d, want 500", w.Code)
	}
}

func TestK8sReset_ResetDatasetエラーのとき500を返す(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	mockVP := &mockVolumeProvider{
		resetDatasetFunc: func(_ context.Context, _ string) error {
			return errors.New("reset error")
		},
	}
	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com").WithVolumeProvider(mockVP)
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodPost, "/snapshots/reset", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("got %d, want 500", w.Code)
	}
}

func TestK8sDeleteSnapshot_DeleteSnapshotエラーのとき500を返す(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	mockVP := &mockVolumeProvider{
		deleteSnapshotFunc: func(_ context.Context, _, _ string) error {
			return errors.New("zfs destroy failed")
		},
	}
	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com").WithVolumeProvider(mockVP)
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodDelete, "/snapshots/snap-001", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("got status %d, want 500", w.Code)
	}
}

func TestK8sGC_GCSnapshotsエラーのとき500を返す(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	mockVP := &mockVolumeProvider{
		gcSnapshotsFunc: func(_ context.Context, _ string, _ int) ([]string, error) {
			return nil, errors.New("gc failed")
		},
	}
	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com").WithVolumeProvider(mockVP)
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodPost, "/gc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("got status %d, want 500", w.Code)
	}
}

func TestK8sReset_dbTypeが一致しないCRはスキップされる(t *testing.T) {
	scheme := newK8sTestScheme()
	// postgres の CR だけ存在する状態で mysql をリセット → スキップされて 0 件
	cr := v1alpha1.DatabaseBranch{
		ObjectMeta: metav1.ObjectMeta{Name: "pg-branch", Namespace: "default"},
		Spec:       v1alpha1.DatabaseBranchSpec{DatabaseType: "postgres"},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&cr).Build()

	mockVP := &mockVolumeProvider{}
	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com").WithVolumeProvider(mockVP)
	router := api.NewK8sRouter(handler)

	body, _ := json.Marshal(map[string]string{"db_type": "mysql"})
	req := httptest.NewRequest(http.MethodPost, "/snapshots/reset", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["deleted_branches"] != float64(0) {
		t.Errorf("deleted_branches = %v, want 0 (postgres CR should be skipped)", resp["deleted_branches"])
	}
}

func TestServeOpenAPISpec_YAMLが返る(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "application/yaml; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/yaml; charset=utf-8", ct)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("BranchDB API")) {
		t.Error("response body does not contain 'BranchDB API'")
	}
}

func TestServeSwaggerUI_HTMLが返る(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("swagger-ui")) {
		t.Error("response body does not contain 'swagger-ui'")
	}
}

func TestK8sPostBranches_DNS非準拠のブランチ名は400を返す(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com").
		WithPortWaitTimeout(10 * time.Millisecond)
	router := api.NewK8sRouter(handler)

	invalidNames := []string{
		"UPPERCASE",
		"has_underscore",
		"-starts-with-dash",
		"ends-with-dash-",
		"contains spaces",
		strings.Repeat("a", 64), // 64文字は DNS-1123 ラベル規格外
	}

	for _, name := range invalidNames {
		t.Run(name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{"name": name})
			req := httptest.NewRequest(http.MethodPost, "/branches", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Errorf("got status %d, want 400 for invalid name %q", w.Code, name)
			}
		})
	}
}

func TestK8sPostBranches_有効なブランチ名は通過する(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com").
		WithPortWaitTimeout(10 * time.Millisecond)
	router := api.NewK8sRouter(handler)

	validNames := []string{
		"feature-login",
		"abc",
		"a1b2c3",
		strings.Repeat("a", 63), // 63文字は DNS-1123 ラベル最大長
	}

	for _, name := range validNames {
		t.Run(name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{"name": name})
			req := httptest.NewRequest(http.MethodPost, "/branches", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusAccepted {
				t.Errorf("got status %d, want 202 for valid name %q", w.Code, name)
			}
		})
	}
}

func TestInferSnapshotRole_日付部分に非数字がある場合はcurrentを返す(t *testing.T) {
	// 末尾16文字が -YYYYMMDD-HHMMSS 形式だが日付部分に非数字を含む → "current"
	// "base-2026abcd-175740" → suffix="-2026abcd-175740", datePart="2026abcd"
	got := api.InferSnapshotRole("base-2026abcd-175740")
	if got != "current" {
		t.Errorf("InferSnapshotRole(%q) = %q, want current", "base-2026abcd-175740", got)
	}
}

// --- BearerTokenMiddleware / WithAuthMiddleware テスト ---

func TestBearerTokenMiddleware_トークンなしは401を返す(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	handler := api.NewK8sBranchHandler(fakeClient, "localhost").
		WithAuthMiddleware(api.BearerTokenMiddleware("secret-api-token"))
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/branches", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want 401 without token", w.Code)
	}
}

func TestBearerTokenMiddleware_誤ったトークンは401を返す(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	handler := api.NewK8sBranchHandler(fakeClient, "localhost").
		WithAuthMiddleware(api.BearerTokenMiddleware("secret-api-token"))
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/branches", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want 401 with wrong token", w.Code)
	}
}

func TestBearerTokenMiddleware_正しいトークンは200を返す(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	handler := api.NewK8sBranchHandler(fakeClient, "localhost").
		WithAuthMiddleware(api.BearerTokenMiddleware("secret-api-token"))
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/branches", nil)
	req.Header.Set("Authorization", "Bearer secret-api-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200 with correct token", w.Code)
	}
}

func TestBearerTokenMiddleware_healthエンドポイントは認証不要(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	handler := api.NewK8sBranchHandler(fakeClient, "localhost").
		WithAuthMiddleware(api.BearerTokenMiddleware("secret-api-token"))
	router := api.NewK8sRouter(handler)

	// /health はトークンなしでもアクセス可能であること
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200 for /health without token", w.Code)
	}
}

func TestWithAuthMiddleware_nilのとき認証なしで動作する(t *testing.T) {
	scheme := newK8sTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	// WithAuthMiddleware なし（デフォルト）= 後方互換
	handler := api.NewK8sBranchHandler(fakeClient, "localhost").
		WithPortWaitTimeout(10 * time.Millisecond)
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/branches", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 認証なしで 200 が返ること（後方互換）
	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200 without auth middleware (backward compat)", w.Code)
	}
}

// --- CredentialSecret / パスワード入り DSN テスト ---

func TestGetBranch_CredentialSecretが設定されているときDSNにパスワードが含まれる(t *testing.T) {
	scheme := newK8sTestSchemeWithCore()
	now := metav1.NewTime(time.Now())

	cr := &v1alpha1.DatabaseBranch{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cred-branch",
			Namespace:         "default",
			CreationTimestamp: now,
		},
		Status: v1alpha1.DatabaseBranchStatus{
			Phase:            v1alpha1.BranchPhaseReady,
			ExternalPort:     30001,
			CredentialSecret: "branchdb-cred-cred-branch",
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "branchdb-cred-cred-branch",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"password": []byte("super-secret-pw"),
		},
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cr, secret).
		Build()

	handler := api.NewK8sBranchHandler(fakeClient, "myhost.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/branches/cred-branch", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}
	var resp api.BranchResponse
	json.NewDecoder(w.Body).Decode(&resp)

	wantDSN := "root:super-secret-pw@tcp(myhost.example.com:30001)/"
	if resp.DSN != wantDSN {
		t.Errorf("DSN = %q, want %q", resp.DSN, wantDSN)
	}
	if resp.CredentialSecret != "branchdb-cred-cred-branch" {
		t.Errorf("CredentialSecret = %q, want branchdb-cred-cred-branch", resp.CredentialSecret)
	}
}

func TestGetBranch_CredentialSecretなしのときDSNはパスワードなし(t *testing.T) {
	scheme := newK8sTestSchemeWithCore()
	now := metav1.NewTime(time.Now())

	cr := &v1alpha1.DatabaseBranch{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "noauth-branch",
			Namespace:         "default",
			CreationTimestamp: now,
		},
		Status: v1alpha1.DatabaseBranchStatus{
			Phase:        v1alpha1.BranchPhaseReady,
			ExternalPort: 30002,
			// CredentialSecret 未設定 = 無認証
		},
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cr).
		Build()

	handler := api.NewK8sBranchHandler(fakeClient, "myhost.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/branches/noauth-branch", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}
	var resp api.BranchResponse
	json.NewDecoder(w.Body).Decode(&resp)

	wantDSN := "root@tcp(myhost.example.com:30002)/"
	if resp.DSN != wantDSN {
		t.Errorf("DSN = %q, want %q", resp.DSN, wantDSN)
	}
	if resp.CredentialSecret != "" {
		t.Errorf("CredentialSecret = %q, want empty (no-auth)", resp.CredentialSecret)
	}
}

func TestGetMetrics_パスワードありMySQLに接続できる(t *testing.T) {
	scheme := newK8sTestSchemeWithCore()

	cr := &v1alpha1.DatabaseBranch{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mysql-cred",
			Namespace: "default",
		},
		Status: v1alpha1.DatabaseBranchStatus{
			Phase:            v1alpha1.BranchPhaseReady,
			ClusterHost:      "mysql.cluster.local",
			CredentialSecret: "branchdb-cred-mysql-cred",
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "branchdb-cred-mysql-cred",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"password": []byte("my-secret"),
		},
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cr, secret).
		Build()

	var capturedPassword string
	handler := api.NewK8sBranchHandler(fakeClient, "host").
		WithMySQLQuerier(func(_ context.Context, _ string, password string) (int, error) {
			capturedPassword = password
			return 3, nil
		})
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/branches/mysql-cred/metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}
	if capturedPassword != "my-secret" {
		t.Errorf("querier received password %q, want my-secret", capturedPassword)
	}
}
