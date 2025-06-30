package backend

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Metrics struct {
	// Метрики бэкендов
	BackendState         *prometheus.GaugeVec     // Текущее состояние бэкенда (1=UP, 0.5=PROBING, 0=DOWN)
	BackendRequestsTotal *prometheus.CounterVec   // Количество запросов к конкретным бэкендам
	BackendLatency       *prometheus.HistogramVec // Латентность запросов к бэкендам
	BackendBytesRead     *prometheus.CounterVec   // Количество прочитанных байт с бэкендов
	BackendBytesWrite    *prometheus.CounterVec   // Количество записанных байт в бэкендов
}

func NewMetrics() *Metrics {
	return &Metrics{
		// Метрики бэкендов
		BackendState: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "s3proxy_backend_state",
				Help: "Current state of a backend (1=UP, 0.5=PROBING, 0=DOWN)",
			},
			[]string{"backend"},
		),
		BackendRequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "s3proxy_backend_requests_total",
				Help: "Total number of requests sent to backends",
			},
			[]string{"backend", "method", "code"},
		),
		BackendLatency: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "s3proxy_backend_latency_seconds",
				Help:    "Latency of requests to backends in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"backend", "method"},
		),
		BackendBytesRead: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "s3proxy_backend_bytes_read_total",
				Help: "Total number of bytes read from backends",
			},
			[]string{"backend"},
		),
		BackendBytesWrite: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "s3proxy_backend_bytes_write_total",
				Help: "Total number of bytes wrote to backends",
			},
			[]string{"backend"},
		),
	}
}
