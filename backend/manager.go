package backend

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"s3proxy/logger"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go/middleware"
	//"github.com/elastic/go-elasticsearch/v9/typedapi/types/enums/result"
)

// Manager реализует BackendProvider и управляет состоянием бэкендов
type Manager struct {
	config   ManagerConfig
	backends map[string]*Backend
	metrics  *Metrics // Для экспорта метрик состояния

	// Управление жизненным циклом
	mu       sync.RWMutex
	running  bool
	stopChan chan struct{}
	wg       sync.WaitGroup
}

// NewManager создает новый менеджер бэкендов
func NewManager(cfg *Config) (*Manager, error) {
	if cfg == nil {
		return nil, fmt.Errorf("Config for Backend Manager not provided")
	}

	// Если ManagerConfig не передан, используем дефолтный
	managerConfig := cfg.Manager
	if managerConfig == (ManagerConfig{}) {
		managerConfig = DefaultManagerConfig()
	}

	// Валидируем конфигурацию
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	manager := &Manager{
		config:   managerConfig,
		backends: make(map[string]*Backend),
		metrics:  NewMetrics(),
		stopChan: make(chan struct{}),
	}

	// Инициализируем бэкенды
	for id, backendConfig := range cfg.Backends {
		backend, err := manager.createBackend(id, backendConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create backend '%s': %w", id, err)
		}
		manager.backends[id] = backend
	}

	logger.Info("Backend manager initialized with %d backends", len(manager.backends))
	for id, backend := range manager.backends {
		logger.Info("  - %s: %s (bucket: %s)", id, backend.Config.Endpoint, backend.Config.Bucket)
	}

	return manager, nil
}

// createBackend создает и настраивает один бэкенд
// Эта функция является методом вашей структуры Manager.
func (m *Manager) createBackend(id string, cfg BackendConfig) (*Backend, error) {
	// ... (код создания awsConfig без изменений) ...
	awsConfig, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKey,
			cfg.SecretKey,
			"",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config for backend %s: %w", id, err)
	}

	// --- Создаем основной S3 клиент ---
	defaultS3Client := s3.NewFromConfig(awsConfig, func(o *s3.Options) {
		o.UsePathStyle = true
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
	})
	// !!! НОВЫЙ ЛОГ !!!
	logger.Debug("Backend '%s': created default S3 client at address [%p]", id, defaultS3Client)

	backend := &Backend{
		ID:          id,
		Config:      cfg,
		S3Client:    defaultS3Client,
		state:       m.config.InitialState,
		windowStart: time.Now(),
	}

	// --- Если бэкенд работает по HTTP, создаем ВТОРОЙ, специальный клиент ---
	isHttp := cfg.Endpoint != "" && strings.HasPrefix(strings.ToLower(cfg.Endpoint), "http://")
	if isHttp {
		logger.Warn("Backend '%s' uses HTTP. Creating a special streaming client for PutObject.", id)
		streamingS3Client := s3.NewFromConfig(awsConfig, func(o *s3.Options) {
			o.UsePathStyle = true
			if cfg.Endpoint != "" {
				o.BaseEndpoint = aws.String(cfg.Endpoint)
			}
			o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
			// Удаляем middleware для вычисления SHA256. Это заставит SDK использовать UNSIGNED-PAYLOAD.
			o.APIOptions = append(o.APIOptions, func(stack *middleware.Stack) error {
				return v4.RemoveComputePayloadSHA256Middleware(stack)
			})
		})
		backend.StreamingPutClient = streamingS3Client
	}

	logger.Info("Created backend '%s' (Endpoint: %s, Bucket: %s) with initial state %s", id, cfg.Endpoint, cfg.Bucket, backend.state)
	return backend, nil
}

// Start запускает менеджер бэкендов
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return fmt.Errorf("backend manager is already running")
	}

	logger.Info("Starting backend manager...")

	// Запускаем горутину для активных проверок здоровья
	m.wg.Add(1)
	go m.runHealthChecks()

	m.running = true
	logger.Info("Backend manager started")

	return nil
}

// Stop останавливает менеджер бэкендов
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return nil
	}

	logger.Info("Stopping backend manager...")

	// Сигнализируем о остановке
	close(m.stopChan)

	// Ждем завершения всех горутин
	m.wg.Wait()

	// Создаем новый канал для возможного повторного запуска
	m.stopChan = make(chan struct{})

	m.running = false
	logger.Info("Backend manager stopped")

	return nil
}

// IsRunning возвращает true, если менеджер запущен
func (m *Manager) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}

// GetLiveBackends возвращает список работоспособных бэкендов
func (m *Manager) GetLiveBackends() []*Backend {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var liveBackends []*Backend
	for _, backend := range m.backends {
		state := backend.GetState()
		if state == StateUp {
			liveBackends = append(liveBackends, backend)
		}
	}

	logger.Debug("GetLiveBackends: returning %d out of %d backends", len(liveBackends), len(m.backends))
	return liveBackends
}

