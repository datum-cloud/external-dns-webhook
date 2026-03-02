package provider

// NamespaceWatchMode determines how the provider watches namespaces.
type NamespaceWatchMode int

const (
	// NamespaceWatchModeAll watches all namespaces.
	NamespaceWatchModeAll NamespaceWatchMode = iota
	// NamespaceWatchModeSpecific watches only the specified namespace.
	NamespaceWatchModeSpecific
	// NamespaceWatchModeLabeled watches namespaces matching the label selector.
	NamespaceWatchModeLabeled
)

// Config holds the configuration for the Datum DNS webhook provider.
type Config struct {
	// OwnerID identifies this ExternalDNS instance
	OwnerID string

	// Namespace is the specific namespace to watch (if NamespaceWatchMode is Specific).
	Namespace string

	// NamespaceLabelSelector is a label selector for namespaces to watch.
	NamespaceLabelSelector string

	// WatchMode determines which namespaces to watch.
	WatchMode NamespaceWatchMode

	// Kubeconfig is the path to the kubeconfig file.
	// If empty, in-cluster config is used.
	Kubeconfig string

	// Port is the HTTP server port for the webhook API.
	Port int

	// BindAddress is the address to bind the webhook HTTP server.
	BindAddress string

	// MetricsPort is the port for Prometheus metrics.
	MetricsPort int

	// LogLevel is the logging level (debug, info, warn, error).
	LogLevel string

	// DryRun if true, prevents actual changes to DNS records.
	DryRun bool
}

// NewConfig creates a new Config with default values.
func NewConfig() *Config {
	return &Config{
		Port:        8888,
		BindAddress: "0.0.0.0",
		MetricsPort: 8080,
		LogLevel:    "info",
		DryRun:      false,
		WatchMode:   NamespaceWatchModeAll,
	}
}
