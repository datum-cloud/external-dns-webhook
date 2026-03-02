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
	client       client.Client
	config       *Config
	ownerID      string
	zoneWatcher  *ZoneWatcher
	recordSetMgr *RecordSetManager
	logger       *log.Logger
}

var _ extdnsprovider.Provider = &Provider{}

// NewProvider creates a new Datum DNS provider.
func NewProvider(k8sClient client.Client, cfg *Config, ownerID string) (*Provider, error) {
	if ownerID == "" {
		return nil, fmt.Errorf("ownerID is required")
	}

	logger := log.StandardLogger()

	p := &Provider{
		client:       k8sClient,
		config:       cfg,
		ownerID:      ownerID,
		logger:       logger,
		zoneWatcher:  NewZoneWatcher(k8sClient, cfg, logger),
		recordSetMgr: NewRecordSetManager(k8sClient, cfg, logger),
	}

	log.Infof("Datum DNS provider initialized (owner: %s, namespace: %s, dry-run: %v)", ownerID, cfg.Namespace, cfg.DryRun)
	return p, nil
}

// Start initializes the provider by starting the zone watcher.
func (p *Provider) Start(ctx context.Context) error {
	return p.zoneWatcher.Start(ctx)
}

// Records returns the current DNS records from DNSRecordSet resources.
// This method retrieves all DNSRecordSet resources and converts them to ExternalDNS endpoints.
func (p *Provider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	p.logger.Debug("Fetching DNS records from Datum DNS operator")

	// Refresh zone cache to ensure we have latest zones
	if err := p.zoneWatcher.Refresh(ctx); err != nil {
		return nil, fmt.Errorf("failed to refresh zone cache: %w", err)
	}

	// List all DNSRecordSet resources owned by this provider
	recordSets, err := p.recordSetMgr.List(ctx, p.ownerID)
	if err != nil {
		return nil, fmt.Errorf("failed to list DNS record sets: %w", err)
	}

	p.logger.Debugf("Found %d DNS record sets", len(recordSets))

	var allEndpoints []*endpoint.Endpoint

	for _, rs := range recordSets {
		// Get the zone for this record set
		zone, err := p.zoneWatcher.GetZoneForDomain(rs.Spec.DNSZoneRef.Name)
		if err != nil {
			// Try to get zone by name from the same namespace
			var zoneObj dnsv1alpha1.DNSZone
			if err := p.client.Get(ctx, client.ObjectKey{Name: rs.Spec.DNSZoneRef.Name, Namespace: rs.Namespace}, &zoneObj); err != nil {
				p.logger.Warnf("Failed to get zone %s for record set %s/%s: %v", rs.Spec.DNSZoneRef.Name, rs.Namespace, rs.Name, err)
				RecordTranslationError("zone_not_found")
				continue
			}
			zone = &zoneObj
		}

		// Convert DNSRecordSet to endpoints
		endpoints, err := DNSRecordSetToEndpoints(rs, zone)
		if err != nil {
			p.logger.Errorf("Failed to convert record set %s/%s to endpoints: %v", rs.Namespace, rs.Name, err)
			RecordTranslationError("recordset_to_endpoint")
			continue
		}

		allEndpoints = append(allEndpoints, endpoints...)
	}

	p.logger.Debugf("Returning %d endpoints", len(allEndpoints))
	return allEndpoints, nil
}

// ApplyChanges applies the given changes to DNS records.
// This method creates, updates, or deletes DNSRecordSet resources to match the desired state.
func (p *Provider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	p.logger.Debugf("Applying changes: %d creates, %d updates, %d deletes",
		len(changes.Create), len(changes.UpdateNew), len(changes.Delete))

	// Process creates
	for _, ep := range changes.Create {
		if err := p.createRecord(ctx, ep); err != nil {
			p.logger.Errorf("Failed to create record for %s: %v", ep.DNSName, err)
			// Continue processing other changes
		}
	}

	// Process updates
	for _, ep := range changes.UpdateNew {
		if err := p.updateRecord(ctx, ep); err != nil {
			p.logger.Errorf("Failed to update record for %s: %v", ep.DNSName, err)
			// Continue processing other changes
		}
	}

	// Process deletes
	for _, ep := range changes.Delete {
		if err := p.deleteRecord(ctx, ep); err != nil {
			p.logger.Errorf("Failed to delete record for %s: %v", ep.DNSName, err)
			// Continue processing other changes
		}
	}

	return nil
}

