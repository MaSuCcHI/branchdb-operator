// Package k8sdatabase は BranchDatabaseProvider を Kubernetes 上で実装する。
// MySQL・PostgreSQL・Redis に対応し、NFS バックの PersistentVolume を使用する。
package k8sdatabase

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

	"github.com/MaSuCcHI/branchdb-operator/internal/domain"
)

// nfsMountOptions は NFS ボリューム上でデータベースを動かすために必要なマウントオプション。
// hard: ネットワーク断絶時に I/O をブロックし データ破損を防ぐ（soft は禁止）。
// nfsvers=4.1: ロック機構がプロトコル統合済みのため NFSv3 の NLM スタック問題を回避。
var nfsMountOptions = []string{"hard", "nfsvers=4.1", "timeo=600", "retrans=3"}

// dbConfig はデータベース種別ごとの設定を保持する。
type dbConfig struct {
	defaultImage string
	port         int32
	dataDir      string
	containerEnv []corev1.EnvVar
	readinessCmd []string
	// needsPermFix が true のとき、NFS マウント後に busybox で chown を実行する initContainer を追加する。
	needsPermFix bool
	permFixUID   int64  // chown で設定する UID（needsPermFix=true のとき使用）
	// mysqlConfig が非空のとき ConfigMap を作成し /etc/... にマウントする。
	mysqlConfig     string // 設定ファイルの内容
	mysqlConfigPath string // コンテナ内のマウント先ディレクトリ
	mysqlConfigKey  string // ConfigMap のキー名
}

// builtinConfigs はサポートするデータベース種別のデフォルト設定。
var builtinConfigs = map[string]dbConfig{
	"mysql": {
		defaultImage: "mysql:8.0",
		port:         3306,
		dataDir:      "/var/lib/mysql",
		containerEnv: []corev1.EnvVar{{Name: "MYSQL_ALLOW_EMPTY_PASSWORD", Value: "yes"}},
		readinessCmd: []string{"mysqladmin", "ping", "-h", "localhost"},
		needsPermFix: true,
		permFixUID:   999,
		// innodb_flush_log_at_trx_commit=2: NFS の fsync レイテンシを隠蔽して実用速度を確保する。
		// 開発・テスト環境専用。本番 OLTP では 1 を使用すること。
		mysqlConfig:     "[mysqld]\ninnodb_flush_log_at_trx_commit=2\n",
		mysqlConfigPath: "/etc/mysql/conf.d",
		mysqlConfigKey:  "branchdb.cnf",
	},
	"postgres": {
		defaultImage: "postgres:16",
		port:         5432,
		dataDir:      "/var/lib/postgresql/data",
		containerEnv: []corev1.EnvVar{{Name: "POSTGRES_HOST_AUTH_METHOD", Value: "trust"}},
		readinessCmd: []string{"pg_isready", "-U", "postgres"},
		needsPermFix: true,
		permFixUID:   999,
	},
	"redis": {
		defaultImage: "redis:7",
		port:         6379,
		dataDir:      "/data",
		readinessCmd: []string{"redis-cli", "ping"},
		needsPermFix: false,
	},
}

// Provider は BranchDatabaseProvider interface を実装する。
// K8s API で Pod + PersistentVolume + PersistentVolumeClaim + Service を作成する。
type Provider struct {
	client       client.Client
	namespace    string
	imageOverrides map[string]string // dbType -> image override（空文字列はデフォルトを使用）
}

// NewProvider は Provider を生成する。
// imageOverrides でデフォルトイメージを上書きできる（例: {"mysql": "mysql:8.4"}）。
func NewProvider(c client.Client, namespace string, imageOverrides map[string]string) *Provider {
	if imageOverrides == nil {
		imageOverrides = map[string]string{}
	}
	return &Provider{client: c, namespace: namespace, imageOverrides: imageOverrides}
}

