package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"s3proxy/apigw"
	"s3proxy/auth"
	"s3proxy/backend"
	"s3proxy/fetch"
	"s3proxy/handlers"
	"s3proxy/logger"
	"s3proxy/monitoring"
	"s3proxy/replicator"
	"s3proxy/routing"
)

func main() {
	// Парсим аргументы командной строки
	var (
		configFile      = flag.String("config", "", "Configuration file path (YAML)")
		listenAddr      = flag.String("listen", "", "Listen address (overrides config)")
		tlsCert         = flag.String("tls-cert", "", "TLS certificate file (overrides config)")
		tlsKey          = flag.String("tls-key", "", "TLS key file (overrides config)")
		readTimeout     = flag.Duration("read-timeout", 0, "Read timeout (overrides config)")
		writeTimeout    = flag.Duration("write-timeout", 0, "Write timeout (overrides config)")
		useMock         = flag.Bool("mock", false, "Use mock handler instead of policy routing engine (overrides config)")
		logLevel        = flag.String("log-level", "", "Log level (debug, info, warn, error) (overrides config)")
		metricsAddr     = flag.String("metrics-listen", "", "Metrics server listen address (overrides config)")
		disableMetrics  = flag.Bool("disable-metrics", false, "Disable metrics collection (overrides config)")
		disableBackends = flag.Bool("disable-backends", false, "Disable backend manager (use mock backends)")
	)
	flag.Parse()

	// Загружаем конфигурацию
	var config *AppConfig
	var err error

	if *configFile != "" {
		logger.Info("Loading configuration from file: %s", *configFile)
		config, err = LoadConfig(*configFile)
		if err != nil {
			log.Fatalf("Failed to load configuration: %v", err)
		}
		logger.Info("Configuration loaded successfully")
	} else {
		logger.Error("Config file not provided or incorrect. Exiting.")
		os.Exit(1)
	}

	// Применяем переопределения из командной строки
	applyCommandLineOverrides(config,
		*listenAddr, *tlsCert, *tlsKey, *readTimeout, *writeTimeout,
		*useMock, *logLevel, *metricsAddr, *disableMetrics)

	// Устанавливаем уровень логирования
	level := logger.ParseLogLevel(config.Logging.Level)
	logger.SetGlobalLevel(level)

	logger.Info("S3 Proxy API Gateway starting...")
	logger.Info("Log level: %s", level.String())

	// Создаем и запускаем модуль мониторинга
	var monitor *monitoring.Monitor
	if !*disableMetrics && config.Monitoring.Enabled {
		monitor, err = monitoring.New(&config.Monitoring)
		if err != nil {
			log.Fatalf("Failed to create monitoring module: %v", err)
		}

		err = monitor.Start()
		if err != nil {
			log.Fatalf("Failed to start monitoring module: %v", err)
		}

		logger.Info("Monitoring enabled on %s", config.Monitoring.ListenAddress)
	} else {
		logger.Info("Monitoring disabled")
	}

	// Получаем метрики для передачи в модули (если мониторинг включен)
	// TODO: Метрики должны определяться в модулях, а не в mnitoring
	// var metrics *monitoring.Metrics
	// if monitor != nil {
	// 	metrics = monitor.GetMetrics()
	// }

	// Создаем и запускаем backend manager
	var backendManager *backend.Manager
	if !*disableBackends {
		backendManager, err = backend.NewManager(&config.Backend)
		if err != nil {
			log.Fatalf("Failed to create backend manager: %v", err)
		}

		err = backendManager.Start()
		if err != nil {
			log.Fatalf("Failed to start backend manager: %v", err)
		}

		logger.Info("Backend manager enabled with %d backends", len(backendManager.GetAllBackends()))

		// Логируем информацию о бэкендах
		for _, b := range backendManager.GetAllBackends() {
			logger.Info("  - %s: %s (bucket: %s)", b.ID, b.Config.Endpoint, b.Config.Bucket)
		}
	} else {
		logger.Info("Backend manager disabled")
	}

	// Создаем конфигурацию API Gateway
	gatewayConfig := config.ToAPIGatewayConfig()

	// Создаем обработчик в зависимости от конфигурации
	var handler apigw.RequestHandler
	if config.Server.UseMock {
		logger.Info("Using Mock Handler (for testing)")
		handler = handlers.NewMockHandler()
	} else {
		logger.Info("Using Policy & Routing Engine")

		// Создаем аутентификатор
		authenticator, err := auth.NewAuthenticatorFromConfig(&config.Auth)
		if err != nil {
			log.Fatalf("Failed to create authenticator: %v", err)
		}

		// Логируем доступные учетные данные
		logger.Info("Authentication configured with %d users:", len(config.Auth.Static.Users))
		for _, user := range config.Auth.Static.Users {
			logger.Info("  - %s (%s)", user.DisplayName, user.AccessKey)
		}

		// Создаем реальные исполнители
		// Replicator для операций записи
		replicatorConfig := replicator.DefaultConfig() // Используем конфигурацию по умолчанию для replicator
		//backendAdapter := replicator.NewBackendAdapter(backendManager)
		replicatorInstance := replicator.NewReplicator(backendManager, replicatorConfig)

		// Fetcher для операций чтения
		cache := fetch.NewStubCache() // Пока используем заглушку кэша
		fetcherInstance := fetch.NewFetcher(backendManager, cache, config.Server.VirtualBucket)

		// Логируем политики маршрутизации``
		logger.Info("Routing policies configured:")
		logger.Info("  PUT operations: ack=%s", config.Routing.Policies.Put.AckLevel)
		logger.Info("  DELETE operations: ack=%s", config.Routing.Policies.Delete.AckLevel)
		logger.Info("  GET operations: strategy=%s", config.Routing.Policies.Get.Strategy)

		// Создаем Policy & Routing Engine
		engine := routing.NewEngine(authenticator, replicatorInstance, fetcherInstance, &config.Routing)
		handler = engine
	}

	// Создаем и запускаем API Gateway
	gateway := apigw.New(gatewayConfig, handler)

	logger.Info("Configuration:")
	logger.Info("  Listen Address: %s", gatewayConfig.ListenAddress)
	logger.Info("  Read Timeout: %v", gatewayConfig.ReadTimeout)
	logger.Info("  Write Timeout: %v", gatewayConfig.WriteTimeout)
	if gatewayConfig.TLSCertFile != "" {
		logger.Info("  TLS Enabled: Yes")
		logger.Info("  TLS Cert: %s", gatewayConfig.TLSCertFile)
		logger.Info("  TLS Key: %s", gatewayConfig.TLSKeyFile)
	} else {
		logger.Info("  TLS Enabled: No")
	}

	// Настраиваем graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Запускаем API Gateway в отдельной горутине
	go func() {
		if err := gateway.Start(); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	logger.Info("S3 Proxy started successfully")
	if monitor != nil && monitor.IsEnabled() {
		logger.Info("Metrics available at: %s", config.Monitoring.ListenAddress)
	}
	if backendManager != nil && backendManager.IsRunning() {
		logger.Info("Backend manager running with %d backends", len(backendManager.GetAllBackends()))
	}

	// Ждем сигнал для остановки
	sig := <-sigChan
	logger.Info("Received signal %v, shutting down...", sig)

	// Создаем контекст с таймаутом для graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Останавливаем API Gateway
	if err := gateway.Stop(ctx); err != nil {
		logger.Error("Error stopping API Gateway: %v", err)
	}

	// Останавливаем backend manager
	if backendManager != nil {
		if err := backendManager.Stop(); err != nil {
			logger.Error("Error stopping backend manager: %v", err)
		}
	}

	// Останавливаем мониторинг
	if monitor != nil {
		if err := monitor.Stop(ctx); err != nil {
			logger.Error("Error stopping monitoring: %v", err)
		}
	}

	logger.Info("S3 Proxy stopped")
}

// applyCommandLineOverrides применяет переопределения из командной строки
func applyCommandLineOverrides(config *AppConfig,
	listenAddr, tlsCert, tlsKey string,
	readTimeout, writeTimeout time.Duration,
	useMock bool, logLevel, metricsAddr string, disableMetrics bool) {

	// Переопределения сервера
	if listenAddr != "" {
		config.Server.ListenAddress = listenAddr
		logger.Debug("Override: server.listen_address = %s", listenAddr)
	}

	if tlsCert != "" {
		config.Server.TLSCertFile = tlsCert
		logger.Debug("Override: server.tls_cert_file = %s", tlsCert)
	}

	if tlsKey != "" {
		config.Server.TLSKeyFile = tlsKey
		logger.Debug("Override: server.tls_key_file = %s", tlsKey)
	}

	if readTimeout > 0 {
		config.Server.ReadTimeout = readTimeout
		logger.Debug("Override: server.read_timeout = %v", readTimeout)
	}

	if writeTimeout > 0 {
		config.Server.WriteTimeout = writeTimeout
		logger.Debug("Override: server.write_timeout = %v", writeTimeout)
	}

	if useMock {
		config.Server.UseMock = true
		logger.Debug("Override: server.use_mock = true")
	}

	// Переопределения логирования
	if logLevel != "" {
		config.Logging.Level = logLevel
		logger.Debug("Override: logging.level = %s", logLevel)
	}

	// Переопределения мониторинга
	if metricsAddr != "" {
		config.Monitoring.ListenAddress = metricsAddr
		logger.Debug("Override: monitoring.listen_address = %s", metricsAddr)
	}

	if disableMetrics {
		config.Monitoring.Enabled = false
		logger.Debug("Override: monitoring.enabled = false")
	}
}
