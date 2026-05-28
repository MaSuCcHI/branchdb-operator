package k8smysql_test

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
	"github.com/MaSuCcHI/branchdb-operator/internal/infrastructure/k8smysql"
)

// nodePortAssigner は NodePort Service 作成時に NodePort を自動付与するフェイククライアント。
// 本番では kube-apiserver が割り当てるが、fake client は割り当てない。
type nodePortAssigner struct {
	client.Client
}

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

func newFakeProvider(c client.Client) *k8smysql.Provider {
	return k8smysql.NewProvider(&nodePortAssigner{c}, "branchdb", "mysql:8.0")
}

func TestStart_PV_PVC_ConfigMap_Pod_Serviceが作成される(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newFakeProvider(c)

	vol := domain.VolumeInfo{NFSServer: "192.168.1.1", NFSPath: "/exports/test"}
	_, err := p.Start(ctx, "test-branch", vol)
	if err != nil {
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
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "branchdb-mysql-cfg-test-branch"}, &cm); err != nil {
		t.Errorf("ConfigMap が作成されていない: %v", err)
	}

	var pod corev1.Pod
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "branchdb-mysql-test-branch"}, &pod); err != nil {
		t.Errorf("Pod が作成されていない: %v", err)
	}

	var svc corev1.Service
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "test-branch"}, &svc); err != nil {
		t.Errorf("Service が作成されていない: %v", err)
	}
}

func TestStart_PodのラベルがServiceセレクタと一致する(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newFakeProvider(c)

	vol := domain.VolumeInfo{NFSServer: "192.168.1.1", NFSPath: "/exports/test"}
	if _, err := p.Start(ctx, "label-branch", vol); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	var pod corev1.Pod
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "branchdb-mysql-label-branch"}, &pod); err != nil {
		t.Fatalf("Pod が作成されていない: %v", err)
	}
	var svc corev1.Service
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "label-branch"}, &svc); err != nil {
		t.Fatalf("Service が作成されていない: %v", err)
	}

	// Service のセレクタが Pod のラベルにマッチしなければトラフィックは届かない。
	for k, v := range svc.Spec.Selector {
		if pod.Labels[k] != v {
			t.Errorf("Pod label %q=%q does not match Service selector %q=%q", k, pod.Labels[k], k, v)
		}
	}
	if len(svc.Spec.Selector) == 0 {
		t.Error("Service selector is empty")
	}
}

func TestStart_BranchEndpointのHostとPortが正しく返る(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newFakeProvider(c)

	vol := domain.VolumeInfo{NFSServer: "192.168.1.1", NFSPath: "/exports/test"}
	ep, err := p.Start(ctx, "my-branch", vol)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	expectedHost := "my-branch.branchdb.svc.cluster.local"
	if ep.Host != expectedHost {
		t.Errorf("Host = %q, want %q", ep.Host, expectedHost)
	}
	if ep.Port != 3306 {
		t.Errorf("Port = %d, want 3306", ep.Port)
	}
	if ep.ExternalPort == 0 {
		t.Error("ExternalPort should be non-zero (NodePort assigned by K8s)")
	}
}

func TestStart_ServiceがNodePortタイプで作成される(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newFakeProvider(c)

	vol := domain.VolumeInfo{NFSServer: "192.168.1.1", NFSPath: "/exports/test"}
	if _, err := p.Start(ctx, "np-branch", vol); err != nil {
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
		t.Error("Service に NodePort が割り当てられていない")
	}
}

func TestStop_PV_PVC_ConfigMap_Pod_Serviceが削除される(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newFakeProvider(c)

	vol := domain.VolumeInfo{NFSServer: "192.168.1.1", NFSPath: "/exports/stop-test"}
	if _, err := p.Start(ctx, "stop-branch", vol); err != nil {
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
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "branchdb-mysql-cfg-stop-branch"}, &cm); err == nil {
		t.Error("ConfigMap が削除されていない")
	}

	var pod corev1.Pod
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "branchdb-mysql-stop-branch"}, &pod); err == nil {
		t.Error("Pod が削除されていない")
	}

	var svc corev1.Service
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "stop-branch"}, &svc); err == nil {
		t.Error("Service が削除されていない")
	}
}

