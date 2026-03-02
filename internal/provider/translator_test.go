package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	dnsv1alpha1 "go.miloapis.com/dns-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/external-dns/endpoint"
)

func TestEndpointToDNSRecordSet(t *testing.T) {
	zone := &dnsv1alpha1.DNSZone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-com",
			Namespace: "default",
		},
		Spec: dnsv1alpha1.DNSZoneSpec{
			DomainName: "example.com",
		},
	}

	tests := []struct {
		name        string
		endpoint    *endpoint.Endpoint
		zone        *dnsv1alpha1.DNSZone
		ownerID     string
		wantErr     bool
		validate    func(t *testing.T, rs *dnsv1alpha1.DNSRecordSet)
	}{
		{
			name: "A record with single target",
			endpoint: &endpoint.Endpoint{
				DNSName:    "app.example.com",
				Targets:    endpoint.Targets{"192.0.2.1"},
				RecordType: endpoint.RecordTypeA,
				RecordTTL:  300,
			},
			zone:    zone,
			ownerID: "test-owner",
			wantErr: false,
			validate: func(t *testing.T, rs *dnsv1alpha1.DNSRecordSet) {
				assert.Equal(t, "default", rs.Namespace)
				assert.Equal(t, dnsv1alpha1.RRTypeA, rs.Spec.RecordType)
				assert.Equal(t, "example-com", rs.Spec.DNSZoneRef.Name)
				assert.Len(t, rs.Spec.Records, 1)
				assert.Equal(t, "app", rs.Spec.Records[0].Name)
				assert.NotNil(t, rs.Spec.Records[0].A)
				assert.Equal(t, "192.0.2.1", rs.Spec.Records[0].A.Content)
				assert.NotNil(t, rs.Spec.Records[0].TTL)
				assert.Equal(t, int64(300), *rs.Spec.Records[0].TTL)
				assert.Equal(t, "test-owner", rs.Labels[LabelOwner])
				assert.Equal(t, "app.example.com", rs.Labels[LabelResource])
				assert.Equal(t, endpoint.RecordTypeA, rs.Labels[LabelRecordType])
				assert.Equal(t, ManagedByValue, rs.Labels[LabelManagedBy])
			},
		},
		{
			name: "A record with multiple targets",
			endpoint: &endpoint.Endpoint{
				DNSName:    "app.example.com",
				Targets:    endpoint.Targets{"192.0.2.1", "192.0.2.2"},
				RecordType: endpoint.RecordTypeA,
			},
			zone:    zone,
			ownerID: "test-owner",
			wantErr: false,
			validate: func(t *testing.T, rs *dnsv1alpha1.DNSRecordSet) {
				assert.Len(t, rs.Spec.Records, 2)
				assert.Equal(t, "app", rs.Spec.Records[0].Name)
				assert.Equal(t, "app", rs.Spec.Records[1].Name)
				assert.NotNil(t, rs.Spec.Records[0].A)
				assert.NotNil(t, rs.Spec.Records[1].A)
				assert.Contains(t, []string{"192.0.2.1", "192.0.2.2"}, rs.Spec.Records[0].A.Content)
				assert.Contains(t, []string{"192.0.2.1", "192.0.2.2"}, rs.Spec.Records[1].A.Content)
			},
		},
		{
			name: "AAAA record with IPv6 target",
			endpoint: &endpoint.Endpoint{
				DNSName:    "app.example.com",
				Targets:    endpoint.Targets{"2001:db8::1"},
				RecordType: endpoint.RecordTypeAAAA,
			},
			zone:    zone,
			ownerID: "test-owner",
			wantErr: false,
			validate: func(t *testing.T, rs *dnsv1alpha1.DNSRecordSet) {
				assert.Equal(t, dnsv1alpha1.RRTypeAAAA, rs.Spec.RecordType)
				assert.Len(t, rs.Spec.Records, 1)
				assert.NotNil(t, rs.Spec.Records[0].AAAA)
				assert.Equal(t, "2001:db8::1", rs.Spec.Records[0].AAAA.Content)
			},
		},
		{
			name: "CNAME record with single target",
			endpoint: &endpoint.Endpoint{
				DNSName:    "www.example.com",
				Targets:    endpoint.Targets{"app.example.com"},
				RecordType: endpoint.RecordTypeCNAME,
			},
			zone:    zone,
			ownerID: "test-owner",
			wantErr: false,
			validate: func(t *testing.T, rs *dnsv1alpha1.DNSRecordSet) {
				assert.Equal(t, dnsv1alpha1.RRTypeCNAME, rs.Spec.RecordType)
				assert.Len(t, rs.Spec.Records, 1)
				assert.NotNil(t, rs.Spec.Records[0].CNAME)
				assert.Equal(t, "app.example.com", rs.Spec.Records[0].CNAME.Content)
			},
		},
		{
			name: "CNAME record with multiple targets should error",
			endpoint: &endpoint.Endpoint{
				DNSName:    "www.example.com",
				Targets:    endpoint.Targets{"app1.example.com", "app2.example.com"},
				RecordType: endpoint.RecordTypeCNAME,
			},
			zone:    zone,
			ownerID: "test-owner",
			wantErr: true,
		},
		{
			name: "TXT record with single value",
			endpoint: &endpoint.Endpoint{
				DNSName:    "_acme-challenge.example.com",
				Targets:    endpoint.Targets{"validation-token"},
				RecordType: endpoint.RecordTypeTXT,
			},
			zone:    zone,
			ownerID: "test-owner",
			wantErr: false,
			validate: func(t *testing.T, rs *dnsv1alpha1.DNSRecordSet) {
				assert.Equal(t, dnsv1alpha1.RRTypeTXT, rs.Spec.RecordType)
				assert.Len(t, rs.Spec.Records, 1)
				assert.NotNil(t, rs.Spec.Records[0].TXT)
				assert.Equal(t, "validation-token", rs.Spec.Records[0].TXT.Content)
			},
		},
		{
			name: "TXT record with multiple values",
			endpoint: &endpoint.Endpoint{
				DNSName:    "_acme-challenge.example.com",
				Targets:    endpoint.Targets{"token1", "token2"},
				RecordType: endpoint.RecordTypeTXT,
			},
			zone:    zone,
			ownerID: "test-owner",
			wantErr: false,
			validate: func(t *testing.T, rs *dnsv1alpha1.DNSRecordSet) {
				assert.Len(t, rs.Spec.Records, 2)
				assert.NotNil(t, rs.Spec.Records[0].TXT)
				assert.NotNil(t, rs.Spec.Records[1].TXT)
			},
		},
		{
			name: "zone apex record should use @",
			endpoint: &endpoint.Endpoint{
				DNSName:    "example.com",
				Targets:    endpoint.Targets{"192.0.2.1"},
				RecordType: endpoint.RecordTypeA,
			},
			zone:    zone,
			ownerID: "test-owner",
			wantErr: false,
			validate: func(t *testing.T, rs *dnsv1alpha1.DNSRecordSet) {
				assert.Equal(t, "@", rs.Spec.Records[0].Name)
			},
		},
		{
			name: "subdomain record should extract relative name",
			endpoint: &endpoint.Endpoint{
				DNSName:    "api.v1.example.com",
				Targets:    endpoint.Targets{"192.0.2.1"},
				RecordType: endpoint.RecordTypeA,
			},
			zone:    zone,
			ownerID: "test-owner",
			wantErr: false,
			validate: func(t *testing.T, rs *dnsv1alpha1.DNSRecordSet) {
				assert.Equal(t, "api.v1", rs.Spec.Records[0].Name)
			},
		},
		{
			name: "unsupported record type should error",
			endpoint: &endpoint.Endpoint{
				DNSName:    "example.com",
				Targets:    endpoint.Targets{"ns1.example.com"},
				RecordType: endpoint.RecordTypeNS,
			},
			zone:    zone,
			ownerID: "test-owner",
			wantErr: true,
		},
		{
			name: "nil endpoint should error",
			endpoint: nil,
			zone:     zone,
			ownerID:  "test-owner",
			wantErr:  true,
		},
		{
			name: "nil zone should error",
			endpoint: &endpoint.Endpoint{
				DNSName:    "example.com",
				Targets:    endpoint.Targets{"192.0.2.1"},
				RecordType: endpoint.RecordTypeA,
			},
			zone:    nil,
			ownerID: "test-owner",
			wantErr: true,
		},
		{
			name: "endpoint with no targets should error",
			endpoint: &endpoint.Endpoint{
				DNSName:    "example.com",
				Targets:    endpoint.Targets{},
				RecordType: endpoint.RecordTypeA,
			},
			zone:    zone,
			ownerID: "test-owner",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rs, err := EndpointToDNSRecordSet(tt.endpoint, tt.zone, tt.ownerID)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, rs)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, rs)
			if tt.validate != nil {
				tt.validate(t, rs)
			}
		})
	}
}

