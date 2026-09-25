package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	dnsv1alpha1 "go.miloapis.com/dns-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	extdnsprovider "sigs.k8s.io/external-dns/provider"
	"sigs.k8s.io/external-dns/provider/webhook"
)

type slowAPI struct {
	delay    time.Duration
	panicOn  string
	inFlight atomic.Int32
	maxSeen  atomic.Int32
	mu       sync.Mutex
	calls    []string
}

func (a *slowAPI) wait(ctx context.Context, call string) error {
	n := a.inFlight.Add(1)
	defer a.inFlight.Add(-1)
	for {
		seen := a.maxSeen.Load()
		if n <= seen || a.maxSeen.CompareAndSwap(seen, n) {
			break
		}
	}
	a.mu.Lock()
	a.calls = append(a.calls, call)
	a.mu.Unlock()
	if a.panicOn != "" && strings.HasSuffix(call, a.panicOn) {
		panic("injected")
	}

	select {
	case <-time.After(a.delay):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *slowAPI) funcs() interceptor.Funcs {
	return interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if err := a.wait(ctx, "create "+obj.GetName()); err != nil {
				return err
			}
			return c.Create(ctx, obj, opts...)
		},
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			if err := a.wait(ctx, "delete "+obj.GetName()); err != nil {
				return err
			}
			return c.Delete(ctx, obj, opts...)
		},
	}
}

func setupSlowServer(t *testing.T, api *slowAPI, cfg *Config, objects ...client.Object) (*Server, client.Client) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, dnsv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	objects = append(objects,
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&dnsv1alpha1.DNSZone{
			ObjectMeta: metav1.ObjectMeta{Name: "example-com", Namespace: "default"},
			Spec:       dnsv1alpha1.DNSZoneSpec{DomainName: "example.com"},
		},
	)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		WithInterceptorFuncs(api.funcs()).
		Build()

	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)

	src := NewZoneSource("test", fakeClient, ZoneSourceConfig{}, cfg, logger)
	p, err := NewProvider(cfg, "test-owner", []*ZoneSource{src})
	require.NoError(t, err)
	require.NoError(t, p.registry.Refresh(context.Background()))

	return NewServer(p, cfg), fakeClient
}

func serveWebhook(t *testing.T, s *Server) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv := s.newWebhookServer(listener.Addr().String())
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(func() { _ = srv.Close() })

	return "http://" + listener.Addr().String()
}

func createChanges(n int) *plan.Changes {
	changes := &plan.Changes{}
	for i := range n {
		changes.Create = append(changes.Create, &endpoint.Endpoint{
			DNSName:    fmt.Sprintf("app%d.example.com", i),
			Targets:    endpoint.Targets{"192.0.2.1"},
			RecordType: endpoint.RecordTypeA,
		})
	}
	return changes
}

func jsonReader(v any) (io.Reader, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(b), nil
}

func TestConfig_WebhookWriteTimeoutExceedsApplyTimeout(t *testing.T) {
	for _, cfg := range []*Config{{}, DefaultConfig(), {ApplyTimeout: 20 * time.Minute}} {
		assert.Greater(t, cfg.WebhookWriteTimeout(), cfg.EffectiveApplyTimeout())
	}
}

func TestApplyChanges_SlowBatchCompletesOverHTTP(t *testing.T) {
	api := &slowAPI{delay: 50 * time.Millisecond}
	cfg := &Config{ApplyTimeout: 5 * time.Second, ApplyConcurrency: 4}
	s, c := setupSlowServer(t, api, cfg)
	url := serveWebhook(t, s)

	client, err := webhook.NewWebhookProvider(url)
	require.NoError(t, err)

	start := time.Now()
	require.NoError(t, client.ApplyChanges(context.Background(), createChanges(20)))
	elapsed := time.Since(start)

	var list dnsv1alpha1.DNSRecordSetList
	require.NoError(t, c.List(context.Background(), &list))
	assert.Len(t, list.Items, 20)
	assert.LessOrEqual(t, int(api.maxSeen.Load()), 4)
	assert.Greater(t, int(api.maxSeen.Load()), 1)
	assert.Less(t, elapsed, 20*api.delay, "changes should be applied in parallel")
}

