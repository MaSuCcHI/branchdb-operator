package k8smysql

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/keisuke/zfs-db-k8s/internal/domain"
)

// nfsMountOptions は NFS over MySQL に必要なマウントオプション。
// hard: ネットワーク断絶時に I/O をブロックし InnoDB 破損を防ぐ（soft は禁止）。
// nfsvers=4.1: ロック機構がプロトコル統合済みのため NFSv3 の NLM スタック問題を回避。
// timeo/retrans: 瞬断に対して過敏な I/O エラーを起こさないチューニング。
var nfsMountOptions = []string{"hard", "nfsvers=4.1", "timeo=600", "retrans=3"}

const (
	mysqlConfigKey   = "branchdb.cnf"
	// innodb_flush_log_at_trx_commit=2: NFS の fsync レイテンシを隠蔽し実用速度を確保する。
	// 開発・テスト環境専用。本番 OLTP では 1 を使用すること。
	mysqlConfigValue = "[mysqld]\ninnodb_flush_log_at_trx_commit=2\n"
	mysqlUID         = int64(999) // mysql:8.0 のデフォルト実行ユーザー
	rootUID          = int64(0)
)

// Provider は BranchMySQLProvider interface を実装する。
// K8s API で Pod + PersistentVolume + PersistentVolumeClaim + Service を作成する。
type Provider struct {
	client    client.Client
	namespace string
	image     string
}

// NewProvider creates a new Provider.
func NewProvider(c client.Client, namespace, image string) *Provider {
	return &Provider{
		client:    c,
		namespace: namespace,
		image:     image,
	}
}

// Start は K8s 上に PV, PVC, ConfigMap, Pod, Service を作成し BranchEndpoint を返す。
func (p *Provider) Start(ctx context.Context, branchName string, vol domain.VolumeInfo) (domain.BranchEndpoint, error) {
	if err := p.createPV(ctx, branchName, vol); err != nil {
		return domain.BranchEndpoint{}, fmt.Errorf("create PV: %w", err)
	}
	if err := p.createPVC(ctx, branchName); err != nil {
		return domain.BranchEndpoint{}, fmt.Errorf("create PVC: %w", err)
	}
	if err := p.createConfigMap(ctx, branchName); err != nil {
		return domain.BranchEndpoint{}, fmt.Errorf("create ConfigMap: %w", err)
	}
	if err := p.createPod(ctx, branchName); err != nil {
		return domain.BranchEndpoint{}, fmt.Errorf("create Pod: %w", err)
	}
	if err := p.createService(ctx, branchName); err != nil {
		return domain.BranchEndpoint{}, fmt.Errorf("create Service: %w", err)
	}
	nodePort, err := p.getNodePort(ctx, branchName)
	if err != nil {
		return domain.BranchEndpoint{}, fmt.Errorf("get NodePort: %w", err)
	}
	return domain.BranchEndpoint{
		Host:         fmt.Sprintf("%s.%s.svc.cluster.local", branchName, p.namespace),
		Port:         3306,
		ExternalPort: nodePort,
	}, nil
}

// Stop は K8s 上の Service, Pod, ConfigMap, PVC, PV を削除する。エラーは無視して全削除を試みる。
func (p *Provider) Stop(ctx context.Context, branchName string) error {
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: branchName, Namespace: p.namespace}}
	_ = p.client.Delete(ctx, svc)

	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: podName(branchName), Namespace: p.namespace}}
	_ = p.client.Delete(ctx, pod)

	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: cmName(branchName), Namespace: p.namespace}}
	_ = p.client.Delete(ctx, cm)

	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: pvcName(branchName), Namespace: p.namespace}}
	_ = p.client.Delete(ctx, pvc)

	pv := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: pvName(branchName)}}
	_ = p.client.Delete(ctx, pv)

	return nil
}

