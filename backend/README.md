# Backend Manager & Health Checker

Модуль Backend Manager & Health Checker является единственным источником правды о состоянии сконфигурированных S3-бэкендов. Он постоянно отслеживает их "здоровье" и предоставляет другим модулям актуальный список работоспособных бэкендов.

## Назначение и зона ответственности

**Ключевые обязанности:**
- Управление конфигурацией S3-бэкендов (эндпоинты, регионы, учетные данные)
- Поддержание внутреннего состояния каждого бэкенда (UP, DOWN, PROBING)
- Активные проверки здоровья (периодические HeadBucket запросы)
- Пассивные проверки (Circuit Breaker на основе отчетов от других модулей)
- Предоставление списка работоспособных бэкендов
- Экспорт метрик состояния в Prometheus

**Вне зоны ответственности:**
- Выполнение бизнес-операций на бэкендах (PUT, GET и т.д.)
- Принятие решений о политиках репликации или чтения

## Архитектура

### Компоненты

1. **Backend Registry** - хранение информации о бэкендах и их состоянии
2. **Active Health Checker** - фоновые проверки доступности
3. **Passive Feedback Collector** - сбор отчетов об операциях (Circuit Breaker)
4. **State Machine** - управление переходами между состояниями

### Состояния бэкенда

- **UP** - бэкенд полностью работоспособен, участвует во всех операциях
- **DOWN** - бэкенд недоступен, исключен из всех операций
- **PROBING** - промежуточное состояние, бэкенд восстанавливается

### Диаграмма переходов состояний

```
    UP ←→ PROBING ←→ DOWN
    
UP → DOWN:
- N последовательных активных проверок завершились ошибкой
- Circuit Breaker сработал (много ошибок в окне времени)

DOWN → PROBING:
- Одна активная проверка завершилась успешно

PROBING → UP:
- M последовательных активных проверок завершились успешно

PROBING → DOWN:
- Одна активная проверка завершилась ошибкой
```

## Использование

### Создание и запуск

```go
import "s3proxy/backend"

// Создание конфигурации
config := &backend.Config{
    Manager: backend.ManagerConfig{
        HealthCheckInterval:     15 * time.Second,
        CheckTimeout:            5 * time.Second,
        FailureThreshold:        3,
        SuccessThreshold:        2,
        CircuitBreakerWindow:    60 * time.Second,
        CircuitBreakerThreshold: 5,
        InitialState:            backend.StateProbing,
    },
    Backends: map[string]backend.BackendConfig{
        "aws-eu-central": {
            Endpoint:  "https://s3.eu-central-1.amazonaws.com",
            Region:    "eu-central-1",
            Bucket:    "my-backup-bucket",
            AccessKey: "AKIAIOSFODNN7EXAMPLE",
            SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
        },
    },
}

// Создание менеджера
manager, err := backend.NewManager(config, metrics)
if err != nil {
    log.Fatal(err)
}

// Запуск активных проверок
err = manager.Start()
if err != nil {
    log.Fatal(err)
}

// Остановка
defer manager.Stop()
```

### Получение бэкендов

```go
// Получить все работоспособные бэкенды (UP или PROBING)
liveBackends := manager.GetLiveBackends()

// Получить все сконфигурированные бэкенды
allBackends := manager.GetAllBackends()

// Получить конкретный бэкенд
backend, exists := manager.GetBackend("aws-eu-central")
```

### Пассивные проверки (Circuit Breaker)

```go
// Сообщить об успешной операции
manager.ReportSuccess("aws-eu-central")

// Сообщить о неудачной операции
manager.ReportFailure("aws-eu-central", err)
```

### Конфигурация

#### YAML конфигурация

```yaml
backend_manager:
  health_check_interval: "15s"  # Интервал активных проверок
  check_timeout: "5s"           # Таймаут одной проверки
  failure_threshold: 3          # Неудач для перехода в DOWN
  success_threshold: 2          # Успехов для перехода в UP
  circuit_breaker_window: "60s" # Окно для Circuit Breaker
  circuit_breaker_threshold: 5  # Ошибок в окне для срабатывания
  initial_state: "PROBING"      # Начальное состояние

backends:
  aws-frankfurt:
    endpoint: "https://s3.eu-central-1.amazonaws.com"
    region: "eu-central-1"
    bucket: "my-company-backup-fra"
    access_key: "AKIAIOSFODNN7EXAMPLE"
    secret_key: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
    
  wasabi-amsterdam:
    endpoint: "https://s3.eu-west-1.wasabisys.com"
    region: "eu-west-1"
    bucket: "my-company-backup-ams"
    access_key: "..."
    secret_key: "..."
```

