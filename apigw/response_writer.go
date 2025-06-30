package apigw

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"

	"s3proxy/logger"
)

// ResponseWriter отвечает за формирование HTTP ответов из S3Response
type ResponseWriter struct{}

// NewResponseWriter создает новый экземпляр writer'а ответов
func NewResponseWriter() *ResponseWriter {
	return &ResponseWriter{}
}

// WriteResponse записывает S3Response в http.ResponseWriter
func (rw *ResponseWriter) WriteResponse(w http.ResponseWriter, s3resp *S3Response) error {
	logger.Debug("Writing response: status=%d, hasBody=%t, hasError=%t", 
		s3resp.StatusCode, s3resp.Body != nil, s3resp.Error != nil)
	
	// Если есть ошибка, формируем XML ответ об ошибке
	if s3resp.Error != nil {
		logger.Debug("Writing error response: %v", s3resp.Error)
		return rw.writeErrorResponse(w, s3resp.Error)
	}

	// Копируем заголовки
	if s3resp.Headers != nil {
		logger.Debug("Setting response headers: %+v", s3resp.Headers)
		for key, values := range s3resp.Headers {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
	}

	// Устанавливаем код ответа
	w.WriteHeader(s3resp.StatusCode)
	logger.Debug("Set response status code: %d", s3resp.StatusCode)

	// Записываем тело ответа, если оно есть
	if s3resp.Body != nil {
		defer s3resp.Body.Close()
		logger.Debug("Writing response body")
		_, err := io.Copy(w, s3resp.Body)
		if err != nil {
			logger.Debug("Error writing response body: %v", err)
		}
		return err
	}

	logger.Debug("Response written successfully")
	return nil
}

// writeErrorResponse записывает стандартный S3 XML ответ об ошибке
func (rw *ResponseWriter) writeErrorResponse(w http.ResponseWriter, err error) error {
	logger.Debug("Writing error response for error: %v", err)
	
	// Определяем код ошибки и HTTP статус на основе типа ошибки
	errorCode, httpStatus := rw.mapErrorToS3Error(err)
	logger.Debug("Mapped error to S3 error: code=%s, status=%d", errorCode, httpStatus)

	// Создаем XML структуру ошибки
	s3Error := S3Error{
		Code:    errorCode,
		Message: err.Error(),
	}

	// Маршалим в XML
	xmlData, xmlErr := xml.Marshal(s3Error)
	if xmlErr != nil {
		// Если не можем создать XML, отправляем простой текстовый ответ
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return xmlErr
	}

	// Устанавливаем заголовки
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(xmlData)))

	// Устанавливаем код ответа
	w.WriteHeader(httpStatus)

	// Записываем XML
	_, writeErr := w.Write(xmlData)
	return writeErr
}

// mapErrorToS3Error сопоставляет Go ошибки с S3 кодами ошибок
func (rw *ResponseWriter) mapErrorToS3Error(err error) (string, int) {
	errMsg := strings.ToLower(err.Error())

	switch {
	case strings.Contains(errMsg, "bucket") && strings.Contains(errMsg, "not found"):
		return "NoSuchBucket", http.StatusNotFound
	case strings.Contains(errMsg, "not found"):
		return "NoSuchKey", http.StatusNotFound
	case strings.Contains(errMsg, "access denied"):
		return "AccessDenied", http.StatusForbidden
	case strings.Contains(errMsg, "invalid"):
		return "InvalidRequest", http.StatusBadRequest
	case strings.Contains(errMsg, "unsupported"):
		return "NotImplemented", http.StatusNotImplemented
	case strings.Contains(errMsg, "bucket"):
		return "BucketError", http.StatusBadRequest
	default:
		return "InternalError", http.StatusInternalServerError
	}
}

// S3Error представляет структуру XML ошибки S3
type S3Error struct {
	XMLName xml.Name `xml:"Error"`
	Code    string   `xml:"Code"`
	Message string   `xml:"Message"`
}
