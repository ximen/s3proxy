package handlers

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"s3proxy/apigw"
	"s3proxy/auth"
)

func TestPolicyRoutingHandler_WithAuthentication(t *testing.T) {
	// Создаем тестовые учетные данные
	creds := map[string]auth.SecretKey{
		"AKIAIOSFODNN7EXAMPLE": {
			SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			DisplayName:     "test-user",
		},
	}

	authenticator, err := auth.NewStaticAuthenticator(creds)
	if err != nil {
		t.Fatalf("Failed to create authenticator: %v", err)
	}

	handler := NewPolicyRoutingHandler(authenticator)

	t.Run("MissingAuthHeader", func(t *testing.T) {
		req := &apigw.S3Request{
			Operation: apigw.GetObject,
			Bucket:    "test-bucket",
			Key:       "test-object.txt",
			Headers:   make(http.Header),
			Query:     make(url.Values),
		}

		resp := handler.Handle(req)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status %d, got %d", http.StatusBadRequest, resp.StatusCode)
		}
		if resp.Error == nil {
			t.Error("Expected error for missing auth header")
		}
	})

	t.Run("InvalidAuthHeader", func(t *testing.T) {
		req := &apigw.S3Request{
			Operation: apigw.GetObject,
			Bucket:    "test-bucket",
			Key:       "test-object.txt",
			Headers:   make(http.Header),
			Query:     make(url.Values),
		}
		req.Headers.Set("Authorization", "Invalid header format")

		resp := handler.Handle(req)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status %d, got %d", http.StatusBadRequest, resp.StatusCode)
		}
		if resp.Error == nil {
			t.Error("Expected error for invalid auth header")
		}
	})

	t.Run("InvalidAccessKey", func(t *testing.T) {
		req := &apigw.S3Request{
			Operation: apigw.GetObject,
			Bucket:    "test-bucket",
			Key:       "test-object.txt",
			Headers:   make(http.Header),
			Query:     make(url.Values),
		}
		req.Headers.Set("Authorization", "AWS4-HMAC-SHA256 Credential=INVALID_KEY/20230101/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-content-sha256, Signature=abc123")
		req.Headers.Set("x-amz-content-sha256", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")

		resp := handler.Handle(req)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Expected status %d, got %d", http.StatusForbidden, resp.StatusCode)
		}
		if resp.Error == nil {
			t.Error("Expected error for invalid access key")
		}
	})

	t.Run("SignatureMismatch", func(t *testing.T) {
		req := &apigw.S3Request{
			Operation: apigw.GetObject,
			Bucket:    "test-bucket",
			Key:       "test-object.txt",
			Headers:   make(http.Header),
			Query:     make(url.Values),
		}
		req.Headers.Set("Authorization", "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20230101/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-content-sha256;x-amz-date, Signature=invalid_signature")
		req.Headers.Set("Host", "s3.amazonaws.com")
		req.Headers.Set("x-amz-content-sha256", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
		req.Headers.Set("x-amz-date", "20230101T120000Z")

		resp := handler.Handle(req)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Expected status %d, got %d", http.StatusForbidden, resp.StatusCode)
		}
		if resp.Error == nil {
			t.Error("Expected error for signature mismatch")
		}
	})
}

func TestPolicyRoutingHandler_WithMockAuth(t *testing.T) {
	// Создаем mock аутентификатор для тестирования успешных случаев
	mockAuth := &MockAuthenticator{
		shouldSucceed: true,
		userIdentity: &auth.UserIdentity{
			AccessKey:   "test-key",
			DisplayName: "test-user",
		},
	}

	handler := NewPolicyRoutingHandler(mockAuth)

	t.Run("SuccessfulAuthentication", func(t *testing.T) {
		req := &apigw.S3Request{
			Operation: apigw.GetObject,
			Bucket:    "test-bucket",
			Key:       "test-object.txt",
			Headers:   make(http.Header),
			Query:     make(url.Values),
		}
		req.Headers.Set("Authorization", "valid-auth-header")

		resp := handler.Handle(req)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, resp.StatusCode)
		}
		if resp.Error != nil {
			t.Errorf("Expected no error, got %v", resp.Error)
		}
	})

	t.Run("PutObjectWithAuth", func(t *testing.T) {
		req := &apigw.S3Request{
			Operation: apigw.PutObject,
			Bucket:    "test-bucket",
			Key:       "test-object.txt",
			Headers:   make(http.Header),
			Query:     make(url.Values),
			Body:      io.NopCloser(strings.NewReader("test data")),
		}
		req.Headers.Set("Authorization", "valid-auth-header")

		resp := handler.Handle(req)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, resp.StatusCode)
		}
		if resp.Headers.Get("ETag") == "" {
			t.Error("Expected ETag header in response")
		}
	})
}

// MockAuthenticator для тестирования
type MockAuthenticator struct {
	shouldSucceed bool
	userIdentity  *auth.UserIdentity
	errorToReturn error
}

func (m *MockAuthenticator) Authenticate(req *apigw.S3Request) (*auth.UserIdentity, error) {
	if m.shouldSucceed {
		return m.userIdentity, nil
	}
	if m.errorToReturn != nil {
		return nil, m.errorToReturn
	}
	return nil, auth.ErrMissingAuthHeader
}

func TestPolicyRoutingHandler_AuthenticationErrors(t *testing.T) {
	tests := []struct {
		name          string
		authError     error
		expectedCode  int
	}{
		{
			name:         "MissingAuthHeader",
			authError:    auth.ErrMissingAuthHeader,
			expectedCode: http.StatusBadRequest,
		},
		{
			name:         "InvalidAuthHeader",
			authError:    auth.ErrInvalidAuthHeader,
			expectedCode: http.StatusBadRequest,
		},
		{
			name:         "InvalidAccessKeyID",
			authError:    auth.ErrInvalidAccessKeyID,
			expectedCode: http.StatusForbidden,
		},
		{
			name:         "SignatureMismatch",
			authError:    auth.ErrSignatureMismatch,
			expectedCode: http.StatusForbidden,
		},
		{
			name:         "RequestExpired",
			authError:    auth.ErrRequestExpired,
			expectedCode: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAuth := &MockAuthenticator{
				shouldSucceed: false,
				errorToReturn: tt.authError,
			}

			handler := NewPolicyRoutingHandler(mockAuth)

			req := &apigw.S3Request{
				Operation: apigw.GetObject,
				Bucket:    "test-bucket",
				Key:       "test-object.txt",
				Headers:   make(http.Header),
				Query:     make(url.Values),
			}

			resp := handler.Handle(req)
			if resp.StatusCode != tt.expectedCode {
				t.Errorf("Expected status %d, got %d", tt.expectedCode, resp.StatusCode)
			}
			if resp.Error == nil {
				t.Error("Expected error in response")
			}
		})
	}
}
