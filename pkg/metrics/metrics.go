package metrics

import (
	"net/http"
	"runtime"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const ApplicationName = "comment-api"

var startTime = time.Now()

// HttpServerRequestsSeconds is a Spring Boot Micrometer compatible HTTP request duration histogram.
var HttpServerRequestsSeconds = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "http_server_requests_seconds",
		Help:    "Duration of HTTP server requests in seconds",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		ConstLabels: prometheus.Labels{
			"application": ApplicationName,
		},
	},
	[]string{"method", "uri", "status", "outcome"},
)

// process_uptime_seconds — Spring Boot compatible process uptime gauge.
var _ = promauto.NewGaugeFunc(
	prometheus.GaugeOpts{
		Name:        "process_uptime_seconds",
		Help:        "Process uptime in seconds",
		ConstLabels: prometheus.Labels{"application": ApplicationName},
	},
	func() float64 {
		return time.Since(startTime).Seconds()
	},
)

// jvm_memory_used_bytes — Maps Go heap allocation to JVM heap for dashboard compatibility.
var _ = promauto.NewGaugeFunc(
	prometheus.GaugeOpts{
		Name: "jvm_memory_used_bytes",
		Help: "Used memory bytes (Go heap mapped for Spring Boot dashboard compatibility)",
		ConstLabels: prometheus.Labels{
			"application": ApplicationName,
			"area":        "heap",
			"id":          "Go-Heap",
		},
	},
	func() float64 {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return float64(m.Alloc)
	},
)

// jvm_memory_max_bytes — Maps Go system memory allocation for dashboard compatibility.
var _ = promauto.NewGaugeFunc(
	prometheus.GaugeOpts{
		Name: "jvm_memory_max_bytes",
		Help: "Max memory bytes (Go Sys mapped for Spring Boot dashboard compatibility)",
		ConstLabels: prometheus.Labels{
			"application": ApplicationName,
			"area":        "heap",
			"id":          "Go-Heap",
		},
	},
	func() float64 {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return float64(m.Sys)
	},
)

// SystemCPUUsage is an approximation of system CPU usage (precise measurement is limited in Go runtime).
var SystemCPUUsage = promauto.NewGauge(prometheus.GaugeOpts{
	Name:        "system_cpu_usage",
	Help:        "System CPU usage (approximation)",
	ConstLabels: prometheus.Labels{"application": ApplicationName},
})

// ProcessCPUUsage is an approximation of process CPU usage.
var ProcessCPUUsage = promauto.NewGauge(prometheus.GaugeOpts{
	Name:        "process_cpu_usage",
	Help:        "Process CPU usage (approximation)",
	ConstLabels: prometheus.Labels{"application": ApplicationName},
})

// LogEventsTotal maps slog levels to logback-compatible log event counters.
var LogEventsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name:        "logback_events_total",
		Help:        "Log events total by level (mapped from slog for Spring Boot dashboard compatibility)",
		ConstLabels: prometheus.Labels{"application": ApplicationName},
	},
	[]string{"level"},
)

// jvm_threads_live_threads — Maps Go goroutine count for dashboard compatibility.
var _ = promauto.NewGaugeFunc(
	prometheus.GaugeOpts{
		Name:        "jvm_threads_live_threads",
		Help:        "Live threads (mapped from Go goroutines for dashboard compatibility)",
		ConstLabels: prometheus.Labels{"application": ApplicationName},
	},
	func() float64 {
		return float64(runtime.NumGoroutine())
	},
)

// httpOutcome maps an HTTP status code to Spring Boot's outcome label value.
func httpOutcome(statusCode int) string {
	switch {
	case statusCode >= 100 && statusCode < 200:
		return "INFORMATIONAL"
	case statusCode >= 200 && statusCode < 300:
		return "SUCCESS"
	case statusCode >= 300 && statusCode < 400:
		return "REDIRECTION"
	case statusCode >= 400 && statusCode < 500:
		return "CLIENT_ERROR"
	default:
		return "SERVER_ERROR"
	}
}

// statusRecorder is a ResponseWriter wrapper that captures the HTTP status code.
type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

// Unwrap returns the underlying ResponseWriter for http.ResponseController support.
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// InstrumentHandler is a middleware that instruments HTTP requests with Prometheus metrics.
func InstrumentHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip instrumentation for the /metrics endpoint itself
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(recorder, r)

		duration := time.Since(start).Seconds()
		status := strconv.Itoa(recorder.statusCode)

		// Use Go 1.22+ r.Pattern for route pattern extraction (limits label cardinality)
		uri := r.Pattern
		if uri == "" {
			uri = r.URL.Path
		}

		HttpServerRequestsSeconds.WithLabelValues(
			r.Method,
			uri,
			status,
			httpOutcome(recorder.statusCode),
		).Observe(duration)
	})
}

// Handler returns the HTTP handler for the /metrics endpoint.
func Handler() http.Handler {
	return promhttp.Handler()
}
