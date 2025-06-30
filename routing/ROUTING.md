### Техническое Задание: Модуль Policy & Routing Engine

#### 1. Назначение и Зона ответственности

Модуль **Policy & Routing Engine** является центральным диспетчером S3-операций. Он получает аутентифицированный запрос и, основываясь на типе операции и глобальных политиках, решает, **как** этот запрос должен быть выполнен. Он делегирует само выполнение нижележащим модулям (`Replication Module`, `Fetching Module` и т.д.).

**Ключевые обязанности:**
*   Реализация интерфейса `RequestHandler`, чтобы служить обработчиком для `API Gateway`.
*   Прием `S3Request` и вызов `Authentication Module` для проверки подлинности запроса.
*   Прием результата аутентификации (`UserIdentity`). В будущем здесь будет точка для вызова модуля авторизации.
*   Маршрутизация запроса в нужный исполнительный модуль в зависимости от типа S3-операции (`S3Request.Operation`).
    *   Запросы на запись (`PUT`, `DELETE`, `*Multipart*`) направляются в `Replication Module`.
    *   Запросы на чтение (`GET`, `HEAD`, `LIST`) направляются в `Fetching Module`.
*   Передача в исполнительный модуль не только самого запроса, но и **контекста выполнения**, который определяется глобальными политиками (например, `ack=one`, `get_policy=newest`).
*   Обработка ошибок, полученных от модулей аутентификации или исполнения, и преобразование их в стандартизированный `S3Response` с правильным кодом и телом ошибки в формате S3 XML.

**Вне зоны ответственности модуля:**
*   Парсинг HTTP и формирование конечного HTTP-ответа (это делает `API Gateway`).
*   Проверка подписи SigV4 (это делает `Authentication Module`).
*   Непосредственное взаимодействие с S3-бэкендами: отправка запросов, обработка ответов от них (это делают `Replication Module` и `Fetching Module`).
*   Кэширование (будет делегировано `Fetching Module` или отдельному кэширующему слою).

#### 2. Архитектура и компоненты

Модуль состоит из главного обработчика и набора политик.

1.  **Engine (Главный обработчик)**: Основная структура, которая реализует `RequestHandler` и содержит ссылки на все необходимые зависимости (аутентификатор, исполнители, конфигурация политик).
2.  **Policies (Политики)**: Структуры данных, которые хранят конфигурацию поведения для разных типов операций. Они загружаются при старте приложения.
3.  **Router (Внутренний маршрутизатор)**: Логическая часть внутри `Engine`, которая выполняет `switch-case` по `S3Request.Operation` и вызывает соответствующий метод исполнителя.

#### 3. Интерфейсы и структуры данных (Go)

Модуль будет взаимодействовать с другими через четко определенные интерфейсы, которые он сам и определяет для своих зависимостей.

**1. Исполнительные интерфейсы (определяются здесь, реализуются в других модулях):**

```go
// WriteOperationPolicy определяет политику для операций записи.
type WriteOperationPolicy struct {
    // AckLevel определяет, сколько подтверждений ждать.
    // Возможные значения: "none", "one", "all".
    AckLevel string `yaml:"ack"`
}

// ReadOperationPolicy определяет политику для операций чтения.
type ReadOperationPolicy struct {
    // Strategy определяет, как выбрать бэкенд для чтения.
    // Возможные значения: "first", "newest".
    Strategy string `yaml:"strategy"`
}

// ReplicationExecutor - интерфейс для модуля, выполняющего запись на бэкенды.
type ReplicationExecutor interface {
    // PutObject выполняет операцию PUT в соответствии с политикой.
    PutObject(ctx context.Context, req *S3Request, policy WriteOperationPolicy) *S3Response
    
    // DeleteObject выполняет операцию DELETE в соответствии с политикой.
    DeleteObject(ctx context.Context, req *S3Request, policy WriteOperationPolicy) *S3Response
    
    // ... и аналогичные методы для всех multipart-операций.
    // CreateMultipartUpload, UploadPart, CompleteMultipartUpload, etc.
}

// FetchingExecutor - интерфейс для модуля, выполняющего чтение с бэкендов.
type FetchingExecutor interface {
    // GetObject выполняет операцию GET в соответствии с политикой.
    GetObject(ctx context.Context, req *S3Request, policy ReadOperationPolicy) *S3Response

    // HeadObject выполняет операцию HEAD в соответствии с политикой.
    HeadObject(ctx context.Context, req *S3Request, policy ReadOperationPolicy) *S3Response

    // ListObjects выполняет операцию LIST. У листинга своя, более сложная логика.
    ListObjects(ctx context.Context, req *S3Request) *S3Response
}
```
*Примечание: Разделение на `ReplicationExecutor` и `FetchingExecutor` повышает модульность. Теоретически, это могут быть два разных пакета.*

**2. Основная структура Engine:**

