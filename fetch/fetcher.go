package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"

	"s3proxy/apigw"
	"s3proxy/backend"
	"s3proxy/routing"
)

// backendOperation - это тип для функций, выполняющих конкретную S3 операцию на бэкенде.
// Это ключ к рефакторингу, позволяющий передавать `performGetObject`, `performHeadObject` и т.д. как аргументы.
type backendOperation func(context.Context, *apigw.S3Request, *backend.Backend) *apigw.S3Response

// Fetcher реализует интерфейс FetchingExecutor
type Fetcher struct {
	backendProvider *backend.Manager
	cache           Cache
	virtualBucket   string
}

// NewFetcher создает новый экземпляр Fetcher
func NewFetcher(provider *backend.Manager, cache Cache, virtualBucket string) *Fetcher {
	return &Fetcher{
		backendProvider: provider,
		cache:           cache,
		virtualBucket:   virtualBucket,
	}
}

// --- Публичные методы-диспетчеры ---

func (f *Fetcher) GetObject(ctx context.Context, req *apigw.S3Request, policy routing.ReadOperationPolicy) *apigw.S3Response {
	if response, found := f.cache.Get(req.Bucket, req.Key); found {
		return response
	}
	backends := f.backendProvider.GetLiveBackends()
	if len(backends) == 0 {
		return f.noBackendsResponse()
	}

	switch policy.Strategy {
	case "first":
		return f.executeFirst(ctx, req, backends, f.performGetObject, "GET", "object not found on any backend")
	case "newest":
		return f.executeNewest(ctx, req, backends, true) // true -> выполнить GET после HEAD
	default:
		return f.unknownStrategyResponse(policy.Strategy)
	}
}

func (f *Fetcher) HeadObject(ctx context.Context, req *apigw.S3Request, policy routing.ReadOperationPolicy) *apigw.S3Response {
	if response, found := f.cache.Get(req.Bucket, req.Key); found {
		response.Body = nil // Убираем тело для HEAD
		return response
	}
	backends := f.backendProvider.GetLiveBackends()
	if len(backends) == 0 {
		return f.noBackendsResponse()
	}

	switch policy.Strategy {
	case "first":
		return f.executeFirst(ctx, req, backends, f.performHeadObject, "HEAD", "object not found on any backend")
	case "newest":
		return f.executeNewest(ctx, req, backends, false) // false -> не выполнять GET, вернуть результат HEAD
	default:
		return f.unknownStrategyResponse(policy.Strategy)
	}
}

func (f *Fetcher) HeadBucket(ctx context.Context, req *apigw.S3Request) *apigw.S3Response {
	backends := f.backendProvider.GetLiveBackends()
	if len(backends) == 0 {
		return f.noBackendsResponse()
	}
	return f.executeFirst(ctx, req, backends, f.performHeadBucket, "HEAD_BUCKET", "bucket not found on any backend")
}

// ... другие методы List* можно отрефакторить аналогично, если они имеют схожие стратегии ...
// (Оставляю их как есть для краткости, так как они не были причиной паники)
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

func (f *Fetcher) ListMultipartUploads(ctx context.Context, req *apigw.S3Request) *apigw.S3Response { /* ... */
	return nil
}

// --- Универсальные исполнители стратегий ---

// executeFirst выполняет операцию op на всех бэкендах параллельно и возвращает первый успешный результат.
// ВАЖНО: Эта версия НЕ отменяет остальные запросы, позволяя им завершиться для сбора
// статистики (пассивного health-check'а).
func (f *Fetcher) executeFirst(ctx context.Context, req *apigw.S3Request, backends []*backend.Backend, op backendOperation, methodName, notFoundMsg string) *apigw.S3Response {
	// НЕ создаем context.WithCancel, чтобы все запросы могли завершиться.
	
	// Буферизованный канал критически важен, чтобы предотвратить утечку горутин.
	// Медленные горутины смогут записать результат и завершиться.
	resultChan := make(chan *apigw.S3Response, len(backends))
	var wg sync.WaitGroup

	for _, be := range backends {
		wg.Add(1)
		go func(b *backend.Backend) {
			defer wg.Done()
			start := time.Now()
			
			// Передаем родительский контекст `ctx` без изменений.
			response := op(ctx, req, b)
			latency := time.Since(start)

			var bytesRead int64
			if counter, ok := response.Body.(*bytesCountingReader); ok && counter != nil {
				bytesRead = counter.totalRead
			}

			isSuccess := response.Error == nil && response.StatusCode >= 200 && response.StatusCode < 300
			if isSuccess {
				f.backendProvider.ReportSuccess(&backend.BackendResult{
					BackendID: b.ID, Method: methodName, StatusCode: response.StatusCode, Duration: latency, BytesRead: bytesRead,
				})
				// Просто отправляем результат. Так как канал буферизованный, это не заблокирует горутину.
				resultChan <- response
			} else {
				f.backendProvider.ReportFailure(&backend.BackendResult{
					BackendID: b.ID, Method: methodName, StatusCode: response.StatusCode, Err: response.Error, Duration: latency, BytesRead: bytesRead,
				})
			}
		}(be)
	}

	// Эта горутина нужна только для того, чтобы закрыть канал после завершения всех воркеров.
	// Это гарантирует, что `<-resultChan` не будет блокироваться вечно, если все запросы провалятся.
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Ждем первый успешный ответ из канала.
	if res := <-resultChan; res != nil {
		// Мы получили самый быстрый ответ.
		// НЕ вызываем cancel(), а просто возвращаем его.
		// Остальные горутины продолжат работать в фоне и отправлять отчеты.
		return res
	}

	// Сюда мы попадем, только если канал был закрыт и в нем не было ни одного успешного ответа.
	return &apigw.S3Response{StatusCode: http.StatusNotFound, Error: errors.New(notFoundMsg)}
}

