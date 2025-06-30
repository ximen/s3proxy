# S3 Proxy API Gateway

Модуль API Gateway для S3 Proxy - единственная точка входа для всех S3-клиентов.

## Описание

API Gateway принимает входящие HTTP-запросы, преобразует их в стандартизированное внутреннее представление S3-операций и передает на обработку в следующий модуль системы.

## Основные возможности

- Поддержка всех основных S3 операций
- Path-Style URL формат (`http://s3.example.com/bucket-name/key-name`)
- Multipart upload операции
- Потоковая обработка больших объектов
- Стандартные S3 XML ответы об ошибках
- Поддержка HTTP и HTTPS
- Настраиваемые таймауты
- Два режима работы: Mock (для тестирования) и Real (с файловой системой)

## Поддерживаемые S3 операции

### Объекты
- `GET /bucket/key` - Получение объекта
- `PUT /bucket/key` - Загрузка объекта
- `HEAD /bucket/key` - Получение метаданных объекта
- `DELETE /bucket/key` - Удаление объекта

### Списки
- `GET /` - Список бакетов
- `GET /bucket/` - Список объектов в бакете

### Multipart Upload
- `POST /bucket/key?uploads` - Инициация multipart загрузки
- `PUT /bucket/key?partNumber=N&uploadId=ID` - Загрузка части
- `POST /bucket/key?uploadId=ID` - Завершение multipart загрузки
- `DELETE /bucket/key?uploadId=ID` - Отмена multipart загрузки
- `GET /bucket/?uploads` - Список активных multipart загрузок

## Установка и запуск

### Сборка

```bash
go build -o s3proxy
```

### Запуск

#### Быстрый старт (конфигурация по умолчанию)
```bash
./s3proxy
```

#### Запуск с файлом конфигурации
```bash
./s3proxy -config config.yml
```

#### Запуск для разработки
```bash
./s3proxy -config config-dev.yml
```

#### Основные параметры командной строки

```bash
# Запуск с Mock обработчиком (для тестирования)
./s3proxy -mock

# Запуск с конфигурационным файлом
./s3proxy -config config.yml

# Запуск на другом порту (переопределяет конфигурацию)
./s3proxy -config config.yml -listen :8080

# Запуск с HTTPS
./s3proxy -tls-cert server.crt -tls-key server.key

# Настройка таймаутов
./s3proxy -read-timeout 60s -write-timeout 60s

# Включение debug логирования для отладки
./s3proxy -log-level debug

# Запуск с минимальным логированием
./s3proxy -log-level error

# Отключение мониторинга
./s3proxy -disable-metrics

# Отключение backend manager (только mock бэкенды)
./s3proxy -disable-backends
```

### Конфигурация

S3 Proxy поддерживает гибкую систему конфигурации через YAML файлы с возможностью переопределения параметров из командной строки.

**Приоритет конфигурации:**
1. Флаги командной строки (высший)
2. Файл конфигурации
3. Значения по умолчанию (низший)

**Примеры конфигурационных файлов:**
- `config.yml` - полная продакшн конфигурация
- `config-dev.yml` - упрощенная конфигурация для разработки

Подробная документация по конфигурации: [CONFIGURATION.md](CONFIGURATION.md)

### Параметры командной строки

Полный список параметров:

```bash
./s3proxy -help
```

- `-config` - Путь к файлу конфигурации (YAML)
- `-listen` - Адрес для прослушивания (переопределяет конфигурацию)
- `-tls-cert` - Путь к SSL сертификату (переопределяет конфигурацию)
- `-tls-key` - Путь к приватному ключу SSL (переопределяет конфигурацию)
- `-read-timeout` - Таймаут чтения (переопределяет конфигурацию)
- `-write-timeout` - Таймаут записи (переопределяет конфигурацию)
- `-mock` - Использовать Mock обработчик (переопределяет конфигурацию)
- `-log-level` - Уровень логирования: `debug`, `info`, `warn`, `error` (переопределяет конфигурацию)
- `-metrics-listen` - Адрес сервера метрик (переопределяет конфигурацию)
- `-disable-metrics` - Отключить сбор метрик (переопределяет конфигурацию)
- `-disable-backends` - Отключить backend manager

