package provider

import (
	"context"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	dnsv1alpha1 "go.miloapis.com/dns-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
)

func setupProviderTest(t *testing.T, objects ...runtime.Object) (*Provider, client.Client) {
	scheme := runtime.NewScheme()
	err := dnsv1alpha1.AddToScheme(scheme)
	require.NoError(t, err)
	err = corev1.AddToScheme(scheme)
	require.NoError(t, err)

	// Add default namespace if not in objects
	hasNamespace := false
	for _, obj := range objects {
		if _, ok := obj.(*corev1.Namespace); ok {
			hasNamespace = true
			break
		}
	}
	if !hasNamespace {
		objects = append(objects, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default",
			},
		})
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(objects...).
		Build()

	config := &Config{
		WatchMode: NamespaceWatchModeAll,
		DryRun:    false,
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	provider := &Provider{
		client:       fakeClient,
		config:       config,
		ownerID:      "test-owner",
		logger:       logger,
		zoneWatcher:  NewZoneWatcher(fakeClient, config, logger),
		recordSetMgr: NewRecordSetManager(fakeClient, config, logger),
	}

	return provider, fakeClient
}

func TestProvider_Records(t *testing.T) {
	ttl := int64(300)

	tests := []struct {
		name      string
		objects   []runtime.Object
		wantCount int
		validate  func(t *testing.T, endpoints []*endpoint.Endpoint)
	}{
		{
			name:      "returns empty when no DNSRecordSets exist",
			objects:   []runtime.Object{},
			wantCount: 0,
		},
		{
			name: "converts DNSRecordSets to Endpoints correctly",
			objects: []runtime.Object{
				&dnsv1alpha1.DNSZone{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-com",
						Namespace: "default",
					},
					Spec: dnsv1alpha1.DNSZoneSpec{
						DomainName: "example.com",
					},
				},
				&dnsv1alpha1.DNSRecordSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "app-a-12345678",
						Namespace: "default",
						Labels: map[string]string{
							LabelOwner:     "test-owner",
							LabelManagedBy: ManagedByValue,
						},
					},
					Spec: dnsv1alpha1.DNSRecordSetSpec{
						DNSZoneRef: corev1.LocalObjectReference{
							Name: "example-com",
						},
						RecordType: dnsv1alpha1.RRTypeA,
						Records: []dnsv1alpha1.RecordEntry{
							{
								Name: "app",
								TTL:  &ttl,
								A:    &dnsv1alpha1.ARecordSpec{Content: "192.0.2.1"},
							},
						},
					},
				},
			},
			wantCount: 1,
			validate: func(t *testing.T, endpoints []*endpoint.Endpoint) {
				ep := endpoints[0]
				assert.Equal(t, "app.example.com", ep.DNSName)
				assert.Equal(t, endpoint.RecordTypeA, ep.RecordType)
				assert.Equal(t, endpoint.Targets{"192.0.2.1"}, ep.Targets)
				assert.Equal(t, endpoint.TTL(300), ep.RecordTTL)
			},
		},
		{
			name: "handles multiple record types",
			objects: []runtime.Object{
				&dnsv1alpha1.DNSZone{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-com",
						Namespace: "default",
					},
					Spec: dnsv1alpha1.DNSZoneSpec{
						DomainName: "example.com",
					},
				},
				&dnsv1alpha1.DNSRecordSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "app-a-12345678",
						Namespace: "default",
						Labels: map[string]string{
							LabelOwner:     "test-owner",
							LabelManagedBy: ManagedByValue,
						},
					},
					Spec: dnsv1alpha1.DNSRecordSetSpec{
						DNSZoneRef: corev1.LocalObjectReference{
							Name: "example-com",
						},
						RecordType: dnsv1alpha1.RRTypeA,
						Records: []dnsv1alpha1.RecordEntry{
							{
								Name: "app",
								A:    &dnsv1alpha1.ARecordSpec{Content: "192.0.2.1"},
							},
						},
					},
				},
				&dnsv1alpha1.DNSRecordSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "www-cname-87654321",
						Namespace: "default",
						Labels: map[string]string{
							LabelOwner:     "test-owner",
							LabelManagedBy: ManagedByValue,
						},
					},
					Spec: dnsv1alpha1.DNSRecordSetSpec{
						DNSZoneRef: corev1.LocalObjectReference{
							Name: "example-com",
						},
						RecordType: dnsv1alpha1.RRTypeCNAME,
						Records: []dnsv1alpha1.RecordEntry{
							{
								Name:  "www",
								CNAME: &dnsv1alpha1.CNAMERecordSpec{Content: "app.example.com"},
							},
						},
					},
				},
			},
			wantCount: 2,
			validate: func(t *testing.T, endpoints []*endpoint.Endpoint) {
				// Check we have both record types
				recordTypes := make(map[string]bool)
				for _, ep := range endpoints {
					recordTypes[ep.RecordType] = true
				}
				assert.True(t, recordTypes[endpoint.RecordTypeA])
				assert.True(t, recordTypes[endpoint.RecordTypeCNAME])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			provider, _ := setupProviderTest(t, tt.objects...)

			err := provider.zoneWatcher.Refresh(ctx)
			require.NoError(t, err)

			endpoints, err := provider.Records(ctx)
			require.NoError(t, err)
			assert.Len(t, endpoints, tt.wantCount)

			if tt.validate != nil {
				tt.validate(t, endpoints)
			}
		})
	}
}

