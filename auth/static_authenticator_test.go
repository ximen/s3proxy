package auth

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"s3proxy/apigw"
)

func TestNewStaticAuthenticator(t *testing.T) {
	t.Run("ValidCredentials", func(t *testing.T) {
		creds := map[string]SecretKey{
			"AKIAIOSFODNN7EXAMPLE": {
				SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
				DisplayName:     "test-user",
			},
		}

		auth, err := NewStaticAuthenticator(creds)
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if auth == nil {
			t.Error("Expected authenticator instance, got nil")
		}
		if auth.signer == nil {
			t.Error("Expected AWS signer to be initialized")
		}
	})

	t.Run("NilCredentials", func(t *testing.T) {
		auth, err := NewStaticAuthenticator(nil)
		if err == nil {
			t.Error("Expected error for nil credentials")
		}
		if auth != nil {
			t.Error("Expected nil authenticator for invalid input")
		}
	})

	t.Run("EmptyCredentials", func(t *testing.T) {
		creds := make(map[string]SecretKey)
		auth, err := NewStaticAuthenticator(creds)
		if err == nil {
			t.Error("Expected error for empty credentials")
		}
		if auth != nil {
			t.Error("Expected nil authenticator for invalid input")
		}
	})
}

func TestStaticAuthenticator_ParseAuthorizationHeader(t *testing.T) {
	auth := &StaticAuthenticator{}

	t.Run("ValidHeader", func(t *testing.T) {
		header := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20230101/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-date, Signature=abcdef123456"
		
		authData, err := auth.parseAuthorizationHeader(header)
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		
		if authData.AccessKey != "AKIAIOSFODNN7EXAMPLE" {
			t.Errorf("Expected AccessKey 'AKIAIOSFODNN7EXAMPLE', got '%s'", authData.AccessKey)
		}
		if authData.Date != "20230101" {
			t.Errorf("Expected Date '20230101', got '%s'", authData.Date)
		}
		if authData.Region != "us-east-1" {
			t.Errorf("Expected Region 'us-east-1', got '%s'", authData.Region)
		}
		if authData.Service != "s3" {
			t.Errorf("Expected Service 's3', got '%s'", authData.Service)
		}
		if authData.Signature != "abcdef123456" {
			t.Errorf("Expected Signature 'abcdef123456', got '%s'", authData.Signature)
		}
		if len(authData.SignedHeaders) != 2 || authData.SignedHeaders[0] != "host" || authData.SignedHeaders[1] != "x-amz-date" {
			t.Errorf("Expected SignedHeaders ['host', 'x-amz-date'], got %v", authData.SignedHeaders)
		}
		if authData.Algorithm != "AWS4-HMAC-SHA256" {
			t.Errorf("Expected Algorithm 'AWS4-HMAC-SHA256', got '%s'", authData.Algorithm)
		}
	})

	t.Run("S3cmdHeader", func(t *testing.T) {
		header := "AWS4-HMAC-SHA256 Credential=AKIAYDR45T3E2EXAMPLE/20250621/US/s3/aws4_request,SignedHeaders=host;x-amz-content-sha256;x-amz-date,Signature=8790ebde95b47ef9cc9547b6f85e77795c7fe7684824012e81fadfb498aa0e3b"
		
		authData, err := auth.parseAuthorizationHeader(header)
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		
		if authData.AccessKey != "AKIAYDR45T3E2EXAMPLE" {
			t.Errorf("Expected AccessKey 'AKIAYDR45T3E2EXAMPLE', got '%s'", authData.AccessKey)
		}
		if authData.Date != "20250621" {
			t.Errorf("Expected Date '20250621', got '%s'", authData.Date)
		}
		if authData.Region != "US" {
			t.Errorf("Expected Region 'US', got '%s'", authData.Region)
		}
		if authData.Service != "s3" {
			t.Errorf("Expected Service 's3', got '%s'", authData.Service)
		}
		if authData.Signature != "8790ebde95b47ef9cc9547b6f85e77795c7fe7684824012e81fadfb498aa0e3b" {
			t.Errorf("Expected Signature '8790ebde95b47ef9cc9547b6f85e77795c7fe7684824012e81fadfb498aa0e3b', got '%s'", authData.Signature)
		}
		if len(authData.SignedHeaders) != 3 || authData.SignedHeaders[0] != "host" || authData.SignedHeaders[1] != "x-amz-content-sha256" || authData.SignedHeaders[2] != "x-amz-date" {
			t.Errorf("Expected SignedHeaders ['host', 'x-amz-content-sha256', 'x-amz-date'], got %v", authData.SignedHeaders)
		}
		if authData.Algorithm != "AWS4-HMAC-SHA256" {
			t.Errorf("Expected Algorithm 'AWS4-HMAC-SHA256', got '%s'", authData.Algorithm)
		}
	})

	t.Run("InvalidPrefix", func(t *testing.T) {
		header := "INVALID-PREFIX Credential=test/20230101/us-east-1/s3/aws4_request, SignedHeaders=host, Signature=abc"
		
		_, err := auth.parseAuthorizationHeader(header)
		if err != ErrInvalidAuthHeader {
			t.Errorf("Expected ErrInvalidAuthHeader, got %v", err)
		}
	})

	t.Run("MissingComponents", func(t *testing.T) {
		header := "AWS4-HMAC-SHA256 Credential=test/20230101/us-east-1/s3/aws4_request"
		
		_, err := auth.parseAuthorizationHeader(header)
		if err != ErrInvalidAuthHeader {
			t.Errorf("Expected ErrInvalidAuthHeader, got %v", err)
		}
	})

	t.Run("InvalidCredentialFormat", func(t *testing.T) {
		header := "AWS4-HMAC-SHA256 Credential=invalid, SignedHeaders=host, Signature=abc"
		
		_, err := auth.parseAuthorizationHeader(header)
		if err != ErrInvalidAuthHeader {
			t.Errorf("Expected ErrInvalidAuthHeader, got %v", err)
		}
	})
}

