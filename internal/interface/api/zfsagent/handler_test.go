package zfsagent_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MaSuCcHI/branchdb-operator/internal/domain"
	"github.com/MaSuCcHI/branchdb-operator/internal/interface/api/zfsagent"
)

// --- モック ---

type mockVolumeProvider struct {
	takeSnapshotFunc  func(ctx context.Context, name string, overwrite bool) error
	createCloneFunc   func(ctx context.Context, snapshot, cloneName string) (domain.VolumeInfo, error)
	deleteCloneFunc   func(ctx context.Context, cloneName string) error
	listSnapshotsFunc func(ctx context.Context) ([]domain.SnapshotInfo, error)
	listClonesFunc    func(ctx context.Context) ([]domain.VolumeInfo, error)
	getCloneFunc      func(ctx context.Context, cloneName string) (domain.VolumeInfo, error)
}

func (m *mockVolumeProvider) TakeSnapshot(ctx context.Context, name string, overwrite bool) error {
	if m.takeSnapshotFunc != nil {
		return m.takeSnapshotFunc(ctx, name, overwrite)
	}
	return nil
}

func (m *mockVolumeProvider) DeleteSnapshot(ctx context.Context, name string) error {
	if m.deleteCloneFunc != nil {
		return m.deleteCloneFunc(ctx, name)
	}
	return nil
}

func (m *mockVolumeProvider) CreateClone(ctx context.Context, snapshot, cloneName string) (domain.VolumeInfo, error) {
	return m.createCloneFunc(ctx, snapshot, cloneName)
}

func (m *mockVolumeProvider) DeleteClone(ctx context.Context, cloneName string) error {
	return m.deleteCloneFunc(ctx, cloneName)
}

func (m *mockVolumeProvider) ListSnapshots(ctx context.Context) ([]domain.SnapshotInfo, error) {
	return m.listSnapshotsFunc(ctx)
}

func (m *mockVolumeProvider) ListClones(ctx context.Context) ([]domain.VolumeInfo, error) {
	return m.listClonesFunc(ctx)
}

func (m *mockVolumeProvider) GetClone(ctx context.Context, cloneName string) (domain.VolumeInfo, error) {
	return m.getCloneFunc(ctx, cloneName)
}

// --- ヘルパー ---

const testToken = "secret-token"

func newRouter(provider *mockVolumeProvider) http.Handler {
	h := zfsagent.NewHandler(map[string]zfsagent.AgentVolumeProvider{"mysql": provider}, testToken)
	return zfsagent.NewRouter(h)
}

func authorizedRequest(method, path string, body []byte) *http.Request {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	return req
}

func unauthorizedRequest(method, path string) *http.Request {
	return httptest.NewRequest(method, path, nil)
}

// --- 認証テスト ---

func TestHandler_認証トークンなしのリクエストは401を返す(t *testing.T) {
	provider := &mockVolumeProvider{}
	router := newRouter(provider)

	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/snapshots"},
		{http.MethodPost, "/snapshots"},
		{http.MethodDelete, "/snapshots/snap-001"},
		{http.MethodGet, "/clones"},
		{http.MethodPost, "/clones"},
		{http.MethodGet, "/clones/feature-login"},
		{http.MethodDelete, "/clones/feature-login"},
	}

	for _, e := range endpoints {
		t.Run(e.method+" "+e.path, func(t *testing.T) {
			req := unauthorizedRequest(e.method, e.path)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusUnauthorized {
				t.Errorf("got status %d, want 401", w.Code)
			}
		})
	}
}

func TestHandler_誤ったトークンのリクエストは401を返す(t *testing.T) {
	provider := &mockVolumeProvider{}
	router := newRouter(provider)

	req := httptest.NewRequest(http.MethodGet, "/snapshots", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want 401", w.Code)
	}
}

// --- スナップショット作成テスト ---

func TestHandler_正常なスナップショット作成は201を返す(t *testing.T) {
	provider := &mockVolumeProvider{
		takeSnapshotFunc: func(ctx context.Context, name string, overwrite bool) error {
			return nil
		},
	}
	router := newRouter(provider)

	body, _ := json.Marshal(map[string]string{"name": "snap-20260526"})
	req := authorizedRequest(http.MethodPost, "/snapshots", body)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("got status %d, want 201", w.Code)
	}
}