// GetAllBackends возвращает список всех бэкендов
func (m *Manager) GetAllBackends() []*Backend {
	m.mu.RLock()
	defer m.mu.RUnlock()

	backends := make([]*Backend, 0, len(m.backends))
	for _, backend := range m.backends {
		backends = append(backends, backend)
	}

	return backends
}

// GetBackend возвращает бэкенд по ID
func (m *Manager) GetBackend(id string) (*Backend, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	backend, exists := m.backends[id]
	return backend, exists
}

// isBenignError классифицирует ошибку как "безопасную", если она не указывает
// на реальную проблему с бэкендом. Эта версия исправляет панику.
func isBenignError(err error) bool {
	if err == nil {
		return true // Отсутствие ошибки.
	}

	// 1. Проверяем на отмену контекста. Это всегда безопасно.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	// 2. Идиоматическая проверка на 404 Not Found для AWS SDK v2.
	// Это самый надежный способ.
	var notFoundError *types.NotFound
	if errors.As(err, &notFoundError) {
		return true // Это точно 404 Not Found, ошибка безопасна.
	}

	// 3. Общий fallback: проверяем всю цепочку на наличие любого типа,
	// который может сообщить HTTP-код. Это делает функцию устойчивой
	// к другим типам ошибок, которые могут вернуть 404.
	var httpErr interface{ HTTPStatusCode() int }
	if errors.As(err, &httpErr) {
		if httpErr.HTTPStatusCode() == http.StatusNotFound {
			return true
		}
	}

	// Все остальные ошибки (5xx, 403 Forbidden, сетевые проблемы) считаются критическими.
	return false
}

// ReportSuccess сообщает об успешной операции.
// Если бэкенд был в состоянии Down, эта функция возвращает его в строй.
func (m *Manager) ReportSuccess(result *BackendResult) {
	m.mu.RLock()
	backend, exists := m.backends[result.BackendID]
	m.mu.RUnlock()

	if !exists {
		logger.Warn("ReportSuccess: backend '%s' not found", result.BackendID)
		return
	}

	backend.mu.Lock()
	defer backend.mu.Unlock()

	// Сбрасываем счетчики неудач
	backend.consecutiveFailures = 0
	backend.consecutiveSuccesses++
	backend.recentFailures = 0 // Успех сбрасывает окно Circuit Breaker

	// Если бэкенд был отключен, успешный запрос возвращает его в строй.
	if backend.state == StateDown {
		logger.Info("Backend '%s' is back online after a successful request.", result.BackendID)
		setBackendState(m, backend, StateUp)
	}

	logger.Debug("ReportSuccess: backend '%s', consecutive successes: %d",
		result.BackendID, backend.consecutiveSuccesses)

	// Обновляем метрики
	m.metrics.BackendRequestsTotal.WithLabelValues(result.BackendID, result.Method, strconv.Itoa(result.StatusCode)).Inc()
	m.metrics.BackendLatency.WithLabelValues(result.BackendID, result.Method).Observe(float64(result.Duration.Seconds()))
	m.metrics.BackendBytesRead.WithLabelValues(result.BackendID).Add(float64(result.BytesRead))
	m.metrics.BackendBytesWrite.WithLabelValues(result.BackendID).Add(float64(result.BytesWritten))
}

// ReportFailure сообщает о неудачной операции, учитывая тип ошибки.
func (m *Manager) ReportFailure(result *BackendResult) {
	m.mu.RLock()
	backend, exists := m.backends[result.BackendID]
	m.mu.RUnlock()

	if !exists {
		logger.Warn("ReportFailure: backend '%s' not found", result.BackendID)
		return
	}

	// --- Новая логика классификации ошибки ---
	if isBenignError(result.Err) {
		// Это "безопасная" ошибка. Мы логируем ее, но не наказываем бэкенд.
		logger.Debug("ReportFailure: Benign error on backend '%s', not affecting circuit breaker. Error: %v",
			result.BackendID, result.Err)
		// Все равно обновляем метрики, так как запрос был
		m.metrics.BackendRequestsTotal.WithLabelValues(result.BackendID, result.Method, strconv.Itoa(result.StatusCode)).Inc()
		m.metrics.BackendLatency.WithLabelValues(result.BackendID, result.Method).Observe(float64(result.Duration.Seconds()))
		return // ВАЖНО: выходим, не трогая счетчики Circuit Breaker
	}

	// --- Логика для КРИТИЧЕСКИХ ошибок (старая логика) ---
	backend.mu.Lock()
	defer backend.mu.Unlock()

	backend.consecutiveSuccesses = 0
	backend.consecutiveFailures++
	backend.lastError = result.Err

	// Обновляем окно Circuit Breaker
	now := time.Now()
	if now.Sub(backend.windowStart) > m.config.CircuitBreakerWindow {
		backend.recentFailures = 1
		backend.windowStart = now
	} else {
		backend.recentFailures++
	}

	logger.Warn("ReportFailure: Critical failure on backend '%s', consecutive: %d, recent: %d. Error: %v",
		result.BackendID, backend.consecutiveFailures, backend.recentFailures, result.Err)

	// Проверяем, не пора ли отключить бэкенд
	if backend.state != StateDown && backend.recentFailures >= m.config.CircuitBreakerThreshold {
		logger.Error("Circuit breaker triggered for backend '%s': %d failures in %v. Setting state to DOWN.",
			result.BackendID, backend.recentFailures, now.Sub(backend.windowStart))
		setBackendState(m, backend, StateDown)
	}

	// Обновляем метрики
	m.metrics.BackendRequestsTotal.WithLabelValues(result.BackendID, result.Method, strconv.Itoa(result.StatusCode)).Inc()
	m.metrics.BackendLatency.WithLabelValues(result.BackendID, result.Method).Observe(float64(result.Duration.Seconds()))
	m.metrics.BackendBytesRead.WithLabelValues(result.BackendID).Add(float64(result.BytesRead))
	m.metrics.BackendBytesWrite.WithLabelValues(result.BackendID).Add(float64(result.BytesWritten))
}

