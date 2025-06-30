package routing

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"s3proxy/apigw"
	"s3proxy/auth"
)

// MockAuthenticator для тестирования
type MockAuthenticator struct {
	shouldFail bool
	failError  error
}

func (m *MockAuthenticator) Authenticate(req *apigw.S3Request) (*auth.UserIdentity, error) {
	if m.shouldFail {
		return nil, m.failError
	}
	return &auth.UserIdentity{
		DisplayName: "test-user",
		AccessKey:   "test-access-key",
	}, nil
}

func TestNewEngine(t *testing.T) {
	auth := &MockAuthenticator{}
	replicator := NewMockReplicationExecutor()
	fetcher := NewMockFetchingExecutor()
	
	// Тест с конфигурацией по умолчанию
	engine := NewEngine(auth, replicator, fetcher, nil)
	if engine == nil {
		t.Fatal("Expected engine to be created")
	}
	
	// Проверяем политики по умолчанию
	if engine.putPolicy.AckLevel != "one" {
		t.Errorf("Expected default put policy ack level 'one', got '%s'", engine.putPolicy.AckLevel)
	}
	
	if engine.deletePolicy.AckLevel != "all" {
		t.Errorf("Expected default delete policy ack level 'all', got '%s'", engine.deletePolicy.AckLevel)
	}
	
	if engine.getPolicy.Strategy != "first" {
		t.Errorf("Expected default get policy strategy 'first', got '%s'", engine.getPolicy.Strategy)
	}
}

func TestNewEngineWithCustomConfig(t *testing.T) {
	auth := &MockAuthenticator{}
	replicator := NewMockReplicationExecutor()
	fetcher := NewMockFetchingExecutor()
	
	config := &Config{
		Policies: Policies{
			Put: WriteOperationPolicy{AckLevel: "all"},
			Delete: WriteOperationPolicy{AckLevel: "one"},
			Get: ReadOperationPolicy{Strategy: "newest"},
		},
	}
	
	engine := NewEngine(auth, replicator, fetcher, config)
	
	// Проверяем кастомные политики
	if engine.putPolicy.AckLevel != "all" {
		t.Errorf("Expected custom put policy ack level 'all', got '%s'", engine.putPolicy.AckLevel)
	}
	
	if engine.deletePolicy.AckLevel != "one" {
		t.Errorf("Expected custom delete policy ack level 'one', got '%s'", engine.deletePolicy.AckLevel)
	}
	
	if engine.getPolicy.Strategy != "newest" {
		t.Errorf("Expected custom get policy strategy 'newest', got '%s'", engine.getPolicy.Strategy)
	}
}

func TestEngine_Handle_AuthenticationFailure(t *testing.T) {
	// Настраиваем mock аутентификатор для возврата ошибки
	auth := &MockAuthenticator{
		shouldFail: true,
		failError:  auth.ErrInvalidAccessKeyID,
	}
	replicator := NewMockReplicationExecutor()
	fetcher := NewMockFetchingExecutor()
	engine := NewEngine(auth, replicator, fetcher, nil)
	
	req := &apigw.S3Request{
		Operation: apigw.GetObject,
		Bucket:    "test-bucket",
		Key:       "test-key",
		Context:   context.Background(),
		Headers:   make(http.Header),
		Query:     make(url.Values),
	}
	
	resp := engine.Handle(req)
	
	// Проверяем, что возвращается ошибка аутентификации
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("Expected status code %d, got %d", http.StatusForbidden, resp.StatusCode)
	}
	
	// Поле Error больше не устанавливается, так как у нас есть правильно сформированный ответ
	// if resp.Error == nil {
	//     t.Error("Expected error to be set")
	// }
	
	// Проверяем, что тело ответа содержит XML ошибку
	if resp.Body != nil {
		body := make([]byte, 1024)
		n, _ := resp.Body.Read(body)
		bodyStr := string(body[:n])
		if !strings.Contains(bodyStr, "InvalidAccessKeyId") {
			t.Errorf("Expected error body to contain 'InvalidAccessKeyId', got: %s", bodyStr)
		}
	}
}

func TestEngine_Handle_WriteOperations(t *testing.T) {
	auth := &MockAuthenticator{}
	replicator := NewMockReplicationExecutor()
	fetcher := NewMockFetchingExecutor()
	engine := NewEngine(auth, replicator, fetcher, nil)
	
	writeOperations := []apigw.S3Operation{
		apigw.PutObject,
		apigw.DeleteObject,
		apigw.CreateMultipartUpload,
		apigw.UploadPart,
		apigw.CompleteMultipartUpload,
		apigw.AbortMultipartUpload,
	}
	
	for _, operation := range writeOperations {
		t.Run(operation.String(), func(t *testing.T) {
			req := &apigw.S3Request{
				Operation: operation,
				Bucket:    "test-bucket",
				Key:       "test-key",
				Context:   context.Background(),
				Headers:   make(http.Header),
				Query:     make(url.Values),
			}
			
			// Для multipart операций добавляем необходимые параметры
			if operation == apigw.UploadPart {
				req.Query.Set("partNumber", "1")
				req.Query.Set("uploadId", "test-upload-id")
			} else if operation == apigw.CompleteMultipartUpload || operation == apigw.AbortMultipartUpload {
				req.Query.Set("uploadId", "test-upload-id")
			}
			
			resp := engine.Handle(req)
			
			// Проверяем, что операция выполнена успешно
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				t.Errorf("Expected successful status code, got %d", resp.StatusCode)
			}
			
			if resp.Error != nil {
				t.Errorf("Expected no error, got %v", resp.Error)
			}
		})
	}
}

