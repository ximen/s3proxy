package routing

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"s3proxy/apigw"
	"s3proxy/auth"
	"s3proxy/logger"
)

// Engine - это реализация Policy & Routing Engine
type Engine struct {
	// Зависимости, внедряемые при создании
	auth       auth.Authenticator  // Модуль аутентификации
	replicator ReplicationExecutor // Модуль для записи
	fetcher    FetchingExecutor    // Модуль для чтения

	// Конфигурация политик, загружаемая при старте
	putPolicy    WriteOperationPolicy
	deletePolicy WriteOperationPolicy
	getPolicy    ReadOperationPolicy
}

// NewEngine создает новый экземпляр Engine
func NewEngine(
	authenticator auth.Authenticator,
	replicator ReplicationExecutor,
	fetcher FetchingExecutor,
	config *Config,
) *Engine {
	if config == nil {
		config = DefaultConfig()
	}

	return &Engine{
		auth:         authenticator,
		replicator:   replicator,
		fetcher:      fetcher,
		putPolicy:    config.Policies.Put,
		deletePolicy: config.Policies.Delete,
		getPolicy:    config.Policies.Get,
	}
}

// Handle - реализация интерфейса RequestHandler. Это точка входа в модуль
func (e *Engine) Handle(req *apigw.S3Request) *apigw.S3Response {
	logger.Debug("Policy & Routing Engine: handling request - Operation: %s, Bucket: %s, Key: %s",
		req.Operation, req.Bucket, req.Key)

	// Шаг 1: Аутентификация
	logger.Debug("Starting authentication")
	identity, err := e.auth.Authenticate(req)
	if err != nil {
		logger.Debug("Authentication failed: %v", err)
		// Преобразовать ошибку аутентификации в стандартный S3Response
		return e.createAuthErrorResponse(err)
	}

	logger.Debug("Policy & Routing Engine received authenticated request:")
	logger.Debug("  User: %s (%s)", identity.DisplayName, identity.AccessKey)
	logger.Debug("  Operation: %s", req.Operation)
	logger.Debug("  Bucket: %s", req.Bucket)
	logger.Debug("  Key: %s", req.Key)

	// Шаг 2: Авторизация (заглушка для будущего)
	// TODO: Реализовать модуль авторизации
	// isAuthorized := e.authorizer.Authorize(identity, req)
	// if !isAuthorized {
	//     return e.createAuthorizationErrorResponse()
	// }
	logger.Debug("Authorization check passed (not implemented yet)")

	// Шаг 3: Маршрутизация на основе типа операции
	logger.Debug("Routing request based on operation: %s", req.Operation)

	switch req.Operation {
	// Операции записи - направляем в Replication Module
	case apigw.PutObject:
		logger.Debug("Routing to replicator.PutObject with policy: %+v", e.putPolicy)
		return e.replicator.PutObject(req.Context, req, e.putPolicy)

	case apigw.DeleteObject:
		logger.Debug("Routing to replicator.DeleteObject with policy: %+v", e.deletePolicy)
		return e.replicator.DeleteObject(req.Context, req, e.deletePolicy)

	case apigw.CreateMultipartUpload:
		logger.Debug("Routing to replicator.CreateMultipartUpload with policy: %+v", e.putPolicy)
		return e.replicator.CreateMultipartUpload(req.Context, req, e.putPolicy)

	case apigw.UploadPart:
		logger.Debug("Routing to replicator.UploadPart with policy: %+v", e.putPolicy)
		return e.replicator.UploadPart(req.Context, req, e.putPolicy)

	case apigw.CompleteMultipartUpload:
		logger.Debug("Routing to replicator.CompleteMultipartUpload with policy: %+v", e.putPolicy)
		return e.replicator.CompleteMultipartUpload(req.Context, req, e.putPolicy)

	case apigw.AbortMultipartUpload:
		logger.Debug("Routing to replicator.AbortMultipartUpload with policy: %+v", e.deletePolicy)
		return e.replicator.AbortMultipartUpload(req.Context, req, e.deletePolicy)

	// Операции чтения - направляем в Fetching Module
	case apigw.GetObject:
		logger.Debug("Routing to fetcher.GetObject with policy: %+v", e.getPolicy)
		return e.fetcher.GetObject(req.Context, req, e.getPolicy)

	case apigw.HeadObject:
		logger.Debug("Routing to fetcher.HeadObject with policy: %+v", e.getPolicy)
		// Для HEAD обычно используется та же политика, что и для GET
		return e.fetcher.HeadObject(req.Context, req, e.getPolicy)

	case apigw.HeadBucket:
		logger.Debug("Routing to fetcher.HeadBucket")
		// HeadBucket - простая операция проверки существования бакета
		return e.fetcher.HeadBucket(req.Context, req)

	case apigw.ListObjectsV2:
		logger.Debug("Routing to fetcher.ListObjects")
		// У листинга своя логика, не требующая политики
		return e.fetcher.ListObjects(req.Context, req)

	case apigw.ListBuckets:
		logger.Debug("Routing to fetcher.ListBuckets")
		return e.fetcher.ListBuckets(req.Context, req)

	case apigw.ListMultipartUploads:
		logger.Debug("Routing to fetcher.ListMultipartUploads")
		return e.fetcher.ListMultipartUploads(req.Context, req)

	default:
		logger.Warn("Unsupported operation: %s", req.Operation)
		// Вернуть ошибку для неподдерживаемых операций
		return e.createOperationNotImplementedResponse(req.Operation)
	}
}

