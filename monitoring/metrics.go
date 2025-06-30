package monitoring

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics - единая структура для хранения всех метрик приложения.
// Экземпляр этой структуры создается при старте и передается во все
// модули, которым нужно обновлять метрики.
type Metrics struct {
	// Метрики кэширования (для будущего использования)
	CacheHitsTotal   prometheus.Counter // Количество попаданий в кэш
	CacheMissesTotal prometheus.Counter // Количество промахов кэша
	CacheSize        prometheus.Gauge   // Текущий размер кэша

	// Метрики репликации
	ReplicationRequestsTotal *prometheus.CounterVec   // Количество запросов репликации
	ReplicationLatency       *prometheus.HistogramVec // Латентность репликации

	// Системные метрики
	ActiveConnections prometheus.Gauge // Количество активных соединений
	MemoryUsage       prometheus.Gauge // Использование памяти
}

// NewMetrics создает и регистрирует все метрики в Prometheus.
// Использует promauto для автоматической регистрации метрик в default registry.
func NewMetrics() *Metrics {
	return &Metrics{
		// Метрики кэширования
		CacheHitsTotal: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "s3proxy_cache_hits_total",
				Help: "Total number of cache hits",
			},
		),
		CacheMissesTotal: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "s3proxy_cache_misses_total",
				Help: "Total number of cache misses",
			},
		),
		CacheSize: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "s3proxy_cache_size_bytes",
				Help: "Current cache size in bytes",
			},
		),

		// Метрики репликации
		ReplicationRequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "s3proxy_replication_requests_total",
				Help: "Total number of replication requests",
			},
			[]string{"operation", "ack_level", "result"}, // put/delete, one/all, success/failure
		),
		ReplicationLatency: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "s3proxy_replication_latency_seconds",
				Help:    "Latency of replication requests in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"operation", "ack_level"},
		),

		// Сиситемные метрки
		ActiveConnections: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "s3proxy_active_connections",
				Help: "Number of active connections",
			},
		),
		MemoryUsage: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "s3proxy_memory_usage_bytes",
				Help: "Current memory usage in bytes",
			},
		),
	}
}

// GetRegistry возвращает default Prometheus registry.
// Это может быть полезно для тестирования или кастомной настройки.
func GetRegistry() *prometheus.Registry {
	return prometheus.DefaultRegisterer.(*prometheus.Registry)
}
