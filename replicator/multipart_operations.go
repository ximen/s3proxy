package replicator

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"s3proxy/apigw"
	"s3proxy/logger"
	"s3proxy/routing"
	"s3proxy/backend"
)

// performCreateMultipartUpload выполняет CreateMultipartUpload на одном бэкенде
func (r *Replicator) performCreateMultipartUpload(ctx context.Context, b *backend.Backend, req *apigw.S3Request) *backend.BackendResult {
	startTime := time.Now()
	
	// Создаем контекст с таймаутом
	ctx, cancel := context.WithTimeout(ctx, r.config.OperationTimeout)
	defer cancel()
	
	// Создаем CreateMultipartUploadInput
	createInput := &s3.CreateMultipartUploadInput{
		Bucket: aws.String(b.Config.Bucket),
		Key:    aws.String(req.Key),
	}
	
	// Копируем заголовки из запроса
	if contentType := req.Headers.Get("Content-Type"); contentType != "" {
		createInput.ContentType = aws.String(contentType)
	}
	if contentEncoding := req.Headers.Get("Content-Encoding"); contentEncoding != "" {
		createInput.ContentEncoding = aws.String(contentEncoding)
	}
	
	logger.Debug("performCreateMultipartUpload: sending CreateMultipartUpload to backend %s", b.ID)
	
	// Выполняем запрос с повторами
	var response *s3.CreateMultipartUploadOutput
	var err error
	
	for attempt := 0; attempt <= r.config.RetryAttempts; attempt++ {
		if attempt > 0 {
			logger.Debug("performCreateMultipartUpload: retry attempt %d for backend %s", attempt, b.ID)
			time.Sleep(r.config.RetryDelay)
		}
		
		response, err = b.S3Client.CreateMultipartUpload(ctx, createInput)
		if err == nil {
			break
		}
		
		logger.Debug("performCreateMultipartUpload: attempt %d failed for backend %s: %v", attempt+1, b.ID, err)
	}
	
	duration := time.Since(startTime)
	
	if err != nil {
		logger.Error("performCreateMultipartUpload: failed on backend %s after %d attempts: %v", b.ID, r.config.RetryAttempts+1, err)
	} else {
		logger.Debug("performCreateMultipartUpload: success on backend %s, uploadId=%s, duration=%v", b.ID, *response.UploadId, duration)
	}
	
	return &backend.BackendResult{
		BackendID: b.ID,
		Response:  response,
		Err:       err,
		Duration:  duration,
		Method:    "PUT",
		StatusCode: http.StatusCreated,
	}
}

// performUploadPartAsync выполняет UploadPart асинхронно
func (r *Replicator) performUploadPartAsync(ctx context.Context, req *apigw.S3Request, backends []*backend.Backend, mapping *multipartUploadMapping, partNumber string, opCtx *operationContext) {
	logger.Debug("performUploadPartAsync: starting async UploadPart for %d backends", len(backends))
	
	// Клонируем reader для каждого бэкенда
	readers, err := r.readerCloner.Clone(req.Body, len(backends))
	if err != nil {
		logger.Error("performUploadPartAsync: failed to clone reader: %v", err)
		return
	}
	
	var wg sync.WaitGroup
	for i, backend_iter := range backends {
		wg.Add(1)
		go func(b *backend.Backend, reader io.Reader) {
			defer wg.Done()
			result := r.performUploadPartToBackend(ctx, b, req, reader, mapping, partNumber)
			r.reportBackendResult(result)
			//r.updateMetrics(b.ID, "upload_part", result)
		}(backend_iter, readers[i])
	}
	
	wg.Wait()
	logger.Debug("performUploadPartAsync: completed async UploadPart for %d backends", len(backends))
}

// performUploadPartSync выполняет UploadPart синхронно
func (r *Replicator) performUploadPartSync(opCtx *operationContext, req *apigw.S3Request, backends []*backend.Backend, mapping *multipartUploadMapping, partNumber string, policy routing.WriteOperationPolicy) *apigw.S3Response {
	logger.Debug("performUploadPartSync: starting sync UploadPart for %d backends with policy %s", len(backends), policy.AckLevel)
	
	// Клонируем reader для каждого бэкенда
	readers, err := r.readerCloner.Clone(req.Body, len(backends))
	if err != nil {
		logger.Error("performUploadPartSync: failed to clone reader: %v", err)
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
			
			result := r.performUploadPartToBackend(opCtx.ctx, b, req, reader, mapping, partNumber)
			r.reportBackendResult(result)
			//r.updateMetrics(b.ID, "upload_part", result)
			
			resultsChan <- result
		}(backend_iter, readers[i])
	}
	
	// Горутина для закрытия канала после завершения всех операций
	go func() {
		wg.Wait()
		close(resultsChan)
	}()
	
	// Агрегируем результаты в соответствии с политикой
	return r.aggregateUploadPartResults(resultsChan, policy, len(backends))
}

