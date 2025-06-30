# Replication Module

Модуль репликации отвечает за выполнение операций записи (`PUT`, `DELETE`, multipart upload) на одном или нескольких S3-бэкендах с поддержкой различных политик подтверждения.

## Назначение и зона ответственности

**Ключевые обязанности:**
- Реализация интерфейса `ReplicationExecutor` для операций записи
- Получение списка живых бэкендов от Backend Manager
- Параллельное выполнение операций на всех целевых бэкендах
- Реализация политик подтверждения (`ack=none/one/all`)
- Потоковая передача данных без полной буферизации в памяти
- Агрегация ответов от бэкендов
- Обратная связь с Backend Manager о результатах операций
- Сбор детальных метрик по каждому бэкенду и операции

**Вне зоны ответственности:**
- Принятие решений о политиках (делает Policy & Routing Engine)
- Аутентификация и авторизация
- Отслеживание состояния бэкендов (только потребляет информацию)

## Архитектура

### Компоненты

1. **Replicator** - основной исполнитель, реализующий `ReplicationExecutor`
2. **Fan-Out/Fan-In Logic** - параллельное выполнение и агрегация результатов
3. **Request Cloner** - клонирование `io.Reader` для отправки на несколько бэкендов
4. **Multipart Store** - управление маппингами multipart upload
5. **Metrics Collector** - сбор метрик производительности

### Политики подтверждения

- **`ack=none`** - немедленный возврат успеха, репликация в фоне
- **`ack=one`** - возврат успеха после первого успешного бэкенда
- **`ack=all`** - возврат успеха только после успеха на всех бэкендах

## Использование

### Создание и настройка

```go
import "s3proxy/replicator"

// Создание конфигурации
config := &replicator.Config{
    MultipartUploadTTL:      24 * time.Hour,
    CleanupInterval:         1 * time.Hour,
    MaxConcurrentOperations: 100,
    OperationTimeout:        30 * time.Second,
    RetryAttempts:           3,
    RetryDelay:              1 * time.Second,
    BufferSize:              32 * 1024,
}

// Создание репликатора
replicator := replicator.NewReplicator(backendProvider, metrics, config)
defer replicator.Stop()
```

### Конфигурация

#### Структура Config

```go
type Config struct {
    MultipartUploadTTL      time.Duration // Время жизни multipart маппингов
    CleanupInterval         time.Duration // Интервал очистки устаревших маппингов
    MaxConcurrentOperations int           // Максимум одновременных операций
    OperationTimeout        time.Duration // Таймаут операций с бэкендами
    RetryAttempts           int           // Количество попыток повтора
    RetryDelay              time.Duration // Задержка между попытками
    BufferSize              int           // Размер буфера для потоков
}
```

#### YAML конфигурация

```yaml
replicator:
  multipart_upload_ttl: "24h"
  cleanup_interval: "1h"
  max_concurrent_operations: 100
  operation_timeout: "30s"
  retry_attempts: 3
  retry_delay: "1s"
  buffer_size: 32768
```

## Поддерживаемые операции

### PUT Object

```go
response := replicator.PutObject(ctx, req, policy)
```

**Особенности:**
- Потоковая передача данных через `io.Reader`
- Клонирование потока для каждого бэкенда
- Подсчет переданных байт
- Поддержка всех политик `ack`

### DELETE Object

```go
response := replicator.DeleteObject(ctx, req, policy)
```

**Особенности:**
- Параллельное удаление на всех бэкендах
- Идемпотентная операция
- Поддержка всех политик `ack`

### Multipart Upload

#### Инициация

```go
response := replicator.CreateMultipartUpload(ctx, req, policy)
```

**Логика:**
1. Создание multipart upload на всех живых бэкендах
2. Сбор всех `uploadId` от бэкендов
3. Генерация уникального `ProxyUploadId`
4. Сохранение маппинга `ProxyUploadId -> {backend: uploadId}`
5. Возврат `ProxyUploadId` клиенту

#### Загрузка части

```go
response := replicator.UploadPart(ctx, req, policy)
```

**Логика:**
1. Поиск маппинга по `ProxyUploadId`
2. Клонирование данных части для каждого бэкенда
3. Параллельная загрузка на все бэкенды из маппинга
4. Агрегация результатов согласно политике

#### Завершение

```go
response := replicator.CompleteMultipartUpload(ctx, req, policy)
```

**Логика:**
1. Поиск маппинга по `ProxyUploadId`
2. Параллельное завершение на всех бэкендах
3. Удаление маппинга после завершения
4. Критическая операция - рекомендуется `ack=all`

#### Отмена

```go
response := replicator.AbortMultipartUpload(ctx, req, policy)
```

**Логика:**
1. Поиск маппинга по `ProxyUploadId`
2. Параллельная отмена на всех бэкендах
3. Удаление маппинга
4. Идемпотентная операция

## Клонирование потоков

### PipeReaderCloner

Реализует клонирование `io.Reader` через `io.Pipe`:

```go
cloner := &PipeReaderCloner{}
readers, err := cloner.Clone(originalReader, backendCount)

// Каждый reader содержит копию данных
for i, reader := range readers {
    go sendToBackend(backends[i], reader)
}
```

