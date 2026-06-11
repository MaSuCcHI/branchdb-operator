// Package k8sdatabase は BranchDatabaseProvider を Kubernetes 上で実装する。
// MySQL・PostgreSQL・Redis に対応し、NFS バックの PersistentVolume を使用する。
package k8sdatabase

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	goerrors "errors"
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

// defaultContainerRequests / defaultContainerLimits は DB Pod の resource requests/limits のデフォルト値。
// ノイジーネイバー対策と PodSecurity Policy への準拠のためにハードコードする。
const (
	defaultCPURequest    = "100m"
	defaultMemoryRequest = "256Mi"
	defaultCPULimit      = "2"
	defaultMemoryLimit   = "2Gi"
)

// nfsMountOptions は NFS ボリューム上でデータベースを動かすために必要なマウントオプション。
// hard:        ネットワーク瞬断時にエラーを返さず I/O をブロックしてデータ破損を防ぐ（soft は禁止）。
// proto=tcp:   信頼性の高い TCP を使用する。
// nfsvers=4.1: ロック機構がプロトコル統合済みのため NFSv3 の NLM スタック問題を回避。
// rsize/wsize: ブロックサイズを 1MB に最大化してスループットを向上させる。
// timeo=600:   タイムアウトを 60 秒に設定する（単位は 0.1 秒）。
// retrans=2:   リトライ回数。
var nfsMountOptions = []string{
	"hard",
	"proto=tcp",
	"nfsvers=4.1",
	"rsize=1048576",
	"wsize=1048576",
	"timeo=600",
	"retrans=2",
}

// dbConfig はデータベース種別ごとの設定を保持する。
type dbConfig struct {
	defaultImage  string
	port          int32
	dataDir       string
	containerEnv  []corev1.EnvVar
	containerArgs []string // Pod Container.Args（entrypoint への追加引数）
	readinessCmd  []string
	// needsPermFix が true のとき、NFS マウント後に busybox で chown を実行する initContainer を追加する。
	needsPermFix bool
	permFixUID   int64 // chown で設定する UID（needsPermFix=true のとき使用）
	// extraConfig が非空のとき ConfigMap を作成しコンテナにマウントする。
	extraConfig          string // 設定ファイルの内容
	extraConfigMountPath string // コンテナ内のマウント先ディレクトリ
	extraConfigKey       string // ConfigMap のキー名
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
		extraConfig:          "[mysqld]\ninnodb_flush_log_at_trx_commit=2\n",
		extraConfigMountPath: "/etc/mysql/conf.d",
		extraConfigKey:       "branchdb.cnf",
	},
	"postgres": {
		defaultImage: "postgres:16",
		port:         5432,
		dataDir:      "/var/lib/postgresql/data",
		containerEnv: []corev1.EnvVar{{Name: "POSTGRES_HOST_AUTH_METHOD", Value: "trust"}},
		readinessCmd: []string{"pg_isready", "-U", "postgres"},
		needsPermFix: true,
		permFixUID:   999,
		// NFS 上での開発・テスト用に fsync を無効化してレイテンシを削減する。本番では使用しないこと。
		// full_page_writes=off: NFS での WAL I/O を削減する。
		containerArgs: []string{
			"-c", "fsync=off",
			"-c", "synchronous_commit=off",
			"-c", "full_page_writes=off",
		},
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
	client         client.Client
	namespace      string
	imageOverrides map[string]string // dbType -> image override（空文字列はデフォルトを使用）
	generatedAuth  bool              // true のとき各ブランチにランダムパスワードを生成して Secret に保存する
}

// ProviderOption は Provider の動作を設定する関数型オプション。
type ProviderOption func(*Provider)

// WithGeneratedAuth はブランチごとにランダムパスワードを生成する機能を有効/無効化する。
// 有効時: パスワードを K8s Secret（branchdb-cred-<branch>）に保存し、DB Pod の環境変数/引数に設定する。
// デフォルト（false）は後方互換のため無認証で動作する。
func WithGeneratedAuth(enabled bool) ProviderOption {
	return func(p *Provider) { p.generatedAuth = enabled }
}

