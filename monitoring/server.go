package monitoring

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"time"

	"s3proxy/logger"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Server представляет HTTP сервер для экспорта метрик Prometheus
type Server struct {
	config  *Config
	metrics *Metrics
	server  *http.Server

	// Канал для остановки сбора системных метрик
	stopSystemMetrics chan struct{}
}

// NewServer создает новый сервер метрик
func NewServer(config *Config, metrics *Metrics) *Server {
	if config == nil {
		config = DefaultConfig()
	}

	return &Server{
		config:            config,
		metrics:           metrics,
		stopSystemMetrics: make(chan struct{}),
	}
}

// Start запускает HTTP сервер для метрик
func (s *Server) Start() error {
	if !s.config.Enabled {
		logger.Info("Monitoring is disabled, skipping metrics server start")
		return nil
	}

	logger.Info("Starting metrics server on %s", s.config.ListenAddress)

	// Создаем HTTP мультиплексор
	mux := http.NewServeMux()

	// Регистрируем обработчик метрик
	mux.Handle(s.config.MetricsPath, promhttp.Handler())

	// Добавляем health check эндпоинты
	mux.HandleFunc("/health", s.healthHandler)
	mux.HandleFunc("/health/live", s.liveHealthHandler)
	mux.HandleFunc("/health/ready", s.readyHealthHandler)

	// Создаем HTTP сервер
	s.server = &http.Server{
		Addr:         s.config.ListenAddress,
		Handler:      mux,
		ReadTimeout:  s.config.ReadTimeout,
		WriteTimeout: s.config.WriteTimeout,
	}

	// Запускаем сбор системных метрик в отдельной горутине
	if s.config.EnableSystemMetrics {
		go s.collectSystemMetrics()
	}

	// Запускаем сервер в отдельной горутине
	go func() {
		logger.Info("Metrics server listening on %s%s", s.config.ListenAddress, s.config.MetricsPath)
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Metrics server failed: %v", err)
		}
	}()

	return nil
}

// Stop останавливает HTTP сервер метрик
func (s *Server) Stop(ctx context.Context) error {
	if !s.config.Enabled || s.server == nil {
		return nil
	}

	logger.Info("Stopping metrics server...")

	// Останавливаем сбор системных метрик
	close(s.stopSystemMetrics)

	// Останавливаем HTTP сервер
	return s.server.Shutdown(ctx)
}

// healthHandler обрабатывает запросы health check
func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"ok","service":"s3proxy-monitoring"}`)
}

// liveHealthHandler обрабатывает запросы /health/live
func (s *Server) liveHealthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "{\"status\": \"OK\"}")
}

// liveHealthHandler обрабатывает запросы /health/ready
func (s *Server) readyHealthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "{\"status\": \"OK\"}")
}

// collectSystemMetrics собирает системные метрики в фоновом режиме
func (s *Server) collectSystemMetrics() {
	logger.Debug("Starting system metrics collection with interval %v", s.config.SystemMetricsInterval)

	ticker := time.NewTicker(s.config.SystemMetricsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.updateSystemMetrics()
		case <-s.stopSystemMetrics:
			logger.Debug("Stopping system metrics collection")
			return
		}
	}
}

// updateSystemMetrics обновляет системные метрики
func (s *Server) updateSystemMetrics() {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// Обновляем метрику использования памяти
	s.metrics.MemoryUsage.Set(float64(memStats.Alloc))

	logger.Debug("Updated system metrics: memory_usage=%d bytes", memStats.Alloc)
}

// GetMetricsURL возвращает полный URL эндпоинта метрик
func (s *Server) GetMetricsURL() string {
	if !s.config.Enabled {
		return ""
	}
	return fmt.Sprintf("http://localhost%s%s", s.config.ListenAddress, s.config.MetricsPath)
}
