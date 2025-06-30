### Задание на разработку: Модуль Мониторинга (Prometheus Exporter)

Давайте формализуем это в виде задания. Этот модуль будет не столько самостоятельным, сколько сквозным — его будут использовать все остальные модули.

**1. Назначение и Зона ответственности**

Модуль Мониторинга отвечает за инициализацию, хранение и предоставление метрик приложения в формате, совместимом с Prometheus.

*   **Инициализация**: Создание и регистрация всех необходимых метрик (счетчиков, калибров, гистограмм) при старте приложения.
*   **Предоставление интерфейса**: Предоставление удобной структуры или методов, через которые другие модули (API Gateway, Backend Manager и т.д.) могут обновлять значения метрик.
*   **Экспорт**: Запуск HTTP-эндпоинта `/metrics` на отдельном порту для сбора метрик сервером Prometheus.

**2. Структуры данных и реализация (Go)**

```go
// Файл: monitoring/metrics.go

package monitoring

import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics - это единая структура для хранения всех метрик приложения.
// Экземпляр этой структуры будет создан при старте и передан во все
// модули, которым нужно обновлять метрики.
type Metrics struct {
    RequestsTotal      *prometheus.CounterVec
    RequestLatency     *prometheus.HistogramVec
    BackendState       *prometheus.GaugeVec
    BackendRequestsTotal *prometheus.CounterVec
    CacheHitsTotal     prometheus.Counter
    CacheMissesTotal   prometheus.Counter
}

// NewMetrics создает и регистрирует все метрики в Prometheus.
func NewMetrics() *Metrics {
    return &Metrics{
        RequestsTotal: promauto.NewCounterVec(
            prometheus.CounterOpts{
                Name: "s3proxy_requests_total",
                Help: "Total number of processed S3 requests.",
            },
            []string{"operation", "code"}, // Метки: put/get/list, 200/404/500
        ),
        RequestLatency: promauto.NewHistogramVec(
            prometheus.HistogramOpts{
                Name:    "s3proxy_request_latency_seconds",
                Help:    "Latency of S3 requests.",
                Buckets: prometheus.DefBuckets, // Стандартные бакеты времени
            },
            []string{"operation"},
        ),
        BackendState: promauto.NewGaugeVec(
            prometheus.GaugeOpts{
                Name: "s3proxy_backend_state",
                Help: "Current state of a backend (1=UP, 0.5=PROBING, 0=DOWN).",
            },
            []string{"backend_id"},
        ),
        // ... и так далее для остальных метрик
    }
}
```

**3. Запуск HTTP-эндпоинта**

Это делается в главной функции приложения `main.go`.

```go
// Файл: cmd/s3proxy/main.go

import (
    "net/http"
    "github.com/prometheus/client_golang/prometheus/promhttp"
    "log"
)

func main() {
    // ... инициализация конфига, логгера, всех модулей ...
    
    // Создаем экземпляр метрик
    metrics := monitoring.NewMetrics()

    // Передаем metrics в конструкторы других модулей
    // backendManager := backend.NewManager(config.Backends, metrics)
    // apiGateway := api.NewGateway(config.API, ..., metrics)

    // Запускаем сервер для метрик в отдельной горутине
    go func() {
        metricsAddr := ":9091" // Порт для метрик
        log.Printf("Starting metrics server on %s", metricsAddr)
        
        // Создаем отдельный роутер для метрик
        mux := http.NewServeMux()
        mux.Handle("/metrics", promhttp.Handler())
        
        if err := http.ListenAndServe(metricsAddr, mux); err != nil {
            log.Fatalf("Failed to start metrics server: %v", err)
        }
    }()

    // ... запуск основного сервера S3-прокси на порту 9000 ...
}
```

**4. Ответственность разработчика**

1.  Создать пакет `monitoring`.
2.  Внутри него определить структуру `Metrics`, содержащую все необходимые приложению метрики (`CounterVec`, `GaugeVec`, `HistogramVec`) с правильными именами, справкой (`Help`) и метками (`labels`).
3.  Реализовать конструктор `NewMetrics()`, который использует `promauto` для автоматической регистрации метрик.
4.  В `main.go` обеспечить запуск HTTP-сервера для метрик на отдельном, конфигурируемом порту.
5.  Обеспечить, чтобы экземпляр `Metrics` был доступен всем модулям, которые должны обновлять метрики (через dependency injection).
6.  В коде соответствующих модулей добавить вызовы для обновления метрик. Например, в `BackendManager` при смене статуса бэкенда вызывать `metrics.BackendState.WithLabelValues(backend.ID).Set(...)`. В `API Gateway` после обработки запроса — `metrics.RequestsTotal.WithLabelValues(...).Inc()` и `metrics.RequestLatency.WithLabelValues(...).Observe(...)`.