// createRecord creates a new DNSRecordSet for the given endpoint.
func (p *Provider) createRecord(ctx context.Context, ep *endpoint.Endpoint) error {
	p.logger.Debugf("Creating record: %s (%s)", ep.DNSName, ep.RecordType)

	// Find the zone for this endpoint
	zone, err := p.zoneWatcher.GetZoneForDomain(ep.DNSName)
	if err != nil {
		RecordTranslationError("zone_not_found")
		return fmt.Errorf("no zone found for domain %s: %w", ep.DNSName, err)
	}

	// Convert endpoint to DNSRecordSet
	rs, err := EndpointToDNSRecordSet(ep, zone, p.ownerID)
	if err != nil {
		RecordTranslationError("endpoint_to_recordset")
		return fmt.Errorf("failed to convert endpoint to record set: %w", err)
	}

	// Create the record set
	return p.recordSetMgr.Create(ctx, rs)
}

// updateRecord updates an existing DNSRecordSet for the given endpoint.
func (p *Provider) updateRecord(ctx context.Context, ep *endpoint.Endpoint) error {
	p.logger.Debugf("Updating record: %s (%s)", ep.DNSName, ep.RecordType)

	// Find the zone for this endpoint
	zone, err := p.zoneWatcher.GetZoneForDomain(ep.DNSName)
	if err != nil {
		RecordTranslationError("zone_not_found")
		return fmt.Errorf("no zone found for domain %s: %w", ep.DNSName, err)
	}

	// Convert endpoint to DNSRecordSet
	rs, err := EndpointToDNSRecordSet(ep, zone, p.ownerID)
	if err != nil {
		RecordTranslationError("endpoint_to_recordset")
		return fmt.Errorf("failed to convert endpoint to record set: %w", err)
	}

	// Try to get existing record to preserve metadata
	existing, err := p.recordSetMgr.Get(ctx, rs.Name, rs.Namespace)
	if err != nil {
		// If it doesn't exist, create it instead
		p.logger.Debugf("Record set %s/%s not found, creating instead", rs.Namespace, rs.Name)
		return p.recordSetMgr.Create(ctx, rs)
	}

	// Check for ownership conflicts
	if existingOwner, ok := existing.Labels[LabelOwner]; ok && existingOwner != p.ownerID {
		p.logger.Warnf("Ownership conflict: record set %s/%s owned by %s, attempted update by %s",
			rs.Namespace, rs.Name, existingOwner, p.ownerID)
		RecordOwnershipConflict()
		return fmt.Errorf("ownership conflict: record set owned by %s", existingOwner)
	}

	// Preserve resource version for update
	rs.ResourceVersion = existing.ResourceVersion
	rs.UID = existing.UID

	// Update the record set
	return p.recordSetMgr.Update(ctx, rs)
}

// deleteRecord deletes the DNSRecordSet for the given endpoint.
func (p *Provider) deleteRecord(ctx context.Context, ep *endpoint.Endpoint) error {
	p.logger.Debugf("Deleting record: %s (%s)", ep.DNSName, ep.RecordType)

	// Find the zone for this endpoint
	zone, err := p.zoneWatcher.GetZoneForDomain(ep.DNSName)
	if err != nil {
		RecordTranslationError("zone_not_found")
		return fmt.Errorf("no zone found for domain %s: %w", ep.DNSName, err)
	}

	// Generate the record set name
	name := GenerateRecordSetName(ep.DNSName, ep.RecordType, ep.SetIdentifier)

	// Delete the record set
	return p.recordSetMgr.Delete(ctx, name, zone.Namespace)
}

// AdjustEndpoints modifies endpoints before they are processed by the plan.
// This can be used to add provider-specific labels or modify endpoint properties.
func (p *Provider) AdjustEndpoints(endpoints []*endpoint.Endpoint) ([]*endpoint.Endpoint, error) {
	p.logger.Debugf("Adjusting %d endpoints", len(endpoints))

	for _, ep := range endpoints {
		// Normalize DNS names (remove trailing dots)
		ep.DNSName = endpoint.NewEndpoint(ep.DNSName, ep.RecordType).DNSName

		// Add owner label if not present
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
// This determines which DNS names the provider will manage.
func (p *Provider) GetDomainFilter() endpoint.DomainFilterInterface {
	p.logger.Debug("Getting domain filter")

	return p.zoneWatcher.GetDomainFilter()
}
