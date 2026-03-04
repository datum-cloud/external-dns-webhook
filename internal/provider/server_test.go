package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
)

func setupServerTest(t *testing.T, objects ...runtime.Object) *Server {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, dnsv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	hasNamespace := false
	for _, obj := range objects {
		if ns, ok := obj.(*corev1.Namespace); ok && ns.Name == "default" {
			hasNamespace = true
			break
		}
	}
	if !hasNamespace {
		objects = append(objects, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "default"},
		})
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(objects...).
		Build()

	config := &Config{
		Port:        8888,
		BindAddress: "127.0.0.1",
		MetricsPort: 8080,
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	src := NewZoneSource("test", fakeClient, ZoneSourceConfig{}, config, logger)

	p, err := NewProvider(config, "test-owner", []*ZoneSource{src})
	require.NoError(t, err)

	server := NewServer(p, config)
	return server
}

func TestServer_NegotiateHandler(t *testing.T) {
	objects := []runtime.Object{
		&dnsv1alpha1.DNSZone{
			ObjectMeta: metav1.ObjectMeta{Name: "example-com", Namespace: "default"},
			Spec:       dnsv1alpha1.DNSZoneSpec{DomainName: "example.com"},
		},
	}

	server := setupServerTest(t, objects...)
	ctx := context.Background()
	require.NoError(t, server.provider.registry.Refresh(ctx))

	tests := []struct {
		name            string
		method          string
		wantStatus      int
		wantContentType string
	}{
		{
			name:            "GET / returns domain filter with correct content-type",
			method:          http.MethodGet,
			wantStatus:      http.StatusOK,
			wantContentType: MediaTypeFormatAndVersion,
		},
		{
			name:       "POST / returns method not allowed",
			method:     http.MethodPost,
			wantStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/", nil)
			w := httptest.NewRecorder()

			server.negotiateHandler(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
			if tt.wantContentType != "" {
				assert.Equal(t, tt.wantContentType, w.Header().Get(ContentTypeHeader))
			}

			if tt.wantStatus == http.StatusOK {
				var domainFilter endpoint.DomainFilter
				err := json.NewDecoder(w.Body).Decode(&domainFilter)
				require.NoError(t, err)
			}
		})
	}
}

func TestServer_GetRecordsHandler(t *testing.T) {
	ttl := int64(300)
	objects := []runtime.Object{
		&dnsv1alpha1.DNSZone{
			ObjectMeta: metav1.ObjectMeta{Name: "example-com", Namespace: "default"},
			Spec:       dnsv1alpha1.DNSZoneSpec{DomainName: "example.com"},
		},
		&dnsv1alpha1.DNSRecordSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "app-a-12345678", Namespace: "default",
				Labels: map[string]string{LabelOwner: "test-owner", LabelManagedBy: ManagedByValue},
			},
			Spec: dnsv1alpha1.DNSRecordSetSpec{
				DNSZoneRef: corev1.LocalObjectReference{Name: "example-com"},
				RecordType: dnsv1alpha1.RRTypeA,
				Records: []dnsv1alpha1.RecordEntry{
					{Name: "app", TTL: &ttl, A: &dnsv1alpha1.ARecordSpec{Content: "192.0.2.1"}},
				},
			},
		},
	}

	server := setupServerTest(t, objects...)
	ctx := context.Background()
	require.NoError(t, server.provider.registry.Refresh(ctx))

	req := httptest.NewRequest(http.MethodGet, "/records", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	server.handleGetRecords(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, MediaTypeFormatAndVersion, w.Header().Get(ContentTypeHeader))

	var endpoints []*endpoint.Endpoint
	err := json.NewDecoder(w.Body).Decode(&endpoints)
	require.NoError(t, err)
	assert.Len(t, endpoints, 1)
	assert.Equal(t, "app.example.com", endpoints[0].DNSName)
}

func TestServer_ApplyChangesHandler(t *testing.T) {
	objects := []runtime.Object{
		&dnsv1alpha1.DNSZone{
			ObjectMeta: metav1.ObjectMeta{Name: "example-com", Namespace: "default"},
			Spec:       dnsv1alpha1.DNSZoneSpec{DomainName: "example.com"},
		},
	}

	server := setupServerTest(t, objects...)
	ctx := context.Background()
	require.NoError(t, server.provider.registry.Refresh(ctx))

	changes := &plan.Changes{
		Create: []*endpoint.Endpoint{
			{DNSName: "app.example.com", Targets: endpoint.Targets{"192.0.2.1"}, RecordType: endpoint.RecordTypeA},
		},
	}

	body, err := json.Marshal(changes)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/records", bytes.NewReader(body))
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	server.handleApplyChanges(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestServer_AdjustEndpointsHandler(t *testing.T) {
	server := setupServerTest(t)

	tests := []struct {
		name       string
		method     string
		body       interface{}
		wantStatus int
		validate   func(t *testing.T, w *httptest.ResponseRecorder)
	}{
		{
			name:   "POST /adjustendpoints returns adjusted endpoints",
			method: http.MethodPost,
			body: []*endpoint.Endpoint{
				{DNSName: "app.example.com.", Targets: endpoint.Targets{"192.0.2.1"}, RecordType: endpoint.RecordTypeA},
			},
			wantStatus: http.StatusOK,
			validate: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Equal(t, MediaTypeFormatAndVersion, w.Header().Get(ContentTypeHeader))
				var endpoints []*endpoint.Endpoint
				err := json.NewDecoder(w.Body).Decode(&endpoints)
				require.NoError(t, err)
				assert.Len(t, endpoints, 1)
				assert.Equal(t, "app.example.com", endpoints[0].DNSName)
			},
		},
		{
			name:       "GET /adjustendpoints returns method not allowed",
			method:     http.MethodGet,
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "POST with invalid body returns bad request",
			method:     http.MethodPost,
			body:       "invalid json",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body []byte
			var err error
			if tt.body != nil {
				if str, ok := tt.body.(string); ok {
					body = []byte(str)
				} else {
					body, err = json.Marshal(tt.body)
					require.NoError(t, err)
				}
			}

			req := httptest.NewRequest(tt.method, "/adjustendpoints", bytes.NewReader(body))
			w := httptest.NewRecorder()

			server.adjustEndpointsHandler(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
			if tt.validate != nil {
				tt.validate(t, w)
			}
		})
	}
}

func TestServer_HealthHandler(t *testing.T) {
	server := setupServerTest(t)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	server.healthHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var response map[string]string
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "ok", response["status"])
}

