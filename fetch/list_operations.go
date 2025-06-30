package fetch

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"s3proxy/apigw"
	"s3proxy/backend"
)

// ListObjectsV2Result представляет результат операции ListObjectsV2
type ListObjectsV2Result struct {
	XMLName               xml.Name `xml:"ListBucketResult"`
	Name                  string   `xml:"Name"`
	Prefix                string   `xml:"Prefix,omitempty"`
	KeyCount              int32    `xml:"KeyCount"`
	MaxKeys               int32    `xml:"MaxKeys"`
	IsTruncated           bool     `xml:"IsTruncated"`
	ContinuationToken     string   `xml:"ContinuationToken,omitempty"`
	NextContinuationToken string   `xml:"NextContinuationToken,omitempty"`
	Contents              []Object `xml:"Contents"`
}

// Object представляет объект в списке
type Object struct {
	Key          string    `xml:"Key"`
	LastModified time.Time `xml:"LastModified"`
	ETag         string    `xml:"ETag"`
	Size         int64     `xml:"Size"`
	StorageClass string    `xml:"StorageClass,omitempty"`
}

// ListBucketsResult представляет результат операции ListBuckets
type ListBucketsResult struct {
	XMLName xml.Name `xml:"ListAllMyBucketsResult"`
	Owner   Owner    `xml:"Owner"`
	Buckets Buckets  `xml:"Buckets"`
}

type Owner struct {
	ID          string `xml:"ID"`
	DisplayName string `xml:"DisplayName"`
}

type Buckets struct {
	Bucket []Bucket `xml:"Bucket"`
}

type Bucket struct {
	Name         string    `xml:"Name"`
	CreationDate time.Time `xml:"CreationDate"`
}

// ListMultipartUploadsResult представляет результат операции ListMultipartUploads
type ListMultipartUploadsResult struct {
	XMLName            xml.Name `xml:"ListMultipartUploadsResult"`
	Bucket             string   `xml:"Bucket"`
	KeyMarker          string   `xml:"KeyMarker,omitempty"`
	UploadIdMarker     string   `xml:"UploadIdMarker,omitempty"`
	NextKeyMarker      string   `xml:"NextKeyMarker,omitempty"`
	NextUploadIdMarker string   `xml:"NextUploadIdMarker,omitempty"`
	MaxUploads         int32    `xml:"MaxUploads"`
	IsTruncated        bool     `xml:"IsTruncated"`
	Upload             []Upload `xml:"Upload"`
}

type Upload struct {
	Key          string    `xml:"Key"`
	UploadId     string    `xml:"UploadId"`
	Initiated    time.Time `xml:"Initiated"`
	StorageClass string    `xml:"StorageClass,omitempty"`
	Owner        Owner     `xml:"Owner"`
}

// listResult представляет результат LIST операции от одного бэкенда
type listResult struct {
	backend *backend.Backend
	result  *s3.ListObjectsV2Output
	error   error
}