// NewProvider は Provider を生成する。
// imageOverrides でデフォルトイメージを上書きできる（例: {"mysql": "mysql:8.4"}）。
// opts には WithGeneratedAuth など ProviderOption を指定できる。
func NewProvider(c client.Client, namespace string, imageOverrides map[string]string, opts ...ProviderOption) *Provider {
	if imageOverrides == nil {
		imageOverrides = map[string]string{}
	}
	p := &Provider{client: c, namespace: namespace, imageOverrides: imageOverrides}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// credSecretName はブランチ名から Credential Secret 名を生成する。
func credSecretName(branchName string) string { return fmt.Sprintf("branchdb-cred-%s", branchName) }

// generatePassword は URL-safe な 24 文字のランダムパスワードを生成する。
func generatePassword() (string, error) {
	b := make([]byte, 18) // base64 で 24 文字になる
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate password: %w", err)
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// Start は K8s 上に PV, PVC, (ConfigMap), Pod, Service を作成し BranchEndpoint を返す。
// dbType が空の場合は "mysql" として扱う。
// dbVersion が空の場合は dbType のデフォルトイメージタグを使用する。
// owner が非 nil の場合は全リソースに OwnerReference を設定する。
// generatedAuth=true のときはブランチ固有のパスワードを生成し、Secret に保存して BranchEndpoint に返す。
func (p *Provider) Start(ctx context.Context, branchName string, vol domain.VolumeInfo, dbType, dbVersion string, owner *domain.OwnerRef) (domain.BranchEndpoint, error) {
	if dbType == "" {
		dbType = "mysql"
	}
	cfg, ok := builtinConfigs[dbType]
	if !ok {
		return domain.BranchEndpoint{}, fmt.Errorf("unsupported database type: %q (supported: mysql, postgres, redis)", dbType)
	}

	// パスワード生成（generatedAuth=true のみ）
	var password string
	if p.generatedAuth {
		var err error
		password, err = generatePassword()
		if err != nil {
			return domain.BranchEndpoint{}, err
		}
	}

	// パスワードが設定されている場合は cfg の認証設定を上書きする
	effectiveCfg := applyAuthToCfg(cfg, dbType, password)

	image := p.resolveImage(cfg, dbType, dbVersion)

	ownerRefs := buildOwnerRefs(owner)

	// Secret を先に作成（Pod が参照するため）
	if password != "" {
		if err := p.createCredSecret(ctx, branchName, password, ownerRefs); err != nil {
			return domain.BranchEndpoint{}, fmt.Errorf("create credential secret: %w", err)
		}
	}

	if err := p.createPV(ctx, branchName, vol, ownerRefs); err != nil {
		return domain.BranchEndpoint{}, fmt.Errorf("create PV: %w", err)
	}
	if err := p.createPVC(ctx, branchName, ownerRefs); err != nil {
		return domain.BranchEndpoint{}, fmt.Errorf("create PVC: %w", err)
	}
	if effectiveCfg.extraConfig != "" {
		if err := p.createConfigMap(ctx, branchName, effectiveCfg, ownerRefs); err != nil {
			return domain.BranchEndpoint{}, fmt.Errorf("create ConfigMap: %w", err)
		}
	}
	if err := p.createPod(ctx, branchName, image, effectiveCfg, ownerRefs); err != nil {
		return domain.BranchEndpoint{}, fmt.Errorf("create Pod: %w", err)
	}
	if err := p.createService(ctx, branchName, effectiveCfg.port, ownerRefs); err != nil {
		return domain.BranchEndpoint{}, fmt.Errorf("create Service: %w", err)
	}
	nodePort, err := p.getNodePort(ctx, branchName)
	if err != nil {
		return domain.BranchEndpoint{}, fmt.Errorf("get NodePort: %w", err)
	}

	ep := domain.BranchEndpoint{
		Host:         fmt.Sprintf("%s.%s.svc.cluster.local", svcName(branchName), p.namespace),
		Port:         int(effectiveCfg.port),
		ExternalPort: nodePort,
	}
	if password != "" {
		ep.Password = password
		ep.CredentialSecret = credSecretName(branchName)
	}
	return ep, nil
}

// applyAuthToCfg は生成されたパスワードを dbConfig に反映した新しい dbConfig を返す。
// password が空の場合は元の cfg をそのまま返す。
func applyAuthToCfg(cfg dbConfig, dbType, password string) dbConfig {
	if password == "" {
		return cfg
	}
	out := cfg // コピー
	switch dbType {
	case "mysql":
		// MYSQL_ALLOW_EMPTY_PASSWORD を除去し MYSQL_ROOT_PASSWORD を設定する
		newEnv := make([]corev1.EnvVar, 0, len(cfg.containerEnv))
		for _, e := range cfg.containerEnv {
			if e.Name != "MYSQL_ALLOW_EMPTY_PASSWORD" {
				newEnv = append(newEnv, e)
			}
		}
		newEnv = append(newEnv, corev1.EnvVar{Name: "MYSQL_ROOT_PASSWORD", Value: password})
		out.containerEnv = newEnv
	case "postgres":
		// POSTGRES_HOST_AUTH_METHOD=trust を除去し POSTGRES_PASSWORD を設定する
		newEnv := make([]corev1.EnvVar, 0, len(cfg.containerEnv))
		for _, e := range cfg.containerEnv {
			if e.Name != "POSTGRES_HOST_AUTH_METHOD" {
				newEnv = append(newEnv, e)
			}
		}
		newEnv = append(newEnv, corev1.EnvVar{Name: "POSTGRES_PASSWORD", Value: password})
		out.containerEnv = newEnv
	case "redis":
		// --requirepass <password> を containerArgs に追加する
		newArgs := make([]string, len(cfg.containerArgs), len(cfg.containerArgs)+2)
		copy(newArgs, cfg.containerArgs)
		newArgs = append(newArgs, "--requirepass", password)
		out.containerArgs = newArgs
	}
	return out
}

// Stop は K8s 上の Service, Pod, ConfigMap, PVC, PV, (Credential Secret) を削除する。
// NotFound エラーは無視し（リソースが既に存在しない場合）、それ以外のエラーは集約して返す。
func (p *Provider) Stop(ctx context.Context, branchName string) error {
	var errs []error

	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: svcName(branchName), Namespace: p.namespace}}
	if err := p.client.Delete(ctx, svc); err != nil && !errors.IsNotFound(err) {
		errs = append(errs, fmt.Errorf("delete Service: %w", err))
	}

	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: podName(branchName), Namespace: p.namespace}}
	if err := p.client.Delete(ctx, pod); err != nil && !errors.IsNotFound(err) {
		errs = append(errs, fmt.Errorf("delete Pod: %w", err))
	}

	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: cmName(branchName), Namespace: p.namespace}}
	if err := p.client.Delete(ctx, cm); err != nil && !errors.IsNotFound(err) {
		errs = append(errs, fmt.Errorf("delete ConfigMap: %w", err))
	}

	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: pvcName(branchName), Namespace: p.namespace}}
	if err := p.client.Delete(ctx, pvc); err != nil && !errors.IsNotFound(err) {
		errs = append(errs, fmt.Errorf("delete PVC: %w", err))
	}

	pv := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: pvName(branchName)}}
	if err := p.client.Delete(ctx, pv); err != nil && !errors.IsNotFound(err) {
		errs = append(errs, fmt.Errorf("delete PV: %w", err))
	}

	// Credential Secret は generatedAuth の有無にかかわらず削除を試みる（NotFound は無視）。
	// ownerRef が設定されていれば K8s GC でも消えるが、明示的に削除して確実にする。
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: credSecretName(branchName), Namespace: p.namespace}}
	if err := p.client.Delete(ctx, secret); err != nil && !errors.IsNotFound(err) {
		errs = append(errs, fmt.Errorf("delete credential secret: %w", err))
	}

	return goerrors.Join(errs...)
}

