package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"s3proxy/apigw"
	"s3proxy/handlers"
)

func TestAPIGateway_Integration(t *testing.T) {
	// Создаем конфигурацию для тестов
	config := apigw.Config{
		ListenAddress: ":0", // Случайный порт
		ReadTimeout:   5 * time.Second,
		WriteTimeout:  5 * time.Second,
	}

	// Создаем тестовый обработчик
	handler := handlers.NewMockHandler()

	// Создаем API Gateway
	gateway := apigw.New(config, handler)

	tests := []struct {
		name           string
		method         string
		path           string
		query          string
		body           string
		expectedStatus int
		expectedBody   string
		checkHeaders   map[string]string
	}{
		{
			name:           "GET object",
			method:         "GET",
			path:           "/test-bucket/test-object.txt",
			expectedStatus: http.StatusOK,
			expectedBody:   "Mock content for object test-bucket/test-object.txt",
			checkHeaders: map[string]string{
				"Content-Type": "text/plain",
				"ETag":        `"mock-etag-12345"`,
			},
		},
		{
			name:           "PUT object",
			method:         "PUT",
			path:           "/test-bucket/test-object.txt",
			body:           "test content",
			expectedStatus: http.StatusOK,
			checkHeaders: map[string]string{
				"ETag": `"mock-etag-67890"`,
			},
		},
		{
			name:           "HEAD object",
			method:         "HEAD",
			path:           "/test-bucket/test-object.txt",
			expectedStatus: http.StatusOK,
			checkHeaders: map[string]string{
				"Content-Type":   "text/plain",
				"Content-Length": "100",
				"ETag":           `"mock-etag-12345"`,
			},
		},
		{
			name:           "HEAD bucket",
			method:         "HEAD",
			path:           "/test-bucket/",
			expectedStatus: http.StatusOK,
			checkHeaders: map[string]string{
				"x-amz-bucket-region": "us-east-1",
			},
		},
		{
			name:           "DELETE object",
			method:         "DELETE",
			path:           "/test-bucket/test-object.txt",
			expectedStatus: http.StatusNoContent,
		},
		{
			name:           "List objects",
			method:         "GET",
			path:           "/test-bucket/",
			expectedStatus: http.StatusOK,
			expectedBody:   "test-bucket", // Проверяем, что имя бакета есть в ответе
			checkHeaders: map[string]string{
				"Content-Type": "application/xml",
			},
		},
		{
			name:           "List buckets",
			method:         "GET",
			path:           "/",
			expectedStatus: http.StatusOK,
			expectedBody:   "ListAllMyBucketsResult", // Проверяем XML структуру
			checkHeaders: map[string]string{
				"Content-Type": "application/xml",
			},
		},
		{
			name:           "Create multipart upload",
			method:         "POST",
			path:           "/test-bucket/test-object.txt",
			query:          "uploads",
			expectedStatus: http.StatusOK,
			expectedBody:   "InitiateMultipartUploadResult",
			checkHeaders: map[string]string{
				"Content-Type": "application/xml",
			},
		},
		{
			name:           "Upload part",
			method:         "PUT",
			path:           "/test-bucket/test-object.txt",
			query:          "partNumber=1&uploadId=test-upload-id",
			body:           "part content",
			expectedStatus: http.StatusOK,
			checkHeaders: map[string]string{
				"ETag": `"mock-part-etag-12345"`,
			},
		},
		{
			name:           "Complete multipart upload",
			method:         "POST",
			path:           "/test-bucket/test-object.txt",
			query:          "uploadId=test-upload-id",
			body:           "<CompleteMultipartUpload></CompleteMultipartUpload>",
			expectedStatus: http.StatusOK,
			expectedBody:   "CompleteMultipartUploadResult",
			checkHeaders: map[string]string{
				"Content-Type": "application/xml",
			},
		},
		{
			name:           "Abort multipart upload",
			method:         "DELETE",
			path:           "/test-bucket/test-object.txt",
			query:          "uploadId=test-upload-id",
			expectedStatus: http.StatusNoContent,
		},
		{
			name:           "List multipart uploads",
			method:         "GET",
			path:           "/test-bucket/",
			query:          "uploads",
			expectedStatus: http.StatusOK,
			expectedBody:   "ListMultipartUploadsResult",
			checkHeaders: map[string]string{
				"Content-Type": "application/xml",
			},
		},
		{
			name:           "Unsupported method",
			method:         "PATCH",
			path:           "/test-bucket/test-object.txt",
			expectedStatus: http.StatusBadRequest,
			expectedBody:   "Error", // Проверяем XML ошибку
		},
		{
			name:           "Invalid path",
			method:         "GET",
			path:           "",
			expectedStatus: http.StatusOK, // Это будет список бакетов
			expectedBody:   "ListAllMyBucketsResult",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Создаем запрос
			var body io.Reader
			if tt.body != "" {
				body = strings.NewReader(tt.body)
			}

			url := "http://example.com" + tt.path
			if tt.query != "" {
				url += "?" + tt.query
			}

			req := httptest.NewRequest(tt.method, url, body)
			if tt.body != "" {
				req.Header.Set("Content-Length", string(rune(len(tt.body))))
			}

			// Создаем ResponseRecorder
			w := httptest.NewRecorder()

			// Выполняем запрос
			gateway.ServeHTTP(w, req)

			// Проверяем статус код
			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			// Проверяем тело ответа
			if tt.expectedBody != "" {
				responseBody := w.Body.String()
				if !strings.Contains(responseBody, tt.expectedBody) {
					t.Errorf("Expected body to contain %q, got %q", tt.expectedBody, responseBody)
				}
			}

			// Проверяем заголовки
			for header, expectedValue := range tt.checkHeaders {
				actualValue := w.Header().Get(header)
				if actualValue != expectedValue {
					t.Errorf("Expected header %s to be %q, got %q", header, expectedValue, actualValue)
				}
			}
		})
	}
}

