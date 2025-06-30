package fetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"s3proxy/apigw"
	"s3proxy/backend"
	"s3proxy/routing"
)

// Fetcher реализует интерфейс FetchingExecutor
type Fetcher struct {
	backendProvider *backend.Manager
	cache           Cache
	//metrics         Metrics
	virtualBucket   string
}

// NewFetcher создает новый экземпляр Fetcher
func NewFetcher(provider *backend.Manager, cache Cache, virtualBucket string) *Fetcher {
	return &Fetcher{
		backendProvider: provider,
		cache:           cache,
		//metrics:         metrics,
		virtualBucket:   virtualBucket,
	}
}

// GetObject выполняет операцию GET в соответствии с политикой
func (f *Fetcher) GetObject(ctx context.Context, req *apigw.S3Request, policy routing.ReadOperationPolicy) *apigw.S3Response {
	// 1. Проверка кэша
	if response, found := f.cache.Get(req.Bucket, req.Key); found {
		return response
	}

	// 2. Получение живых бэкендов
	backends := f.backendProvider.GetLiveBackends()
	if len(backends) == 0 {
		return &apigw.S3Response{
			StatusCode: http.StatusServiceUnavailable,
			Headers:    make(http.Header),
			Error:      fmt.Errorf("no live backends available"),
		}
	}

	// 3. Выполнение запроса в зависимости от стратегии
	switch policy.Strategy {
	case "first":
		return f.getObjectFirst(ctx, req, backends)
	case "newest":
		return f.getObjectNewest(ctx, req, backends)
	default:
		return &apigw.S3Response{
			StatusCode: http.StatusInternalServerError,
			Headers:    make(http.Header),
			Error:      fmt.Errorf("unknown read strategy: %s", policy.Strategy),
		}
	}
}

// HeadObject выполняет операцию HEAD в соответствии с политикой
func (f *Fetcher) HeadObject(ctx context.Context, req *apigw.S3Request, policy routing.ReadOperationPolicy) *apigw.S3Response {
	// 1. Проверка кэша
	if response, found := f.cache.Get(req.Bucket, req.Key); found {
		// Для HEAD запроса убираем тело ответа
		return &apigw.S3Response{
			StatusCode: response.StatusCode,
			Headers:    response.Headers,
			Body:       nil,
			Error:      response.Error,
		}
	}

	// 2. Получение живых бэкендов
	backends := f.backendProvider.GetLiveBackends()
	if len(backends) == 0 {
		return &apigw.S3Response{
			StatusCode: http.StatusServiceUnavailable,
			Headers:    make(http.Header),
			Error:      fmt.Errorf("no live backends available"),
		}
	}

	// 3. Выполнение запроса в зависимости от стратегии
	switch policy.Strategy {
	case "first":
		return f.headObjectFirst(ctx, req, backends)
	case "newest":
		return f.headObjectNewest(ctx, req, backends)
	default:
		return &apigw.S3Response{
			StatusCode: http.StatusInternalServerError,
			Headers:    make(http.Header),
			Error:      fmt.Errorf("unknown read strategy: %s", policy.Strategy),
		}
	}
}

// HeadBucket выполняет операцию HEAD BUCKET для проверки существования бакета
func (f *Fetcher) HeadBucket(ctx context.Context, req *apigw.S3Request) *apigw.S3Response {
	backends := f.backendProvider.GetLiveBackends()
	if len(backends) == 0 {
		return &apigw.S3Response{
			StatusCode: http.StatusServiceUnavailable,
			Headers:    make(http.Header),
			Error:      fmt.Errorf("no live backends available"),
		}
	}

	// Для HEAD Bucket используем стратегию "first" - достаточно одного успешного ответа
	return f.headBucketFirst(ctx, req, backends)
}

// ListObjects выполняет операцию LIST
func (f *Fetcher) ListObjects(ctx context.Context, req *apigw.S3Request) *apigw.S3Response {
	backends := f.backendProvider.GetLiveBackends()
	if len(backends) == 0 {
		return &apigw.S3Response{
			StatusCode: http.StatusServiceUnavailable,
			Headers:    make(http.Header),
			Error:      fmt.Errorf("no live backends available"),
		}
	}

	return f.listObjects(ctx, req, backends)
}

// ListBuckets выполняет операцию LIST BUCKETS
func (f *Fetcher) ListBuckets(ctx context.Context, req *apigw.S3Request) *apigw.S3Response {
	backends := f.backendProvider.GetLiveBackends()
	if len(backends) == 0 {
		return &apigw.S3Response{
			StatusCode: http.StatusServiceUnavailable,
			Headers:    make(http.Header),
			Error:      fmt.Errorf("no live backends available"),
		}
	}

	return f.listBuckets(ctx, req, backends)
}

