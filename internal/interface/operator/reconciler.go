// Package operator contains the Kubernetes Operator reconciler for DatabaseBranch resources.
package operator

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha1 "github.com/MaSuCcHI/branchdb-operator/api/v1alpha1"
	"github.com/MaSuCcHI/branchdb-operator/internal/domain"
)

// DatabaseBranchReconciler reconciles DatabaseBranch resources.
type DatabaseBranchReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	VolumeProvider   domain.VolumeProvider
	DatabaseProvider domain.BranchDatabaseProvider
	ExternalHost     string
}

// requeueInterval is the period after which we recheck TTL expiry.
const requeueInterval = 10 * time.Minute

// Reconcile is the main reconciliation loop for DatabaseBranch.
func (r *DatabaseBranchReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the DatabaseBranch resource.
	branch := &v1alpha1.DatabaseBranch{}
	if err := r.Get(ctx, req.NamespacedName, branch); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Handle deletion.
	if !branch.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, branch)
	}

	// Check TTL expiry before proceeding.
	if branch.Status.ExpiresAt != nil && branch.Status.Phase == v1alpha1.BranchPhaseReady {
		if time.Now().After(branch.Status.ExpiresAt.Time) {
			logger.Info("TTL expired, deleting branch", "name", branch.Name)
			if err := r.Delete(ctx, branch); err != nil {
				return ctrl.Result{}, fmt.Errorf("delete expired branch: %w", err)
			}
			return ctrl.Result{}, nil
		}
	}

	// Add finalizer if not present.
	// Update を呼ぶとキャッシュが追いつくまで局所オブジェクトの ResourceVersion が
	// 古い状態になる可能性があるため、ここで早期 return して次の Reconcile に Status 更新を委ねる。
	if !controllerutil.ContainsFinalizer(branch, v1alpha1.FinalizerName) {
		controllerutil.AddFinalizer(branch, v1alpha1.FinalizerName)
		if err := r.Update(ctx, branch); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Skip if already in terminal / in-progress state.
	if branch.Status.Phase == v1alpha1.BranchPhaseReady {
		return ctrl.Result{RequeueAfter: requeueInterval}, nil
	}

	// --- Creation flow ---

	// Step 1: Mark Creating.
	branch.Status.Phase = v1alpha1.BranchPhaseCreating
	if err := r.Status().Update(ctx, branch); err != nil {
		return ctrl.Result{}, fmt.Errorf("update status to Creating: %w", err)
	}

	// Step 2: Create volume clone.
	volumeInfo, err := r.VolumeProvider.CreateClone(ctx, branch.Spec.SnapshotRef, branch.Name)
	if err != nil {
		return r.setError(ctx, branch, fmt.Errorf("create clone: %w", err))
	}

	// Step 3: Start database. NodePort は Provider が K8s に割り当てさせる。
	dbInfo, err := r.DatabaseProvider.Start(ctx, branch.Name, volumeInfo, branch.Spec.DatabaseType, branch.Spec.DatabaseVersion)
	if err != nil {
		return r.setError(ctx, branch, fmt.Errorf("start database: %w", err))
	}

	// Step 4: Record all connection info and set TTL.
	branch.Status.ClusterHost = dbInfo.Host
	branch.Status.ClusterPort = dbInfo.Port
	branch.Status.ExternalHost = r.ExternalHost
	branch.Status.ExternalPort = dbInfo.ExternalPort

	if branch.Spec.TTLHours > 0 {
		expiresAt := metav1.NewTime(time.Now().Add(time.Duration(branch.Spec.TTLHours) * time.Hour))
		branch.Status.ExpiresAt = &expiresAt
	}

	// Step 5: Mark Ready.
	branch.Status.Phase = v1alpha1.BranchPhaseReady
	branch.Status.Message = ""
	if err := r.Status().Update(ctx, branch); err != nil {
		return ctrl.Result{}, fmt.Errorf("update status to Ready: %w", err)
	}

	return ctrl.Result{RequeueAfter: requeueInterval}, nil
}

// handleDeletion runs the cleanup flow when DeletionTimestamp is set.
func (r *DatabaseBranchReconciler) handleDeletion(ctx context.Context, branch *v1alpha1.DatabaseBranch) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(branch, v1alpha1.FinalizerName) {
		return ctrl.Result{}, nil
	}

	// Mark Deleting.
	branch.Status.Phase = v1alpha1.BranchPhaseDeleting
	if err := r.Status().Update(ctx, branch); err != nil {
		return ctrl.Result{}, fmt.Errorf("update status to Deleting: %w", err)
	}

	// Stop database.
	if err := r.DatabaseProvider.Stop(ctx, branch.Name); err != nil {
		return r.setError(ctx, branch, fmt.Errorf("stop database: %w", err))
	}

	// Destroy volume clone.
	if err := r.VolumeProvider.DeleteClone(ctx, branch.Name); err != nil {
		return r.setError(ctx, branch, fmt.Errorf("delete clone: %w", err))
	}

	// Remove finalizer.
	controllerutil.RemoveFinalizer(branch, v1alpha1.FinalizerName)
	if err := r.Update(ctx, branch); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}

	return ctrl.Result{}, nil
}

// setError records an error in the status and returns the error to trigger a retry.
func (r *DatabaseBranchReconciler) setError(ctx context.Context, branch *v1alpha1.DatabaseBranch, err error) (ctrl.Result, error) {
	branch.Status.Phase = v1alpha1.BranchPhaseError
	branch.Status.Message = err.Error()
	_ = r.Status().Update(ctx, branch)
	return ctrl.Result{}, err
}
