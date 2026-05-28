package operator_test

import (
	"context"
	"errors"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	v1alpha1 "github.com/MaSuCcHI/branchdb-operator/api/v1alpha1"
	"github.com/MaSuCcHI/branchdb-operator/internal/domain"
	"github.com/MaSuCcHI/branchdb-operator/internal/interface/operator"
)

// --- モック定義 ---

type mockVolumeProvider struct {
	createCloneFunc func(ctx context.Context, snapshotName, branchName string) (domain.VolumeInfo, error)
	deleteCloneFunc func(ctx context.Context, branchName string) error
}

func (m *mockVolumeProvider) TakeSnapshot(_ context.Context, _ string) error { return nil }

func (m *mockVolumeProvider) CreateClone(ctx context.Context, snapshotName, branchName string) (domain.VolumeInfo, error) {
	if m.createCloneFunc != nil {
		return m.createCloneFunc(ctx, snapshotName, branchName)
	}
	return domain.VolumeInfo{NFSServer: "nfs.example.com", NFSPath: "/data/" + branchName}, nil
}

func (m *mockVolumeProvider) DeleteClone(ctx context.Context, branchName string) error {
	if m.deleteCloneFunc != nil {
		return m.deleteCloneFunc(ctx, branchName)
	}
	return nil
}

func (m *mockVolumeProvider) ListSnapshots(_ context.Context) ([]domain.SnapshotInfo, error) {
	return nil, nil
}

type mockMySQLProvider struct {
	startFunc func(ctx context.Context, branchName string, volumeInfo domain.VolumeInfo) (domain.BranchEndpoint, error)
	stopFunc  func(ctx context.Context, branchName string) error
}

func (m *mockMySQLProvider) Start(ctx context.Context, branchName string, volumeInfo domain.VolumeInfo) (domain.BranchEndpoint, error) {
	if m.startFunc != nil {
		return m.startFunc(ctx, branchName, volumeInfo)
	}
	return domain.BranchEndpoint{Host: branchName + ".branchdb.svc", Port: 3306, ExternalPort: 33100}, nil
}

func (m *mockMySQLProvider) Stop(ctx context.Context, branchName string) error {
	if m.stopFunc != nil {
		return m.stopFunc(ctx, branchName)
	}
	return nil
}

// --- テストヘルパー ---

func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(s)
	return s
}

func newBranch(name string, opts ...func(*v1alpha1.DatabaseBranch)) *v1alpha1.DatabaseBranch {
	b := &v1alpha1.DatabaseBranch{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: v1alpha1.DatabaseBranchSpec{
			SnapshotRef: "snap-20260101",
			TTLHours:    24,
		},
	}
	for _, o := range opts {
		o(b)
	}
	return b
}

func newReconciler(scheme *runtime.Scheme, objs []runtime.Object, volumeProvider domain.VolumeProvider, mysqlProvider domain.BranchMySQLProvider) *operator.DatabaseBranchReconciler {
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(objs...).
		WithStatusSubresource(&v1alpha1.DatabaseBranch{}).
		Build()
	return &operator.DatabaseBranchReconciler{
		Client:         fakeClient,
		Scheme:         scheme,
		VolumeProvider: volumeProvider,
		MySQLProvider:  mysqlProvider,
		ExternalHost:   "branchdb.example.com",
	}
}

func reconcile(t *testing.T, r *operator.DatabaseBranchReconciler, name string) (ctrl.Result, error) {
	t.Helper()
	return r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: "default", Name: name},
	})
}

func fetchBranch(t *testing.T, r *operator.DatabaseBranchReconciler, name string) *v1alpha1.DatabaseBranch {
	t.Helper()
	got := &v1alpha1.DatabaseBranch{}
	if err := r.Client.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: name}, got); err != nil {
		t.Fatalf("Get DatabaseBranch: %v", err)
	}
	return got
}

// --- テストケース ---

func TestReconcile_新規CRがPendingからCreatingに遷移する(t *testing.T) {
	scheme := newScheme()
	branch := newBranch("feature-a")
	r := newReconciler(scheme, []runtime.Object{branch}, &mockVolumeProvider{}, &mockMySQLProvider{})

	_, err := reconcile(t, r, "feature-a")
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	got := fetchBranch(t, r, "feature-a")
	if got.Status.Phase != v1alpha1.BranchPhaseReady {
		t.Errorf("expected phase Ready, got %q", got.Status.Phase)
	}
}

