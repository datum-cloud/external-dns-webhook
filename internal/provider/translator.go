package provider

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"

	dnsv1alpha1 "go.miloapis.com/dns-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/external-dns/endpoint"
)

var (
	// sanitizeRegex removes characters not allowed in Kubernetes resource names
	sanitizeRegex = regexp.MustCompile(`[^a-z0-9-]`)
)

// EndpointToDNSRecordSet converts an ExternalDNS Endpoint to a DNSRecordSet.
func EndpointToDNSRecordSet(ep *endpoint.Endpoint, zone *dnsv1alpha1.DNSZone, ownerID string) (*dnsv1alpha1.DNSRecordSet, error) {
	if ep == nil {
		return nil, fmt.Errorf("endpoint cannot be nil")
	}
	if zone == nil {
		return nil, fmt.Errorf("zone cannot be nil")
	}

	// Get relative name for the record
	relativeName := GetRelativeName(ep.DNSName, zone.Spec.DomainName)

	// Validate record type specific constraints
	if err := validateEndpoint(ep); err != nil {
		return nil, fmt.Errorf("invalid endpoint: %w", err)
	}

	// Convert RRType
	recordType, err := endpointTypeToRRType(ep.RecordType)
	if err != nil {
		return nil, err
	}

	// Build record entries
	records := make([]dnsv1alpha1.RecordEntry, 0, len(ep.Targets))
	ttl := ep.RecordTTL

	// Use default TTL of 300 seconds if not configured
	var ttlValue int64 = 300
	if ttl.IsConfigured() {
		ttlValue = int64(ttl)
	}
	ttlPtr := &ttlValue

	for _, target := range ep.Targets {
		entry := dnsv1alpha1.RecordEntry{
			Name: relativeName,
			TTL:  ttlPtr,
		}

		switch ep.RecordType {
		case endpoint.RecordTypeA:
			entry.A = &dnsv1alpha1.ARecordSpec{Content: target}
		case endpoint.RecordTypeAAAA:
			entry.AAAA = &dnsv1alpha1.AAAARecordSpec{Content: target}
		case endpoint.RecordTypeCNAME:
			entry.CNAME = &dnsv1alpha1.CNAMERecordSpec{Content: target}
		case endpoint.RecordTypeTXT:
			entry.TXT = &dnsv1alpha1.TXTRecordSpec{Content: target}
		default:
			return nil, fmt.Errorf("unsupported record type: %s", ep.RecordType)
		}

		records = append(records, entry)
	}

	// Generate deterministic name
	name := GenerateRecordSetName(ep.DNSName, ep.RecordType, ep.SetIdentifier)

	rs := &dnsv1alpha1.DNSRecordSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: zone.Namespace,
			Labels: map[string]string{
				LabelOwner:      ownerID,
				LabelResource:   ep.DNSName,
				LabelRecordType: ep.RecordType,
				LabelManagedBy:  ManagedByValue,
			},
		},
		Spec: dnsv1alpha1.DNSRecordSetSpec{
			DNSZoneRef: corev1.LocalObjectReference{
				Name: zone.Name,
			},
			RecordType: recordType,
			Records:    records,
		},
	}

	return rs, nil
}

// DNSRecordSetToEndpoints converts a DNSRecordSet to ExternalDNS Endpoints.
func DNSRecordSetToEndpoints(rs *dnsv1alpha1.DNSRecordSet, zone *dnsv1alpha1.DNSZone) ([]*endpoint.Endpoint, error) {
	if rs == nil {
		return nil, fmt.Errorf("recordset cannot be nil")
	}
	if zone == nil {
		return nil, fmt.Errorf("zone cannot be nil")
	}

	// Get owner ID from labels
	ownerID := rs.Labels[LabelOwner]

	// Group records by name (owner)
	recordsByName := make(map[string][]dnsv1alpha1.RecordEntry)
	for _, record := range rs.Spec.Records {
		recordsByName[record.Name] = append(recordsByName[record.Name], record)
	}

	endpoints := make([]*endpoint.Endpoint, 0, len(recordsByName))

	for name, entries := range recordsByName {
		// Construct FQDN
		fqdn := constructFQDN(name, zone.Spec.DomainName)

		// Extract targets and TTL
		targets := make([]string, 0, len(entries))
		var ttl endpoint.TTL

		for _, entry := range entries {
			if entry.TTL != nil && ttl == 0 {
				ttl = endpoint.TTL(*entry.TTL)
			}

			target, err := extractTarget(&entry, string(rs.Spec.RecordType))
			if err != nil {
				return nil, fmt.Errorf("failed to extract target: %w", err)
			}
			targets = append(targets, target)
		}

		// Convert record type
		epRecordType, err := rrTypeToEndpointType(rs.Spec.RecordType)
		if err != nil {
			return nil, err
		}

		ep := endpoint.NewEndpointWithTTL(fqdn, epRecordType, ttl, targets...)
		if ownerID != "" {
			ep.Labels[endpoint.OwnerLabelKey] = ownerID
		}

		endpoints = append(endpoints, ep)
	}

	return endpoints, nil
}