func TestStart_同名のリソースが既に存在しても成功する(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newFakeProvider(c)

	vol := domain.VolumeInfo{NFSServer: "10.0.0.1", NFSPath: "/nfs/idempotent"}

	// 1回目
	if _, err := p.Start(ctx, "idempotent-branch", vol); err != nil {
		t.Fatalf("1回目の Start returned error: %v", err)
	}

	// 2回目（冪等性）
	if _, err := p.Start(ctx, "idempotent-branch", vol); err != nil {
		t.Fatalf("2回目の Start returned error: %v", err)
	}
}

// nthErrClient は N 回目の Create 呼び出しで指定エラーを返すフェイククライアント（1-indexed）。
type nthErrClient struct {
	client.Client
	failAt  int
	callNum int
	err     error
}

func (e *nthErrClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	e.callNum++
	if e.callNum == e.failAt {
		return e.err
	}
	return e.Client.Create(ctx, obj, opts...)
}

func TestStart_PV作成に失敗したときエラーを返す(t *testing.T) {
	ctx := context.Background()
	base := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	c := &nthErrClient{Client: &nodePortAssigner{base}, failAt: 1, err: errors.New("API server unreachable")}
	p := k8smysql.NewProvider(c, "branchdb", "mysql:8.0")

	vol := domain.VolumeInfo{NFSServer: "10.0.0.1", NFSPath: "/nfs/fail"}
	if _, err := p.Start(ctx, "fail-branch", vol); err == nil {
		t.Error("Start はエラーを返すべきだがエラーがなかった")
	}
}

func TestStart_PVC作成に失敗したときエラーを返す(t *testing.T) {
	ctx := context.Background()
	base := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	c := &nthErrClient{Client: &nodePortAssigner{base}, failAt: 2, err: errors.New("PVC creation failed")}
	p := k8smysql.NewProvider(c, "branchdb", "mysql:8.0")

	vol := domain.VolumeInfo{NFSServer: "10.0.0.1", NFSPath: "/nfs/fail-pvc"}
	if _, err := p.Start(ctx, "fail-pvc-branch", vol); err == nil {
		t.Error("Start はエラーを返すべきだがエラーがなかった")
	}
}

func TestStart_ConfigMap作成に失敗したときエラーを返す(t *testing.T) {
	ctx := context.Background()
	base := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	c := &nthErrClient{Client: &nodePortAssigner{base}, failAt: 3, err: errors.New("ConfigMap creation failed")}
	p := k8smysql.NewProvider(c, "branchdb", "mysql:8.0")

	vol := domain.VolumeInfo{NFSServer: "10.0.0.1", NFSPath: "/nfs/fail-cm"}
	if _, err := p.Start(ctx, "fail-cm-branch", vol); err == nil {
		t.Error("Start はエラーを返すべきだがエラーがなかった")
	}
}

func TestStart_Pod作成に失敗したときエラーを返す(t *testing.T) {
	ctx := context.Background()
	base := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	c := &nthErrClient{Client: &nodePortAssigner{base}, failAt: 4, err: errors.New("Pod creation failed")}
	p := k8smysql.NewProvider(c, "branchdb", "mysql:8.0")

	vol := domain.VolumeInfo{NFSServer: "10.0.0.1", NFSPath: "/nfs/fail-pod"}
	if _, err := p.Start(ctx, "fail-pod-branch", vol); err == nil {
		t.Error("Start はエラーを返すべきだがエラーがなかった")
	}
}

func TestStart_NodePortが割り当てられていないときエラーを返す(t *testing.T) {
	ctx := context.Background()
	// nodePortAssigner を使わない素のフェイククライアントを使う。
	// NodePort=0 のまま Service が作成されるため getNodePort がエラーを返す。
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := k8smysql.NewProvider(c, "branchdb", "mysql:8.0")

	vol := domain.VolumeInfo{NFSServer: "192.168.1.1", NFSPath: "/exports/test"}
	if _, err := p.Start(ctx, "no-nodeport-branch", vol); err == nil {
		t.Error("NodePort が割り当てられていない場合 Start はエラーを返すべき")
	}
}

