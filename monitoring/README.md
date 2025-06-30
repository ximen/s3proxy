# Monitoring Module

Модуль мониторинга предоставляет инфраструктуру для сбора и экспорта метрик в формате Prometheus. Модуль подготавливает все необходимые структуры данных и HTTP-эндпоинты, но пока не реализует сбор самих метрик (это будет отдельной задачей).

## Назначение и зона ответственности

**Ключевые обязанности:**
- Инициализация и регистрация всех метрик Prometheus при старте приложения
- Предоставление единого интерфейса для обновления метрик другими модулями
- Запуск HTTP-сервера для экспорта метрик на отдельном порту
- Сбор базовых системных метрик (использование памяти)
- Предоставление health check эндпоинта

**Вне зоны ответственности:**
- Реализация бизнес-логики сбора метрик (делают другие модули)
- Хранение исторических данных (делает Prometheus)
- Алертинг и уведомления (делает Alertmanager)

## Архитектура

### Компоненты

1. **Metrics** - структура, содержащая все метрики приложения
2. **Server** - HTTP сервер для экспорта метрик
3. **Monitor** - основной интерфейс модуля
4. **Config** - конфигурация модуля

### Типы метрик

#### Общие метрики запросов
- `s3proxy_requests_total` - общее количество S3 запросов
- `s3proxy_request_latency_seconds` - латентность S3 запросов

#### Метрики бэкендов
- `s3proxy_backend_state` - состояние бэкенда (1=UP, 0.5=PROBING, 0=DOWN)
- `s3proxy_backend_requests_total` - количество запросов к бэкендам
- `s3proxy_backend_latency_seconds` - латентность запросов к бэкендам

#### Метрики кэширования
- `s3proxy_cache_hits_total` - количество попаданий в кэш
- `s3proxy_cache_misses_total` - количество промахов кэша
- `s3proxy_cache_size_bytes` - размер кэша

#### Метрики аутентификации
- `s3proxy_auth_requests_total` - количество запросов аутентификации
- `s3proxy_auth_latency_seconds` - латентность аутентификации

#### Метрики репликации
- `s3proxy_replication_requests_total` - количество запросов репликации
- `s3proxy_replication_latency_seconds` - латентность репликации

#### Системные метрики
- `s3proxy_active_connections` - количество активных соединений
- `s3proxy_memory_usage_bytes` - использование памяти

## Использование

### Создание и запуск

```go
import "s3proxy/monitoring"

// Создание с конфигурацией по умолчанию
monitor, err := monitoring.New(nil)
if err != nil {
    log.Fatal(err)
}

// Запуск модуля
err = monitor.Start()
if err != nil {
    log.Fatal(err)
}

// Получение метрик для использования в других модулях
metrics := monitor.GetMetrics()

// Остановка модуля
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
err = monitor.Stop(ctx)
```

### Конфигурация

```go
config := &monitoring.Config{
    Enabled:               true,
    ListenAddress:         ":9091",
    MetricsPath:           "/metrics",
    ReadTimeout:           30 * time.Second,
    WriteTimeout:          30 * time.Second,
    EnableSystemMetrics:   true,
    SystemMetricsInterval: 15 * time.Second,
}

monitor, err := monitoring.New(config)
```

### YAML конфигурация

```yaml
monitoring:
  enabled: true
  listen_address: ":9091"
  metrics_path: "/metrics"
  read_timeout: 30s
  write_timeout: 30s
  enable_system_metrics: true
  system_metrics_interval: 15s
```

### Использование метрик в других модулях

```go
// В конструкторе модуля принимаем метрики
func NewAPIGateway(config *Config, metrics *monitoring.Metrics) *APIGateway {
    return &APIGateway{
        config:  config,
        metrics: metrics,
    }
}

// В коде модуля обновляем метрики
func (gw *APIGateway) handleRequest(req *S3Request) *S3Response {
    start := time.Now()
    
    // Обрабатываем запрос...
    resp := gw.processRequest(req)
    
    // Обновляем метрики
    if gw.metrics != nil {
        duration := time.Since(start).Seconds()
        operation := req.Operation.String()
        code := fmt.Sprintf("%d", resp.StatusCode)
        
        gw.metrics.RequestsTotal.WithLabelValues(operation, code).Inc()
        gw.metrics.RequestLatency.WithLabelValues(operation).Observe(duration)
    }
    
    return resp
}
```

## HTTP эндпоинты

### Метрики
- **URL:** `http://localhost:9091/metrics`
- **Метод:** GET
- **Описание:** Экспорт всех метрик в формате Prometheus

### Health Check
- **URL:** `http://localhost:9091/health`
- **Метод:** GET
- **Описание:** Проверка состояния модуля мониторинга

## Интеграция с Prometheus

### Конфигурация Prometheus

```yaml
# prometheus.yml
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: 's3proxy'
    static_configs:
      - targets: ['localhost:9091']
    scrape_interval: 10s
    metrics_path: /metrics
```

### Пример метрик

```
# HELP s3proxy_requests_total Total number of processed S3 requests
# TYPE s3proxy_requests_total counter
s3proxy_requests_total{code="200",operation="GET_OBJECT"} 1523
s3proxy_requests_total{code="404",operation="GET_OBJECT"} 45
s3proxy_requests_total{code="200",operation="PUT_OBJECT"} 892

# HELP s3proxy_backend_state Current state of a backend (1=UP, 0.5=PROBING, 0=DOWN)
# TYPE s3proxy_backend_state gauge
s3proxy_backend_state{backend_id="aws-us-east-1"} 1
s3proxy_backend_state{backend_id="wasabi-eu-central-1"} 0.5

# HELP s3proxy_memory_usage_bytes Current memory usage in bytes
# TYPE s3proxy_memory_usage_bytes gauge
s3proxy_memory_usage_bytes 67108864
```

## Тестирование

```bash
# Запуск тестов модуля
go test -v ./monitoring

# Запуск тестов с покрытием
go test -v -cover ./monitoring

# Проверка метрик вручную
curl http://localhost:9091/metrics

# Проверка health check
curl http://localhost:9091/health
```

## Расширяемость

### Добавление новой метрики

1. Добавить поле в структуру `Metrics`:
```go
type Metrics struct {
    // ... существующие метрики
    NewMetric prometheus.Counter `// Новая метрика`
}
```

2. Инициализировать в `NewMetrics()`:
```go
NewMetric: promauto.NewCounter(
    prometheus.CounterOpts{
        Name: "s3proxy_new_metric_total",
        Help: "Description of new metric",
    },
),
```

3. Использовать в коде:
```go
metrics.NewMetric.Inc()
```

### Добавление системных метрик

Расширить метод `updateSystemMetrics()` в `server.go`:

```go
func (s *Server) updateSystemMetrics() {
    // Существующие метрики...
    
    // Новые системные метрики
    s.metrics.CPUUsage.Set(getCPUUsage())
    s.metrics.DiskUsage.Set(getDiskUsage())
}
```

## Зависимости

- `github.com/prometheus/client_golang` - клиентская библиотека Prometheus
- `s3proxy/logger` - система логирования

## Производительность

- Метрики хранятся в памяти и обновляются атомарно
- HTTP сервер работает на отдельном порту, не влияя на основной трафик
- Системные метрики собираются с настраиваемым интервалом
- Минимальные накладные расходы на обновление метрик
