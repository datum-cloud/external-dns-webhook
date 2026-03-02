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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestZoneWatcher_Refresh(t *testing.T) {
	tests := []struct {
		name      string
		zones     []dnsv1alpha1.DNSZone
		namespace string
		config    *Config
		validate  func(t *testing.T, watcher *ZoneWatcher)
		wantErr   bool
	}{
		{
			name: "single DNSZone discovered adds to cache",
			zones: []dnsv1alpha1.DNSZone{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-com",
						Namespace: "default",
					},
					Spec: dnsv1alpha1.DNSZoneSpec{
						DomainName: "example.com",
					},
				},
			},
			config: &Config{
				WatchMode: NamespaceWatchModeAll,
			},
			validate: func(t *testing.T, watcher *ZoneWatcher) {
				assert.Len(t, watcher.cache, 1)
				zone, ok := watcher.cache["example.com"]
				assert.True(t, ok)
				assert.Equal(t, "example-com", zone.Name)
			},
		},
		{
			name: "multiple DNSZones in different namespaces all discovered (cluster-wide mode)",
			zones: []dnsv1alpha1.DNSZone{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-com",
						Namespace: "namespace1",
					},
					Spec: dnsv1alpha1.DNSZoneSpec{
						DomainName: "example.com",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-org",
						Namespace: "namespace2",
					},
					Spec: dnsv1alpha1.DNSZoneSpec{
						DomainName: "example.org",
					},
				},
			},
			config: &Config{
				WatchMode: NamespaceWatchModeAll,
			},
			validate: func(t *testing.T, watcher *ZoneWatcher) {
				assert.Len(t, watcher.cache, 2)
				_, ok1 := watcher.cache["example.com"]
				_, ok2 := watcher.cache["example.org"]
				assert.True(t, ok1)
				assert.True(t, ok2)
			},
		},
		{
			name: "single namespace mode only discovers zones in that namespace",
			zones: []dnsv1alpha1.DNSZone{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-com",
						Namespace: "namespace1",
					},
					Spec: dnsv1alpha1.DNSZoneSpec{
						DomainName: "example.com",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-org",
						Namespace: "namespace2",
					},
					Spec: dnsv1alpha1.DNSZoneSpec{
						DomainName: "example.org",
					},
				},
			},
			config: &Config{
				WatchMode: NamespaceWatchModeSpecific,
				Namespace: "namespace1",
			},
			validate: func(t *testing.T, watcher *ZoneWatcher) {
				assert.Len(t, watcher.cache, 1)
				zone, ok := watcher.cache["example.com"]
				assert.True(t, ok)
				assert.Equal(t, "namespace1", zone.Namespace)
				_, ok2 := watcher.cache["example.org"]
				assert.False(t, ok2)
			},
		},
		{
			name: "empty zones produces empty cache",
			zones: []dnsv1alpha1.DNSZone{},
			config: &Config{
				WatchMode: NamespaceWatchModeAll,
			},
			validate: func(t *testing.T, watcher *ZoneWatcher) {
				assert.Len(t, watcher.cache, 0)
			},
		},
		{
			name: "zone domain with trailing dot is normalized",
			zones: []dnsv1alpha1.DNSZone{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-com",
						Namespace: "default",
					},
					Spec: dnsv1alpha1.DNSZoneSpec{
						DomainName: "example.com.",
					},
				},
			},
			config: &Config{
				WatchMode: NamespaceWatchModeAll,
			},
			validate: func(t *testing.T, watcher *ZoneWatcher) {
				assert.Len(t, watcher.cache, 1)
				// Should be stored without trailing dot
				zone, ok := watcher.cache["example.com"]
				assert.True(t, ok)
				assert.Equal(t, "example-com", zone.Name)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			// Create scheme and add types
			scheme := runtime.NewScheme()
			err := dnsv1alpha1.AddToScheme(scheme)
			require.NoError(t, err)
			err = corev1.AddToScheme(scheme)
			require.NoError(t, err)

			// Create namespaces for the test
			objects := []runtime.Object{}
			namespaceNames := map[string]bool{}
			for _, zone := range tt.zones {
				namespaceNames[zone.Namespace] = true
				objects = append(objects, &zone)
			}
			for ns := range namespaceNames {
				objects = append(objects, &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: ns,
					},
				})
			}

			// Create fake client
			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objects...).
				Build()

			logger := logrus.New()
			logger.SetLevel(logrus.ErrorLevel) // Reduce noise in tests

			watcher := NewZoneWatcher(client, tt.config, logger)

			err = watcher.refresh(ctx)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, watcher)
			}
		})
	}
}

