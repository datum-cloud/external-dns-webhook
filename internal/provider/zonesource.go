package provider

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	dnsv1alpha1 "go.miloapis.com/dns-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const defaultRefreshInterval = 60 * time.Second

// ZoneSource represents a connection to a control plane that manages DNS zones.
// It discovers DNSZone resources, caches them, and provides a K8s client for
// creating/managing DNSRecordSets in that control plane.
type ZoneSource struct {
	name         string
	k8sClient    client.Client
	config       ZoneSourceConfig
	logger       *log.Entry
	recordSetMgr *RecordSetManager

	cache map[string]*dnsv1alpha1.DNSZone
	mu    sync.RWMutex
}

// NewZoneSource creates a ZoneSource from a pre-built client.
func NewZoneSource(name string, k8sClient client.Client, cfg ZoneSourceConfig, providerCfg *Config, logger *log.Logger) *ZoneSource {
	entry := logger.WithField("source", name)

	if cfg.RefreshInterval == 0 {
		cfg.RefreshInterval = defaultRefreshInterval
	}

	return &ZoneSource{
		name:      name,
		k8sClient: k8sClient,
		config:    cfg,
		logger:    entry,
		cache:     make(map[string]*dnsv1alpha1.DNSZone),
		recordSetMgr: &RecordSetManager{
			client: k8sClient,
			config: providerCfg,
			logger: logger,
		},
	}
}

// Start performs an initial zone refresh, then begins the periodic refresh loop.
func (s *ZoneSource) Start(ctx context.Context) error {
	s.logger.Info("Starting zone source")

	if err := s.refresh(ctx); err != nil {
		return fmt.Errorf("initial zone refresh for source %q failed: %w", s.name, err)
	}

	go s.refreshLoop(ctx)
	return nil
}

func (s *ZoneSource) refreshLoop(ctx context.Context) {
	ticker := time.NewTicker(s.config.RefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("Zone source refresh loop stopped")
			return
		case <-ticker.C:
			if err := s.refresh(ctx); err != nil {
				s.logger.WithError(err).Error("Periodic zone refresh failed")
			}
		}
	}
}

func (s *ZoneSource) refresh(ctx context.Context) error {
	namespaces, err := s.getWatchedNamespaces(ctx)
	if err != nil {
		return fmt.Errorf("failed to get watched namespaces: %w", err)
	}

	newCache := make(map[string]*dnsv1alpha1.DNSZone)
	zonesByNamespace := make(map[string]int)

	for _, ns := range namespaces {
		var zones dnsv1alpha1.DNSZoneList
		if err := s.k8sClient.List(ctx, &zones, client.InNamespace(ns)); err != nil {
			s.logger.WithError(err).Errorf("Failed to list zones in namespace %s", ns)
			continue
		}

		zonesByNamespace[ns] = len(zones.Items)

		for i := range zones.Items {
			zone := &zones.Items[i]
			domainName := strings.TrimSuffix(zone.Spec.DomainName, ".")
			newCache[domainName] = zone
			s.logger.Debugf("Discovered zone: %s (namespace: %s)", domainName, zone.Namespace)
		}
	}

	s.mu.Lock()
	s.cache = newCache
	s.mu.Unlock()

	s.logger.Infof("Zone cache refreshed: %d zones", len(newCache))

	for ns, count := range zonesByNamespace {
		RecordZonesDiscovered(ns, count)
	}

	return nil
}

func (s *ZoneSource) getWatchedNamespaces(ctx context.Context) ([]string, error) {
	switch {
	case s.config.Namespace != "":
		return []string{s.config.Namespace}, nil

	case s.config.NamespaceLabelSelector != "":
		labelSelector, err := metav1.ParseToLabelSelector(s.config.NamespaceLabelSelector)
		if err != nil {
			return nil, fmt.Errorf("invalid label selector: %w", err)
		}

		selector, err := metav1.LabelSelectorAsSelector(labelSelector)
		if err != nil {
			return nil, fmt.Errorf("failed to convert label selector: %w", err)
		}

		var namespaceList corev1.NamespaceList
		if err := s.k8sClient.List(ctx, &namespaceList, client.MatchingLabelsSelector{Selector: selector}); err != nil {
			return nil, fmt.Errorf("failed to list namespaces: %w", err)
		}

		namespaces := make([]string, 0, len(namespaceList.Items))
		for _, ns := range namespaceList.Items {
			namespaces = append(namespaces, ns.Name)
		}
		return namespaces, nil

	default:
		var namespaceList corev1.NamespaceList
		if err := s.k8sClient.List(ctx, &namespaceList); err != nil {
			return nil, fmt.Errorf("failed to list namespaces: %w", err)
		}

		namespaces := make([]string, 0, len(namespaceList.Items))
		for _, ns := range namespaceList.Items {
			namespaces = append(namespaces, ns.Name)
		}
		return namespaces, nil
	}
}

// Name returns the human-readable name of this source.
func (s *ZoneSource) Name() string { return s.name }

// Client returns the K8s client for this source's control plane.
func (s *ZoneSource) Client() client.Client { return s.k8sClient }

// RecordSets returns the record set manager for this source.
func (s *ZoneSource) RecordSets() *RecordSetManager { return s.recordSetMgr }

// GetZones returns a snapshot of all cached zones keyed by domain name.
func (s *ZoneSource) GetZones() map[string]*dnsv1alpha1.DNSZone {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make(map[string]*dnsv1alpha1.DNSZone, len(s.cache))
	for k, v := range s.cache {
		out[k] = v
	}
	return out
}

// GetZoneByRef looks up a cached zone by its Kubernetes object name and
// namespace. Returns nil if no matching zone is found in the cache.
func (s *ZoneSource) GetZoneByRef(name, namespace string) *dnsv1alpha1.DNSZone {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, zone := range s.cache {
		if zone.Name == name && zone.Namespace == namespace {
			return zone
		}
	}
	return nil
}

// GetZoneNamespaces returns the set of namespaces that contain discovered
// zones. Only records in these namespaces should be considered managed.
func (s *ZoneSource) GetZoneNamespaces() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ns := make(map[string]bool)
	for _, zone := range s.cache {
		ns[zone.Namespace] = true
	}
	return ns
}

// Refresh forces an immediate zone cache refresh.
func (s *ZoneSource) Refresh(ctx context.Context) error {
	return s.refresh(ctx)
}
