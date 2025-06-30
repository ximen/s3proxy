package fetch

import (
	"s3proxy/apigw"
)

// Cache - интерфейс для взаимодействия с кэшем
type Cache interface {
	// Get ищет объект в кэше. Если нашел, возвращает готовый для отправки S3Response.
	Get(bucket, key string) (response *apigw.S3Response, found bool)
}

// Metrics - интерфейс для сбора метрик операций чтения
// type Metrics interface {
// 	// ObserveBackendRequestLatency записывает время выполнения запроса к бэкенду
// 	ObserveBackendRequestLatency(backendID, operation string, latency float64)
	
// 	// IncrementBackendRequestsTotal увеличивает счетчик запросов к бэкенду
// 	IncrementBackendRequestsTotal(backendID, operation, result string)
	
// 	// AddBackendBytesRead добавляет количество прочитанных байт
// 	AddBackendBytesRead(backendID string, bytes int64)
// }

// ProxyContinuationToken представляет токен пагинации для нескольких бэкендов
type ProxyContinuationToken struct {
	// BackendTokens содержит токены продолжения для каждого бэкенда
	BackendTokens map[string]string `json:"backend_tokens"`
}
