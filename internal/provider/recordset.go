package provider

import (
	"context"
	"fmt"

	log "github.com/sirupsen/logrus"
	dnsv1alpha1 "go.miloapis.com/dns-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
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

// RecordSetManager handles DNSRecordSet CRUD operations.
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
func (m *RecordSetManager) List(ctx context.Context, ownerID string) ([]*dnsv1alpha1.DNSRecordSet, error) {
	m.logger.Debugf("Listing DNS record sets for owner: %s", ownerID)

	// Get namespaces to search based on configuration
	namespaces, err := m.getNamespaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get namespaces: %w", err)
	}

	var allRecordSets []*dnsv1alpha1.DNSRecordSet

	// Track recordsets per namespace and type for metrics
	recordsetsByNamespaceAndType := make(map[string]map[string]int)

	for _, ns := range namespaces {
		var recordSets dnsv1alpha1.DNSRecordSetList
		listOpts := []client.ListOption{
			client.InNamespace(ns),
			client.MatchingLabels{
				LabelOwner:     ownerID,
				LabelManagedBy: ManagedByValue,
			},
		}

		if err := m.client.List(ctx, &recordSets, listOpts...); err != nil {
			m.logger.Errorf("Failed to list record sets in namespace %s: %v", ns, err)
			continue
		}

		// Initialize namespace map if needed
		if recordsetsByNamespaceAndType[ns] == nil {
			recordsetsByNamespaceAndType[ns] = make(map[string]int)
		}

		for i := range recordSets.Items {
			rs := &recordSets.Items[i]
			allRecordSets = append(allRecordSets, rs)

			// Count by record type
			recordType := string(rs.Spec.RecordType)
			recordsetsByNamespaceAndType[ns][recordType]++
		}
	}

	m.logger.Debugf("Found %d DNS record sets", len(allRecordSets))

	// Record metrics for recordsets managed per namespace and type
	for ns, typeMap := range recordsetsByNamespaceAndType {
		for recordType, count := range typeMap {
			RecordRecordsetsManaged(ns, recordType, count)
		}
	}

	return allRecordSets, nil
}

// Create creates a new DNSRecordSet resource.
func (m *RecordSetManager) Create(ctx context.Context, rs *dnsv1alpha1.DNSRecordSet) error {
	m.logger.Debugf("Creating DNS record set: %s/%s", rs.Namespace, rs.Name)

	if m.config.DryRun {
		m.logger.Infof("[DRY-RUN] Would create DNS record set: %s/%s", rs.Namespace, rs.Name)
		RecordOperation("create", "success")
		return nil
	}

	// Set TypeMeta
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

// getNamespaces returns the list of namespaces to search based on configuration.
func (m *RecordSetManager) getNamespaces(ctx context.Context) ([]string, error) {
	switch m.config.WatchMode {
	case NamespaceWatchModeSpecific:
		if m.config.Namespace == "" {
			return nil, fmt.Errorf("specific namespace mode requires namespace to be set")
		}
		return []string{m.config.Namespace}, nil

	case NamespaceWatchModeLabeled:
		if m.config.NamespaceLabelSelector == "" {
			return nil, fmt.Errorf("labeled namespace mode requires label selector to be set")
		}

		// Parse and use label selector
		labelSelector, err := metav1.ParseToLabelSelector(m.config.NamespaceLabelSelector)
		if err != nil {
			return nil, fmt.Errorf("invalid label selector: %w", err)
		}

		selector, err := metav1.LabelSelectorAsSelector(labelSelector)
		if err != nil {
			return nil, fmt.Errorf("failed to convert label selector: %w", err)
		}

		var namespaceList corev1.NamespaceList
		if err := m.client.List(ctx, &namespaceList, client.MatchingLabelsSelector{Selector: selector}); err != nil {
			return nil, fmt.Errorf("failed to list namespaces: %w", err)
		}

		namespaces := make([]string, 0, len(namespaceList.Items))
		for _, ns := range namespaceList.Items {
			namespaces = append(namespaces, ns.Name)
		}
		return namespaces, nil

	case NamespaceWatchModeAll:
		var namespaceList corev1.NamespaceList
		if err := m.client.List(ctx, &namespaceList); err != nil {
			return nil, fmt.Errorf("failed to list namespaces: %w", err)
		}

		namespaces := make([]string, 0, len(namespaceList.Items))
		for _, ns := range namespaceList.Items {
			namespaces = append(namespaces, ns.Name)
		}
		return namespaces, nil

	default:
		return nil, fmt.Errorf("unknown namespace watch mode: %d", m.config.WatchMode)
	}
}
