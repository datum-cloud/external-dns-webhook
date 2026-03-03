package provider

import (
	"context"
	"fmt"

	log "github.com/sirupsen/logrus"
	dnsv1alpha1 "go.miloapis.com/dns-operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	extdnsprovider "sigs.k8s.io/external-dns/provider"
)

// Provider implements the external-dns provider interface for Datum DNS.
type Provider struct {
	config   *Config
	ownerID  string
	registry *ZoneRegistry
	logger   *log.Logger
}

var _ extdnsprovider.Provider = &Provider{}

// NewProvider creates a new Datum DNS provider with the given zone sources.
func NewProvider(cfg *Config, ownerID string, sources []*ZoneSource) (*Provider, error) {
	if ownerID == "" {
		return nil, fmt.Errorf("ownerID is required")
	}

	logger := log.StandardLogger()

	p := &Provider{
		config:   cfg,
		ownerID:  ownerID,
		logger:   logger,
		registry: NewZoneRegistry(sources, logger),
	}

	log.Infof("Datum DNS provider initialized (owner: %s, sources: %d, dry-run: %v)",
		ownerID, len(sources), cfg.DryRun)
	return p, nil
}

// Start initializes the provider by starting the zone registry.
func (p *Provider) Start(ctx context.Context) error {
	return p.registry.Start(ctx)
}

// Records returns the current DNS records from DNSRecordSet resources across
// all zone sources.
func (p *Provider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	p.logger.Debug("Fetching DNS records from all zone sources")

	if err := p.registry.Refresh(ctx); err != nil {
		return nil, fmt.Errorf("failed to refresh zone cache: %w", err)
	}

	var allEndpoints []*endpoint.Endpoint

	for _, src := range p.registry.Sources() {
		recordSets, err := src.RecordSets().List(ctx, p.ownerID)
		if err != nil {
			p.logger.WithError(err).Errorf("Failed to list records from source %q", src.Name())
			continue
		}

		for _, rs := range recordSets {
			zone := p.resolveZoneForRecordSet(ctx, rs, src)
			if zone == nil {
				continue
			}

			endpoints, err := DNSRecordSetToEndpoints(rs, zone)
			if err != nil {
				p.logger.Errorf("Failed to convert record set %s/%s to endpoints: %v",
					rs.Namespace, rs.Name, err)
				RecordTranslationError("recordset_to_endpoint")
				continue
			}
			allEndpoints = append(allEndpoints, endpoints...)
		}
	}

	p.logger.Debugf("Returning %d endpoints", len(allEndpoints))
	return allEndpoints, nil
}

// resolveZoneForRecordSet attempts to find the zone that owns a record set,
// first via the registry cache, then via a direct API lookup.
func (p *Provider) resolveZoneForRecordSet(ctx context.Context, rs *dnsv1alpha1.DNSRecordSet, src *ZoneSource) *dnsv1alpha1.DNSZone {
	match, err := p.registry.GetZoneForDomain(rs.Spec.DNSZoneRef.Name)
	if err == nil {
		return match.Zone
	}

	var zoneObj dnsv1alpha1.DNSZone
	if err := src.Client().Get(ctx, client.ObjectKey{
		Name:      rs.Spec.DNSZoneRef.Name,
		Namespace: rs.Namespace,
	}, &zoneObj); err != nil {
		p.logger.Warnf("Failed to get zone %s for record set %s/%s: %v",
			rs.Spec.DNSZoneRef.Name, rs.Namespace, rs.Name, err)
		RecordTranslationError("zone_not_found")
		return nil
	}
	return &zoneObj
}

// ApplyChanges applies the given changes to DNS records, routing each operation
// to the zone source that owns the target zone.
func (p *Provider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	p.logger.Debugf("Applying changes: %d creates, %d updates, %d deletes",
		len(changes.Create), len(changes.UpdateNew), len(changes.Delete))

	for _, ep := range changes.Create {
		if err := p.createRecord(ctx, ep); err != nil {
			p.logger.Errorf("Failed to create record for %s: %v", ep.DNSName, err)
		}
	}

	for _, ep := range changes.UpdateNew {
		if err := p.updateRecord(ctx, ep); err != nil {
			p.logger.Errorf("Failed to update record for %s: %v", ep.DNSName, err)
		}
	}

	for _, ep := range changes.Delete {
		if err := p.deleteRecord(ctx, ep); err != nil {
			p.logger.Errorf("Failed to delete record for %s: %v", ep.DNSName, err)
		}
	}

	return nil
}