func TestHandler_スナップショット名が空のとき400を返す(t *testing.T) {
	provider := &mockVolumeProvider{}
	router := newRouter(provider)

	body, _ := json.Marshal(map[string]string{"name": ""})
	req := authorizedRequest(http.MethodPost, "/snapshots", body)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want 400", w.Code)
	}
}

func TestHandler_スナップショット作成でProviderがエラーを返したとき500を返す(t *testing.T) {
	provider := &mockVolumeProvider{
		takeSnapshotFunc: func(ctx context.Context, name string, overwrite bool) error {
			return errors.New("ZFS error")
		},
	}
	router := newRouter(provider)

	body, _ := json.Marshal(map[string]string{"name": "snap-20260526"})
	req := authorizedRequest(http.MethodPost, "/snapshots", body)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("got status %d, want 500", w.Code)
	}
}

func TestHandler_overwrite_trueのとき上書きフラグがproviderに渡される(t *testing.T) {
	var gotOverwrite bool
	provider := &mockVolumeProvider{
		takeSnapshotFunc: func(ctx context.Context, name string, overwrite bool) error {
			gotOverwrite = overwrite
			return nil
		},
	}
	router := newRouter(provider)

	body, _ := json.Marshal(map[string]any{"name": "base", "overwrite": true})
	req := authorizedRequest(http.MethodPost, "/snapshots", body)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("got status %d, want 201", w.Code)
	}
	if !gotOverwrite {
		t.Error("overwrite flag was not passed to provider")
	}
}

func TestHandler_不正なJSONでスナップショット作成すると400を返す(t *testing.T) {
	provider := &mockVolumeProvider{}
	router := newRouter(provider)

	req := authorizedRequest(http.MethodPost, "/snapshots", []byte("invalid json"))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want 400", w.Code)
	}
}

// --- スナップショット一覧テスト ---

func TestHandler_スナップショット一覧は200とリストを返す(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	provider := &mockVolumeProvider{
		listSnapshotsFunc: func(ctx context.Context) ([]domain.SnapshotInfo, error) {
			return []domain.SnapshotInfo{
				{Name: "snap-20260526", CreatedAt: now},
			}, nil
		},
	}
	router := newRouter(provider)

	req := authorizedRequest(http.MethodGet, "/snapshots?db_type=mysql", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}
	var resp []map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp) != 1 {
		t.Errorf("got %d items, want 1", len(resp))
	}
	if resp[0]["name"] != "snap-20260526" {
		t.Errorf("got name %q, want %q", resp[0]["name"], "snap-20260526")
	}
	if resp[0]["database_type"] != "mysql" {
		t.Errorf("got database_type %q, want %q", resp[0]["database_type"], "mysql")
	}
}

func TestHandler_スナップショット一覧でProviderがエラーを返したとき500を返す(t *testing.T) {
	provider := &mockVolumeProvider{
		listSnapshotsFunc: func(ctx context.Context) ([]domain.SnapshotInfo, error) {
			return nil, errors.New("ZFS error")
		},
	}
	router := newRouter(provider)

	req := authorizedRequest(http.MethodGet, "/snapshots", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("got status %d, want 500", w.Code)
	}
}

// --- スナップショット削除テスト ---

func TestHandler_正常なスナップショット削除は204を返す(t *testing.T) {
	var deletedName string
	provider := &mockVolumeProvider{
		takeSnapshotFunc: func(ctx context.Context, name string, overwrite bool) error { return nil },
		deleteCloneFunc: func(ctx context.Context, cloneName string) error {
			deletedName = cloneName
			return nil
		},
	}
	router := newRouter(provider)

	req := authorizedRequest(http.MethodDelete, "/snapshots/snap-20260526", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("got status %d, want 204", w.Code)
	}
	if deletedName != "snap-20260526" {
		t.Errorf("got deleted name %q, want %q", deletedName, "snap-20260526")
	}
}

