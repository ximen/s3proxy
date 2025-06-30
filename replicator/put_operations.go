package replicator

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"s3proxy/apigw"
	"s3proxy/logger"
	"s3proxy/routing"
	"s3proxy/backend"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// performPutSync выполняет PUT операцию синхронно (для ack=one и ack=all)
func (r *Replicator) performPutSync(opCtx *operationContext, req *apigw.S3Request, backends []*backend.Backend, policy routing.WriteOperationPolicy) *apigw.S3Response {
	logger.Debug("performPutSync: starting sync PUT for %d backends with policy %s", len(backends), policy.AckLevel)

	// Клонируем reader для каждого бэкенда
	readers, err := r.readerCloner.Clone(req.Body, len(backends))
	if err != nil {
		logger.Error("performPutSync: failed to clone reader: %v", err)
		return r.createErrorResponse(http.StatusInternalServerError, "InternalError", "Failed to prepare request body")
	}

	// Создаем канал для результатов
	resultsChan := make(chan *backend.BackendResult, len(backends))

	// Запускаем горутины для каждого бэкенда
	var wg sync.WaitGroup
	for i, backend_iter := range backends {
		wg.Add(1)
		go func(b *backend.Backend, reader io.Reader) {
			defer wg.Done()

			// Ограничиваем количество одновременных операций
			r.semaphore <- struct{}{}
			defer func() { <-r.semaphore }()

			// Для политики 'one' операции должны продолжаться в фоне,
			// даже если исходный запрос завершился. Используем фоновый контекст.
			// Для 'all' мы хотим отменить операции, если клиент отключается,
			// поэтому используем контекст исходного запроса.
			backendCtx := opCtx.ctx
			if policy.AckLevel == "one" {
				backendCtx = context.Background()
			}

			result := r.performPutToBackend(backendCtx, b, req, reader)
			r.reportBackendResult(result)
			//r.updateMetrics(b.ID, "put_object", result)

			resultsChan <- result
		}(backend_iter, readers[i])
	}

	// Горутина для закрытия канала после завершения всех операций
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Агрегируем результаты в соответствии с политикой
	return r.aggregatePutResults(resultsChan, policy, len(backends))
}

func (r *Replicator) performPutToBackend(ctx context.Context, b *backend.Backend, req *apigw.S3Request, body io.Reader) *backend.BackendResult {
	startTime := time.Now()

	ctx, cancel := context.WithTimeout(ctx, r.config.OperationTimeout)
	defer cancel()

	countingReader := NewCountingReader(body)

	// --- ВЫБОР КЛИЕНТА ---
	// Делаем это ДО того, как начнем модифицировать putInput
	isStreamingClient := b.StreamingPutClient != nil
	clientToUse := b.S3Client
	if isStreamingClient {
		logger.Debug("Using dedicated streaming client for PutObject on backend %s", b.ID)
		clientToUse = b.StreamingPutClient
	}

	// 1. Создаем "скелет" запроса с обязательными полями
	putInput := &s3.PutObjectInput{
		Bucket: aws.String(b.Config.Bucket),
		Key:    aws.String(req.Key),
		Body:   countingReader,
	}

	// 2. Явно указываем ContentLength. Это критически важно для корректной подписи
	// и обработки запроса на стороне S3-совместимого хранилища.
	if req.ContentLength > 0 {
		putInput.ContentLength = aws.Int64(req.ContentLength)
	}

	// 3. Перебираем заголовки входящего запроса и "перекладываем" их в поля PutObjectInput.
	// Это - необходимая логика прокси, которую нельзя автоматизировать через SDK.
	metadata := make(map[string]string)
	for key, values := range req.Headers {
		if len(values) == 0 {
			continue
		}
		// Используем каноническое имя заголовка (например, "Content-Type")
		canonicalKey := http.CanonicalHeaderKey(key)
		value := values[0]

		switch canonicalKey {
		case "Content-Type":
			putInput.ContentType = aws.String(value)
		case "Content-Encoding":
			putInput.ContentEncoding = aws.String(value)
		case "Content-Md5":
			putInput.ContentMD5 = aws.String(value)
		case "Cache-Control":
			putInput.CacheControl = aws.String(value)
		case "X-Amz-Storage-Class":
			putInput.StorageClass = types.StorageClass(value)
		// Если клиент прислал SHA256 хэш, мы ему доверяем и используем его!
		// Это избавляет SDK от необходимости читать поток для вычисления хэша.
		case "X-Amz-Content-Sha256":
			if !isStreamingClient {
				putInput.ChecksumSHA256 = aws.String(value)
			}
		// Игнорируем только то, что относится к подписи и транспорту
		case "Authorization", "X-Amz-Date", "Host", "Content-Length":
			continue
		default:
			// Все, что начинается с "X-Amz-Meta-", складываем в метаданные.
			if strings.HasPrefix(canonicalKey, "X-Amz-Meta-") {
				metaKey := strings.TrimPrefix(canonicalKey, "X-Amz-Meta-")
				metadata[strings.ToLower(metaKey)] = value // Ключи метаданных принято хранить в нижнем регистре
			}
		}
	}

	if len(metadata) > 0 {
		putInput.Metadata = metadata
	}

	logger.Debug(
		"performPutToBackend: sending PUT to backend %s (Bucket: %s, Key: %s) with ContentLength=%d, Metadata: %v",
		b.ID, *putInput.Bucket, *putInput.Key, req.ContentLength, putInput.Metadata,
	)

	// Выполняем запрос с повторами, используя ВЫБРАННЫЙ клиент
	var response *s3.PutObjectOutput
	var err error

	for attempt := 0; attempt <= r.config.RetryAttempts; attempt++ {
		if attempt > 0 {
			// Перед повторной попыткой необходимо "перемотать" reader в начало,
			// если он поддерживает io.Seeker.
			if seeker, ok := putInput.Body.(io.Seeker); ok {
				_, _ = seeker.Seek(0, io.SeekStart)
			}
			logger.Debug("performPutToBackend: retry attempt %d for backend %s", attempt, b.ID)
			time.Sleep(r.config.RetryDelay)
		}

		response, err = clientToUse.PutObject(ctx, putInput)
		if err == nil {
			break
		}
		logger.Debug("performPutToBackend: attempt %d failed for backend %s: %v", attempt+1, b.ID, err)

		// Оптимизация: нет смысла повторять запрос, если это ошибка клиента (4xx)
		var responseError interface {
			HTTPStatusCode() int
		}
		if ok := errors.As(err, &responseError); ok {
			if responseError.HTTPStatusCode() >= 400 && responseError.HTTPStatusCode() < 500 {
				logger.Error("performPutToBackend: received client error %d, stopping retries.", responseError.HTTPStatusCode())
				break
			}
		}
	}

	duration := time.Since(startTime)
	bytesWritten := countingReader.Count()

	if err != nil {
		logger.Error("performPutToBackend: failed on backend %s after retries: %v", b.ID, err)
	} else {
		logger.Debug("performPutToBackend: success on backend %s, bytes=%d, duration=%v", b.ID, bytesWritten, duration)
	}

	return & backend.BackendResult{
		BackendID:    b.ID,
		Method:       "PUT",
		Response:     response,
		Err:          err,
		Duration:     duration,
		BytesWritten: bytesWritten,
		BytesRead: 	  0,
	}
}

