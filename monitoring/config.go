package monitoring

import (
	"fmt"
	"time"
)

// Config содержит конфигурацию для модуля мониторинга
type Config struct {
	// Enabled определяет, включен ли мониторинг
	Enabled bool `yaml:"enabled"`
	
	// ListenAddress - адрес для HTTP сервера метрик (например, ":9091")
	ListenAddress string `yaml:"listen_address"`
	
	// MetricsPath - путь для эндпоинта метрик (по умолчанию "/metrics")
	MetricsPath string `yaml:"metrics_path"`
	
	// ReadTimeout - таймаут чтения для HTTP сервера метрик
	ReadTimeout time.Duration `yaml:"read_timeout"`
	
	// WriteTimeout - таймаут записи для HTTP сервера метрик
	WriteTimeout time.Duration `yaml:"write_timeout"`
	
	// EnableSystemMetrics - включить сбор системных метрик (память, CPU и т.д.)
	EnableSystemMetrics bool `yaml:"enable_system_metrics"`
	
	// SystemMetricsInterval - интервал сбора системных метрик
	SystemMetricsInterval time.Duration `yaml:"system_metrics_interval"`
}

// DefaultConfig возвращает конфигурацию по умолчанию
func DefaultConfig() *Config {
	return &Config{
		Enabled:               true,
		ListenAddress:         ":9091",
		MetricsPath:           "/metrics",
		ReadTimeout:           30 * time.Second,
		WriteTimeout:          30 * time.Second,
		EnableSystemMetrics:   true,
		SystemMetricsInterval: 15 * time.Second,
	}
}

// Validate проверяет корректность конфигурации
func (c *Config) Validate() error {
	if !c.Enabled {
		return nil // Если мониторинг отключен, валидация не нужна
	}
	
	if c.ListenAddress == "" {
		return fmt.Errorf("listen_address cannot be empty when monitoring is enabled")
	}
	
	if c.MetricsPath == "" {
		return fmt.Errorf("metrics_path cannot be empty")
	}
	
	if c.ReadTimeout <= 0 {
		return fmt.Errorf("read_timeout must be positive")
	}
	
	if c.WriteTimeout <= 0 {
		return fmt.Errorf("write_timeout must be positive")
	}
	
	if c.EnableSystemMetrics && c.SystemMetricsInterval <= 0 {
		return fmt.Errorf("system_metrics_interval must be positive when system metrics are enabled")
	}
	
	return nil
}