**Принцип работы:**
1. Создание `io.Pipe` для каждого бэкенда
2. Использование `io.MultiWriter` для записи во все pipes
3. Горутина копирует данные из исходного reader во все pipes
4. Каждый бэкенд получает независимый reader

## Multipart Store

### Управление маппингами

```go
// Создание маппинга
proxyUploadID, err := store.CreateMapping(bucket, key, backendUploads)

// Получение маппинга
mapping, exists := store.GetMapping(proxyUploadID)

// Удаление маппинга
store.DeleteMapping(proxyUploadID)
```

### Автоматическая очистка

- Фоновая горутина очищает устаревшие маппинги
- Настраиваемый TTL и интервал очистки
- Graceful shutdown с ожиданием завершения

## Метрики

### Собираемые метрики

```
# Латентность запросов к бэкендам
s3proxy_backend_latency_seconds{backend_id,operation}

# Количество запросов к бэкендам
s3proxy_backend_requests_total{backend_id,operation,result}

# Состояние бэкендов (через Backend Manager)
s3proxy_backend_state{backend_id}
```

### Обновление метрик

Метрики обновляются автоматически после каждой операции:

```go
// Латентность
metrics.BackendLatency.WithLabelValues(backendID, operation).Observe(duration.Seconds())

// Счетчик запросов
status := "success" // или "error"
metrics.BackendRequestsTotal.WithLabelValues(backendID, operation, status).Inc()
```

## Обратная связь с Backend Manager

После каждой операции репликатор сообщает результат:

```go
if err != nil {
    backendProvider.ReportFailure(backendID, err)
} else {
    backendProvider.ReportSuccess(backendID)
}
```

Это позволяет Backend Manager:
- Обновлять состояние бэкендов
- Срабатывать Circuit Breaker при множественных ошибках
- Переводить бэкенды в состояние DOWN при проблемах

## Обработка ошибок

### Retry логика

```go
for attempt := 0; attempt <= config.RetryAttempts; attempt++ {
    if attempt > 0 {
        time.Sleep(config.RetryDelay)
    }
    
    response, err = backend.S3Client.Operation(ctx, input)
    if err == nil {
        break
    }
}
```

### Таймауты

Каждая операция выполняется с контекстом и таймаутом:

```go
ctx, cancel := context.WithTimeout(ctx, config.OperationTimeout)
defer cancel()
```

### Агрегация ошибок

- Для `ack=one`: возврат ошибки только если все бэкенды неуспешны
- Для `ack=all`: возврат ошибки если хотя бы один бэкенд неуспешен
- Для `ack=none`: ошибки логируются, но не влияют на ответ

## Производительность

### Оптимизации

- **Потоковая обработка**: данные не буферизуются полностью в памяти
- **Параллельное выполнение**: все бэкенды обрабатываются одновременно
- **Семафор**: ограничение количества одновременных операций
- **Переиспользование соединений**: AWS SDK управляет пулом соединений

### Мониторинг производительности

- Метрики латентности по каждому бэкенду
- Счетчики успешных/неуспешных операций
- Подсчет переданных байт
- Время выполнения операций

## Тестирование

```bash
# Запуск тестов
go test -v ./replicator

# Запуск тестов с покрытием
go test -v -cover ./replicator

# Запуск конкретного теста
go test -v -run TestPutObject ./replicator
```

### Mock объекты

Для тестирования используются mock объекты:

```go
// Mock Backend Provider
provider := NewMockBackendProvider(3) // 3 бэкенда

// Mock метрики
metrics := &monitoring.Metrics{}

// Создание репликатора для тестов
replicator := NewReplicator(provider, metrics, config)
```

## Интеграция

### С Policy & Routing Engine

```go
// В routing engine
replicator := replicator.NewReplicator(backendProvider, metrics, config)

// Обработка PUT запроса
response := replicator.PutObject(ctx, req, policy)
```

### С Backend Manager

```go
// Backend Manager предоставляет живые бэкенды
liveBackends := backendProvider.GetLiveBackends()

// Репликатор сообщает о результатах
backendProvider.ReportSuccess(backendID)
backendProvider.ReportFailure(backendID, err)
```

## Расширяемость

### Добавление новых операций

1. Добавить метод в интерфейс `ReplicationExecutor`
2. Реализовать метод в структуре `Replicator`
3. Добавить специфичную логику агрегации результатов
4. Обновить метрики и тесты

### Кастомные политики

Можно расширить логику агрегации для поддержки новых политик:

```go
// Новая политика: ack=majority
if policy.AckLevel == "majority" {
    requiredSuccesses := (totalBackends / 2) + 1
    if successCount >= requiredSuccesses {
        return firstSuccessResponse
    }
}
```

## Зависимости

- `github.com/aws/aws-sdk-go-v2` - AWS SDK для Go v2
- `s3proxy/backend` - модуль управления бэкендами
- `s3proxy/monitoring` - модуль метрик
- `s3proxy/apigw` - типы S3 запросов и ответов
- `s3proxy/routing` - типы политик
- `s3proxy/logger` - система логирования