```go
// Engine - это реализация Policy & Routing Engine.
type Engine struct {
    // Зависимости, внедряемые при создании.
    auth        Authenticator       // Модуль аутентификации.
    replicator  ReplicationExecutor // Модуль для записи.
    fetcher     FetchingExecutor    // Модуль для чтения.

    // Конфигурация политик, загружаемая при старте.
    putPolicy    WriteOperationPolicy
    deletePolicy WriteOperationPolicy
    getPolicy    ReadOperationPolicy
    
    // ... другие зависимости, например, логгер.
}

// NewEngine создает новый экземпляр Engine.
func NewEngine(
    auth Authenticator,
    replicator ReplicationExecutor,
    fetcher FetchingExecutor,
    config *Config, // Глобальная конфигурация приложения
) *Engine {
    // Здесь из config извлекаются и сохраняются нужные политики
    return &Engine{
        auth:        auth,
        replicator:  replicator,
        fetcher:     fetcher,
        putPolicy:   config.Policies.Put,
        deletePolicy: config.Policies.Delete,
        getPolicy:    config.Policies.Get,
    }
}

// Handle - реализация интерфейса RequestHandler. Это точка входа в модуль.
func (e *Engine) Handle(req *S3Request) *S3Response {
    // Шаг 1: Аутентификация
    identity, err := e.auth.Authenticate(req)
    if err != nil {
        // Преобразовать ошибку аутентификации в стандартный S3Response
        return e.createAuthErrorResponse(err)
    }

    // Шаг 2: Авторизация (заглушка для будущего)
    // isAuthorized := e.authorizer.Authorize(identity, req)
    // if !isAuthorized { ... }

    // Шаг 3: Маршрутизация на основе типа операции
    switch req.Operation {
    case PutObject:
        return e.replicator.PutObject(req.Context, req, e.putPolicy)
    
    case DeleteObject:
        return e.replicator.DeleteObject(req.Context, req, e.deletePolicy)

    case GetObject:
        return e.fetcher.GetObject(req.Context, req, e.getPolicy)

    case HeadObject:
        // Для HEAD обычно используется та же политика, что и для GET
        return e.fetcher.HeadObject(req.Context, req, e.getPolicy)

    case ListObjectsV2:
        // У листинга своя логика, не требующая политики
        return e.fetcher.ListObjects(req.Context, req)

    // ... кейсы для всех multipart операций, вызывающие методы e.replicator

    default:
        // Вернуть ошибку для неподдерживаемых операций
        return e.createOperationNotImplementedResponse()
    }
}
```

#### 4. Расширяемость политик

Текущая модель `WriteOperationPolicy` и `ReadOperationPolicy` проста, но расширяема. Чтобы добавить новую политику, например, для `GET`, нужно:
1.  **Добавить поле** в структуру `ReadOperationPolicy`: `Strategy string` -> `Strategy string`, `AllowStale bool`.
2.  **Обновить конфигурацию**, чтобы можно было задать `allow_stale: true`.
3.  **Обновить `FetchingExecutor`**, чтобы он учитывал новый параметр `policy.AllowStale` в своей логике.

Такой подход не требует изменения самого `Engine`, он просто "пробрасывает" структуру с политикой дальше. Это сохраняет его простым и сфокусированным на маршрутизации.

#### 5. Обработка ошибок

`Engine` должен содержать вспомогательные методы для преобразования Go-ошибок в стандартные `S3Response` с XML-телом.

```go
// Пример вспомогательного метода
func (e *Engine) createAuthErrorResponse(err error) *S3Response {
    var code string
    var message string
    var statusCode int

    switch {
    case errors.Is(err, ErrInvalidAccessKeyID):
        code = "InvalidAccessKeyId"
        message = "The Access Key Id you provided does not exist in our records."
        statusCode = 403
    case errors.Is(err, ErrSignatureMismatch):
        code = "SignatureDoesNotMatch"
        message = "The request signature we calculated does not match the signature you provided."
        statusCode = 403
    // ... другие кейсы
    default:
        // Общая ошибка
        code = "InternalError"
        message = "An internal error occurred."
        statusCode = 500
    }
    
    // Создать S3 XML тело ошибки
    errorBody := formatS3ErrorXML(code, message)
    
    return &S3Response{
        StatusCode: statusCode,
        Body:       io.NopCloser(strings.NewReader(errorBody)),
        Headers:    http.Header{"Content-Type": []string{"application/xml"}},
    }
}
```

#### 6. Конфигурация модуля

Этот модуль сам по себе не имеет конфигурации, но он является потребителем глобальной конфигурации политик.

Пример `YAML` конфигурации, которую он использует:

```yaml
policies:
  put:
    # Возможные значения: none, one, all
    ack: "one"
  delete:
    ack: "all"
  get:
    # Возможные значения: first, newest
    strategy: "first"
```

#### 7. Ответственность разработчика

Разработчик, реализующий данный модуль, должен:

1.  Определить интерфейсы `ReplicationExecutor` и `FetchingExecutor`.
2.  Определить структуры для политик `WriteOperationPolicy` и `ReadOperationPolicy`.
3.  Реализовать структуру `Engine` и ее конструктор `NewEngine`, который принимает зависимости и конфигурацию.
4.  Реализовать метод `Engine.Handle`, который выполняет последовательность "Аутентификация -> (будущая Авторизация) -> Маршрутизация".
5.  Реализовать логику `switch-case` для всех поддерживаемых S3-операций, вызывая соответствующие методы исполнителей.
6.  Реализовать хелперы для создания стандартных S3-ответов об ошибках (аутентификации, нереализованных операций и т.д.).
7.  Написать unit-тесты, которые мокируют (`mock`) интерфейсы `Authenticator`, `ReplicationExecutor` и `FetchingExecutor`, чтобы проверить корректность логики маршрутизации и обработки ошибок в `Engine`. Тесты должны проверять, что для `PUT` вызывается `replicator`, а для `GET` — `fetcher`, и что им передается правильная политика из конфигурации.