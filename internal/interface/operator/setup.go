package operator

import (
	ctrl "sigs.k8s.io/controller-runtime"

	v1alpha1 "github.com/keisuke/zfs-db-k8s/api/v1alpha1"
)

// SetupWithManager registers the reconciler with the controller-runtime Manager.
// This requires a running Kubernetes manager (real kubeconfig) and is not unit-testable.
func (r *DatabaseBranchReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.DatabaseBranch{}).
		Complete(r)
}
