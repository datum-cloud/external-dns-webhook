package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	log "github.com/sirupsen/logrus"
	dnsv1alpha1 "go.miloapis.com/dns-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.datumapis.com/external-dns-webhook/internal/provider"
)

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
)

func init() {
	flag.StringVar(&ownerID, "owner-id", "", "Owner ID to identify this ExternalDNS instance (required)")
	flag.StringVar(&namespace, "namespace", "", "Specific namespace to watch (empty means all namespaces)")
	flag.StringVar(&namespaceLabelSelector, "namespace-label-selector", "", "Label selector to filter namespaces to watch")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig file (empty uses in-cluster config)")
	flag.IntVar(&port, "port", 8888, "HTTP server port for webhook API")
	flag.StringVar(&bindAddress, "bind-address", "0.0.0.0", "Address to bind the webhook HTTP server")
	flag.IntVar(&metricsPort, "metrics-port", 8080, "Port for Prometheus metrics and health checks")
	flag.StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	flag.BoolVar(&dryRun, "dry-run", false, "Dry-run mode: do not make actual DNS changes")
}

func main() {
	flag.Parse()

	// Configure logging
	level, err := log.ParseLevel(logLevel)
	if err != nil {
		log.Fatalf("Invalid log level: %v", err)
	}
	log.SetLevel(level)
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})

	// Validate required flags
	if ownerID == "" {
		log.Fatal("--owner-id is required")
	}

	log.Infof("Starting Datum DNS webhook provider")
	log.Infof("Configuration: owner-id=%s, namespace=%s, label-selector=%s, port=%d, metrics-port=%d, dry-run=%v",
		ownerID, namespace, namespaceLabelSelector, port, metricsPort, dryRun)

	// Create configuration
	config := provider.NewConfig()
	config.OwnerID = ownerID
	config.Namespace = namespace
	config.NamespaceLabelSelector = namespaceLabelSelector
	config.Kubeconfig = kubeconfig
	config.Port = port
	config.BindAddress = bindAddress
	config.MetricsPort = metricsPort
	config.LogLevel = logLevel
	config.DryRun = dryRun

	// Determine namespace watch mode
	if namespace != "" {
		config.WatchMode = provider.NamespaceWatchModeSpecific
	} else if namespaceLabelSelector != "" {
		config.WatchMode = provider.NamespaceWatchModeLabeled
	} else {
		config.WatchMode = provider.NamespaceWatchModeAll
	}

	// Create Kubernetes client
	k8sClient, err := createKubernetesClient(kubeconfig)
	if err != nil {
		log.Fatalf("Failed to create Kubernetes client: %v", err)
	}
	log.Info("Kubernetes client initialized")

	// Create provider
	providerInstance, err := provider.NewProvider(k8sClient, config, ownerID)
	if err != nil {
		log.Fatalf("Failed to create provider: %v", err)
	}

	// Create context for provider startup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start zone watcher
	if err := providerInstance.Start(ctx); err != nil {
		log.Fatalf("Failed to start provider: %v", err)
	}

	// Create and start server
	server := provider.NewServer(providerInstance, config)

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Infof("Received signal %v, initiating shutdown...", sig)
		cancel()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Errorf("Server shutdown error: %v", err)
		}
	}()

	// Start server (blocks until shutdown)
	if err := server.Start(ctx); err != nil {
		log.Fatalf("Server error: %v", err)
	}

	log.Info("Shutdown complete")
}

// createKubernetesClient creates a Kubernetes client using the provided kubeconfig or in-cluster config.
func createKubernetesClient(kubeconfigPath string) (client.Client, error) {
	var config *rest.Config
	var err error

	if kubeconfigPath != "" {
		log.Infof("Using kubeconfig: %s", kubeconfigPath)
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	} else {
		log.Info("Using in-cluster configuration")
		config, err = rest.InClusterConfig()
	}

	if err != nil {
		return nil, err
	}

	// Create a new scheme and register our types
	runtimeScheme := runtime.NewScheme()
	if err := scheme.AddToScheme(runtimeScheme); err != nil {
		return nil, fmt.Errorf("failed to add core types to scheme: %w", err)
	}

	// Register Datum DNS types
	if err := dnsv1alpha1.AddToScheme(runtimeScheme); err != nil {
		return nil, fmt.Errorf("failed to add Datum DNS types to scheme: %w", err)
	}

	// Create a client
	k8sClient, err := client.New(config, client.Options{
		Scheme: runtimeScheme,
	})
	if err != nil {
		return nil, err
	}

	return k8sClient, nil
}