func TestDNSRecordSetToEndpoints(t *testing.T) {
	zone := &dnsv1alpha1.DNSZone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-com",
			Namespace: "default",
		},
		Spec: dnsv1alpha1.DNSZoneSpec{
			DomainName: "example.com",
		},
	}

	ttl := int64(300)

	tests := []struct {
		name      string
		recordSet *dnsv1alpha1.DNSRecordSet
		zone      *dnsv1alpha1.DNSZone
		wantErr   bool
		validate  func(t *testing.T, endpoints []*endpoint.Endpoint)
	}{
		{
			name: "convert A record to endpoint",
			recordSet: &dnsv1alpha1.DNSRecordSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "app-a-12345678",
					Namespace: "default",
					Labels: map[string]string{
						LabelOwner: "test-owner",
					},
				},
				Spec: dnsv1alpha1.DNSRecordSetSpec{
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
			zone:    zone,
			wantErr: false,
			validate: func(t *testing.T, endpoints []*endpoint.Endpoint) {
				require.Len(t, endpoints, 1)
				ep := endpoints[0]
				assert.Equal(t, "app.example.com", ep.DNSName)
				assert.Equal(t, endpoint.RecordTypeA, ep.RecordType)
				assert.Equal(t, endpoint.Targets{"192.0.2.1"}, ep.Targets)
				assert.Equal(t, endpoint.TTL(300), ep.RecordTTL)
				assert.Equal(t, "test-owner", ep.Labels[endpoint.OwnerLabelKey])
			},
		},
		{
			name: "convert AAAA record to endpoint",
			recordSet: &dnsv1alpha1.DNSRecordSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "app-aaaa-12345678",
					Namespace: "default",
				},
				Spec: dnsv1alpha1.DNSRecordSetSpec{
					RecordType: dnsv1alpha1.RRTypeAAAA,
					Records: []dnsv1alpha1.RecordEntry{
						{
							Name: "app",
							AAAA: &dnsv1alpha1.AAAARecordSpec{Content: "2001:db8::1"},
						},
					},
				},
			},
			zone:    zone,
			wantErr: false,
			validate: func(t *testing.T, endpoints []*endpoint.Endpoint) {
				require.Len(t, endpoints, 1)
				ep := endpoints[0]
				assert.Equal(t, "app.example.com", ep.DNSName)
				assert.Equal(t, endpoint.RecordTypeAAAA, ep.RecordType)
				assert.Equal(t, endpoint.Targets{"2001:db8::1"}, ep.Targets)
			},
		},
		{
			name: "convert CNAME record to endpoint",
			recordSet: &dnsv1alpha1.DNSRecordSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "www-cname-12345678",
					Namespace: "default",
				},
				Spec: dnsv1alpha1.DNSRecordSetSpec{
					RecordType: dnsv1alpha1.RRTypeCNAME,
					Records: []dnsv1alpha1.RecordEntry{
						{
							Name:  "www",
							CNAME: &dnsv1alpha1.CNAMERecordSpec{Content: "app.example.com"},
						},
					},
				},
			},
			zone:    zone,
			wantErr: false,
			validate: func(t *testing.T, endpoints []*endpoint.Endpoint) {
				require.Len(t, endpoints, 1)
				ep := endpoints[0]
				assert.Equal(t, "www.example.com", ep.DNSName)
				assert.Equal(t, endpoint.RecordTypeCNAME, ep.RecordType)
				assert.Equal(t, endpoint.Targets{"app.example.com"}, ep.Targets)
			},
		},
		{
			name: "convert TXT record to endpoint",
			recordSet: &dnsv1alpha1.DNSRecordSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "acme-txt-12345678",
					Namespace: "default",
				},
				Spec: dnsv1alpha1.DNSRecordSetSpec{
					RecordType: dnsv1alpha1.RRTypeTXT,
					Records: []dnsv1alpha1.RecordEntry{
						{
							Name: "_acme-challenge",
							TXT:  &dnsv1alpha1.TXTRecordSpec{Content: "validation-token"},
						},
					},
				},
			},
			zone:    zone,
			wantErr: false,
			validate: func(t *testing.T, endpoints []*endpoint.Endpoint) {
				require.Len(t, endpoints, 1)
				ep := endpoints[0]
				assert.Equal(t, "_acme-challenge.example.com", ep.DNSName)
				assert.Equal(t, endpoint.RecordTypeTXT, ep.RecordType)
				assert.Equal(t, endpoint.Targets{"validation-token"}, ep.Targets)
			},
		},
		{
			name: "multiple RecordEntry items aggregate into single endpoint",
			recordSet: &dnsv1alpha1.DNSRecordSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "app-a-12345678",
					Namespace: "default",
				},
				Spec: dnsv1alpha1.DNSRecordSetSpec{
					RecordType: dnsv1alpha1.RRTypeA,
					Records: []dnsv1alpha1.RecordEntry{
						{
							Name: "app",
							A:    &dnsv1alpha1.ARecordSpec{Content: "192.0.2.1"},
						},
						{
							Name: "app",
							A:    &dnsv1alpha1.ARecordSpec{Content: "192.0.2.2"},
						},
					},
				},
			},
			zone:    zone,
			wantErr: false,
			validate: func(t *testing.T, endpoints []*endpoint.Endpoint) {
				require.Len(t, endpoints, 1)
				ep := endpoints[0]
				assert.Equal(t, "app.example.com", ep.DNSName)
				assert.Len(t, ep.Targets, 2)
				assert.Contains(t, ep.Targets, "192.0.2.1")
				assert.Contains(t, ep.Targets, "192.0.2.2")
			},
		},
		{
			name: "zone apex @ converts to FQDN",
			recordSet: &dnsv1alpha1.DNSRecordSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "example-com-a-12345678",
					Namespace: "default",
				},
				Spec: dnsv1alpha1.DNSRecordSetSpec{
					RecordType: dnsv1alpha1.RRTypeA,
					Records: []dnsv1alpha1.RecordEntry{
						{
							Name: "@",
							A:    &dnsv1alpha1.ARecordSpec{Content: "192.0.2.1"},
						},
					},
				},
			},
			zone:    zone,
			wantErr: false,
			validate: func(t *testing.T, endpoints []*endpoint.Endpoint) {
				require.Len(t, endpoints, 1)
				assert.Equal(t, "example.com", endpoints[0].DNSName)
			},
		},
		{
			name:      "nil recordset should error",
			recordSet: nil,
			zone:      zone,
			wantErr:   true,
		},
		{
			name: "nil zone should error",
			recordSet: &dnsv1alpha1.DNSRecordSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "app-a-12345678",
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
			zone:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoints, err := DNSRecordSetToEndpoints(tt.recordSet, tt.zone)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, endpoints)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, endpoints)
			if tt.validate != nil {
				tt.validate(t, endpoints)
			}
		})
	}
}

