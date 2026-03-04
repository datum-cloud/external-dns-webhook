package provider

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadZoneSources(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		validate func(t *testing.T, sources []ZoneSourceConfig)
		wantErr  string
	}{
		{
			name: "single source with defaults",
			yaml: `
zoneSources:
  - name: default
`,
			validate: func(t *testing.T, sources []ZoneSourceConfig) {
				require.Len(t, sources, 1)
				assert.Equal(t, "default", sources[0].Name)
				assert.Equal(t, 60*time.Second, sources[0].RefreshInterval)
			},
		},
		{
			name: "multiple sources with full config",
			yaml: `
zoneSources:
  - name: production
    kubeconfig: /etc/prod.kubeconfig
    refreshInterval: 30s
  - name: staging
    kubeconfig: /etc/staging.kubeconfig
    namespace: dns-zones
    refreshInterval: 2m
`,
			validate: func(t *testing.T, sources []ZoneSourceConfig) {
				require.Len(t, sources, 2)

				assert.Equal(t, "production", sources[0].Name)
				assert.Equal(t, "/etc/prod.kubeconfig", sources[0].Kubeconfig)
				assert.Equal(t, 30*time.Second, sources[0].RefreshInterval)

				assert.Equal(t, "staging", sources[1].Name)
				assert.Equal(t, "dns-zones", sources[1].Namespace)
				assert.Equal(t, 2*time.Minute, sources[1].RefreshInterval)
			},
		},
		{
			name: "label selector source",
			yaml: `
zoneSources:
  - name: labeled
    namespaceLabelSelector: "dns=enabled"
`,
			validate: func(t *testing.T, sources []ZoneSourceConfig) {
				require.Len(t, sources, 1)
				assert.Equal(t, "dns=enabled", sources[0].NamespaceLabelSelector)
				assert.Empty(t, sources[0].Namespace)
			},
		},
		{
			name: "empty file returns nil sources",
			yaml: ``,
			validate: func(t *testing.T, sources []ZoneSourceConfig) {
				assert.Empty(t, sources)
			},
		},
		{
			name: "no zoneSources key returns nil",
			yaml: `
ownerID: test
logLevel: debug
`,
			validate: func(t *testing.T, sources []ZoneSourceConfig) {
				assert.Empty(t, sources)
			},
		},
		{
			name: "source missing name",
			yaml: `
zoneSources:
  - kubeconfig: /etc/foo.kubeconfig
`,
			wantErr: "zoneSources[0]: name is required",
		},
		{
			name: "refresh interval defaults when omitted",
			yaml: `
zoneSources:
  - name: no-interval
`,
			validate: func(t *testing.T, sources []ZoneSourceConfig) {
				assert.Equal(t, 60*time.Second, sources[0].RefreshInterval)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tt.yaml), 0o644))

			sources, err := LoadZoneSources(path)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, sources)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, 8888, cfg.Port)
	assert.Equal(t, "0.0.0.0", cfg.BindAddress)
	assert.Equal(t, 8080, cfg.MetricsPort)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.False(t, cfg.DryRun)
	assert.Empty(t, cfg.OwnerID)
	assert.Empty(t, cfg.ZoneSources)
}
