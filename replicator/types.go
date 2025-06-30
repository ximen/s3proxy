package replicator

import (
	"context"
	"io"
	"time"

	"s3proxy/apigw"
	"s3proxy/backend"

	//"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Backend представляет один S3-бэкенд (локальная копия для избежания циклических зависимостей)
// type Backend struct {
// 	ID                 string
// 	S3Client           *s3.Client
// 	Config             backend.BackendConfig
// 	StreamingPutClient *s3.Client
// }

// BackendProvider интерфейс для получения бэкендов
type BackendProvider interface {
	GetLiveBackends() []*backend.Backend
	ReportSuccess(backendID string)
	ReportFailure(backendID string, err error)
}

// // backendResult представляет результат операции на одном бэкенде
// type backendResult struct {
// 	backendID    string
// 	response     interface{} // Ответ от AWS SDK (может быть разных типов)
// 	err          error
// 	duration     time.Duration
// 	bytesWritten int64
// }

// multipartUploadMapping хранит маппинг ProxyUploadId -> backend uploadIds
type multipartUploadMapping struct {
	ProxyUploadID  string
	BackendUploads map[string]string // backendID -> uploadID
	CreatedAt      time.Time
	Bucket         string
	Key            string
}

// ReaderCloner интерфейс для клонирования io.Reader
type ReaderCloner interface {
	Clone(reader io.Reader, count int) ([]io.Reader, error)
}

// PipeReaderCloner реализует клонирование через io.Pipe
type PipeReaderCloner struct{}

// Clone создает несколько копий io.Reader для параллельной отправки
func (c *PipeReaderCloner) Clone(reader io.Reader, count int) ([]io.Reader, error) {
	if count <= 0 {
		return nil, nil
	}

	if count == 1 {
		return []io.Reader{reader}, nil
	}

	// Создаем pipes для каждого бэкенда
	pipes := make([]*io.PipeWriter, count)
	readers := make([]io.Reader, count)

	for i := 0; i < count; i++ {
		r, w := io.Pipe()
		pipes[i] = w
		readers[i] = r
	}

	// Запускаем горутину для копирования данных во все pipes
	go func() {
		defer func() {
			for _, pipe := range pipes {
				pipe.Close()
			}
		}()

		// Создаем MultiWriter для записи во все pipes одновременно
		multiWriter := io.MultiWriter(func() []io.Writer {
			writers := make([]io.Writer, len(pipes))
			for i, pipe := range pipes {
				writers[i] = pipe
			}
			return writers
		}()...)

		// Копируем данные из исходного reader во все pipes
		_, err := io.Copy(multiWriter, reader)
		if err != nil {
			// Закрываем все pipes с ошибкой
			for _, pipe := range pipes {
				pipe.CloseWithError(err)
			}
		}
	}()

	return readers, nil
}

// CountingReader оборачивает io.Reader и считает прочитанные байты
type CountingReader struct {
	reader io.Reader
	count  int64
}

// NewCountingReader создает новый CountingReader
func NewCountingReader(reader io.Reader) *CountingReader {
	return &CountingReader{reader: reader}
}

// Read реализует io.Reader и считает байты
func (cr *CountingReader) Read(p []byte) (n int, err error) {
	n, err = cr.reader.Read(p)
	cr.count += int64(n)
	return n, err
}

// Count возвращает количество прочитанных байт
func (cr *CountingReader) Count() int64 {
	return cr.count
}

// operationContext содержит контекст для выполнения операции
type operationContext struct {
	ctx       context.Context
	operation string
	bucket    string
	key       string
	startTime time.Time
}

// newOperationContext создает новый контекст операции
func newOperationContext(ctx context.Context, operation, bucket, key string) *operationContext {
	return &operationContext{
		ctx:       ctx,
		operation: operation,
		bucket:    bucket,
		key:       key,
		startTime: time.Now(),
	}
}

// Duration возвращает время выполнения операции
func (oc *operationContext) Duration() time.Duration {
	return time.Since(oc.startTime)
}

// BackendOperation представляет операцию, выполняемую на бэкенде
type BackendOperation func(ctx context.Context, backend *backend.Backend, req *apigw.S3Request, body io.Reader) *backend.BackendResult
