package fetch

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"s3proxy/apigw"
	"s3proxy/backend"
	"s3proxy/routing"
)

// Mock implementations

type MockBackendProvider struct {
	mock.Mock
}

func (m *MockBackendProvider) GetLiveBackends() []*backend.Backend {
	args := m.Called()
	return args.Get(0).([]*backend.Backend)
}

func (m *MockBackendProvider) GetAllBackends() []*backend.Backend {
	args := m.Called()
	return args.Get(0).([]*backend.Backend)
}

func (m *MockBackendProvider) GetBackend(id string) (*backend.Backend, bool) {
	args := m.Called(id)
	return args.Get(0).(*backend.Backend), args.Bool(1)
}

func (m *MockBackendProvider) ReportSuccess(backendID string) {
	m.Called(backendID)
}

func (m *MockBackendProvider) ReportFailure(backendID string, err error) {
	m.Called(backendID, err)
}

func (m *MockBackendProvider) Start() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockBackendProvider) Stop() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockBackendProvider) IsRunning() bool {
	args := m.Called()
	return args.Bool(0)
}

type MockCache struct {
	mock.Mock
}

func (m *MockCache) Get(bucket, key string) (*apigw.S3Response, bool) {
	args := m.Called(bucket, key)
	if args.Get(0) == nil {
		return nil, args.Bool(1)
	}
	return args.Get(0).(*apigw.S3Response), args.Bool(1)
}

type MockMetrics struct {
	mock.Mock
}

func (m *MockMetrics) ObserveBackendRequestLatency(backendID, operation string, latency float64) {
	m.Called(backendID, operation, latency)
}

func (m *MockMetrics) IncrementBackendRequestsTotal(backendID, operation, result string) {
	m.Called(backendID, operation, result)
}

func (m *MockMetrics) AddBackendBytesRead(backendID string, bytes int64) {
	m.Called(backendID, bytes)
}

// Helper functions

func createTestBackend(id string) *backend.Backend {
	return &backend.Backend{
		ID: id,
		Config: backend.BackendConfig{
			Endpoint:  "https://s3.amazonaws.com",
			Region:    "us-east-1",
			Bucket:    "test-bucket",
			AccessKey: "test-access-key",
			SecretKey: "test-secret-key",
		},
	}
}

func createTestRequest(operation apigw.S3Operation, bucket, key string) *apigw.S3Request {
	return &apigw.S3Request{
		Operation: operation,
		Bucket:    bucket,
		Key:       key,
		Headers:   make(http.Header),
		Query:     make(url.Values),
		Context:   context.Background(),
	}
}

// Tests

func TestNewFetcher(t *testing.T) {
	mockProvider := &MockBackendProvider{}
	mockCache := &MockCache{}
	mockMetrics := &MockMetrics{}

	fetcher := NewFetcher(mockProvider, mockCache, mockMetrics, "test-bucket")

	assert.NotNil(t, fetcher)
	assert.Equal(t, mockProvider, fetcher.backendProvider)
	assert.Equal(t, mockCache, fetcher.cache)
	assert.Equal(t, mockMetrics, fetcher.metrics)
	assert.Equal(t, "test-bucket", fetcher.virtualBucket)
}

func TestFetcher_GetObject_CacheHit(t *testing.T) {
	mockProvider := &MockBackendProvider{}
	mockCache := &MockCache{}
	mockMetrics := &MockMetrics{}

	fetcher := NewFetcher(mockProvider, mockCache, mockMetrics, "test-bucket")

	// Настраиваем мок кэша для возврата результата
	cachedResponse := &apigw.S3Response{
		StatusCode: http.StatusOK,
		Headers:    make(http.Header),
		Body:       io.NopCloser(strings.NewReader("cached content")),
	}
	mockCache.On("Get", "test-bucket", "test-key").Return(cachedResponse, true)

	req := createTestRequest(apigw.GetObject, "test-bucket", "test-key")
	policy := routing.ReadOperationPolicy{Strategy: "first"}

	response := fetcher.GetObject(context.Background(), req, policy)

	assert.Equal(t, http.StatusOK, response.StatusCode)
	assert.Equal(t, cachedResponse, response)
	mockCache.AssertExpectations(t)
}

