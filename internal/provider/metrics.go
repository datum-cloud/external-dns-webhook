package provider

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Zone discovery metrics
	zonesDiscovered = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "datum_dns",
			Name:      "zones_discovered",
			Help:      "Number of DNSZone resources discovered",
		},
		[]string{"namespace"},
	)

	// RecordSet metrics
	recordsetsManaged = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "datum_dns",
			Name:      "recordsets_managed",
			Help:      "Number of DNSRecordSet resources managed",
		},
		[]string{"namespace", "record_type"},
	)

	// Operation metrics
	operationsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "datum_dns",
			Name:      "operations_total",
			Help:      "Total number of DNS operations",
		},
		[]string{"operation", "status"}, // operation: create, update, delete; status: success, error
	)

	// Translation errors
	translationErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "datum_dns",
			Name:      "translation_errors_total",
			Help:      "Total number of endpoint translation errors",
		},
		[]string{"error_type"},
	)

	// Ownership conflicts
	ownershipConflicts = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "datum_dns",
			Name:      "ownership_conflicts_total",
			Help:      "Total number of ownership conflicts encountered",
		},
	)

	// HTTP request metrics
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "datum_dns",
			Name:      "http_requests_total",
			Help:      "Total number of HTTP requests",
		},
		[]string{"method", "path", "status"},
	)

	httpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "datum_dns",
			Name:      "http_request_duration_seconds",
			Help:      "HTTP request duration in seconds",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)
)

// RecordZonesDiscovered records the number of zones discovered per namespace.
func RecordZonesDiscovered(namespace string, count int) {
	zonesDiscovered.WithLabelValues(namespace).Set(float64(count))
}

// RecordRecordsetsManaged records the number of recordsets managed per namespace and record type.
func RecordRecordsetsManaged(namespace, recordType string, count int) {
	recordsetsManaged.WithLabelValues(namespace, recordType).Set(float64(count))
}

// RecordOperation records a DNS operation with its status.
func RecordOperation(operation, status string) {
	operationsTotal.WithLabelValues(operation, status).Inc()
}

// RecordTranslationError records an endpoint translation error.
func RecordTranslationError(errorType string) {
	translationErrors.WithLabelValues(errorType).Inc()
}

// RecordOwnershipConflict records an ownership conflict.
func RecordOwnershipConflict() {
	ownershipConflicts.Inc()
}

// RecordHTTPRequest records an HTTP request with its duration and status.
func RecordHTTPRequest(method, path, status string, duration time.Duration) {
	httpRequestsTotal.WithLabelValues(method, path, status).Inc()
	httpRequestDuration.WithLabelValues(method, path).Observe(duration.Seconds())
}
