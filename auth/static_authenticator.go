package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/url" // <-- Импорт добавлен
	"sort"
	"strings"
	"time"

	"s3proxy/apigw"
	"s3proxy/logger"
)

// StaticAuthenticator реализует интерфейс Authenticator,
// используя для проверки map[accessKey]secretKey и ручную валидацию подписи SigV4.
type StaticAuthenticator struct {
	credentials map[string]SecretKey // credentials - это карта для хранения ключей.
	metrics     *Metrics
}

// NewStaticAuthenticator создает новый экземпляр аутентификатора.
func NewStaticAuthenticator(creds map[string]SecretKey) (*StaticAuthenticator, error) {
	if len(creds) == 0 {
		return nil, errors.New("credentials map cannot be nil or empty")
	}

	return &StaticAuthenticator{
		credentials: creds,
		metrics:     NewMetrics(),
	}, nil
}

// Authenticate реализует интерфейс Authenticator. Логика полностью переработана для корректной валидации.
func (s *StaticAuthenticator) Authenticate(req *apigw.S3Request) (*UserIdentity, error) {
	start := time.Now()
	logger.Debug("Starting authentication for request")

	// 1. Извлечение и парсинг заголовка Authorization
	authHeader := req.Headers.Get("Authorization")
	if authHeader == "" {
		logger.Debug("Missing Authorization header")
		s.metrics.AuthRequestsTotal.WithLabelValues("error").Inc()
		return nil, ErrMissingAuthHeader
	}

	authData, err := s.parseAuthorizationHeader(authHeader, req.Headers.Get("x-amz-date"))
	if err != nil {
		logger.Debug("Failed to parse authorization header: %v", err)
		s.metrics.AuthRequestsTotal.WithLabelValues("error").Inc()
		return nil, err
	}

	// 2. Поиск ключа в credentials
	secretKey, exists := s.credentials[authData.AccessKey]
	if !exists {
		logger.Debug("Access key not found: %s", authData.AccessKey)
		s.metrics.AuthRequestsTotal.WithLabelValues("error").Inc()
		return nil, ErrInvalidAccessKeyID
	}
	logger.Debug("Found credentials for access key: %s", authData.AccessKey)

	// 3. Ручное построение канонического запроса (Canonical Request)
	canonicalRequest, err := s.buildCanonicalRequest(req, authData)
	if err != nil {
		logger.Debug("Failed to build canonical request: %v", err)
		s.metrics.AuthRequestsTotal.WithLabelValues("error").Inc()
		return nil, err
	}
	logger.Debug("Built Canonical Request:\n%s", canonicalRequest)

	// 4. Построение строки для подписи (String to Sign)
	stringToSign := s.buildStringToSign(canonicalRequest, authData)
	logger.Debug("Built String to Sign:\n%s", stringToSign)

	// 5. Вычисление ожидаемой подписи
	expectedSignature := s.calculateSignature(stringToSign, secretKey.SecretAccessKey, authData)
	logger.Debug("Client signature:   %s", authData.Signature)
	logger.Debug("Expected signature: %s", expectedSignature)

	// 6. Сравнение подписей (безопасное от атак по времени)
	if subtle.ConstantTimeCompare([]byte(authData.Signature), []byte(expectedSignature)) != 1 {
		logger.Debug("Signature mismatch")
		s.metrics.AuthRequestsTotal.WithLabelValues("error").Inc()
		return nil, ErrSignatureMismatch
	}

	logger.Debug("Authentication successful for user: %s", secretKey.DisplayName)

	latency := time.Since(start).Seconds()
	s.metrics.AuthRequestsTotal.WithLabelValues("success").Inc()
	s.metrics.AuthLatency.WithLabelValues("success").Observe(latency)

	return &UserIdentity{
		AccessKey:   authData.AccessKey,
		DisplayName: secretKey.DisplayName,
	}, nil
}

