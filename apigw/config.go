package apigw

import "time"

// Config содержит конфигурацию для API Gateway
type Config struct {
	// ListenAddress - адрес и порт для прослушивания (например, ":9000")
	ListenAddress string

	// TLSCertFile - путь к файлу SSL-сертификата (опционально, для включения HTTPS)
	TLSCertFile string

	// TLSKeyFile - путь к файлу приватного ключа SSL (опционально)
	TLSKeyFile string

	// ReadTimeout - таймаут на чтение всего запроса, включая тело
	ReadTimeout time.Duration

	// WriteTimeout - таймаут на запись всего ответа
	WriteTimeout time.Duration
}

// DefaultConfig возвращает конфигурацию по умолчанию
func DefaultConfig() Config {
	return Config{
		ListenAddress: ":9000",
		ReadTimeout:   30 * time.Second,
		WriteTimeout:  30 * time.Second,
	}
}