func TestZoneWatcher_GetZoneForDomain(t *testing.T) {
	tests := []struct {
		name       string
		zones      []dnsv1alpha1.DNSZone
		domain     string
		wantZone   string
		wantErr    bool
	}{
		{
			name: "exact match",
			zones: []dnsv1alpha1.DNSZone{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-com",
						Namespace: "default",
					},
					Spec: dnsv1alpha1.DNSZoneSpec{
						DomainName: "example.com",
					},
				},
			},
			domain:   "example.com",
			wantZone: "example-com",
			wantErr:  false,
		},
		{
			name: "subdomain finds zone",
			zones: []dnsv1alpha1.DNSZone{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-com",
						Namespace: "default",
					},
					Spec: dnsv1alpha1.DNSZoneSpec{
						DomainName: "example.com",
					},
				},
			},
			domain:   "app.example.com",
			wantZone: "example-com",
			wantErr:  false,
		},
		{
			name: "longest suffix match (sub.example.com vs example.com)",
			zones: []dnsv1alpha1.DNSZone{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-com",
						Namespace: "default",
					},
					Spec: dnsv1alpha1.DNSZoneSpec{
						DomainName: "example.com",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "sub-example-com",
						Namespace: "default",
					},
					Spec: dnsv1alpha1.DNSZoneSpec{
						DomainName: "sub.example.com",
					},
				},
			},
			domain:   "app.sub.example.com",
			wantZone: "sub-example-com",
			wantErr:  false,
		},
		{
			name: "unknown domain returns error",
			zones: []dnsv1alpha1.DNSZone{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-com",
						Namespace: "default",
					},
					Spec: dnsv1alpha1.DNSZoneSpec{
						DomainName: "example.com",
					},
				},
			},
			domain:  "unknown.org",
			wantErr: true,
		},
		{
			name:    "empty zones returns error",
			zones:   []dnsv1alpha1.DNSZone{},
			domain:  "example.com",
			wantErr: true,
		},
		{
			name: "domain with trailing dot is normalized",
			zones: []dnsv1alpha1.DNSZone{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-com",
						Namespace: "default",
					},
					Spec: dnsv1alpha1.DNSZoneSpec{
						DomainName: "example.com",
					},
				},
			},
			domain:   "app.example.com.",
			wantZone: "example-com",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			// Create scheme
			scheme := runtime.NewScheme()
			err := dnsv1alpha1.AddToScheme(scheme)
			require.NoError(t, err)
			err = corev1.AddToScheme(scheme)
			require.NoError(t, err)

			// Create objects
			objects := []runtime.Object{}
			for _, zone := range tt.zones {
				z := zone
				objects = append(objects, &z)
			}
			objects = append(objects, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "default",
				},
			})

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objects...).
				Build()

			logger := logrus.New()
			logger.SetLevel(logrus.ErrorLevel)

			watcher := NewZoneWatcher(client, &Config{WatchMode: NamespaceWatchModeAll}, logger)
			err = watcher.refresh(ctx)
			require.NoError(t, err)

			zone, err := watcher.GetZoneForDomain(tt.domain)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, zone)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, zone)
			assert.Equal(t, tt.wantZone, zone.Name)
		})
	}
}

func TestZoneWatcher_GetDomainFilter(t *testing.T) {
	tests := []struct {
		name   string
		zones  []dnsv1alpha1.DNSZone
		expect []string
	}{
		{
			name: "single zone",
			zones: []dnsv1alpha1.DNSZone{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-com",
						Namespace: "default",
					},
					Spec: dnsv1alpha1.DNSZoneSpec{
						DomainName: "example.com",
					},
				},
			},
			expect: []string{"example.com"},
		},
		{
			name: "multiple zones",
			zones: []dnsv1alpha1.DNSZone{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-com",
						Namespace: "default",
					},
					Spec: dnsv1alpha1.DNSZoneSpec{
						DomainName: "example.com",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-org",
						Namespace: "default",
					},
					Spec: dnsv1alpha1.DNSZoneSpec{
						DomainName: "example.org",
					},
				},
			},
			expect: []string{"example.com", "example.org"},
		},
		{
			name:   "no zones returns empty filter",
			zones:  []dnsv1alpha1.DNSZone{},
			expect: []string{},
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
			}
			for _, zone := range tt.zones {
				z := zone
				objects = append(objects, &z)
			}

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objects...).
				Build()

			logger := logrus.New()
			logger.SetLevel(logrus.ErrorLevel)

			watcher := NewZoneWatcher(client, &Config{WatchMode: NamespaceWatchModeAll}, logger)
			err = watcher.refresh(ctx)
			require.NoError(t, err)

			filter := watcher.GetDomainFilter()
			require.NotNil(t, filter)

			// Verify the filter contains all expected domains
			for _, domain := range tt.expect {
				// Create a test endpoint to check if it matches the filter
				testEndpoint := "test." + domain
				matches := filter.Match(testEndpoint)
				assert.True(t, matches, "Domain filter should match %s", testEndpoint)
			}
		})
	}
}

func TestZoneWatcher_LabeledNamespaceMode(t *testing.T) {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	err := dnsv1alpha1.AddToScheme(scheme)
	require.NoError(t, err)
	err = corev1.AddToScheme(scheme)
	require.NoError(t, err)

	// Create namespaces with labels
	objects := []runtime.Object{
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "labeled-ns",
				Labels: map[string]string{
					"dns": "enabled",
				},
			},
		},
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "unlabeled-ns",
			},
		},
		&dnsv1alpha1.DNSZone{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "zone-in-labeled",
				Namespace: "labeled-ns",
			},
			Spec: dnsv1alpha1.DNSZoneSpec{
				DomainName: "labeled.example.com",
			},
		},
		&dnsv1alpha1.DNSZone{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "zone-in-unlabeled",
				Namespace: "unlabeled-ns",
			},
			Spec: dnsv1alpha1.DNSZoneSpec{
				DomainName: "unlabeled.example.com",
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(objects...).
		Build()

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := &Config{
		WatchMode:              NamespaceWatchModeLabeled,
		NamespaceLabelSelector: "dns=enabled",
	}

	watcher := NewZoneWatcher(client, config, logger)
	err = watcher.refresh(ctx)
	require.NoError(t, err)

	// Should only discover zone in labeled namespace
	assert.Len(t, watcher.cache, 1)
	zone, ok := watcher.cache["labeled.example.com"]
	assert.True(t, ok)
	assert.Equal(t, "labeled-ns", zone.Namespace)

	_, ok = watcher.cache["unlabeled.example.com"]
	assert.False(t, ok)
}
