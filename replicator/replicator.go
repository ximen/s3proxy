package replicator

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	//"github.com/aws/aws-sdk-go-v2/service/s3"
	"s3proxy/apigw"
	"s3proxy/logger"

	//"s3proxy/monitoring"
	"s3proxy/backend"
	"s3proxy/routing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	//"github.com/aws/aws-sdk-go-v2/service/s3"
	//"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Replicator реализует интерфейс ReplicationExecutor
type Replicator struct {
	backendProvider *backend.Manager
	//metrics         *monitoring.Metrics
	multipartStore *MultipartStore
	readerCloner   ReaderCloner
	config         *Config

	// Семафор для ограничения количества одновременных операций
	semaphore chan struct{}
}

// NewReplicator создает новый экземпляр репликатора
func NewReplicator(provider *backend.Manager, config *Config) *Replicator {
	if config == nil {
		config = DefaultConfig()
	}

	replicator := &Replicator{
		backendProvider: provider,
		//metrics:         metrics,
		multipartStore: NewMultipartStore(config),
		readerCloner:   &PipeReaderCloner{},
		config:         config,
		semaphore:      make(chan struct{}, config.MaxConcurrentOperations),
	}

	logger.Info("Replicator initialized with config: max_concurrent=%d, timeout=%v",
		config.MaxConcurrentOperations, config.OperationTimeout)

	return replicator
}

// Stop останавливает репликатор
func (r *Replicator) Stop() {
	r.multipartStore.Stop()
	logger.Info("Replicator stopped")
}

// PutObject выполняет репликацию PUT операции
func (r *Replicator) PutObject(ctx context.Context, req *apigw.S3Request, policy routing.WriteOperationPolicy) *apigw.S3Response {
	opCtx := newOperationContext(ctx, "PUT_OBJECT", req.Bucket, req.Key)

	logger.Debug("PutObject: bucket=%s, key=%s, policy=%+v", req.Bucket, req.Key, policy)

	// Получаем живые бэкенды
	liveBackends := r.backendProvider.GetLiveBackends()
	if len(liveBackends) == 0 {
		logger.Warn("PutObject: no live backends available")
		return r.createErrorResponse(http.StatusServiceUnavailable, "ServiceUnavailable", "No available backends")
	}

	logger.Debug("PutObject: using %d backends", len(liveBackends))

	// Синхронное выполнение для ack=one и ack=all
	return r.performPutSync(opCtx, req, liveBackends, policy)
}

// DeleteObject выполняет репликацию DELETE операции
func (r *Replicator) DeleteObject(ctx context.Context, req *apigw.S3Request, policy routing.WriteOperationPolicy) *apigw.S3Response {
	opCtx := newOperationContext(ctx, "DELETE_OBJECT", req.Bucket, req.Key)

	logger.Debug("DeleteObject: bucket=%s, key=%s, policy=%+v", req.Bucket, req.Key, policy)

	// Получаем живые бэкенды
	liveBackends := r.backendProvider.GetLiveBackends()
	if len(liveBackends) == 0 {
		logger.Warn("DeleteObject: no live backends available")
		return r.createErrorResponse(http.StatusServiceUnavailable, "ServiceUnavailable", "No available backends")
	}

	// Синхронное выполнение для ack=one и ack=all
	return r.performDeleteSync(opCtx, req, liveBackends, policy)
}