func TestApplyChanges_OverrunReturnsSoftErrorNotEOF(t *testing.T) {
	api := &slowAPI{delay: 100 * time.Millisecond}
	cfg := &Config{ApplyTimeout: 250 * time.Millisecond, ApplyConcurrency: 1}
	s, c := setupSlowServer(t, api, cfg)
	url := serveWebhook(t, s)

	client, err := webhook.NewWebhookProvider(url)
	require.NoError(t, err)

	err = client.ApplyChanges(context.Background(), createChanges(10))
	require.Error(t, err)
	assert.True(t, errors.Is(err, extdnsprovider.SoftError),
		"external-dns must see a soft error so it retries instead of exiting, got: %v", err)
	assert.NotContains(t, err.Error(), "EOF")

	var list dnsv1alpha1.DNSRecordSetList
	require.NoError(t, c.List(context.Background(), &list))
	assert.NotEmpty(t, list.Items)
	assert.Less(t, len(list.Items), 10)
}

func TestApplyChanges_OverrunResponseNamesTheLimit(t *testing.T) {
	api := &slowAPI{delay: 100 * time.Millisecond}
	cfg := &Config{ApplyTimeout: 250 * time.Millisecond, ApplyConcurrency: 1}
	s, _ := setupSlowServer(t, api, cfg)
	url := serveWebhook(t, s)

	body, err := jsonReader(createChanges(10))
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, url+"/records", body)
	require.NoError(t, err)
	req.Header.Set(ContentTypeHeader, MediaTypeFormatAndVersion)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "the server must answer rather than drop the connection")
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	msg, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(msg), "apply timeout of 250ms reached")
	assert.Contains(t, string(msg), "of 10 changes")
	assert.Contains(t, string(msg), "--apply-timeout")
}

func TestApplyChanges_DeleteRunsBeforeCreateOfSameRecordSet(t *testing.T) {
	name := GenerateRecordSetName("app.example.com", endpoint.RecordTypeA, "")
	existing := &dnsv1alpha1.DNSRecordSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    map[string]string{LabelOwner: "test-owner", LabelManagedBy: ManagedByValue},
		},
		Spec: dnsv1alpha1.DNSRecordSetSpec{
			DNSZoneRef: corev1.LocalObjectReference{Name: "example-com"},
			RecordType: dnsv1alpha1.RRTypeA,
		},
	}

	api := &slowAPI{delay: time.Millisecond}
	cfg := &Config{ApplyTimeout: 5 * time.Second, ApplyConcurrency: 4}
	s, c := setupSlowServer(t, api, cfg, existing)

	changes := &plan.Changes{
		Create: []*endpoint.Endpoint{
			{DNSName: "app.example.com", Targets: endpoint.Targets{"192.0.2.2"}, RecordType: endpoint.RecordTypeA},
			{DNSName: "new.example.com", Targets: endpoint.Targets{"192.0.2.3"}, RecordType: endpoint.RecordTypeA},
		},
		Delete: []*endpoint.Endpoint{
			{DNSName: "app.example.com", Targets: endpoint.Targets{"192.0.2.1"}, RecordType: endpoint.RecordTypeA},
			{DNSName: "old.example.com", Targets: endpoint.Targets{"192.0.2.4"}, RecordType: endpoint.RecordTypeA},
		},
	}
	require.NoError(t, s.provider.ApplyChanges(context.Background(), changes))

	var got dnsv1alpha1.DNSRecordSet
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, &got))
	require.Len(t, got.Spec.Records, 1)
	assert.Equal(t, "192.0.2.2", got.Spec.Records[0].A.Content)

	api.mu.Lock()
	defer api.mu.Unlock()
	require.Len(t, api.calls, 4)
	assert.Equal(t, "delete "+name, api.calls[0])
	assert.True(t, strings.HasPrefix(api.calls[3], "delete old-example-com-a-"),
		"unrelated deletes run after creates, got %v", api.calls)
}

func TestApplyChanges_PanicInOneChangeDoesNotAbortBatch(t *testing.T) {
	api := &slowAPI{
		delay:   time.Millisecond,
		panicOn: GenerateRecordSetName("app0.example.com", endpoint.RecordTypeA, ""),
	}
	cfg := &Config{ApplyTimeout: 5 * time.Second, ApplyConcurrency: 2}
	s, c := setupSlowServer(t, api, cfg)

	require.NoError(t, s.provider.ApplyChanges(context.Background(), createChanges(4)))

	var list dnsv1alpha1.DNSRecordSetList
	require.NoError(t, c.List(context.Background(), &list))
	assert.Len(t, list.Items, 3)
}