// executeNewest находит самый новый объект среди всех бэкендов и либо возвращает его (performGet=true),
// либо возвращает результат HEAD запроса к нему (performGet=false).
func (f *Fetcher) executeNewest(ctx context.Context, req *apigw.S3Request, backends []*backend.Backend, performGet bool) *apigw.S3Response {
	type headResult struct {
		response     *apigw.S3Response
		backend      *backend.Backend
		lastModified time.Time
	}

	resultsChan := make(chan headResult, len(backends))
	var wg sync.WaitGroup

	// Фаза 1: HEAD запросы ко всем бэкендам
	for _, be := range backends {
		wg.Add(1)
		go func(b *backend.Backend) {
			defer wg.Done()
			response := f.performHeadObject(ctx, req, b)
			if response.Error == nil && response.StatusCode == http.StatusOK {
				lastModified, _ := time.Parse(time.RFC1123, response.Headers.Get("Last-Modified"))
				resultsChan <- headResult{response: response, backend: b, lastModified: lastModified}
			}
		}(be)
	}

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Фаза 2: Находим самый новый объект
	var newest *headResult
	for result := range resultsChan {
		if newest == nil || result.lastModified.After(newest.lastModified) {
			resCopy := result // Копируем, чтобы избежать проблем с замыканием
			newest = &resCopy
		}
	}

	if newest == nil {
		return &apigw.S3Response{StatusCode: http.StatusNotFound, Error: fmt.Errorf("object not found on any backend")}
	}

	// Фаза 3: Выполняем GET (если нужно) или возвращаем результат HEAD
	if performGet {
		return f.performGetObject(ctx, req, newest.backend)
	}
	return newest.response
}

// --- Функции для выполнения конкретных S3 операций ---

func (f *Fetcher) performGetObject(ctx context.Context, req *apigw.S3Request, backend *backend.Backend) *apigw.S3Response {
	input := &s3.GetObjectInput{Bucket: aws.String(backend.Config.Bucket), Key: aws.String(req.Key)}
	result, err := backend.S3Client.GetObject(ctx, input)
	if err != nil {
		return f.handleS3Error(err)
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
		Body:       &bytesCountingReader{reader: result.Body},
	}
}

func (f *Fetcher) performHeadObject(ctx context.Context, req *apigw.S3Request, backend *backend.Backend) *apigw.S3Response {
	input := &s3.HeadObjectInput{Bucket: aws.String(backend.Config.Bucket), Key: aws.String(req.Key)}
	result, err := backend.S3Client.HeadObject(ctx, input)
	if err != nil {
		return f.handleS3Error(err)
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

	return &apigw.S3Response{StatusCode: http.StatusOK, Headers: headers}
}

func (f *Fetcher) performHeadBucket(ctx context.Context, req *apigw.S3Request, backend *backend.Backend) *apigw.S3Response {
	input := &s3.HeadBucketInput{Bucket: aws.String(backend.Config.Bucket)}
	_, err := backend.S3Client.HeadBucket(ctx, input)
	if err != nil {
		return f.handleS3Error(err)
	}
	return &apigw.S3Response{StatusCode: http.StatusOK}
}

// --- Вспомогательные функции ---

func (f *Fetcher) handleS3Error(err error) *apigw.S3Response {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NotFound", "NoSuchKey", "NoSuchBucket":
			return &apigw.S3Response{StatusCode: http.StatusNotFound, Error: err}
		}
		// Можно добавить другие коды ошибок S3
	}
	return &apigw.S3Response{StatusCode: http.StatusInternalServerError, Error: err}
}

func (f *Fetcher) noBackendsResponse() *apigw.S3Response {
	return &apigw.S3Response{StatusCode: http.StatusServiceUnavailable, Error: fmt.Errorf("no live backends available")}
}

func (f *Fetcher) unknownStrategyResponse(strategy string) *apigw.S3Response {
	return &apigw.S3Response{StatusCode: http.StatusInternalServerError, Error: fmt.Errorf("unknown read strategy: %s", strategy)}
}

// bytesCountingReader оборачивает io.ReadCloser для подсчета прочитанных байт
type bytesCountingReader struct {
	reader    io.ReadCloser
	totalRead int64
}

func (b *bytesCountingReader) Read(p []byte) (n int, err error) {
	n, err = b.reader.Read(p)
	b.totalRead += int64(n)
	return n, err
}

func (b *bytesCountingReader) Close() error {
	return b.reader.Close()
}
