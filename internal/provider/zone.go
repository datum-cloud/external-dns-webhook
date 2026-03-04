package provider

import (
	"context"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"
	dnsv1alpha1 "go.miloapis.com/dns-operator/api/v1alpha1"

	"sigs.k8s.io/external-dns/endpoint"
)

// ZoneMatch contains the result of a zone lookup, including the source that owns it.
type ZoneMatch struct {
	Zone   *dnsv1alpha1.DNSZone
	Source *ZoneSource
}

// ZoneRegistry aggregates zones from one or more ZoneSources and provides
// domain-to-zone resolution with routing to the correct control plane.
type ZoneRegistry struct {
	sources []*ZoneSource
	logger  *log.Logger
}

// NewZoneRegistry creates a registry from one or more zone sources.
func NewZoneRegistry(sources []*ZoneSource, logger *log.Logger) *ZoneRegistry {
	return &ZoneRegistry{
		sources: sources,
		logger:  logger,
	}
}

// Start initializes all zone sources (initial discovery + refresh loops).
func (r *ZoneRegistry) Start(ctx context.Context) error {
	r.logger.Infof("Starting zone registry with %d source(s)", len(r.sources))

	for _, src := range r.sources {
		if err := src.Start(ctx); err != nil {
			return fmt.Errorf("failed to start zone source %q: %w", src.Name(), err)
		}
	}
	return nil
}

// GetZoneForDomain finds the best-matching zone across all sources using longest
// suffix match. Returns both the zone and the source that owns it.
func (r *ZoneRegistry) GetZoneForDomain(domain string) (*ZoneMatch, error) {
	domain = strings.TrimSuffix(domain, ".")

	var bestMatch *ZoneMatch
	var bestMatchLen int

	for _, src := range r.sources {
		zones := src.GetZones()

		// Exact match
		if zone, ok := zones[domain]; ok {
			if len(domain) > bestMatchLen {
				bestMatch = &ZoneMatch{Zone: zone, Source: src}
				bestMatchLen = len(domain)
			}
			continue
		}

		// Longest suffix match
		for zoneDomain, zone := range zones {
			if strings.HasSuffix(domain, "."+zoneDomain) || domain == zoneDomain {
				if len(zoneDomain) > bestMatchLen {
					bestMatch = &ZoneMatch{Zone: zone, Source: src}
					bestMatchLen = len(zoneDomain)
				}
			}
		}
	}

	if bestMatch == nil {
		return nil, fmt.Errorf("no zone found for domain: %s", domain)
	}
	return bestMatch, nil
}

// GetDomainFilter builds a domain filter from all zones across all sources.
func (r *ZoneRegistry) GetDomainFilter() endpoint.DomainFilter {
	var domains []string

	for _, src := range r.sources {
		for domain := range src.GetZones() {
			domains = append(domains, domain)
		}
	}

	return endpoint.NewDomainFilter(domains)
}

// ListZones returns all zones across all sources.
func (r *ZoneRegistry) ListZones() []*dnsv1alpha1.DNSZone {
	var zones []*dnsv1alpha1.DNSZone

	for _, src := range r.sources {
		for _, zone := range src.GetZones() {
			zones = append(zones, zone)
		}
	}
	return zones
}

// Refresh forces a refresh of all sources.
func (r *ZoneRegistry) Refresh(ctx context.Context) error {
	for _, src := range r.sources {
		if err := src.Refresh(ctx); err != nil {
			r.logger.WithError(err).Errorf("Failed to refresh source %q", src.Name())
		}
	}
	return nil
}

// Sources returns the underlying zone sources.
func (r *ZoneRegistry) Sources() []*ZoneSource {
	return r.sources
}