// performUploadPartToBackend выполняет UploadPart на одном бэкенде
func (r *Replicator) performUploadPartToBackend(ctx context.Context, b *backend.Backend, req *apigw.S3Request, body io.Reader, mapping *multipartUploadMapping, partNumber string) *backend.BackendResult {
	startTime := time.Now()
	
	// Создаем контекст с таймаутом
	ctx, cancel := context.WithTimeout(ctx, r.config.OperationTimeout)
	defer cancel()
	
	// Получаем uploadId для этого бэкенда
	backendUploadID, exists := mapping.BackendUploads[b.ID]
	if !exists {
		return &backend.BackendResult{
			BackendID: b.ID,
			Err:       fmt.Errorf("no upload ID found for backend %s", b.ID),
			Duration:  time.Since(startTime),
			Method:    "PUT",
			StatusCode: http.StatusCreated,
		}
	}
	
	// Парсим номер части
	partNum, err := strconv.ParseInt(partNumber, 10, 32)
	if err != nil {
		return &backend.BackendResult{
			BackendID: b.ID,
			Err:       fmt.Errorf("invalid part number: %s", partNumber),
			Duration:  time.Since(startTime),
			Method:    "PUT",
			StatusCode: http.StatusBadRequest,
		}
	}
	
	// Оборачиваем reader для подсчета байт
	countingReader := NewCountingReader(body)
	
	// Создаем UploadPartInput
	uploadInput := &s3.UploadPartInput{
		Bucket:     aws.String(b.Config.Bucket),
		Key:        aws.String(req.Key),
		UploadId:   aws.String(backendUploadID),
		PartNumber: aws.Int32(int32(partNum)),
		Body:       countingReader,
	}
	
	logger.Debug("performUploadPartToBackend: sending UploadPart to backend %s, uploadId=%s, partNumber=%d", b.ID, backendUploadID, partNum)
	
	// Выполняем запрос с повторами
	var response *s3.UploadPartOutput
	
	for attempt := 0; attempt <= r.config.RetryAttempts; attempt++ {
		if attempt > 0 {
			logger.Debug("performUploadPartToBackend: retry attempt %d for backend %s", attempt, b.ID)
			time.Sleep(r.config.RetryDelay)
		}
		
		response, err = b.S3Client.UploadPart(ctx, uploadInput)
		if err == nil {
			break
		}
		
		logger.Debug("performUploadPartToBackend: attempt %d failed for backend %s: %v", attempt+1, b.ID, err)
	}
	
	duration := time.Since(startTime)
	bytesWritten := countingReader.Count()
	
	if err != nil {
		logger.Error("performUploadPartToBackend: failed on backend %s after %d attempts: %v", b.ID, r.config.RetryAttempts+1, err)
	} else {
		logger.Debug("performUploadPartToBackend: success on backend %s, bytes=%d, duration=%v", b.ID, bytesWritten, duration)
	}
	
	return &backend.BackendResult{
		BackendID:    b.ID,
		Response:     response,
		Err:          err,
		Duration:     duration,
		BytesWritten: bytesWritten,
		Method:       "PUT",
		StatusCode:   http.StatusOK,
	}
}

