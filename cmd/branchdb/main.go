// branchdb is the BranchDB K8s API server.
// It exposes a REST API and web console for managing DatabaseBranch CRs.
//
// Environment variables:
//
//	ZFSDB_LISTEN_ADDR     HTTP listen address (default: :8080)
//	ZFSDB_EXTERNAL_HOST   NodePort external hostname/IP (default: localhost)
//	ZFSDB_NAMESPACE       Kubernetes namespace (default: default)
//	ZFSDB_ZFSAGENT_URL    ZFS Agent URL; enables snapshot API when set
//	ZFSDB_ZFSAGENT_TOKEN  ZFS Agent auth token
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/client/config"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/MaSuCcHI/branchdb-operator/api/v1alpha1"
	"github.com/MaSuCcHI/branchdb-operator/internal/infrastructure/zfsagent"
	"github.com/MaSuCcHI/branchdb-operator/internal/interface/api"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	listenAddr := envOr("ZFSDB_LISTEN_ADDR", ":8080")
	externalHost := envOr("ZFSDB_EXTERNAL_HOST", "localhost")
	namespace := envOr("ZFSDB_NAMESPACE", "default")

	scheme := k8sruntime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("register CRD scheme: %w", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("register core scheme: %w", err)
	}
	_ = clientgoscheme.AddToScheme(scheme)

	restCfg, err := ctrlconfig.GetConfig()
	if err != nil {
		return fmt.Errorf("get kubeconfig: %w", err)
	}
	k8sClient, err := k8sclient.New(restCfg, k8sclient.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("create k8s client: %w", err)
	}

	handler := api.NewK8sBranchHandler(k8sClient, externalHost).
		WithNamespace(namespace)

	if url := envOr("ZFSDB_ZFSAGENT_URL", ""); url != "" {
		handler = handler.WithVolumeProvider(
			zfsagent.NewProvider(url, envOr("ZFSDB_ZFSAGENT_TOKEN", "")),
		)
	}

	router := api.NewK8sRouter(handler)

	srv := &http.Server{
		Addr:         listenAddr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	go func() {
		log.Printf("branchdb: listening on %s (namespace=%s, externalHost=%s)", listenAddr, namespace, externalHost)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down...")
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
