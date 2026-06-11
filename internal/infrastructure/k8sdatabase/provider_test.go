package k8sdatabase_test

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/MaSuCcHI/branchdb-operator/internal/domain"
	"github.com/MaSuCcHI/branchdb-operator/internal/infrastructure/k8sdatabase"
)

// nodePortAssigner は NodePort Service 作成時に NodePort を自動付与するフェイククライアント。
// 本番では kube-apiserver が割り当てるが、fake client は割り当てない。
type nodePortAssigner struct{ client.Client }

func (n *nodePortAssigner) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if svc, ok := obj.(*corev1.Service); ok && svc.Spec.Type == corev1.ServiceTypeNodePort {
		for i := range svc.Spec.Ports {
			if svc.Spec.Ports[i].NodePort == 0 {
				svc.Spec.Ports[i].NodePort = 30100
			}
		}
	}
	return n.Client.Create(ctx, obj, opts...)
}

func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	return s
}

func newProvider(c client.Client) *k8sdatabase.Provider {
	return k8sdatabase.NewProvider(&nodePortAssigner{c}, "branchdb", nil)
}

var testVol = domain.VolumeInfo{NFSServer: "192.168.1.1", NFSPath: "/exports/test"}

func TestStart_MySQL_PV_PVC_ConfigMap_Pod_Serviceが作成される(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newProvider(c)

	if _, err := p.Start(ctx, "test-branch", testVol, "mysql", "", nil); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	var pv corev1.PersistentVolume
	if err := c.Get(ctx, types.NamespacedName{Name: "branchdb-pv-test-branch"}, &pv); err != nil {
		t.Errorf("PV が作成されていない: %v", err)
	}
	var pvc corev1.PersistentVolumeClaim
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "branchdb-pvc-test-branch"}, &pvc); err != nil {
		t.Errorf("PVC が作成されていない: %v", err)
	}
	var cm corev1.ConfigMap
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "branchdb-cfg-test-branch"}, &cm); err != nil {
		t.Errorf("ConfigMap が作成されていない: %v", err)
	}
	var pod corev1.Pod
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "branchdb-db-test-branch"}, &pod); err != nil {
		t.Errorf("Pod が作成されていない: %v", err)
	}
	var svc corev1.Service
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "branchdb-svc-test-branch"}, &svc); err != nil {
		t.Errorf("Service が作成されていない: %v", err)
	}
}

func TestStart_Postgres_ConfigMapなしでPodが作成される(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newProvider(c)

	if _, err := p.Start(ctx, "pg-branch", testVol, "postgres", "", nil); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	var pod corev1.Pod
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "branchdb-db-pg-branch"}, &pod); err != nil {
		t.Fatalf("Pod が作成されていない: %v", err)
	}
	if pod.Spec.Containers[0].Image != "postgres:16" {
		t.Errorf("image = %q, want postgres:16", pod.Spec.Containers[0].Image)
	}

	var cm corev1.ConfigMap
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "branchdb-cfg-pg-branch"}, &cm); err == nil {
		t.Error("PostgreSQL では ConfigMap は作成されないはず")
	}
}

func TestStart_Redis_initContainerなしでPodが作成される(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newProvider(c)

	if _, err := p.Start(ctx, "redis-branch", testVol, "redis", "", nil); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	var pod corev1.Pod
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "branchdb-db-redis-branch"}, &pod); err != nil {
		t.Fatalf("Pod が作成されていない: %v", err)
	}
	if pod.Spec.Containers[0].Image != "redis:7" {
		t.Errorf("image = %q, want redis:7", pod.Spec.Containers[0].Image)
	}
	if len(pod.Spec.InitContainers) != 0 {
		t.Errorf("Redis では InitContainer は不要 (got %d)", len(pod.Spec.InitContainers))
	}
}

func TestStart_dbVersionが指定されたときイメージタグが上書きされる(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newProvider(c)

	if _, err := p.Start(ctx, "ver-branch", testVol, "mysql", "8.4", nil); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	var pod corev1.Pod
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "branchdb-db-ver-branch"}, &pod); err != nil {
		t.Fatalf("Pod が作成されていない: %v", err)
	}
	if pod.Spec.Containers[0].Image != "mysql:8.4" {
		t.Errorf("image = %q, want mysql:8.4", pod.Spec.Containers[0].Image)
	}
}

func TestStart_imageOverridesが指定されたときイメージが上書きされる(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := k8sdatabase.NewProvider(&nodePortAssigner{c}, "branchdb", map[string]string{"mysql": "mysql:8.4"})

	if _, err := p.Start(ctx, "override-branch", testVol, "mysql", "", nil); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	var pod corev1.Pod
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "branchdb-db-override-branch"}, &pod); err != nil {
		t.Fatalf("Pod が作成されていない: %v", err)
	}
	if pod.Spec.Containers[0].Image != "mysql:8.4" {
		t.Errorf("image = %q, want mysql:8.4", pod.Spec.Containers[0].Image)
	}
}

