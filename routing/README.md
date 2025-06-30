# Policy & Routing Engine

Модуль Policy & Routing Engine является центральным диспетчером S3-операций в системе S3 Proxy. Он получает аутентифицированные запросы и, основываясь на типе операции и глобальных политиках, решает, как эти запросы должны быть выполнены.

## Назначение и зона ответственности

**Ключевые обязанности:**
- Реализация интерфейса `RequestHandler` для обработки запросов от API Gateway
- Вызов Authentication Module для проверки подлинности запросов
- Маршрутизация запросов в соответствующие исполнительные модули
- Применение политик выполнения операций
- Обработка ошибок и формирование стандартных S3 ответов

**Вне зоны ответственности:**
- Парсинг HTTP запросов (делает API Gateway)
- Проверка подписи SigV4 (делает Authentication Module)
- Непосредственное взаимодействие с S3-бэкендами (делают Replication и Fetching модули)

## Архитектура

### Компоненты

1. **Engine** - основная структура, реализующая `RequestHandler`
2. **Policies** - структуры данных с конфигурацией поведения
3. **Router** - внутренняя логика маршрутизации по типу операции

### Интерфейсы

#### ReplicationExecutor
Интерфейс для модуля, выполняющего операции записи:
- `PutObject` - загрузка объекта
- `DeleteObject` - удаление объекта
- `CreateMultipartUpload` - инициация multipart upload
- `UploadPart` - загрузка части
- `CompleteMultipartUpload` - завершение multipart upload
- `AbortMultipartUpload` - отмена multipart upload

#### FetchingExecutor
Интерфейс для модуля, выполняющего операции чтения:
- `GetObject` - получение объекта
- `HeadObject` - получение метаданных объекта
- `ListObjects` - список объектов в бакете
- `ListBuckets` - список бакетов
- `ListMultipartUploads` - список активных multipart uploads

### Политики

#### WriteOperationPolicy
```go
type WriteOperationPolicy struct {
    AckLevel string `yaml:"ack"` // "none", "one", "all"
}
```

#### ReadOperationPolicy
```go
type ReadOperationPolicy struct {
    Strategy string `yaml:"strategy"` // "first", "newest"
}
```

## Использование

### Создание Engine

```go
import (
    "s3proxy/auth"
    "s3proxy/routing"
)

// Создание зависимостей
authenticator := auth.NewStaticAuthenticator(credentials)
replicator := NewReplicationModule(backends)
fetcher := NewFetchingModule(backends)

// Конфигурация политик
config := &routing.Config{
    Policies: routing.Policies{
        Put: routing.WriteOperationPolicy{AckLevel: "one"},
        Delete: routing.WriteOperationPolicy{AckLevel: "all"},
        Get: routing.ReadOperationPolicy{Strategy: "first"},
    },
}

// Создание engine
engine := routing.NewEngine(authenticator, replicator, fetcher, config)

// Использование в API Gateway
gateway := apigw.New(gatewayConfig, engine)
```

### Конфигурация

Пример YAML конфигурации:

```yaml
policies:
  put:
    ack: "one"      # Ждать подтверждения от одного бэкенда
  delete:
    ack: "all"      # Ждать подтверждения от всех бэкендов
  get:
    strategy: "first" # Читать с первого доступного бэкенда
```

## Обработка ошибок

Engine автоматически преобразует ошибки в стандартные S3 XML ответы с правильными HTTP кодами:

### Ошибки аутентификации
- `ErrMissingAuthHeader` → `MissingSecurityHeader` (400 Bad Request)
- `ErrInvalidAccessKeyID` → `InvalidAccessKeyId` (403 Forbidden)
- `ErrSignatureMismatch` → `SignatureDoesNotMatch` (403 Forbidden)
- `ErrRequestExpired` → `RequestTimeTooSkewed` (403 Forbidden)
- Неизвестные ошибки аутентификации → `AccessDenied` (403 Forbidden)

### Ошибки операций
- Неподдерживаемая операция → `NotImplemented` (501 Not Implemented)

**Важно:** Engine формирует правильные S3 XML ответы с корректными HTTP кодами статуса. Поле `Error` в `S3Response` не устанавливается, чтобы избежать переопределения кодов ошибок в API Gateway.

## Маршрутизация операций

### Операции записи → ReplicationExecutor
- `PUT_OBJECT`
- `DELETE_OBJECT`
- `CREATE_MULTIPART_UPLOAD`
- `UPLOAD_PART`
- `COMPLETE_MULTIPART_UPLOAD`
- `ABORT_MULTIPART_UPLOAD`

### Операции чтения → FetchingExecutor
- `GET_OBJECT`
- `HEAD_OBJECT`
- `LIST_OBJECTS_V2`
- `LIST_BUCKETS`
- `LIST_MULTIPART_UPLOADS`

## Тестирование

Модуль включает mock реализации исполнителей для тестирования:

```go
// Создание mock исполнителей
replicator := routing.NewMockReplicationExecutor()
fetcher := routing.NewMockFetchingExecutor()

// Использование в тестах
engine := routing.NewEngine(mockAuth, replicator, fetcher, nil)
```

## Расширяемость

### Добавление новой политики

1. Обновить структуру политики:
```go
type ReadOperationPolicy struct {
    Strategy   string `yaml:"strategy"`
    AllowStale bool   `yaml:"allow_stale"` // Новое поле
}
```

2. Обновить конфигурацию:
```yaml
policies:
  get:
    strategy: "first"
    allow_stale: true
```

3. Обновить исполнитель для учета новой политики

### Добавление новой операции

1. Добавить операцию в `apigw.S3Operation`
2. Добавить case в `Engine.Handle`
3. Реализовать метод в соответствующем исполнителе

## Логирование

Engine использует систему логирования с различными уровнями:
- `DEBUG` - детали маршрутизации и политик
- `INFO` - информация об аутентифицированных запросах
- `WARN` - предупреждения о неподдерживаемых операциях
- `ERROR` - ошибки выполнения

## Зависимости

- `s3proxy/apigw` - типы данных S3 запросов и ответов
- `s3proxy/auth` - интерфейсы и ошибки аутентификации
- `s3proxy/logger` - система логирования
