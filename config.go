package main

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"s3proxy/apigw"
	"s3proxy/auth"
	"s3proxy/backend"
	"s3proxy/monitoring"
	"s3proxy/routing"
)

// AppConfig содержит полную конфигурацию приложения
type AppConfig struct {
	// Конфигурация API Gateway
	Server ServerConfig `yaml:"server"`
	
	// Конфигурация логирования
	Logging LoggingConfig `yaml:"logging"`
	
	// Конфигурация аутентификации
	Auth auth.Config `yaml:"auth"`
	
	// Конфигурация бэкендов
	Backend backend.Config `yaml:"backend"`
	
	// Конфигурация мониторинга
	Monitoring monitoring.Config `yaml:"monitoring"`
	
	// Конфигурация политик маршрутизации
	Routing routing.Config `yaml:"routing"`
}

// ServerConfig содержит конфигурацию HTTP сервера
type ServerConfig struct {
	ListenAddress string        `yaml:"listen_address"`
	VirtualBucket string        `yaml:"virtual_bucket"`
	TLSCertFile   string        `yaml:"tls_cert_file"`
	TLSKeyFile    string        `yaml:"tls_key_file"`
	ReadTimeout   time.Duration `yaml:"read_timeout"`
	WriteTimeout  time.Duration `yaml:"write_timeout"`
	UseMock       bool          `yaml:"use_mock"`
}

// LoggingConfig содержит конфигурацию логирования
type LoggingConfig struct {
	Level string `yaml:"level"`
}

// DefaultAppConfig возвращает конфигурацию по умолчанию
func DefaultAppConfig() *AppConfig {
	return &AppConfig{
		Server: ServerConfig{
			ListenAddress: ":9000",
			VirtualBucket: "s3proxy-bucket",
			ReadTimeout:   30 * time.Second,
			WriteTimeout:  30 * time.Second,
			UseMock:       false,
		},
		Logging: LoggingConfig{
			Level: "info",
		},
		Auth:       *auth.DefaultConfig(),
		Backend:    *backend.DefaultConfig(),
		Monitoring: *monitoring.DefaultConfig(),
		Routing:    *routing.DefaultConfig(),
	}
}

// LoadConfig загружает конфигурацию из файла
func LoadConfig(filename string) (*AppConfig, error) {
	// Читаем файл
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", filename, err)
	}
	
	// Начинаем с конфигурации по умолчанию
	config := DefaultAppConfig()
	
	// Парсим YAML
	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", filename, err)
	}
	
	// Валидируем конфигурацию
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	
	return config, nil
}

// Validate проверяет корректность конфигурации
func (c *AppConfig) Validate() error {
	// Валидируем server конфигурацию
	if c.Server.ListenAddress == "" {
		return fmt.Errorf("server.listen_address cannot be empty")
	}
	
	if c.Server.ReadTimeout <= 0 {
		return fmt.Errorf("server.read_timeout must be positive")
	}
	
	if c.Server.WriteTimeout <= 0 {
		return fmt.Errorf("server.write_timeout must be positive")
	}
	
	// Проверяем TLS конфигурацию
	if (c.Server.TLSCertFile != "" && c.Server.TLSKeyFile == "") ||
		(c.Server.TLSCertFile == "" && c.Server.TLSKeyFile != "") {
		return fmt.Errorf("both tls_cert_file and tls_key_file must be specified for TLS")
	}
	
	// Валидируем уровень логирования
	if !isValidLogLevel(c.Logging.Level) {
		return fmt.Errorf("invalid logging level: %s", c.Logging.Level)
	}
	
	// Валидируем конфигурации модулей
	if err := c.Auth.Validate(); err != nil {
		return fmt.Errorf("auth config: %w", err)
	}
	
	if err := c.Backend.Validate(); err != nil {
		return fmt.Errorf("backend config: %w", err)
	}
	
	if err := c.Monitoring.Validate(); err != nil {
		return fmt.Errorf("monitoring config: %w", err)
	}
	
	return nil
}

// ToAPIGatewayConfig преобразует в конфигурацию API Gateway
func (c *AppConfig) ToAPIGatewayConfig() apigw.Config {
	return apigw.Config{
		ListenAddress: c.Server.ListenAddress,
		TLSCertFile:   c.Server.TLSCertFile,
		TLSKeyFile:    c.Server.TLSKeyFile,
		ReadTimeout:   c.Server.ReadTimeout,
		WriteTimeout:  c.Server.WriteTimeout,
	}
}

// isValidLogLevel проверяет корректность уровня логирования
func isValidLogLevel(level string) bool {
	validLevels := []string{"debug", "info", "warn", "error"}
	for _, valid := range validLevels {
		if level == valid {
			return true
		}
	}
	return false
}

// SaveConfig сохраняет конфигурацию в файл (для генерации примера)
func (c *AppConfig) SaveConfig(filename string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file %s: %w", filename, err)
	}
	
	return nil
}