func TestHandler_存在しないスナップショット削除でProviderがエラーを返したとき404を返す(t *testing.T) {
	provider := &mockVolumeProvider{
		deleteCloneFunc: func(ctx context.Context, cloneName string) error {
			return zfsagent.ErrNotFound
		},
	}
	router := newRouter(provider)

	req := authorizedRequest(http.MethodDelete, "/snapshots/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("got status %d, want 404", w.Code)
	}
}

func TestHandler_スナップショット削除でProviderが一般エラーを返したとき500を返す(t *testing.T) {
	provider := &mockVolumeProvider{
		deleteCloneFunc: func(ctx context.Context, cloneName string) error {
			return errors.New("ZFS error")
		},
	}
	router := newRouter(provider)

	req := authorizedRequest(http.MethodDelete, "/snapshots/snap-001", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("got status %d, want 500", w.Code)
	}
}

// --- クローン作成テスト ---

func TestHandler_正常なクローン作成は201とVolumeInfoを返す(t *testing.T) {
	provider := &mockVolumeProvider{
		createCloneFunc: func(ctx context.Context, snapshot, cloneName string) (domain.VolumeInfo, error) {
			return domain.VolumeInfo{
				CloneName: cloneName,
				NFSServer: "10.0.0.5",
				NFSPath:   "/tank/branches/" + cloneName,
			}, nil
		},
	}
	router := newRouter(provider)

	body, _ := json.Marshal(map[string]string{
		"snapshot": "snap-20260526",
		"name":     "feature-login",
	})
	req := authorizedRequest(http.MethodPost, "/clones", body)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("got status %d, want 201", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["clone_name"] != "feature-login" {
		t.Errorf("got clone_name %q, want %q", resp["clone_name"], "feature-login")
	}
	if resp["nfs_server"] != "10.0.0.5" {
		t.Errorf("got nfs_server %q, want %q", resp["nfs_server"], "10.0.0.5")
	}
	if resp["nfs_path"] != "/tank/branches/feature-login" {
		t.Errorf("got nfs_path %q, want %q", resp["nfs_path"], "/tank/branches/feature-login")
	}
}

func TestHandler_クローン作成でスナップショット名が空のとき400を返す(t *testing.T) {
	provider := &mockVolumeProvider{}
	router := newRouter(provider)

	body, _ := json.Marshal(map[string]string{"snapshot": "", "name": "feature-login"})
	req := authorizedRequest(http.MethodPost, "/clones", body)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want 400", w.Code)
	}
}

func TestHandler_クローン作成でクローン名が空のとき400を返す(t *testing.T) {
	provider := &mockVolumeProvider{}
	router := newRouter(provider)

	body, _ := json.Marshal(map[string]string{"snapshot": "snap-20260526", "name": ""})
	req := authorizedRequest(http.MethodPost, "/clones", body)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want 400", w.Code)
	}
}

func TestHandler_クローン作成でProviderがエラーを返したとき500を返す(t *testing.T) {
	provider := &mockVolumeProvider{
		createCloneFunc: func(ctx context.Context, snapshot, cloneName string) (domain.VolumeInfo, error) {
			return domain.VolumeInfo{}, errors.New("ZFS error")
		},
	}
	router := newRouter(provider)

	body, _ := json.Marshal(map[string]string{"snapshot": "snap-20260526", "name": "feature-login"})
	req := authorizedRequest(http.MethodPost, "/clones", body)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("got status %d, want 500", w.Code)
	}
}

func TestHandler_不正なJSONでクローン作成すると400を返す(t *testing.T) {
	provider := &mockVolumeProvider{}
	router := newRouter(provider)

	req := authorizedRequest(http.MethodPost, "/clones", []byte("invalid json"))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want 400", w.Code)
	}
}

// --- クローン一覧テスト ---