// ListMultipartUploads выполняет операцию LIST MULTIPART UPLOADS
func (f *Fetcher) ListMultipartUploads(ctx context.Context, req *apigw.S3Request) *apigw.S3Response {
	backends := f.backendProvider.GetLiveBackends()
	if len(backends) == 0 {
		return &apigw.S3Response{
			StatusCode: http.StatusServiceUnavailable,
			Headers:    make(http.Header),
			Error:      fmt.Errorf("no live backends available"),
		}
	}

	return f.listMultipartUploads(ctx, req, backends)
}

// getObjectFirst реализует стратегию "first" для GET запросов
func (f *Fetcher) getObjectFirst(ctx context.Context, req *apigw.S3Request, backends []*backend.Backend) *apigw.S3Response {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		response  *apigw.S3Response
		backendID string
	}

	resultChan := make(chan result, len(backends))
	var wg sync.WaitGroup

	// Запускаем параллельные запросы ко всем бэкендам
	for _, backend_iter := range backends {
		wg.Add(1)
		go func(b *backend.Backend) {
			defer wg.Done()
			
			start := time.Now()
			response := f.performGetObject(ctx, req, b)
			latency := time.Since(start)

			//f.metrics.ObserveBackendRequestLatency(backend.ID, "get", latency)

			if response.Error == nil && response.StatusCode == http.StatusOK {
				//f.metrics.IncrementBackendRequestsTotal(backend.ID, "get", "success")
				f.backendProvider.ReportSuccess(& backend.BackendResult{
					BackendID:    b.ID,
					Method:       "GET",
					Response:     response,
					StatusCode:   response.StatusCode,
					Err:          response.Error,
					Duration:     latency,
					BytesRead: 	  response.Body.(*bytesCountingReader).totalRead,
				})
				
				select {
				case resultChan <- result{response: response, backendID: b.ID}:
				case <-ctx.Done():
				}
			} else {
				//f.metrics.IncrementBackendRequestsTotal(backend.ID, "get", "error")
				f.backendProvider.ReportFailure(& backend.BackendResult{
					BackendID:    b.ID,
					Method:       "GET",
					Response:     response,
					StatusCode:   response.StatusCode,
					Err:          response.Error,
					Duration:     latency,
					BytesRead: 	  response.Body.(*bytesCountingReader).totalRead,
				})
			}
		}(backend_iter)
	}

	// Ждем первый успешный ответ
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	for res := range resultChan {
		cancel() // Отменяем остальные запросы
		return res.response
	}

	// Если никто не ответил успешно
	return &apigw.S3Response{
		StatusCode: http.StatusNotFound,
		Headers:    make(http.Header),
		Error:      fmt.Errorf("object not found on any backend"),
	}
}

