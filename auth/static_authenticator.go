package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"

	"s3proxy/apigw"
	"s3proxy/logger"
)

// StaticAuthenticator реализует интерфейс Authenticator,
// используя для проверки map[accessKey]secretKey и AWS SDK для валидации подписи.
type StaticAuthenticator struct {
	credentials map[string]SecretKey // credentials - это карта для хранения ключей.
	signer      *v4.Signer           // signer - AWS SDK signer для валидации подписей
	metrics     *Metrics
}

// NewStaticAuthenticator создает новый экземпляр аутентификатора.
func NewStaticAuthenticator(creds map[string]SecretKey) (*StaticAuthenticator, error) {
	if len(creds) == 0 {
		return nil, errors.New("credentials map cannot be nil or empty")
	}

	return &StaticAuthenticator{
		credentials: creds,
		signer:      v4.NewSigner(),
		metrics:     NewMetrics(),
	}, nil
}

// Authenticate реализует интерфейс Authenticator
func (s *StaticAuthenticator) Authenticate(req *apigw.S3Request) (*UserIdentity, error) {
	start := time.Now()
	var latency float64

	logger.Debug("Starting authentication for request")
	logger.Debug("Request headers: %+v", req.Headers)

	// 1. Извлечение заголовка Authorization
	authHeader := req.Headers.Get("Authorization")
	if authHeader == "" {
		logger.Debug("Missing Authorization header")
		s.metrics.AuthRequestsTotal.WithLabelValues("error").Inc()
		return nil, ErrMissingAuthHeader
	}

	logger.Debug("Authorization header: %s", authHeader)

	// 2. Парсинг заголовка SigV4
	authData, err := s.parseAuthorizationHeader(authHeader)
	if err != nil {
		logger.Debug("Failed to parse authorization header: %v", err)
		s.metrics.AuthRequestsTotal.WithLabelValues("error").Inc()
		return nil, err
	}

	logger.Debug("Parsed auth data - AccessKey: %s, Region: %s, Service: %s",
		authData.AccessKey, authData.Region, authData.Service)

	// 3. Поиск ключа в credentials
	secretKey, exists := s.credentials[authData.AccessKey]
	if !exists {
		logger.Debug("Access key not found: %s", authData.AccessKey)
		s.metrics.AuthRequestsTotal.WithLabelValues("error").Inc()
		return nil, ErrInvalidAccessKeyID
	}

	logger.Debug("Found credentials for access key: %s", authData.AccessKey)

	// 4. Создание HTTP запроса для валидации подписи
	httpReq, err := s.createHTTPRequest(req, authData)
	if err != nil {
		logger.Debug("Failed to create HTTP request: %v", err)
		s.metrics.AuthRequestsTotal.WithLabelValues("error").Inc()
		return nil, fmt.Errorf("failed to create HTTP request: %v", err)
	}

	logger.Debug("Created HTTP request: %s %s", httpReq.Method, httpReq.URL.String())
	logger.Debug("HTTP request headers: %+v", httpReq.Header)

	// 5. Создание AWS credentials для валидации
	awsCreds := aws.Credentials{
		AccessKeyID:     authData.AccessKey,
		SecretAccessKey: secretKey.SecretAccessKey,
	}

	// 6. Вычисление ожидаемой подписи с помощью AWS SDK
	expectedSignature, err := s.calculateExpectedSignature(httpReq, awsCreds, authData)
	if err != nil {
		logger.Debug("Failed to calculate expected signature: %v", err)
		s.metrics.AuthRequestsTotal.WithLabelValues("error").Inc()
		return nil, fmt.Errorf("failed to calculate expected signature: %v", err)
	}

	logger.Debug("Client signature: %s", authData.Signature)
	logger.Debug("Expected signature: %s", expectedSignature)

	// 7. Сравнение подписей (безопасное от атак по времени)
	if subtle.ConstantTimeCompare([]byte(authData.Signature), []byte(expectedSignature)) != 1 {
		logger.Debug("Signature mismatch")
		s.metrics.AuthRequestsTotal.WithLabelValues("error").Inc()
		return nil, ErrSignatureMismatch
	}

	logger.Debug("Authentication successful for user: %s", secretKey.DisplayName)

	latency = time.Since(start).Seconds()
	s.metrics.AuthRequestsTotal.WithLabelValues("success").Inc()
	s.metrics.AuthLatency.WithLabelValues("success").Observe(latency)

	// 8. Успех - возвращаем идентификацию пользователя
	return &UserIdentity{
		AccessKey:   authData.AccessKey,
		DisplayName: secretKey.DisplayName,
	}, nil
}

