# S3 Proxy Configuration Guide

S3 Proxy поддерживает гибкую систему конфигурации через YAML файлы и переопределения из командной строки.

## Использование

### Запуск с конфигурацией по умолчанию
```bash
./s3proxy
```

### Запуск с файлом конфигурации
```bash
./s3proxy -config config.yml
```

### Переопределение параметров из командной строки
```bash
./s3proxy -config config.yml -listen :8080 -log-level debug -mock
```

## Приоритет конфигурации

1. **Флаги командной строки** (высший приоритет)
2. **Файл конфигурации** (если указан)
3. **Значения по умолчанию** (низший приоритет)

## Структура конфигурации

### Server Configuration
```yaml
server:
  listen_address: ":9000"           # Адрес для прослушивания
  tls_cert_file: ""                 # Путь к SSL сертификату
  tls_key_file: ""                  # Путь к приватному ключу SSL
  read_timeout: 30s                 # Таймаут чтения
  write_timeout: 30s                # Таймаут записи
  use_mock: false                   # Использовать Mock обработчик
```

**Переопределения командной строки:**
- `-listen` - адрес прослушивания
- `-tls-cert` - SSL сертификат
- `-tls-key` - SSL ключ
- `-read-timeout` - таймаут чтения
- `-write-timeout` - таймаут записи
- `-mock` - включить Mock режим

### Logging Configuration
```yaml
logging:
  level: "info"                     # debug, info, warn, error
```

**Переопределения командной строки:**
- `-log-level` - уровень логирования

### Authentication Configuration
```yaml
auth:
  provider: "static"                # Тип провайдера (пока только static)
  static:
    users:
      - access_key: "AKIAIOSFODNN7EXAMPLE"
        secret_key: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
        display_name: "Admin User"
```

### Backend Configuration
```yaml
backend:
  manager:
    health_check_interval: 15s      # Интервал проверки здоровья
    check_timeout: 5s               # Таймаут одной проверки
    failure_threshold: 3            # Неудач для перехода в DOWN
    success_threshold: 2            # Успехов для перехода в UP
    circuit_breaker_window: 60s     # Окно Circuit Breaker
    circuit_breaker_threshold: 5    # Порог срабатывания CB
    initial_state: "PROBING"        # Начальное состояние
  
  backends:
    backend-name:
      endpoint: "https://s3.amazonaws.com"
      region: "us-east-1"
      bucket: "bucket-name"
      access_key: "ACCESS_KEY"
      secret_key: "SECRET_KEY"
```

**Переопределения командной строки:**
- `-disable-backends` - отключить Backend Manager

### Monitoring Configuration
```yaml
monitoring:
  enabled: true                     # Включить мониторинг
  listen_address: ":9091"           # Адрес для метрик
  metrics_path: "/metrics"          # Путь к эндпоинту метрик
  read_timeout: 30s                 # Таймаут чтения
  write_timeout: 30s                # Таймаут записи
  enable_system_metrics: true       # Системные метрики
  system_metrics_interval: 15s      # Интервал сбора системных метрик
```

**Переопределения командной строки:**
- `-metrics-listen` - адрес для метрик
- `-disable-metrics` - отключить мониторинг

### Routing Configuration
```yaml
routing:
  policies:
    put:
      ack: "one"                    # one, all
    delete:
      ack: "all"                    # one, all
    get:
      strategy: "first"             # first, newest
```

## Примеры конфигураций

