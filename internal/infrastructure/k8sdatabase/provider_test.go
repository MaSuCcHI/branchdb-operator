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

	if _, err := p.Start(ctx, "test-branch", testVol, "mysql", ""); err != nil {
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
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "test-branch"}, &svc); err != nil {
		t.Errorf("Service が作成されていない: %v", err)
	}
}

func TestStart_Postgres_ConfigMapなしでPodが作成される(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newProvider(c)

	if _, err := p.Start(ctx, "pg-branch", testVol, "postgres", ""); err != nil {
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

	if _, err := p.Start(ctx, "redis-branch", testVol, "redis", ""); err != nil {
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

	if _, err := p.Start(ctx, "ver-branch", testVol, "mysql", "8.4"); err != nil {
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

	if _, err := p.Start(ctx, "override-branch", testVol, "mysql", ""); err != nil {
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

	if _, err := p.Start(ctx, "x", testVol, "oracle", ""); err == nil {
		t.Error("unsupported db type should return error")
	}
}

func TestStart_dbTypeが空のときmysqlとして動作する(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newProvider(c)

	ep, err := p.Start(ctx, "default-branch", testVol, "", "")
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

	if _, err := p.Start(ctx, "label-branch", testVol, "mysql", ""); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	var pod corev1.Pod
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "branchdb-db-label-branch"}, &pod); err != nil {
		t.Fatalf("Pod が作成されていない: %v", err)
	}
	var svc corev1.Service
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "label-branch"}, &svc); err != nil {
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

	ep, err := p.Start(ctx, "my-branch", testVol, "mysql", "")
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if ep.Host != "my-branch.branchdb.svc.cluster.local" {
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

	ep, err := p.Start(ctx, "pg-port-branch", testVol, "postgres", "")
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

	ep, err := p.Start(ctx, "redis-port-branch", testVol, "redis", "")
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

	if _, err := p.Start(ctx, "np-branch", testVol, "mysql", ""); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	var svc corev1.Service
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "np-branch"}, &svc); err != nil {
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

	if _, err := p.Start(ctx, "stop-branch", testVol, "mysql", ""); err != nil {
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

	if _, err := p.Start(ctx, "idempotent-branch", testVol, "mysql", ""); err != nil {
		t.Fatalf("1回目の Start returned error: %v", err)
	}
	if _, err := p.Start(ctx, "idempotent-branch", testVol, "mysql", ""); err != nil {
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
	if _, err := p.Start(context.Background(), "fail-branch", testVol, "mysql", ""); err == nil {
		t.Error("Start はエラーを返すべき")
	}
}

func TestStart_PVC作成に失敗したときエラーを返す(t *testing.T) {
	base := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	c := &nthErrClient{Client: &nodePortAssigner{base}, failAt: 2, err: errors.New("PVC creation failed")}
	p := k8sdatabase.NewProvider(c, "branchdb", nil)
	if _, err := p.Start(context.Background(), "fail-pvc-branch", testVol, "mysql", ""); err == nil {
		t.Error("Start はエラーを返すべき")
	}
}

func TestStart_ConfigMap作成に失敗したときエラーを返す(t *testing.T) {
	base := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	// mysql は ConfigMap を作成するので failAt=3
	c := &nthErrClient{Client: &nodePortAssigner{base}, failAt: 3, err: errors.New("ConfigMap creation failed")}
	p := k8sdatabase.NewProvider(c, "branchdb", nil)
	if _, err := p.Start(context.Background(), "fail-cm-branch", testVol, "mysql", ""); err == nil {
		t.Error("Start はエラーを返すべき")
	}
}

func TestStart_Pod作成に失敗したときエラーを返す(t *testing.T) {
	base := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	// mysql: PV(1) PVC(2) CM(3) Pod(4)
	c := &nthErrClient{Client: &nodePortAssigner{base}, failAt: 4, err: errors.New("Pod creation failed")}
	p := k8sdatabase.NewProvider(c, "branchdb", nil)
	if _, err := p.Start(context.Background(), "fail-pod-branch", testVol, "mysql", ""); err == nil {
		t.Error("Start はエラーを返すべき")
	}
}

func TestStart_NodePortが割り当てられていないときエラーを返す(t *testing.T) {
	// nodePortAssigner なし → NodePort=0 のまま
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := k8sdatabase.NewProvider(c, "branchdb", nil)
	if _, err := p.Start(context.Background(), "no-nodeport-branch", testVol, "mysql", ""); err == nil {
		t.Error("NodePort が割り当てられていない場合 Start はエラーを返すべき")
	}
}

func TestStart_PVにhardマウントオプションとNFSv4が設定される(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newProvider(c)

	if _, err := p.Start(ctx, "mount-branch", testVol, "mysql", ""); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	var pv corev1.PersistentVolume
	if err := c.Get(ctx, types.NamespacedName{Name: "branchdb-pv-mount-branch"}, &pv); err != nil {
		t.Fatalf("PV が作成されていない: %v", err)
	}
	for _, want := range []string{"hard", "nfsvers=4.1"} {
		found := false
		for _, o := range pv.Spec.MountOptions {
			if o == want {
				found = true
			}
		}
		if !found {
			t.Errorf("PV.MountOptions に %q が含まれていない (got %v)", want, pv.Spec.MountOptions)
		}
	}
}

func TestStart_MySQL_PodにinitContainerが設定される(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newProvider(c)

	if _, err := p.Start(ctx, "init-branch", testVol, "mysql", ""); err != nil {
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

func TestStart_ConfigMapにinnodb_flush設定が含まれる(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newProvider(c)

	if _, err := p.Start(ctx, "cfg-branch", testVol, "mysql", ""); err != nil {
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