func (p *Provider) createRecord(ctx context.Context, ep *endpoint.Endpoint) error {
	p.logger.Debugf("Creating record: %s (%s)", ep.DNSName, ep.RecordType)

	match, err := p.registry.GetZoneForDomain(ep.DNSName)
	if err != nil {
		RecordTranslationError("zone_not_found")
		return fmt.Errorf("no zone found for domain %s: %w", ep.DNSName, err)
	}

	rs, err := EndpointToDNSRecordSet(ep, match.Zone, p.ownerID)
	if err != nil {
		RecordTranslationError("endpoint_to_recordset")
		return fmt.Errorf("failed to convert endpoint to record set: %w", err)
	}

	return match.Source.RecordSets().Create(ctx, rs)
}

func (p *Provider) updateRecord(ctx context.Context, ep *endpoint.Endpoint) error {
	p.logger.Debugf("Updating record: %s (%s)", ep.DNSName, ep.RecordType)

	match, err := p.registry.GetZoneForDomain(ep.DNSName)
	if err != nil {
		RecordTranslationError("zone_not_found")
		return fmt.Errorf("no zone found for domain %s: %w", ep.DNSName, err)
	}

	rs, err := EndpointToDNSRecordSet(ep, match.Zone, p.ownerID)
	if err != nil {
		RecordTranslationError("endpoint_to_recordset")
		return fmt.Errorf("failed to convert endpoint to record set: %w", err)
	}

	mgr := match.Source.RecordSets()

	existing, err := mgr.Get(ctx, rs.Name, rs.Namespace)
	if err != nil {
		p.logger.Debugf("Record set %s/%s not found, creating instead", rs.Namespace, rs.Name)
		return mgr.Create(ctx, rs)
	}

	if existingOwner, ok := existing.Labels[LabelOwner]; ok && existingOwner != p.ownerID {
		p.logger.Warnf("Ownership conflict: record set %s/%s owned by %s, attempted update by %s",
			rs.Namespace, rs.Name, existingOwner, p.ownerID)
		RecordOwnershipConflict()
		return fmt.Errorf("ownership conflict: record set owned by %s", existingOwner)
	}

	rs.ResourceVersion = existing.ResourceVersion
	rs.UID = existing.UID

	return mgr.Update(ctx, rs)
}

func (p *Provider) deleteRecord(ctx context.Context, ep *endpoint.Endpoint) error {
	p.logger.Debugf("Deleting record: %s (%s)", ep.DNSName, ep.RecordType)

	match, err := p.registry.GetZoneForDomain(ep.DNSName)
	if err != nil {
		RecordTranslationError("zone_not_found")
		return fmt.Errorf("no zone found for domain %s: %w", ep.DNSName, err)
	}

	name := GenerateRecordSetName(ep.DNSName, ep.RecordType, ep.SetIdentifier)
	return match.Source.RecordSets().Delete(ctx, name, match.Zone.Namespace)
}

// AdjustEndpoints modifies endpoints before they are processed by the plan.
func (p *Provider) AdjustEndpoints(endpoints []*endpoint.Endpoint) ([]*endpoint.Endpoint, error) {
	p.logger.Debugf("Adjusting %d endpoints", len(endpoints))

	for _, ep := range endpoints {
		ep.DNSName = endpoint.NewEndpoint(ep.DNSName, ep.RecordType).DNSName

		if ep.Labels == nil {
			ep.Labels = make(map[string]string)
		}
		if _, ok := ep.Labels[endpoint.OwnerLabelKey]; !ok {
			ep.Labels[endpoint.OwnerLabelKey] = p.ownerID
		}
	}

	return endpoints, nil
}

// GetDomainFilter returns the domain filter for this provider.
func (p *Provider) GetDomainFilter() endpoint.DomainFilterInterface {
	p.logger.Debug("Getting domain filter")
	return p.registry.GetDomainFilter()
}

// Registry returns the underlying zone registry (used by the server for readiness).
func (p *Provider) Registry() *ZoneRegistry {
	return p.registry
}