### Продакшн конфигурация
```yaml
# config.yml - полная продакшн конфигурация
server:
  listen_address: ":9000"
  tls_cert_file: "/etc/ssl/certs/s3proxy.crt"
  tls_key_file: "/etc/ssl/private/s3proxy.key"
  read_timeout: 60s
  write_timeout: 60s
  use_mock: false

logging:
  level: "info"

auth:
  provider: "static"
  static:
    users:
      - access_key: "PROD_ACCESS_KEY"
        secret_key: "PROD_SECRET_KEY"
        display_name: "Production User"

backend:
  manager:
    health_check_interval: 30s
    check_timeout: 10s
    failure_threshold: 5
    success_threshold: 3
    circuit_breaker_window: 300s
    circuit_breaker_threshold: 10
    initial_state: "PROBING"
  
  backends:
    primary:
      endpoint: "https://s3.amazonaws.com"
      region: "us-east-1"
      bucket: "prod-primary-bucket"
      access_key: "AWS_ACCESS_KEY"
      secret_key: "AWS_SECRET_KEY"
    
    backup:
      endpoint: "https://s3.eu-central-1.amazonaws.com"
      region: "eu-central-1"
      bucket: "prod-backup-bucket"
      access_key: "AWS_ACCESS_KEY"
      secret_key: "AWS_SECRET_KEY"

monitoring:
  enabled: true
  listen_address: ":9091"
  metrics_path: "/metrics"
  read_timeout: 30s
  write_timeout: 30s
  enable_system_metrics: true
  system_metrics_interval: 60s

routing:
  policies:
    put:
      ack: "one"
    delete:
      ack: "all"
    get:
      strategy: "newest"
```

### Разработка конфигурация
```yaml
# config-dev.yml - упрощенная конфигурация для разработки
server:
  listen_address: ":9000"
  use_mock: true

logging:
  level: "debug"

auth:
  provider: "static"
  static:
    users:
      - access_key: "test"
        secret_key: "test123"
        display_name: "Test User"

backend:
  manager:
    health_check_interval: 5s
    check_timeout: 2s
    failure_threshold: 2
    success_threshold: 1
  
  backends:
    local:
      endpoint: "http://localhost:9001"
      region: "us-east-1"
      bucket: "test-bucket"
      access_key: "minioadmin"
      secret_key: "minioadmin"

monitoring:
  enabled: true
  listen_address: ":9091"
  enable_system_metrics: false

routing:
  policies:
    put:
      ack: "one"
    delete:
      ack: "one"
    get:
      strategy: "first"
```

## Валидация конфигурации

При запуске S3 Proxy автоматически валидирует конфигурацию:

- Проверяет обязательные поля
- Валидирует форматы (таймауты, адреса)
- Проверяет логическую согласованность
- Валидирует TLS конфигурацию
- Проверяет уникальность access_key в аутентификации

## Переменные окружения

В будущих версиях планируется поддержка переменных окружения:

```yaml
backend:
  backends:
    aws:
      access_key: "${AWS_ACCESS_KEY}"
      secret_key: "${AWS_SECRET_KEY}"
```

## Генерация конфигурации

Для генерации примера конфигурации можно использовать:

```bash
# Создать пример конфигурации
./s3proxy -generate-config > my-config.yml
```

## Отладка конфигурации

Для отладки конфигурации используйте:

```bash
# Показать загруженную конфигурацию
./s3proxy -config config.yml -log-level debug

# Проверить конфигурацию без запуска
./s3proxy -config config.yml -validate-config
```

## Безопасность

**Важные рекомендации по безопасности:**

1. **Файлы конфигурации** должны иметь ограниченные права доступа:
   ```bash
   chmod 600 config.yml
   ```

2. **Секретные ключи** не должны храниться в открытом виде в репозитории

3. **TLS** должен быть включен в продакшне:
   ```yaml
   server:
     tls_cert_file: "/path/to/cert.pem"
     tls_key_file: "/path/to/key.pem"
   ```

4. **Мониторинг** должен быть защищен от внешнего доступа

## Миграция конфигурации

При обновлении версий S3 Proxy:

1. Проверьте CHANGELOG на изменения в конфигурации
2. Используйте валидацию для проверки совместимости
3. Тестируйте новую конфигурацию в dev окружении

## Поддержка

Для получения помощи по конфигурации:

```bash
./s3proxy -help
```

Или обратитесь к документации в README.md
