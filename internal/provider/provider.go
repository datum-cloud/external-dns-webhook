package provider

import (
	"context"
	"fmt"
	"sync"

	log "github.com/sirupsen/logrus"
	dnsv1alpha1 "go.miloapis.com/dns-operator/api/v1alpha1"

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
// all zone sources. Only records in namespaces with discovered zones are
// returned, ensuring namespace-level filtering is respected.
func (p *Provider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	p.logger.Debug("Fetching DNS records from all zone sources")

	if err := p.registry.Refresh(ctx); err != nil {
		return nil, fmt.Errorf("failed to refresh zone cache: %w", err)
	}

	var allEndpoints []*endpoint.Endpoint

	for _, src := range p.registry.Sources() {
		zoneNamespaces := src.GetZoneNamespaces()

		recordSets, err := src.RecordSets().List(ctx, p.ownerID)
		if err != nil {
			p.logger.WithError(err).Errorf("Failed to list records from source %q", src.Name())
			continue
		}

		for _, rs := range recordSets {
			if !zoneNamespaces[rs.Namespace] {
				p.logger.Debugf("Skipping record %s/%s: namespace not in discovered zone set",
					rs.Namespace, rs.Name)
				continue
			}

			zone := p.resolveZoneForRecordSet(rs, src)
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

// resolveZoneForRecordSet looks up the zone for a record set from the source's
// zone cache using the DNSZoneRef object name and namespace. No API fallback is
// performed — if the zone isn't in the cache, the record is skipped.
func (p *Provider) resolveZoneForRecordSet(rs *dnsv1alpha1.DNSRecordSet, src *ZoneSource) *dnsv1alpha1.DNSZone {
	zone := src.GetZoneByRef(rs.Spec.DNSZoneRef.Name, rs.Namespace)
	if zone == nil {
		p.logger.Warnf("Zone %s not found in cache for record %s/%s",
			rs.Spec.DNSZoneRef.Name, rs.Namespace, rs.Name)
		RecordTranslationError("zone_not_found")
	}
	return zone
}

// IncompleteApplyError reports that the context ended before every change in
// a batch was attempted. Changes that were not attempted are left for the next
// sync.
type IncompleteApplyError struct {
	Attempted int
	Total     int
	Err       error
}

func (e *IncompleteApplyError) Error() string {
	return fmt.Sprintf("attempted %d of %d changes before stopping: %v", e.Attempted, e.Total, e.Err)
}

func (e *IncompleteApplyError) Unwrap() error { return e.Err }

type changeFunc func(context.Context, *endpoint.Endpoint) error

// ApplyChanges applies the given changes to DNS records, routing each operation
// to the zone source that owns the target zone. Changes within a phase run in
// parallel, bounded by the configured concurrency. A delete whose record set is
// recreated in the same batch runs before the creates; every other delete runs
// last. Per-change failures are logged and do not fail the batch. If ctx ends
// first, the remaining changes are skipped and an *IncompleteApplyError is
// returned.
func (p *Provider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	total := len(changes.Create) + len(changes.UpdateNew) + len(changes.Delete)
	p.logger.Debugf("Applying changes: %d creates, %d updates, %d deletes",
		len(changes.Create), len(changes.UpdateNew), len(changes.Delete))

	created := make(map[string]bool, len(changes.Create))
	for _, ep := range changes.Create {
		created[recordSetNameFor(ep)] = true
	}
	var replacedDeletes, deletes []*endpoint.Endpoint
	for _, ep := range changes.Delete {
		if created[recordSetNameFor(ep)] {
			replacedDeletes = append(replacedDeletes, ep)
		} else {
			deletes = append(deletes, ep)
		}
	}

	phases := []struct {
		op    string
		eps   []*endpoint.Endpoint
		apply changeFunc
	}{
		{"delete", replacedDeletes, p.deleteRecord},
		{"create", changes.Create, p.createRecord},
		{"update", changes.UpdateNew, p.updateRecord},
		{"delete", deletes, p.deleteRecord},
	}

	attempted := 0
	for _, phase := range phases {
		attempted += p.applyPhase(ctx, phase.op, phase.eps, phase.apply)
		if err := ctx.Err(); err != nil {
			return &IncompleteApplyError{Attempted: attempted, Total: total, Err: err}
		}
	}
	return nil
}

func (p *Provider) applyPhase(ctx context.Context, op string, eps []*endpoint.Endpoint, apply changeFunc) int {
	sem := make(chan struct{}, p.config.EffectiveApplyConcurrency())
	var wg sync.WaitGroup
	attempted := 0

	for _, ep := range eps {
		select {
		case <-ctx.Done():
		case sem <- struct{}{}:
		}
		if ctx.Err() != nil {
			break
		}
		attempted++
		wg.Add(1)
		go func(ep *endpoint.Endpoint) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := p.applyOne(ctx, apply, ep); err != nil {
				p.logger.Errorf("Failed to %s record for %s: %v", op, ep.DNSName, err)
			}
		}(ep)
	}

	wg.Wait()
	return attempted
}

func (p *Provider) applyOne(ctx context.Context, apply changeFunc, ep *endpoint.Endpoint) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return apply(ctx, ep)
}

func recordSetNameFor(ep *endpoint.Endpoint) string {
	return GenerateRecordSetName(ep.DNSName, ep.RecordType, ep.SetIdentifier)
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

	return match.Source.RecordSets().Delete(ctx, recordSetNameFor(ep), match.Zone.Namespace)
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