// getObjectNewest реализует стратегию "newest" для GET запросов
func (f *Fetcher) getObjectNewest(ctx context.Context, req *apigw.S3Request, backends []*backend.Backend) *apigw.S3Response {
	// Фаза 1: HEAD запросы ко всем бэкендам
	type headResult struct {
		backend      *backend.Backend
		lastModified time.Time
		success      bool
		error        error
	}

	headResults := make([]headResult, 0, len(backends))
	var wg sync.WaitGroup
	resultsChan := make(chan headResult, len(backends))

	for _, backend_iter := range backends {
		wg.Add(1)
		go func(b *backend.Backend) {
			defer wg.Done()
			
			start := time.Now()
			response := f.performHeadObject(ctx, req, b)
			latency := time.Since(start)

			//f.metrics.ObserveBackendRequestLatency(backend.ID, "head", latency)

			bytesRead := int64(0)
			if response.Body != nil {
    			if counter, ok := response.Body.(*bytesCountingReader); ok {
        			bytesRead = counter.totalRead
    			}
			}

			if response.Error == nil && response.StatusCode == http.StatusOK {
				//f.metrics.IncrementBackendRequestsTotal(backend.ID, "head", "success")
				f.backendProvider.ReportSuccess(& backend.BackendResult{
					BackendID:    b.ID,
					Method:       "HEAD",
					Response:     response,
					StatusCode:   response.StatusCode,
					Err:          response.Error,
					Duration:     latency,
					BytesRead: 	  bytesRead,
				})
				
				// Парсим Last-Modified
				lastModifiedStr := response.Headers.Get("Last-Modified")
				lastModified, err := time.Parse(time.RFC1123, lastModifiedStr)
				if err != nil {
					lastModified = time.Time{} // Если не удалось распарсить, используем нулевое время
				}
				
				resultsChan <- headResult{
					backend:      b,
					lastModified: lastModified,
					success:      true,
				}
			} else {
				//f.metrics.IncrementBackendRequestsTotal(backend.ID, "head", "error")
				f.backendProvider.ReportFailure(& backend.BackendResult{
					BackendID:    b.ID,
					Method:       "HEAD",
					Response:     response,
					StatusCode:   response.StatusCode,
					Err:          response.Error,
					Duration:     latency,
					BytesRead: 	  bytesRead,
				})
				
				resultsChan <- headResult{
					backend: b,
					success: false,
					error:   response.Error,
				}
			}
		}(backend_iter)
	}

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Собираем результаты HEAD запросов
	for result := range resultsChan {
		headResults = append(headResults, result)
	}

	// Фаза 2: Находим бэкенд с самым новым объектом
	var newestBackend *backend.Backend
	var newestTime time.Time

	for _, result := range headResults {
		if result.success && result.lastModified.After(newestTime) {
			newestTime = result.lastModified
			newestBackend = result.backend
		}
	}

	if newestBackend == nil {
		return &apigw.S3Response{
			StatusCode: http.StatusNotFound,
			Headers:    make(http.Header),
			Error:      fmt.Errorf("object not found on any backend"),
		}
	}

	// Фаза 3: GET запрос к выбранному бэкенду
	start := time.Now()
	response := f.performGetObject(ctx, req, newestBackend)
	latency := time.Since(start)

	//f.metrics.ObserveBackendRequestLatency(newestBackend.ID, "get", latency)

	if response.Error == nil && response.StatusCode == http.StatusOK {
		//f.metrics.IncrementBackendRequestsTotal(newestBackend.ID, "get", "success")
		f.backendProvider.ReportSuccess(& backend.BackendResult{
					BackendID:    newestBackend.ID,
					Method:       "GET",
					Response:     response,
					StatusCode:   response.StatusCode,
					Err:          response.Error,
					Duration:     latency,
					BytesRead: 	  response.Body.(*bytesCountingReader).totalRead,
				})
	} else {
		//f.metrics.IncrementBackendRequestsTotal(newestBackend.ID, "get", "error")
		f.backendProvider.ReportFailure(& backend.BackendResult{
					BackendID:    newestBackend.ID,
					Method:       "GET",
					Response:     response,
					StatusCode:   response.StatusCode,
					Err:          response.Error,
					Duration:     latency,
					BytesRead: 	  response.Body.(*bytesCountingReader).totalRead,
				})
	}

	return response
}

// headObjectFirst реализует стратегию "first" для HEAD запросов
func (f *Fetcher) headObjectFirst(ctx context.Context, req *apigw.S3Request, backends []*backend.Backend) *apigw.S3Response {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		response  *apigw.S3Response
		backendID string
	}

	resultChan := make(chan result, len(backends))
	var wg sync.WaitGroup

	for _, backend_iter := range backends {
		wg.Add(1)
		go func(b *backend.Backend) {
			defer wg.Done()
			
			start := time.Now()
			response := f.performHeadObject(ctx, req, b)
			latency := time.Since(start)

			//f.metrics.ObserveBackendRequestLatency(backend.ID, "head", latency)

			bytesRead := int64(0)
			if response.Body != nil {
    			if counter, ok := response.Body.(*bytesCountingReader); ok {
        			bytesRead = counter.totalRead
    			}
			}

			if response.Error == nil && response.StatusCode == http.StatusOK {
				//f.metrics.IncrementBackendRequestsTotal(backend.ID, "head", "success")
				f.backendProvider.ReportSuccess(& backend.BackendResult{
					BackendID:    b.ID,
					Method:       "HEAD",
					Response:     response,
					StatusCode:   response.StatusCode,
					Err:          response.Error,
					Duration:     latency,
					BytesRead: 	  bytesRead,
				})
				
				select {
				case resultChan <- result{response: response, backendID: b.ID}:
				case <-ctx.Done():
				}
			} else {
				//f.metrics.IncrementBackendRequestsTotal(backend.ID, "head", "error")
				f.backendProvider.ReportFailure(& backend.BackendResult{
					BackendID:    b.ID,
					Method:       "HEAD",
					Response:     response,
					StatusCode:   response.StatusCode,
					Err:          response.Error,
					Duration:     latency,
					BytesRead: 	  bytesRead,
				})
			}
		}(backend_iter)
	}

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	for res := range resultChan {
		cancel()
		return res.response
	}

	return &apigw.S3Response{
		StatusCode: http.StatusNotFound,
		Headers:    make(http.Header),
		Error:      fmt.Errorf("object not found on any backend"),
	}
}