// aggregatePutResults агрегирует результаты PUT операций
func (r *Replicator) aggregatePutResults(resultsChan <-chan *backend.BackendResult, policy routing.WriteOperationPolicy, totalBackends int) *apigw.S3Response {
	successCount := 0
	errorCount := 0
	var firstSuccessResult *backend.BackendResult
	var lastError error

	logger.Debug("aggregatePutResults: waiting for results with policy %s", policy.AckLevel)

	for result := range resultsChan {
		if result.Err == nil {
			successCount++
			if firstSuccessResult == nil {
				firstSuccessResult = result
			}

			logger.Debug("aggregatePutResults: success from backend %s (%d/%d)", result.BackendID, successCount, totalBackends)

			// Для ack=one возвращаем успех сразу после первого успешного ответа
			if policy.AckLevel == "one" {
				logger.Debug("aggregatePutResults: returning success for ack=one policy")
				return r.convertPutResultToResponse(firstSuccessResult)
			}
		} else {
			errorCount++
			lastError = result.Err
			logger.Debug("aggregatePutResults: error from backend %s: %v (%d/%d)", result.BackendID, result.Err, errorCount, totalBackends)
		}
	}

	logger.Debug("aggregatePutResults: final results - success: %d, errors: %d", successCount, errorCount)

	// Логика для ack=all
	if policy.AckLevel == "all" {
		if successCount == totalBackends {
			logger.Debug("aggregatePutResults: all backends succeeded for ack=all policy")
			return r.convertPutResultToResponse(firstSuccessResult)
		} else {
			logger.Error("aggregatePutResults: not all backends succeeded for ack=all policy (%d/%d)", successCount, totalBackends)
			return r.createErrorResponse(http.StatusInternalServerError, "InternalError", "Failed to replicate object to all backends")
		}
	}

	// Если мы дошли сюда при ack=one, значит ни один бэкенд не ответил успехом
	if policy.AckLevel == "one" && successCount == 0 {
		logger.Error("aggregatePutResults: no backends succeeded for ack=one policy")
		if lastError != nil {
			return r.createErrorResponse(http.StatusServiceUnavailable, "ServiceUnavailable", lastError.Error())
		}
		return r.createErrorResponse(http.StatusServiceUnavailable, "ServiceUnavailable", "Failed to write to any backend")
	}

	// Не должны сюда попасть
	logger.Error("aggregatePutResults: unexpected code path reached")
	return r.createErrorResponse(http.StatusInternalServerError, "InternalError", "Unexpected error in result aggregation")
}

// convertPutResultToResponse преобразует результат PUT в S3Response
func (r *Replicator) convertPutResultToResponse(result *backend.BackendResult) *apigw.S3Response {
	headers := make(http.Header)

	if putOutput, ok := result.Response.(*s3.PutObjectOutput); ok {
		if putOutput.ETag != nil {
			headers.Set("ETag", *putOutput.ETag)
		}
		if putOutput.VersionId != nil {
			headers.Set("x-amz-version-id", *putOutput.VersionId)
		}
	}

	return &apigw.S3Response{
		StatusCode: http.StatusOK,
		Headers:    headers,
	}
}

// reportBackendResult сообщает результат операции в Backend Manager
func (r *Replicator) reportBackendResult(result *backend.BackendResult) {
	if result.Err != nil {
		r.backendProvider.ReportFailure(result)
	} else {
		r.backendProvider.ReportSuccess(result)
	}
}
