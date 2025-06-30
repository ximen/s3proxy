package monitoring

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()
	
	if !config.Enabled {
		t.Error("Expected monitoring to be enabled by default")
	}
	
	if config.ListenAddress != ":9091" {
		t.Errorf("Expected default listen address ':9091', got '%s'", config.ListenAddress)
	}
	
	if config.MetricsPath != "/metrics" {
		t.Errorf("Expected default metrics path '/metrics', got '%s'", config.MetricsPath)
	}
	
	if config.ReadTimeout != 30*time.Second {
		t.Errorf("Expected default read timeout 30s, got %v", config.ReadTimeout)
	}
	
	if !config.EnableSystemMetrics {
		t.Error("Expected system metrics to be enabled by default")
	}
}

func TestConfigValidation(t *testing.T) {
	testCases := []struct {
		name        string
		config      *Config
		expectError bool
	}{
		{
			name:        "Valid config",
			config:      DefaultConfig(),
			expectError: false,
		},
		{
			name: "Disabled monitoring",
			config: &Config{
				Enabled: false,
			},
			expectError: false,
		},
		{
			name: "Empty listen address",
			config: &Config{
				Enabled:       true,
				ListenAddress: "",
				MetricsPath:   "/metrics",
				ReadTimeout:   30 * time.Second,
				WriteTimeout:  30 * time.Second,
			},
			expectError: true,
		},
		{
			name: "Empty metrics path",
			config: &Config{
				Enabled:       true,
				ListenAddress: ":9091",
				MetricsPath:   "",
				ReadTimeout:   30 * time.Second,
				WriteTimeout:  30 * time.Second,
			},
			expectError: true,
		},
		{
			name: "Invalid read timeout",
			config: &Config{
				Enabled:       true,
				ListenAddress: ":9091",
				MetricsPath:   "/metrics",
				ReadTimeout:   0,
				WriteTimeout:  30 * time.Second,
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

func TestNewMetrics(t *testing.T) {
	// Создаем отдельный registry для тестов
	registry := prometheus.NewRegistry()
	
	// Временно заменяем default registerer
	oldRegisterer := prometheus.DefaultRegisterer
	prometheus.DefaultRegisterer = registry
	defer func() {
		prometheus.DefaultRegisterer = oldRegisterer
	}()
	
	metrics := NewMetrics()
	
	if metrics == nil {
		t.Fatal("Expected metrics to be created")
	}
	
	// Проверяем, что все основные метрики созданы
	if metrics.RequestsTotal == nil {
		t.Error("Expected RequestsTotal to be created")
	}
	
	if metrics.RequestLatency == nil {
		t.Error("Expected RequestLatency to be created")
	}
	
	if metrics.BackendState == nil {
		t.Error("Expected BackendState to be created")
	}
	
	if metrics.CacheHitsTotal == nil {
		t.Error("Expected CacheHitsTotal to be created")
	}
}

func TestNewMonitor(t *testing.T) {
	// Создаем отдельный registry для тестов
	registry := prometheus.NewRegistry()
	
	// Временно заменяем default registerer
	oldRegisterer := prometheus.DefaultRegisterer
	prometheus.DefaultRegisterer = registry
	defer func() {
		prometheus.DefaultRegisterer = oldRegisterer
	}()
	
	// Тест с конфигурацией по умолчанию
	monitor, err := New(nil)
	if err != nil {
		t.Fatalf("Expected no error creating monitor, got: %v", err)
	}
	
	if monitor == nil {
		t.Fatal("Expected monitor to be created")
	}
	
	if !monitor.IsEnabled() {
		t.Error("Expected monitor to be enabled by default")
	}
	
	if monitor.GetMetrics() == nil {
		t.Error("Expected metrics to be available")
	}
}

func TestNewMonitorWithInvalidConfig(t *testing.T) {
	invalidConfig := &Config{
		Enabled:       true,
		ListenAddress: "", // Invalid
		MetricsPath:   "/metrics",
		ReadTimeout:   30 * time.Second,
		WriteTimeout:  30 * time.Second,
	}
	
	_, err := New(invalidConfig)
	if err == nil {
		t.Error("Expected error creating monitor with invalid config")
	}
}

func TestMonitorDisabled(t *testing.T) {
	config := &Config{
		Enabled: false,
	}
	
	monitor, err := New(config)
	if err != nil {
		t.Fatalf("Expected no error creating disabled monitor, got: %v", err)
	}
	
	if monitor.IsEnabled() {
		t.Error("Expected monitor to be disabled")
	}
	
	// Запуск и остановка отключенного монитора не должны вызывать ошибок
	err = monitor.Start()
	if err != nil {
		t.Errorf("Expected no error starting disabled monitor, got: %v", err)
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	err = monitor.Stop(ctx)
	if err != nil {
		t.Errorf("Expected no error stopping disabled monitor, got: %v", err)
	}
}

func TestMonitorStartStop(t *testing.T) {
	// Создаем отдельный registry для тестов
	registry := prometheus.NewRegistry()
	
	// Временно заменяем default registerer
	oldRegisterer := prometheus.DefaultRegisterer
	prometheus.DefaultRegisterer = registry
	defer func() {
		prometheus.DefaultRegisterer = oldRegisterer
	}()
	
	// Используем другой порт для тестов, чтобы избежать конфликтов
	config := &Config{
		Enabled:               true,
		ListenAddress:         ":0", // Используем случайный свободный порт
		MetricsPath:           "/metrics",
		ReadTimeout:           5 * time.Second,
		WriteTimeout:          5 * time.Second,
		EnableSystemMetrics:   false, // Отключаем для тестов
		SystemMetricsInterval: 1 * time.Second,
	}
	
	monitor, err := New(config)
	if err != nil {
		t.Fatalf("Expected no error creating monitor, got: %v", err)
	}
	
	// Запускаем монитор
	err = monitor.Start()
	if err != nil {
		t.Fatalf("Expected no error starting monitor, got: %v", err)
	}
	
	// Даем время серверу запуститься
	time.Sleep(100 * time.Millisecond)
	
	// Останавливаем монитор
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	err = monitor.Stop(ctx)
	if err != nil {
		t.Errorf("Expected no error stopping monitor, got: %v", err)
	}
}

func TestHealthEndpoint(t *testing.T) {
	// Создаем отдельный registry для тестов
	registry := prometheus.NewRegistry()
	
	// Временно заменяем default registerer
	oldRegisterer := prometheus.DefaultRegisterer
	prometheus.DefaultRegisterer = registry
	defer func() {
		prometheus.DefaultRegisterer = oldRegisterer
	}()
	
	config := &Config{
		Enabled:               true,
		ListenAddress:         ":0",
		MetricsPath:           "/metrics",
		ReadTimeout:           5 * time.Second,
		WriteTimeout:          5 * time.Second,
		EnableSystemMetrics:   false,
		SystemMetricsInterval: 1 * time.Second,
	}
	
	monitor, err := New(config)
	if err != nil {
		t.Fatalf("Expected no error creating monitor, got: %v", err)
	}
	
	server := NewServer(config, monitor.GetMetrics())
	
	// Тестируем health handler напрямую
	req, err := http.NewRequest("GET", "/health", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	
	// Используем ResponseRecorder для тестирования
	rr := &testResponseWriter{
		header: make(http.Header),
		body:   make([]byte, 0),
	}
	
	server.healthHandler(rr, req)
	
	if rr.statusCode != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, rr.statusCode)
	}
	
	expectedContentType := "application/json"
	if contentType := rr.header.Get("Content-Type"); contentType != expectedContentType {
		t.Errorf("Expected Content-Type %s, got %s", expectedContentType, contentType)
	}
}

// testResponseWriter - простая реализация http.ResponseWriter для тестов
type testResponseWriter struct {
	header     http.Header
	body       []byte
	statusCode int
}

func (w *testResponseWriter) Header() http.Header {
	return w.header
}

func (w *testResponseWriter) Write(data []byte) (int, error) {
	w.body = append(w.body, data...)
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	return len(data), nil
}

func (w *testResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
}