## Тестирование

### Запуск unit тестов

```bash
go test -v ./...
```

### Запуск интеграционных тестов

```bash
go test -v -run Integration
```

### Запуск всех тестов с покрытием

```bash
go test -v -cover ./...
```

## Примеры использования

### Тестирование с curl

```bash
# Запуск сервера с Policy & Routing Engine (симуляция)
./s3proxy

# Загрузка объекта
curl -X PUT -d "Hello World" http://localhost:9000/my-bucket/my-object.txt

# Получение объекта
curl http://localhost:9000/my-bucket/my-object.txt

# Получение метаданных объекта
curl -I http://localhost:9000/my-bucket/my-object.txt

# Список объектов в бакете
curl http://localhost:9000/my-bucket/

# Список бакетов
curl http://localhost:9000/

# Удаление объекта
curl -X DELETE http://localhost:9000/my-bucket/my-object.txt
```

### Тестирование multipart upload

```bash
# Инициация multipart upload
curl -X POST http://localhost:9000/my-bucket/large-file.bin?uploads

# Загрузка части (предполагая uploadId=abc123)
curl -X PUT -d "part1 data" "http://localhost:9000/my-bucket/large-file.bin?partNumber=1&uploadId=abc123"

# Завершение multipart upload
curl -X POST -d "<CompleteMultipartUpload></CompleteMultipartUpload>" "http://localhost:9000/my-bucket/large-file.bin?uploadId=abc123"
```

## Архитектура

### Структура проекта

```
s3proxy/
├── main.go                 # Точка входа приложения
├── apigw/                  # Пакет API Gateway
│   ├── config.go          # Конфигурация
│   ├── types.go           # Типы данных (S3Request, S3Response, etc.)
│   ├── gateway.go         # Основная логика API Gateway
│   ├── parser.go          # Парсер HTTP запросов в S3 операции
│   ├── parser_test.go     # Тесты парсера
│   └── response_writer.go # Формирование HTTP ответов
├── auth/                   # Пакет аутентификации
│   ├── types.go           # Интерфейсы и типы данных
│   ├── static_authenticator.go # Статический аутентификатор
│   ├── static_authenticator_test.go # Тесты аутентификатора
│   ├── config.go          # Конфигурация аутентификации
│   ├── config_test.go     # Тесты конфигурации
│   └── README.md          # Документация модуля
├── handlers/               # Пакет обработчиков (заглушки для следующих модулей)
│   ├── mock.go            # Mock обработчик для тестирования
│   ├── s3handler.go       # Заглушка Policy & Routing Engine с аутентификацией
│   └── s3handler_test.go  # Тесты заглушки
└── integration_test.go     # Интеграционные тесты
```

### Компоненты

1. **HTTP Listener** - Обработка входящих HTTP соединений
2. **Request Parser** - Парсинг S3 запросов из HTTP
3. **Request Dispatcher** - Маршрутизация к обработчику
4. **Response Writer** - Формирование HTTP ответов
5. **Authentication Module** - Проверка подписи AWS SigV4
6. **Request Handlers** - Интерфейс для следующих модулей системы

### Обработчики

#### Mock Handler
- Используется для тестирования API Gateway
- Возвращает фиксированные ответы
- Не выполняет реальную бизнес-логику
- Включается флагом `-mock` или в конфигурации `server.use_mock: true`

#### Policy & Routing Engine Handler (реальная реализация)
- Использует реальные модули системы:
  - **Replicator** - для операций записи (PUT, DELETE, Multipart Upload)
  - **Fetcher** - для операций чтения (GET, HEAD, LIST)
  - **Authentication Module** - для проверки подписи AWS SigV4
- Применяет политики доступа и маршрутизации
- Работает с реальными S3 бэкендами
- Поддерживает репликацию и отказоустойчивость