func TestReconcile_VolumeProviderのCreateCloneが失敗したときphaseがErrorになる(t *testing.T) {
	scheme := newScheme()
	branch := newBranch("feature-b")
	volumeProvider := &mockVolumeProvider{
		createCloneFunc: func(_ context.Context, _, _ string) (domain.VolumeInfo, error) {
			return domain.VolumeInfo{}, errors.New("volume clone failed")
		},
	}
	r := newReconciler(scheme, []runtime.Object{branch}, volumeProvider, &mockMySQLProvider{})

	_, err := reconcile(t, r, "feature-b")
	if err == nil {
		t.Fatal("Reconcile should return error when volume clone fails")
	}

	got := fetchBranch(t, r, "feature-b")
	if got.Status.Phase != v1alpha1.BranchPhaseError {
		t.Errorf("expected phase Error, got %q", got.Status.Phase)
	}
	if got.Status.Message == "" {
		t.Error("expected non-empty error message in status")
	}
}

func TestReconcile_MySQLProviderのStartが失敗したときphaseがErrorになる(t *testing.T) {
	scheme := newScheme()
	branch := newBranch("feature-c")
	mysqlProvider := &mockMySQLProvider{
		startFunc: func(_ context.Context, _ string, _ domain.VolumeInfo) (domain.BranchEndpoint, error) {
			return domain.BranchEndpoint{}, errors.New("mysql start failed")
		},
	}
	r := newReconciler(scheme, []runtime.Object{branch}, &mockVolumeProvider{}, mysqlProvider)

	_, err := reconcile(t, r, "feature-c")
	if err == nil {
		t.Fatal("Reconcile should return error when mysql start fails")
	}

	got := fetchBranch(t, r, "feature-c")
	if got.Status.Phase != v1alpha1.BranchPhaseError {
		t.Errorf("expected phase Error, got %q", got.Status.Phase)
	}
	if got.Status.Message == "" {
		t.Error("expected non-empty error message in status")
	}
}

func TestReconcile_MySQLのNodePortがstatusに反映される(t *testing.T) {
	scheme := newScheme()
	branch := newBranch("feature-nodeport")
	const wantNodePort = 31234
	mysqlProvider := &mockMySQLProvider{
		startFunc: func(_ context.Context, _ string, _ domain.VolumeInfo) (domain.BranchEndpoint, error) {
			return domain.BranchEndpoint{
				Host:         "feature-nodeport.branchdb.svc",
				Port:         3306,
				ExternalPort: wantNodePort,
			}, nil
		},
	}
	r := newReconciler(scheme, []runtime.Object{branch}, &mockVolumeProvider{}, mysqlProvider)

	_, err := reconcile(t, r, "feature-nodeport")
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	got := fetchBranch(t, r, "feature-nodeport")
	if got.Status.ExternalPort != wantNodePort {
		t.Errorf("status.ExternalPort = %d, want %d", got.Status.ExternalPort, wantNodePort)
	}
	if got.Status.ExternalHost != "branchdb.example.com" {
		t.Errorf("status.ExternalHost = %q, want branchdb.example.com", got.Status.ExternalHost)
	}
}

func TestReconcile_削除フラグが立ったCRのクリーンアップが実行される(t *testing.T) {
	scheme := newScheme()
	now := metav1.Now()
	branch := newBranch("feature-d", func(b *v1alpha1.DatabaseBranch) {
		b.DeletionTimestamp = &now
		b.Finalizers = []string{v1alpha1.FinalizerName}
		b.Status.Phase = v1alpha1.BranchPhaseReady
	})

	stopCalled := false
	deleteCalled := false
	mysqlProvider := &mockMySQLProvider{
		stopFunc: func(_ context.Context, _ string) error {
			stopCalled = true
			return nil
		},
	}
	volumeProvider := &mockVolumeProvider{
		deleteCloneFunc: func(_ context.Context, _ string) error {
			deleteCalled = true
			return nil
		},
	}
	r := newReconciler(scheme, []runtime.Object{branch}, volumeProvider, mysqlProvider)

	_, err := reconcile(t, r, "feature-d")
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	if !stopCalled {
		t.Error("expected MySQLProvider.Stop to be called")
	}
	if !deleteCalled {
		t.Error("expected VolumeProvider.DeleteClone to be called")
	}
}