func pvName(branchName string) string  { return fmt.Sprintf("branchdb-pv-%s", branchName) }
func pvcName(branchName string) string { return fmt.Sprintf("branchdb-pvc-%s", branchName) }
func podName(branchName string) string { return fmt.Sprintf("branchdb-mysql-%s", branchName) }
func cmName(branchName string) string  { return fmt.Sprintf("branchdb-mysql-cfg-%s", branchName) }

func (p *Provider) createPV(ctx context.Context, branchName string, vol domain.VolumeInfo) error {
	storageClass := ""
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: pvName(branchName),
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("10Gi"),
			},
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			MountOptions:     nfsMountOptions,
			StorageClassName: storageClass,
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				NFS: &corev1.NFSVolumeSource{
					Server: vol.NFSServer,
					Path:   vol.NFSPath,
				},
			},
		},
	}
	err := p.client.Create(ctx, pv)
	if errors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func (p *Provider) createConfigMap(ctx context.Context, branchName string) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName(branchName),
			Namespace: p.namespace,
		},
		Data: map[string]string{
			mysqlConfigKey: mysqlConfigValue,
		},
	}
	err := p.client.Create(ctx, cm)
	if errors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func (p *Provider) createPVC(ctx context.Context, branchName string) error {
	storageClass := ""
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName(branchName),
			Namespace: p.namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			StorageClassName: &storageClass,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
	}
	err := p.client.Create(ctx, pvc)
	if errors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func (p *Provider) createPod(ctx context.Context, branchName string) error {
	initialDelay := int32(10)
	period := int32(5)
	rootUIDVal := rootUID
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName(branchName),
			Namespace: p.namespace,
			Labels: map[string]string{
				"branchdb-branch": branchName,
			},
		},
		Spec: corev1.PodSpec{
			// NFSボリュームはデフォルト root:root 所有のため、MySQLコンテナ(UID 999)が
			// 書き込めるよう起動前に権限を修正する。
			InitContainers: []corev1.Container{
				{
					Name:    "fix-permissions",
					Image:   "busybox:1.36",
					Command: []string{"sh", "-c", "chown -R 999:999 /var/lib/mysql"},
					SecurityContext: &corev1.SecurityContext{
						RunAsUser: &rootUIDVal,
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "mysql-data", MountPath: "/var/lib/mysql"},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:  "mysql",
					Image: p.image,
					Env: []corev1.EnvVar{
						{Name: "MYSQL_ALLOW_EMPTY_PASSWORD", Value: "yes"},
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "mysql-data", MountPath: "/var/lib/mysql"},
						{Name: "mysql-config", MountPath: "/etc/mysql/conf.d"},
					},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							Exec: &corev1.ExecAction{
								Command: []string{"mysqladmin", "ping", "-h", "localhost"},
							},
						},
						InitialDelaySeconds: initialDelay,
						PeriodSeconds:       period,
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "mysql-data",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcName(branchName),
						},
					},
				},
				{
					Name: "mysql-config",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: cmName(branchName),
							},
						},
					},
				},
			},
		},
	}
	err := p.client.Create(ctx, pod)
	if errors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func (p *Provider) createService(ctx context.Context, branchName string) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      branchName,
			Namespace: p.namespace,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeNodePort,
			Selector: map[string]string{
				"branchdb-branch": branchName,
			},
			Ports: []corev1.ServicePort{
				{
					Port:       3306,
					TargetPort: intstr.FromInt32(3306),
				},
			},
		},
	}
	err := p.client.Create(ctx, svc)
	if errors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

// getNodePort は作成済み Service の NodePort を返す。
func (p *Provider) getNodePort(ctx context.Context, branchName string) (int, error) {
	var svc corev1.Service
	if err := p.client.Get(ctx, types.NamespacedName{Name: branchName, Namespace: p.namespace}, &svc); err != nil {
		return 0, fmt.Errorf("get service: %w", err)
	}
	for _, port := range svc.Spec.Ports {
		if port.NodePort > 0 {
			return int(port.NodePort), nil
		}
	}
	return 0, fmt.Errorf("NodePort not yet assigned for service %s", branchName)
}