### Зона ответственности API Gateway

**API Gateway отвечает ТОЛЬКО за:**
- Прием HTTP запросов
- Парсинг S3 операций из HTTP
- Преобразование в стандартизированный `S3Request`
- Передачу запроса следующему модулю через интерфейс `RequestHandler`
- Формирование HTTP ответа из полученного `S3Response`

**API Gateway НЕ отвечает за:**
- Сохранение файлов (это делает Storage Engine)
- Аутентификацию и авторизацию (это делает Policy & Routing Engine)
- Управление бэкендами (это делает Policy & Routing Engine)
- Репликацию данных (это делает Storage Engine)

### Структуры данных

- `S3Request` - Внутреннее представление S3 запроса
- `S3Response` - Внутреннее представление S3 ответа
- `RequestHandler` - Интерфейс для следующих модулей системы

### Жизненный цикл запроса

1. **API Gateway** принимает HTTP запрос
2. **Request Parser** парсит его в `S3Request`
3. **API Gateway** передает `S3Request` в `RequestHandler`
4. **Policy & Routing Engine**:
   - Выполняет аутентификацию через **Authentication Module**
   - Применяет политики доступа и авторизацию
   - Для операций записи: маршрутизирует к **Replicator**
   - Для операций чтения: маршрутизирует к **Fetcher**
5. **Replicator/Fetcher** выполняет операцию с реальными S3 бэкендами
6. Результат возвращается через цепочку модулей как `S3Response`
7. **API Gateway** формирует HTTP ответ из `S3Response`

## Расширение

Для интеграции с реальной бизнес-логикой необходимо:

1. Реализовать интерфейс `RequestHandler`
2. Добавить новый обработчик в пакет `handlers`
3. Обновить `main.go` для поддержки нового обработчика

Пример:

```go
type MyHandler struct {
    // ваши зависимости
}

func (h *MyHandler) Handle(req *apigw.S3Request) *apigw.S3Response {
    // ваша бизнес-логика
    return &apigw.S3Response{
        StatusCode: http.StatusOK,
        // ...
    }
}

func main() {
    handler := &MyHandler{}
    gateway := apigw.New(config, handler)
    gateway.Start()
}
```

## Логирование

API Gateway поддерживает 4 уровня логирования:

- `debug` - Подробная отладочная информация (заголовки запросов, детали парсинга, аутентификации)
- `info` - Основная информация о запросах и операциях (по умолчанию)
- `warn` - Предупреждения
- `error` - Только ошибки

### Что логируется на каждом уровне:

**DEBUG:**
- Все заголовки HTTP запросов
- Детали парсинга S3 операций
- Подробности аутентификации (подписи, ключи)
- Детали формирования ответов
- Внутренние структуры данных

**INFO:**
- Входящие запросы (метод и путь)
- Распарсенные операции
- Коды ответов
- Конфигурация при запуске

**WARN:**
- Предупреждения о потенциальных проблемах

**ERROR:**
- Ошибки парсинга и записи ответов
- Критические ошибки

### Примеры использования:

```bash
# Отладка проблем с аутентификацией
./s3proxy -log-level debug

# Продакшн режим с минимальным логированием
./s3proxy -log-level error

# Обычная работа (по умолчанию)
./s3proxy -log-level info
```

## Безопасность

- Поддержка HTTPS через TLS сертификаты
- Настраиваемые таймауты для защиты от медленных клиентов
- Потоковая обработка для предотвращения DoS атак через большие файлы

## Ограничения

- Поддерживается только Path-Style URL формат
- Virtual-Hosted-Style не реализован в первой версии
- Аутентификация и авторизация выполняется в следующих модулях
- Multipart upload в реальном обработчике имеет базовую реализацию

## Производительность

- Потоковая обработка тел запросов и ответов
- Минимальное использование памяти для больших объектов
- Отсутствие буферизации данных в API Gateway
- Прямая работа с файловой системой в реальном обработчике
