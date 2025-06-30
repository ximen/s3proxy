package monitoring

import (
	"context"
	"fmt"

	"s3proxy/logger"
)

// Monitor представляет основной интерфейс модуля мониторинга
type Monitor struct {
	config  *Config
	metrics *Metrics
	server  *Server
}

// New создает новый экземпляр Monitor
func New(config *Config) (*Monitor, error) {
	if config == nil {
		config = DefaultConfig()
	}
	
	// Валидируем конфигурацию
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid monitoring config: %w", err)
	}
	
	// Создаем метрики
	metrics := NewMetrics()
	
	// Создаем сервер
	server := NewServer(config, metrics)
	
	monitor := &Monitor{
		config:  config,
		metrics: metrics,
		server:  server,
	}
	
	logger.Info("Monitoring module initialized")
	logger.Debug("Monitoring config: enabled=%v, listen=%s, path=%s", 
		config.Enabled, config.ListenAddress, config.MetricsPath)
	
	return monitor, nil
}

// Start запускает модуль мониторинга
func (m *Monitor) Start() error {
	if !m.config.Enabled {
		logger.Info("Monitoring is disabled")
		return nil
	}
	
	logger.Info("Starting monitoring module...")
	
	// Запускаем HTTP сервер метрик
	if err := m.server.Start(); err != nil {
		return fmt.Errorf("failed to start metrics server: %w", err)
	}
	
	logger.Info("Monitoring module started successfully")
	return nil
}

// Stop останавливает модуль мониторинга
func (m *Monitor) Stop(ctx context.Context) error {
	if !m.config.Enabled {
		return nil
	}
	
	logger.Info("Stopping monitoring module...")
	
	// Останавливаем HTTP сервер
	if err := m.server.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop metrics server: %w", err)
	}
	
	logger.Info("Monitoring module stopped")
	return nil
}

// GetMetrics возвращает экземпляр метрик для использования в других модулях
func (m *Monitor) GetMetrics() *Metrics {
	return m.metrics
}

// GetConfig возвращает конфигурацию мониторинга
func (m *Monitor) GetConfig() *Config {
	return m.config
}

// IsEnabled возвращает true, если мониторинг включен
func (m *Monitor) IsEnabled() bool {
	return m.config.Enabled
}

// GetMetricsURL возвращает URL эндпоинта метрик
func (m *Monitor) GetMetricsURL() string {
	return m.server.GetMetricsURL()
}
