package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	// HTTPRequestsTotal counts total HTTP requests by method, path pattern, and status.
	HTTPRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests.",
		},
		[]string{"method", "path", "status"},
	)

	// HTTPRequestDuration records HTTP request latency in seconds.
	HTTPRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request latency in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)

	// StorageOperationsTotal counts storage backend operations.
	StorageOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "storage_operations_total",
			Help: "Total number of storage operations.",
		},
		[]string{"operation", "backend", "status"},
	)

	// GCQueueSize tracks the current GC queue depth.
	GCQueueSize = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "gc_queue_size",
			Help: "Current number of items in the GC queue.",
		},
	)

	// GCItemsProcessedTotal counts items successfully processed by the GC worker.
	GCItemsProcessedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gc_items_processed_total",
			Help: "Total number of items processed by garbage collection.",
		},
		[]string{"type"},
	)

	// GCItemsEnqueuedTotal counts items enqueued by each scanner phase.
	GCItemsEnqueuedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gc_items_enqueued_total",
			Help: "Total number of items enqueued by GC scanner phases.",
		},
		[]string{"phase"},
	)

	// GCErrorsTotal counts item processing failures in the GC worker.
	GCErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gc_errors_total",
			Help: "Total number of GC item processing errors.",
		},
		[]string{"type"},
	)

	// GCItemsSkippedTotal counts items skipped because they were re-referenced during grace period.
	GCItemsSkippedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "gc_items_skipped_total",
			Help: "Total number of GC items skipped (re-referenced during grace period).",
		},
	)

	// GCLastWorkerRun records the Unix timestamp of the last worker pass.
	GCLastWorkerRun = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "gc_last_worker_run_timestamp_seconds",
			Help: "Unix timestamp of the last GC worker run.",
		},
	)

	// GCLastScannerRun records the Unix timestamp of the last scanner pass.
	GCLastScannerRun = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "gc_last_scanner_run_timestamp_seconds",
			Help: "Unix timestamp of the last GC scanner run.",
		},
	)

	// GCScannerLastPhaseRun records the Unix timestamp of the last run of each scanner phase.
	GCScannerLastPhaseRun = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gc_scanner_last_phase_run_timestamp_seconds",
			Help: "Unix timestamp of the last run of each GC scanner phase.",
		},
		[]string{"phase"},
	)

	// GCWorkerDuration observes the duration of each GC worker pass.
	GCWorkerDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "gc_worker_duration_seconds",
			Help:    "Duration of GC worker passes in seconds.",
			Buckets: prometheus.DefBuckets,
		},
	)

	// GCScannerDuration observes the duration of each GC scanner pass.
	GCScannerDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "gc_scanner_duration_seconds",
			Help:    "Duration of GC scanner passes in seconds.",
			Buckets: prometheus.DefBuckets,
		},
	)

	// ActiveSessions tracks the number of active user sessions.
	ActiveSessions = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "active_sessions_total",
			Help: "Current number of active user sessions.",
		},
	)
)

// Register registers all custom metrics with the default Prometheus registry.
func Register() {
	prometheus.MustRegister(
		HTTPRequestsTotal,
		HTTPRequestDuration,
		StorageOperationsTotal,
		GCQueueSize,
		GCItemsProcessedTotal,
		GCItemsEnqueuedTotal,
		GCErrorsTotal,
		GCItemsSkippedTotal,
		GCLastWorkerRun,
		GCLastScannerRun,
		GCScannerLastPhaseRun,
		GCWorkerDuration,
		GCScannerDuration,
		ActiveSessions,
	)
}