// aggregateUploadPartResults агрегирует результаты UploadPart операций
func (r *Replicator) aggregateUploadPartResults(resultsChan <-chan *backend.BackendResult, policy routing.WriteOperationPolicy, totalBackends int) *apigw.S3Response {
	successCount := 0
	errorCount := 0
	var firstSuccessResult *backend.BackendResult
	var lastError error
	
	logger.Debug("aggregateUploadPartResults: waiting for results with policy %s", policy.AckLevel)
	
	for result := range resultsChan {
		if result.Err == nil {
			successCount++
			if firstSuccessResult == nil {
				firstSuccessResult = result
			}
			
			logger.Debug("aggregateUploadPartResults: success from backend %s (%d/%d)", result.BackendID, successCount, totalBackends)
			
			// Для ack=one возвращаем успех сразу после первого успешного ответа
			if policy.AckLevel == "one" {
				logger.Debug("aggregateUploadPartResults: returning success for ack=one policy")
				return r.convertUploadPartResultToResponse(firstSuccessResult)
			}
		} else {
			errorCount++
			lastError = result.Err
			logger.Debug("aggregateUploadPartResults: error from backend %s: %v (%d/%d)", result.BackendID, result.Err, errorCount, totalBackends)
		}
	}
	
	logger.Debug("aggregateUploadPartResults: final results - success: %d, errors: %d", successCount, errorCount)
	
	// Логика для ack=all
	if policy.AckLevel == "all" {
		if successCount == totalBackends {
			logger.Debug("aggregateUploadPartResults: all backends succeeded for ack=all policy")
			return r.convertUploadPartResultToResponse(firstSuccessResult)
		} else {
			logger.Error("aggregateUploadPartResults: not all backends succeeded for ack=all policy (%d/%d)", successCount, totalBackends)
			return r.createErrorResponse(http.StatusInternalServerError, "InternalError", "Failed to upload part to all backends")
		}
	}
	
	// Если мы дошли сюда при ack=one, значит ни один бэкенд не ответил успехом
	if policy.AckLevel == "one" && successCount == 0 {
		logger.Error("aggregateUploadPartResults: no backends succeeded for ack=one policy")
		if lastError != nil {
			return r.createErrorResponse(http.StatusServiceUnavailable, "ServiceUnavailable", lastError.Error())
		}
		return r.createErrorResponse(http.StatusServiceUnavailable, "ServiceUnavailable", "Failed to upload part to any backend")
	}
	
	// Не должны сюда попасть
	logger.Error("aggregateUploadPartResults: unexpected code path reached")
	return r.createErrorResponse(http.StatusInternalServerError, "InternalError", "Unexpected error in result aggregation")
}

// convertUploadPartResultToResponse преобразует результат UploadPart в S3Response
func (r *Replicator) convertUploadPartResultToResponse(result *backend.BackendResult) *apigw.S3Response {
	headers := make(http.Header)
	
	if uploadOutput, ok := result.Response.(*s3.UploadPartOutput); ok {
		if uploadOutput.ETag != nil {
			headers.Set("ETag", *uploadOutput.ETag)
		}
	}
	
	return &apigw.S3Response{
		StatusCode: http.StatusOK,
		Headers:    headers,
	}
}

// performCompleteMultipartUploadSync выполняет CompleteMultipartUpload синхронно
func (r *Replicator) performCompleteMultipartUploadSync(opCtx *operationContext, req *apigw.S3Request, backends []*backend.Backend, mapping *multipartUploadMapping, policy routing.WriteOperationPolicy) *apigw.S3Response {
	logger.Debug("performCompleteMultipartUploadSync: starting sync CompleteMultipartUpload for %d backends with policy %s", len(backends), policy.AckLevel)
	
	// Создаем канал для результатов
	resultsChan := make(chan *backend.BackendResult, len(backends))
	
	// Запускаем горутины для каждого бэкенда
	var wg sync.WaitGroup
	for _, backend_iter := range backends {
		wg.Add(1)
		go func(b *backend.Backend) {
			defer wg.Done()
			
			result := r.performCompleteMultipartUploadToBackend(opCtx.ctx, b, req, mapping)
			r.reportBackendResult(result)
			//r.updateMetrics(b.ID, "complete_multipart_upload", result)
			
			resultsChan <- result
		}(backend_iter)
	}
	
	// Горутина для закрытия канала после завершения всех операций
	go func() {
		wg.Wait()
		close(resultsChan)
	}()
	
	// Агрегируем результаты в соответствии с политикой
	return r.aggregateCompleteMultipartUploadResults(resultsChan, policy, len(backends))
}