func TestFetcher_GetObject_NoLiveBackends(t *testing.T) {
	mockProvider := &MockBackendProvider{}
	mockCache := &MockCache{}
	mockMetrics := &MockMetrics{}

	fetcher := NewFetcher(mockProvider, mockCache, mockMetrics, "test-bucket")

	// Настраиваем мок кэша для промаха
	mockCache.On("Get", "test-bucket", "test-key").Return(nil, false)

	// Настраиваем мок провайдера для возврата пустого списка бэкендов
	mockProvider.On("GetLiveBackends").Return([]*backend.Backend{})

	req := createTestRequest(apigw.GetObject, "test-bucket", "test-key")
	policy := routing.ReadOperationPolicy{Strategy: "first"}

	response := fetcher.GetObject(context.Background(), req, policy)

	assert.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
	assert.NotNil(t, response.Error)
	assert.Contains(t, response.Error.Error(), "no live backends available")

	mockCache.AssertExpectations(t)
	mockProvider.AssertExpectations(t)
}

func TestFetcher_GetObject_UnknownStrategy(t *testing.T) {
	mockProvider := &MockBackendProvider{}
	mockCache := &MockCache{}
	mockMetrics := &MockMetrics{}

	fetcher := NewFetcher(mockProvider, mockCache, mockMetrics, "test-bucket")

	// Настраиваем мок кэша для промаха
	mockCache.On("Get", "test-bucket", "test-key").Return(nil, false)
	
	// Добавляем мок для GetLiveBackends, хотя он не должен вызываться из-за неизвестной стратегии
	backend1 := createTestBackend("backend1")
	mockProvider.On("GetLiveBackends").Return([]*backend.Backend{backend1})

	req := createTestRequest(apigw.GetObject, "test-bucket", "test-key")
	policy := routing.ReadOperationPolicy{Strategy: "unknown"}

	response := fetcher.GetObject(context.Background(), req, policy)

	assert.Equal(t, http.StatusInternalServerError, response.StatusCode)
	assert.NotNil(t, response.Error)
	assert.Contains(t, response.Error.Error(), "unknown read strategy")

	mockCache.AssertExpectations(t)
}

func TestFetcher_HeadObject_CacheHit(t *testing.T) {
	mockProvider := &MockBackendProvider{}
	mockCache := &MockCache{}
	mockMetrics := &MockMetrics{}

	fetcher := NewFetcher(mockProvider, mockCache, mockMetrics, "test-bucket")

	// Настраиваем мок кэша для возврата результата
	cachedResponse := &apigw.S3Response{
		StatusCode: http.StatusOK,
		Headers:    make(http.Header),
		Body:       io.NopCloser(strings.NewReader("cached content")),
	}
	mockCache.On("Get", "test-bucket", "test-key").Return(cachedResponse, true)

	req := createTestRequest(apigw.HeadObject, "test-bucket", "test-key")
	policy := routing.ReadOperationPolicy{Strategy: "first"}

	response := fetcher.HeadObject(context.Background(), req, policy)

	assert.Equal(t, http.StatusOK, response.StatusCode)
	assert.Nil(t, response.Body) // HEAD запрос не должен возвращать тело
	assert.Equal(t, cachedResponse.Headers, response.Headers)

	mockCache.AssertExpectations(t)
}

func TestFetcher_HeadBucket_NoLiveBackends(t *testing.T) {
	mockProvider := &MockBackendProvider{}
	mockCache := &MockCache{}
	mockMetrics := &MockMetrics{}

	fetcher := NewFetcher(mockProvider, mockCache, mockMetrics, "test-bucket")

	// Настраиваем мок провайдера для возврата пустого списка бэкендов
	mockProvider.On("GetLiveBackends").Return([]*backend.Backend{})

	req := createTestRequest(apigw.HeadBucket, "test-bucket", "")

	response := fetcher.HeadBucket(context.Background(), req)

	assert.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
	assert.NotNil(t, response.Error)
	assert.Contains(t, response.Error.Error(), "no live backends available")

	mockProvider.AssertExpectations(t)
}

