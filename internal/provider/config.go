package provider

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds the configuration for the Datum DNS webhook provider.
type Config struct {
	OwnerID     string             `yaml:"ownerID"`
	ZoneSources []ZoneSourceConfig `yaml:"zoneSources"`
	Port        int                `yaml:"port"`
	BindAddress string             `yaml:"bindAddress"`
	MetricsPort int                `yaml:"metricsPort"`
	LogLevel    string             `yaml:"logLevel"`
	DryRun      bool               `yaml:"dryRun"`

	// ApplyTimeout bounds how long a single POST /records may spend applying
	// changes. The webhook server's write timeout is derived from it so the
	// handler always answers before the server closes the connection.
	ApplyTimeout time.Duration `yaml:"applyTimeout"`
	// ApplyConcurrency is the number of record changes applied in parallel.
	ApplyConcurrency int `yaml:"applyConcurrency"`
}

const (
	defaultApplyTimeout     = 5 * time.Minute
	defaultApplyConcurrency = 4
	writeTimeoutGrace       = 30 * time.Second
)

// EffectiveApplyTimeout returns ApplyTimeout, or the default when unset.
func (c *Config) EffectiveApplyTimeout() time.Duration {
	if c.ApplyTimeout <= 0 {
		return defaultApplyTimeout
	}
	return c.ApplyTimeout
}

// EffectiveApplyConcurrency returns ApplyConcurrency, or the default when unset.
func (c *Config) EffectiveApplyConcurrency() int {
	if c.ApplyConcurrency <= 0 {
		return defaultApplyConcurrency
	}
	return c.ApplyConcurrency
}

// WebhookWriteTimeout is the webhook server's write timeout. It exceeds the
// apply timeout so a request that runs out of time still gets a response.
func (c *Config) WebhookWriteTimeout() time.Duration {
	return c.EffectiveApplyTimeout() + writeTimeoutGrace
}

// ZoneSourceConfig holds connection and filtering settings for a single
// control plane. Namespace scoping is derived from field presence:
//   - namespace set             → watch only that namespace
//   - namespaceLabelSelector set → watch matching namespaces
//   - neither set               → watch all namespaces
type ZoneSourceConfig struct {
	Name                   string        `yaml:"name"`
	Kubeconfig             string        `yaml:"kubeconfig,omitempty"`
	Namespace              string        `yaml:"namespace,omitempty"`
	NamespaceLabelSelector string        `yaml:"namespaceLabelSelector,omitempty"`
	RefreshInterval        time.Duration `yaml:"refreshInterval,omitempty"`
}

// LoadZoneSources reads a YAML config file and returns the zone source
// definitions from it. This is the primary way to configure multiple zone
// sources (e.g. for multi-cluster setups) alongside CLI flags.
func LoadZoneSources(path string) ([]ZoneSourceConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	for i := range cfg.ZoneSources {
		if cfg.ZoneSources[i].Name == "" {
			return nil, fmt.Errorf("zoneSources[%d]: name is required", i)
		}
		if cfg.ZoneSources[i].RefreshInterval == 0 {
			cfg.ZoneSources[i].RefreshInterval = defaultRefreshInterval
		}
	}

	return cfg.ZoneSources, nil
}

// DefaultConfig returns a Config populated with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Port:        8888,
		BindAddress: "0.0.0.0",
		MetricsPort: 8080,
		LogLevel:    "info",

		ApplyTimeout:     defaultApplyTimeout,
		ApplyConcurrency: defaultApplyConcurrency,
	}
}
