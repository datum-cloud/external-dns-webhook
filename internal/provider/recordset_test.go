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
)

func TestRecordSetManager_Create(t *testing.T) {
	tests := []struct {
		name      string
		recordSet *dnsv1alpha1.DNSRecordSet
		config    *Config
		validate  func(t *testing.T, client client.Client)
		wantErr   bool
	}{
		{
			name: "create adds ownership labels",
			recordSet: &dnsv1alpha1.DNSRecordSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-record",
					Namespace: "default",
					Labels: map[string]string{
						LabelOwner:     "test-owner",
						LabelManagedBy: ManagedByValue,
					},
				},
				Spec: dnsv1alpha1.DNSRecordSetSpec{
					RecordType: dnsv1alpha1.RRTypeA,
					Records: []dnsv1alpha1.RecordEntry{
						{
							Name: "app",
							A:    &dnsv1alpha1.ARecordSpec{Content: "192.0.2.1"},
						},
					},
				},
			},
			config: &Config{},
			validate: func(t *testing.T, c client.Client) {
				var rs dnsv1alpha1.DNSRecordSet
				err := c.Get(context.Background(), client.ObjectKey{
					Name:      "test-record",
					Namespace: "default",
				}, &rs)
				require.NoError(t, err)
				assert.Equal(t, "test-owner", rs.Labels[LabelOwner])
				assert.Equal(t, ManagedByValue, rs.Labels[LabelManagedBy])
			},
		},
		{
			name: "dry-run mode prevents actual create",
			recordSet: &dnsv1alpha1.DNSRecordSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-record",
					Namespace: "default",
					Labels: map[string]string{
						LabelOwner:     "test-owner",
						LabelManagedBy: ManagedByValue,
					},
				},
				Spec: dnsv1alpha1.DNSRecordSetSpec{
					RecordType: dnsv1alpha1.RRTypeA,
					Records: []dnsv1alpha1.RecordEntry{
						{
							Name: "app",
							A:    &dnsv1alpha1.ARecordSpec{Content: "192.0.2.1"},
						},
					},
				},
			},
			config: &Config{
				DryRun: true,
			},
			validate: func(t *testing.T, c client.Client) {
				var rs dnsv1alpha1.DNSRecordSet
				err := c.Get(context.Background(), client.ObjectKey{
					Name:      "test-record",
					Namespace: "default",
				}, &rs)
				// Should not exist in dry-run mode
				assert.Error(t, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			scheme := runtime.NewScheme()
			err := dnsv1alpha1.AddToScheme(scheme)
			require.NoError(t, err)
			err = corev1.AddToScheme(scheme)
			require.NoError(t, err)

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			logger := logrus.New()
			logger.SetLevel(logrus.ErrorLevel)

			mgr := NewRecordSetManager(fakeClient, tt.config, logger)

			err = mgr.Create(ctx, tt.recordSet)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, fakeClient)
			}
		})
	}
}