func TestStart_サポート外のdbTypeはエラーを返す(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newProvider(c)

	if _, err := p.Start(ctx, "x", testVol, "oracle", "", nil); err == nil {
		t.Error("unsupported db type should return error")
	}
}

func TestStart_dbTypeが空のときmysqlとして動作する(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newProvider(c)

	ep, err := p.Start(ctx, "default-branch", testVol, "", "", nil)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if ep.Port != 3306 {
		t.Errorf("Port = %d, want 3306 (mysql default)", ep.Port)
	}
}

func TestStart_PodのラベルがServiceセレクタと一致する(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newProvider(c)

	if _, err := p.Start(ctx, "label-branch", testVol, "mysql", "", nil); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	var pod corev1.Pod
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "branchdb-db-label-branch"}, &pod); err != nil {
		t.Fatalf("Pod が作成されていない: %v", err)
	}
	var svc corev1.Service
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "branchdb-svc-label-branch"}, &svc); err != nil {
		t.Fatalf("Service が作成されていない: %v", err)
	}
	for k, v := range svc.Spec.Selector {
		if pod.Labels[k] != v {
			t.Errorf("Pod label %q=%q does not match Service selector", k, pod.Labels[k])
		}
	}
}

func TestStart_BranchEndpointのHostとPortが正しく返る(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newProvider(c)

	ep, err := p.Start(ctx, "my-branch", testVol, "mysql", "", nil)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if ep.Host != "branchdb-svc-my-branch.branchdb.svc.cluster.local" {
		t.Errorf("Host = %q", ep.Host)
	}
	if ep.Port != 3306 {
		t.Errorf("Port = %d, want 3306", ep.Port)
	}
	if ep.ExternalPort == 0 {
		t.Error("ExternalPort should be non-zero")
	}
}

func TestStart_PostgresのBranchEndpointPortは5432(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newProvider(c)

	ep, err := p.Start(ctx, "pg-port-branch", testVol, "postgres", "", nil)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if ep.Port != 5432 {
		t.Errorf("Port = %d, want 5432", ep.Port)
	}
}

func TestStart_RedisのBranchEndpointPortは6379(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newProvider(c)

	ep, err := p.Start(ctx, "redis-port-branch", testVol, "redis", "", nil)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if ep.Port != 6379 {
		t.Errorf("Port = %d, want 6379", ep.Port)
	}
}

func TestStart_ServiceがNodePortタイプで作成される(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newProvider(c)

	if _, err := p.Start(ctx, "np-branch", testVol, "mysql", "", nil); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	var svc corev1.Service
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "branchdb-svc-np-branch"}, &svc); err != nil {
		t.Fatalf("Service が作成されていない: %v", err)
	}
	if svc.Spec.Type != corev1.ServiceTypeNodePort {
		t.Errorf("Service.Type = %q, want NodePort", svc.Spec.Type)
	}
	if len(svc.Spec.Ports) == 0 || svc.Spec.Ports[0].NodePort == 0 {
		t.Error("NodePort が割り当てられていない")
	}
}

func TestStop_PV_PVC_ConfigMap_Pod_Serviceが削除される(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newProvider(c)

	if _, err := p.Start(ctx, "stop-branch", testVol, "mysql", "", nil); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if err := p.Stop(ctx, "stop-branch"); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	var pv corev1.PersistentVolume
	if err := c.Get(ctx, types.NamespacedName{Name: "branchdb-pv-stop-branch"}, &pv); err == nil {
		t.Error("PV が削除されていない")
	}
	var pvc corev1.PersistentVolumeClaim
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "branchdb-pvc-stop-branch"}, &pvc); err == nil {
		t.Error("PVC が削除されていない")
	}
	var cm corev1.ConfigMap
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "branchdb-cfg-stop-branch"}, &cm); err == nil {
		t.Error("ConfigMap が削除されていない")
	}
	var pod corev1.Pod
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "branchdb-db-stop-branch"}, &pod); err == nil {
		t.Error("Pod が削除されていない")
	}
}

func TestStart_同名のリソースが既に存在しても成功する(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newProvider(c)

	if _, err := p.Start(ctx, "idempotent-branch", testVol, "mysql", "", nil); err != nil {
		t.Fatalf("1回目の Start returned error: %v", err)
	}
	if _, err := p.Start(ctx, "idempotent-branch", testVol, "mysql", "", nil); err != nil {
		t.Fatalf("2回目の Start returned error: %v", err)
	}
}

