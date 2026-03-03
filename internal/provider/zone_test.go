package provider

import (
	"context"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	dnsv1alpha1 "go.miloapis.com/dns-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTestSource(t *testing.T, name string, cfg ZoneSourceConfig, objects ...runtime.Object) *ZoneSource {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, dnsv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(objects...).
		Build()

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	return NewZoneSource(name, fakeClient, cfg, &Config{}, logger)
}

func TestZoneSource_Refresh(t *testing.T) {
	tests := []struct {
		name     string
		zones    []dnsv1alpha1.DNSZone
		config   ZoneSourceConfig
		validate func(t *testing.T, src *ZoneSource)
		wantErr  bool
	}{
		{
			name: "single DNSZone discovered adds to cache",
			zones: []dnsv1alpha1.DNSZone{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "example-com", Namespace: "default"},
					Spec:       dnsv1alpha1.DNSZoneSpec{DomainName: "example.com"},
				},
			},
			config: ZoneSourceConfig{},
			validate: func(t *testing.T, src *ZoneSource) {
				zones := src.GetZones()
				assert.Len(t, zones, 1)
				zone, ok := zones["example.com"]
				assert.True(t, ok)
				assert.Equal(t, "example-com", zone.Name)
			},
		},
		{
			name: "multiple DNSZones in different namespaces all discovered",
			zones: []dnsv1alpha1.DNSZone{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "example-com", Namespace: "namespace1"},
					Spec:       dnsv1alpha1.DNSZoneSpec{DomainName: "example.com"},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "example-org", Namespace: "namespace2"},
					Spec:       dnsv1alpha1.DNSZoneSpec{DomainName: "example.org"},
				},
			},
			config: ZoneSourceConfig{},
			validate: func(t *testing.T, src *ZoneSource) {
				zones := src.GetZones()
				assert.Len(t, zones, 2)
				_, ok1 := zones["example.com"]
				_, ok2 := zones["example.org"]
				assert.True(t, ok1)
				assert.True(t, ok2)
			},
		},
		{
			name: "namespace set: only discovers zones in that namespace",
			zones: []dnsv1alpha1.DNSZone{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "example-com", Namespace: "namespace1"},
					Spec:       dnsv1alpha1.DNSZoneSpec{DomainName: "example.com"},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "example-org", Namespace: "namespace2"},
					Spec:       dnsv1alpha1.DNSZoneSpec{DomainName: "example.org"},
				},
			},
			config: ZoneSourceConfig{Namespace: "namespace1"},
			validate: func(t *testing.T, src *ZoneSource) {
				zones := src.GetZones()
				assert.Len(t, zones, 1)
				zone, ok := zones["example.com"]
				assert.True(t, ok)
				assert.Equal(t, "namespace1", zone.Namespace)
			},
		},
		{
			name:   "empty zones produces empty cache",
			zones:  []dnsv1alpha1.DNSZone{},
			config: ZoneSourceConfig{},
			validate: func(t *testing.T, src *ZoneSource) {
				assert.Len(t, src.GetZones(), 0)
			},
		},
		{
			name: "zone domain with trailing dot is normalized",
			zones: []dnsv1alpha1.DNSZone{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "example-com", Namespace: "default"},
					Spec:       dnsv1alpha1.DNSZoneSpec{DomainName: "example.com."},
				},
			},
			config: ZoneSourceConfig{},
			validate: func(t *testing.T, src *ZoneSource) {
				zones := src.GetZones()
				assert.Len(t, zones, 1)
				zone, ok := zones["example.com"]
				assert.True(t, ok)
				assert.Equal(t, "example-com", zone.Name)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			objects := []runtime.Object{}
			namespaceNames := map[string]bool{}
			for _, zone := range tt.zones {
				z := zone
				namespaceNames[z.Namespace] = true
				objects = append(objects, &z)
			}
			for ns := range namespaceNames {
				objects = append(objects, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})
			}

			src := newTestSource(t, "test", tt.config, objects...)
			err := src.Refresh(ctx)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, src)
			}
		})
	}
}