func TestGenerateRecordSetName(t *testing.T) {
	tests := []struct {
		name          string
		dnsName       string
		recordType    string
		setIdentifier string
		validate      func(t *testing.T, result string)
	}{
		{
			name:       "standard DNS name produces valid Kubernetes name",
			dnsName:    "app.example.com",
			recordType: endpoint.RecordTypeA,
			validate: func(t *testing.T, result string) {
				assert.Contains(t, result, "app-example-com")
				assert.Contains(t, result, "-a-")
				// Should be a valid Kubernetes name
				assert.LessOrEqual(t, len(result), 253)
				assert.Regexp(t, `^[a-z0-9-]+$`, result)
			},
		},
		{
			name:       "wildcard record produces valid name",
			dnsName:    "*.example.com",
			recordType: endpoint.RecordTypeA,
			validate: func(t *testing.T, result string) {
				// Wildcard should be sanitized
				assert.NotContains(t, result, "*")
				assert.Contains(t, result, "example-com")
				assert.Regexp(t, `^[a-z0-9-]+$`, result)
			},
		},
		{
			name:       "very long DNS name is truncated properly",
			dnsName:    "this-is-a-very-long-subdomain-name-that-exceeds-the-kubernetes-name-limit-and-should-be-truncated-to-fit-within-253-characters-which-is-the-maximum-allowed-length-for-a-kubernetes-resource-name-in-the-api-server-validation-rules.example.com",
			recordType: endpoint.RecordTypeA,
			validate: func(t *testing.T, result string) {
				assert.LessOrEqual(t, len(result), 253)
				assert.Regexp(t, `^[a-z0-9-]+$`, result)
			},
		},
		{
			name:          "same input produces same output (deterministic)",
			dnsName:       "app.example.com",
			recordType:    endpoint.RecordTypeA,
			setIdentifier: "set-1",
			validate: func(t *testing.T, result string) {
				// Generate again with same inputs
				result2 := GenerateRecordSetName("app.example.com", endpoint.RecordTypeA, "set-1")
				assert.Equal(t, result, result2)
			},
		},
		{
			name:          "different set identifiers produce different names",
			dnsName:       "app.example.com",
			recordType:    endpoint.RecordTypeA,
			setIdentifier: "set-1",
			validate: func(t *testing.T, result string) {
				result2 := GenerateRecordSetName("app.example.com", endpoint.RecordTypeA, "set-2")
				assert.NotEqual(t, result, result2)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateRecordSetName(tt.dnsName, tt.recordType, tt.setIdentifier)
			assert.NotEmpty(t, result)
			if tt.validate != nil {
				tt.validate(t, result)
			}
		})
	}
}