func TestProvider_ApplyChanges(t *testing.T) {
	tests := []struct {
		name     string
		objects  []runtime.Object
		changes  *plan.Changes
		validate func(t *testing.T, c client.Client)
	}{
		{
			name: "Create creates new DNSRecordSets",
			objects: []runtime.Object{
				&dnsv1alpha1.DNSZone{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-com",
						Namespace: "default",
					},
					Spec: dnsv1alpha1.DNSZoneSpec{
						DomainName: "example.com",
					},
				},
			},
			changes: &plan.Changes{
				Create: []*endpoint.Endpoint{
					{
						DNSName:    "app.example.com",
						Targets:    endpoint.Targets{"192.0.2.1"},
						RecordType: endpoint.RecordTypeA,
						RecordTTL:  300,
					},
				},
			},
			validate: func(t *testing.T, c client.Client) {
				var recordSets dnsv1alpha1.DNSRecordSetList
				err := c.List(context.Background(), &recordSets, client.InNamespace("default"))
				require.NoError(t, err)
				assert.Len(t, recordSets.Items, 1)
				assert.Equal(t, dnsv1alpha1.RRTypeA, recordSets.Items[0].Spec.RecordType)
				assert.Equal(t, "app", recordSets.Items[0].Spec.Records[0].Name)
			},
		},
		{
			name: "Update updates existing DNSRecordSets",
			objects: []runtime.Object{
				&dnsv1alpha1.DNSZone{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-com",
						Namespace: "default",
					},
					Spec: dnsv1alpha1.DNSZoneSpec{
						DomainName: "example.com",
					},
				},
				&dnsv1alpha1.DNSRecordSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      GenerateRecordSetName("app.example.com", endpoint.RecordTypeA, ""),
						Namespace: "default",
						Labels: map[string]string{
							LabelOwner:     "test-owner",
							LabelManagedBy: ManagedByValue,
						},
					},
					Spec: dnsv1alpha1.DNSRecordSetSpec{
						DNSZoneRef: corev1.LocalObjectReference{
							Name: "example-com",
						},
						RecordType: dnsv1alpha1.RRTypeA,
						Records: []dnsv1alpha1.RecordEntry{
							{
								Name: "app",
								A:    &dnsv1alpha1.ARecordSpec{Content: "192.0.2.1"},
							},
						},
					},
				},
			},
			changes: &plan.Changes{
				UpdateNew: []*endpoint.Endpoint{
					{
						DNSName:    "app.example.com",
						Targets:    endpoint.Targets{"192.0.2.2"},
						RecordType: endpoint.RecordTypeA,
					},
				},
			},
			validate: func(t *testing.T, c client.Client) {
				var rs dnsv1alpha1.DNSRecordSet
				name := GenerateRecordSetName("app.example.com", endpoint.RecordTypeA, "")
				err := c.Get(context.Background(), client.ObjectKey{
					Name:      name,
					Namespace: "default",
				}, &rs)
				require.NoError(t, err)
				assert.Equal(t, "192.0.2.2", rs.Spec.Records[0].A.Content)
			},
		},
		{
			name: "Delete removes DNSRecordSets",
			objects: []runtime.Object{
				&dnsv1alpha1.DNSZone{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-com",
						Namespace: "default",
					},
					Spec: dnsv1alpha1.DNSZoneSpec{
						DomainName: "example.com",
					},
				},
				&dnsv1alpha1.DNSRecordSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      GenerateRecordSetName("app.example.com", endpoint.RecordTypeA, ""),
						Namespace: "default",
						Labels: map[string]string{
							LabelOwner:     "test-owner",
							LabelManagedBy: ManagedByValue,
						},
					},
					Spec: dnsv1alpha1.DNSRecordSetSpec{
						DNSZoneRef: corev1.LocalObjectReference{
							Name: "example-com",
						},
						RecordType: dnsv1alpha1.RRTypeA,
						Records: []dnsv1alpha1.RecordEntry{
							{
								Name: "app",
								A:    &dnsv1alpha1.ARecordSpec{Content: "192.0.2.1"},
							},
						},
					},
				},
			},
			changes: &plan.Changes{
				Delete: []*endpoint.Endpoint{
					{
						DNSName:    "app.example.com",
						Targets:    endpoint.Targets{"192.0.2.1"},
						RecordType: endpoint.RecordTypeA,
					},
				},
			},
			validate: func(t *testing.T, c client.Client) {
				var recordSets dnsv1alpha1.DNSRecordSetList
				err := c.List(context.Background(), &recordSets, client.InNamespace("default"))
				require.NoError(t, err)
				assert.Len(t, recordSets.Items, 0)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			provider, fakeClient := setupProviderTest(t, tt.objects...)

			err := provider.zoneWatcher.Refresh(ctx)
			require.NoError(t, err)

			err = provider.ApplyChanges(ctx, tt.changes)
			require.NoError(t, err)

			if tt.validate != nil {
				tt.validate(t, fakeClient)
			}
		})
	}
}

