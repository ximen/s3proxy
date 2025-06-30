package replicator

import (
	"fmt"
	"time"
)

// Config содержит конфигурацию модуля репликации
type Config struct {
	// MultipartUploadTTL - время жизни маппинга multipart upload
	MultipartUploadTTL time.Duration `yaml:"multipart_upload_ttl"`
	
	// CleanupInterval - интервал очистки устаревших multipart upload маппингов
	CleanupInterval time.Duration `yaml:"cleanup_interval"`
	
	// MaxConcurrentOperations - максимальное количество одновременных операций
	MaxConcurrentOperations int `yaml:"max_concurrent_operations"`
	
	// OperationTimeout - таймаут для операций с бэкендами
	OperationTimeout time.Duration `yaml:"operation_timeout"`
	
	// RetryAttempts - количество попыток повтора при ошибках
	RetryAttempts int `yaml:"retry_attempts"`
	
	// RetryDelay - задержка между попытками повтора
	RetryDelay time.Duration `yaml:"retry_delay"`
	
	// BufferSize - размер буфера для потоковых операций
	BufferSize int `yaml:"buffer_size"`
}

// DefaultConfig возвращает конфигурацию по умолчанию
func DefaultConfig() *Config {
	return &Config{
		MultipartUploadTTL:      24 * time.Hour,  // 24 часа
		CleanupInterval:         1 * time.Hour,   // Очистка каждый час
		MaxConcurrentOperations: 100,             // Максимум 100 одновременных операций
		OperationTimeout:        30 * time.Second, // 30 секунд на операцию
		RetryAttempts:           3,               // 3 попытки
		RetryDelay:              1 * time.Second, // 1 секунда между попытками
		BufferSize:              32 * 1024,       // 32KB буфер
	}
}

// Validate проверяет корректность конфигурации
func (c *Config) Validate() error {
	if c.MultipartUploadTTL <= 0 {
		return fmt.Errorf("multipart_upload_ttl must be positive")
	}
	
	if c.CleanupInterval <= 0 {
		return fmt.Errorf("cleanup_interval must be positive")
	}
	
	if c.MaxConcurrentOperations <= 0 {
		return fmt.Errorf("max_concurrent_operations must be positive")
	}
	
	if c.OperationTimeout <= 0 {
		return fmt.Errorf("operation_timeout must be positive")
	}
	
	if c.RetryAttempts < 0 {
		return fmt.Errorf("retry_attempts must be non-negative")
	}
	
	if c.RetryDelay < 0 {
		return fmt.Errorf("retry_delay must be non-negative")
	}
	
	if c.BufferSize <= 0 {
		return fmt.Errorf("buffer_size must be positive")
	}
	
	return nil
}
