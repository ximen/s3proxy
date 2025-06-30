package apigw

import (
	"context"
	"io"
	"net/http"
	"net/url"
)

// S3Operation определяет тип S3 операции.
type S3Operation int

const (
	// Определяем константы для всех поддерживаемых операций
	UnsupportedOperation S3Operation = iota
	PutObject
	GetObject
	HeadObject
	HeadBucket
	DeleteObject
	ListObjectsV2
	CreateMultipartUpload
	UploadPart
	CompleteMultipartUpload
	AbortMultipartUpload
	ListMultipartUploads
	ListBuckets
)

// String возвращает строковое представление операции
func (op S3Operation) String() string {
	switch op {
	case PutObject:
		return "PUT_OBJECT"
	case GetObject:
		return "GET_OBJECT"
	case HeadObject:
		return "HEAD_OBJECT"
	case HeadBucket:
		return "HEAD_BUCKET"
	case DeleteObject:
		return "DELETE_OBJECT"
	case ListObjectsV2:
		return "LIST_OBJECTS_V2"
	case CreateMultipartUpload:
		return "CREATE_MULTIPART_UPLOAD"
	case UploadPart:
		return "UPLOAD_PART"
	case CompleteMultipartUpload:
		return "COMPLETE_MULTIPART_UPLOAD"
	case AbortMultipartUpload:
		return "ABORT_MULTIPART_UPLOAD"
	case ListMultipartUploads:
		return "LIST_MULTIPART_UPLOADS"
	case ListBuckets:
		return "LIST_BUCKETS"
	default:
		return "UNSUPPORTED_OPERATION"
	}
}

// S3Request - это стандартизированное внутреннее представление S3-запроса.
// Создается модулем API Gateway из http.Request.
type S3Request struct {
	// Тип операции, определенный парсером.
	Operation S3Operation

	// Имя бакета, извлеченное из URL.
	Bucket string

	// Ключ объекта, извлеченный из URL.
	Key string

	// Хост из заголовка Host (для аутентификации)
	Host string

	// Схема запроса (http или https)
	Scheme string

	// Оригинальные заголовки HTTP запроса.
	Headers http.Header

	// Оригинальные query-параметры запроса.
	Query url.Values

	// Тело запроса для операций PUT, POST.
	// Передается как есть для потоковой обработки.
	Body io.ReadCloser

	// Размер тела запроса, из заголовка Content-Length.
	ContentLength int64

	// Оригинальный контекст запроса для поддержки таймаутов и отмены.
	Context context.Context
}

// S3Response - это стандартизированное внутреннее представление ответа.
// Формируется нижележащими модулями и используется API Gateway для отправки ответа.
type S3Response struct {
	// HTTP код состояния для отправки клиенту (например, 200, 404, 500).
	StatusCode int

	// Заголовки для отправки клиенту.
	Headers http.Header

	// Тело ответа для отправки клиенту.
	// Должно быть потоком для эффективной передачи больших объектов.
	Body io.ReadCloser

	// Ошибка, возникшая при обработке. Если не nil, Body игнорируется.
	// Используется для формирования стандартного S3 XML-ответа об ошибке.
	Error error
}

// RequestHandler - это интерфейс, который должен реализовывать
// следующий по цепочке модуль (Policy & Routing Engine).
type RequestHandler interface {
	// Handle принимает распарсенный S3Request и выполняет всю бизнес-логику,
	// возвращая S3Response, готовый для отправки клиенту.
	Handle(req *S3Request) *S3Response
}