// CreateMultipartUpload инициирует multipart upload на всех бэкендах
func (r *Replicator) CreateMultipartUpload(ctx context.Context, req *apigw.S3Request, policy routing.WriteOperationPolicy) *apigw.S3Response {
	opCtx := newOperationContext(ctx, "CREATE_MULTIPART_UPLOAD", req.Bucket, req.Key)

	logger.Debug("CreateMultipartUpload: bucket=%s, key=%s", req.Bucket, req.Key)

	// Получаем живые бэкенды
	liveBackends := r.backendProvider.GetLiveBackends()
	if len(liveBackends) == 0 {
		return r.createErrorResponse(http.StatusServiceUnavailable, "ServiceUnavailable", "No available backends")
	}

	// Создаем multipart upload на всех бэкендах
	backendUploads := make(map[string]string)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var firstError error

	for _, backend_iter := range liveBackends {
		wg.Add(1)
		go func(b *backend.Backend) {
			defer wg.Done()

			result := r.performCreateMultipartUpload(opCtx.ctx, b, req)

			mu.Lock()
			defer mu.Unlock()

			if result.Err != nil {
				if firstError == nil {
					firstError = result.Err
				}
				r.backendProvider.ReportFailure(result)
			} else {
				// Извлекаем UploadId из ответа
				if createResp, ok := result.Response.(*s3.CreateMultipartUploadOutput); ok {
					backendUploads[b.ID] = *createResp.UploadId
					r.backendProvider.ReportSuccess(result)
				}
			}

			// Обновляем метрики
			//r.updateMetrics(b.ID, "create_multipart_upload", result)
		}(backend_iter)
	}

	wg.Wait()

	// Проверяем результаты
	if len(backendUploads) == 0 {
		logger.Error("CreateMultipartUpload: failed on all backends")
		return r.createErrorResponse(http.StatusInternalServerError, "InternalError", "Failed to create multipart upload on any backend")
	}

	// Создаем маппинг
	proxyUploadID, err := r.multipartStore.CreateMapping(req.Bucket, req.Key, backendUploads)
	if err != nil {
		logger.Error("CreateMultipartUpload: failed to create mapping: %v", err)
		return r.createErrorResponse(http.StatusInternalServerError, "InternalError", "Failed to create upload mapping")
	}

	logger.Info("CreateMultipartUpload: created proxy upload ID %s for %d backends", proxyUploadID, len(backendUploads))

	// Возвращаем ответ с proxy upload ID
	return r.createMultipartUploadResponse(req, proxyUploadID)
}

// UploadPart загружает часть multipart upload
func (r *Replicator) UploadPart(ctx context.Context, req *apigw.S3Request, policy routing.WriteOperationPolicy) *apigw.S3Response {
	opCtx := newOperationContext(ctx, "UPLOAD_PART", req.Bucket, req.Key)

	// Извлекаем параметры из query
	uploadID := req.Query["uploadId"][0]
	partNumber := req.Query["partNumber"][0]

	logger.Debug("UploadPart: bucket=%s, key=%s, uploadId=%s, partNumber=%s", req.Bucket, req.Key, uploadID, partNumber)

	// Получаем маппинг
	mapping, exists := r.multipartStore.GetMapping(uploadID)
	if !exists {
		return r.createErrorResponse(http.StatusNotFound, "NoSuchUpload", "The specified multipart upload does not exist")
	}

	// Получаем живые бэкенды
	liveBackends := r.backendProvider.GetLiveBackends()
	if len(liveBackends) == 0 {
		return r.createErrorResponse(http.StatusServiceUnavailable, "ServiceUnavailable", "No available backends")
	}

	// Фильтруем бэкенды, которые участвуют в этом upload
	var targetBackends []*backend.Backend
	for _, b := range liveBackends {
		if _, exists := mapping.BackendUploads[b.ID]; exists {
			targetBackends = append(targetBackends, b)
		}
	}

	if len(targetBackends) == 0 {
		return r.createErrorResponse(http.StatusServiceUnavailable, "ServiceUnavailable", "No available backends for this upload")
	}

	// Синхронное выполнение
	return r.performUploadPartSync(opCtx, req, targetBackends, mapping, partNumber, policy)
}

// CompleteMultipartUpload завершает multipart upload
func (r *Replicator) CompleteMultipartUpload(ctx context.Context, req *apigw.S3Request, policy routing.WriteOperationPolicy) *apigw.S3Response {
	opCtx := newOperationContext(ctx, "COMPLETE_MULTIPART_UPLOAD", req.Bucket, req.Key)

	// Извлекаем uploadId из query
	uploadID := req.Query["uploadId"][0]

	logger.Debug("CompleteMultipartUpload: bucket=%s, key=%s, uploadId=%s", req.Bucket, req.Key, uploadID)

	// Получаем маппинг
	mapping, exists := r.multipartStore.GetMapping(uploadID)
	if !exists {
		return r.createErrorResponse(http.StatusNotFound, "NoSuchUpload", "The specified multipart upload does not exist")
	}

	// Получаем живые бэкенды
	liveBackends := r.backendProvider.GetLiveBackends()
	targetBackends := r.filterBackendsForUpload(liveBackends, mapping)

	if len(targetBackends) == 0 {
		return r.createErrorResponse(http.StatusServiceUnavailable, "ServiceUnavailable", "No available backends for this upload")
	}

	// Complete всегда выполняется синхронно (критическая операция)
	response := r.performCompleteMultipartUploadSync(opCtx, req, targetBackends, mapping, policy)

	// Удаляем маппинг после завершения (успешного или нет)
	r.multipartStore.DeleteMapping(uploadID)

	return response
}

