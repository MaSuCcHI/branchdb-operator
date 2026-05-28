package zfsagent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/keisuke/zfs-db-k8s/internal/domain"
)

// ErrNotFound はリソースが見つからない場合に VolumeProvider が返すエラー。
var ErrNotFound = errors.New("not found")

// AgentVolumeProvider は ZFS Agent が必要とする操作の抽象インターフェース。
// domain.VolumeProvider を拡張してクローン一覧・単一取得を追加している。
type AgentVolumeProvider interface {
	TakeSnapshot(ctx context.Context, name string) error
	ListSnapshots(ctx context.Context) ([]domain.SnapshotInfo, error)
	CreateClone(ctx context.Context, snapshot, cloneName string) (domain.VolumeInfo, error)
	DeleteClone(ctx context.Context, cloneName string) error
	ListClones(ctx context.Context) ([]domain.VolumeInfo, error)
	GetClone(ctx context.Context, cloneName string) (domain.VolumeInfo, error)
}

// Handler は ZFS Agent の HTTP ハンドラ。
type Handler struct {
	provider AgentVolumeProvider
	token    string
}

// NewHandler は Handler を生成する。
func NewHandler(provider AgentVolumeProvider, token string) *Handler {
	return &Handler{provider: provider, token: token}
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

	return r
}

// authMiddleware は Bearer トークン認証を行う。
func (h *Handler) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token != h.token {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- スナップショット ---

type createSnapshotRequest struct {
	Name string `json:"name"`
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
	if err := h.provider.TakeSnapshot(r.Context(), req.Name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
}

type snapshotResponse struct {
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

func (h *Handler) handleListSnapshots(w http.ResponseWriter, r *http.Request) {
	snapshots, err := h.provider.ListSnapshots(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp := make([]snapshotResponse, len(snapshots))
	for i, s := range snapshots {
		resp[i] = snapshotResponse{
			Name:      s.Name,
			CreatedAt: s.CreatedAt.Format("2006-01-02T15:04:05Z"),
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleDeleteSnapshot(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.provider.DeleteClone(r.Context(), name); err != nil {
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
	vol, err := h.provider.CreateClone(r.Context(), req.Snapshot, req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toCloneResponse(vol))
}

func (h *Handler) handleListClones(w http.ResponseWriter, r *http.Request) {
	clones, err := h.provider.ListClones(r.Context())
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
	vol, err := h.provider.GetClone(r.Context(), name)
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
	if err := h.provider.DeleteClone(r.Context(), name); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
