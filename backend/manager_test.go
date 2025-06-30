package backend

import (
	"fmt"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()
	
	if config == nil {
		t.Fatal("Expected config to be created")
	}
	
	if len(config.Backends) == 0 {
		t.Error("Expected at least one backend in default config")
	}
	
	if config.Manager.HealthCheckInterval <= 0 {
		t.Error("Expected positive health check interval")
	}
	
	if config.Manager.CheckTimeout <= 0 {
		t.Error("Expected positive check timeout")
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
			name: "Empty backends",
			config: &Config{
				Manager:  DefaultManagerConfig(),
				Backends: map[string]BackendConfig{},
			},
			expectError: true,
		},
		{
			name: "Invalid manager config - zero interval",
			config: &Config{
				Manager: ManagerConfig{
					HealthCheckInterval: 0,
					CheckTimeout:        5 * time.Second,
					FailureThreshold:    3,
					SuccessThreshold:    2,
					CircuitBreakerWindow: 60 * time.Second,
					CircuitBreakerThreshold: 5,
					InitialState:        StateProbing,
				},
				Backends: map[string]BackendConfig{
					"test": {
						Endpoint:  "http://localhost:9000",
						Region:    "us-east-1",
						Bucket:    "test",
						AccessKey: "test",
						SecretKey: "test",
					},
				},
			},
			expectError: true,
		},
		{
			name: "Invalid backend config - empty endpoint",
			config: &Config{
				Manager: DefaultManagerConfig(),
				Backends: map[string]BackendConfig{
					"test": {
						Endpoint:  "", // Invalid
						Region:    "us-east-1",
						Bucket:    "test",
						AccessKey: "test",
						SecretKey: "test",
					},
				},
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

func TestBackendStateToFloat64(t *testing.T) {
	testCases := []struct {
		state    BackendState
		expected float64
	}{
		{StateUp, 1.0},
		{StateProbing, 0.5},
		{StateDown, 0.0},
		{BackendState("UNKNOWN"), 0.0},
	}
	
	for _, tc := range testCases {
		t.Run(string(tc.state), func(t *testing.T) {
			result := tc.state.ToFloat64()
			if result != tc.expected {
				t.Errorf("Expected %.1f, got %.1f", tc.expected, result)
			}
		})
	}
}

func TestNewManager(t *testing.T) {
	config := DefaultConfig()
	
	manager, err := NewManager(config, nil)
	if err != nil {
		t.Fatalf("Expected no error creating manager, got: %v", err)
	}
	
	if manager == nil {
		t.Fatal("Expected manager to be created")
	}
	
	if len(manager.backends) != len(config.Backends) {
		t.Errorf("Expected %d backends, got %d", len(config.Backends), len(manager.backends))
	}
	
	if manager.IsRunning() {
		t.Error("Expected manager to not be running initially")
	}
}

func TestNewManagerWithInvalidConfig(t *testing.T) {
	invalidConfig := &Config{
		Manager: ManagerConfig{
			HealthCheckInterval: 0, // Invalid
		},
		Backends: map[string]BackendConfig{},
	}
	
	_, err := NewManager(invalidConfig, nil)
	if err == nil {
		t.Error("Expected error creating manager with invalid config")
	}
}

func TestManagerStartStop(t *testing.T) {
	config := DefaultConfig()
	// Используем быстрые интервалы для тестов
	config.Manager.HealthCheckInterval = 100 * time.Millisecond
	config.Manager.CheckTimeout = 50 * time.Millisecond
	
	manager, err := NewManager(config, nil)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	
	// Тестируем запуск
	err = manager.Start()
	if err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	
	if !manager.IsRunning() {
		t.Error("Expected manager to be running after start")
	}
	
	// Даем время для выполнения нескольких проверок
	time.Sleep(300 * time.Millisecond)
	
	// Тестируем остановку
	err = manager.Stop()
	if err != nil {
		t.Errorf("Failed to stop manager: %v", err)
	}
	
	if manager.IsRunning() {
		t.Error("Expected manager to not be running after stop")
	}
	
	// Тестируем повторный запуск после остановки
	err = manager.Start()
	if err != nil {
		t.Fatalf("Failed to restart manager: %v", err)
	}
	
	// Останавливаем для очистки
	manager.Stop()
}

func TestManagerDoubleStart(t *testing.T) {
	config := DefaultConfig()
	manager, err := NewManager(config, nil)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	
	// Первый запуск должен быть успешным
	err = manager.Start()
	if err != nil {
		t.Fatalf("First start failed: %v", err)
	}
	
	// Второй запуск должен вернуть ошибку
	err = manager.Start()
	if err == nil {
		t.Error("Expected error on double start")
	}
	
	manager.Stop()
}

func TestGetBackends(t *testing.T) {
	config := &Config{
		Manager: DefaultManagerConfig(),
		Backends: map[string]BackendConfig{
			"backend1": {
				Endpoint:  "http://localhost:9001",
				Region:    "us-east-1",
				Bucket:    "test1",
				AccessKey: "test",
				SecretKey: "test",
			},
			"backend2": {
				Endpoint:  "http://localhost:9002",
				Region:    "us-east-1",
				Bucket:    "test2",
				AccessKey: "test",
				SecretKey: "test",
			},
		},
	}
	
	manager, err := NewManager(config, nil)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	
	// Тестируем GetAllBackends
	allBackends := manager.GetAllBackends()
	if len(allBackends) != 2 {
		t.Errorf("Expected 2 backends, got %d", len(allBackends))
	}
	
	// Тестируем GetBackend
	backend1, exists := manager.GetBackend("backend1")
	if !exists {
		t.Error("Expected backend1 to exist")
	}
	if backend1.ID != "backend1" {
		t.Errorf("Expected backend ID 'backend1', got '%s'", backend1.ID)
	}
	
	_, exists = manager.GetBackend("nonexistent")
	if exists {
		t.Error("Expected nonexistent backend to not exist")
	}
	
	// Тестируем GetLiveBackends (все должны быть в состоянии PROBING по умолчанию)
	liveBackends := manager.GetLiveBackends()
	if len(liveBackends) != 2 {
		t.Errorf("Expected 2 live backends, got %d", len(liveBackends))
	}
}

func TestReportSuccessFailure(t *testing.T) {
	config := DefaultConfig()
	manager, err := NewManager(config, nil)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	
	backendID := "local-minio" // Из default config
	
	// Получаем бэкенд
	backend, exists := manager.GetBackend(backendID)
	if !exists {
		t.Fatalf("Backend %s not found", backendID)
	}
	
	// Проверяем начальное состояние
	initialFailures, initialSuccesses, _ := backend.GetStats()
	if initialFailures != 0 || initialSuccesses != 0 {
		t.Errorf("Expected initial stats to be 0, got failures=%d, successes=%d", 
			initialFailures, initialSuccesses)
	}
	
	// Тестируем ReportSuccess
	manager.ReportSuccess(backendID)
	
	failures, successes, _ := backend.GetStats()
	if failures != 0 || successes != 1 {
		t.Errorf("After ReportSuccess: expected failures=0, successes=1, got failures=%d, successes=%d", 
			failures, successes)
	}
	
	// Тестируем ReportFailure
	testErr := fmt.Errorf("test error")
	manager.ReportFailure(backendID, testErr)
	
	failures, successes, _ = backend.GetStats()
	if failures != 1 || successes != 0 {
		t.Errorf("After ReportFailure: expected failures=1, successes=0, got failures=%d, successes=%d", 
			failures, successes)
	}
	
	if backend.GetLastError() != testErr {
		t.Errorf("Expected last error to be set")
	}
	
	// Тестируем с несуществующим бэкендом
	manager.ReportSuccess("nonexistent")
	manager.ReportFailure("nonexistent", testErr)
	// Не должно паниковать
}

func TestCircuitBreaker(t *testing.T) {
	config := DefaultConfig()
	config.Manager.CircuitBreakerThreshold = 3
	config.Manager.CircuitBreakerWindow = 1 * time.Second
	
	manager, err := NewManager(config, nil)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	
	backendID := "local-minio"
	backend, _ := manager.GetBackend(backendID)
	
	// Устанавливаем состояние UP для теста
	backend.mu.Lock()
	backend.state = StateUp
	backend.mu.Unlock()
	
	testErr := fmt.Errorf("test error")
	
	// Отправляем несколько ошибок, но меньше порога
	for i := 0; i < 2; i++ {
		manager.ReportFailure(backendID, testErr)
	}
	
	// Состояние должно остаться UP
	if backend.GetState() != StateUp {
		t.Errorf("Expected state UP after %d failures, got %s", 2, backend.GetState())
	}
	
	// Отправляем еще одну ошибку - должен сработать Circuit Breaker
	manager.ReportFailure(backendID, testErr)
	
	// Состояние должно стать DOWN
	if backend.GetState() != StateDown {
		t.Errorf("Expected state DOWN after circuit breaker trigger, got %s", backend.GetState())
	}
}

func TestBackendGetters(t *testing.T) {
	config := DefaultConfig()
	manager, err := NewManager(config, nil)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	
	backendID := "local-minio"
	backend, _ := manager.GetBackend(backendID)
	
	// Тестируем GetState
	state := backend.GetState()
	if state != StateProbing { // Начальное состояние по умолчанию
		t.Errorf("Expected initial state PROBING, got %s", state)
	}
	
	// Тестируем GetLastError (должно быть nil изначально)
	if backend.GetLastError() != nil {
		t.Error("Expected initial last error to be nil")
	}
	
	// Тестируем GetLastCheckTime (должно быть zero time изначально)
	checkTime := backend.GetLastCheckTime()
	if !checkTime.IsZero() {
		t.Error("Expected initial check time to be zero")
	}
	
	// Тестируем GetStats
	failures, successes, recentFailures := backend.GetStats()
	if failures != 0 || successes != 0 || recentFailures != 0 {
		t.Errorf("Expected initial stats to be 0, got failures=%d, successes=%d, recent=%d", 
			failures, successes, recentFailures)
	}
}