func TestZoneRegistry_GetZoneForDomain(t *testing.T) {
	tests := []struct {
		name     string
		zones    []dnsv1alpha1.DNSZone
		domain   string
		wantZone string
		wantErr  bool
	}{
		{
			name: "exact match",
			zones: []dnsv1alpha1.DNSZone{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "example-com", Namespace: "default"},
					Spec:       dnsv1alpha1.DNSZoneSpec{DomainName: "example.com"},
				},
			},
			domain:   "example.com",
			wantZone: "example-com",
		},
		{
			name: "subdomain finds zone",
			zones: []dnsv1alpha1.DNSZone{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "example-com", Namespace: "default"},
					Spec:       dnsv1alpha1.DNSZoneSpec{DomainName: "example.com"},
				},
			},
			domain:   "app.example.com",
			wantZone: "example-com",
		},
		{
			name: "longest suffix match",
			zones: []dnsv1alpha1.DNSZone{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "example-com", Namespace: "default"},
					Spec:       dnsv1alpha1.DNSZoneSpec{DomainName: "example.com"},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "sub-example-com", Namespace: "default"},
					Spec:       dnsv1alpha1.DNSZoneSpec{DomainName: "sub.example.com"},
				},
			},
			domain:   "app.sub.example.com",
			wantZone: "sub-example-com",
		},
		{
			name: "unknown domain returns error",
			zones: []dnsv1alpha1.DNSZone{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "example-com", Namespace: "default"},
					Spec:       dnsv1alpha1.DNSZoneSpec{DomainName: "example.com"},
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
					ObjectMeta: metav1.ObjectMeta{Name: "example-com", Namespace: "default"},
					Spec:       dnsv1alpha1.DNSZoneSpec{DomainName: "example.com"},
				},
			},
			domain:   "app.example.com.",
			wantZone: "example-com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			objects := []runtime.Object{
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			}
			for _, zone := range tt.zones {
				z := zone
				objects = append(objects, &z)
			}

			src := newTestSource(t, "test", ZoneSourceConfig{}, objects...)
			require.NoError(t, src.Refresh(ctx))

			logger := logrus.New()
			logger.SetLevel(logrus.ErrorLevel)

			registry := NewZoneRegistry([]*ZoneSource{src}, logger)

			match, err := registry.GetZoneForDomain(tt.domain)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, match)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, match)
			assert.Equal(t, tt.wantZone, match.Zone.Name)
			assert.Equal(t, "test", match.Source.Name())
		})
	}
}

func TestZoneRegistry_GetDomainFilter(t *testing.T) {
	tests := []struct {
		name   string
		zones  []dnsv1alpha1.DNSZone
		expect []string
	}{
		{
			name: "single zone",
			zones: []dnsv1alpha1.DNSZone{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "example-com", Namespace: "default"},
					Spec:       dnsv1alpha1.DNSZoneSpec{DomainName: "example.com"},
				},
			},
			expect: []string{"example.com"},
		},
		{
			name: "multiple zones",
			zones: []dnsv1alpha1.DNSZone{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "example-com", Namespace: "default"},
					Spec:       dnsv1alpha1.DNSZoneSpec{DomainName: "example.com"},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "example-org", Namespace: "default"},
					Spec:       dnsv1alpha1.DNSZoneSpec{DomainName: "example.org"},
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

			objects := []runtime.Object{
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			}
			for _, zone := range tt.zones {
				z := zone
				objects = append(objects, &z)
			}

			src := newTestSource(t, "test", ZoneSourceConfig{}, objects...)
			require.NoError(t, src.Refresh(ctx))

			logger := logrus.New()
			logger.SetLevel(logrus.ErrorLevel)

			registry := NewZoneRegistry([]*ZoneSource{src}, logger)
			filter := registry.GetDomainFilter()
			require.NotNil(t, filter)

			for _, domain := range tt.expect {
				testEndpoint := "test." + domain
				assert.True(t, filter.Match(testEndpoint), "Domain filter should match %s", testEndpoint)
			}
		})
	}
}

