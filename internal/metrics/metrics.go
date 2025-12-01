package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	RequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "bytefetch_http_requests_total",
			Help: "Total HTTP requests",
		},
		[]string{"path", "method", "status"},
	)
	RequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "bytefetch_http_request_duration_seconds",
			Help:    "HTTP request duration",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"path", "method"},
	)
	WorkerTasksTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "bytefetch_worker_tasks_total",
			Help: "Worker tasks processed",
		},
		[]string{"status"},
	)
	WorkerTaskDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "bytefetch_worker_task_duration_seconds",
			Help:    "Duration of worker task processing",
			Buckets: prometheus.DefBuckets,
		},
	)
	FetchDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "bytefetch_fetch_duration_seconds",
			Help:    "Duration of HTTP/JS fetch",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"host", "js"},
	)
	FetchHTTPStatus = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "bytefetch_fetch_http_status_total",
			Help: "HTTP fetch counts by status code",
		},
		[]string{"host", "status"},
	)
	ExtractDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "bytefetch_extract_duration_seconds",
			Help:    "Duration of extraction step",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"host"},
	)
	RenderDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "bytefetch_render_duration_seconds",
			Help:    "Duration of JS render via browser",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"host"},
	)
	BrowserRenderFailures = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "bytefetch_browser_render_failures_total",
			Help: "Count of render failures by host and stage",
		},
		[]string{"host", "stage"},
	)
	BrowserRestarts = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "bytefetch_browser_restarts_total",
			Help: "Total browser restart attempts",
		},
	)
	BrowserPoolAvailable = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "bytefetch_browser_pool_available",
			Help: "Current available pages in the browser pool",
		},
	)
	BrowserPoolInUse = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "bytefetch_browser_pool_in_use",
			Help: "Pages currently leased from the browser pool",
		},
	)
	BrowserHostInFlight = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "bytefetch_browser_host_inflight",
			Help: "Current JS render usage per host",
		},
		[]string{"host"},
	)
	BrowserHealth = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "bytefetch_browser_health",
			Help: "Browser health probe (1=ok,0=fail)",
		},
	)
	BrowserHealthChecksTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "bytefetch_browser_health_checks_total",
			Help: "Number of browser pool health checks executed",
		},
	)
	BrowserHealthFailuresTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "bytefetch_browser_health_failures_total",
			Help: "Number of browser health check failures",
		},
	)
	CleanupRunsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "bytefetch_cleanup_runs_total",
			Help: "Number of cleanup sweeps executed",
		},
	)
	CleanupErrorsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "bytefetch_cleanup_errors_total",
			Help: "Number of cleanup sweep errors",
		},
	)
)

func MustRegister() {
	prometheus.MustRegister(
		RequestsTotal,
		RequestDuration,
		WorkerTasksTotal,
		WorkerTaskDuration,
		FetchDuration,
		FetchHTTPStatus,
		ExtractDuration,
		RenderDuration,
		BrowserRenderFailures,
		BrowserRestarts,
		BrowserPoolAvailable,
		BrowserPoolInUse,
		BrowserHostInFlight,
		BrowserHealth,
		BrowserHealthChecksTotal,
		BrowserHealthFailuresTotal,
		CleanupRunsTotal,
		CleanupErrorsTotal,
	)
}