// listObjects выполняет операцию LIST OBJECTS с слиянием результатов
func (f *Fetcher) listObjects(ctx context.Context, req *apigw.S3Request, backends []*backend.Backend) *apigw.S3Response {
	// 1. Декодирование токена пагинации
	backendTokens := make(map[string]string)
	if continuationToken := req.Query.Get("continuation-token"); continuationToken != "" {
		var proxyToken ProxyContinuationToken
		if err := json.Unmarshal([]byte(continuationToken), &proxyToken); err == nil {
			backendTokens = proxyToken.BackendTokens
		}
	}

	// 2. Параллельные запросы ко всем бэкендам
	resultsChan := make(chan listResult, len(backends))
	var wg sync.WaitGroup

	for _, backend_iter := range backends {
		wg.Add(1)
		go func(b *backend.Backend) {
			defer wg.Done()

			start := time.Now()
			result := f.performListObjects(ctx, req, b, backendTokens[b.ID])
			latency := time.Since(start)

			//f.metrics.ObserveBackendRequestLatency(backend.ID, "list", latency)

			if result.error == nil {
				//f.metrics.IncrementBackendRequestsTotal(backend.ID, "list", "success")
				// TODO: Fix Response, StatusCode, Err, BytesRead
				f.backendProvider.ReportSuccess(&backend.BackendResult{
					BackendID:  b.ID,
					Method:     "GET",
					Response:   result.result,
					StatusCode: 200,
					Err:        result.error,
					Duration:   latency,
					BytesRead:  0,
				})
			} else {
				//f.metrics.IncrementBackendRequestsTotal(backend.ID, "list", "error")
				f.backendProvider.ReportFailure(&backend.BackendResult{
					BackendID:  b.ID,
					Method:     "GET",
					Response:   result.result,
					StatusCode: 400,
					Err:        result.error,
					Duration:   latency,
				})
			}

			resultsChan <- result
		}(backend_iter)
	}

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// 3. Сбор результатов
	var allResults []listResult
	for result := range resultsChan {
		allResults = append(allResults, result)
	}

	// 4. Слияние результатов
	return f.mergeListResults(req, allResults)
}

// performListObjects выполняет LIST запрос к конкретному бэкенду
func (f *Fetcher) performListObjects(ctx context.Context, req *apigw.S3Request, backend *backend.Backend, continuationToken string) listResult {
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(backend.Config.Bucket),
	}

	if prefix := req.Query.Get("prefix"); prefix != "" {
		input.Prefix = aws.String(prefix)
	}
	if delimiter := req.Query.Get("delimiter"); delimiter != "" {
		input.Delimiter = aws.String(delimiter)
	}
	if maxKeys := req.Query.Get("max-keys"); maxKeys != "" {
		// Парсим max-keys, но для простоты используем значение по умолчанию
		input.MaxKeys = aws.Int32(1000)
	}
	if continuationToken != "" {
		input.ContinuationToken = aws.String(continuationToken)
	}

	result, err := backend.S3Client.ListObjectsV2(ctx, input)
	return listResult{
		backend: backend,
		result:  result,
		error:   err,
	}
}

// mergeListResults объединяет результаты LIST операций от всех бэкендов
func (f *Fetcher) mergeListResults(req *apigw.S3Request, results []listResult) *apigw.S3Response {
	// Собираем все объекты
	objectsMap := make(map[string]Object) // key -> Object
	var isTruncated bool
	newBackendTokens := make(map[string]string)

	for _, result := range results {
		if result.error != nil {
			continue // Пропускаем ошибочные результаты
		}

		// Добавляем объекты, оставляя самые новые версии
		for _, obj := range result.result.Contents {
			key := *obj.Key
			newObj := Object{
				Key:          key,
				LastModified: *obj.LastModified,
				ETag:         *obj.ETag,
				Size:         *obj.Size,
			}
			if obj.StorageClass != "" {
				newObj.StorageClass = string(obj.StorageClass)
			}

			// Если объект уже есть, оставляем более новый
			if existing, exists := objectsMap[key]; exists {
				if newObj.LastModified.After(existing.LastModified) {
					objectsMap[key] = newObj
				}
			} else {
				objectsMap[key] = newObj
			}
		}

		// Проверяем флаг IsTruncated
		if result.result.IsTruncated != nil && *result.result.IsTruncated {
			isTruncated = true
		}

		// Сохраняем токены продолжения
		if result.result.NextContinuationToken != nil {
			newBackendTokens[result.backend.ID] = *result.result.NextContinuationToken
		}
	}

	// Преобразуем map в slice и сортируем
	var objects []Object
	for _, obj := range objectsMap {
		objects = append(objects, obj)
	}
	sort.Slice(objects, func(i, j int) bool {
		return objects[i].Key < objects[j].Key
	})

	// Формируем новый токен продолжения
	var nextContinuationToken string
	if isTruncated && len(newBackendTokens) > 0 {
		proxyToken := ProxyContinuationToken{
			BackendTokens: newBackendTokens,
		}
		if tokenBytes, err := json.Marshal(proxyToken); err == nil {
			nextContinuationToken = string(tokenBytes)
		}
	}

	// Создаем XML ответ
	listResult := ListObjectsV2Result{
		Name:        req.Bucket,
		KeyCount:    int32(len(objects)),
		MaxKeys:     1000, // Значение по умолчанию
		IsTruncated: isTruncated,
		Contents:    objects,
	}

	if prefix := req.Query.Get("prefix"); prefix != "" {
		listResult.Prefix = prefix
	}
	if req.Query.Get("continuation-token") != "" {
		listResult.ContinuationToken = req.Query.Get("continuation-token")
	}
	if nextContinuationToken != "" {
		listResult.NextContinuationToken = nextContinuationToken
	}

	xmlData, err := xml.Marshal(listResult)
	if err != nil {
		return &apigw.S3Response{
			StatusCode: http.StatusInternalServerError,
			Headers:    make(http.Header),
			Error:      err,
		}
	}

	headers := make(http.Header)
	headers.Set("Content-Type", "application/xml")
	headers.Set("Content-Length", fmt.Sprintf("%d", len(xmlData)))

	return &apigw.S3Response{
		StatusCode: http.StatusOK,
		Headers:    headers,
		Body:       io.NopCloser(bytes.NewReader(xmlData)),
	}
}

