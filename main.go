package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	dnsv1alpha1 "go.miloapis.com/dns-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.datumapis.com/external-dns-webhook/internal/provider"
)

func main() {
	var (
		ownerID                string
		namespace              string
		namespaceLabelSelector string
		kubeconfig             string
		port                   int
		bindAddress            string
		metricsPort            int
		logLevel               string
		dryRun                 bool
		configPath             string
	)

	flag.StringVar(&ownerID, "owner-id", "", "Owner ID to identify this ExternalDNS instance (required)")
	flag.StringVar(&namespace, "namespace", "", "Specific namespace to watch (empty means all namespaces)")
	flag.StringVar(&namespaceLabelSelector, "namespace-label-selector", "", "Label selector to filter namespaces to watch")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig file (empty uses in-cluster config)")
	flag.IntVar(&port, "port", 8888, "HTTP server port for webhook API")
	flag.StringVar(&bindAddress, "bind-address", "0.0.0.0", "Address to bind the webhook HTTP server")
	flag.IntVar(&metricsPort, "metrics-port", 8080, "Port for Prometheus metrics and health checks")
	flag.StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	flag.BoolVar(&dryRun, "dry-run", false, "Dry-run mode: do not make actual DNS changes")
	flag.StringVar(&configPath, "config", "", "Path to YAML config file for additional zone sources")
	flag.Parse()

	cfg := &provider.Config{
		OwnerID:     ownerID,
		Port:        port,
		BindAddress: bindAddress,
		MetricsPort: metricsPort,
		LogLevel:    logLevel,
		DryRun:      dryRun,
	}

	if configPath != "" {
		zoneSources, err := provider.LoadZoneSources(configPath)
		if err != nil {
			log.Fatalf("Failed to load config: %v", err)
		}
		cfg.ZoneSources = zoneSources
	}

	// If no zone sources from config file, create a default source from flags
	if len(cfg.ZoneSources) == 0 {
		cfg.ZoneSources = []provider.ZoneSourceConfig{{
			Name:                   "default",
			Kubeconfig:             kubeconfig,
			Namespace:              namespace,
			NamespaceLabelSelector: namespaceLabelSelector,
		}}
	}

	if cfg.OwnerID == "" {
		fmt.Fprintln(os.Stderr, "error: --owner-id is required")
		flag.Usage()
		os.Exit(1)
	}

	level, err := log.ParseLevel(cfg.LogLevel)
	if err != nil {
		log.Fatalf("Invalid log level: %v", err)
	}
	log.SetLevel(level)
	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})

	log.Infof("Starting Datum DNS webhook provider (owner: %s, sources: %d, dry-run: %v)",
		cfg.OwnerID, len(cfg.ZoneSources), cfg.DryRun)

	sources, err := buildZoneSources(cfg)
	if err != nil {
		log.Fatalf("Failed to build zone sources: %v", err)
	}

	providerInstance, err := provider.NewProvider(cfg, cfg.OwnerID, sources)
	if err != nil {
		log.Fatalf("Failed to create provider: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := providerInstance.Start(ctx); err != nil {
		log.Fatalf("Failed to start provider: %v", err)
	}

	server := provider.NewServer(providerInstance, cfg)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Infof("Received signal %v, initiating shutdown...", sig)
		cancel()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Errorf("Server shutdown error: %v", err)
		}
	}()

	if err := server.Start(ctx); err != nil {
		log.Fatalf("Server error: %v", err)
	}

	log.Info("Shutdown complete")
}

func buildZoneSources(cfg *provider.Config) ([]*provider.ZoneSource, error) {
	logger := log.StandardLogger()
	sources := make([]*provider.ZoneSource, 0, len(cfg.ZoneSources))

	for _, srcCfg := range cfg.ZoneSources {
		k8sClient, err := createKubernetesClient(srcCfg.Kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create K8s client for source %q: %w", srcCfg.Name, err)
		}
		log.Infof("Zone source %q: K8s client ready (kubeconfig=%q)", srcCfg.Name, srcCfg.Kubeconfig)

		src := provider.NewZoneSource(srcCfg.Name, k8sClient, srcCfg, cfg, logger)
		sources = append(sources, src)
	}

	return sources, nil
}

func createKubernetesClient(kubeconfigPath string) (client.Client, error) {
	var config *rest.Config
	var err error

	if kubeconfigPath != "" {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	} else {
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, err
	}

	runtimeScheme := runtime.NewScheme()
	if err := scheme.AddToScheme(runtimeScheme); err != nil {
		return nil, fmt.Errorf("failed to add core types to scheme: %w", err)
	}
	if err := dnsv1alpha1.AddToScheme(runtimeScheme); err != nil {
		return nil, fmt.Errorf("failed to add Datum DNS types to scheme: %w", err)
	}

	return client.New(config, client.Options{Scheme: runtimeScheme})
}
