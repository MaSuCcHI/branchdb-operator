package zfsagent_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MaSuCcHI/branchdb-operator/internal/domain"
	"github.com/MaSuCcHI/branchdb-operator/internal/infrastructure/zfsagent"
)

// errTransport は常にエラーを返す http.RoundTripper。
type errTransport struct{}

func (errTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("transport error")
}

func newFailingProvider(baseURL string) *zfsagent.Provider {
	return zfsagent.NewProvider(baseURL, "token").WithHTTPClient(&http.Client{Transport: errTransport{}})
}

// --- helper ---

func newTestServer(t *testing.T, handler http.Handler) (*httptest.Server, *zfsagent.Provider) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, zfsagent.NewProvider(srv.URL, "test-token")
}

// --- tests ---

func TestProvider_スナップショット作成に成功する(t *testing.T) {
	srv, provider := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/snapshots" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if body["name"] != "snap-001" {
			t.Errorf("expected name snap-001, got %q", body["name"])
		}
		w.WriteHeader(http.StatusCreated)
	}))
	_ = srv

	err := provider.TakeSnapshot(context.Background(), "mysql", "snap-001", false)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestProvider_スナップショット作成でサーバーがエラーを返したときエラーを返す(t *testing.T) {
	srv, provider := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	_ = srv

	err := provider.TakeSnapshot(context.Background(), "mysql", "snap-001", false)
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestProvider_クローン作成に成功しVolumeInfoを返す(t *testing.T) {
	wantVol := domain.VolumeInfo{
		CloneName: "feature-login",
		NFSServer: "10.0.0.5",
		NFSPath:   "/tank/branches/feature-login",
	}

	srv, provider := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/clones" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if body["snapshot"] != "snap-001" {
			t.Errorf("snapshot: got %q, want snap-001", body["snapshot"])
		}
		if body["name"] != "feature-login" {
			t.Errorf("name: got %q, want feature-login", body["name"])
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"clone_name": wantVol.CloneName,
			"nfs_server": wantVol.NFSServer,
			"nfs_path":   wantVol.NFSPath,
		})
	}))
	_ = srv

	got, err := provider.CreateClone(context.Background(), "mysql", "snap-001", "feature-login")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != wantVol {
		t.Errorf("VolumeInfo: got %+v, want %+v", got, wantVol)
	}
}

func TestProvider_クローン作成でサーバーがエラーを返したときエラーを返す(t *testing.T) {
	srv, provider := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "conflict", http.StatusConflict)
	}))
	_ = srv

	_, err := provider.CreateClone(context.Background(), "mysql", "snap-001", "feature-login")
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestProvider_クローン削除に成功する(t *testing.T) {
	srv, provider := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/clones/feature-login" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	_ = srv

	err := provider.DeleteClone(context.Background(), "mysql", "feature-login")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestProvider_スナップショット一覧を取得する(t *testing.T) {
	now := time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC)
	wantSnaps := []domain.SnapshotInfo{
		{Name: "snap-001", CreatedAt: now},
		{Name: "snap-002", CreatedAt: now.Add(time.Hour)},
	}

	srv, provider := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/snapshots" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		type snapshotResp struct {
			Name         string `json:"name"`
			CreatedAt    string `json:"created_at"`
			DatabaseType string `json:"database_type"`
		}
		resp := []snapshotResp{
			{Name: "snap-001", CreatedAt: now.Format(time.RFC3339), DatabaseType: "mysql"},
			{Name: "snap-002", CreatedAt: now.Add(time.Hour).Format(time.RFC3339), DatabaseType: "mysql"},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	_ = srv

	got, err := provider.ListSnapshots(context.Background(), "mysql")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != len(wantSnaps) {
		t.Fatalf("ListSnapshots: got %d items, want %d", len(got), len(wantSnaps))
	}
	for i, want := range wantSnaps {
		if got[i].Name != want.Name {
			t.Errorf("[%d] Name: got %q, want %q", i, got[i].Name, want.Name)
		}
		if !got[i].CreatedAt.Equal(want.CreatedAt) {
			t.Errorf("[%d] CreatedAt: got %v, want %v", i, got[i].CreatedAt, want.CreatedAt)
		}
		if got[i].DatabaseType != "mysql" {
			t.Errorf("[%d] DatabaseType: got %q, want mysql", i, got[i].DatabaseType)
		}
	}
}

