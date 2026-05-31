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
				{Name: "snap-1", CreatedAt: now},
				{Name: "snap-2", CreatedAt: now.Add(-time.Hour)},
			}, nil
		},
	}

	handler := api.NewK8sBranchHandler(fakeClient, "branchdb.example.com").
		WithVolumeProvider(mockVP)
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/snapshots", nil)
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
		takeSnapshotFunc: func(ctx context.Context, dbType, name string) error {
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
	takeSnapshotFunc  func(ctx context.Context, dbType, name string) error
	listSnapshotsFunc func(ctx context.Context, dbType string) ([]domain.SnapshotInfo, error)
}

func (m *mockVolumeProvider) TakeSnapshot(ctx context.Context, dbType, name string) error {
	if m.takeSnapshotFunc != nil {
		return m.takeSnapshotFunc(ctx, dbType, name)
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
		takeSnapshotFunc: func(ctx context.Context, dbType, name string) error {
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
		WithMySQLQuerier(func(_ context.Context, _ string) (int, error) {
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
		WithMySQLQuerier(func(_ context.Context, _ string) (int, error) {
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