func TestRecordSetManager_Update(t *testing.T) {
	tests := []struct {
		name      string
		existing  *dnsv1alpha1.DNSRecordSet
		updated   *dnsv1alpha1.DNSRecordSet
		config    *Config
		validate  func(t *testing.T, client client.Client)
		wantErr   bool
	}{
		{
			name: "update preserves existing metadata while updating spec",
			existing: &dnsv1alpha1.DNSRecordSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-record",
					Namespace:       "default",
					ResourceVersion: "1",
					Labels: map[string]string{
						LabelOwner:     "test-owner",
						LabelManagedBy: ManagedByValue,
					},
				},
				Spec: dnsv1alpha1.DNSRecordSetSpec{
					RecordType: dnsv1alpha1.RRTypeA,
					Records: []dnsv1alpha1.RecordEntry{
						{
							Name: "app",
							A:    &dnsv1alpha1.ARecordSpec{Content: "192.0.2.1"},
						},
					},
				},
			},
			updated: &dnsv1alpha1.DNSRecordSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-record",
					Namespace:       "default",
					ResourceVersion: "1",
					Labels: map[string]string{
						LabelOwner:     "test-owner",
						LabelManagedBy: ManagedByValue,
					},
				},
				Spec: dnsv1alpha1.DNSRecordSetSpec{
					RecordType: dnsv1alpha1.RRTypeA,
					Records: []dnsv1alpha1.RecordEntry{
						{
							Name: "app",
							A:    &dnsv1alpha1.ARecordSpec{Content: "192.0.2.2"},
						},
					},
				},
			},
			config: &Config{},
			validate: func(t *testing.T, c client.Client) {
				var rs dnsv1alpha1.DNSRecordSet
				err := c.Get(context.Background(), client.ObjectKey{
					Name:      "test-record",
					Namespace: "default",
				}, &rs)
				require.NoError(t, err)
				assert.Equal(t, "192.0.2.2", rs.Spec.Records[0].A.Content)
			},
		},
		{
			name: "dry-run mode prevents actual update",
			existing: &dnsv1alpha1.DNSRecordSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-record",
					Namespace:       "default",
					ResourceVersion: "1",
				},
				Spec: dnsv1alpha1.DNSRecordSetSpec{
					RecordType: dnsv1alpha1.RRTypeA,
					Records: []dnsv1alpha1.RecordEntry{
						{
							Name: "app",
							A:    &dnsv1alpha1.ARecordSpec{Content: "192.0.2.1"},
						},
					},
				},
			},
			updated: &dnsv1alpha1.DNSRecordSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-record",
					Namespace:       "default",
					ResourceVersion: "1",
				},
				Spec: dnsv1alpha1.DNSRecordSetSpec{
					RecordType: dnsv1alpha1.RRTypeA,
					Records: []dnsv1alpha1.RecordEntry{
						{
							Name: "app",
							A:    &dnsv1alpha1.ARecordSpec{Content: "192.0.2.2"},
						},
					},
				},
			},
			config: &Config{
				DryRun: true,
			},
			validate: func(t *testing.T, c client.Client) {
				var rs dnsv1alpha1.DNSRecordSet
				err := c.Get(context.Background(), client.ObjectKey{
					Name:      "test-record",
					Namespace: "default",
				}, &rs)
				require.NoError(t, err)
				// Should still have old value in dry-run mode
				assert.Equal(t, "192.0.2.1", rs.Spec.Records[0].A.Content)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			scheme := runtime.NewScheme()
			err := dnsv1alpha1.AddToScheme(scheme)
			require.NoError(t, err)
			err = corev1.AddToScheme(scheme)
			require.NoError(t, err)

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(tt.existing).
				Build()

			logger := logrus.New()
			logger.SetLevel(logrus.ErrorLevel)

			mgr := NewRecordSetManager(fakeClient, tt.config, logger)

			err = mgr.Update(ctx, tt.updated)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, fakeClient)
			}
		})
	}
}

func TestRecordSetManager_Delete(t *testing.T) {
	tests := []struct {
		name      string
		existing  *dnsv1alpha1.DNSRecordSet
		config    *Config
		validate  func(t *testing.T, client client.Client)
		wantErr   bool
	}{
		{
			name: "delete removes the resource",
			existing: &dnsv1alpha1.DNSRecordSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-record",
					Namespace: "default",
				},
				Spec: dnsv1alpha1.DNSRecordSetSpec{
					RecordType: dnsv1alpha1.RRTypeA,
					Records: []dnsv1alpha1.RecordEntry{
						{
							Name: "app",
							A:    &dnsv1alpha1.ARecordSpec{Content: "192.0.2.1"},
						},
					},
				},
			},
			config: &Config{},
			validate: func(t *testing.T, c client.Client) {
				var rs dnsv1alpha1.DNSRecordSet
				err := c.Get(context.Background(), client.ObjectKey{
					Name:      "test-record",
					Namespace: "default",
				}, &rs)
				assert.Error(t, err)
			},
		},
		{
			name: "dry-run mode prevents actual delete",
			existing: &dnsv1alpha1.DNSRecordSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-record",
					Namespace: "default",
				},
				Spec: dnsv1alpha1.DNSRecordSetSpec{
					RecordType: dnsv1alpha1.RRTypeA,
					Records: []dnsv1alpha1.RecordEntry{
						{
							Name: "app",
							A:    &dnsv1alpha1.ARecordSpec{Content: "192.0.2.1"},
						},
					},
				},
			},
			config: &Config{
				DryRun: true,
			},
			validate: func(t *testing.T, c client.Client) {
				var rs dnsv1alpha1.DNSRecordSet
				err := c.Get(context.Background(), client.ObjectKey{
					Name:      "test-record",
					Namespace: "default",
				}, &rs)
				// Should still exist in dry-run mode
				require.NoError(t, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			scheme := runtime.NewScheme()
			err := dnsv1alpha1.AddToScheme(scheme)
			require.NoError(t, err)
			err = corev1.AddToScheme(scheme)
			require.NoError(t, err)

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(tt.existing).
				Build()

			logger := logrus.New()
			logger.SetLevel(logrus.ErrorLevel)

			mgr := NewRecordSetManager(fakeClient, tt.config, logger)

			err = mgr.Delete(ctx, tt.existing.Name, tt.existing.Namespace)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, fakeClient)
			}
		})
	}
}