func TestEngine_Handle_ReadOperations(t *testing.T) {
	auth := &MockAuthenticator{}
	replicator := NewMockReplicationExecutor()
	fetcher := NewMockFetchingExecutor()
	engine := NewEngine(auth, replicator, fetcher, nil)
	
	readOperations := []apigw.S3Operation{
		apigw.GetObject,
		apigw.HeadObject,
		apigw.ListObjectsV2,
		apigw.ListBuckets,
		apigw.ListMultipartUploads,
	}
	
	for _, operation := range readOperations {
		t.Run(operation.String(), func(t *testing.T) {
			req := &apigw.S3Request{
				Operation: operation,
				Bucket:    "test-bucket",
				Key:       "test-key",
				Context:   context.Background(),
				Headers:   make(http.Header),
				Query:     make(url.Values),
			}
			
			resp := engine.Handle(req)
			
			// Проверяем, что операция выполнена успешно
			if resp.StatusCode != http.StatusOK {
				t.Errorf("Expected status code %d, got %d", http.StatusOK, resp.StatusCode)
			}
			
			if resp.Error != nil {
				t.Errorf("Expected no error, got %v", resp.Error)
			}
		})
	}
}

func TestEngine_Handle_UnsupportedOperation(t *testing.T) {
	auth := &MockAuthenticator{}
	replicator := NewMockReplicationExecutor()
	fetcher := NewMockFetchingExecutor()
	engine := NewEngine(auth, replicator, fetcher, nil)
	
	req := &apigw.S3Request{
		Operation: apigw.UnsupportedOperation,
		Bucket:    "test-bucket",
		Key:       "test-key",
		Context:   context.Background(),
		Headers:   make(http.Header),
		Query:     make(url.Values),
	}
	
	resp := engine.Handle(req)
	
	// Проверяем, что возвращается ошибка "не реализовано"
	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("Expected status code %d, got %d", http.StatusNotImplemented, resp.StatusCode)
	}
	
	// Поле Error больше не устанавливается, так как у нас есть правильно сформированный ответ
	// if resp.Error == nil {
	//     t.Error("Expected error to be set")
	// }
	
	// Проверяем, что тело ответа содержит XML ошибку
	if resp.Body != nil {
		body := make([]byte, 1024)
		n, _ := resp.Body.Read(body)
		bodyStr := string(body[:n])
		if !strings.Contains(bodyStr, "NotImplemented") {
			t.Errorf("Expected error body to contain 'NotImplemented', got: %s", bodyStr)
		}
	}
}

func TestEngine_AuthErrorMapping(t *testing.T) {
	replicator := NewMockReplicationExecutor()
	fetcher := NewMockFetchingExecutor()
	
	testCases := []struct {
		name           string
		authError      error
		expectedStatus int
		expectedCode   string
	}{
		{
			name:           "MissingAuthHeader",
			authError:      auth.ErrMissingAuthHeader,
			expectedStatus: http.StatusBadRequest,
			expectedCode:   "MissingSecurityHeader",
		},
		{
			name:           "InvalidAccessKeyID",
			authError:      auth.ErrInvalidAccessKeyID,
			expectedStatus: http.StatusForbidden,
			expectedCode:   "InvalidAccessKeyId",
		},
		{
			name:           "SignatureMismatch",
			authError:      auth.ErrSignatureMismatch,
			expectedStatus: http.StatusForbidden,
			expectedCode:   "SignatureDoesNotMatch",
		},
		{
			name:           "RequestExpired",
			authError:      auth.ErrRequestExpired,
			expectedStatus: http.StatusForbidden,
			expectedCode:   "RequestTimeTooSkewed",
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			auth := &MockAuthenticator{
				shouldFail: true,
				failError:  tc.authError,
			}
			engine := NewEngine(auth, replicator, fetcher, nil)
			
			req := &apigw.S3Request{
				Operation: apigw.GetObject,
				Bucket:    "test-bucket",
				Key:       "test-key",
				Context:   context.Background(),
				Headers:   make(http.Header),
				Query:     make(url.Values),
			}
			
			resp := engine.Handle(req)
			
			if resp.StatusCode != tc.expectedStatus {
				t.Errorf("Expected status code %d, got %d", tc.expectedStatus, resp.StatusCode)
			}
			
			if resp.Body != nil {
				body := make([]byte, 1024)
				n, _ := resp.Body.Read(body)
				bodyStr := string(body[:n])
				if !strings.Contains(bodyStr, tc.expectedCode) {
					t.Errorf("Expected error body to contain '%s', got: %s", tc.expectedCode, bodyStr)
				}
			}
		})
	}
}