// AbortMultipartUpload отменяет multipart upload
func (r *Replicator) AbortMultipartUpload(ctx context.Context, req *apigw.S3Request, policy routing.WriteOperationPolicy) *apigw.S3Response {
	opCtx := newOperationContext(ctx, "ABORT_MULTIPART_UPLOAD", req.Bucket, req.Key)

	// Извлекаем uploadId из query
	uploadID := req.Query["uploadId"][0]

	logger.Debug("AbortMultipartUpload: bucket=%s, key=%s, uploadId=%s", req.Bucket, req.Key, uploadID)

	// Получаем маппинг
	mapping, exists := r.multipartStore.GetMapping(uploadID)
	if !exists {
		// Если маппинг не найден, возвращаем успех (идемпотентная операция)
		return &apigw.S3Response{StatusCode: http.StatusNoContent}
	}

	// Получаем живые бэкенды
	liveBackends := r.backendProvider.GetLiveBackends()
	targetBackends := r.filterBackendsForUpload(liveBackends, mapping)

	// Выполняем abort на всех бэкендах (даже если они недоступны, пытаемся)
	r.performAbortMultipartUpload(opCtx, req, targetBackends, mapping)

	// Удаляем маппинг
	r.multipartStore.DeleteMapping(uploadID)

	return &apigw.S3Response{StatusCode: http.StatusNoContent}
}

// filterBackendsForUpload фильтрует бэкенды для конкретного upload
func (r *Replicator) filterBackendsForUpload(liveBackends []*backend.Backend, mapping *multipartUploadMapping) []*backend.Backend {
	var targetBackends []*backend.Backend
	for _, b := range liveBackends {
		if _, exists := mapping.BackendUploads[b.ID]; exists {
			targetBackends = append(targetBackends, b)
		}
	}
	return targetBackends
}

// createErrorResponse создает ответ об ошибке
func (r *Replicator) createErrorResponse(statusCode int, errorCode, message string) *apigw.S3Response {
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<Error>
    <Code>%s</Code>
    <Message>%s</Message>
</Error>`, errorCode, message)

	headers := make(http.Header)
	headers.Set("Content-Type", "application/xml")
	headers.Set("Content-Length", fmt.Sprintf("%d", len(body)))

	return &apigw.S3Response{
		StatusCode: statusCode,
		Headers:    headers,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// createSuccessResponse создает успешный ответ
func (r *Replicator) createSuccessResponse(req *apigw.S3Request, message string) *apigw.S3Response {
	headers := make(http.Header)
	headers.Set("ETag", `"replicator-success"`)

	return &apigw.S3Response{
		StatusCode: http.StatusOK,
		Headers:    headers,
		Body:       io.NopCloser(strings.NewReader(message)),
	}
}

// createMultipartUploadResponse создает ответ для CreateMultipartUpload
func (r *Replicator) createMultipartUploadResponse(req *apigw.S3Request, uploadID string) *apigw.S3Response {
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<InitiateMultipartUploadResult>
    <Bucket>%s</Bucket>
    <Key>%s</Key>
    <UploadId>%s</UploadId>
</InitiateMultipartUploadResult>`, req.Bucket, req.Key, uploadID)

	headers := make(http.Header)
	headers.Set("Content-Type", "application/xml")
	headers.Set("Content-Length", fmt.Sprintf("%d", len(body)))

	return &apigw.S3Response{
		StatusCode: http.StatusOK,
		Headers:    headers,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// // updateMetrics обновляет метрики для операции
// func (r *Replicator) updateMetrics(backendID, operation string, result *backendResult) {
// 	if r.metrics == nil {
// 		return
// 	}

// 	// Обновляем латентность
// 	r.metrics.BackendLatency.WithLabelValues(backendID, operation).Observe(result.duration.Seconds())

// 	// Обновляем счетчик запросов
// 	status := "success"
// 	if result.err != nil {
// 		status = "error"
// 	}
// 	r.metrics.BackendRequestsTotal.WithLabelValues(backendID, operation, status).Inc()

// 	// Обновляем счетчик байт (если есть данные)
// 	if result.bytesWritten > 0 {
// 		// Предполагаем, что в будущем добавим эту метрику
// 		logger.Debug("Bytes written to backend %s: %d", backendID, result.bytesWritten)
// 	}
// }
