package backend

import (
	"fmt"
	"time"
)

// ManagerConfig содержит конфигурацию для менеджера бэкендов
type ManagerConfig struct {
	// HealthCheckInterval - интервал между активными проверками здоровья
	HealthCheckInterval time.Duration `yaml:"health_check_interval"`

	// CheckTimeout - таймаут для одной проверки здоровья
	CheckTimeout time.Duration `yaml:"check_timeout"`

	// FailureThreshold - количество последовательных неудач для перехода в DOWN
	FailureThreshold int `yaml:"failure_threshold"`

	// SuccessThreshold - количество последовательных успехов для перехода из PROBING в UP
	SuccessThreshold int `yaml:"success_threshold"`

	// CircuitBreakerWindow - размер скользящего окна для Circuit Breaker
	CircuitBreakerWindow time.Duration `yaml:"circuit_breaker_window"`

	// CircuitBreakerThreshold - количество ошибок в окне для срабатывания Circuit Breaker
	CircuitBreakerThreshold int `yaml:"circuit_breaker_threshold"`

	// InitialState - начальное состояние бэкендов при запуске
	InitialState BackendState `yaml:"initial_state"`
}

// Config содержит полную конфигурацию модуля
type Config struct {
	Manager  ManagerConfig                `yaml:"manager"`
	Backends map[string]BackendConfig     `yaml:"backends"`
}

// DefaultManagerConfig возвращает конфигурацию менеджера по умолчанию
func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		HealthCheckInterval:     15 * time.Second,
		CheckTimeout:            5 * time.Second,
		FailureThreshold:        3,
		SuccessThreshold:        2,
		CircuitBreakerWindow:    60 * time.Second,
		CircuitBreakerThreshold: 5,
		InitialState:            StateProbing, // Начинаем с проверки
	}
}

// DefaultConfig возвращает конфигурацию по умолчанию
func DefaultConfig() *Config {
	return &Config{
		Manager: DefaultManagerConfig(),
		Backends: map[string]BackendConfig{
			"local-minio": {
				Endpoint:  "http://localhost:9000",
				Region:    "us-east-1",
				Bucket:    "test-bucket",
				AccessKey: "minioadmin",
				SecretKey: "minioadmin",
			},
		},
	}
}

// Validate проверяет корректность конфигурации
func (c *Config) Validate() error {
	// Проверяем конфигурацию менеджера
	if err := c.Manager.Validate(); err != nil {
		return fmt.Errorf("invalid manager config: %w", err)
	}

	// Проверяем, что есть хотя бы один бэкенд
	if len(c.Backends) == 0 {
		return fmt.Errorf("at least one backend must be configured")
	}

	// Проверяем каждый бэкенд
	for id, backend := range c.Backends {
		if err := backend.Validate(); err != nil {
			return fmt.Errorf("invalid backend config '%s': %w", id, err)
		}
	}

	return nil
}

// Validate проверяет корректность конфигурации менеджера
func (mc *ManagerConfig) Validate() error {
	if mc.HealthCheckInterval <= 0 {
		return fmt.Errorf("health_check_interval must be positive")
	}

	if mc.CheckTimeout <= 0 {
		return fmt.Errorf("check_timeout must be positive")
	}

	if mc.CheckTimeout >= mc.HealthCheckInterval {
		return fmt.Errorf("check_timeout must be less than health_check_interval")
	}

	if mc.FailureThreshold <= 0 {
		return fmt.Errorf("failure_threshold must be positive")
	}

	if mc.SuccessThreshold <= 0 {
		return fmt.Errorf("success_threshold must be positive")
	}

	if mc.CircuitBreakerWindow <= 0 {
		return fmt.Errorf("circuit_breaker_window must be positive")
	}

	if mc.CircuitBreakerThreshold <= 0 {
		return fmt.Errorf("circuit_breaker_threshold must be positive")
	}

	if mc.InitialState != StateUp && mc.InitialState != StateDown && mc.InitialState != StateProbing {
		return fmt.Errorf("initial_state must be one of: UP, DOWN, PROBING")
	}

	return nil
}

// Validate проверяет корректность конфигурации бэкенда
func (bc *BackendConfig) Validate() error {
	if bc.Endpoint == "" {
		return fmt.Errorf("endpoint cannot be empty")
	}

	if bc.Region == "" {
		return fmt.Errorf("region cannot be empty")
	}

	if bc.Bucket == "" {
		return fmt.Errorf("bucket cannot be empty")
	}

	if bc.AccessKey == "" {
		return fmt.Errorf("access_key cannot be empty")
	}

	if bc.SecretKey == "" {
		return fmt.Errorf("secret_key cannot be empty")
	}

	return nil
}