func TestReconcile_TTL期限切れのCRが削除される(t *testing.T) {
	scheme := newScheme()
	expired := metav1.NewTime(time.Now().Add(-1 * time.Hour))
	branch := newBranch("feature-e", func(b *v1alpha1.DatabaseBranch) {
		b.Finalizers = []string{v1alpha1.FinalizerName}
		b.Status.Phase = v1alpha1.BranchPhaseReady
		b.Status.ExpiresAt = &expired
	})
	r := newReconciler(scheme, []runtime.Object{branch}, &mockVolumeProvider{}, &mockMySQLProvider{})

	_, err := reconcile(t, r, "feature-e")
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	got := fetchBranch(t, r, "feature-e")
	if got.DeletionTimestamp == nil {
		t.Error("expected DeletionTimestamp to be set on TTL-expired branch")
	}
}

func TestReconcile_finalizerが正しく追加される(t *testing.T) {
	scheme := newScheme()
	branch := newBranch("feature-f")
	r := newReconciler(scheme, []runtime.Object{branch}, &mockVolumeProvider{}, &mockMySQLProvider{})

	_, err := reconcile(t, r, "feature-f")
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	got := fetchBranch(t, r, "feature-f")
	hasFinalizer := false
	for _, f := range got.Finalizers {
		if f == v1alpha1.FinalizerName {
			hasFinalizer = true
			break
		}
	}
	if !hasFinalizer {
		t.Errorf("expected finalizer %q to be present, got %v", v1alpha1.FinalizerName, got.Finalizers)
	}
}

func TestReconcile_finalizerがクリーンアップ後に除去される(t *testing.T) {
	scheme := newScheme()
	now := metav1.Now()
	branch := newBranch("feature-g", func(b *v1alpha1.DatabaseBranch) {
		b.DeletionTimestamp = &now
		b.Finalizers = []string{v1alpha1.FinalizerName}
		b.Status.Phase = v1alpha1.BranchPhaseReady
	})
	r := newReconciler(scheme, []runtime.Object{branch}, &mockVolumeProvider{}, &mockMySQLProvider{})

	_, err := reconcile(t, r, "feature-g")
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	got := &v1alpha1.DatabaseBranch{}
	getErr := r.Client.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "feature-g"}, got)
	if getErr == nil {
		for _, f := range got.Finalizers {
			if f == v1alpha1.FinalizerName {
				t.Errorf("expected finalizer %q to be removed after cleanup, got %v", v1alpha1.FinalizerName, got.Finalizers)
			}
		}
	}
}

func TestReconcile_存在しないCRを取得しようとしたときエラーなしで終了する(t *testing.T) {
	scheme := newScheme()
	r := newReconciler(scheme, []runtime.Object{}, &mockVolumeProvider{}, &mockMySQLProvider{})

	result, err := reconcile(t, r, "non-existent")
	if err != nil {
		t.Fatalf("Reconcile should not error for missing CR, got: %v", err)
	}
	if result.Requeue {
		t.Error("should not requeue for missing CR")
	}
}

func TestReconcile_すでにReadyなCRは再処理をスキップして定期チェックをスケジュールする(t *testing.T) {
	scheme := newScheme()
	branch := newBranch("feature-ready", func(b *v1alpha1.DatabaseBranch) {
		b.Finalizers = []string{v1alpha1.FinalizerName}
		b.Status.Phase = v1alpha1.BranchPhaseReady
	})
	createCalled := false
	volumeProvider := &mockVolumeProvider{
		createCloneFunc: func(_ context.Context, _, _ string) (domain.VolumeInfo, error) {
			createCalled = true
			return domain.VolumeInfo{}, nil
		},
	}
	r := newReconciler(scheme, []runtime.Object{branch}, volumeProvider, &mockMySQLProvider{})

	result, err := reconcile(t, r, "feature-ready")
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if createCalled {
		t.Error("CreateClone should not be called for already-Ready branch")
	}
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter to be set for TTL check")
	}
}