func TestProvider_トークンがAuthorizationヘッダーに含まれる(t *testing.T) {
	var gotAuth string
	srv, provider := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusCreated)
	}))
	_ = srv

	_ = provider.TakeSnapshot(context.Background(), "mysql", "snap-001", false)

	wantAuth := "Bearer test-token"
	if gotAuth != wantAuth {
		t.Errorf("Authorization: got %q, want %q", gotAuth, wantAuth)
	}
}

func TestProvider_コンテキストキャンセル時にエラーを返す(t *testing.T) {
	srv, provider := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// never respond — the context should cancel first
		select {}
	}))
	_ = srv

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := provider.TakeSnapshot(ctx, "mysql", "snap-001", false)
	if err == nil {
		t.Error("expected error on cancelled context, got nil")
	}
	if !strings.Contains(err.Error(), "context canceled") && !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("expected context error, got: %v", err)
	}
}

func TestProvider_無効なURLのときTakeSnapshotはエラーを返す(t *testing.T) {
	provider := zfsagent.NewProvider("://invalid-url", "token")
	err := provider.TakeSnapshot(context.Background(), "mysql", "snap-001", false)
	if err == nil {
		t.Error("expected error for invalid URL, got nil")
	}
}

func TestProvider_無効なURLのときCreateCloneはエラーを返す(t *testing.T) {
	provider := zfsagent.NewProvider("://invalid-url", "token")
	_, err := provider.CreateClone(context.Background(), "mysql", "snap-001", "clone-1")
	if err == nil {
		t.Error("expected error for invalid URL, got nil")
	}
}

func TestProvider_無効なURLのときDeleteCloneはエラーを返す(t *testing.T) {
	provider := zfsagent.NewProvider("://invalid-url", "token")
	err := provider.DeleteClone(context.Background(), "mysql", "clone-1")
	if err == nil {
		t.Error("expected error for invalid URL, got nil")
	}
}

func TestProvider_無効なURLのときListSnapshotsはエラーを返す(t *testing.T) {
	provider := zfsagent.NewProvider("://invalid-url", "token")
	_, err := provider.ListSnapshots(context.Background(), "mysql")
	if err == nil {
		t.Error("expected error for invalid URL, got nil")
	}
}

func TestProvider_クローン作成でレスポンスボディが不正なJSONのときエラーを返す(t *testing.T) {
	srv, provider := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("not-json"))
	}))
	_ = srv

	_, err := provider.CreateClone(context.Background(), "mysql", "snap-001", "clone-1")
	if err == nil {
		t.Error("expected error for invalid JSON response, got nil")
	}
}

func TestProvider_スナップショット一覧取得でレスポンスボディが不正なJSONのときエラーを返す(t *testing.T) {
	srv, provider := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	}))
	_ = srv

	_, err := provider.ListSnapshots(context.Background(), "mysql")
	if err == nil {
		t.Error("expected error for invalid JSON response, got nil")
	}
}