func TestProvider_AdjustEndpoints(t *testing.T) {
	provider, _ := setupProviderTest(t)

	tests := []struct {
		name      string
		endpoints []*endpoint.Endpoint
		validate  func(t *testing.T, adjusted []*endpoint.Endpoint)
	}{
		{
			name: "normalizes DNS names",
			endpoints: []*endpoint.Endpoint{
				{
					DNSName:    "app.example.com.",
					Targets:    endpoint.Targets{"192.0.2.1"},
					RecordType: endpoint.RecordTypeA,
				},
			},
			validate: func(t *testing.T, adjusted []*endpoint.Endpoint) {
				assert.Equal(t, "app.example.com", adjusted[0].DNSName)
			},
		},
		{
			name: "adds owner label if not present",
			endpoints: []*endpoint.Endpoint{
				{
					DNSName:    "app.example.com",
					Targets:    endpoint.Targets{"192.0.2.1"},
					RecordType: endpoint.RecordTypeA,
				},
			},
			validate: func(t *testing.T, adjusted []*endpoint.Endpoint) {
				assert.Equal(t, "test-owner", adjusted[0].Labels[endpoint.OwnerLabelKey])
			},
		},
		{
			name: "preserves existing owner label",
			endpoints: []*endpoint.Endpoint{
				{
					DNSName:    "app.example.com",
					Targets:    endpoint.Targets{"192.0.2.1"},
					RecordType: endpoint.RecordTypeA,
					Labels: map[string]string{
						endpoint.OwnerLabelKey: "existing-owner",
					},
				},
			},
			validate: func(t *testing.T, adjusted []*endpoint.Endpoint) {
				assert.Equal(t, "existing-owner", adjusted[0].Labels[endpoint.OwnerLabelKey])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adjusted, err := provider.AdjustEndpoints(tt.endpoints)
			require.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, adjusted)
			}
		})
	}
}

