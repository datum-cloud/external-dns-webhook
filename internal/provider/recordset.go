package provider

import (
	"context"
	"fmt"

	log "github.com/sirupsen/logrus"
	dnsv1alpha1 "go.miloapis.com/dns-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// LabelOwner identifies the ExternalDNS instance that owns this record
	LabelOwner = "external-dns.io/owner"
	// LabelResource identifies the DNS name this record is for
	LabelResource = "external-dns.io/resource"
	// LabelRecordType identifies the record type
	LabelRecordType = "external-dns.io/record-type"
	// LabelManagedBy identifies this as managed by the Datum webhook
	LabelManagedBy = "external-dns.io/managed-by"
	// ManagedByValue is the value for the managed-by label
	ManagedByValue = "datum-cloud-webhook"
)

// RecordSetManager handles DNSRecordSet CRUD operations against a single
// control plane's API server.
type RecordSetManager struct {
	client client.Client
	config *Config
	logger *log.Logger
}

// NewRecordSetManager creates a new RecordSetManager.
func NewRecordSetManager(client client.Client, config *Config, logger *log.Logger) *RecordSetManager {
	return &RecordSetManager{
		client: client,
		config: config,
		logger: logger,
	}
}

// List retrieves all DNSRecordSet resources owned by the given owner ID.
// It searches across all namespaces visible to this manager's client.
func (m *RecordSetManager) List(ctx context.Context, ownerID string) ([]*dnsv1alpha1.DNSRecordSet, error) {
	m.logger.Debugf("Listing DNS record sets for owner: %s", ownerID)

	var recordSets dnsv1alpha1.DNSRecordSetList
	listOpts := []client.ListOption{
		client.MatchingLabels{
			LabelOwner:     ownerID,
			LabelManagedBy: ManagedByValue,
		},
	}

	if err := m.client.List(ctx, &recordSets, listOpts...); err != nil {
		return nil, fmt.Errorf("failed to list DNS record sets: %w", err)
	}

	result := make([]*dnsv1alpha1.DNSRecordSet, 0, len(recordSets.Items))
	recordsetsByNamespaceAndType := make(map[string]map[string]int)

	for i := range recordSets.Items {
		rs := &recordSets.Items[i]
		result = append(result, rs)

		ns := rs.Namespace
		if recordsetsByNamespaceAndType[ns] == nil {
			recordsetsByNamespaceAndType[ns] = make(map[string]int)
		}
		recordsetsByNamespaceAndType[ns][string(rs.Spec.RecordType)]++
	}

	m.logger.Debugf("Found %d DNS record sets", len(result))

	for ns, typeMap := range recordsetsByNamespaceAndType {
		for recordType, count := range typeMap {
			RecordRecordsetsManaged(ns, recordType, count)
		}
	}

	return result, nil
}

// Create creates a new DNSRecordSet resource.
func (m *RecordSetManager) Create(ctx context.Context, rs *dnsv1alpha1.DNSRecordSet) error {
	m.logger.Debugf("Creating DNS record set: %s/%s", rs.Namespace, rs.Name)

	if m.config.DryRun {
		m.logger.Infof("[DRY-RUN] Would create DNS record set: %s/%s", rs.Namespace, rs.Name)
		RecordOperation("create", "success")
		return nil
	}

	rs.TypeMeta = metav1.TypeMeta{
		APIVersion: "dns.networking.miloapis.com/v1alpha1",
		Kind:       "DNSRecordSet",
	}

	if err := m.client.Create(ctx, rs); err != nil {
		RecordOperation("create", "error")
		return fmt.Errorf("failed to create DNS record set %s/%s: %w", rs.Namespace, rs.Name, err)
	}

	m.logger.Infof("Created DNS record set: %s/%s", rs.Namespace, rs.Name)
	RecordOperation("create", "success")
	return nil
}

// Update updates an existing DNSRecordSet resource.
func (m *RecordSetManager) Update(ctx context.Context, rs *dnsv1alpha1.DNSRecordSet) error {
	m.logger.Debugf("Updating DNS record set: %s/%s", rs.Namespace, rs.Name)

	if m.config.DryRun {
		m.logger.Infof("[DRY-RUN] Would update DNS record set: %s/%s", rs.Namespace, rs.Name)
		RecordOperation("update", "success")
		return nil
	}

	if err := m.client.Update(ctx, rs); err != nil {
		RecordOperation("update", "error")
		return fmt.Errorf("failed to update DNS record set %s/%s: %w", rs.Namespace, rs.Name, err)
	}

	m.logger.Infof("Updated DNS record set: %s/%s", rs.Namespace, rs.Name)
	RecordOperation("update", "success")
	return nil
}

// Delete deletes a DNSRecordSet resource.
func (m *RecordSetManager) Delete(ctx context.Context, name, namespace string) error {
	m.logger.Debugf("Deleting DNS record set: %s/%s", namespace, name)

	if m.config.DryRun {
		m.logger.Infof("[DRY-RUN] Would delete DNS record set: %s/%s", namespace, name)
		RecordOperation("delete", "success")
		return nil
	}

	rs := &dnsv1alpha1.DNSRecordSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	if err := m.client.Delete(ctx, rs); err != nil {
		RecordOperation("delete", "error")
		return fmt.Errorf("failed to delete DNS record set %s/%s: %w", namespace, name, err)
	}

	m.logger.Infof("Deleted DNS record set: %s/%s", namespace, name)
	RecordOperation("delete", "success")
	return nil
}

// Get retrieves a specific DNSRecordSet resource.
func (m *RecordSetManager) Get(ctx context.Context, name, namespace string) (*dnsv1alpha1.DNSRecordSet, error) {
	m.logger.Debugf("Getting DNS record set: %s/%s", namespace, name)

	var rs dnsv1alpha1.DNSRecordSet
	if err := m.client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &rs); err != nil {
		return nil, fmt.Errorf("failed to get DNS record set %s/%s: %w", namespace, name, err)
	}

	return &rs, nil
}