// headObjectNewest реализует стратегию "newest" для HEAD запросов
func (f *Fetcher) headObjectNewest(ctx context.Context, req *apigw.S3Request, backends []*backend.Backend) *apigw.S3Response {
	// Для HEAD запросов стратегия "newest" работает так же, как и для GET,
	// но возвращаем результат HEAD запроса к самому новому объекту
	type headResult struct {
		backend      *backend.Backend
		response     *apigw.S3Response
		lastModified time.Time
		success      bool
	}

	headResults := make([]headResult, 0, len(backends))
	var wg sync.WaitGroup
	resultsChan := make(chan headResult, len(backends))

	for _, backend_iter := range backends {
		wg.Add(1)
		go func(b *backend.Backend) {
			defer wg.Done()
			
			start := time.Now()
			response := f.performHeadObject(ctx, req, b)
			latency := time.Since(start)

			//f.metrics.ObserveBackendRequestLatency(backend.ID, "head", latency)
			bytesRead := int64(0)
			if response.Body != nil {
    			if counter, ok := response.Body.(*bytesCountingReader); ok {
        			bytesRead = counter.totalRead
    			}
			}

			if response.Error == nil && response.StatusCode == http.StatusOK {
				//f.metrics.IncrementBackendRequestsTotal(backend.ID, "head", "success")
				f.backendProvider.ReportSuccess(& backend.BackendResult{
					BackendID:    b.ID,
					Method:       "HEAD",
					Response:     response,
					StatusCode:   response.StatusCode,
					Err:          response.Error,
					Duration:     latency,
					BytesRead: 	  bytesRead,
				})
				
				lastModifiedStr := response.Headers.Get("Last-Modified")
				lastModified, err := time.Parse(time.RFC1123, lastModifiedStr)
				if err != nil {
					lastModified = time.Time{}
				}
				
				resultsChan <- headResult{
					backend:      b,
					response:     response,
					lastModified: lastModified,
					success:      true,
				}
			} else {
				//f.metrics.IncrementBackendRequestsTotal(backend.ID, "head", "error")
				f.backendProvider.ReportFailure(& backend.BackendResult{
					BackendID:    b.ID,
					Method:       "HEAD",
					Response:     response,
					StatusCode:   response.StatusCode,
					Err:          response.Error,
					Duration:     latency,
					BytesRead: 	  bytesRead,
				})
				
				resultsChan <- headResult{
					backend:  b,
					response: response,
					success:  false,
				}
			}
		}(backend_iter)
	}

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	for result := range resultsChan {
		headResults = append(headResults, result)
	}

	// Находим самый новый объект
	var newestResult *headResult
	var newestTime time.Time

	for i, result := range headResults {
		if result.success && result.lastModified.After(newestTime) {
			newestTime = result.lastModified
			newestResult = &headResults[i]
		}
	}

	if newestResult == nil {
		return &apigw.S3Response{
			StatusCode: http.StatusNotFound,
			Headers:    make(http.Header),
			Error:      fmt.Errorf("object not found on any backend"),
		}
	}

	return newestResult.response
}

// headBucketFirst реализует HEAD Bucket с стратегией "first"
func (f *Fetcher) headBucketFirst(ctx context.Context, req *apigw.S3Request, backends []*backend.Backend) *apigw.S3Response {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		response  *apigw.S3Response
		backendID string
	}

	resultChan := make(chan result, len(backends))
	var wg sync.WaitGroup

	for _, backend_iter := range backends {
		wg.Add(1)
		go func(b *backend.Backend) {
			defer wg.Done()
			
			start := time.Now()
			response := f.performHeadBucket(ctx, req, b)
			latency := time.Since(start)

			//f.metrics.ObserveBackendRequestLatency(backend.ID, "head_bucket", latency)
			bytesRead := int64(0)
			if response.Body != nil {
    			if counter, ok := response.Body.(*bytesCountingReader); ok {
        			bytesRead = counter.totalRead
    			}
			}

			if response.Error == nil && response.StatusCode == http.StatusOK {
				//f.metrics.IncrementBackendRequestsTotal(backend.ID, "head_bucket", "success")
				f.backendProvider.ReportSuccess(& backend.BackendResult{
					BackendID:    b.ID,
					Method:       "HEAD",
					Response:     response,
					StatusCode:   response.StatusCode,
					Err:          response.Error,
					Duration:     latency,
					BytesRead: 	  bytesRead,
				})
				
				select {
				case resultChan <- result{response: response, backendID: b.ID}:
				case <-ctx.Done():
				}
			} else {
				//f.metrics.IncrementBackendRequestsTotal(backend.ID, "head_bucket", "error")
				f.backendProvider.ReportFailure(& backend.BackendResult{
					BackendID:    b.ID,
					Method:       "HEAD",
					Response:     response,
					StatusCode:   response.StatusCode,
					Err:          response.Error,
					Duration:     latency,
					BytesRead: 	  bytesRead,
				})
			}
		}(backend_iter)
	}

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	for res := range resultChan {
		cancel()
		return res.response
	}

	return &apigw.S3Response{
		StatusCode: http.StatusNotFound,
		Headers:    make(http.Header),
		Error:      fmt.Errorf("bucket not found on any backend"),
	}
}

