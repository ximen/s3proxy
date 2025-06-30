package monitoring

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"

	"s3proxy/backend"
	"s3proxy/logger"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Server представляет HTTP сервер для экспорта метрик Prometheus
type Server struct {
	config            *Config
	server            *http.Server
	backendManager    *backend.Manager
	shuttingDown      atomic.Bool

	// Канал для остановки сбора системных метрик
	stopSystemMetrics chan struct{}
}

// NewServer создает новый сервер метрик
func NewServer(config *Config, backendManager *backend.Manager) *Server {
	if config == nil {
		config = DefaultConfig()
	}

	s := &Server{
		config:            config,
		backendManager:    backendManager,
		stopSystemMetrics: make(chan struct{}),
	}
	s.shuttingDown.Store(false)
	return s
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
	mux.HandleFunc("/health/live", s.liveHealthHandler)
	mux.HandleFunc("/health/ready", s.readyHealthHandler)

	// Создаем HTTP сервер
	s.server = &http.Server{
		Addr:         s.config.ListenAddress,
		Handler:      mux,
		ReadTimeout:  s.config.ReadTimeout,
		WriteTimeout: s.config.WriteTimeout,
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

// liveHealthHandler обрабатывает запросы /health/live
func (s *Server) liveHealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"ok"}`)
}

// readyHealthHandler обрабатывает запросы /health/ready
func (s *Server) readyHealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Проверяем, не находимся ли мы в состоянии graceful shutdown
	if s.shuttingDown.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, `{"status":"shutting down"}`)
		return
	}

	// Проверяем, есть ли живые бэкенды (если backendManager доступен)
	if s.backendManager != nil && len(s.backendManager.GetLiveBackends()) == 0 {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, `{"status":"no live backends"}`)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"ok"}`)
}
