package fetch_test

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"

	"s3proxy/apigw"
	"s3proxy/backend"
	"s3proxy/fetch"
	"s3proxy/monitoring"
	"s3proxy/routing"
)

// Глобальный экземпляр метрик для примеров
var globalMetrics = monitoring.NewMetrics()

// ExampleFetcher демонстрирует основное использование Fetching Module
func ExampleFetcher() {
	// 1. Создание зависимостей
	
	// Backend Manager (в реальном приложении будет настроен с реальными бэкендами)
	backendConfig := &backend.Config{
		Manager: backend.DefaultManagerConfig(),
		Backends: map[string]backend.BackendConfig{
			"primary": {
				Endpoint:  "https://s3.amazonaws.com",
				Region:    "us-east-1",
				Bucket:    "my-primary-bucket",
				AccessKey: "AKIAIOSFODNN7EXAMPLE",
				SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			},
			"backup": {
				Endpoint:  "https://s3.eu-central-1.amazonaws.com",
				Region:    "eu-central-1",
				Bucket:    "my-backup-bucket",
				AccessKey: "AKIAIOSFODNN7EXAMPLE",
				SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			},
		},
	}
	
	backendManager, err := backend.NewManager(backendConfig, globalMetrics)
	if err != nil {
		log.Fatal(err)
	}
	
	// Cache (используем заглушку)
	cache := fetch.NewStubCache()
	
	// 2. Создание Fetcher
	fetcher := fetch.NewFetcher(backendManager, cache, globalMetrics)
	
	// 3. Создание тестового запроса
	req := &apigw.S3Request{
		Operation: apigw.GetObject,
		Bucket:    "my-bucket",
		Key:       "my-object.txt",
		Headers:   make(http.Header),
		Query:     make(url.Values),
		Context:   context.Background(),
	}
	
	// 4. Выполнение GET запроса с стратегией "first"
	policy := routing.ReadOperationPolicy{Strategy: "first"}
	response := fetcher.GetObject(context.Background(), req, policy)
	
	fmt.Printf("GET Object response status: %d\n", response.StatusCode)
	
	// 5. Выполнение HEAD запроса с стратегией "newest"
	policy = routing.ReadOperationPolicy{Strategy: "newest"}
	response = fetcher.HeadObject(context.Background(), req, policy)
	
	fmt.Printf("HEAD Object response status: %d\n", response.StatusCode)
	
	// 6. Выполнение LIST запроса
	listReq := &apigw.S3Request{
		Operation: apigw.ListObjectsV2,
		Bucket:    "my-bucket",
		Key:       "",
		Headers:   make(http.Header),
		Query:     make(url.Values),
		Context:   context.Background(),
	}
	
	response = fetcher.ListObjects(context.Background(), listReq)
	fmt.Printf("LIST Objects response status: %d\n", response.StatusCode)
	
	// Output:
	// GET Object response status: 404
	// HEAD Object response status: 404
	// LIST Objects response status: 200
}

// ExampleFetcher_withCache демонстрирует работу с кэшем
func ExampleFetcher_withCache() {
	// Создание зависимостей
	backendConfig := &backend.Config{
		Manager: backend.DefaultManagerConfig(),
		Backends: map[string]backend.BackendConfig{
			"test": {
				Endpoint:  "https://s3.amazonaws.com",
				Region:    "us-east-1",
				Bucket:    "test-bucket",
				AccessKey: "test-key",
				SecretKey: "test-secret",
			},
		},
	}
	
	backendManager, _ := backend.NewManager(backendConfig, globalMetrics)
	cache := fetch.NewStubCache() // Заглушка всегда возвращает "не найдено"
	
	fetcher := fetch.NewFetcher(backendManager, cache, globalMetrics)
	
	// Запрос к объекту (кэш промахнется, пойдем к бэкендам)
	req := &apigw.S3Request{
		Operation: apigw.GetObject,
		Bucket:    "test-bucket",
		Key:       "test-object.txt",
		Headers:   make(http.Header),
		Query:     make(url.Values),
		Context:   context.Background(),
	}
	
	policy := routing.ReadOperationPolicy{Strategy: "first"}
	response := fetcher.GetObject(context.Background(), req, policy)
	
	fmt.Printf("Cache miss response status: %d\n", response.StatusCode)
	
	// Output:
	// Cache miss response status: 404
}

// ExampleFetcher_strategies демонстрирует различные стратегии чтения
func ExampleFetcher_strategies() {
	backendConfig := &backend.Config{
		Manager: backend.DefaultManagerConfig(),
		Backends: map[string]backend.BackendConfig{
			"primary": {
				Endpoint:  "https://s3.amazonaws.com",
				Region:    "us-east-1",
				Bucket:    "primary-bucket",
				AccessKey: "test-key",
				SecretKey: "test-secret",
			},
		},
	}
	
	backendManager, _ := backend.NewManager(backendConfig, globalMetrics)
	cache := fetch.NewStubCache()
	
	fetcher := fetch.NewFetcher(backendManager, cache, globalMetrics)
	
	req := &apigw.S3Request{
		Operation: apigw.GetObject,
		Bucket:    "test-bucket",
		Key:       "test-object.txt",
		Headers:   make(http.Header),
		Query:     make(url.Values),
		Context:   context.Background(),
	}
	
	// Стратегия "first" - возвращает ответ от первого успешно ответившего бэкенда
	firstPolicy := routing.ReadOperationPolicy{Strategy: "first"}
	response := fetcher.GetObject(context.Background(), req, firstPolicy)
	fmt.Printf("First strategy response status: %d\n", response.StatusCode)
	
	// Стратегия "newest" - возвращает самую новую версию объекта
	newestPolicy := routing.ReadOperationPolicy{Strategy: "newest"}
	response = fetcher.GetObject(context.Background(), req, newestPolicy)
	fmt.Printf("Newest strategy response status: %d\n", response.StatusCode)
	
	// Output:
	// First strategy response status: 404
	// Newest strategy response status: 404
}

// ExampleProxyContinuationToken демонстрирует работу с токенами пагинации
func ExampleProxyContinuationToken() {
	// Создание токена пагинации для нескольких бэкендов
	token := fetch.ProxyContinuationToken{
		BackendTokens: map[string]string{
			"backend1": "token-for-backend1",
			"backend2": "token-for-backend2",
			"backend3": "token-for-backend3",
		},
	}
	
	fmt.Printf("Backend tokens count: %d\n", len(token.BackendTokens))
	fmt.Printf("Backend1 token: %s\n", token.BackendTokens["backend1"])
	fmt.Printf("Backend2 token: %s\n", token.BackendTokens["backend2"])
	
	// Output:
	// Backend tokens count: 3
	// Backend1 token: token-for-backend1
	// Backend2 token: token-for-backend2
}