func TestProvider_スナップショット一覧取得でcreated_atが不正な日時形式のときエラーを返す(t *testing.T) {
	srv, provider := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"name":"snap-001","created_at":"not-a-date"}]`))
	}))
	_ = srv

	_, err := provider.ListSnapshots(context.Background(), "mysql")
	if err == nil {
		t.Error("expected error for invalid date format, got nil")
	}
}

func TestProvider_クローン削除で404のときエラーなし(t *testing.T) {
	// 404 = クローンが既に存在しない = 削除済みとみなして成功とする（冪等性）。
	srv, provider := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	_ = srv

	if err := provider.DeleteClone(context.Background(), "mysql", "feature-login"); err != nil {
		t.Errorf("404 should be treated as success (idempotent), got: %v", err)
	}
}

func TestProvider_クローン削除でサーバーエラーのときエラーを返す(t *testing.T) {
	srv, provider := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	_ = srv

	err := provider.DeleteClone(context.Background(), "mysql", "feature-login")
	if err == nil {
		t.Error("expected error for 500, got nil")
	}
}

func TestProvider_スナップショット一覧取得でサーバーがエラーを返したときエラーを返す(t *testing.T) {
	srv, provider := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	_ = srv

	_, err := provider.ListSnapshots(context.Background(), "mysql")
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestProvider_HTTPクライアントがエラーを返したときTakeSnapshotはエラーを返す(t *testing.T) {
	provider := newFailingProvider("http://localhost:9090")
	err := provider.TakeSnapshot(context.Background(), "mysql", "snap-001", false)
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestProvider_HTTPクライアントがエラーを返したときCreateCloneはエラーを返す(t *testing.T) {
	provider := newFailingProvider("http://localhost:9090")
	_, err := provider.CreateClone(context.Background(), "mysql", "snap-001", "clone-1")
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestProvider_HTTPクライアントがエラーを返したときDeleteCloneはエラーを返す(t *testing.T) {
	provider := newFailingProvider("http://localhost:9090")
	err := provider.DeleteClone(context.Background(), "mysql", "clone-1")
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestProvider_HTTPクライアントがエラーを返したときListSnapshotsはエラーを返す(t *testing.T) {
	provider := newFailingProvider("http://localhost:9090")
	_, err := provider.ListSnapshots(context.Background(), "mysql")
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestProvider_DeleteSnapshotに成功する(t *testing.T) {
	var gotPath string
	srv, provider := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("unexpected method: %s", r.Method)
		}
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	_ = srv

	err := provider.DeleteSnapshot(context.Background(), "mysql", "snap-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/snapshots/snap-001" {
		t.Errorf("path = %q, want /snapshots/snap-001", gotPath)
	}
}

func TestProvider_ListClonesに成功する(t *testing.T) {
	srv, provider := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/clones" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]string{
			{"clone_name": "branch-1"},
			{"clone_name": "branch-2"},
		})
	}))
	_ = srv

	got, err := provider.ListClones(context.Background(), "mysql")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0] != "branch-1" || got[1] != "branch-2" {
		t.Errorf("ListClones = %v, want [branch-1 branch-2]", got)
	}
}

func TestProvider_GCSnapshotsに成功する(t *testing.T) {
	srv, provider := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/gc" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"deleted": []string{"base-20260101-000000"}})
	}))
	_ = srv

	got, err := provider.GCSnapshots(context.Background(), "mysql", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != "base-20260101-000000" {
		t.Errorf("GCSnapshots = %v, want [base-20260101-000000]", got)
	}
}

func TestProvider_ResetDatasetに成功する(t *testing.T) {
	called := false
	srv, provider := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/reset" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		called = true
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	_ = srv

	err := provider.ResetDataset(context.Background(), "mysql")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("ResetDataset was not called")
	}
}

func TestProvider_DeleteSnapshotで404のときエラーメッセージを含む(t *testing.T) {
	srv, provider := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
	}))
	_ = srv
	err := provider.DeleteSnapshot(context.Background(), "mysql", "gone")
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestProvider_DeleteSnapshotで500のときエラーを返す(t *testing.T) {
	srv, provider := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "zfs error"})
	}))
	_ = srv
	err := provider.DeleteSnapshot(context.Background(), "mysql", "snap-x")
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestProvider_ListClonesでサーバーエラーのときエラーを返す(t *testing.T) {
	srv, provider := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", http.StatusInternalServerError)
	}))
	_ = srv
	_, err := provider.ListClones(context.Background(), "mysql")
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestProvider_ListClonesで不正なJSONのときエラーを返す(t *testing.T) {
	srv, provider := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not-json"))
	}))
	_ = srv
	_, err := provider.ListClones(context.Background(), "mysql")
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestProvider_GCSnapshotsでサーバーエラーのときエラーを返す(t *testing.T) {
	srv, provider := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "gc failed"})
	}))
	_ = srv
	_, err := provider.GCSnapshots(context.Background(), "mysql", 5)
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestProvider_GCSnapshotsで不正なJSONのときエラーを返す(t *testing.T) {
	srv, provider := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("bad-json"))
	}))
	_ = srv
	_, err := provider.GCSnapshots(context.Background(), "mysql", 5)
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestProvider_ResetDatasetでサーバーエラーのときエラーを返す(t *testing.T) {
	srv, provider := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "reset failed"})
	}))
	_ = srv
	err := provider.ResetDataset(context.Background(), "mysql")
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestProvider_dbTypeQueryは空のときを空文字を返す(t *testing.T) {
	// ListSnapshots with empty db_type → no ?db_type= suffix
	srv, provider := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("expected no query string, got %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]any{})
	}))
	_ = srv
	_, _ = provider.ListSnapshots(context.Background(), "")
}

func TestProvider_DeleteSnapshotでHTTPエラーのときエラーを返す(t *testing.T) {
	provider := newFailingProvider("http://localhost:1")
	err := provider.DeleteSnapshot(context.Background(), "mysql", "snap-x")
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestProvider_ListClonesでHTTPエラーのときエラーを返す(t *testing.T) {
	provider := newFailingProvider("http://localhost:1")
	_, err := provider.ListClones(context.Background(), "mysql")
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestProvider_GCSnapshotsでHTTPエラーのときエラーを返す(t *testing.T) {
	provider := newFailingProvider("http://localhost:1")
	_, err := provider.GCSnapshots(context.Background(), "mysql", 5)
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestProvider_ResetDatasetでHTTPエラーのときエラーを返す(t *testing.T) {
	provider := newFailingProvider("http://localhost:1")
	err := provider.ResetDataset(context.Background(), "mysql")
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestProvider_DeleteSnapshotで404かつエラーボディなしのときsnapshotNotFound(t *testing.T) {
	srv, provider := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound) // empty body
	}))
	_ = srv
	err := provider.DeleteSnapshot(context.Background(), "mysql", "snap-gone")
	if err == nil {
		t.Error("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "snap-gone") {
		t.Errorf("error should mention snap-gone: %v", err)
	}
}

func TestProvider_GCSnapshotsで500かつエラーボディなしのときgenericエラー(t *testing.T) {
	srv, provider := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	_ = srv
	_, err := provider.GCSnapshots(context.Background(), "mysql", 5)
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestProvider_ResetDatasetで500かつエラーボディなしのときgenericエラー(t *testing.T) {
	srv, provider := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	_ = srv
	err := provider.ResetDataset(context.Background(), "mysql")
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestProvider_TakeSnapshotで500かつerrorフィールドなしのときgenericエラー(t *testing.T) {
	srv, provider := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	_ = srv
	err := provider.TakeSnapshot(context.Background(), "mysql", "snap", false)
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestProvider_無効なURLのときDeleteSnapshotはエラーを返す(t *testing.T) {
	provider := zfsagent.NewProvider("://invalid", "token")
	err := provider.DeleteSnapshot(context.Background(), "mysql", "snap")
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestProvider_無効なURLのときListClonesはエラーを返す(t *testing.T) {
	provider := zfsagent.NewProvider("://invalid", "token")
	_, err := provider.ListClones(context.Background(), "mysql")
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestProvider_無効なURLのときGCSnapshotsはエラーを返す(t *testing.T) {
	provider := zfsagent.NewProvider("://invalid", "token")
	_, err := provider.GCSnapshots(context.Background(), "mysql", 5)
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestProvider_無効なURLのときResetDatasetはエラーを返す(t *testing.T) {
	provider := zfsagent.NewProvider("://invalid", "token")
	err := provider.ResetDataset(context.Background(), "mysql")
	if err == nil {
		t.Error("expected error, got nil")
	}
}