// createCredSecret はブランチ固有のパスワードを保存する K8s Secret を作成する。
func (p *Provider) createCredSecret(ctx context.Context, branchName, password string, ownerRefs []metav1.OwnerReference) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            credSecretName(branchName),
			Namespace:       p.namespace,
			OwnerReferences: ownerRefs,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"password": []byte(password),
		},
	}
	err := p.client.Create(ctx, secret)
	if errors.IsAlreadyExists(err) {
		return nil
	}
	return err
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

// svcName は Service 名を生成する。namespace 内の既存 Service との衝突を避けるためプレフィックスを付与する。
func svcName(branchName string) string { return fmt.Sprintf("branchdb-svc-%s", branchName) }

// buildOwnerRefs は domain.OwnerRef から metav1.OwnerReference のスライスを構築する。
// owner が nil の場合は nil を返す。
func buildOwnerRefs(owner *domain.OwnerRef) []metav1.OwnerReference {
	if owner == nil {
		return nil
	}
	t := true
	return []metav1.OwnerReference{
		{
			APIVersion:         owner.APIVersion,
			Kind:               owner.Kind,
			Name:               owner.Name,
			UID:                types.UID(owner.UID),
			BlockOwnerDeletion: &t,
			Controller:         &t,
		},
	}
}

func (p *Provider) createPV(ctx context.Context, branchName string, vol domain.VolumeInfo, ownerRefs []metav1.OwnerReference) error {
	storageClass := ""
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: pvName(branchName), OwnerReferences: ownerRefs},
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

