package provider

import (
	"context"
	"fmt"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
	dnsv1alpha1 "go.miloapis.com/dns-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/external-dns/endpoint"
)

// ZoneWatcher watches DNSZone resources and maintains a cache.
type ZoneWatcher struct {
	client client.Client
	config *Config
	logger *log.Logger

	// cache maps domainName -> DNSZone
	cache map[string]*dnsv1alpha1.DNSZone
	mu    sync.RWMutex
}

// NewZoneWatcher creates a new ZoneWatcher.
func NewZoneWatcher(client client.Client, config *Config, logger *log.Logger) *ZoneWatcher {
	return &ZoneWatcher{
		client: client,
		config: config,
		logger: logger,
		cache:  make(map[string]*dnsv1alpha1.DNSZone),
	}
}

// Start begins watching DNSZone resources and populates the cache.
func (w *ZoneWatcher) Start(ctx context.Context) error {
	w.logger.Info("Starting zone watcher")

	if err := w.refresh(ctx); err != nil {
		return fmt.Errorf("initial zone refresh failed: %w", err)
	}

	// In a production implementation, you would start a controller here
	// to watch for changes. For now, we just do periodic refreshes.
	go w.refreshLoop(ctx)

	return nil
}

// refreshLoop periodically refreshes the zone cache.
func (w *ZoneWatcher) refreshLoop(ctx context.Context) {
	// TODO: Replace with a proper controller that watches DNSZone resources
	// For now, we'll rely on the refresh being called manually or at startup
	<-ctx.Done()
	w.logger.Info("Zone watcher stopped")
}

// refresh updates the zone cache by listing DNSZone resources.
func (w *ZoneWatcher) refresh(ctx context.Context) error {
	namespaces, err := w.getWatchedNamespaces(ctx)
	if err != nil {
		return fmt.Errorf("failed to get watched namespaces: %w", err)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// Clear cache
	w.cache = make(map[string]*dnsv1alpha1.DNSZone)

	// Track zones per namespace for metrics
	zonesByNamespace := make(map[string]int)

	for _, ns := range namespaces {
		var zones dnsv1alpha1.DNSZoneList
		if err := w.client.List(ctx, &zones, client.InNamespace(ns)); err != nil {
			w.logger.Errorf("Failed to list zones in namespace %s: %v", ns, err)
			continue
		}

		zonesByNamespace[ns] = len(zones.Items)

		for i := range zones.Items {
			zone := &zones.Items[i]
			// Normalize domain name (remove trailing dot if present)
			domainName := strings.TrimSuffix(zone.Spec.DomainName, ".")
			w.cache[domainName] = zone
			w.logger.Debugf("Cached zone: %s (namespace: %s)", domainName, zone.Namespace)
		}
	}

	w.logger.Infof("Zone cache refreshed: %d zones", len(w.cache))

	// Record metrics for zones discovered per namespace
	for ns, count := range zonesByNamespace {
		RecordZonesDiscovered(ns, count)
	}

	return nil
}

// getWatchedNamespaces returns the list of namespaces to watch based on configuration.
func (w *ZoneWatcher) getWatchedNamespaces(ctx context.Context) ([]string, error) {
	switch w.config.WatchMode {
	case NamespaceWatchModeSpecific:
		if w.config.Namespace == "" {
			return nil, fmt.Errorf("specific namespace mode requires namespace to be set")
		}
		return []string{w.config.Namespace}, nil

	case NamespaceWatchModeLabeled:
		if w.config.NamespaceLabelSelector == "" {
			return nil, fmt.Errorf("labeled namespace mode requires label selector to be set")
		}

		var namespaceList corev1.NamespaceList
		labelSelector, err := metav1.ParseToLabelSelector(w.config.NamespaceLabelSelector)
		if err != nil {
			return nil, fmt.Errorf("invalid label selector: %w", err)
		}

		selector, err := metav1.LabelSelectorAsSelector(labelSelector)
		if err != nil {
			return nil, fmt.Errorf("failed to convert label selector: %w", err)
		}

		if err := w.client.List(ctx, &namespaceList, client.MatchingLabelsSelector{Selector: selector}); err != nil {
			return nil, fmt.Errorf("failed to list namespaces: %w", err)
		}

		namespaces := make([]string, 0, len(namespaceList.Items))
		for _, ns := range namespaceList.Items {
			namespaces = append(namespaces, ns.Name)
		}
		return namespaces, nil

	case NamespaceWatchModeAll:
		var namespaceList corev1.NamespaceList
		if err := w.client.List(ctx, &namespaceList); err != nil {
			return nil, fmt.Errorf("failed to list namespaces: %w", err)
		}

		namespaces := make([]string, 0, len(namespaceList.Items))
		for _, ns := range namespaceList.Items {
			namespaces = append(namespaces, ns.Name)
		}
		return namespaces, nil

	default:
		return nil, fmt.Errorf("unknown namespace watch mode: %d", w.config.WatchMode)
	}
}

// GetDomainFilter builds a domain filter from cached zones.
func (w *ZoneWatcher) GetDomainFilter() endpoint.DomainFilter {
	w.mu.RLock()
	defer w.mu.RUnlock()

	domains := make([]string, 0, len(w.cache))
	for domain := range w.cache {
		domains = append(domains, domain)
	}

	return endpoint.NewDomainFilter(domains)
}

// GetZoneForDomain finds the DNSZone that manages the given domain using longest suffix match.
func (w *ZoneWatcher) GetZoneForDomain(domain string) (*dnsv1alpha1.DNSZone, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	// Normalize domain (remove trailing dot)
	domain = strings.TrimSuffix(domain, ".")

	// Try exact match first
	if zone, ok := w.cache[domain]; ok {
		return zone, nil
	}

	// Find longest matching suffix
	var bestMatch *dnsv1alpha1.DNSZone
	var bestMatchLen int

	for zoneDomain, zone := range w.cache {
		if strings.HasSuffix(domain, "."+zoneDomain) || domain == zoneDomain {
			if len(zoneDomain) > bestMatchLen {
				bestMatch = zone
				bestMatchLen = len(zoneDomain)
			}
		}
	}

	if bestMatch == nil {
		return nil, fmt.Errorf("no zone found for domain: %s", domain)
	}

	return bestMatch, nil
}

// ListZones returns all cached zones.
func (w *ZoneWatcher) ListZones() []*dnsv1alpha1.DNSZone {
	w.mu.RLock()
	defer w.mu.RUnlock()

	zones := make([]*dnsv1alpha1.DNSZone, 0, len(w.cache))
	for _, zone := range w.cache {
		zones = append(zones, zone)
	}

	return zones
}

// Refresh forces a refresh of the zone cache.
func (w *ZoneWatcher) Refresh(ctx context.Context) error {
	return w.refresh(ctx)
}