// performCompleteMultipartUploadToBackend выполняет CompleteMultipartUpload на одном бэкенде
func (r *Replicator) performCompleteMultipartUploadToBackend(ctx context.Context, b *backend.Backend, req *apigw.S3Request, mapping *multipartUploadMapping) *backend.BackendResult {
	startTime := time.Now()
	
	// Создаем контекст с таймаутом
	ctx, cancel := context.WithTimeout(ctx, r.config.OperationTimeout)
	defer cancel()
	
	// Получаем uploadId для этого бэкенда
	backendUploadID, exists := mapping.BackendUploads[b.ID]
	if !exists {
		return &backend.BackendResult{
			BackendID: b.ID,
			Err:       fmt.Errorf("no upload ID found for backend %s", b.ID),
			Duration:  time.Since(startTime),
			Method:    "PUT",
			StatusCode: http.StatusCreated,
		}
	}
	
	// Парсим XML тело запроса для получения списка частей
	// Для простоты используем пустой список - в реальной реализации нужно парсить XML
	var completedParts []types.CompletedPart
	
	// Создаем CompleteMultipartUploadInput
	completeInput := &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(b.Config.Bucket),
		Key:      aws.String(req.Key),
		UploadId: aws.String(backendUploadID),
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: completedParts,
		},
	}
	
	logger.Debug("performCompleteMultipartUploadToBackend: sending CompleteMultipartUpload to backend %s, uploadId=%s", b.ID, backendUploadID)
	
	// Выполняем запрос с повторами
	var response *s3.CompleteMultipartUploadOutput
	var err error
	
	for attempt := 0; attempt <= r.config.RetryAttempts; attempt++ {
		if attempt > 0 {
			logger.Debug("performCompleteMultipartUploadToBackend: retry attempt %d for backend %s", attempt, b.ID)
			time.Sleep(r.config.RetryDelay)
		}
		
		response, err = b.S3Client.CompleteMultipartUpload(ctx, completeInput)
		if err == nil {
			break
		}
		
		logger.Debug("performCompleteMultipartUploadToBackend: attempt %d failed for backend %s: %v", attempt+1, b.ID, err)
	}
	
	duration := time.Since(startTime)
	
	if err != nil {
		logger.Error("performCompleteMultipartUploadToBackend: failed on backend %s after %d attempts: %v", b.ID, r.config.RetryAttempts+1, err)
	} else {
		logger.Debug("performCompleteMultipartUploadToBackend: success on backend %s, duration=%v", b.ID, duration)
	}
	
	return &backend.BackendResult{
		BackendID: b.ID,
		Response:  response,
		Err:       err,
		Duration:  duration,
	}
}

// aggregateCompleteMultipartUploadResults агрегирует результаты CompleteMultipartUpload операций
func (r *Replicator) aggregateCompleteMultipartUploadResults(resultsChan <-chan *backend.BackendResult, policy routing.WriteOperationPolicy, totalBackends int) *apigw.S3Response {
	successCount := 0
	errorCount := 0
	var firstSuccessResult *backend.BackendResult
	var lastError error
	
	logger.Debug("aggregateCompleteMultipartUploadResults: waiting for results with policy %s", policy.AckLevel)
	
	for result := range resultsChan {
		if result.Err == nil {
			successCount++
			if firstSuccessResult == nil {
				firstSuccessResult = result
			}
			
			logger.Debug("aggregateCompleteMultipartUploadResults: success from backend %s (%d/%d)", result.BackendID, successCount, totalBackends)
			
			// Для ack=one возвращаем успех сразу после первого успешного ответа
			if policy.AckLevel == "one" {
				logger.Debug("aggregateCompleteMultipartUploadResults: returning success for ack=one policy")
				return r.convertCompleteMultipartUploadResultToResponse(firstSuccessResult)
			}
		} else {
			errorCount++
			lastError = result.Err
			logger.Debug("aggregateCompleteMultipartUploadResults: error from backend %s: %v (%d/%d)", result.BackendID, result.Err, errorCount, totalBackends)
		}
	}
	
	logger.Debug("aggregateCompleteMultipartUploadResults: final results - success: %d, errors: %d", successCount, errorCount)
	
	// Логика для ack=all
	if policy.AckLevel == "all" {
		if successCount == totalBackends {
			logger.Debug("aggregateCompleteMultipartUploadResults: all backends succeeded for ack=all policy")
			return r.convertCompleteMultipartUploadResultToResponse(firstSuccessResult)
		} else {
			logger.Error("aggregateCompleteMultipartUploadResults: not all backends succeeded for ack=all policy (%d/%d)", successCount, totalBackends)
			return r.createErrorResponse(http.StatusInternalServerError, "InternalError", "Failed to complete multipart upload on all backends")
		}
	}
	
	// Если мы дошли сюда при ack=one, значит ни один бэкенд не ответил успехом
	if policy.AckLevel == "one" && successCount == 0 {
		logger.Error("aggregateCompleteMultipartUploadResults: no backends succeeded for ack=one policy")
		if lastError != nil {
			return r.createErrorResponse(http.StatusServiceUnavailable, "ServiceUnavailable", lastError.Error())
		}
		return r.createErrorResponse(http.StatusServiceUnavailable, "ServiceUnavailable", "Failed to complete multipart upload on any backend")
	}
	
	// Не должны сюда попасть
	logger.Error("aggregateCompleteMultipartUploadResults: unexpected code path reached")
	return r.createErrorResponse(http.StatusInternalServerError, "InternalError", "Unexpected error in result aggregation")
}