func TestRecordSetManager_List(t *testing.T) {
	tests := []struct {
		name       string
		recordSets []dnsv1alpha1.DNSRecordSet
		ownerID    string
		config     *Config
		wantCount  int
	}{
		{
			name: "list returns only records with matching owner label",
			recordSets: []dnsv1alpha1.DNSRecordSet{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "record1",
						Namespace: "default",
						Labels: map[string]string{
							LabelOwner:     "owner1",
							LabelManagedBy: ManagedByValue,
						},
					},
					Spec: dnsv1alpha1.DNSRecordSetSpec{
						RecordType: dnsv1alpha1.RRTypeA,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "record2",
						Namespace: "default",
						Labels: map[string]string{
							LabelOwner:     "owner2",
							LabelManagedBy: ManagedByValue,
						},
					},
					Spec: dnsv1alpha1.DNSRecordSetSpec{
						RecordType: dnsv1alpha1.RRTypeA,
					},
				},
			},
			ownerID: "owner1",
			config: &Config{},
			wantCount: 1,
		},
		{
			name: "list across different namespaces",
			recordSets: []dnsv1alpha1.DNSRecordSet{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "record1",
						Namespace: "namespace1",
						Labels: map[string]string{
							LabelOwner:     "owner1",
							LabelManagedBy: ManagedByValue,
						},
					},
					Spec: dnsv1alpha1.DNSRecordSetSpec{
						RecordType: dnsv1alpha1.RRTypeA,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "record2",
						Namespace: "namespace2",
						Labels: map[string]string{
							LabelOwner:     "owner1",
							LabelManagedBy: ManagedByValue,
						},
					},
					Spec: dnsv1alpha1.DNSRecordSetSpec{
						RecordType: dnsv1alpha1.RRTypeA,
					},
				},
			},
			ownerID: "owner1",
			config: &Config{},
			wantCount: 2,
		},
		{
			name: "list finds records across all namespaces (cluster-wide)",
			recordSets: []dnsv1alpha1.DNSRecordSet{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "record1",
						Namespace: "namespace1",
						Labels: map[string]string{
							LabelOwner:     "owner1",
							LabelManagedBy: ManagedByValue,
						},
					},
					Spec: dnsv1alpha1.DNSRecordSetSpec{
						RecordType: dnsv1alpha1.RRTypeA,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "record2",
						Namespace: "namespace2",
						Labels: map[string]string{
							LabelOwner:     "owner1",
							LabelManagedBy: ManagedByValue,
						},
					},
					Spec: dnsv1alpha1.DNSRecordSetSpec{
						RecordType: dnsv1alpha1.RRTypeA,
					},
				},
			},
			ownerID: "owner1",
			config: &Config{},
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			scheme := runtime.NewScheme()
			err := dnsv1alpha1.AddToScheme(scheme)
			require.NoError(t, err)
			err = corev1.AddToScheme(scheme)
			require.NoError(t, err)

			objects := []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "default",
					},
				},
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "namespace1",
					},
				},
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "namespace2",
					},
				},
			}
			for i := range tt.recordSets {
				objects = append(objects, &tt.recordSets[i])
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objects...).
				Build()

			logger := logrus.New()
			logger.SetLevel(logrus.ErrorLevel)

			mgr := NewRecordSetManager(fakeClient, tt.config, logger)

			recordSets, err := mgr.List(ctx, tt.ownerID)
			require.NoError(t, err)
			assert.Len(t, recordSets, tt.wantCount)
		})
	}
}

func TestRecordSetManager_Get(t *testing.T) {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	err := dnsv1alpha1.AddToScheme(scheme)
	require.NoError(t, err)

	existing := &dnsv1alpha1.DNSRecordSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-record",
			Namespace: "default",
		},
		Spec: dnsv1alpha1.DNSRecordSetSpec{
			RecordType: dnsv1alpha1.RRTypeA,
			Records: []dnsv1alpha1.RecordEntry{
				{
					Name: "app",
					A:    &dnsv1alpha1.ARecordSpec{Content: "192.0.2.1"},
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(existing).
		Build()

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := &Config{}

	mgr := NewRecordSetManager(fakeClient, config, logger)

	t.Run("get existing record", func(t *testing.T) {
		rs, err := mgr.Get(ctx, "test-record", "default")
		require.NoError(t, err)
		assert.Equal(t, "test-record", rs.Name)
		assert.Equal(t, "default", rs.Namespace)
	})

	t.Run("get non-existent record returns error", func(t *testing.T) {
		rs, err := mgr.Get(ctx, "non-existent", "default")
		assert.Error(t, err)
		assert.Nil(t, rs)
	})
}