func TestFetcher_ListObjects_NoLiveBackends(t *testing.T) {
	mockProvider := &MockBackendProvider{}
	mockCache := &MockCache{}
	mockMetrics := &MockMetrics{}

	fetcher := NewFetcher(mockProvider, mockCache, mockMetrics, "test-bucket")

	// Настраиваем мок провайдера для возврата пустого списка бэкендов
	mockProvider.On("GetLiveBackends").Return([]*backend.Backend{})

	req := createTestRequest(apigw.ListObjectsV2, "test-bucket", "")

	response := fetcher.ListObjects(context.Background(), req)

	assert.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
	assert.NotNil(t, response.Error)
	assert.Contains(t, response.Error.Error(), "no live backends available")

	mockProvider.AssertExpectations(t)
}

func TestFetcher_ListBuckets_NoLiveBackends(t *testing.T) {
	mockProvider := &MockBackendProvider{}
	mockCache := &MockCache{}
	mockMetrics := &MockMetrics{}

	fetcher := NewFetcher(mockProvider, mockCache, mockMetrics, "test-bucket")

	// Настраиваем мок провайдера для возврата пустого списка бэкендов
	mockProvider.On("GetLiveBackends").Return([]*backend.Backend{})

	req := createTestRequest(apigw.ListBuckets, "", "")

	response := fetcher.ListBuckets(context.Background(), req)

	assert.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
	assert.NotNil(t, response.Error)
	assert.Contains(t, response.Error.Error(), "no live backends available")

	mockProvider.AssertExpectations(t)
}

func TestFetcher_ListMultipartUploads_NoLiveBackends(t *testing.T) {
	mockProvider := &MockBackendProvider{}
	mockCache := &MockCache{}
	mockMetrics := &MockMetrics{}

	fetcher := NewFetcher(mockProvider, mockCache, mockMetrics, "test-bucket")

	// Настраиваем мок провайдера для возврата пустого списка бэкендов
	mockProvider.On("GetLiveBackends").Return([]*backend.Backend{})

	req := createTestRequest(apigw.ListMultipartUploads, "test-bucket", "")

	response := fetcher.ListMultipartUploads(context.Background(), req)

	assert.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
	assert.NotNil(t, response.Error)
	assert.Contains(t, response.Error.Error(), "no live backends available")

	mockProvider.AssertExpectations(t)
}

func TestBytesCountingReader(t *testing.T) {
	mockMetrics := &MockMetrics{}
	content := "test content for counting"
	reader := &bytesCountingReader{
		reader:    io.NopCloser(strings.NewReader(content)),
		backendID: "test-backend",
		metrics:   mockMetrics,
	}

	// Настраиваем мок метрик
	mockMetrics.On("AddBackendBytesRead", "test-backend", int64(len(content))).Return()

	// Читаем все содержимое
	buf := make([]byte, 1024)
	totalRead := 0
	for {
		n, err := reader.Read(buf[totalRead:])
		totalRead += n
		if err == io.EOF {
			break
		}
		assert.NoError(t, err)
	}

	assert.Equal(t, len(content), totalRead)

	// Закрываем reader - должна быть вызвана метрика
	err := reader.Close()
	assert.NoError(t, err)

	mockMetrics.AssertExpectations(t)
}

func TestStubCache(t *testing.T) {
	cache := NewStubCache()
	
	response, found := cache.Get("test-bucket", "test-key")
	
	assert.False(t, found)
	assert.Nil(t, response)
}

func TestProxyContinuationToken(t *testing.T) {
	token := ProxyContinuationToken{
		BackendTokens: map[string]string{
			"backend1": "token1",
			"backend2": "token2",
		},
	}
	
	assert.Equal(t, "token1", token.BackendTokens["backend1"])
	assert.Equal(t, "token2", token.BackendTokens["backend2"])
}
