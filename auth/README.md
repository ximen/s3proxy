# Authentication Module

Модуль аутентификации для S3 Proxy, реализующий проверку подписи AWS Signature Version 4 (SigV4) с использованием AWS SDK Go v2.

## Описание

Модуль отвечает за проверку подлинности входящих S3 запросов. Он принимает `S3Request` от Policy & Routing Engine и возвращает либо подтвержденную личность пользователя (`UserIdentity`), либо ошибку аутентификации.

## Основные возможности

- Реализация AWS Signature Version 4 (SigV4) с использованием AWS SDK Go v2
- Статический список пользователей (StaticAuthenticator)
- Расширяемая архитектура для других провайдеров аутентификации
- Безопасное сравнение подписей (защита от атак по времени)
- Подробная диагностика ошибок аутентификации
- Использование проверенной криптографической библиотеки AWS

## Зависимости

- `github.com/aws/aws-sdk-go-v2/aws` - основные типы AWS SDK
- `github.com/aws/aws-sdk-go-v2/aws/signer/v4` - подписание запросов SigV4

## Поддерживаемые провайдеры

### StaticAuthenticator
- Использует статически заданный список ключей доступа
- Подходит для тестирования и небольших развертываний
- Конфигурируется через файл или код
- Использует AWS SDK для валидации подписей

## Использование

### Создание аутентификатора

```go
// Создание через конфигурацию
config := auth.DefaultConfig()
authenticator, err := auth.NewAuthenticatorFromConfig(config)

// Создание напрямую
creds := map[string]auth.SecretKey{
    "AKIAIOSFODNN7EXAMPLE": {
        SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
        DisplayName:     "test-user",
    },
}
authenticator, err := auth.NewStaticAuthenticator(creds)
```

### Аутентификация запроса

```go
userIdentity, err := authenticator.Authenticate(s3Request)
if err != nil {
    // Обработка ошибки аутентификации
    switch err {
    case auth.ErrMissingAuthHeader:
        // Отсутствует заголовок Authorization
    case auth.ErrInvalidAccessKeyID:
        // Неизвестный Access Key
    case auth.ErrSignatureMismatch:
        // Неверная подпись
    }
    return
}

// Успешная аутентификация
fmt.Printf("Authenticated user: %s (%s)", 
    userIdentity.DisplayName, userIdentity.AccessKey)
```

## Архитектура

### Процесс валидации подписи

1. **Парсинг заголовка Authorization** - извлечение компонентов SigV4
2. **Поиск ключа доступа** - проверка существования Access Key
3. **Создание HTTP запроса** - преобразование S3Request в http.Request
4. **Подписание с AWS SDK** - использование v4.Signer для создания эталонной подписи
5. **Сравнение подписей** - безопасное сравнение с помощью `crypto/subtle`

### Преимущества использования AWS SDK

- ✅ **Проверенная реализация** - используется та же библиотека, что и в официальных AWS клиентах
- ✅ **Безопасность** - отсутствие самописной криптографии
- ✅ **Совместимость** - полная совместимость с AWS SigV4
- ✅ **Обновления** - автоматические обновления алгоритма через SDK
- ✅ **Тестирование** - библиотека протестирована AWS

## Конфигурация

### Конфигурация по умолчанию

```go
config := auth.DefaultConfig()
// Содержит тестовых пользователей для разработки
```

### Пользовательская конфигурация

```go
config := auth.Config{
    Provider: "static",
    Static: &auth.StaticConfig{
        Users: []auth.UserConfig{
            {
                AccessKey:   "AKIAIOSFODNN7EXAMPLE",
                SecretKey:   "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
                DisplayName: "my-user",
            },
        },
    },
}
```

## Ошибки аутентификации

Модуль возвращает специфические ошибки для точной диагностики:

- `ErrMissingAuthHeader` - отсутствует заголовок Authorization
- `ErrInvalidAuthHeader` - некорректный формат заголовка
- `ErrInvalidAccessKeyID` - неизвестный Access Key
- `ErrSignatureMismatch` - неверная подпись
- `ErrRequestExpired` - запрос просрочен

## Безопасность

- Использование `crypto/subtle.ConstantTimeCompare` для сравнения подписей
- Полная реализация алгоритма AWS SigV4 через AWS SDK
- Проверка временных меток запросов (опционально)
- Безопасное хранение секретных ключей
- Отсутствие самописной криптографии

## Расширение

Для добавления нового провайдера аутентификации:

1. Реализуйте интерфейс `Authenticator`
2. Добавьте новый тип в `Config`
3. Обновите `NewAuthenticatorFromConfig`

```go
type MyAuthenticator struct {
    // ваши зависимости
}

func (a *MyAuthenticator) Authenticate(req *apigw.S3Request) (*UserIdentity, error) {
    // ваша логика аутентификации
}
```

## Тестирование

```bash
# Запуск unit тестов
go test -v ./auth

# Запуск с покрытием
go test -v -cover ./auth
```

## Примеры

### Интеграция с Policy & Routing Engine

```go
// Создание аутентификатора
authenticator, err := auth.NewAuthenticatorFromConfig(authConfig)
if err != nil {
    log.Fatal(err)
}

// Использование в обработчике
func (h *PolicyRoutingHandler) Handle(req *apigw.S3Request) *apigw.S3Response {
    // Аутентификация
    userIdentity, err := h.authenticator.Authenticate(req)
    if err != nil {
        return h.handleAuthenticationError(err)
    }
    
    // Продолжение обработки с аутентифицированным пользователем
    // ...
}
```

### Тестирование с реальными S3 клиентами

Модуль полностью совместим с официальными AWS SDK и CLI:

```bash
# Настройка AWS CLI с тестовыми ключами
aws configure set aws_access_key_id AKIAIOSFODNN7EXAMPLE
aws configure set aws_secret_access_key wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
aws configure set region us-east-1

# Использование с S3 Proxy
aws s3 --endpoint-url http://localhost:9000 ls
```

## Технические детали

### Создание HTTP запроса для валидации

Модуль преобразует `S3Request` в `http.Request` для использования с AWS SDK:

```go
// Создание URL
path := s.getRequestPath(req.Bucket, req.Key)
rawURL := fmt.Sprintf("https://s3.amazonaws.com%s", path)

// Создание HTTP запроса
httpReq, err := http.NewRequest(method, rawURL, body)

// Копирование заголовков
for name, values := range req.Headers {
    for _, value := range values {
        httpReq.Header.Add(name, value)
    }
}
```

### Валидация подписи

```go
// Создание AWS credentials
awsCreds := aws.Credentials{
    AccessKeyID:     authData.AccessKey,
    SecretAccessKey: secretKey.SecretAccessKey,
}

// Подписание запроса с AWS SDK
err := s.signer.SignHTTP(context.Background(), creds, reqCopy, 
    payloadHash, service, region, signTime)

// Извлечение и сравнение подписей
expectedSignature := extractSignatureFromHeader(signedRequest)
if subtle.ConstantTimeCompare([]byte(clientSignature), []byte(expectedSignature)) != 1 {
    return ErrSignatureMismatch
}
```

## Ограничения

- В текущей версии поддерживается только StaticAuthenticator
- Не реализована интеграция с внешними системами (IAM, Vault)
- Упрощенная проверка временных меток
- Не поддерживается ротация ключей

## Производительность

- Минимальные накладные расходы на аутентификацию
- Эффективное сравнение подписей
- Использование оптимизированной реализации AWS SDK
- Кэширование не реализовано (каждый запрос проверяется заново)