func TestServer_ReadyHandler(t *testing.T) {
	tests := []struct {
		name       string
		zones      []runtime.Object
		startTime  time.Time
		wantStatus int
		validate   func(t *testing.T, w *httptest.ResponseRecorder)
	}{
		{
			name: "returns 200 when zones exist",
			zones: []runtime.Object{
				&dnsv1alpha1.DNSZone{
					ObjectMeta: metav1.ObjectMeta{Name: "example-com", Namespace: "default"},
					Spec:       dnsv1alpha1.DNSZoneSpec{DomainName: "example.com"},
				},
			},
			startTime:  time.Now(),
			wantStatus: http.StatusOK,
			validate: func(t *testing.T, w *httptest.ResponseRecorder) {
				var response map[string]interface{}
				err := json.NewDecoder(w.Body).Decode(&response)
				require.NoError(t, err)
				assert.Equal(t, "ready", response["status"])
				assert.Equal(t, float64(1), response["zones"])
			},
		},
		{
			name:       "returns 503 when no zones (outside grace period)",
			zones:      []runtime.Object{},
			startTime:  time.Now().Add(-35 * time.Second),
			wantStatus: http.StatusServiceUnavailable,
			validate: func(t *testing.T, w *httptest.ResponseRecorder) {
				var response map[string]interface{}
				err := json.NewDecoder(w.Body).Decode(&response)
				require.NoError(t, err)
				assert.Equal(t, "not_ready", response["status"])
				assert.Equal(t, "no zones discovered", response["reason"])
			},
		},
		{
			name:       "returns 200 when no zones (within grace period)",
			zones:      []runtime.Object{},
			startTime:  time.Now().Add(-10 * time.Second),
			wantStatus: http.StatusOK,
			validate: func(t *testing.T, w *httptest.ResponseRecorder) {
				var response map[string]interface{}
				err := json.NewDecoder(w.Body).Decode(&response)
				require.NoError(t, err)
				assert.Equal(t, "ready", response["status"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := setupServerTest(t, tt.zones...)
			server.startTime = tt.startTime

			ctx := context.Background()
			require.NoError(t, server.provider.registry.Refresh(ctx))

			req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
			w := httptest.NewRecorder()

			server.readyHandler(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
			if tt.validate != nil {
				tt.validate(t, w)
			}
		})
	}
}

func TestServer_RecordsHandler(t *testing.T) {
	objects := []runtime.Object{
		&dnsv1alpha1.DNSZone{
			ObjectMeta: metav1.ObjectMeta{Name: "example-com", Namespace: "default"},
			Spec:       dnsv1alpha1.DNSZoneSpec{DomainName: "example.com"},
		},
	}

	server := setupServerTest(t, objects...)
	ctx := context.Background()
	require.NoError(t, server.provider.registry.Refresh(ctx))

	tests := []struct {
		name       string
		method     string
		wantStatus int
	}{
		{name: "GET /records returns records", method: http.MethodGet, wantStatus: http.StatusOK},
		{name: "POST /records applies changes", method: http.MethodPost, wantStatus: http.StatusNoContent},
		{name: "PUT /records returns method not allowed", method: http.MethodPut, wantStatus: http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body []byte
			if tt.method == http.MethodPost {
				changes := &plan.Changes{}
				body, _ = json.Marshal(changes)
			}

			req := httptest.NewRequest(tt.method, "/records", bytes.NewReader(body))
			req = req.WithContext(ctx)
			w := httptest.NewRecorder()

			server.recordsHandler(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestServer_InstrumentHandler(t *testing.T) {
	server := setupServerTest(t)

	handlerCalled := false
	testHandler := func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}

	instrumentedHandler := server.instrumentHandler("/test", testHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	instrumentedHandler(w, req)

	assert.True(t, handlerCalled)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestServer_ResponseWriterWrapper(t *testing.T) {
	w := httptest.NewRecorder()
	wrapper := &responseWriterWrapper{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}

	assert.Equal(t, http.StatusOK, wrapper.statusCode)

	wrapper.WriteHeader(http.StatusNotFound)
	assert.Equal(t, http.StatusNotFound, wrapper.statusCode)
	assert.Equal(t, http.StatusNotFound, w.Code)

	_, err := wrapper.Write([]byte("test"))
	require.NoError(t, err)
	assert.Equal(t, "test", w.Body.String())
}