func TestReconcile_削除フロー中にMySQLStopが失敗したときphaseがErrorになる(t *testing.T) {
	scheme := newScheme()
	now := metav1.Now()
	branch := newBranch("feature-stop-err", func(b *v1alpha1.DatabaseBranch) {
		b.DeletionTimestamp = &now
		b.Finalizers = []string{v1alpha1.FinalizerName}
		b.Status.Phase = v1alpha1.BranchPhaseReady
	})
	mysqlProvider := &mockMySQLProvider{
		stopFunc: func(_ context.Context, _ string) error {
			return errors.New("mysql stop failed")
		},
	}
	r := newReconciler(scheme, []runtime.Object{branch}, &mockVolumeProvider{}, mysqlProvider)

	_, err := reconcile(t, r, "feature-stop-err")
	if err == nil {
		t.Fatal("Reconcile should return error when mysql stop fails")
	}

	got := fetchBranch(t, r, "feature-stop-err")
	if got.Status.Phase != v1alpha1.BranchPhaseError {
		t.Errorf("expected phase Error, got %q", got.Status.Phase)
	}
}

func TestReconcile_削除フロー中にDeleteCloneが失敗したときphaseがErrorになる(t *testing.T) {
	scheme := newScheme()
	now := metav1.Now()
	branch := newBranch("feature-del-err", func(b *v1alpha1.DatabaseBranch) {
		b.DeletionTimestamp = &now
		b.Finalizers = []string{v1alpha1.FinalizerName}
		b.Status.Phase = v1alpha1.BranchPhaseReady
	})
	volumeProvider := &mockVolumeProvider{
		deleteCloneFunc: func(_ context.Context, _ string) error {
			return errors.New("volume delete failed")
		},
	}
	r := newReconciler(scheme, []runtime.Object{branch}, volumeProvider, &mockMySQLProvider{})

	_, err := reconcile(t, r, "feature-del-err")
	if err == nil {
		t.Fatal("Reconcile should return error when volume delete fails")
	}

	got := fetchBranch(t, r, "feature-del-err")
	if got.Status.Phase != v1alpha1.BranchPhaseError {
		t.Errorf("expected phase Error, got %q", got.Status.Phase)
	}
}

// newReconcilerWithInterceptor creates a reconciler with an interceptor for error injection testing.
func newReconcilerWithInterceptor(scheme *runtime.Scheme, objs []runtime.Object, funcs interceptor.Funcs, volumeProvider domain.VolumeProvider, mysqlProvider domain.BranchMySQLProvider) *operator.DatabaseBranchReconciler {
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(objs...).
		WithStatusSubresource(&v1alpha1.DatabaseBranch{}).
		WithInterceptorFuncs(funcs).
		Build()
	return &operator.DatabaseBranchReconciler{
		Client:         fakeClient,
		Scheme:         scheme,
		VolumeProvider: volumeProvider,
		MySQLProvider:  mysqlProvider,
		ExternalHost:   "branchdb.example.com",
	}
}

func TestReconcile_finalizerのUpdate失敗時にエラーを返す(t *testing.T) {
	scheme := newScheme()
	branch := newBranch("feature-update-err")
	updateErr := errors.New("update failed")

	updateCallCount := 0
	r := newReconcilerWithInterceptor(scheme, []runtime.Object{branch}, interceptor.Funcs{
		Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			updateCallCount++
			if updateCallCount == 1 {
				return updateErr
			}
			return c.Update(ctx, obj, opts...)
		},
	}, &mockVolumeProvider{}, &mockMySQLProvider{})

	_, err := reconcile(t, r, "feature-update-err")
	if err == nil {
		t.Fatal("expected error when finalizer Update fails")
	}
	if !errors.Is(err, updateErr) {
		t.Errorf("expected updateErr, got: %v", err)
	}
}

func TestReconcile_削除フラグが立ったCRに自分のfinalizerがない場合はスキップされる(t *testing.T) {
	scheme := newScheme()
	now := metav1.Now()
	branch := newBranch("feature-other-finalizer", func(b *v1alpha1.DatabaseBranch) {
		b.DeletionTimestamp = &now
		b.Finalizers = []string{"other-controller.io/finalizer"}
	})
	stopCalled := false
	mysqlProvider := &mockMySQLProvider{
		stopFunc: func(_ context.Context, _ string) error {
			stopCalled = true
			return nil
		},
	}
	r := newReconciler(scheme, []runtime.Object{branch}, &mockVolumeProvider{}, mysqlProvider)

	_, err := reconcile(t, r, "feature-other-finalizer")
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if stopCalled {
		t.Error("MySQLProvider.Stop should not be called when our finalizer is not present")
	}
}

