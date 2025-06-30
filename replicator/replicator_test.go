package replicator

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"s3proxy/apigw"
	"s3proxy/routing"
)

// MockBackendProvider для тестов
type MockBackendProvider struct {
	backends        []*Backend
	successReports  []string
	failureReports  map[string]error
}

func NewMockBackendProvider(backendCount int) *MockBackendProvider {
	backends := make([]*Backend, backendCount)
	for i := 0; i < backendCount; i++ {
		backends[i] = &Backend{
			ID: fmt.Sprintf("backend-%d", i+1),
		}
	}
	
	return &MockBackendProvider{
		backends:       backends,
		successReports: make([]string, 0),
		failureReports: make(map[string]error),
	}
}

func (m *MockBackendProvider) GetLiveBackends() []*Backend {
	return m.backends
}

func (m *MockBackendProvider) GetAllBackends() []*Backend {
	return m.backends
}

func (m *MockBackendProvider) GetBackend(id string) (*Backend, bool) {
	for _, b := range m.backends {
		if b.ID == id {
			return b, true
		}
	}
	return nil, false
}

func (m *MockBackendProvider) ReportSuccess(backendID string) {
	m.successReports = append(m.successReports, backendID)
}

func (m *MockBackendProvider) ReportFailure(backendID string, err error) {
	m.failureReports[backendID] = err
}

func (m *MockBackendProvider) Start() error { return nil }
func (m *MockBackendProvider) Stop() error { return nil }
func (m *MockBackendProvider) IsRunning() bool { return true }

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()
	
	if config == nil {
		t.Fatal("Expected config to be created")
	}
	
	if config.MultipartUploadTTL <= 0 {
		t.Error("Expected positive multipart upload TTL")
	}
	
	if config.MaxConcurrentOperations <= 0 {
		t.Error("Expected positive max concurrent operations")
	}
	
	if config.OperationTimeout <= 0 {
		t.Error("Expected positive operation timeout")
	}
}

func TestConfigValidation(t *testing.T) {
	testCases := []struct {
		name        string
		config      *Config
		expectError bool
	}{
		{
			name:        "Valid default config",
			config:      DefaultConfig(),
			expectError: false,
		},
		{
			name: "Invalid multipart TTL",
			config: &Config{
				MultipartUploadTTL:      0,
				CleanupInterval:         1 * time.Hour,
				MaxConcurrentOperations: 100,
				OperationTimeout:        30 * time.Second,
				RetryAttempts:           3,
				RetryDelay:              1 * time.Second,
				BufferSize:              32 * 1024,
			},
			expectError: true,
		},
		{
			name: "Invalid max concurrent operations",
			config: &Config{
				MultipartUploadTTL:      24 * time.Hour,
				CleanupInterval:         1 * time.Hour,
				MaxConcurrentOperations: 0,
				OperationTimeout:        30 * time.Second,
				RetryAttempts:           3,
				RetryDelay:              1 * time.Second,
				BufferSize:              32 * 1024,
			},
			expectError: true,
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.config.Validate()
			if tc.expectError && err == nil {
				t.Error("Expected validation error, but got none")
			}
			if !tc.expectError && err != nil {
				t.Errorf("Expected no validation error, but got: %v", err)
			}
		})
	}
}

func TestNewReplicator(t *testing.T) {
	provider := NewMockBackendProvider(2)
	config := DefaultConfig()
	
	replicator := NewReplicator(provider, nil, config)
	
	if replicator == nil {
		t.Fatal("Expected replicator to be created")
	}
	
	if replicator.backendProvider != provider {
		t.Error("Expected backend provider to be set")
	}
	
	if replicator.config != config {
		t.Error("Expected config to be set")
	}
	
	if replicator.multipartStore == nil {
		t.Error("Expected multipart store to be created")
	}
	
	if replicator.readerCloner == nil {
		t.Error("Expected reader cloner to be created")
	}
}

func TestReaderCloner(t *testing.T) {
	cloner := &PipeReaderCloner{}
	
	// Тест с одним reader
	originalData := "test data for cloning"
	readers, err := cloner.Clone(strings.NewReader(originalData), 1)
	if err != nil {
		t.Fatalf("Failed to clone reader: %v", err)
	}
	
	if len(readers) != 1 {
		t.Errorf("Expected 1 reader, got %d", len(readers))
	}
	
	// Читаем данные
	data, err := io.ReadAll(readers[0])
	if err != nil {
		t.Fatalf("Failed to read from cloned reader: %v", err)
	}
	
	if string(data) != originalData {
		t.Errorf("Expected %q, got %q", originalData, string(data))
	}
	
	// Тест с несколькими readers - читаем параллельно
	readers, err = cloner.Clone(strings.NewReader(originalData), 3)
	if err != nil {
		t.Fatalf("Failed to clone reader: %v", err)
	}
	
	if len(readers) != 3 {
		t.Errorf("Expected 3 readers, got %d", len(readers))
	}
	
	// Читаем данные из всех readers параллельно
	var wg sync.WaitGroup
	results := make([]string, 3)
	errors := make([]error, 3)
	
	for i, reader := range readers {
		wg.Add(1)
		go func(idx int, r io.Reader) {
			defer wg.Done()
			data, err := io.ReadAll(r)
			results[idx] = string(data)
			errors[idx] = err
		}(i, reader)
	}
	
	wg.Wait()
	
	// Проверяем результаты
	for i := 0; i < 3; i++ {
		if errors[i] != nil {
			t.Fatalf("Failed to read from cloned reader %d: %v", i, errors[i])
		}
		
		if results[i] != originalData {
			t.Errorf("Reader %d: expected %q, got %q", i, originalData, results[i])
		}
	}
}