// authorizationData содержит распарсенные данные из заголовка Authorization
type authorizationData struct {
	AccessKey     string
	Date          string
	Region        string
	Service       string
	SignedHeaders []string
	Signature     string
	Algorithm     string
	Timestamp     string
}

// parseAuthorizationHeader парсит заголовок Authorization в формате AWS SigV4
func (s *StaticAuthenticator) parseAuthorizationHeader(authHeader string) (*authorizationData, error) {
	logger.Debug("Parsing authorization header: %s", authHeader)

	// Проверяем, что заголовок начинается с AWS4-HMAC-SHA256
	if !strings.HasPrefix(authHeader, "AWS4-HMAC-SHA256 ") {
		logger.Debug("Header doesn't start with AWS4-HMAC-SHA256")
		return nil, ErrInvalidAuthHeader
	}

	// Убираем префикс
	authContent := strings.TrimPrefix(authHeader, "AWS4-HMAC-SHA256 ")
	logger.Debug("Auth content after prefix removal: %s", authContent)

	// Парсим компоненты - поддерживаем как ", " так и ","
	var parts []string
	if strings.Contains(authContent, ", ") {
		parts = strings.Split(authContent, ", ")
		logger.Debug("Split by ', ' - got %d parts", len(parts))
	} else {
		parts = strings.Split(authContent, ",")
		logger.Debug("Split by ',' - got %d parts", len(parts))
	}

	if len(parts) != 3 {
		logger.Debug("Expected 3 parts, got %d", len(parts))
		return nil, ErrInvalidAuthHeader
	}

	authData := &authorizationData{
		Algorithm: "AWS4-HMAC-SHA256",
	}

	for i, part := range parts {
		part = strings.TrimSpace(part) // Убираем лишние пробелы
		logger.Debug("Processing part %d: %s", i, part)

		if strings.HasPrefix(part, "Credential=") {
			credential := strings.TrimPrefix(part, "Credential=")
			credParts := strings.Split(credential, "/")
			if len(credParts) != 5 {
				logger.Debug("Expected 5 credential parts, got %d", len(credParts))
				return nil, ErrInvalidAuthHeader
			}
			authData.AccessKey = credParts[0]
			authData.Date = credParts[1]
			authData.Region = credParts[2]
			authData.Service = credParts[3]
			logger.Debug("Parsed credential - AccessKey: %s, Date: %s, Region: %s, Service: %s",
				authData.AccessKey, authData.Date, authData.Region, authData.Service)
		} else if strings.HasPrefix(part, "SignedHeaders=") {
			signedHeaders := strings.TrimPrefix(part, "SignedHeaders=")
			authData.SignedHeaders = strings.Split(signedHeaders, ";")
			logger.Debug("Parsed signed headers: %v", authData.SignedHeaders)
		} else if strings.HasPrefix(part, "Signature=") {
			authData.Signature = strings.TrimPrefix(part, "Signature=")
			logger.Debug("Parsed signature: %s", authData.Signature)
		}
	}

	// Проверяем, что все необходимые поля заполнены
	if authData.AccessKey == "" || authData.Date == "" || authData.Region == "" ||
		authData.Service == "" || len(authData.SignedHeaders) == 0 || authData.Signature == "" {
		logger.Debug("Missing required fields in authorization data")
		return nil, ErrInvalidAuthHeader
	}

	logger.Debug("Successfully parsed authorization header")
	return authData, nil
}