func (p *Provider) createPVC(ctx context.Context, branchName string, ownerRefs []metav1.OwnerReference) error {
	storageClass := ""
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: pvcName(branchName), Namespace: p.namespace, OwnerReferences: ownerRefs},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			StorageClassName: &storageClass,
			VolumeName:       pvName(branchName),
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

func (p *Provider) createConfigMap(ctx context.Context, branchName string, cfg dbConfig, ownerRefs []metav1.OwnerReference) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: cmName(branchName), Namespace: p.namespace, OwnerReferences: ownerRefs},
		Data:       map[string]string{cfg.extraConfigKey: cfg.extraConfig},
	}
	err := p.client.Create(ctx, cm)
	if errors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func (p *Provider) createPod(ctx context.Context, branchName, image string, cfg dbConfig, ownerRefs []metav1.OwnerReference) error {
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

	if cfg.extraConfig != "" {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      cfgVolName,
			MountPath: cfg.extraConfigMountPath,
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

	allowPrivEsc := false
	spec := corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name:         "db",
				Image:        image,
				Args:         cfg.containerArgs,
				Env:          cfg.containerEnv,
				VolumeMounts: volumeMounts,
				ReadinessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						Exec: &corev1.ExecAction{Command: cfg.readinessCmd},
					},
					InitialDelaySeconds: initialDelay,
					PeriodSeconds:       period,
				},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse(defaultCPURequest),
						corev1.ResourceMemory: resource.MustParse(defaultMemoryRequest),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse(defaultCPULimit),
						corev1.ResourceMemory: resource.MustParse(defaultMemoryLimit),
					},
				},
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: &allowPrivEsc,
					Capabilities: &corev1.Capabilities{
						Drop: []corev1.Capability{"ALL"},
					},
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
				Name:            "fix-permissions",
				Image:           "busybox:1.36",
				Command:         []string{"sh", "-c", fmt.Sprintf("chown -R %d:%d %s", uid, uid, cfg.dataDir)},
				SecurityContext: &corev1.SecurityContext{RunAsUser: &rootUID},
				VolumeMounts: []corev1.VolumeMount{
					{Name: dataVolName, MountPath: cfg.dataDir},
				},
			},
		}
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            podName(branchName),
			Namespace:       p.namespace,
			Labels:          map[string]string{"branchdb-branch": branchName},
			OwnerReferences: ownerRefs,
		},
		Spec: spec,
	}
	err := p.client.Create(ctx, pod)
	if errors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func (p *Provider) createService(ctx context.Context, branchName string, port int32, ownerRefs []metav1.OwnerReference) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: svcName(branchName), Namespace: p.namespace, OwnerReferences: ownerRefs},
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
	if err := p.client.Get(ctx, types.NamespacedName{Name: svcName(branchName), Namespace: p.namespace}, &svc); err != nil {
		return 0, fmt.Errorf("get service: %w", err)
	}
	for _, port := range svc.Spec.Ports {
		if port.NodePort > 0 {
			return int(port.NodePort), nil
		}
	}
	return 0, fmt.Errorf("NodePort not yet assigned for service %s", branchName)
}