// buildCanonicalRequest создает строку канонического запроса согласно спецификации AWS.
func (s *StaticAuthenticator) buildCanonicalRequest(req *apigw.S3Request, authData *authorizationData) (string, error) {
	httpMethod := s.getHTTPMethod(req.Operation)
	canonicalURI := s.getRequestPath(req.Bucket, req.Key)
	canonicalQueryString := req.Query.Encode()

	var canonicalHeaders strings.Builder
	headerKeys := make([]string, 0, len(authData.SignedHeaders))
	headerValues := make(map[string]string)

	for _, key := range authData.SignedHeaders {
		lowerKey := strings.ToLower(key)
		headerKeys = append(headerKeys, lowerKey)
		if lowerKey == "host" {
			headerValues[lowerKey] = req.Host
		} else {
			val := req.Headers.Get(key)
			headerValues[lowerKey] = strings.TrimSpace(val)
		}
	}
	sort.Strings(headerKeys)

	for _, key := range headerKeys {
		fmt.Fprintf(&canonicalHeaders, "%s:%s\n", key, headerValues[key])
	}

	signedHeaders := strings.Join(authData.SignedHeaders, ";")
	payloadHash := req.Headers.Get("x-amz-content-sha256")
	if payloadHash == "" {
		payloadHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	}

	return fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		httpMethod, canonicalURI, canonicalQueryString, canonicalHeaders.String(), signedHeaders, payloadHash), nil
}

// buildStringToSign создает "строку для подписи" на основе канонического запроса.
func (s *StaticAuthenticator) buildStringToSign(canonicalRequest string, authData *authorizationData) string {
	algorithm := authData.Algorithm
	timestamp := authData.Timestamp
	scope := fmt.Sprintf("%s/%s/%s/aws4_request", authData.Date, authData.Region, authData.Service)

	hash := sha256.New()
	hash.Write([]byte(canonicalRequest))
	hashedCanonicalRequest := fmt.Sprintf("%x", hash.Sum(nil))

	return fmt.Sprintf("%s\n%s\n%s\n%s",
		algorithm, timestamp, scope, hashedCanonicalRequest)
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

func (s *StaticAuthenticator) calculateSignature(stringToSign, secretKey string, authData *authorizationData) string {
	kSecret := "AWS4" + secretKey
	kDate := hmacSHA256([]byte(kSecret), authData.Date)
	kRegion := hmacSHA256(kDate, authData.Region)
	kService := hmacSHA256(kRegion, authData.Service)
	kSigning := hmacSHA256(kService, "aws4_request")

	signature := hmac.New(sha256.New, kSigning)
	signature.Write([]byte(stringToSign))
	return fmt.Sprintf("%x", signature.Sum(nil))
}

type authorizationData struct {
	AccessKey, Date, Region, Service, Signature, Algorithm, Timestamp string
	SignedHeaders                                                     []string
}

func (s *StaticAuthenticator) parseAuthorizationHeader(authHeader, xAmzDate string) (*authorizationData, error) {
	if !strings.HasPrefix(authHeader, "AWS4-HMAC-SHA256 ") {
		return nil, ErrInvalidAuthHeader
	}
	authContent := strings.TrimPrefix(authHeader, "AWS4-HMAC-SHA256 ")
	parts := strings.Split(authContent, ", ")
	if len(parts) != 3 {
		parts = strings.Split(authContent, ",")
		if len(parts) != 3 {
			return nil, ErrInvalidAuthHeader
		}
	}
	authData := &authorizationData{Algorithm: "AWS4-HMAC-SHA256", Timestamp: xAmzDate}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "Credential=") {
			credential := strings.TrimPrefix(part, "Credential=")
			credParts := strings.Split(credential, "/")
			if len(credParts) != 5 {
				return nil, ErrInvalidAuthHeader
			}
			authData.AccessKey, authData.Date, authData.Region, authData.Service = credParts[0], credParts[1], credParts[2], credParts[3]
		} else if strings.HasPrefix(part, "SignedHeaders=") {
			authData.SignedHeaders = strings.Split(strings.TrimPrefix(part, "SignedHeaders="), ";")
		} else if strings.HasPrefix(part, "Signature=") {
			authData.Signature = strings.TrimPrefix(part, "Signature=")
		}
	}
	if authData.AccessKey == "" || authData.Date == "" || authData.Region == "" || authData.Service == "" || len(authData.SignedHeaders) == 0 || authData.Signature == "" || authData.Timestamp == "" {
		return nil, ErrInvalidAuthHeader
	}
	return authData, nil
}

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
		return "GET"
	}
}

// getRequestPath возвращает URI-кодированный путь запроса.
// УЛУЧШЕННАЯ ВЕРСИЯ.
func (s *StaticAuthenticator) getRequestPath(bucket, key string) string {
	path := "/"
	if bucket != "" {
		path = "/" + bucket + "/"
		if key != "" {
			path += key
		}
	}
	// Используем стандартную библиотеку для корректного URI-кодирования пути.
	// Этот метод кодирует спецсимволы в сегментах пути, но оставляет '/' как есть,
	// что в точности соответствует требованиям S3.
	return (&url.URL{Path: path}).RequestURI()
}