func TestProvider_GetDomainFilter(t *testing.T) {
	objects := []runtime.Object{
		&dnsv1alpha1.DNSZone{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example-com",
				Namespace: "default",
			},
			Spec: dnsv1alpha1.DNSZoneSpec{
				DomainName: "example.com",
			},
		},
		&dnsv1alpha1.DNSZone{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example-org",
				Namespace: "default",
			},
			Spec: dnsv1alpha1.DNSZoneSpec{
				DomainName: "example.org",
			},
		},
	}

	provider, _ := setupProviderTest(t, objects...)

	ctx := context.Background()
	err := provider.zoneWatcher.Refresh(ctx)
	require.NoError(t, err)

	filter := provider.GetDomainFilter()
	require.NotNil(t, filter)

	// Test that filter matches domains from zones
	assert.True(t, filter.Match("app.example.com"))
	assert.True(t, filter.Match("app.example.org"))
	assert.False(t, filter.Match("app.other.com"))
}

func TestProvider_NewProvider(t *testing.T) {
	scheme := runtime.NewScheme()
	err := dnsv1alpha1.AddToScheme(scheme)
	require.NoError(t, err)
	err = corev1.AddToScheme(scheme)
	require.NoError(t, err)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	config := &Config{
		WatchMode: NamespaceWatchModeAll,
	}

	t.Run("creates provider with valid ownerID", func(t *testing.T) {
		provider, err := NewProvider(fakeClient, config, "test-owner")
		require.NoError(t, err)
		assert.NotNil(t, provider)
		assert.Equal(t, "test-owner", provider.ownerID)
	})

	t.Run("returns error with empty ownerID", func(t *testing.T) {
		provider, err := NewProvider(fakeClient, config, "")
		assert.Error(t, err)
		assert.Nil(t, provider)
	})
}

func TestProvider_OwnershipConflict(t *testing.T) {
	objects := []runtime.Object{
		&dnsv1alpha1.DNSZone{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example-com",
				Namespace: "default",
			},
			Spec: dnsv1alpha1.DNSZoneSpec{
				DomainName: "example.com",
			},
		},
		&dnsv1alpha1.DNSRecordSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      GenerateRecordSetName("app.example.com", endpoint.RecordTypeA, ""),
				Namespace: "default",
				Labels: map[string]string{
					LabelOwner:     "other-owner",
					LabelManagedBy: ManagedByValue,
				},
			},
			Spec: dnsv1alpha1.DNSRecordSetSpec{
				DNSZoneRef: corev1.LocalObjectReference{
					Name: "example-com",
				},
				RecordType: dnsv1alpha1.RRTypeA,
				Records: []dnsv1alpha1.RecordEntry{
					{
						Name: "app",
						A:    &dnsv1alpha1.ARecordSpec{Content: "192.0.2.1"},
					},
				},
			},
		},
	}

	ctx := context.Background()
	provider, _ := setupProviderTest(t, objects...)

	err := provider.zoneWatcher.Refresh(ctx)
	require.NoError(t, err)

	// Attempt to update a record owned by another owner
	changes := &plan.Changes{
		UpdateNew: []*endpoint.Endpoint{
			{
				DNSName:    "app.example.com",
				Targets:    endpoint.Targets{"192.0.2.2"},
				RecordType: endpoint.RecordTypeA,
			},
		},
	}

	err = provider.ApplyChanges(ctx, changes)
	// Should complete without error (logs warning but continues)
	require.NoError(t, err)
}