// createHTTPRequest создает http.Request из S3Request для валидации подписи
func (s *StaticAuthenticator) createHTTPRequest(req *apigw.S3Request, authData *authorizationData) (*http.Request, error) {
	// Определяем HTTP метод
	method := s.getHTTPMethod(req.Operation)

	// Получаем хост и схему из S3Request
	host := req.Host
	if host == "" {
		host = "s3.amazonaws.com" // fallback
	}

	scheme := req.Scheme
	if scheme == "" {
		scheme = "https" // fallback для совместимости
	}

	path := s.getRequestPath(req.Bucket, req.Key)
	rawURL := fmt.Sprintf("%s://%s%s", scheme, host, path)

	// Добавляем query параметры
	if len(req.Query) > 0 {
		rawURL += "?" + req.Query.Encode()
	}

	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %v", err)
	}

	logger.Debug("Creating HTTP request: %s %s", method, parsedURL.String())

	// Для валидации подписи нам не нужно тело, только его хэш из заголовка.
	// Передаем nil, чтобы не "потратить" реальный поток req.Body.
	httpReq, err := http.NewRequest(method, parsedURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %v", err)
	}

	// Копируем только заголовки, указанные в SignedHeaders
	signedHeadersSet := make(map[string]bool)
	for _, header := range authData.SignedHeaders {
		signedHeadersSet[strings.ToLower(header)] = true
	}

	for name, values := range req.Headers {
		lowerName := strings.ToLower(name)
		if lowerName == "authorization" {
			continue
		}
		if signedHeadersSet[lowerName] {
			for _, value := range values {
				httpReq.Header.Add(name, value)
			}
		}
	}

	// Принудительно устанавливаем Host заголовок
	httpReq.Header.Set("Host", host)
	httpReq.Host = host

	// Устанавливаем Content-Length для http.Request, чтобы он соответствовал запросу клиента
	httpReq.ContentLength = req.ContentLength

	logger.Debug("Created HTTP request with headers: %+v", httpReq.Header)
	return httpReq, nil
}

// calculateExpectedSignature вычисляет ожидаемую подпись с помощью AWS SDK
func (s *StaticAuthenticator) calculateExpectedSignature(httpReq *http.Request, creds aws.Credentials, authData *authorizationData) (string, error) {
	// Определяем время подписи из заголовков
	dateHeader := httpReq.Header.Get("x-amz-date")
	if dateHeader == "" {
		dateHeader = httpReq.Header.Get("Date")
		if dateHeader == "" {
			return "", errors.New("missing required date header (x-amz-date or Date)")
		}
	}

	signTime, err := time.Parse(time.RFC1123, dateHeader)
	if err != nil {
		signTime, err = time.Parse("20060102T150405Z", dateHeader)
		if err != nil {
			return "", fmt.Errorf("failed to parse date header: %v", err)
		}
	}

	logger.Debug("Signing request with time: %v", signTime)

	// Получаем хэш тела запроса
	bodyHash := httpReq.Header.Get("x-amz-content-sha256")
	if bodyHash == "" {
		// Для PUT/POST запросов этот заголовок обязателен
		if httpReq.Method == "PUT" || httpReq.Method == "POST" {
			return "", errors.New("missing required header for PUT/POST: x-amz-content-sha256")
		}
		// Для других запросов (GET, HEAD, DELETE) можно использовать хэш пустого тела
		bodyHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	}

	// Подписываем запрос
	err = s.signer.SignHTTP(context.Background(), creds, httpReq, bodyHash, authData.Service, authData.Region, signTime)
	if err != nil {
		return "", fmt.Errorf("failed to sign request: %v", err)
	}

	// Извлекаем подпись из подписанного запроса
	signedAuthHeader := httpReq.Header.Get("Authorization")
	if signedAuthHeader == "" {
		return "", fmt.Errorf("no authorization header in signed request")
	}

	logger.Debug("SDK generated authorization header: %s", signedAuthHeader)

	// Парсим подпись из заголовка
	const signaturePrefix = "Signature="
	start := strings.Index(signedAuthHeader, signaturePrefix)
	if start == -1 {
		return "", fmt.Errorf("no signature found in authorization header")
	}
	signature := signedAuthHeader[start+len(signaturePrefix):]

	logger.Debug("Extracted signature: %s", signature)
	return signature, nil
}

// getHTTPMethod возвращает HTTP метод для операции
func (s *StaticAuthenticator) getHTTPMethod(operation apigw.S3Operation) string {
	switch operation {
	case apigw.GetObject, apigw.ListObjectsV2, apigw.ListBuckets, apigw.ListMultipartUploads:
		return "GET"
	case apigw.PutObject, apigw.UploadPart:
		return "PUT"
	case apigw.HeadObject:
		return "HEAD"
	case apigw.DeleteObject, apigw.AbortMultipartUpload:
		return "DELETE"
	case apigw.CreateMultipartUpload, apigw.CompleteMultipartUpload:
		return "POST"
	default:
		return "GET" // По умолчанию
	}
}

// getRequestPath возвращает путь запроса
func (s *StaticAuthenticator) getRequestPath(bucket, key string) string {
	if bucket == "" {
		return "/"
	}
	if key == "" {
		return "/" + bucket + "/"
	}
	return "/" + bucket + "/" + key
}