func TestStart_Service作成に失敗したときエラーを返す(t *testing.T) {
	ctx := context.Background()
	base := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	c := &nthErrClient{Client: &nodePortAssigner{base}, failAt: 5, err: errors.New("Service creation failed")}
	p := k8smysql.NewProvider(c, "branchdb", "mysql:8.0")

	vol := domain.VolumeInfo{NFSServer: "10.0.0.1", NFSPath: "/nfs/fail-svc"}
	if _, err := p.Start(ctx, "fail-svc-branch", vol); err == nil {
		t.Error("Start はエラーを返すべきだがエラーがなかった")
	}
}

func TestStart_PVにhardマウントオプションとNFSv4が設定される(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newFakeProvider(c)

	vol := domain.VolumeInfo{NFSServer: "192.168.1.1", NFSPath: "/exports/mount-opts"}
	if _, err := p.Start(ctx, "mount-branch", vol); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	var pv corev1.PersistentVolume
	if err := c.Get(ctx, types.NamespacedName{Name: "branchdb-pv-mount-branch"}, &pv); err != nil {
		t.Fatalf("PV が作成されていない: %v", err)
	}

	opts := pv.Spec.MountOptions
	mustContain := []string{"hard", "nfsvers=4.1"}
	for _, want := range mustContain {
		found := false
		for _, o := range opts {
			if o == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("PV.MountOptions に %q が含まれていない (got %v)", want, opts)
		}
	}
}

func TestStart_PodにinitContainerとConfigMapマウントが設定される(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newFakeProvider(c)

	vol := domain.VolumeInfo{NFSServer: "192.168.1.1", NFSPath: "/exports/init-test"}
	if _, err := p.Start(ctx, "init-branch", vol); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	var pod corev1.Pod
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "branchdb-mysql-init-branch"}, &pod); err != nil {
		t.Fatalf("Pod が作成されていない: %v", err)
	}

	if len(pod.Spec.InitContainers) == 0 {
		t.Fatal("Pod に InitContainer が設定されていない")
	}
	initC := pod.Spec.InitContainers[0]
	if initC.Name != "fix-permissions" {
		t.Errorf("InitContainer.Name = %q, want %q", initC.Name, "fix-permissions")
	}
	if initC.SecurityContext == nil || initC.SecurityContext.RunAsUser == nil || *initC.SecurityContext.RunAsUser != 0 {
		t.Error("fix-permissions InitContainer は root (UID 0) で実行される必要がある")
	}

	// mysql コンテナに mysql-config ボリュームがマウントされていること。
	mysqlC := pod.Spec.Containers[0]
	hasCfgMount := false
	for _, m := range mysqlC.VolumeMounts {
		if m.Name == "mysql-config" {
			hasCfgMount = true
			break
		}
	}
	if !hasCfgMount {
		t.Error("mysql コンテナに mysql-config VolumeMount が設定されていない")
	}
}

func TestStart_ConfigMapにinnodb_flush設定が含まれる(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	p := newFakeProvider(c)

	vol := domain.VolumeInfo{NFSServer: "192.168.1.1", NFSPath: "/exports/cfg-test"}
	if _, err := p.Start(ctx, "cfg-branch", vol); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	var cm corev1.ConfigMap
	if err := c.Get(ctx, types.NamespacedName{Namespace: "branchdb", Name: "branchdb-mysql-cfg-cfg-branch"}, &cm); err != nil {
		t.Fatalf("ConfigMap が作成されていない: %v", err)
	}

	const wantKey = "branchdb.cnf"
	val, ok := cm.Data[wantKey]
	if !ok {
		t.Fatalf("ConfigMap.Data[%q] が存在しない", wantKey)
	}
	if !containsString(val, "innodb_flush_log_at_trx_commit") {
		t.Errorf("ConfigMap に innodb_flush_log_at_trx_commit 設定が含まれていない: %q", val)
	}
}

func containsString(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && stringContains(s, sub))
}

func stringContains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
