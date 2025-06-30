package apigw

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"s3proxy/logger"
)

// Gateway представляет модуль API Gateway
type Gateway struct {
	config         Config
	handler        RequestHandler
	parser         *RequestParser
	responseWriter *ResponseWriter
	server         *http.Server // Добавляем поле для сервера
	metrics        *Metrics     // Добавляем поле для метрик
}

// New создает новый экземпляр API Gateway
func New(config Config, handler RequestHandler) *Gateway {
	return &Gateway{
		config:         config,
		handler:        handler,
		parser:         NewRequestParser(),
		responseWriter: NewResponseWriter(),
		metrics:        NewMetrics(),
	}
}

// ServeHTTP реализует интерфейс http.Handler
func (gw *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var latency float64
	// Логируем входящий запрос
	logger.Info("Incoming request: %s %s", r.Method, r.URL.Path)
	logger.Debug("Request headers: %+v", r.Header)

	// Парсим запрос
	s3req, err := gw.parser.Parse(r)
	if err != nil {
		logger.Error("Failed to parse request: %v", err)
		// Создаем ответ об ошибке парсинга
		s3resp := &S3Response{
			StatusCode: http.StatusBadRequest,
			Error:      fmt.Errorf("invalid request: %v", err),
		}
		gw.responseWriter.WriteResponse(w, s3resp)

		latency := time.Since(start).Seconds()
		gw.metrics.RequestsTotal.WithLabelValues(r.Method, strconv.Itoa(s3resp.StatusCode)).Inc()
		gw.metrics.RequestLatency.WithLabelValues(r.Method).Observe(latency)
		return
	}

	// Логируем распарсенную операцию
	logger.Debug("Parsed S3 request: %+v", s3req)
	logger.Debug("Parsed operation: %s, Bucket: %s, Key: %s",
		s3req.Operation.String(), s3req.Bucket, s3req.Key)

	// Передаем управление обработчику
	s3resp := gw.handler.Handle(s3req)
	logger.Debug("Handler response: %+v", s3resp)

	// Отправляем ответ клиенту
	if err := gw.responseWriter.WriteResponse(w, s3resp); err != nil {
		logger.Error("Failed to write response: %v", err)
	}

	// Логируем ответ
	logger.Info("Response sent: %d, %.3f ms", s3resp.StatusCode, float64(time.Since(start).Microseconds())/1000.0)

	// Updateing metric
	latency = time.Since(start).Seconds()
	gw.metrics.RequestsTotal.WithLabelValues(r.Method, strconv.Itoa(s3resp.StatusCode)).Inc()
	gw.metrics.RequestLatency.WithLabelValues(r.Method).Observe(latency)
}

// Start запускает сервер
func (gw *Gateway) Start() error {

	// HTTP server
	gw.server = &http.Server{
		Addr:         gw.config.ListenAddress,
		Handler:      gw,
		ReadTimeout:  gw.config.ReadTimeout,
		WriteTimeout: gw.config.WriteTimeout,
	}

	logger.Info("Starting API Gateway on %s", gw.config.ListenAddress)

	// Проверяем, нужно ли использовать TLS
	if gw.config.TLSCertFile != "" && gw.config.TLSKeyFile != "" {
		logger.Info("Starting HTTPS server with TLS")
		return gw.server.ListenAndServeTLS(gw.config.TLSCertFile, gw.config.TLSKeyFile)
	}

	logger.Info("Starting HTTP server")
	return gw.server.ListenAndServe()
}

// Stop останавливает сервер
func (gw *Gateway) Stop(ctx context.Context) error {
	if gw.server == nil {
		return nil
	}

	logger.Info("Stopping API Gateway...")
	return gw.server.Shutdown(ctx)
}