// nthErrClient は N 回目の Create 呼び出しで指定エラーを返すフェイククライアント（1-indexed）。
type nthErrClient struct {
	client.Client
	failAt, callNum int
	err             error
}

func (e *nthErrClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	e.callNum++
	if e.callNum == e.failAt {
		return e.err
	}
	return e.Client.Create(ctx, obj, opts...)
}

func TestStart_PV作成に失敗したときエラーを返す(t *testing.T) {
	base := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	c := &nthErrClient{Client: &nodePortAssigner{base}, failAt: 1, err: errors.New("API server unreachable")}
	p := k8sdatabase.NewProvider(c, "branchdb", nil)
	if _, err := p.Start(context.Background(), "fail-branch", testVol, "mysql", "", nil); err == nil {
		t.Error("Start はエラーを返すべき")
	}
}

func TestStart_PVC作成に失敗したときエラーを返す(t *testing.T) {
	base := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	c := &nthErrClient{Client: &nodePortAssigner{base}, failAt: 2, err: errors.New("PVC creation failed")}
	p := k8sdatabase.NewProvider(c, "branchdb", nil)
	if _, err := p.Start(context.Background(), "fail-pvc-branch", testVol, "mysql", "", nil); err == nil {
		t.Error("Start はエラーを返すべき")
	}
}

func TestStart_ConfigMap作成に失敗したときエラーを返す(t *testing.T) {
	base := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	// mysql は ConfigMap を作成するので failAt=3
	c := &nthErrClient{Client: &nodePortAssigner{base}, failAt: 3, err: errors.New("ConfigMap creation failed")}
	p := k8sdatabase.NewProvider(c, "branchdb", nil)
	if _, err := p.Start(context.Background(), "fail-cm-branch", testVol, "mysql", "", nil); err == nil {
		t.Error("Start はエラーを返すべき")
	}
}

func TestStart_Pod作成に失敗したときエラーを返す(t *testing.T) {
	base := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	// mysql: PV(1) PVC(2) CM(3) Pod(4)
	c := &nthErrClient{Client: &nodePortAssigner{base}, failAt: 4, err: errors.New("Pod creation failed")}
	p := k8sdatabase.NewProvider(c, "branchdb", nil)
	if _, err := p.Start(context.Background(), "fail-pod-branch", testVol, "mysql", "", nil); err == nil {
		t.Error("Start はエラーを返すべき")
	}
}

func TestStart_NodePortが割り当てられていないときエラーを返す(t *testing.T) {
	// nodePortAssigner なし → NodePort=0 のまま
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := k8sdatabase.NewProvider(c, "branchdb", nil)
	if _, err := p.Start(context.Background(), "no-nodeport-branch", testVol, "mysql", "", nil); err == nil {
		t.Error("NodePort が割り当てられていない場合 Start はエラーを返すべき")
	}
}

func TestStart_PVのNFSマウントオプションが全て設定される(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newProvider(c)

	if _, err := p.Start(ctx, "mount-branch", testVol, "mysql", "", nil); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	var pv corev1.PersistentVolume
	if err := c.Get(ctx, types.NamespacedName{Name: "branchdb-pv-mount-branch"}, &pv); err != nil {
		t.Fatalf("PV が作成されていない: %v", err)
	}
	wantOptions := []string{
		"hard",
		"proto=tcp",
		"nfsvers=4.1",
		"rsize=1048576",
		"wsize=1048576",
		"timeo=600",
		"retrans=2",
	}
	optSet := make(map[string]bool, len(pv.Spec.MountOptions))
	for _, o := range pv.Spec.MountOptions {
		optSet[o] = true
	}
	for _, want := range wantOptions {
		if !optSet[want] {
			t.Errorf("PV.MountOptions に %q が含まれていない (got %v)", want, pv.Spec.MountOptions)
		}
	}
}

func TestStart_MySQL_PodにinitContainerが設定される(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newProvider(c)

	if _, err := p.Start(ctx, "init-branch", testVol, "mysql", "", nil); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	var pod corev1.Pod
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "branchdb-db-init-branch"}, &pod); err != nil {
		t.Fatalf("Pod が作成されていない: %v", err)
	}
	if len(pod.Spec.InitContainers) == 0 {
		t.Fatal("MySQL Pod に InitContainer が設定されていない")
	}
	initC := pod.Spec.InitContainers[0]
	if initC.Name != "fix-permissions" {
		t.Errorf("InitContainer.Name = %q, want fix-permissions", initC.Name)
	}
	if initC.SecurityContext == nil || initC.SecurityContext.RunAsUser == nil || *initC.SecurityContext.RunAsUser != 0 {
		t.Error("fix-permissions InitContainer は root (UID 0) で実行される必要がある")
	}
}

