package zfsagent

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/MaSuCcHI/branchdb-operator/internal/domain"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// ErrNotFound はリソースが見つからない場合に VolumeProvider が返すエラー。
var ErrNotFound = errors.New("not found")

// AgentVolumeProvider は ZFS Agent が必要とする操作の抽象インターフェース。
// domain.VolumeProvider を拡張してクローン一覧・単一取得を追加している。
type AgentVolumeProvider interface {
	TakeSnapshot(ctx context.Context, name string, overwrite bool) error
	DeleteSnapshot(ctx context.Context, name string) error
	ListSnapshots(ctx context.Context) ([]domain.SnapshotInfo, error)
	GCSnapshots(ctx context.Context, keepCount int) ([]string, error)
	ResetDataset(ctx context.Context) error
	CreateClone(ctx context.Context, snapshot, cloneName string) (domain.VolumeInfo, error)
	DeleteClone(ctx context.Context, cloneName string) error
	ListClones(ctx context.Context) ([]domain.VolumeInfo, error)
	GetClone(ctx context.Context, cloneName string) (domain.VolumeInfo, error)
}

// Handler は ZFS Agent の HTTP ハンドラ。
// providers は dbType → AgentVolumeProvider のマップで、複数 dataset に対応する。
// defaultType は db_type クエリパラメータが省略されたときに使用する dbType。
type Handler struct {
	providers   map[string]AgentVolumeProvider
	defaultType string
	token       string
}

// NewHandler は Handler を生成する。
// defaultType は db_type クエリパラメータ省略時に使用するプロバイダーで、
// 単一エントリならそのエントリ、複数エントリなら "mysql"（存在する場合）を決定的に選ぶ。
// mysql を含まない複数エントリ構成では defaultType は空となり、db_type が必須になる。
func NewHandler(providers map[string]AgentVolumeProvider, token string) *Handler {
	defaultType := ""
	if len(providers) == 1 {
		for k := range providers {
			defaultType = k
		}
	} else if _, ok := providers["mysql"]; ok {
		// システム全体の慣例（db_type 省略 = mysql）に合わせる。
		defaultType = "mysql"
	}
	return &Handler{providers: providers, defaultType: defaultType, token: token}
}

// NewRouter は ZFS Agent 用のルーターを返す。
func NewRouter(h *Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(h.authMiddleware)

	r.Post("/snapshots", h.handleCreateSnapshot)
	r.Get("/snapshots", h.handleListSnapshots)
	r.Delete("/snapshots/{name}", h.handleDeleteSnapshot)

	r.Post("/clones", h.handleCreateClone)
	r.Get("/clones", h.handleListClones)
	r.Get("/clones/{name}", h.handleGetClone)
	r.Delete("/clones/{name}", h.handleDeleteClone)

	r.Post("/gc", h.handleGC)
	r.Post("/reset", h.handleReset)

	return r
}

// pickProvider は ?db_type= クエリパラメータでプロバイダーを選択する。
// パラメータが省略された場合はデフォルトプロバイダーを使用する。
// デフォルトプロバイダーが設定されていない（複数プロバイダー構成）かつ db_type が省略された場合は
// nil と "db_type is required" エラーを返す。
func (h *Handler) pickProvider(r *http.Request) (AgentVolumeProvider, error) {
	dbType := r.URL.Query().Get("db_type")
	if dbType == "" {
		if h.defaultType == "" {
			return nil, errors.New("db_type is required when multiple datasets are configured")
		}
		dbType = h.defaultType
	}
	if p, ok := h.providers[dbType]; ok {
		return p, nil
	}
	return nil, fmt.Errorf("unknown db_type %q", dbType)
}

// authMiddleware は Bearer トークン認証を行う。
// タイミング攻撃を防ぐため crypto/subtle.ConstantTimeCompare でトークンを比較する。
func (h *Handler) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if subtle.ConstantTimeCompare([]byte(token), []byte(h.token)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- スナップショット ---

type createSnapshotRequest struct {
	Name      string `json:"name"`
	Overwrite bool   `json:"overwrite"`
}

func (h *Handler) handleCreateSnapshot(w http.ResponseWriter, r *http.Request) {
	var req createSnapshotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	provider, err := h.pickProvider(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := provider.TakeSnapshot(r.Context(), req.Name, req.Overwrite); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
}

type snapshotResponse struct {
	Name         string `json:"name"`
	CreatedAt    string `json:"created_at"`
	DatabaseType string `json:"database_type,omitempty"`
}

func (h *Handler) handleListSnapshots(w http.ResponseWriter, r *http.Request) {
	dbType := r.URL.Query().Get("db_type")
	if dbType == "" {
		dbType = h.defaultType
	}
	provider, err := h.pickProvider(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	snapshots, err := provider.ListSnapshots(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp := make([]snapshotResponse, len(snapshots))
	for i, s := range snapshots {
		resp[i] = snapshotResponse{
			Name:         s.Name,
			CreatedAt:    s.CreatedAt.UTC().Format(time.RFC3339),
			DatabaseType: dbType,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleDeleteSnapshot(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	provider, err := h.pickProvider(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := provider.DeleteSnapshot(r.Context(), name); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- クローン ---

type createCloneRequest struct {
	Snapshot string `json:"snapshot"`
	Name     string `json:"name"`
}

type cloneResponse struct {
	CloneName string `json:"clone_name"`
	NFSServer string `json:"nfs_server"`
	NFSPath   string `json:"nfs_path"`
}

func toCloneResponse(v domain.VolumeInfo) cloneResponse {
	return cloneResponse{
		CloneName: v.CloneName,
		NFSServer: v.NFSServer,
		NFSPath:   v.NFSPath,
	}
}

func (h *Handler) handleCreateClone(w http.ResponseWriter, r *http.Request) {
	var req createCloneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Snapshot == "" {
		writeError(w, http.StatusBadRequest, "snapshot is required")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	provider, err := h.pickProvider(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	vol, err := provider.CreateClone(r.Context(), req.Snapshot, req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toCloneResponse(vol))
}

func (h *Handler) handleListClones(w http.ResponseWriter, r *http.Request) {
	provider, err := h.pickProvider(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	clones, err := provider.ListClones(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp := make([]cloneResponse, len(clones))
	for i, v := range clones {
		resp[i] = toCloneResponse(v)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleGetClone(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	provider, err := h.pickProvider(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	vol, err := provider.GetClone(r.Context(), name)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toCloneResponse(vol))
}

func (h *Handler) handleDeleteClone(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	provider, err := h.pickProvider(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := provider.DeleteClone(r.Context(), name); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- GC / Reset ---

type gcRequest struct {
	KeepSnapshots int `json:"keep_snapshots"`
}

func (h *Handler) handleGC(w http.ResponseWriter, r *http.Request) {
	var req gcRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.KeepSnapshots <= 0 {
		req.KeepSnapshots = 5
	}
	provider, err := h.pickProvider(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	deleted, err := provider.GCSnapshots(r.Context(), req.KeepSnapshots)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
}

func (h *Handler) handleReset(w http.ResponseWriter, r *http.Request) {
	provider, err := h.pickProvider(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := provider.ResetDataset(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- ユーティリティ ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
