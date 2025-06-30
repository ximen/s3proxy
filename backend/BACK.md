### Техническое Задание: Модуль Backend Manager & Health Checker

#### 1. Назначение и Зона ответственности

Модуль **Backend Manager & Health Checker** является единственным источником правды о состоянии сконфигурированных S3-бэкендов. Его задача — постоянно отслеживать их "здоровье" и предоставлять другим модулям (таким как `ReplicationExecutor` и `FetchingExecutor`) актуальный список **работоспособных** бэкендов для выполнения операций.

**Ключевые обязанности:**
*   **Управление конфигурацией**: Загрузка и хранение списка S3-бэкендов из конфигурационного файла, включая их эндпоинты, регионы и учетные данные.
*   **Хранение состояния**: Поддержание в памяти внутреннего состояния для каждого бэкенда (`UP`, `DOWN`, `DEGRADED`).
*   **Активные проверки (Probing)**: Периодическое выполнение фоновых, легковесных запросов (например, `HEAD Bucket`) к каждому бэкенду для проверки его доступности.
*   **Пассивные проверки (Circuit Breaking)**: Сбор информации об успешных и неуспешных операциях от исполнительных модулей. Если количество ошибок для конкретного бэкенда превышает порог, он немедленно помечается как `DOWN`, не дожидаясь активной проверки.
*   **Предоставление списка**: Реализация методов, которые по запросу возвращают список "живых" бэкендов.
*   **Экспорт метрик**: Предоставление Prometheus-метрик о состоянии каждого бэкенда.

**Вне зоны ответственности модуля:**
*   Выполнение бизнес-операций (`PUT`, `GET` и т.д.) на бэкендах. Модуль только проверяет их доступность.
*   Принятие решений о политиках репликации или чтения. Он лишь предоставляет список кандидатов для этих операций.

#### 2. Архитектура и компоненты

1.  **Backend Registry**: Основной компонент, хранящий информацию о каждом бэкенде: его конфигурацию, S3-клиент для него и текущий статус.
2.  **Active Health Checker**: Фоновый процесс (горутина), который с заданным интервалом запускает проверки для всех бэкендов.
3.  **Passive Feedback Collector**: Набор методов, которые исполнительные модули могут вызывать, чтобы сообщить об успехе или неудаче операции с конкретным бэкендом. Это реализует паттерн **Circuit Breaker**.
4.  **State Machine**: Для каждого бэкенда будет реализована простая машина состояний для защиты от "дребезга" (flapping), когда бэкенд быстро переключается между UP и DOWN.

#### 3. Состояния бэкенда и переходы между ними

Каждый бэкенд может находиться в одном из трех состояний:

*   **`UP` (Здоров)**: Бэкенд полностью работоспособен. Он участвует во всех операциях.
*   **`DOWN` (Недоступен)**: Бэкенд считается неработоспособным. Он полностью исключается из всех операций. Активные проверки продолжаются, чтобы обнаружить восстановление.
*   **`PROBING` (Проверка)**: Промежуточное состояние. Бэкенд был в `DOWN`, но последняя активная проверка прошла успешно. Он еще не возвращается в общий пул, но система может попытаться направить на него небольшую долю трафика или выполнить несколько контрольных операций перед тем, как перевести в `UP`.

**Диаграмма состояний:**

![State Diagram](https.viewer.diagrams.net/previews/ERD/8f3b0e77-9876-430c-8e81-229237e8c37d.png)

1.  **UP -> DOWN**:
    *   (Активная проверка) `N` последовательных активных проверок завершились ошибкой.
    *   (Пассивная проверка) Количество ошибок в скользящем окне времени превысило порог (Circuit Breaker "разомкнулся").
2.  **DOWN -> PROBING**:
    *   (Активная проверка) Одна активная проверка завершилась успешно.
3.  **PROBING -> UP**:
    *   (Активная проверка) `M` последовательных активных проверок завершились успешно.
4.  **PROBING -> DOWN**:
    *   (Активная проверка) Одна активная проверка завершилась ошибкой.

#### 4. Интерфейсы и структуры данных (Go)

**1. Основной интерфейс менеджера:**

```go
// BackendProvider - это интерфейс, который предоставляет доступ к бэкендам.
// Его будет использовать Replication/Fetching Executor.
type BackendProvider interface {
    // GetLiveBackends возвращает срез всех бэкендов в состоянии UP или PROBING.
    // Возвращает копию, чтобы избежать гонок данных.
    GetLiveBackends() []*Backend

    // GetAllBackends возвращает срез всех сконфигурированных бэкендов,
    // независимо от их состояния. Полезно для UI или фоновой сверки.
    GetAllBackends() []*Backend

    // ReportSuccess сообщает менеджеру об успешной операции с бэкендом.
    ReportSuccess(backendID string)

    // ReportFailure сообщает менеджеру о неудачной операции с бэкендом.
    ReportFailure(backendID string, err error)
}

// Backend - структура, представляющая один S3-бэкенд.
type Backend struct {
    ID          string      // Уникальный идентификатор (например, "eu-central-1-main")
    Config      BackendConfig // Конфигурация из файла
    S3Client    *s3.Client  // Готовый и настроенный S3-клиент для этого бэкенда
    
    // Внутреннее состояние, защищенное мьютексом
    mu          sync.RWMutex
    state       BackendState
    lastError   error
    failureCount int
    successCount int
}

type BackendState string

const (
    StateUp      BackendState = "UP"
    StateDown    BackendState = "DOWN"
    StateProbing BackendState = "PROBING"
)

// BackendConfig - конфигурация одного бэкенда из YAML/JSON.
type BackendConfig struct {
    Endpoint  string `yaml:"endpoint"`
    Region    string `yaml:"region"`
    Bucket    string `yaml:"bucket"` // Бакет на бэкенде, с которым работаем
    AccessKey string `yaml:"access_key"`
    SecretKey string `yaml:"secret_key"`
}
```