func TestZoneSource_LabeledNamespaceMode(t *testing.T) {
	ctx := context.Background()

	objects := []runtime.Object{
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "labeled-ns",
				Labels: map[string]string{"dns": "enabled"},
			},
		},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "unlabeled-ns"}},
		&dnsv1alpha1.DNSZone{
			ObjectMeta: metav1.ObjectMeta{Name: "zone-in-labeled", Namespace: "labeled-ns"},
			Spec:       dnsv1alpha1.DNSZoneSpec{DomainName: "labeled.example.com"},
		},
		&dnsv1alpha1.DNSZone{
			ObjectMeta: metav1.ObjectMeta{Name: "zone-in-unlabeled", Namespace: "unlabeled-ns"},
			Spec:       dnsv1alpha1.DNSZoneSpec{DomainName: "unlabeled.example.com"},
		},
	}

	src := newTestSource(t, "test", ZoneSourceConfig{
		NamespaceLabelSelector: "dns=enabled",
	}, objects...)

	require.NoError(t, src.Refresh(ctx))

	zones := src.GetZones()
	assert.Len(t, zones, 1)
	zone, ok := zones["labeled.example.com"]
	assert.True(t, ok)
	assert.Equal(t, "labeled-ns", zone.Namespace)
	_, ok = zones["unlabeled.example.com"]
	assert.False(t, ok)
}

func TestZoneRegistry_MultiSourceRouting(t *testing.T) {
	ctx := context.Background()

	objects1 := []runtime.Object{
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&dnsv1alpha1.DNSZone{
			ObjectMeta: metav1.ObjectMeta{Name: "example-com", Namespace: "default"},
			Spec:       dnsv1alpha1.DNSZoneSpec{DomainName: "example.com"},
		},
	}
	src1 := newTestSource(t, "cluster-a", ZoneSourceConfig{}, objects1...)
	require.NoError(t, src1.Refresh(ctx))

	objects2 := []runtime.Object{
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&dnsv1alpha1.DNSZone{
			ObjectMeta: metav1.ObjectMeta{Name: "example-org", Namespace: "default"},
			Spec:       dnsv1alpha1.DNSZoneSpec{DomainName: "example.org"},
		},
	}
	src2 := newTestSource(t, "cluster-b", ZoneSourceConfig{}, objects2...)
	require.NoError(t, src2.Refresh(ctx))

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	registry := NewZoneRegistry([]*ZoneSource{src1, src2}, logger)

	t.Run("routes to correct source for example.com", func(t *testing.T) {
		match, err := registry.GetZoneForDomain("app.example.com")
		require.NoError(t, err)
		assert.Equal(t, "cluster-a", match.Source.Name())
		assert.Equal(t, "example-com", match.Zone.Name)
	})

	t.Run("routes to correct source for example.org", func(t *testing.T) {
		match, err := registry.GetZoneForDomain("app.example.org")
		require.NoError(t, err)
		assert.Equal(t, "cluster-b", match.Source.Name())
		assert.Equal(t, "example-org", match.Zone.Name)
	})

	t.Run("domain filter includes both sources", func(t *testing.T) {
		filter := registry.GetDomainFilter()
		assert.True(t, filter.Match("app.example.com"))
		assert.True(t, filter.Match("app.example.org"))
		assert.False(t, filter.Match("app.unknown.net"))
	})

	t.Run("list zones returns zones from all sources", func(t *testing.T) {
		zones := registry.ListZones()
		assert.Len(t, zones, 2)
	})
}

func TestZoneSource_RefreshInterval(t *testing.T) {
	src := newTestSource(t, "test", ZoneSourceConfig{
		RefreshInterval: 100 * time.Millisecond,
	},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
	)

	assert.Equal(t, 100*time.Millisecond, src.config.RefreshInterval)
}
