package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/go-chi/chi/v5"
	v1alpha1 "github.com/MaSuCcHI/branchdb-operator/api/v1alpha1"
	"github.com/MaSuCcHI/branchdb-operator/internal/domain"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DatabaseBranchClient is the minimal K8s client interface used by K8sBranchHandler.
type DatabaseBranchClient interface {
	Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error
	Get(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error
	List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error
	Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error
}

// BranchResponse is the response body for K8s branch operations.
type BranchResponse struct {
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	Host        string     `json:"host,omitempty"`
	Port        int        `json:"port,omitempty"`
	DSN         string     `json:"dsn,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	Message     string     `json:"message,omitempty"`
	ClusterHost string     `json:"cluster_host,omitempty"`
	ClusterPort int        `json:"cluster_port,omitempty"`
	SnapshotRef string     `json:"snapshot_ref,omitempty"`
	TTLHours    int        `json:"ttl_hours,omitempty"`
}

// K8sStats holds phase counts for all branches.
type K8sStats struct {
	Total    int `json:"total"`
	Ready    int `json:"ready"`
	Creating int `json:"creating"`
	Error    int `json:"error"`
	Pending  int `json:"pending"`
	Deleting int `json:"deleting"`
}

// PodInfo holds basic pod status.
type PodInfo struct {
	Phase   string `json:"phase"`
	Ready   bool   `json:"ready"`
	Message string `json:"message,omitempty"`
}

// BranchMetrics holds MySQL metrics for a branch.
type BranchMetrics struct {
	ThreadsConnected int    `json:"threads_connected"`
	Available        bool   `json:"available"`
	ErrorMsg         string `json:"error,omitempty"`
}

// K8sSnapshotResponse is the response for snapshot operations.
type K8sSnapshotResponse struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// K8sBranchHandler handles branch CRUD via DatabaseBranch CRs.
// ExternalPort は NodePort として K8s が割り当てるため、ハンドラ側でポート管理は行わない。
type K8sBranchHandler struct {
	k8sClient       DatabaseBranchClient
	volumeProvider  domain.VolumeProvider // optional; nil = snapshots unavailable
	externalHost    string
	namespace       string
	portWaitTimeout time.Duration
}

// NewK8sBranchHandler creates a new K8sBranchHandler. Namespace defaults to "default".
func NewK8sBranchHandler(k8sClient DatabaseBranchClient, externalHost string) *K8sBranchHandler {
	return &K8sBranchHandler{
		k8sClient:       k8sClient,
		externalHost:    externalHost,
		namespace:       "default",
		portWaitTimeout: 5 * time.Second,
	}
}

// WithNamespace sets the Kubernetes namespace.
func (h *K8sBranchHandler) WithNamespace(ns string) *K8sBranchHandler {
	h.namespace = ns
	return h
}

// WithPortWaitTimeout overrides the default 5-second poll timeout for external port assignment.
func (h *K8sBranchHandler) WithPortWaitTimeout(d time.Duration) *K8sBranchHandler {
	h.portWaitTimeout = d
	return h
}

// WithVolumeProvider sets an optional VolumeProvider for snapshot operations.
func (h *K8sBranchHandler) WithVolumeProvider(vp domain.VolumeProvider) *K8sBranchHandler {
	h.volumeProvider = vp
	return h
}

type k8sCreateRequest struct {
	Name        string `json:"name"`
	SnapshotRef string `json:"snapshot_ref,omitempty"`
	TTLHours    int    `json:"ttl_hours,omitempty"`
}

func (h *K8sBranchHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req k8sCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	cr := &v1alpha1.DatabaseBranch{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: h.namespace,
		},
		Spec: v1alpha1.DatabaseBranchSpec{
			SnapshotRef: req.SnapshotRef,
			TTLHours:    req.TTLHours,
		},
	}

	if err := h.k8sClient.Create(r.Context(), cr); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	created := h.pollForPort(r.Context(), req.Name, h.portWaitTimeout, 100*time.Millisecond)

	resp := h.toBranchResponse(created)
	resp.Status = "creating"
	writeJSON(w, http.StatusAccepted, resp)
}

// pollForPort polls the CR until externalPort > 0 or the deadline elapses.
func (h *K8sBranchHandler) pollForPort(ctx context.Context, name string, timeout, interval time.Duration) *v1alpha1.DatabaseBranch {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var cr v1alpha1.DatabaseBranch
		if err := h.k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: h.namespace}, &cr); err == nil {
			if cr.Status.ExternalPort > 0 {
				return &cr
			}
		}
		select {
		case <-ctx.Done():
			break
		case <-time.After(interval):
		}
	}
	var cr v1alpha1.DatabaseBranch
	_ = h.k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: h.namespace}, &cr)
	return &cr
}

func (h *K8sBranchHandler) handleList(w http.ResponseWriter, r *http.Request) {
	var list v1alpha1.DatabaseBranchList
	if err := h.k8sClient.List(r.Context(), &list, client.InNamespace(h.namespace)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := make([]BranchResponse, len(list.Items))
	for i := range list.Items {
		resp[i] = h.toBranchResponse(&list.Items[i])
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *K8sBranchHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var cr v1alpha1.DatabaseBranch
	if err := h.k8sClient.Get(r.Context(), types.NamespacedName{Name: name, Namespace: h.namespace}, &cr); err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "branch not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, h.toBranchResponse(&cr))
}

func (h *K8sBranchHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var cr v1alpha1.DatabaseBranch
	if err := h.k8sClient.Get(r.Context(), types.NamespacedName{Name: name, Namespace: h.namespace}, &cr); err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "branch not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.k8sClient.Delete(r.Context(), &cr); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *K8sBranchHandler) handleStats(w http.ResponseWriter, r *http.Request) {
	var list v1alpha1.DatabaseBranchList
	if err := h.k8sClient.List(r.Context(), &list, client.InNamespace(h.namespace)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	stats := K8sStats{Total: len(list.Items)}
	for i := range list.Items {
		switch list.Items[i].Status.Phase {
		case v1alpha1.BranchPhaseReady:
			stats.Ready++
		case v1alpha1.BranchPhaseCreating:
			stats.Creating++
		case v1alpha1.BranchPhaseError:
			stats.Error++
		case v1alpha1.BranchPhasePending:
			stats.Pending++
		case v1alpha1.BranchPhaseDeleting:
			stats.Deleting++
		}
	}
	writeJSON(w, http.StatusOK, stats)
}

func (h *K8sBranchHandler) handleGetPod(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	podName := "branchdb-mysql-" + name

	var pod corev1.Pod
	if err := h.k8sClient.Get(r.Context(), types.NamespacedName{Name: podName, Namespace: h.namespace}, &pod); err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	ready := false
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			ready = true
			break
		}
	}

	info := PodInfo{
		Phase:   string(pod.Status.Phase),
		Ready:   ready,
		Message: pod.Status.Message,
	}
	writeJSON(w, http.StatusOK, info)
}

func (h *K8sBranchHandler) handleGetMetrics(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	var cr v1alpha1.DatabaseBranch
	if err := h.k8sClient.Get(r.Context(), types.NamespacedName{Name: name, Namespace: h.namespace}, &cr); err != nil {
		writeJSON(w, http.StatusOK, BranchMetrics{Available: false, ErrorMsg: err.Error()})
		return
	}

	if cr.Status.ClusterHost == "" {
		writeJSON(w, http.StatusOK, BranchMetrics{Available: false, ErrorMsg: "cluster host not yet assigned"})
		return
	}

	dsn := fmt.Sprintf("root@tcp(%s:3306)/", cr.Status.ClusterHost)
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		writeJSON(w, http.StatusOK, BranchMetrics{Available: false, ErrorMsg: err.Error()})
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "SHOW STATUS LIKE 'Threads_connected'")
	if err != nil {
		writeJSON(w, http.StatusOK, BranchMetrics{Available: false, ErrorMsg: err.Error()})
		return
	}
	defer rows.Close()

	var varName, varValue string
	if rows.Next() {
		if err := rows.Scan(&varName, &varValue); err != nil {
			writeJSON(w, http.StatusOK, BranchMetrics{Available: false, ErrorMsg: err.Error()})
			return
		}
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusOK, BranchMetrics{Available: false, ErrorMsg: err.Error()})
		return
	}

	threads := 0
	fmt.Sscanf(varValue, "%d", &threads)
	writeJSON(w, http.StatusOK, BranchMetrics{ThreadsConnected: threads, Available: true})
}

func (h *K8sBranchHandler) handleListSnapshots(w http.ResponseWriter, r *http.Request) {
	if h.volumeProvider == nil {
		writeError(w, http.StatusNotImplemented, "VolumeProvider not configured")
		return
	}

	snaps, err := h.volumeProvider.ListSnapshots(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := make([]K8sSnapshotResponse, len(snaps))
	for i, s := range snaps {
		resp[i] = K8sSnapshotResponse{
			Name:      s.Name,
			CreatedAt: s.CreatedAt,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *K8sBranchHandler) handleTakeSnapshot(w http.ResponseWriter, r *http.Request) {
	if h.volumeProvider == nil {
		writeError(w, http.StatusNotImplemented, "VolumeProvider not configured")
		return
	}

	if err := h.volumeProvider.TakeSnapshot(r.Context(), "auto"); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *K8sBranchHandler) toBranchResponse(cr *v1alpha1.DatabaseBranch) BranchResponse {
	resp := BranchResponse{
		Name:        cr.Name,
		Status:      string(cr.Status.Phase),
		CreatedAt:   cr.CreationTimestamp.Time,
		Message:     cr.Status.Message,
		ClusterHost: cr.Status.ClusterHost,
		ClusterPort: cr.Status.ClusterPort,
		SnapshotRef: cr.Spec.SnapshotRef,
		TTLHours:    cr.Spec.TTLHours,
	}
	if resp.Status == "" {
		resp.Status = "creating"
	}

	if cr.Status.ExpiresAt != nil {
		t := cr.Status.ExpiresAt.Time
		resp.ExpiresAt = &t
	}

	if cr.Status.ExternalPort > 0 {
		resp.Port = cr.Status.ExternalPort
		resp.Host = h.externalHost
		resp.DSN = fmt.Sprintf("root@tcp(%s:%d)/", h.externalHost, cr.Status.ExternalPort)
	}

	return resp
}

// isNotFound checks if the error is a Kubernetes NotFound error.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	return client.IgnoreNotFound(err) == nil
}

// NewK8sRouter creates an http.Handler for the K8s mode API.
// Pass a non-nil hub to enable the WebSocket broadcast endpoint at /ws.
func NewK8sRouter(h *K8sBranchHandler, hub ...*WSHub) http.Handler {
	r := chi.NewRouter()
	r.Get("/branches", h.handleList)
	r.Post("/branches", h.handleCreate)
	r.Get("/branches/{name}", h.handleGet)
	r.Delete("/branches/{name}", h.handleDelete)
	r.Get("/branches/{name}/pod", h.handleGetPod)
	r.Get("/branches/{name}/metrics", h.handleGetMetrics)
	r.Get("/stats", h.handleStats)
	r.Get("/snapshots", h.handleListSnapshots)
	r.Post("/snapshots", h.handleTakeSnapshot)
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	if len(hub) > 0 && hub[0] != nil {
		r.Get("/ws", hub[0].ServeWS)
	}
	r.Get("/", serveK8sSPA)
	r.Get("/assets/*", k8sStaticHandler().ServeHTTP)
	r.Get("/favicon.svg", k8sStaticHandler().ServeHTTP)
	r.Get("/icons.svg", k8sStaticHandler().ServeHTTP)
	return r
}
