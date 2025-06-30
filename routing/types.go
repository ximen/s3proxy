package routing

import (
	"context"

	"s3proxy/apigw"
)

// WriteOperationPolicy определяет политику для операций записи
type WriteOperationPolicy struct {
	// AckLevel определяет, сколько подтверждений ждать
	// Возможные значения: "none", "one", "all"
	AckLevel string `yaml:"ack"`
}

// ReadOperationPolicy определяет политику для операций чтения
type ReadOperationPolicy struct {
	// Strategy определяет, как выбрать бэкенд для чтения
	// Возможные значения: "first", "newest"
	Strategy string `yaml:"strategy"`
}

// ReplicationExecutor - интерфейс для модуля, выполняющего запись на бэкенды
type ReplicationExecutor interface {
	// PutObject выполняет операцию PUT в соответствии с политикой
	PutObject(ctx context.Context, req *apigw.S3Request, policy WriteOperationPolicy) *apigw.S3Response
	
	// DeleteObject выполняет операцию DELETE в соответствии с политикой
	DeleteObject(ctx context.Context, req *apigw.S3Request, policy WriteOperationPolicy) *apigw.S3Response
	
	// CreateMultipartUpload выполняет инициацию multipart upload
	CreateMultipartUpload(ctx context.Context, req *apigw.S3Request, policy WriteOperationPolicy) *apigw.S3Response
	
	// UploadPart выполняет загрузку части multipart upload
	UploadPart(ctx context.Context, req *apigw.S3Request, policy WriteOperationPolicy) *apigw.S3Response
	
	// CompleteMultipartUpload завершает multipart upload
	CompleteMultipartUpload(ctx context.Context, req *apigw.S3Request, policy WriteOperationPolicy) *apigw.S3Response
	
	// AbortMultipartUpload отменяет multipart upload
	AbortMultipartUpload(ctx context.Context, req *apigw.S3Request, policy WriteOperationPolicy) *apigw.S3Response
}

// FetchingExecutor - интерфейс для модуля, выполняющего чтение с бэкендов
type FetchingExecutor interface {
	// GetObject выполняет операцию GET в соответствии с политикой
	GetObject(ctx context.Context, req *apigw.S3Request, policy ReadOperationPolicy) *apigw.S3Response

	// HeadObject выполняет операцию HEAD в соответствии с политикой
	HeadObject(ctx context.Context, req *apigw.S3Request, policy ReadOperationPolicy) *apigw.S3Response

	// HeadBucket выполняет операцию HEAD BUCKET для проверки существования бакета
	HeadBucket(ctx context.Context, req *apigw.S3Request) *apigw.S3Response

	// ListObjects выполняет операцию LIST. У листинга своя, более сложная логика
	ListObjects(ctx context.Context, req *apigw.S3Request) *apigw.S3Response
	
	// ListBuckets выполняет операцию LIST BUCKETS
	ListBuckets(ctx context.Context, req *apigw.S3Request) *apigw.S3Response
	
	// ListMultipartUploads выполняет операцию LIST MULTIPART UPLOADS
	ListMultipartUploads(ctx context.Context, req *apigw.S3Request) *apigw.S3Response
}

// Policies содержит все политики для различных операций
type Policies struct {
	Put    WriteOperationPolicy `yaml:"put"`
	Delete WriteOperationPolicy `yaml:"delete"`
	Get    ReadOperationPolicy  `yaml:"get"`
}

// Config содержит конфигурацию для Policy & Routing Engine
type Config struct {
	Policies Policies `yaml:"policies"`
}

// DefaultConfig возвращает конфигурацию по умолчанию
func DefaultConfig() *Config {
	return &Config{
		Policies: Policies{
			Put: WriteOperationPolicy{
				AckLevel: "one",
			},
			Delete: WriteOperationPolicy{
				AckLevel: "all",
			},
			Get: ReadOperationPolicy{
				Strategy: "first",
			},
		},
	}
}