// listBuckets выполняет операцию LIST BUCKETS
func (f *Fetcher) listBuckets(ctx context.Context, req *apigw.S3Request, backends []*backend.Backend) *apigw.S3Response {
	// Возвращаем один виртуальный бакет, указанный в конфигурации
	buckets := []Bucket{
		{
			Name:         f.virtualBucket,
			CreationDate: time.Now().UTC(), // Используем текущее время
		},
	}

	// Создаем XML ответ
	listResult := ListBucketsResult{
		Owner: Owner{
			ID:          "s3proxy-owner-id",
			DisplayName: "s3proxy-owner",
		},
		Buckets: Buckets{
			Bucket: buckets,
		},
	}

	xmlData, err := xml.Marshal(listResult)
	if err != nil {
		return &apigw.S3Response{
			StatusCode: http.StatusInternalServerError,
			Headers:    make(http.Header),
			Error:      err,
		}
	}

	headers := make(http.Header)
	headers.Set("Content-Type", "application/xml")
	headers.Set("Content-Length", fmt.Sprintf("%d", len(xmlData)))

	return &apigw.S3Response{
		StatusCode: http.StatusOK,
		Headers:    headers,
		Body:       io.NopCloser(bytes.NewReader(xmlData)),
	}
}

// listMultipartUploads выполняет операцию LIST MULTIPART UPLOADS
func (f *Fetcher) listMultipartUploads(ctx context.Context, req *apigw.S3Request, backends []*backend.Backend) *apigw.S3Response {
	type uploadResult struct {
		backend *backend.Backend
		result  *s3.ListMultipartUploadsOutput
		error   error
	}

	resultsChan := make(chan uploadResult, len(backends))
	var wg sync.WaitGroup

	for _, backend_iter := range backends {
		wg.Add(1)
		go func(b *backend.Backend) {
			defer wg.Done()

			input := &s3.ListMultipartUploadsInput{
				Bucket: aws.String(b.Config.Bucket),
			}

			if prefix := req.Query.Get("prefix"); prefix != "" {
				input.Prefix = aws.String(prefix)
			}
			if delimiter := req.Query.Get("delimiter"); delimiter != "" {
				input.Delimiter = aws.String(delimiter)
			}
			if keyMarker := req.Query.Get("key-marker"); keyMarker != "" {
				input.KeyMarker = aws.String(keyMarker)
			}
			if uploadIdMarker := req.Query.Get("upload-id-marker"); uploadIdMarker != "" {
				input.UploadIdMarker = aws.String(uploadIdMarker)
			}

			start := time.Now()
			result, err := b.S3Client.ListMultipartUploads(ctx, input)
			latency := time.Since(start)

			//f.metrics.ObserveBackendRequestLatency(backend.ID, "list_multipart_uploads", latency)

			if err == nil {
				//f.metrics.IncrementBackendRequestsTotal(backend.ID, "list_multipart_uploads", "success")
				f.backendProvider.ReportSuccess(&backend.BackendResult{
					BackendID:  b.ID,
					Method:     "GET",
					Response:   result,
					StatusCode: 200,
					Err:        err,
					Duration:   latency,
					BytesRead:  0,
				})
			} else {
				//f.metrics.IncrementBackendRequestsTotal(backend.ID, "list_multipart_uploads", "error")
				f.backendProvider.ReportFailure(&backend.BackendResult{
					BackendID:  b.ID,
					Method:     "GET",
					Response:   result,
					StatusCode: 200,
					Err:        err,
					Duration:   latency,
					BytesRead:  0,
				})
			}

			resultsChan <- uploadResult{
				backend: b,
				result:  result,
				error:   err,
			}
		}(backend_iter)
	}

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Собираем все uploads
	uploadsMap := make(map[string]Upload) // key+uploadId -> Upload
	var isTruncated bool

	for result := range resultsChan {
		if result.error != nil {
			continue
		}

		// Добавляем uploads
		for _, upload := range result.result.Uploads {
			uploadKey := aws.ToString(upload.Key) + ":" + aws.ToString(upload.UploadId)
			newUpload := Upload{
				Key:       aws.ToString(upload.Key),
				UploadId:  aws.ToString(upload.UploadId),
				Initiated: aws.ToTime(upload.Initiated),
				Owner: Owner{
					ID:          aws.ToString(upload.Owner.ID),
					DisplayName: aws.ToString(upload.Owner.DisplayName),
				},
			}
			if upload.StorageClass != "" {
				newUpload.StorageClass = string(upload.StorageClass)
			}
			uploadsMap[uploadKey] = newUpload
		}

		if result.result.IsTruncated != nil && *result.result.IsTruncated {
			isTruncated = true
		}
	}

	// Преобразуем в slice и сортируем
	var uploads []Upload
	for _, upload := range uploadsMap {
		uploads = append(uploads, upload)
	}
	sort.Slice(uploads, func(i, j int) bool {
		if uploads[i].Key == uploads[j].Key {
			return uploads[i].UploadId < uploads[j].UploadId
		}
		return uploads[i].Key < uploads[j].Key
	})

	// Создаем XML ответ
	listResult := ListMultipartUploadsResult{
		Bucket:      req.Bucket,
		MaxUploads:  1000,
		IsTruncated: isTruncated,
		Upload:      uploads,
	}

	if keyMarker := req.Query.Get("key-marker"); keyMarker != "" {
		listResult.KeyMarker = keyMarker
	}
	if uploadIdMarker := req.Query.Get("upload-id-marker"); uploadIdMarker != "" {
		listResult.UploadIdMarker = uploadIdMarker
	}

	xmlData, err := xml.Marshal(listResult)
	if err != nil {
		return &apigw.S3Response{
			StatusCode: http.StatusInternalServerError,
			Headers:    make(http.Header),
			Error:      err,
		}
	}

	headers := make(http.Header)
	headers.Set("Content-Type", "application/xml")
	headers.Set("Content-Length", fmt.Sprintf("%d", len(xmlData)))

	return &apigw.S3Response{
		StatusCode: http.StatusOK,
		Headers:    headers,
		Body:       io.NopCloser(bytes.NewReader(xmlData)),
	}
}