// performGetObject выполняет GET запрос к конкретному бэкенду
func (f *Fetcher) performGetObject(ctx context.Context, req *apigw.S3Request, backend *backend.Backend) *apigw.S3Response {
	input := &s3.GetObjectInput{
		Bucket: aws.String(backend.Config.Bucket),
		Key:    aws.String(req.Key),
	}

	result, err := backend.S3Client.GetObject(ctx, input)
	if err != nil {
		return &apigw.S3Response{
			StatusCode: http.StatusInternalServerError,
			Headers:    make(http.Header),
			Error:      err,
		}
	}

	headers := make(http.Header)
	if result.ContentType != nil {
		headers.Set("Content-Type", *result.ContentType)
	}
	if result.ContentLength != nil {
		headers.Set("Content-Length", fmt.Sprintf("%d", *result.ContentLength))
	}
	if result.LastModified != nil {
		headers.Set("Last-Modified", result.LastModified.Format(time.RFC1123))
	}
	if result.ETag != nil {
		headers.Set("ETag", *result.ETag)
	}

	// Оборачиваем тело ответа в счетчик байт
	body := &bytesCountingReader{
		reader:    result.Body,
		backendID: backend.ID,
		//metrics:   f.metrics,
	}

	return &apigw.S3Response{
		StatusCode: http.StatusOK,
		Headers:    headers,
		Body:       body,
	}
}

// performHeadObject выполняет HEAD запрос к конкретному бэкенду
func (f *Fetcher) performHeadObject(ctx context.Context, req *apigw.S3Request, backend *backend.Backend) *apigw.S3Response {
	input := &s3.HeadObjectInput{
		Bucket: aws.String(backend.Config.Bucket),
		Key:    aws.String(req.Key),
	}

	result, err := backend.S3Client.HeadObject(ctx, input)
	if err != nil {
		return &apigw.S3Response{
			StatusCode: http.StatusInternalServerError,
			Headers:    make(http.Header),
			Error:      err,
		}
	}

	headers := make(http.Header)
	if result.ContentType != nil {
		headers.Set("Content-Type", *result.ContentType)
	}
	if result.ContentLength != nil {
		headers.Set("Content-Length", fmt.Sprintf("%d", *result.ContentLength))
	}
	if result.LastModified != nil {
		headers.Set("Last-Modified", result.LastModified.Format(time.RFC1123))
	}
	if result.ETag != nil {
		headers.Set("ETag", *result.ETag)
	}

	return &apigw.S3Response{
		StatusCode: http.StatusOK,
		Headers:    headers,
		Body:       nil,
	}
}

// performHeadBucket выполняет HEAD Bucket запрос к конкретному бэкенду
func (f *Fetcher) performHeadBucket(ctx context.Context, req *apigw.S3Request, backend *backend.Backend) *apigw.S3Response {
	input := &s3.HeadBucketInput{
		Bucket: aws.String(backend.Config.Bucket),
	}

	_, err := backend.S3Client.HeadBucket(ctx, input)
	if err != nil {
		return &apigw.S3Response{
			StatusCode: http.StatusInternalServerError,
			Headers:    make(http.Header),
			Error:      err,
		}
	}

	return &apigw.S3Response{
		StatusCode: http.StatusOK,
		Headers:    make(http.Header),
		Body:       nil,
	}
}

// bytesCountingReader оборачивает io.ReadCloser для подсчета прочитанных байт
type bytesCountingReader struct {
	reader    io.ReadCloser
	backendID string
	//metrics   Metrics
	totalRead int64
}

func (b *bytesCountingReader) Read(p []byte) (n int, err error) {
	n, err = b.reader.Read(p)
	b.totalRead += int64(n)
	return n, err
}

func (b *bytesCountingReader) Close() error {
	// Записываем метрику при закрытии
	//b.metrics.AddBackendBytesRead(b.backendID, b.totalRead)
	return b.reader.Close()
}
