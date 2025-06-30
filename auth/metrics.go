package auth

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Metrics struct {
	// Общие метрики запросов
	AuthRequestsTotal *prometheus.CounterVec   // Количество запросов аутентификации
	AuthLatency       *prometheus.HistogramVec // Латентность аутентификации
}

func NewMetrics() *Metrics {
	return &Metrics{
		// Общие метрики запросов
		AuthRequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "s3proxy_auth_requests_total",
				Help: "Total number of authentication requests",
			},
			[]string{"result"}, // success/failure
		),
		AuthLatency: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "s3proxy_auth_latency_seconds",
				Help:    "Latency of authentication requests in seconds",
				Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0}, // Более мелкие бакеты для аутентификации
			},
			[]string{"result"},
		),
	}
}