// GenerateRecordSetName creates a deterministic name for a DNSRecordSet.
// Format: <sanitized-name>-<record-type>-<hash>
func GenerateRecordSetName(dnsName, recordType, setIdentifier string) string {
	// Sanitize DNS name for Kubernetes naming
	sanitized := strings.ToLower(dnsName)
	sanitized = strings.ReplaceAll(sanitized, ".", "-")
	sanitized = sanitizeRegex.ReplaceAllString(sanitized, "")

	// Limit length to avoid exceeding Kubernetes name limits
	if len(sanitized) > 200 {
		sanitized = sanitized[:200]
	}

	// Create hash for uniqueness
	hashInput := fmt.Sprintf("%s:%s:%s", dnsName, recordType, setIdentifier)
	hash := sha256.Sum256([]byte(hashInput))
	hashStr := fmt.Sprintf("%x", hash[:4]) // Use first 4 bytes

	recordTypeLC := strings.ToLower(recordType)

	return fmt.Sprintf("%s-%s-%s", sanitized, recordTypeLC, hashStr)
}

// GetRelativeName extracts the relative record name from FQDN given the zone.
func GetRelativeName(fqdn, zoneDomain string) string {
	// Normalize both (remove trailing dots)
	fqdn = strings.TrimSuffix(fqdn, ".")
	zoneDomain = strings.TrimSuffix(zoneDomain, ".")

	// Check if fqdn is the zone apex
	if fqdn == zoneDomain {
		return "@"
	}

	// Extract relative name
	if before, ok := strings.CutSuffix(fqdn, "."+zoneDomain); ok {
		relativeName := before
		return relativeName
	}

	// If it doesn't match the zone, return the FQDN as-is
	// This shouldn't happen in normal operation
	return fqdn
}

// constructFQDN builds the full DNS name from a relative name and zone domain.
func constructFQDN(relativeName, zoneDomain string) string {
	zoneDomain = strings.TrimSuffix(zoneDomain, ".")

	if relativeName == "@" || relativeName == "" {
		return zoneDomain
	}

	return fmt.Sprintf("%s.%s", relativeName, zoneDomain)
}

// validateEndpoint performs record-type specific validation.
func validateEndpoint(ep *endpoint.Endpoint) error {
	if ep.RecordType == endpoint.RecordTypeCNAME && len(ep.Targets) != 1 {
		return fmt.Errorf("CNAME records must have exactly one target, got %d", len(ep.Targets))
	}

	if len(ep.Targets) == 0 {
		return fmt.Errorf("endpoint must have at least one target")
	}

	return nil
}

// extractTarget extracts the target value from a RecordEntry based on record type.
func extractTarget(entry *dnsv1alpha1.RecordEntry, recordType string) (string, error) {
	switch recordType {
	case "A":
		if entry.A == nil {
			return "", fmt.Errorf("A record entry missing A spec")
		}
		return entry.A.Content, nil
	case "AAAA":
		if entry.AAAA == nil {
			return "", fmt.Errorf("AAAA record entry missing AAAA spec")
		}
		return entry.AAAA.Content, nil
	case "CNAME":
		if entry.CNAME == nil {
			return "", fmt.Errorf("CNAME record entry missing CNAME spec")
		}
		return entry.CNAME.Content, nil
	case "TXT":
		if entry.TXT == nil {
			return "", fmt.Errorf("TXT record entry missing TXT spec")
		}
		return entry.TXT.Content, nil
	default:
		return "", fmt.Errorf("unsupported record type: %s", recordType)
	}
}

// endpointTypeToRRType converts ExternalDNS record type to RRType.
func endpointTypeToRRType(epType string) (dnsv1alpha1.RRType, error) {
	switch epType {
	case endpoint.RecordTypeA:
		return dnsv1alpha1.RRTypeA, nil
	case endpoint.RecordTypeAAAA:
		return dnsv1alpha1.RRTypeAAAA, nil
	case endpoint.RecordTypeCNAME:
		return dnsv1alpha1.RRTypeCNAME, nil
	case endpoint.RecordTypeTXT:
		return dnsv1alpha1.RRTypeTXT, nil
	default:
		return "", fmt.Errorf("unsupported endpoint record type: %s", epType)
	}
}

// rrTypeToEndpointType converts RRType to ExternalDNS record type.
func rrTypeToEndpointType(rrType dnsv1alpha1.RRType) (string, error) {
	switch rrType {
	case dnsv1alpha1.RRTypeA:
		return endpoint.RecordTypeA, nil
	case dnsv1alpha1.RRTypeAAAA:
		return endpoint.RecordTypeAAAA, nil
	case dnsv1alpha1.RRTypeCNAME:
		return endpoint.RecordTypeCNAME, nil
	case dnsv1alpha1.RRTypeTXT:
		return endpoint.RecordTypeTXT, nil
	default:
		return "", fmt.Errorf("unsupported RRType: %s", rrType)
	}
}
