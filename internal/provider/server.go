package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
)

const (
	// MediaTypeFormatAndVersion is the content type for the webhook API.
	MediaTypeFormatAndVersion = "application/external.dns.webhook+json;version=1"
	// ContentTypeHeader is the HTTP header for content type.
	ContentTypeHeader = "Content-Type"
)

// Server implements the ExternalDNS webhook HTTP server.
type Server struct {
	provider *Provider
	config   *Config

	webhookServer *http.Server
	metricsServer *http.Server
	startTime     time.Time
}

// NewServer creates a new webhook server.
func NewServer(provider *Provider, cfg *Config) *Server {
	return &Server{
		provider:  provider,
		config:    cfg,
		startTime: time.Now(),
	}
}

// Start starts both the webhook and metrics HTTP servers.
func (s *Server) Start(ctx context.Context) error {
	// Start metrics server - bind to 0.0.0.0 for health probe accessibility
	metricsAddr := fmt.Sprintf("0.0.0.0:%d", s.config.MetricsPort)
	metricsMux := http.NewServeMux()
	metricsMux.HandleFunc("/healthz", s.healthHandler)
	metricsMux.HandleFunc("/readyz", s.readyHandler)
	metricsMux.Handle("/metrics", promhttp.Handler())

	s.metricsServer = &http.Server{
		Addr:         metricsAddr,
		Handler:      metricsMux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	metricsListener, err := net.Listen("tcp", metricsAddr)
	if err != nil {
		return fmt.Errorf("failed to create metrics listener: %w", err)
	}

	go func() {
		log.Infof("Starting metrics server on %s", metricsAddr)
		if err := s.metricsServer.Serve(metricsListener); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Metrics server error: %v", err)
		}
	}()

	// Start webhook server
	webhookAddr := fmt.Sprintf("%s:%d", s.config.BindAddress, s.config.Port)
	webhookMux := http.NewServeMux()
	webhookMux.HandleFunc("/", s.instrumentHandler("/", s.negotiateHandler))
	webhookMux.HandleFunc("/records", s.instrumentHandler("/records", s.recordsHandler))
	webhookMux.HandleFunc("/adjustendpoints", s.instrumentHandler("/adjustendpoints", s.adjustEndpointsHandler))

	s.webhookServer = &http.Server{
		Addr:         webhookAddr,
		Handler:      webhookMux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	webhookListener, err := net.Listen("tcp", webhookAddr)
	if err != nil {
		return fmt.Errorf("failed to create webhook listener: %w", err)
	}

	log.Infof("Starting webhook server on %s", webhookAddr)
	if err := s.webhookServer.Serve(webhookListener); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("webhook server error: %w", err)
	}

	return nil
}

// Shutdown gracefully shuts down both servers.
func (s *Server) Shutdown(ctx context.Context) error {
	log.Info("Shutting down servers...")

	var webhookErr, metricsErr error

	if s.webhookServer != nil {
		webhookErr = s.webhookServer.Shutdown(ctx)
	}

	if s.metricsServer != nil {
		metricsErr = s.metricsServer.Shutdown(ctx)
	}

	if webhookErr != nil {
		return webhookErr
	}
	return metricsErr
}

// instrumentHandler wraps an HTTP handler with metrics collection.
func (s *Server) instrumentHandler(path string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a response writer wrapper to capture status code
		wrapper := &responseWriterWrapper{ResponseWriter: w, statusCode: http.StatusOK}

		// Call the actual handler
		handler(wrapper, r)

		// Record metrics
		duration := time.Since(start)
		status := strconv.Itoa(wrapper.statusCode)
		RecordHTTPRequest(r.Method, path, status, duration)
	}
}

// responseWriterWrapper wraps http.ResponseWriter to capture the status code.
type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode int
}

func (w *responseWriterWrapper) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

// negotiateHandler handles the GET / endpoint for content-type negotiation.
func (s *Server) negotiateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set(ContentTypeHeader, MediaTypeFormatAndVersion)
	w.WriteHeader(http.StatusOK)

	domainFilter := s.provider.GetDomainFilter()
	if err := json.NewEncoder(w).Encode(domainFilter); err != nil {
		log.Errorf("Failed to encode domain filter: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

// recordsHandler handles GET /records and POST /records endpoints.
func (s *Server) recordsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetRecords(w, r)
	case http.MethodPost:
		s.handleApplyChanges(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGetRecords retrieves current DNS records.
func (s *Server) handleGetRecords(w http.ResponseWriter, r *http.Request) {
	records, err := s.provider.Records(r.Context())
	if err != nil {
		log.Errorf("Failed to get records: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set(ContentTypeHeader, MediaTypeFormatAndVersion)
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(records); err != nil {
		log.Errorf("Failed to encode records: %v", err)
	}
}

// handleApplyChanges applies DNS record changes.
func (s *Server) handleApplyChanges(w http.ResponseWriter, r *http.Request) {
	var changes plan.Changes
	if err := json.NewDecoder(r.Body).Decode(&changes); err != nil {
		log.Errorf("Failed to decode changes: %v", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if err := s.provider.ApplyChanges(r.Context(), &changes); err != nil {
		log.Errorf("Failed to apply changes: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// adjustEndpointsHandler handles POST /adjustendpoints endpoint.
func (s *Server) adjustEndpointsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var endpoints []*endpoint.Endpoint
	if err := json.NewDecoder(r.Body).Decode(&endpoints); err != nil {
		log.Errorf("Failed to decode endpoints: %v", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	adjusted, err := s.provider.AdjustEndpoints(endpoints)
	if err != nil {
		log.Errorf("Failed to adjust endpoints: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set(ContentTypeHeader, MediaTypeFormatAndVersion)
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(adjusted); err != nil {
		log.Errorf("Failed to encode adjusted endpoints: %v", err)
	}
}

// healthHandler handles /healthz endpoint (liveness probe).
func (s *Server) healthHandler(w http.ResponseWriter, _ *http.Request) {
	response := map[string]string{"status": "ok"}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Errorf("Failed to write health response: %v", err)
	}
}

// readyHandler handles /readyz endpoint (readiness probe).
func (s *Server) readyHandler(w http.ResponseWriter, _ *http.Request) {
	zones := s.provider.Registry().ListZones()

	// Grace period for startup: allow 30 seconds to discover zones
	gracePeriod := 30 * time.Second
	timeSinceStart := time.Since(s.startTime)

	response := make(map[string]any)

	if len(zones) > 0 || timeSinceStart < gracePeriod {
		// Ready if we have zones or still within grace period
		response["status"] = "ready"
		response["zones"] = len(zones)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	} else {
		// Not ready if no zones and grace period expired
		response["status"] = "not_ready"
		response["reason"] = "no zones discovered"
		response["zones"] = 0

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Errorf("Failed to write ready response: %v", err)
	}
}