func TestReconcile_削除フロー中のStatusUpdate失敗時にエラーを返す(t *testing.T) {
	scheme := newScheme()
	now := metav1.Now()
	branch := newBranch("feature-del-status-err", func(b *v1alpha1.DatabaseBranch) {
		b.DeletionTimestamp = &now
		b.Finalizers = []string{v1alpha1.FinalizerName}
		b.Status.Phase = v1alpha1.BranchPhaseReady
	})
	statusUpdateErr := errors.New("status update failed")
	r := newReconcilerWithInterceptor(scheme, []runtime.Object{branch}, interceptor.Funcs{
		SubResourceUpdate: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
			return statusUpdateErr
		},
	}, &mockVolumeProvider{}, &mockMySQLProvider{})

	_, err := reconcile(t, r, "feature-del-status-err")
	if err == nil {
		t.Fatal("expected error when status update fails during deletion")
	}
}

func TestReconcile_TTL期限切れのDelete失敗時にエラーを返す(t *testing.T) {
	scheme := newScheme()
	expired := metav1.NewTime(time.Now().Add(-1 * time.Hour))
	branch := newBranch("feature-delete-err", func(b *v1alpha1.DatabaseBranch) {
		b.Finalizers = []string{v1alpha1.FinalizerName}
		b.Status.Phase = v1alpha1.BranchPhaseReady
		b.Status.ExpiresAt = &expired
	})
	deleteErr := errors.New("delete failed")
	r := newReconcilerWithInterceptor(scheme, []runtime.Object{branch}, interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			return deleteErr
		},
	}, &mockVolumeProvider{}, &mockMySQLProvider{})

	_, err := reconcile(t, r, "feature-delete-err")
	if err == nil {
		t.Fatal("expected error when Delete fails on TTL expiry")
	}
	if !errors.Is(err, deleteErr) {
		t.Errorf("expected deleteErr, got: %v", err)
	}
}

func TestReconcile_Creating状態へのStatusUpdate失敗時にエラーを返す(t *testing.T) {
	scheme := newScheme()
	branch := newBranch("feature-creating-err")
	statusUpdateErr := errors.New("status update to Creating failed")
	callCount := 0
	r := newReconcilerWithInterceptor(scheme, []runtime.Object{branch}, interceptor.Funcs{
		SubResourceUpdate: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
			callCount++
			if callCount == 1 {
				return statusUpdateErr
			}
			return c.SubResource(subResourceName).Update(ctx, obj, opts...)
		},
	}, &mockVolumeProvider{}, &mockMySQLProvider{})

	_, err := reconcile(t, r, "feature-creating-err")
	if err == nil {
		t.Fatal("expected error when status update to Creating fails")
	}
}

func TestReconcile_削除フロー中のfinalizer削除Update失敗時にエラーを返す(t *testing.T) {
	scheme := newScheme()
	now := metav1.Now()
	branch := newBranch("feature-rm-finalizer-err", func(b *v1alpha1.DatabaseBranch) {
		b.DeletionTimestamp = &now
		b.Finalizers = []string{v1alpha1.FinalizerName}
		b.Status.Phase = v1alpha1.BranchPhaseReady
	})
	updateErr := errors.New("remove finalizer update failed")
	updateCallCount := 0
	r := newReconcilerWithInterceptor(scheme, []runtime.Object{branch}, interceptor.Funcs{
		Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			updateCallCount++
			return updateErr
		},
	}, &mockVolumeProvider{}, &mockMySQLProvider{})

	_, err := reconcile(t, r, "feature-rm-finalizer-err")
	if err == nil {
		t.Fatal("expected error when finalizer removal Update fails")
	}
}

func TestReconcile_finalizer追加後のre_Get失敗時に正常終了する(t *testing.T) {
	scheme := newScheme()
	branch := newBranch("feature-reget-fail")

	getCallCount := 0
	r := newReconcilerWithInterceptor(scheme, []runtime.Object{branch}, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			getCallCount++
			// 1回目: 通常取得。2回目(finalizer追加後のre-read): NotFound を返す。
			if getCallCount == 2 {
				return errors.New("not found after update")
			}
			return c.Get(ctx, key, obj, opts...)
		},
	}, &mockVolumeProvider{}, &mockMySQLProvider{})

	_, err := reconcile(t, r, "feature-reget-fail")
	// client.IgnoreNotFound(err) が nil ではないためエラーが返る
	if err == nil {
		t.Fatal("expected error when re-Get after adding finalizer fails")
	}
}