func TestHandler_クローン一覧は200とリストを返す(t *testing.T) {
	provider := &mockVolumeProvider{
		listClonesFunc: func(ctx context.Context) ([]domain.VolumeInfo, error) {
			return []domain.VolumeInfo{
				{CloneName: "feature-login", NFSServer: "10.0.0.5", NFSPath: "/tank/branches/feature-login"},
			}, nil
		},
	}
	router := newRouter(provider)

	req := authorizedRequest(http.MethodGet, "/clones", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}
	var resp []map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp) != 1 {
		t.Errorf("got %d items, want 1", len(resp))
	}
	if resp[0]["clone_name"] != "feature-login" {
		t.Errorf("got clone_name %q, want %q", resp[0]["clone_name"], "feature-login")
	}
}

func TestHandler_クローン一覧でProviderがエラーを返したとき500を返す(t *testing.T) {
	provider := &mockVolumeProvider{
		listClonesFunc: func(ctx context.Context) ([]domain.VolumeInfo, error) {
			return nil, errors.New("ZFS error")
		},
	}
	router := newRouter(provider)

	req := authorizedRequest(http.MethodGet, "/clones", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("got status %d, want 500", w.Code)
	}
}

// --- クローン取得テスト ---

func TestHandler_正常なクローン取得は200とVolumeInfoを返す(t *testing.T) {
	provider := &mockVolumeProvider{
		getCloneFunc: func(ctx context.Context, cloneName string) (domain.VolumeInfo, error) {
			return domain.VolumeInfo{
				CloneName: cloneName,
				NFSServer: "10.0.0.5",
				NFSPath:   "/tank/branches/" + cloneName,
			}, nil
		},
	}
	router := newRouter(provider)

	req := authorizedRequest(http.MethodGet, "/clones/feature-login", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["clone_name"] != "feature-login" {
		t.Errorf("got clone_name %q, want %q", resp["clone_name"], "feature-login")
	}
}

func TestHandler_存在しないクローン取得で404を返す(t *testing.T) {
	provider := &mockVolumeProvider{
		getCloneFunc: func(ctx context.Context, cloneName string) (domain.VolumeInfo, error) {
			return domain.VolumeInfo{}, zfsagent.ErrNotFound
		},
	}
	router := newRouter(provider)

	req := authorizedRequest(http.MethodGet, "/clones/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("got status %d, want 404", w.Code)
	}
}

func TestHandler_クローン取得でProviderが一般エラーを返したとき500を返す(t *testing.T) {
	provider := &mockVolumeProvider{
		getCloneFunc: func(ctx context.Context, cloneName string) (domain.VolumeInfo, error) {
			return domain.VolumeInfo{}, errors.New("ZFS error")
		},
	}
	router := newRouter(provider)

	req := authorizedRequest(http.MethodGet, "/clones/feature-login", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("got status %d, want 500", w.Code)
	}
}

// --- クローン削除テスト ---

func TestHandler_正常なクローン削除は204を返す(t *testing.T) {
	var deletedName string
	provider := &mockVolumeProvider{
		deleteCloneFunc: func(ctx context.Context, cloneName string) error {
			deletedName = cloneName
			return nil
		},
	}
	router := newRouter(provider)

	req := authorizedRequest(http.MethodDelete, "/clones/feature-login", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("got status %d, want 204", w.Code)
	}
	if deletedName != "feature-login" {
		t.Errorf("got deleted name %q, want %q", deletedName, "feature-login")
	}
}

func TestHandler_存在しないクローン削除でProviderがエラーを返したとき404を返す(t *testing.T) {
	provider := &mockVolumeProvider{
		deleteCloneFunc: func(ctx context.Context, cloneName string) error {
			return zfsagent.ErrNotFound
		},
	}
	router := newRouter(provider)

	req := authorizedRequest(http.MethodDelete, "/clones/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("got status %d, want 404", w.Code)
	}
}

func TestHandler_クローン削除でProviderが一般エラーを返したとき500を返す(t *testing.T) {
	provider := &mockVolumeProvider{
		deleteCloneFunc: func(ctx context.Context, cloneName string) error {
			return errors.New("ZFS error")
		},
	}
	router := newRouter(provider)

	req := authorizedRequest(http.MethodDelete, "/clones/feature-login", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("got status %d, want 500", w.Code)
	}
}