func TestStart_OwnerReferenceがPodに設定される(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newProvider(c)

	owner := &domain.OwnerRef{
		Name:       "feature-x",
		UID:        "test-uid-1234",
		APIVersion: "branchdb.io/v1alpha1",
		Kind:       "DatabaseBranch",
	}
	if _, err := p.Start(ctx, "owner-branch", testVol, "mysql", "", owner); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	var pod corev1.Pod
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "branchdb-db-owner-branch"}, &pod); err != nil {
		t.Fatalf("Pod が作成されていない: %v", err)
	}
	if len(pod.OwnerReferences) == 0 {
		t.Fatal("Pod に OwnerReference が設定されていない")
	}
	or := pod.OwnerReferences[0]
	if or.UID != "test-uid-1234" {
		t.Errorf("OwnerReference.UID = %q, want test-uid-1234", or.UID)
	}
	if or.Kind != "DatabaseBranch" {
		t.Errorf("OwnerReference.Kind = %q, want DatabaseBranch", or.Kind)
	}
}

func TestStop_NotFound以外のエラーを集約して返す(t *testing.T) {
	ctx := context.Background()
	base := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	deleteErr := errors.New("delete failed")

	// 全ての Delete 呼び出しで NotFound 以外のエラーを返す
	type deleteErrClient struct {
		client.Client
		err error
	}
	errClient := &struct {
		client.Client
		err error
	}{Client: &nodePortAssigner{base}, err: deleteErr}

	type wrappedClient struct {
		client.Client
		err error
	}
	_ = errClient

	// nthErrClient を流用: 全コールでエラーを返す failAt=-1 のように毎回エラー
	allErrClient := &allDeleteErrClient{Client: &nodePortAssigner{base}, err: deleteErr}
	p := k8sdatabase.NewProvider(allErrClient, "branchdb", nil)

	err := p.Stop(ctx, "err-branch")
	if err == nil {
		t.Error("Stop は NotFound 以外のエラーを返すべき")
	}
}

// allDeleteErrClient は全ての Delete 呼び出しでエラーを返す。
type allDeleteErrClient struct {
	client.Client
	err error
}

func (a *allDeleteErrClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	return a.err
}

func TestStart_ServiceにbranchdbプレフィックスがつくことでCRと衝突しない(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newProvider(c)

	if _, err := p.Start(ctx, "prefix-branch", testVol, "mysql", "", nil); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	var svc corev1.Service
	// "branchdb-svc-prefix-branch" という名前で作られているはず
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "branchdb-svc-prefix-branch"}, &svc); err != nil {
		t.Errorf("Service が branchdb-svc-prefix-branch という名前で作成されていない: %v", err)
	}
}

func TestStart_BranchEndpointHostにServiceプレフィックスが反映される(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newProvider(c)

	ep, err := p.Start(ctx, "svc-branch", testVol, "mysql", "", nil)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	// Host は branchdb-svc-<branch>.namespace.svc.cluster.local になるはず
	wantHost := "branchdb-svc-svc-branch.branchdb.svc.cluster.local"
	if ep.Host != wantHost {
		t.Errorf("Host = %q, want %q", ep.Host, wantHost)
	}
}

func TestStart_PVCのvolumeNameにPV名が設定される(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newProvider(c)

	if _, err := p.Start(ctx, "bind-branch", testVol, "mysql", "", nil); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	var pvc corev1.PersistentVolumeClaim
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "branchdb-pvc-bind-branch"}, &pvc); err != nil {
		t.Fatalf("PVC が作成されていない: %v", err)
	}
	wantVolumeName := "branchdb-pv-bind-branch"
	if pvc.Spec.VolumeName != wantVolumeName {
		t.Errorf("PVC.Spec.VolumeName = %q, want %q", pvc.Spec.VolumeName, wantVolumeName)
	}
}

func TestStart_ConfigMapにinnodb_flush設定が含まれる(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newProvider(c)

	if _, err := p.Start(ctx, "cfg-branch", testVol, "mysql", "", nil); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	var cm corev1.ConfigMap
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "branchdb-cfg-cfg-branch"}, &cm); err != nil {
		t.Fatalf("ConfigMap が作成されていない: %v", err)
	}
	val, ok := cm.Data["branchdb.cnf"]
	if !ok {
		t.Fatal("ConfigMap.Data[branchdb.cnf] が存在しない")
	}
	for i := 0; i <= len(val)-len("innodb_flush_log_at_trx_commit"); i++ {
		if val[i:i+len("innodb_flush_log_at_trx_commit")] == "innodb_flush_log_at_trx_commit" {
			return
		}
	}
	t.Errorf("ConfigMap に innodb_flush_log_at_trx_commit が含まれていない: %q", val)
}