**2. Реализация менеджера:**

```go
// Manager - конкретная реализация BackendProvider.
type Manager struct {
    backends map[string]*Backend
    config   ManagerConfig
    mu       sync.RWMutex
    // ... логгер и другие зависимости
}

// ManagerConfig - конфигурация для самого менеджера.
type ManagerConfig struct {
    HealthCheckInterval  time.Duration `yaml:"interval"`         // Интервал активных проверок
    FailureThreshold     int           `yaml:"failure_threshold"`  // Кол-во ошибок для перехода в DOWN
    SuccessThreshold     int           `yaml:"success_threshold"`  // Кол-во успехов для перехода PROBING -> UP
    CheckTimeout         time.Duration `yaml:"check_timeout"`    // Таймаут для одного health check запроса
}

// NewManager создает и запускает менеджер бэкендов.
func NewManager(configs map[string]BackendConfig, managerConf ManagerConfig) (*Manager, error) {
    // 1. Инициализировать map бэкендов
    // 2. Для каждой конфигурации создать s3.Client
    // 3. Создать структуру Backend для каждого, начальное состояние - PROBING
    // 4. Запустить фоновую горутину для активных проверок (Active Health Checker)
    // 5. Вернуть экземпляр Manager
}

// Внутренний метод для активных проверок
func (m *Manager) runHealthChecks() {
    ticker := time.NewTicker(m.config.HealthCheckInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            // Асинхронно проверить каждый бэкенд
            for id := range m.backends {
                go m.checkBackend(id)
            }
        // ... case для graceful shutdown
        }
    }
}

// checkBackend выполняет один health check для бэкенда.
func (m *Manager) checkBackend(id string) {
    backend := m.backends[id]
    // Создать контекст с таймаутом
    ctx, cancel := context.WithTimeout(context.Background(), m.config.CheckTimeout)
    defer cancel()

    // Выполнить легковесный запрос, например HeadBucket
    _, err := backend.S3Client.HeadBucket(ctx, &s3.HeadBucketInput{
        Bucket: &backend.Config.Bucket,
    })

    // Обновить состояние бэкенда в соответствии с диаграммой состояний
    backend.mu.Lock()
    defer backend.mu.Unlock()
    
    if err != nil {
        // Логика перехода UP -> DOWN или PROBING -> DOWN
    } else {
        // Логика перехода DOWN -> PROBING или PROBING -> UP
    }
}

// Методы для пассивной проверки (Circuit Breaker)
func (m *Manager) ReportFailure(backendID string, err error) {
    // Найти бэкенд, инкрементировать счетчик ошибок.
    // Если счетчик превысил порог, немедленно перевести состояние в DOWN.
}

func (m *Manager) ReportSuccess(backendID string) {
    // Найти бэкенд, сбросить счетчик ошибок.
}
```

#### 5. Конфигурация модуля

Пример `YAML` конфигурации:

```yaml
backend_manager:
  # Настройки самого менеджера
  health_check_interval: "15s"  # Как часто проводить активные проверки
  check_timeout: "5s"           # Таймаут на одну проверку
  failure_threshold: 3          # 3 провала подряд для перехода в DOWN
  success_threshold: 2          # 2 успеха подряд для перехода из PROBING в UP

# Список самих S3-бэкендов
backends:
  # ID бэкенда: "aws-frankfurt"
  aws-frankfurt:
    endpoint: "https://s3.eu-central-1.amazonaws.com"
    region: "eu-central-1"
    bucket: "my-company-backup-fra"
    access_key: "..."
    secret_key: "..."

  # ID бэкенда: "wasabi-amsterdam"
  wasabi-amsterdam:
    endpoint: "https://s3.eu-west-1.wasabisys.com"
    region: "eu-west-1"
    bucket: "my-company-backup-ams"
    access_key: "..."
    secret_key: "..."
```

#### 6. Ответственность разработчика

1.  Реализовать интерфейс `BackendProvider` и все структуры (`Backend`, `Manager`, `BackendConfig` и т.д.).
2.  Реализовать конструктор `NewManager`, который корректно инициализирует S3-клиенты для каждого бэкенда (используя `aws-sdk-go-v2`) и запускает фоновый процесс проверок.
3.  Реализовать логику `runHealthChecks` и `checkBackend`, включая управление контекстом и таймаутами.
4.  Четко реализовать машину состояний `UP <-> PROBING <-> DOWN` внутри `checkBackend`, защитив доступ к состоянию мьютексами.
5.  Реализовать потокобезопасные методы `ReportSuccess` и `ReportFailure` для механизма Circuit Breaker.
6.  Реализовать потокобезопасные методы `GetLiveBackends` и `GetAllBackends`.
7.  Интегрировать экспорт Prometheus-метрик. Для каждого бэкенда должна экспортироваться метрика вида `s3proxy_backend_state{backend_id="aws-frankfurt"}` со значением (UP=1, PROBING=0.5, DOWN=0).
8.  Написать unit-тесты, проверяющие логику переходов состояний как от активных, так и от пассивных проверок. Например: "после 3 вызовов ReportFailure бэкенд переходит в DOWN", "после успешного health check бэкенд из DOWN переходит в PROBING".