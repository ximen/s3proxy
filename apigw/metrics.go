package apigw

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Metrics struct {
	// Общие метрики запросов
	RequestsTotal  *prometheus.CounterVec   // Общее количество обработанных S3 запросов
	RequestLatency *prometheus.HistogramVec // Латентность S3 запросов
}

func NewMetrics() *Metrics {
	return &Metrics{
		// Общие метрики запросов
		RequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "s3proxy_apigw_requests_total",
				Help: "Total number of processed S3 requests",
			},
			[]string{"method", "code"},
		),
		RequestLatency: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "s3proxy_apigw_request_latency_seconds",
				Help:    "Latency of S3 requests in seconds",
				Buckets: prometheus.DefBuckets, // Стандартные бакеты времени
			},
			[]string{"method"},
		),
	}
}

// // ObserveBackendRequestLatency записывает время выполнения запроса к бэкенду
// func (m *Metrics) ObserveBackendRequestLatency(backendID, operation string, latency float64) {
// 	m.BackendLatency.WithLabelValues(backendID, operation).Observe(latency)
// }