// createAuthErrorResponse преобразует ошибку аутентификации в стандартный S3Response
func (e *Engine) createAuthErrorResponse(err error) *apigw.S3Response {
	var code string
	var message string
	var statusCode int

	logger.Debug("Creating auth error response for error: %v", err)

	switch {
	case errors.Is(err, auth.ErrMissingAuthHeader):
		code = "MissingSecurityHeader"
		message = "Your request was missing a required header."
		statusCode = http.StatusBadRequest // 400 - клиент не предоставил обязательный заголовок
	case errors.Is(err, auth.ErrInvalidAccessKeyID):
		code = "InvalidAccessKeyId"
		message = "The Access Key Id you provided does not exist in our records."
		statusCode = http.StatusForbidden // 403 - неверный ключ доступа
	case errors.Is(err, auth.ErrSignatureMismatch):
		code = "SignatureDoesNotMatch"
		message = "The request signature we calculated does not match the signature you provided."
		statusCode = http.StatusForbidden // 403 - неверная подпись
	case errors.Is(err, auth.ErrRequestExpired):
		code = "RequestTimeTooSkewed"
		message = "The difference between the request time and the current time is too large."
		statusCode = http.StatusForbidden // 403 - запрос устарел
	default:
		// Для неизвестных ошибок аутентификации используем 403, а не 500
		// 500 должен использоваться только для внутренних ошибок сервера
		code = "AccessDenied"
		message = "Access Denied"
		statusCode = http.StatusForbidden // 403 - общая ошибка доступа
	}

	// Создать S3 XML тело ошибки
	errorBody := e.formatS3ErrorXML(code, message)

	headers := make(http.Header)
	headers.Set("Content-Type", "application/xml")
	headers.Set("Content-Length", fmt.Sprintf("%d", len(errorBody)))

	return &apigw.S3Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(strings.NewReader(errorBody)),
		Headers:    headers,
		// Не устанавливаем Error, так как у нас уже есть правильно сформированный ответ
	}
}

// createOperationNotImplementedResponse создает ответ для неподдерживаемых операций
func (e *Engine) createOperationNotImplementedResponse(operation apigw.S3Operation) *apigw.S3Response {
	code := "NotImplemented"
	message := fmt.Sprintf("The operation %s is not implemented", operation)
	statusCode := http.StatusNotImplemented

	errorBody := e.formatS3ErrorXML(code, message)

	headers := make(http.Header)
	headers.Set("Content-Type", "application/xml")
	headers.Set("Content-Length", fmt.Sprintf("%d", len(errorBody)))

	return &apigw.S3Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(strings.NewReader(errorBody)),
		Headers:    headers,
		// Не устанавливаем Error, так как у нас уже есть правильно сформированный ответ
	}
}

// formatS3ErrorXML форматирует ошибку в стандартный S3 XML формат
func (e *Engine) formatS3ErrorXML(code, message string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<Error>
    <Code>%s</Code>
    <Message>%s</Message>
    <RequestId>%s</RequestId>
    <HostId>%s</HostId>
</Error>`, code, message, "policy-routing-engine", "s3proxy")
}