func TestCountingReader(t *testing.T) {
	data := "test data for counting"
	reader := strings.NewReader(data)
	countingReader := NewCountingReader(reader)
	
	// Читаем данные
	result, err := io.ReadAll(countingReader)
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}
	
	if string(result) != data {
		t.Errorf("Expected %q, got %q", data, string(result))
	}
	
	expectedCount := int64(len(data))
	if countingReader.Count() != expectedCount {
		t.Errorf("Expected count %d, got %d", expectedCount, countingReader.Count())
	}
}

func TestMultipartStore(t *testing.T) {
	config := DefaultConfig()
	config.MultipartUploadTTL = 100 * time.Millisecond // Короткий TTL для тестов
	config.CleanupInterval = 50 * time.Millisecond
	
	store := NewMultipartStore(config)
	defer store.Stop()
	
	// Тест создания маппинга
	backendUploads := map[string]string{
		"backend-1": "upload-1",
		"backend-2": "upload-2",
	}
	
	proxyUploadID, err := store.CreateMapping("test-bucket", "test-key", backendUploads)
	if err != nil {
		t.Fatalf("Failed to create mapping: %v", err)
	}
	
	if proxyUploadID == "" {
		t.Error("Expected non-empty proxy upload ID")
	}
	
	// Тест получения маппинга
	mapping, exists := store.GetMapping(proxyUploadID)
	if !exists {
		t.Error("Expected mapping to exist")
	}
	
	if mapping.Bucket != "test-bucket" {
		t.Errorf("Expected bucket 'test-bucket', got '%s'", mapping.Bucket)
	}
	
	if mapping.Key != "test-key" {
		t.Errorf("Expected key 'test-key', got '%s'", mapping.Key)
	}
	
	if len(mapping.BackendUploads) != 2 {
		t.Errorf("Expected 2 backend uploads, got %d", len(mapping.BackendUploads))
	}
	
	// Тест TTL
	time.Sleep(150 * time.Millisecond) // Ждем истечения TTL
	
	_, exists = store.GetMapping(proxyUploadID)
	if exists {
		t.Error("Expected mapping to expire")
	}
	
	// Тест удаления маппинга
	proxyUploadID2, err := store.CreateMapping("test-bucket-2", "test-key-2", backendUploads)
	if err != nil {
		t.Fatalf("Failed to create second mapping: %v", err)
	}
	
	store.DeleteMapping(proxyUploadID2)
	
	_, exists = store.GetMapping(proxyUploadID2)
	if exists {
		t.Error("Expected mapping to be deleted")
	}
}

func TestCreateErrorResponse(t *testing.T) {
	provider := NewMockBackendProvider(1)
	replicator := NewReplicator(provider, nil, nil)
	
	response := replicator.createErrorResponse(404, "NoSuchKey", "The specified key does not exist")
	
	if response.StatusCode != 404 {
		t.Errorf("Expected status code 404, got %d", response.StatusCode)
	}
	
	contentType := response.Headers.Get("Content-Type")
	if contentType != "application/xml" {
		t.Errorf("Expected Content-Type 'application/xml', got '%s'", contentType)
	}
	
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}
	
	bodyStr := string(body)
	if !strings.Contains(bodyStr, "NoSuchKey") {
		t.Errorf("Expected body to contain 'NoSuchKey', got: %s", bodyStr)
	}
	
	if !strings.Contains(bodyStr, "The specified key does not exist") {
		t.Errorf("Expected body to contain error message, got: %s", bodyStr)
	}
}

func TestCreateSuccessResponse(t *testing.T) {
	provider := NewMockBackendProvider(1)
	replicator := NewReplicator(provider, nil, nil)
	
	req := &apigw.S3Request{
		Bucket: "test-bucket",
		Key:    "test-key",
	}
	
	response := replicator.createSuccessResponse(req, "Operation completed")
	
	if response.StatusCode != 200 {
		t.Errorf("Expected status code 200, got %d", response.StatusCode)
	}
	
	etag := response.Headers.Get("ETag")
	if etag != `"replicator-success"` {
		t.Errorf("Expected ETag to be set, got '%s'", etag)
	}
	
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}
	
	bodyStr := string(body)
	if bodyStr != "Operation completed" {
		t.Errorf("Expected body 'Operation completed', got '%s'", bodyStr)
	}
}

func TestPutObjectNoBackends(t *testing.T) {
	// Создаем provider без бэкендов
	provider := NewMockBackendProvider(0)
	replicator := NewReplicator(provider, nil, nil)
	
	req := &apigw.S3Request{
		Bucket: "test-bucket",
		Key:    "test-key",
		Body:   io.NopCloser(strings.NewReader("test data")),
	}
	
	policy := routing.WriteOperationPolicy{AckLevel: "one"}
	
	response := replicator.PutObject(context.Background(), req, policy)
	
	if response.StatusCode != 503 {
		t.Errorf("Expected status code 503, got %d", response.StatusCode)
	}
}

func TestOperationContext(t *testing.T) {
	ctx := context.Background()
	opCtx := newOperationContext(ctx, "PUT_OBJECT", "test-bucket", "test-key")
	
	if opCtx.operation != "PUT_OBJECT" {
		t.Errorf("Expected operation 'PUT_OBJECT', got '%s'", opCtx.operation)
	}
	
	if opCtx.bucket != "test-bucket" {
		t.Errorf("Expected bucket 'test-bucket', got '%s'", opCtx.bucket)
	}
	
	if opCtx.key != "test-key" {
		t.Errorf("Expected key 'test-key', got '%s'", opCtx.key)
	}
	
	// Проверяем, что время засекается
	time.Sleep(10 * time.Millisecond)
	duration := opCtx.Duration()
	
	if duration < 10*time.Millisecond {
		t.Errorf("Expected duration >= 10ms, got %v", duration)
	}
}
