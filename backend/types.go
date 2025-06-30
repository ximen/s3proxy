package backend

import (
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// BackendState представляет состояние бэкенда
type BackendState string

const (
	StateUp      BackendState = "UP"      // Бэкенд полностью работоспособен
	StateDown    BackendState = "DOWN"    // Бэкенд недоступен
	StateProbing BackendState = "PROBING" // Промежуточное состояние - проверка восстановления
)

// String возвращает строковое представление состояния
func (s BackendState) String() string {
	return string(s)
}

// ToFloat64 возвращает числовое представление состояния для метрик Prometheus
func (s BackendState) ToFloat64() float64 {
	switch s {
	case StateUp:
		return 1.0
	case StateProbing:
		return 0.5
	case StateDown:
		return 0.0
	default:
		return 0.0
	}
}

// BackendConfig содержит конфигурацию одного бэкенда
type BackendConfig struct {
	Endpoint  string `yaml:"endpoint"`   // URL эндпоинта S3 (например, https://s3.amazonaws.com)
	Region    string `yaml:"region"`     // Регион AWS (например, us-east-1)
	Bucket    string `yaml:"bucket"`     // Имя бакета на этом бэкенде
	AccessKey string `yaml:"access_key"` // Access Key для аутентификации
	SecretKey string `yaml:"secret_key"` // Secret Key для аутентификации
}

// Backend представляет один S3-бэкенд с его состоянием
type Backend struct {
	ID                 string        // Уникальный идентификатор бэкенда
	Config             BackendConfig // Конфигурация бэкенда
	S3Client           *s3.Client    // Настроенный S3 клиент
	StreamingPutClient *s3.Client    // Специальный клиент для PUT

	// Внутреннее состояние, защищенное мьютексом
	mu                   sync.RWMutex
	state                BackendState
	lastError            error
	lastCheckTime        time.Time
	consecutiveFailures  int // Количество последовательных неудач
	consecutiveSuccesses int // Количество последовательных успехов

	// Статистика для Circuit Breaker
	recentFailures int       // Количество неудач в скользящем окне
	windowStart    time.Time // Начало текущего окна
}

// backendResult представляет результат операции на одном бэкенде
type BackendResult struct {
	BackendID    string
	Method       string      // Операция, на которую получен ответ
	Response     interface{} // Ответ от AWS SDK (может быть разных типов)
	StatusCode   int         // Код статуса ответа
	Err          error
	Duration     time.Duration
	BytesWritten int64
	BytesRead    int64
}

// GetState возвращает текущее состояние бэкенда (потокобезопасно)
func (b *Backend) GetState() BackendState {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.state
}

// GetLastError возвращает последнюю ошибку (потокобезопасно)
func (b *Backend) GetLastError() error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.lastError
}

// GetLastCheckTime возвращает время последней проверки (потокобезопасно)
func (b *Backend) GetLastCheckTime() time.Time {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.lastCheckTime
}

// GetStats возвращает статистику бэкенда (потокобезопасно)
func (b *Backend) GetStats() (consecutiveFailures, consecutiveSuccesses, recentFailures int) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.consecutiveFailures, b.consecutiveSuccesses, b.recentFailures
}

// BackendProvider - интерфейс для получения информации о бэкендах
type BackendProvider interface {
	// GetLiveBackends возвращает список всех работоспособных бэкендов (UP или PROBING)
	GetLiveBackends() []*Backend

	// GetAllBackends возвращает список всех сконфигурированных бэкендов
	GetAllBackends() []*Backend

	// GetBackend возвращает бэкенд по ID
	GetBackend(id string) (*Backend, bool)

	// ReportSuccess сообщает об успешной операции с бэкендом (пассивная проверка)
	ReportSuccess(backendID string)

	// ReportFailure сообщает о неудачной операции с бэкендом (пассивная проверка)
	ReportFailure(backendID string, err error)

	// Start запускает менеджер бэкендов (активные проверки)
	Start() error

	// Stop останавливает менеджер бэкендов
	Stop() error

	// IsRunning возвращает true, если менеджер запущен
	IsRunning() bool
}