// Start は K8s 上に PV, PVC, (ConfigMap), Pod, Service を作成し BranchEndpoint を返す。
// dbType が空の場合は "mysql" として扱う。
// dbVersion が空の場合は dbType のデフォルトイメージタグを使用する。
func (p *Provider) Start(ctx context.Context, branchName string, vol domain.VolumeInfo, dbType, dbVersion string) (domain.BranchEndpoint, error) {
	if dbType == "" {
		dbType = "mysql"
	}
	cfg, ok := builtinConfigs[dbType]
	if !ok {
		return domain.BranchEndpoint{}, fmt.Errorf("unsupported database type: %q (supported: mysql, postgres, redis)", dbType)
	}

	image := p.resolveImage(cfg, dbType, dbVersion)

	if err := p.createPV(ctx, branchName, vol); err != nil {
		return domain.BranchEndpoint{}, fmt.Errorf("create PV: %w", err)
	}
	if err := p.createPVC(ctx, branchName); err != nil {
		return domain.BranchEndpoint{}, fmt.Errorf("create PVC: %w", err)
	}
	if cfg.mysqlConfig != "" {
		if err := p.createConfigMap(ctx, branchName, cfg); err != nil {
			return domain.BranchEndpoint{}, fmt.Errorf("create ConfigMap: %w", err)
		}
	}
	if err := p.createPod(ctx, branchName, image, cfg); err != nil {
		return domain.BranchEndpoint{}, fmt.Errorf("create Pod: %w", err)
	}
	if err := p.createService(ctx, branchName, cfg.port); err != nil {
		return domain.BranchEndpoint{}, fmt.Errorf("create Service: %w", err)
	}
	nodePort, err := p.getNodePort(ctx, branchName)
	if err != nil {
		return domain.BranchEndpoint{}, fmt.Errorf("get NodePort: %w", err)
	}
	return domain.BranchEndpoint{
		Host:         fmt.Sprintf("%s.%s.svc.cluster.local", branchName, p.namespace),
		Port:         int(cfg.port),
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

// resolveImage はイメージを決定する。優先順位: dbVersion引数 > imageOverrides > builtinDefaults
func (p *Provider) resolveImage(cfg dbConfig, dbType, dbVersion string) string {
	if dbVersion != "" {
		// baseImage:tag 形式にする（dbType に応じて base 部分を変える）
		base := baseImageName(cfg.defaultImage)
		return fmt.Sprintf("%s:%s", base, dbVersion)
	}
	if override, ok := p.imageOverrides[dbType]; ok && override != "" {
		return override
	}
	return cfg.defaultImage
}

// baseImageName は "mysql:8.0" → "mysql" のようにタグを除いたイメージ名を返す。
func baseImageName(image string) string {
	for i := len(image) - 1; i >= 0; i-- {
		if image[i] == ':' {
			return image[:i]
		}
	}
	return image
}

func pvName(branchName string) string  { return fmt.Sprintf("branchdb-pv-%s", branchName) }
func pvcName(branchName string) string { return fmt.Sprintf("branchdb-pvc-%s", branchName) }
// podName はブランチ名から Pod 名を生成する。DB 種別に関わらず一意な名前を使用する。
func podName(branchName string) string { return fmt.Sprintf("branchdb-db-%s", branchName) }
func cmName(branchName string) string  { return fmt.Sprintf("branchdb-cfg-%s", branchName) }

func (p *Provider) createPV(ctx context.Context, branchName string, vol domain.VolumeInfo) error {
	storageClass := ""
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: pvName(branchName)},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("10Gi"),
			},
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			MountOptions:     nfsMountOptions,
			StorageClassName: storageClass,
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				NFS: &corev1.NFSVolumeSource{Server: vol.NFSServer, Path: vol.NFSPath},
			},
		},
	}
	err := p.client.Create(ctx, pv)
	if errors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func (p *Provider) createPVC(ctx context.Context, branchName string) error {
	storageClass := ""
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: pvcName(branchName), Namespace: p.namespace},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
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

func (p *Provider) createConfigMap(ctx context.Context, branchName string, cfg dbConfig) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: cmName(branchName), Namespace: p.namespace},
		Data:       map[string]string{cfg.mysqlConfigKey: cfg.mysqlConfig},
	}
	err := p.client.Create(ctx, cm)
	if errors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func (p *Provider) createPod(ctx context.Context, branchName, image string, cfg dbConfig) error {
	const dataVolName = "db-data"
	const cfgVolName = "db-config"
	initialDelay := int32(10)
	period := int32(5)

	volumeMounts := []corev1.VolumeMount{
		{Name: dataVolName, MountPath: cfg.dataDir},
	}
	volumes := []corev1.Volume{
		{
			Name: dataVolName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: pvcName(branchName),
				},
			},
		},
	}

	if cfg.mysqlConfig != "" {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      cfgVolName,
			MountPath: cfg.mysqlConfigPath,
		})
		volumes = append(volumes, corev1.Volume{
			Name: cfgVolName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: cmName(branchName)},
				},
			},
		})
	}

	spec := corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name:         "db",
				Image:        image,
				Env:          cfg.containerEnv,
				VolumeMounts: volumeMounts,
				ReadinessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						Exec: &corev1.ExecAction{Command: cfg.readinessCmd},
					},
					InitialDelaySeconds: initialDelay,
					PeriodSeconds:       period,
				},
			},
		},
		Volumes: volumes,
	}

	if cfg.needsPermFix {
		rootUID := int64(0)
		uid := cfg.permFixUID
		spec.InitContainers = []corev1.Container{
			{
				Name:    "fix-permissions",
				Image:   "busybox:1.36",
				Command: []string{"sh", "-c", fmt.Sprintf("chown -R %d:%d %s", uid, uid, cfg.dataDir)},
				SecurityContext: &corev1.SecurityContext{RunAsUser: &rootUID},
				VolumeMounts: []corev1.VolumeMount{
					{Name: dataVolName, MountPath: cfg.dataDir},
				},
			},
		}
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName(branchName),
			Namespace: p.namespace,
			Labels:    map[string]string{"branchdb-branch": branchName},
		},
		Spec: spec,
	}
	err := p.client.Create(ctx, pod)
	if errors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func (p *Provider) createService(ctx context.Context, branchName string, port int32) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: branchName, Namespace: p.namespace},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeNodePort,
			Selector: map[string]string{"branchdb-branch": branchName},
			Ports: []corev1.ServicePort{
				{Port: port, TargetPort: intstr.FromInt32(port)},
			},
		},
	}
	err := p.client.Create(ctx, svc)
	if errors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

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
