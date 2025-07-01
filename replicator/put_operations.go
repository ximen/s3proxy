package replicator

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"s3proxy/apigw"
	"s3proxy/backend"
	"s3proxy/logger"
	"s3proxy/routing"

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

// buildPutObjectInput инкапсулирует сложную логику преобразования
// входящего HTTP-запроса в нативный s3.PutObjectInput для AWS SDK.
// Эта функция ничего не отправляет, только собирает структуру.
func (r *Replicator) buildPutObjectInput(req *apigw.S3Request, body io.Reader, b *backend.Backend) *s3.PutObjectInput {
	// 1. Создаем "скелет" запроса с обязательными полями
	putInput := &s3.PutObjectInput{
		Bucket: aws.String(b.Config.Bucket),
		Key:    aws.String(req.Key),
		Body:   body,
	}

	// 2. Явно указываем ContentLength.
	if req.ContentLength > 0 {
		putInput.ContentLength = aws.Int64(req.ContentLength)
	}

	// 3. Перебираем заголовки и "перекладываем" их в поля PutObjectInput.
	metadata := make(map[string]string)
	isStreamingClient := b.StreamingPutClient != nil

	for key, values := range req.Headers {
		if len(values) == 0 {
			continue
		}
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
		// Если клиент прислал SHA256 хэш, доверяем ему. Это экономит чтение потока.
		// НЕ используем для streaming-клиента, так как он вычисляет его сам.
		case "X-Amz-Content-Sha256":
			if !isStreamingClient {
				putInput.ChecksumSHA256 = aws.String(value)
			}
		// Игнорируем заголовки, относящиеся к аутентификации и транспорту
		case "Authorization", "X-Amz-Date", "Host", "Content-Length", "Expect":
			continue
		default:
			if strings.HasPrefix(canonicalKey, "X-Amz-Meta-") {
				metaKey := strings.TrimPrefix(canonicalKey, "X-Amz-Meta-")
				metadata[strings.ToLower(metaKey)] = value
			}
		}
	}

	if len(metadata) > 0 {
		putInput.Metadata = metadata
	}

	return putInput
}

// performPutToBackend выполняет ОДНУ попытку отправки объекта на бэкенд.
// Логика повторных попыток (retries) должна быть реализована на уровне выше.
func (r *Replicator) performPutToBackend(ctx context.Context, b *backend.Backend, req *apigw.S3Request, body io.Reader) *backend.BackendResult {
	startTime := time.Now()

	// Устанавливаем таймаут на операцию
	ctx, cancel := context.WithTimeout(ctx, r.config.OperationTimeout)
	defer cancel()

	// Оборачиваем тело для подсчета байт
	countingReader := NewCountingReader(body)

	// 1. Собираем запрос с помощью новой функции-хелпера
	putInput := r.buildPutObjectInput(req, countingReader, b)

	// 2. Выбираем правильный S3 клиент (обычный или для стриминга)
	clientToUse := b.S3Client
	if b.StreamingPutClient != nil {
		logger.Debug("Using dedicated streaming client for PutObject on backend %s", b.ID)
		clientToUse = b.StreamingPutClient
	}

	logger.Debug(
		"performPutToBackend: sending PUT to backend %s (Bucket: %s, Key: %s) with ContentLength=%d",
		b.ID, *putInput.Bucket, *putInput.Key, req.ContentLength,
	)

	// 3. Выполняем ОДНУ попытку запроса
	response, err := clientToUse.PutObject(ctx, putInput)

	duration := time.Since(startTime)
	bytesWritten := countingReader.Count()

	// 4. Логируем результат и возвращаем его
	if err != nil {
		logger.Error("performPutToBackend: failed on backend %s: %v", b.ID, err)
	} else {
		logger.Debug("performPutToBackend: success on backend %s, bytes=%d, duration=%v", b.ID, bytesWritten, duration)
	}

	return &backend.BackendResult{
		BackendID:    b.ID,
		Method:       "PUT",
		Response:     response,
		Err:          err,
		Duration:     duration,
		BytesWritten: bytesWritten,
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