// runHealthChecks выполняет активные проверки здоровья в фоновом режиме
func (m *Manager) runHealthChecks() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.config.HealthCheckInterval)
	defer ticker.Stop()

	logger.Debug("Doing initial health check")
	m.performHealthChecks()

	logger.Debug("Health check routine started with interval %v", m.config.HealthCheckInterval)
	for {
		select {
		case <-ticker.C:
			m.performHealthChecks()
		case <-m.stopChan:
			logger.Debug("Health check routine stopped")
			return
		}
	}
}

// performHealthChecks выполняет проверку всех бэкендов
func (m *Manager) performHealthChecks() {
	m.mu.RLock()
	backends := make([]*Backend, 0, len(m.backends))
	for _, backend := range m.backends {
		backends = append(backends, backend)
	}
	m.mu.RUnlock()

	logger.Debug("Performing health checks for %d backends", len(backends))

	// Проверяем каждый бэкенд асинхронно
	var wg sync.WaitGroup
	for _, backend := range backends {
		wg.Add(1)
		go func(b *Backend) {
			defer wg.Done()
			m.checkBackend(b)
		}(backend)
	}

	wg.Wait()
	logger.Debug("Health checks completed")
}

// checkBackend выполняет проверку одного бэкенда
func (m *Manager) checkBackend(backend *Backend) {
	ctx, cancel := context.WithTimeout(context.Background(), m.config.CheckTimeout)
	defer cancel()

	logger.Debug("Checking backend %s (state: %s)", backend.ID, backend.GetState())

	// Выполняем легковесную проверку - HeadBucket
	_, err := backend.S3Client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(backend.Config.Bucket),
	})

	// Обновляем состояние бэкенда
	backend.mu.Lock()
	defer backend.mu.Unlock()

	backend.lastCheckTime = time.Now()
	oldState := backend.state

	if err != nil {
		// Неудачная проверка
		backend.lastError = err
		backend.consecutiveSuccesses = 0
		backend.consecutiveFailures++

		logger.Debug("Backend %s health check failed: %v (consecutive failures: %d)",
			backend.ID, err, backend.consecutiveFailures)

		// Логика переходов состояний при неудаче
		switch backend.state {
		case StateUp:
			if backend.consecutiveFailures >= m.config.FailureThreshold {
				setBackendState(m, backend, StateDown)
				logger.Warn("Backend %s transitioned from UP to DOWN after %d consecutive failures",
					backend.ID, backend.consecutiveFailures)
			}
		case StateProbing:
			// Из PROBING сразу в DOWN при любой неудаче
			setBackendState(m, backend, StateDown)
			logger.Warn("Backend %s transitioned from PROBING to DOWN after health check failure", backend.ID)
		case StateDown:
			// Остаемся в DOWN
		}
	} else {
		// Успешная проверка
		backend.lastError = nil
		backend.consecutiveFailures = 0
		backend.consecutiveSuccesses++

		logger.Debug("Backend %s health check succeeded (consecutive successes: %d)",
			backend.ID, backend.consecutiveSuccesses)

		// Логика переходов состояний при успехе
		switch backend.state {
		case StateDown:
			// Из DOWN в PROBING при первом успехе
			setBackendState(m, backend, StateProbing)
			logger.Info("Backend %s transitioned from DOWN to PROBING after successful health check", backend.ID)
		case StateProbing:
			if backend.consecutiveSuccesses >= m.config.SuccessThreshold {
				setBackendState(m, backend, StateUp)
				logger.Info("Backend %s transitioned from PROBING to UP after %d consecutive successes",
					backend.ID, backend.consecutiveSuccesses)
			}
		case StateUp:
			// Остаемся в UP
		}
	}

	// Логируем изменения состояния
	if oldState != backend.state {
		logger.Info("Backend %s state changed: %s -> %s", backend.ID, oldState, backend.state)
	}
}

func setBackendState(m *Manager, backend *Backend, state BackendState) {
	backend.state = state
	m.metrics.BackendState.WithLabelValues(backend.ID).Set(backend.state.ToFloat64())
}
