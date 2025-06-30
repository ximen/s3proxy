package backend

import (
	"fmt"
	"time"
)

// ExampleManager демонстрирует основное использование Backend Manager
func ExampleManager() {
	// Создаем конфигурацию
	config := &Config{
		Manager: ManagerConfig{
			HealthCheckInterval:     5 * time.Second,
			CheckTimeout:            2 * time.Second,
			FailureThreshold:        2,
			SuccessThreshold:        1,
			CircuitBreakerWindow:    30 * time.Second,
			CircuitBreakerThreshold: 3,
			InitialState:            StateProbing,
		},
		Backends: map[string]BackendConfig{
			"primary": {
				Endpoint:  "https://s3.amazonaws.com",
				Region:    "us-east-1",
				Bucket:    "my-primary-bucket",
				AccessKey: "AKIAIOSFODNN7EXAMPLE",
				SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			},
			"backup": {
				Endpoint:  "https://s3.eu-central-1.amazonaws.com",
				Region:    "eu-central-1",
				Bucket:    "my-backup-bucket",
				AccessKey: "AKIAIOSFODNN7EXAMPLE",
				SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			},
		},
	}

	// Создаем менеджер
	manager, err := NewManager(config, nil)
	if err != nil {
		fmt.Printf("Failed to create manager: %v\n", err)
		return
	}

	// Запускаем активные проверки
	err = manager.Start()
	if err != nil {
		fmt.Printf("Failed to start manager: %v\n", err)
		return
	}
	defer manager.Stop()

	// Получаем все бэкенды
	allBackends := manager.GetAllBackends()
	fmt.Printf("Total backends: %d\n", len(allBackends))

	// Получаем живые бэкенды
	liveBackends := manager.GetLiveBackends()
	fmt.Printf("Live backends: %d\n", len(liveBackends))

	// Симулируем успешную операцию
	manager.ReportSuccess("primary")
	fmt.Println("Reported success for primary backend")

	// Симулируем неудачную операцию
	manager.ReportFailure("backup", fmt.Errorf("connection timeout"))
	fmt.Println("Reported failure for backup backend")

	// Проверяем состояние конкретного бэкенда
	if backend, exists := manager.GetBackend("primary"); exists {
		state := backend.GetState()
		fmt.Printf("Primary backend state: %s\n", state)
	}

	// Output:
	// Total backends: 2
	// Live backends: 2
	// Reported success for primary backend
	// Reported failure for backup backend
	// Primary backend state: PROBING
}

// ExampleCircuitBreaker демонстрирует работу Circuit Breaker
func Example_circuitBreaker() {
	config := DefaultConfig()
	config.Manager.CircuitBreakerThreshold = 2 // Низкий порог для демонстрации
	config.Manager.CircuitBreakerWindow = 10 * time.Second

	manager, _ := NewManager(config, nil)

	backendID := "local-minio"
	testError := fmt.Errorf("network error")

	// Получаем бэкенд и устанавливаем состояние UP
	backend, _ := manager.GetBackend(backendID)
	backend.mu.Lock()
	backend.state = StateUp
	backend.mu.Unlock()

	fmt.Printf("Initial state: %s\n", backend.GetState())

	// Отправляем ошибки
	manager.ReportFailure(backendID, testError)
	fmt.Printf("After 1 failure: %s\n", backend.GetState())

	manager.ReportFailure(backendID, testError)
	fmt.Printf("After 2 failures (circuit breaker): %s\n", backend.GetState())

	// Output:
	// Initial state: UP
	// After 1 failure: UP
	// After 2 failures (circuit breaker): DOWN
}

// ExampleStateTransitions демонстрирует переходы состояний
func Example_stateTransitions() {
	// Создаем бэкенд в состоянии DOWN
	backend := &Backend{
		ID:    "test-backend",
		state: StateDown,
	}

	fmt.Printf("Initial state: %s (%.1f)\n", backend.GetState(), backend.GetState().ToFloat64())

	// Симулируем успешную проверку здоровья
	backend.mu.Lock()
	backend.state = StateProbing
	backend.consecutiveSuccesses = 1
	backend.mu.Unlock()

	fmt.Printf("After health check success: %s (%.1f)\n", backend.GetState(), backend.GetState().ToFloat64())

	// Симулируем достижение порога успехов
	backend.mu.Lock()
	backend.state = StateUp
	backend.consecutiveSuccesses = 2
	backend.mu.Unlock()

	fmt.Printf("After reaching success threshold: %s (%.1f)\n", backend.GetState(), backend.GetState().ToFloat64())

	// Output:
	// Initial state: DOWN (0.0)
	// After health check success: PROBING (0.5)
	// After reaching success threshold: UP (1.0)
}