func TestGetRelativeName(t *testing.T) {
	tests := []struct {
		name       string
		fqdn       string
		zoneDomain string
		want       string
	}{
		{
			name:       "subdomain extracts correctly",
			fqdn:       "app.example.com",
			zoneDomain: "example.com",
			want:       "app",
		},
		{
			name:       "zone apex returns @",
			fqdn:       "example.com",
			zoneDomain: "example.com",
			want:       "@",
		},
		{
			name:       "multi-level subdomain",
			fqdn:       "deep.sub.example.com",
			zoneDomain: "example.com",
			want:       "deep.sub",
		},
		{
			name:       "handles trailing dots in fqdn",
			fqdn:       "app.example.com.",
			zoneDomain: "example.com",
			want:       "app",
		},
		{
			name:       "handles trailing dots in zone",
			fqdn:       "app.example.com",
			zoneDomain: "example.com.",
			want:       "app",
		},
		{
			name:       "handles trailing dots in both",
			fqdn:       "app.example.com.",
			zoneDomain: "example.com.",
			want:       "app",
		},
		{
			name:       "FQDN not in zone returns as-is",
			fqdn:       "app.other.com",
			zoneDomain: "example.com",
			want:       "app.other.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetRelativeName(tt.fqdn, tt.zoneDomain)
			assert.Equal(t, tt.want, got)
		})
	}
}
