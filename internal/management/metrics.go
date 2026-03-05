package management

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds all Prometheus metrics for the proxy.
type Metrics struct {
	RequestsTotal   *prometheus.CounterVec
	RequestDuration *prometheus.HistogramVec
	ActiveStreams    prometheus.Gauge
	DroppedLogs     prometheus.Counter
	DBWriteErrors   prometheus.Counter
}

// NewMetrics registers all metrics and returns the struct.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		RequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "memoryelaine_requests_total",
			Help: "Total number of proxied requests",
		}, []string{"method", "path", "status"}),
		RequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "memoryelaine_request_duration_seconds",
			Help:    "Request duration in seconds",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "path"}),
		ActiveStreams: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "memoryelaine_active_streams",
			Help: "Number of currently active streaming connections",
		}),
		DroppedLogs: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "memoryelaine_dropped_logs_total",
			Help: "Total number of dropped log entries due to full queue",
		}),
		DBWriteErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "memoryelaine_db_write_errors_total",
			Help: "Total number of database write errors",
		}),
	}

	reg.MustRegister(m.RequestsTotal, m.RequestDuration, m.ActiveStreams, m.DroppedLogs, m.DBWriteErrors)
	return m
}

func metricsHandler() http.Handler {
	return promhttp.Handler()
}