## Интерфейсы

### BackendProvider

Основной интерфейс для взаимодействия с менеджером:

```go
type BackendProvider interface {
    GetLiveBackends() []*Backend
    GetAllBackends() []*Backend
    GetBackend(id string) (*Backend, bool)
    ReportSuccess(backendID string)
    ReportFailure(backendID string, err error)
    Start() error
    Stop() error
    IsRunning() bool
}
```

### Backend

Структура, представляющая один бэкенд:

```go
type Backend struct {
    ID       string
    Config   BackendConfig
    S3Client *s3.Client
    // ... внутренние поля
}

// Потокобезопасные методы
func (b *Backend) GetState() BackendState
func (b *Backend) GetLastError() error
func (b *Backend) GetLastCheckTime() time.Time
func (b *Backend) GetStats() (consecutiveFailures, consecutiveSuccesses, recentFailures int)
```

## Активные проверки здоровья

Менеджер периодически выполняет легковесные запросы `HeadBucket` к каждому бэкенду:

- **Интервал:** настраивается через `health_check_interval`
- **Таймаут:** настраивается через `check_timeout`
- **Асинхронность:** каждый бэкенд проверяется в отдельной горутине
- **Логика:** на основе результатов обновляется состояние согласно state machine

## Пассивные проверки (Circuit Breaker)

Другие модули сообщают о результатах операций через `ReportSuccess`/`ReportFailure`:

- **Скользящее окно:** настраивается через `circuit_breaker_window`
- **Порог срабатывания:** настраивается через `circuit_breaker_threshold`
- **Немедленная реакция:** при превышении порога бэкенд сразу переводится в DOWN
- **Сброс:** успешные операции сбрасывают счетчик ошибок

## Метрики Prometheus

Модуль экспортирует следующие метрики:

```
# Состояние бэкенда (1=UP, 0.5=PROBING, 0=DOWN)
s3proxy_backend_state{backend_id="aws-frankfurt"} 1
s3proxy_backend_state{backend_id="wasabi-amsterdam"} 0.5
```

## Потокобезопасность

- Все публичные методы потокобезопасны
- Внутреннее состояние защищено мьютексами
- Активные проверки выполняются в отдельных горутинах
- Graceful shutdown с ожиданием завершения всех горутин

## Тестирование

```bash
# Запуск тестов
go test -v ./backend

# Запуск тестов с покрытием
go test -v -cover ./backend

# Запуск конкретного теста
go test -v -run TestCircuitBreaker ./backend
```

## Примеры использования

### Интеграция с Replication Module

```go
type ReplicationExecutor struct {
    backendProvider backend.BackendProvider
}

func (r *ReplicationExecutor) PutObject(req *S3Request) *S3Response {
    // Получаем живые бэкенды
    backends := r.backendProvider.GetLiveBackends()
    
    for _, backend := range backends {
        err := r.putToBackend(backend, req)
        if err != nil {
            // Сообщаем о неудаче
            r.backendProvider.ReportFailure(backend.ID, err)
        } else {
            // Сообщаем об успехе
            r.backendProvider.ReportSuccess(backend.ID)
        }
    }
}
```

### Мониторинг состояния

```go
// Получение статистики всех бэкендов
for _, backend := range manager.GetAllBackends() {
    state := backend.GetState()
    lastError := backend.GetLastError()
    failures, successes, recent := backend.GetStats()
    
    fmt.Printf("Backend %s: state=%s, failures=%d, successes=%d\n", 
        backend.ID, state, failures, successes)
}
```

## Расширяемость

### Добавление новых типов проверок

Можно расширить `checkBackend` для выполнения дополнительных проверок:

```go
func (m *Manager) checkBackend(backend *Backend) {
    // Существующая проверка HeadBucket
    // ...
    
    // Дополнительная проверка - ListObjects
    _, err2 := backend.S3Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
        Bucket: aws.String(backend.Config.Bucket),
        MaxKeys: aws.Int32(1),
    })
    
    // Комбинированная логика оценки здоровья
}
```

### Добавление новых состояний

Можно добавить дополнительные состояния, например `DEGRADED`:

```go
const (
    StateUp       BackendState = "UP"
    StateDegraded BackendState = "DEGRADED"
    StateProbing  BackendState = "PROBING"
    StateDown     BackendState = "DOWN"
)
```

## Зависимости

- `github.com/aws/aws-sdk-go-v2` - AWS SDK для Go v2
- `s3proxy/logger` - система логирования
- `s3proxy/monitoring` - экспорт метрик Prometheus