func TestStaticAuthenticator_Authenticate(t *testing.T) {
	// Создаем тестовые учетные данные
	creds := map[string]SecretKey{
		"AKIAIOSFODNN7EXAMPLE": {
			SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			DisplayName:     "test-user",
		},
	}

	auth, err := NewStaticAuthenticator(creds)
	if err != nil {
		t.Fatalf("Failed to create authenticator: %v", err)
	}

	t.Run("MissingAuthHeader", func(t *testing.T) {
		req := &apigw.S3Request{
			Operation: apigw.GetObject,
			Bucket:    "test-bucket",
			Key:       "test-object",
			Host:      "localhost:9000",
			Scheme:    "http",
			Headers:   make(http.Header),
			Query:     make(url.Values),
		}

		_, err := auth.Authenticate(req)
		if err != ErrMissingAuthHeader {
			t.Errorf("Expected ErrMissingAuthHeader, got %v", err)
		}
	})

	t.Run("InvalidAuthHeader", func(t *testing.T) {
		req := &apigw.S3Request{
			Operation: apigw.GetObject,
			Bucket:    "test-bucket",
			Key:       "test-object",
			Host:      "localhost:9000",
			Scheme:    "http",
			Headers:   make(http.Header),
			Query:     make(url.Values),
		}
		req.Headers.Set("Authorization", "Invalid header format")

		_, err := auth.Authenticate(req)
		if err != ErrInvalidAuthHeader {
			t.Errorf("Expected ErrInvalidAuthHeader, got %v", err)
		}
	})

	t.Run("InvalidAccessKey", func(t *testing.T) {
		req := &apigw.S3Request{
			Operation: apigw.GetObject,
			Bucket:    "test-bucket",
			Key:       "test-object",
			Host:      "localhost:9000",
			Scheme:    "http",
			Headers:   make(http.Header),
			Query:     make(url.Values),
		}
		req.Headers.Set("Authorization", "AWS4-HMAC-SHA256 Credential=INVALID_KEY/20230101/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-content-sha256, Signature=abc123")
		req.Headers.Set("x-amz-content-sha256", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")

		_, err := auth.Authenticate(req)
		if err != ErrInvalidAccessKeyID {
			t.Errorf("Expected ErrInvalidAccessKeyID, got %v", err)
		}
	})

	t.Run("SignatureMismatch", func(t *testing.T) {
		req := &apigw.S3Request{
			Operation: apigw.GetObject,
			Bucket:    "test-bucket",
			Key:       "test-object",
			Host:      "localhost:9000",
			Scheme:    "http",
			Headers:   make(http.Header),
			Query:     make(url.Values),
		}
		req.Headers.Set("Authorization", "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20230101/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-content-sha256, Signature=invalid_signature")
		req.Headers.Set("Host", "s3.amazonaws.com")
		req.Headers.Set("x-amz-content-sha256", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
		req.Headers.Set("x-amz-date", "20230101T120000Z")

		_, err := auth.Authenticate(req)
		if err != ErrSignatureMismatch {
			t.Errorf("Expected ErrSignatureMismatch, got %v", err)
		}
	})
}

func TestStaticAuthenticator_HelperMethods(t *testing.T) {
	auth := &StaticAuthenticator{}

	t.Run("GetHTTPMethod", func(t *testing.T) {
		tests := []struct {
			operation apigw.S3Operation
			expected  string
		}{
			{apigw.GetObject, "GET"},
			{apigw.PutObject, "PUT"},
			{apigw.HeadObject, "HEAD"},
			{apigw.DeleteObject, "DELETE"},
			{apigw.CreateMultipartUpload, "POST"},
			{apigw.ListBuckets, "GET"},
		}

		for _, test := range tests {
			result := auth.getHTTPMethod(test.operation)
			if result != test.expected {
				t.Errorf("For operation %v, expected method %s, got %s", test.operation, test.expected, result)
			}
		}
	})

	t.Run("GetRequestPath", func(t *testing.T) {
		tests := []struct {
			bucket   string
			key      string
			expected string
		}{
			{"", "", "/"},
			{"bucket", "", "/bucket/"},
			{"bucket", "key", "/bucket/key"},
			{"my-bucket", "path/to/object.txt", "/my-bucket/path/to/object.txt"},
		}

		for _, test := range tests {
			result := auth.getRequestPath(test.bucket, test.key)
			if result != test.expected {
				t.Errorf("For bucket '%s' and key '%s', expected path '%s', got '%s'", 
					test.bucket, test.key, test.expected, result)
			}
		}
	})
}

func TestStaticAuthenticator_CreateHTTPRequest(t *testing.T) {
	auth := &StaticAuthenticator{}

	t.Run("BasicRequest", func(t *testing.T) {
		req := &apigw.S3Request{
			Operation: apigw.GetObject,
			Bucket:    "test-bucket",
			Key:       "test-object.txt",
			Host:      "s3.amazonaws.com",
			Scheme:    "https",
			Headers:   make(http.Header),
			Query:     make(url.Values),
		}
		req.Headers.Set("Host", "s3.amazonaws.com")
		req.Headers.Set("x-amz-content-sha256", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")

		authData := &authorizationData{
			SignedHeaders: []string{"host", "x-amz-content-sha256"},
		}

		httpReq, err := auth.createHTTPRequest(req, authData)
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}

		if httpReq.Method != "GET" {
			t.Errorf("Expected method GET, got %s", httpReq.Method)
		}

		expectedPath := "/test-bucket/test-object.txt"
		if httpReq.URL.Path != expectedPath {
			t.Errorf("Expected path %s, got %s", expectedPath, httpReq.URL.Path)
		}

		if httpReq.URL.Scheme != "https" {
			t.Errorf("Expected scheme https, got %s", httpReq.URL.Scheme)
		}

		if httpReq.Header.Get("Host") != "s3.amazonaws.com" {
			t.Errorf("Expected Host header 's3.amazonaws.com', got '%s'", httpReq.Header.Get("Host"))
		}
	})

	t.Run("RequestWithQuery", func(t *testing.T) {
		req := &apigw.S3Request{
			Operation: apigw.CreateMultipartUpload,
			Bucket:    "test-bucket",
			Key:       "test-object.txt",
			Host:      "s3.amazonaws.com",
			Scheme:    "https",
			Headers:   make(http.Header),
			Query:     make(url.Values),
		}
		req.Query.Set("uploads", "")
		req.Headers.Set("Host", "s3.amazonaws.com")

		authData := &authorizationData{
			SignedHeaders: []string{"host"},
		}

		httpReq, err := auth.createHTTPRequest(req, authData)
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}

		if httpReq.Method != "POST" {
			t.Errorf("Expected method POST, got %s", httpReq.Method)
		}

		if !strings.Contains(httpReq.URL.RawQuery, "uploads=") {
			t.Errorf("Expected query to contain 'uploads=', got '%s'", httpReq.URL.RawQuery)
		}
	})

	t.Run("HTTPScheme", func(t *testing.T) {
		req := &apigw.S3Request{
			Operation: apigw.GetObject,
			Bucket:    "test-bucket",
			Key:       "test-object.txt",
			Host:      "localhost:9000",
			Scheme:    "http",
			Headers:   make(http.Header),
			Query:     make(url.Values),
		}
		req.Headers.Set("Host", "localhost:9000")

		authData := &authorizationData{
			SignedHeaders: []string{"host"},
		}

		httpReq, err := auth.createHTTPRequest(req, authData)
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}

		if httpReq.URL.Scheme != "http" {
			t.Errorf("Expected scheme http, got %s", httpReq.URL.Scheme)
		}

		if httpReq.URL.Host != "localhost:9000" {
			t.Errorf("Expected host localhost:9000, got %s", httpReq.URL.Host)
		}
	})

	t.Run("HTTPSScheme", func(t *testing.T) {
		req := &apigw.S3Request{
			Operation: apigw.GetObject,
			Bucket:    "test-bucket",
			Key:       "test-object.txt",
			Host:      "localhost:9443",
			Scheme:    "https",
			Headers:   make(http.Header),
			Query:     make(url.Values),
		}
		req.Headers.Set("Host", "localhost:9443")

		authData := &authorizationData{
			SignedHeaders: []string{"host"},
		}

		httpReq, err := auth.createHTTPRequest(req, authData)
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}

		if httpReq.URL.Scheme != "https" {
			t.Errorf("Expected scheme https, got %s", httpReq.URL.Scheme)
		}

		if httpReq.URL.Host != "localhost:9443" {
			t.Errorf("Expected host localhost:9443, got %s", httpReq.URL.Host)
		}
	})
}


