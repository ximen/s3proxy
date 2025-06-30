package replicator

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"s3proxy/apigw"
	"s3proxy/logger"
	"s3proxy/routing"
	"s3proxy/backend"
)

// performDeleteSync выполняет DELETE операцию синхронно (для ack=one и ack=all)
func (r *Replicator) performDeleteSync(opCtx *operationContext, req *apigw.S3Request, backends []*backend.Backend, policy routing.WriteOperationPolicy) *apigw.S3Response {
	logger.Debug("performDeleteSync: starting sync DELETE for %d backends with policy %s", len(backends), policy.AckLevel)
	
	// Создаем канал для результатов
	resultsChan := make(chan *backend.BackendResult, len(backends))
	
	// Запускаем горутины для каждого бэкенда
	var wg sync.WaitGroup
	for _, backend_iter := range backends {
		wg.Add(1)
		go func(b *backend.Backend) {
			defer wg.Done()
			
			// Ограничиваем количество одновременных операций
			r.semaphore <- struct{}{}
			defer func() { <-r.semaphore }()
			
			// Для политики 'one' операции должны продолжаться в фоне,
			// даже если исходный запрос завершился. Используем фоновый контекст.
			backendCtx := opCtx.ctx
			if policy.AckLevel == "one" {
				backendCtx = context.Background()
			}

			result := r.performDeleteFromBackend(backendCtx, b, req)
			r.reportBackendResult(result)
			//r.updateMetrics(b.ID, "delete_object", result)
			
			resultsChan <- result
		}(backend_iter)
	}
	
	// Горутина для закрытия канала после завершения всех операций
	go func() {
		wg.Wait()
		close(resultsChan)
	}()
	
	// Агрегируем результаты в соответствии с политикой
	return r.aggregateDeleteResults(resultsChan, policy, len(backends))
}

// performDeleteFromBackend выполняет DELETE операцию на одном бэкенде
func (r *Replicator) performDeleteFromBackend(ctx context.Context, b *backend.Backend, req *apigw.S3Request) *backend.BackendResult {
	startTime := time.Now()
	
	// Создаем контекст с таймаутом
	ctx, cancel := context.WithTimeout(ctx, r.config.OperationTimeout)
	defer cancel()
	
	// Создаем DeleteObjectInput
	deleteInput := &s3.DeleteObjectInput{
		Bucket: aws.String(b.Config.Bucket),
		Key:    aws.String(req.Key),
	}
	
	logger.Debug("performDeleteFromBackend: sending DELETE to backend %s", b.ID)
	
	// Выполняем запрос с повторами
	var response *s3.DeleteObjectOutput
	var err error
	
	for attempt := 0; attempt <= r.config.RetryAttempts; attempt++ {
		if attempt > 0 {
			logger.Debug("performDeleteFromBackend: retry attempt %d for backend %s", attempt, b.ID)
			time.Sleep(r.config.RetryDelay)
		}
		
		response, err = b.S3Client.DeleteObject(ctx, deleteInput)
		if err == nil {
			break
		}
		
		logger.Debug("performDeleteFromBackend: attempt %d failed for backend %s: %v", attempt+1, b.ID, err)
	}
	
	duration := time.Since(startTime)
	
	if err != nil {
		logger.Error("performDeleteFromBackend: failed on backend %s after %d attempts: %v", b.ID, r.config.RetryAttempts+1, err)
	} else {
		logger.Debug("performDeleteFromBackend: success on backend %s, duration=%v", b.ID, duration)
	}
	
	return &backend.BackendResult{
		BackendID: b.ID,
		Response:  response,
		Err:       err,
		Duration:  duration,
		Method:    "DELETE",
		StatusCode: http.StatusNoContent,
	}
}

// aggregateDeleteResults агрегирует результаты DELETE операций
func (r *Replicator) aggregateDeleteResults(resultsChan <-chan *backend.BackendResult, policy routing.WriteOperationPolicy, totalBackends int) *apigw.S3Response {
	successCount := 0
	errorCount := 0
	var firstSuccessResult *backend.BackendResult
	var lastError error
	
	logger.Debug("aggregateDeleteResults: waiting for results with policy %s", policy.AckLevel)
	
	for result := range resultsChan {
		if result.Err == nil {
			successCount++
			if firstSuccessResult == nil {
				firstSuccessResult = result
			}
			
			logger.Debug("aggregateDeleteResults: success from backend %s (%d/%d)", result.BackendID, successCount, totalBackends)
			
			// Для ack=one возвращаем успех сразу после первого успешного ответа
			if policy.AckLevel == "one" {
				logger.Debug("aggregateDeleteResults: returning success for ack=one policy")
				return r.convertDeleteResultToResponse(firstSuccessResult)
			}
		} else {
			errorCount++
			lastError = result.Err
			logger.Debug("aggregateDeleteResults: error from backend %s: %v (%d/%d)", result.BackendID, result.Err, errorCount, totalBackends)
		}
	}
	
	logger.Debug("aggregateDeleteResults: final results - success: %d, errors: %d", successCount, errorCount)
	
	// Логика для ack=all
	if policy.AckLevel == "all" {
		if successCount == totalBackends {
			logger.Debug("aggregateDeleteResults: all backends succeeded for ack=all policy")
			return r.convertDeleteResultToResponse(firstSuccessResult)
		} else {
			logger.Error("aggregateDeleteResults: not all backends succeeded for ack=all policy (%d/%d)", successCount, totalBackends)
			return r.createErrorResponse(http.StatusInternalServerError, "InternalError", "Failed to delete object from all backends")
		}
	}
	
	// Если мы дошли сюда при ack=one, значит ни один бэкенд не ответил успехом
	if policy.AckLevel == "one" && successCount == 0 {
		logger.Error("aggregateDeleteResults: no backends succeeded for ack=one policy")
		if lastError != nil {
			return r.createErrorResponse(http.StatusServiceUnavailable, "ServiceUnavailable", lastError.Error())
		}
		return r.createErrorResponse(http.StatusServiceUnavailable, "ServiceUnavailable", "Failed to delete from any backend")
	}
	
	// Не должны сюда попасть
	logger.Error("aggregateDeleteResults: unexpected code path reached")
	return r.createErrorResponse(http.StatusInternalServerError, "InternalError", "Unexpected error in result aggregation")
}

// convertDeleteResultToResponse преобразует результат DELETE в S3Response
func (r *Replicator) convertDeleteResultToResponse(result *backend.BackendResult) *apigw.S3Response {
	headers := make(http.Header)
	
	if deleteOutput, ok := result.Response.(*s3.DeleteObjectOutput); ok {
		if deleteOutput.VersionId != nil {
			headers.Set("x-amz-version-id", *deleteOutput.VersionId)
		}
		if deleteOutput.DeleteMarker != nil && *deleteOutput.DeleteMarker {
			headers.Set("x-amz-delete-marker", "true")
		}
	}
	
	return &apigw.S3Response{
		StatusCode: http.StatusNoContent,
		Headers:    headers,
	}
}
