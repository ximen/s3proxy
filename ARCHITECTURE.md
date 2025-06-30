# Архитектура S3 Proxy API Gateway

## Обзор

S3 Proxy API Gateway - это модульная реализация HTTP-сервера, который принимает S3-совместимые запросы и преобразует их в стандартизированное внутреннее представление для дальнейшей обработки.

## Структура проекта

```
s3proxy/
├── main.go                 # Точка входа приложения
├── types.go               # Основные типы данных и интерфейсы
├── config.go              # Конфигурация приложения
├── apigw.go               # Основной модуль API Gateway
├── parser.go              # Парсер HTTP запросов в S3Request
├── response_writer.go     # Формирование HTTP ответов из S3Response
├── mock_handler.go        # Тестовый обработчик для демонстрации
├── parser_test.go         # Unit тесты для парсера
├── integration_test.go    # Интеграционные тесты
├── go.mod                 # Go модуль
├── Makefile              # Автоматизация сборки и тестирования
└── README.md             # Документация пользователя
```

## Основные компоненты

### 1. Типы данных (types.go)

#### S3Operation
Перечисление всех поддерживаемых S3 операций:
- `PutObject` - Загрузка объекта
- `GetObject` - Получение объекта
- `HeadObject` - Получение метаданных объекта
- `DeleteObject` - Удаление объекта
- `ListObjectsV2` - Список объектов в бакете
- `ListBuckets` - Список бакетов
- `CreateMultipartUpload` - Инициация multipart загрузки
- `UploadPart` - Загрузка части multipart
- `CompleteMultipartUpload` - Завершение multipart загрузки
- `AbortMultipartUpload` - Отмена multipart загрузки
- `ListMultipartUploads` - Список активных multipart загрузок

#### S3Request
Стандартизированное представление S3 запроса:
```go
type S3Request struct {
    Operation     S3Operation    // Тип операции
    Bucket        string         // Имя бакета
    Key           string         // Ключ объекта
    Headers       http.Header    // HTTP заголовки
    Query         url.Values     // Query параметры
    Body          io.ReadCloser  // Тело запроса
    ContentLength int64          // Размер тела
    Context       context.Context // Контекст запроса
}
```

#### S3Response
Стандартизированное представление S3 ответа:
```go
type S3Response struct {
    StatusCode int            // HTTP статус код
    Headers    http.Header    // HTTP заголовки ответа
    Body       io.ReadCloser  // Тело ответа
    Error      error          // Ошибка (если есть)
}
```

#### RequestHandler
Интерфейс для обработчиков запросов:
```go
type RequestHandler interface {
    Handle(req *S3Request) *S3Response
}
```

### 2. Конфигурация (config.go)

Структура конфигурации с параметрами:
- `ListenAddress` - Адрес прослушивания
- `TLSCertFile` - Путь к SSL сертификату
- `TLSKeyFile` - Путь к приватному ключу SSL
- `ReadTimeout` - Таймаут чтения
- `WriteTimeout` - Таймаут записи

### 3. Парсер запросов (parser.go)

#### RequestParser
Отвечает за анализ HTTP запросов и преобразование их в S3Request.

**Основные методы:**
- `Parse(r *http.Request) (*S3Request, error)` - Главный метод парсинга
- `parsePath(path string, s3req *S3Request)` - Извлечение bucket и key из URL
- `determineOperation(method string, s3req *S3Request)` - Определение типа операции

**Логика определения операций:**
- Анализ HTTP метода (GET, PUT, POST, DELETE, HEAD)
- Анализ query параметров (uploads, uploadId, partNumber)
- Анализ пути URL (наличие bucket и key)

### 4. Обработчик ответов (response_writer.go)

#### ResponseWriter
Отвечает за формирование HTTP ответов из S3Response.

**Основные методы:**
- `WriteResponse(w http.ResponseWriter, s3resp *S3Response)` - Запись ответа
- `writeErrorResponse(w http.ResponseWriter, err error)` - Формирование XML ошибок
- `mapErrorToS3Error(err error)` - Сопоставление ошибок с S3 кодами

**Поддерживаемые S3 коды ошибок:**
- `NoSuchKey` - Объект не найден
- `NoSuchBucket` - Бакет не найден
- `AccessDenied` - Доступ запрещен
- `InvalidRequest` - Некорректный запрос
- `NotImplemented` - Операция не реализована
- `InternalError` - Внутренняя ошибка

### 5. API Gateway (apigw.go)

#### APIGateway
Главный компонент, объединяющий все части системы.

**Структура:**
```go
type APIGateway struct {
    config         Config
    handler        RequestHandler
    parser         *RequestParser
    responseWriter *ResponseWriter
}
```

**Жизненный цикл запроса:**
1. Прием HTTP запроса через `ServeHTTP`
2. Парсинг запроса в `S3Request`
3. Передача в `RequestHandler`
4. Получение `S3Response`
5. Формирование HTTP ответа

### 6. Тестовый обработчик (mock_handler.go)

#### MockHandler
Реализация `RequestHandler` для демонстрации и тестирования.

Возвращает заранее подготовленные ответы для всех поддерживаемых операций:
- XML ответы для списков (бакеты, объекты, multipart uploads)
- Мок-данные для объектов
- Корректные HTTP статус коды

## Потоки данных

### Входящий запрос
```
HTTP Request → RequestParser → S3Request → RequestHandler → S3Response → ResponseWriter → HTTP Response
```

### Обработка ошибок
```
Error → ResponseWriter → S3 XML Error → HTTP Response
```

## Особенности реализации

### 1. Потоковая обработка
- Тело запроса передается как `io.ReadCloser` без буферизации
- Тело ответа также передается потоком
- Минимальное использование памяти для больших объектов

### 2. Модульность
- Четкое разделение ответственности между компонентами
- Интерфейсы для расширяемости
- Легкая замена компонентов

### 3. Тестируемость
- Unit тесты для каждого компонента
- Интеграционные тесты для полного цикла
- Покрытие кода 80.2%

### 4. Конфигурируемость
- Параметры командной строки
- Поддержка HTTP и HTTPS
- Настраиваемые таймауты

## Расширение системы

### Добавление новой операции
1. Добавить константу в `S3Operation`
2. Обновить метод `String()` для операции
3. Добавить логику определения в `RequestParser`
4. Реализовать обработку в `RequestHandler`
5. Добавить тесты

### Интеграция с реальной бизнес-логикой
1. Реализовать интерфейс `RequestHandler`
2. Заменить `MockHandler` на реальную реализацию
3. Добавить необходимые зависимости

### Добавление аутентификации
1. Расширить `S3Request` полями аутентификации
2. Добавить извлечение данных в `RequestParser`
3. Реализовать проверку в `RequestHandler`

## Производительность

### Оптимизации
- Отсутствие копирования данных в API Gateway
- Потоковая передача больших объектов
- Минимальные аллокации памяти

### Ограничения
- Один запрос = одна горутина (стандартное поведение Go HTTP сервера)
- Таймауты защищают от медленных клиентов
- Нет встроенного rate limiting

## Безопасность

### Реализованные меры
- Поддержка HTTPS/TLS
- Настраиваемые таймауты
- Валидация входных данных

### Рекомендации
- Использование reverse proxy (nginx, HAProxy)
- Настройка rate limiting на уровне proxy
- Мониторинг и логирование запросов

## Мониторинг и логирование

### Текущее логирование
- Входящие запросы (метод и путь)
- Распарсенные операции
- Коды ответов
- Ошибки парсинга

### Рекомендации для продакшена
- Структурированное логирование (JSON)
- Метрики производительности
- Трассировка запросов
- Health check endpoints