// convertCompleteMultipartUploadResultToResponse преобразует результат CompleteMultipartUpload в S3Response
func (r *Replicator) convertCompleteMultipartUploadResultToResponse(result *backend.BackendResult) *apigw.S3Response {
	headers := make(http.Header)
	
	if completeOutput, ok := result.Response.(*s3.CompleteMultipartUploadOutput); ok {
		if completeOutput.ETag != nil {
			headers.Set("ETag", *completeOutput.ETag)
		}
		if completeOutput.VersionId != nil {
			headers.Set("x-amz-version-id", *completeOutput.VersionId)
		}
		
		// Создаем XML ответ
		body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<CompleteMultipartUploadResult>
    <Location>%s</Location>
    <Bucket>%s</Bucket>
    <Key>%s</Key>
    <ETag>%s</ETag>
</CompleteMultipartUploadResult>`, 
			aws.ToString(completeOutput.Location),
			aws.ToString(completeOutput.Bucket),
			aws.ToString(completeOutput.Key),
			aws.ToString(completeOutput.ETag))
		
		headers.Set("Content-Type", "application/xml")
		headers.Set("Content-Length", fmt.Sprintf("%d", len(body)))
		
		return &apigw.S3Response{
			StatusCode: http.StatusOK,
			Headers:    headers,
			Body:       io.NopCloser(strings.NewReader(body)),
		}
	}
	
	return &apigw.S3Response{
		StatusCode: http.StatusOK,
		Headers:    headers,
	}
}

// performAbortMultipartUpload выполняет AbortMultipartUpload на всех бэкендах
func (r *Replicator) performAbortMultipartUpload(opCtx *operationContext, req *apigw.S3Request, backends []*backend.Backend, mapping *multipartUploadMapping) {
	logger.Debug("performAbortMultipartUpload: starting AbortMultipartUpload for %d backends", len(backends))
	
	var wg sync.WaitGroup
	for _, backend_iter := range backends {
		wg.Add(1)
		go func(b *backend.Backend) {
			defer wg.Done()
			
			result := r.performAbortMultipartUploadToBackend(opCtx.ctx, b, req, mapping)
			r.reportBackendResult(result)
			//r.updateMetrics(b.ID, "abort_multipart_upload", result)
		}(backend_iter)
	}
	
	wg.Wait()
	logger.Debug("performAbortMultipartUpload: completed AbortMultipartUpload for %d backends", len(backends))
}

// performAbortMultipartUploadToBackend выполняет AbortMultipartUpload на одном бэкенде
func (r *Replicator) performAbortMultipartUploadToBackend(ctx context.Context, b *backend.Backend, req *apigw.S3Request, mapping *multipartUploadMapping) *backend.BackendResult {
	startTime := time.Now()
	
	// Создаем контекст с таймаутом
	ctx, cancel := context.WithTimeout(ctx, r.config.OperationTimeout)
	defer cancel()
	
	// Получаем uploadId для этого бэкенда
	backendUploadID, exists := mapping.BackendUploads[b.ID]
	if !exists {
		return &backend.BackendResult{
			BackendID: b.ID,
			Err:       fmt.Errorf("no upload ID found for backend %s", b.ID),
			Duration:  time.Since(startTime),
			Method:    "PUT",
			StatusCode: http.StatusCreated,
		}
	}
	
	// Создаем AbortMultipartUploadInput
	abortInput := &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(b.Config.Bucket),
		Key:      aws.String(req.Key),
		UploadId: aws.String(backendUploadID),
	}
	
	logger.Debug("performAbortMultipartUploadToBackend: sending AbortMultipartUpload to backend %s, uploadId=%s", b.ID, backendUploadID)
	
	// Выполняем запрос с повторами
	var response *s3.AbortMultipartUploadOutput
	var err error
	
	for attempt := 0; attempt <= r.config.RetryAttempts; attempt++ {
		if attempt > 0 {
			logger.Debug("performAbortMultipartUploadToBackend: retry attempt %d for backend %s", attempt, b.ID)
			time.Sleep(r.config.RetryDelay)
		}
		
		response, err = b.S3Client.AbortMultipartUpload(ctx, abortInput)
		if err == nil {
			break
		}
		
		logger.Debug("performAbortMultipartUploadToBackend: attempt %d failed for backend %s: %v", attempt+1, b.ID, err)
	}
	
	duration := time.Since(startTime)
	
	if err != nil {
		logger.Error("performAbortMultipartUploadToBackend: failed on backend %s after %d attempts: %v", b.ID, r.config.RetryAttempts+1, err)
	} else {
		logger.Debug("performAbortMultipartUploadToBackend: success on backend %s, duration=%v", b.ID, duration)
	}
	
	return &backend.BackendResult{
		BackendID: b.ID,
		Response:  response,
		Err:       err,
		Duration:  duration,
	}
}
