// Package main is the entrypoint for the BranchDB Kubernetes Operator.
package main

import (
	"flag"
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	v1alpha1 "github.com/MaSuCcHI/branchdb-operator/api/v1alpha1"
	"github.com/MaSuCcHI/branchdb-operator/internal/infrastructure/k8sdatabase"
	"github.com/MaSuCcHI/branchdb-operator/internal/infrastructure/zfsagent"
	"github.com/MaSuCcHI/branchdb-operator/internal/interface/operator"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var probeAddr string
	var leaderElect bool

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&leaderElect, "leader-elect", false, "Enable leader election for controller manager. "+
		"Enabling this will ensure there is only one active controller manager.")

	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	setupLog := ctrl.Log.WithName("setup")

	// Read configuration from environment variables.
	externalHost := getEnv("ZFSDB_EXTERNAL_HOST", "")
	namespace := getEnv("ZFSDB_NAMESPACE", "branchdb-system")
	zfsAgentURL := getEnv("ZFSAGENT_URL", "")
	zfsAgentToken := getEnv("ZFSAGENT_TOKEN", "")
	// Per-database image overrides（空文字列はデフォルトイメージを使用）
	imageOverrides := map[string]string{
		"mysql":    getEnv("ZFSDB_MYSQL_IMAGE", ""),
		"postgres": getEnv("ZFSDB_POSTGRES_IMAGE", ""),
		"redis":    getEnv("ZFSDB_REDIS_IMAGE", ""),
	}

	if externalHost == "" {
		setupLog.Info("Warning: ZFSDB_EXTERNAL_HOST is not set; external connectivity will use empty host")
	}
	if zfsAgentURL == "" {
		setupLog.Error(fmt.Errorf("ZFSAGENT_URL is required"), "missing volume provider configuration")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         leaderElect,
		LeaderElectionID:       "branchdb.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// VolumeProvider: ZFS clone/snapshot operations are delegated to the ZFS Agent over HTTP.
	// DatabaseProvider: per-branch database runs as a Pod+PVC+Service in the cluster.
	volumeProvider := zfsagent.NewProvider(zfsAgentURL, zfsAgentToken)
	dbProvider := k8sdatabase.NewProvider(mgr.GetClient(), namespace, imageOverrides)

	reconciler := &operator.DatabaseBranchReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		ExternalHost:     externalHost,
		VolumeProvider:   volumeProvider,
		DatabaseProvider: dbProvider,
	}

	if err := reconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DatabaseBranch")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager", "externalHost", externalHost)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
