package apigw

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"s3proxy/logger"
)

// RequestParser отвечает за парсинг HTTP запросов в S3Request
type RequestParser struct{}

// NewRequestParser создает новый экземпляр парсера
func NewRequestParser() *RequestParser {
	return &RequestParser{}
}

// Parse анализирует HTTP запрос и создает S3Request
func (p *RequestParser) Parse(r *http.Request) (*S3Request, error) {
	logger.Debug("Parsing HTTP request: %s %s", r.Method, r.URL.Path)
	logger.Debug("Query parameters: %v", r.URL.Query())
	logger.Debug("Request headers: %+v", r.Header)

	// Определяем схему из запроса
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	// Также проверяем заголовки для случаев с прокси/балансировщиками
	if r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}

	logger.Debug("Determined scheme: %s", scheme)

	s3req := &S3Request{
		Host:      r.Host,
		Scheme:    scheme,
		Headers:   r.Header.Clone(),
		Query:     r.URL.Query(),
		Body:      r.Body,
		Context:   r.Context(),
	}

	// Извлекаем Content-Length
	if contentLengthStr := r.Header.Get("Content-Length"); contentLengthStr != "" {
		if contentLength, err := strconv.ParseInt(contentLengthStr, 10, 64); err == nil {
			s3req.ContentLength = contentLength
			logger.Debug("Content-Length: %d", contentLength)
		}
	}

	// Парсим путь для извлечения bucket и key
	if err := p.parsePath(r.URL.Path, s3req); err != nil {
		logger.Debug("Failed to parse path: %v", err)
		return nil, err
	}

	logger.Debug("Parsed path - Bucket: %s, Key: %s", s3req.Bucket, s3req.Key)

	// Определяем операцию на основе метода и query параметров
	if err := p.determineOperation(r.Method, s3req); err != nil {
		logger.Debug("Failed to determine operation: %v", err)
		return nil, err
	}

	logger.Debug("Determined operation: %s", s3req.Operation.String())
	logger.Debug("Created S3Request: %+v", s3req)
	return s3req, nil
}

// parsePath извлекает bucket и key из пути URL
func (p *RequestParser) parsePath(path string, s3req *S3Request) error {
	// Убираем ведущий слеш
	path = strings.TrimPrefix(path, "/")
	
	// Если путь пустой, это запрос списка бакетов
	if path == "" {
		return nil
	}

	// Разделяем путь на части
	parts := strings.SplitN(path, "/", 2)
	
	// Первая часть - это всегда bucket
	s3req.Bucket = parts[0]
	
	// Если есть вторая часть, это key
	if len(parts) > 1 {
		s3req.Key = parts[1]
	}

	return nil
}

// determineOperation определяет тип S3 операции на основе HTTP метода и query параметров
func (p *RequestParser) determineOperation(method string, s3req *S3Request) error {
	query := s3req.Query

	switch method {
	case "GET":
		return p.determineGetOperation(s3req, query)
	case "PUT":
		return p.determinePutOperation(s3req, query)
	case "POST":
		return p.determinePostOperation(s3req, query)
	case "DELETE":
		return p.determineDeleteOperation(s3req, query)
	case "HEAD":
		return p.determineHeadOperation(s3req, query)
	default:
		s3req.Operation = UnsupportedOperation
		return fmt.Errorf("unsupported HTTP method: %s", method)
	}
}

// determineGetOperation определяет GET операции
func (p *RequestParser) determineGetOperation(s3req *S3Request, query map[string][]string) error {
	// Проверяем специальные query параметры
	if _, hasUploads := query["uploads"]; hasUploads {
		s3req.Operation = ListMultipartUploads
		return nil
	}

	// Если нет bucket, это список бакетов
	if s3req.Bucket == "" {
		s3req.Operation = ListBuckets
		return nil
	}

	// Если нет key или key заканчивается на "/", это список объектов
	if s3req.Key == "" || strings.HasSuffix(s3req.Key, "/") {
		s3req.Operation = ListObjectsV2
		return nil
	}

	// Иначе это получение объекта
	s3req.Operation = GetObject
	return nil
}

// determinePutOperation определяет PUT операции
func (p *RequestParser) determinePutOperation(s3req *S3Request, query map[string][]string) error {
	// Проверяем multipart upload параметры
	if partNumber, hasPartNumber := query["partNumber"]; hasPartNumber {
		if uploadId, hasUploadId := query["uploadId"]; hasUploadId {
			if len(partNumber) > 0 && len(uploadId) > 0 {
				s3req.Operation = UploadPart
				return nil
			}
		}
	}

	// Обычная загрузка объекта
	if s3req.Bucket != "" && s3req.Key != "" {
		s3req.Operation = PutObject
		return nil
	}

	s3req.Operation = UnsupportedOperation
	return fmt.Errorf("unsupported PUT operation")
}

// determinePostOperation определяет POST операции
func (p *RequestParser) determinePostOperation(s3req *S3Request, query map[string][]string) error {
	// Инициация multipart upload
	if _, hasUploads := query["uploads"]; hasUploads {
		s3req.Operation = CreateMultipartUpload
		return nil
	}

	// Завершение multipart upload
	if uploadId, hasUploadId := query["uploadId"]; hasUploadId {
		if len(uploadId) > 0 {
			s3req.Operation = CompleteMultipartUpload
			return nil
		}
	}

	s3req.Operation = UnsupportedOperation
	return fmt.Errorf("unsupported POST operation")
}

// determineDeleteOperation определяет DELETE операции
func (p *RequestParser) determineDeleteOperation(s3req *S3Request, query map[string][]string) error {
	// Отмена multipart upload
	if uploadId, hasUploadId := query["uploadId"]; hasUploadId {
		if len(uploadId) > 0 {
			s3req.Operation = AbortMultipartUpload
			return nil
		}
	}

	// Удаление объекта
	if s3req.Bucket != "" && s3req.Key != "" {
		s3req.Operation = DeleteObject
		return nil
	}

	s3req.Operation = UnsupportedOperation
	return fmt.Errorf("unsupported DELETE operation")
}

// determineHeadOperation определяет HEAD операции
func (p *RequestParser) determineHeadOperation(s3req *S3Request, query map[string][]string) error {
	// HEAD для объекта (bucket + key)
	if s3req.Bucket != "" && s3req.Key != "" {
		s3req.Operation = HeadObject
		return nil
	}
	
	// HEAD для бакета (только bucket, без key)
	if s3req.Bucket != "" && s3req.Key == "" {
		s3req.Operation = HeadBucket
		return nil
	}

	s3req.Operation = UnsupportedOperation
	return fmt.Errorf("unsupported HEAD operation")
}