func TestAPIGateway_ErrorHandling(t *testing.T) {
	config := apigw.Config{
		ListenAddress: ":0",
		ReadTimeout:   5 * time.Second,
		WriteTimeout:  5 * time.Second,
	}

	// Создаем обработчик, который всегда возвращает ошибку
	errorHandler := &ErrorHandler{}
	gateway := apigw.New(config, errorHandler)

	req := httptest.NewRequest("GET", "http://example.com/test-bucket/test-object.txt", nil)
	w := httptest.NewRecorder()

	gateway.ServeHTTP(w, req)

	// Проверяем, что возвращается ошибка
	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}

	// Проверяем, что ответ содержит XML ошибку
	responseBody := w.Body.String()
	if !strings.Contains(responseBody, "<Error>") {
		t.Errorf("Expected XML error response, got %q", responseBody)
	}

	// Проверяем Content-Type
	contentType := w.Header().Get("Content-Type")
	if contentType != "application/xml" {
		t.Errorf("Expected Content-Type application/xml, got %q", contentType)
	}
}

// ErrorHandler - тестовый обработчик, который всегда возвращает ошибку
type ErrorHandler struct{}

func (h *ErrorHandler) Handle(req *apigw.S3Request) *apigw.S3Response {
	return &apigw.S3Response{
		StatusCode: http.StatusInternalServerError,
		Error:      errors.New("test error"),
	}
}

func TestResponseWriter_WriteErrorResponse(t *testing.T) {
	writer := apigw.NewResponseWriter()

	// Тестируем различные типы ошибок
	tests := []struct {
		name           string
		error          string
		expectedStatus int
		expectedCode   string
	}{
		{
			name:           "Not found error",
			error:          "object not found",
			expectedStatus: http.StatusNotFound,
			expectedCode:   "NoSuchKey",
		},
		{
			name:           "Access denied error",
			error:          "access denied",
			expectedStatus: http.StatusForbidden,
			expectedCode:   "AccessDenied",
		},
		{
			name:           "Invalid request error",
			error:          "invalid parameter",
			expectedStatus: http.StatusBadRequest,
			expectedCode:   "InvalidRequest",
		},
		{
			name:           "Bucket not found error",
			error:          "bucket not found",
			expectedStatus: http.StatusNotFound,
			expectedCode:   "NoSuchBucket",
		},
		{
			name:           "Generic error",
			error:          "something went wrong",
			expectedStatus: http.StatusInternalServerError,
			expectedCode:   "InternalError",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			// Создаем S3Response с ошибкой
			s3resp := &apigw.S3Response{
				StatusCode: tt.expectedStatus,
				Error:      errors.New(tt.error),
			}
			
			err := writer.WriteResponse(w, s3resp)

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			responseBody := w.Body.String()
			if !strings.Contains(responseBody, tt.expectedCode) {
				t.Errorf("Expected error code %q in response, got %q", tt.expectedCode, responseBody)
			}

			if !strings.Contains(responseBody, tt.error) {
				t.Errorf("Expected error message %q in response, got %q", tt.error, responseBody)
			}
		})
	}
}
