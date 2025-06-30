# Makefile для S3 Proxy API Gateway

.PHONY: build test test-unit test-integration clean run help

# Переменные
BINARY_NAME=s3proxy
GO_FILES=$(shell find . -name "*.go" -type f)

# Цель по умолчанию
all: build

# Сборка приложения
build: $(BINARY_NAME)

$(BINARY_NAME): $(GO_FILES)
	@echo "Building $(BINARY_NAME)..."
	go build -o $(BINARY_NAME) .

# Запуск всех тестов
test:
	@echo "Running all tests..."
	go test -v -cover ./...

# Запуск только unit тестов
test-unit:
	@echo "Running unit tests..."
	go test -v -cover -run "^Test[^I]" ./...

# Запуск только интеграционных тестов
test-integration:
	@echo "Running integration tests..."
	go test -v -cover -run "Integration" ./...

# Запуск тестов с детальным покрытием
test-coverage:
	@echo "Running tests with coverage report..."
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Форматирование кода
fmt:
	@echo "Formatting code..."
	go fmt ./...

# Проверка кода
vet:
	@echo "Vetting code..."
	go vet ./...

# Линтинг (требует golangci-lint)
lint:
	@echo "Running linter..."
	golangci-lint run

# Запуск приложения
run: build
	@echo "Starting S3 Proxy API Gateway..."
	./$(BINARY_NAME)

# Запуск с кастомными параметрами
run-custom: build
	@echo "Starting S3 Proxy API Gateway with custom settings..."
	./$(BINARY_NAME) -listen :8080 -read-timeout 60s -write-timeout 60s

# Запуск с HTTPS (требует сертификаты)
run-https: build
	@echo "Starting S3 Proxy API Gateway with HTTPS..."
	./$(BINARY_NAME) -tls-cert server.crt -tls-key server.key

# Очистка
clean:
	@echo "Cleaning up..."
	rm -f $(BINARY_NAME)
	rm -f coverage.out coverage.html

# Инициализация модуля Go
init:
	@echo "Initializing Go module..."
	go mod init s3proxy
	go mod tidy

# Обновление зависимостей
deps:
	@echo "Updating dependencies..."
	go mod tidy
	go mod download

# Создание самоподписанного сертификата для тестирования HTTPS
cert:
	@echo "Generating self-signed certificate for testing..."
	openssl req -x509 -newkey rsa:4096 -keyout server.key -out server.crt -days 365 -nodes \
		-subj "/C=US/ST=Test/L=Test/O=Test/CN=localhost"

# Демонстрация API с помощью curl
demo: build
	@echo "Starting demo server in background..."
	./$(BINARY_NAME) &
	@sleep 2
	@echo "\n=== S3 Proxy API Gateway Demo ==="
	@echo "\n1. List buckets:"
	curl -s http://localhost:9000/ | head -10
	@echo "\n\n2. List objects in bucket:"
	curl -s http://localhost:9000/test-bucket/ | head -10
	@echo "\n\n3. Get object:"
	curl -s http://localhost:9000/test-bucket/test-object.txt
	@echo "\n\n4. Put object:"
	curl -s -X PUT -d "Hello from S3 Proxy!" http://localhost:9000/test-bucket/new-object.txt
	@echo "\n\n5. Head object:"
	curl -s -I http://localhost:9000/test-bucket/test-object.txt | head -5
	@echo "\n\n6. Create multipart upload:"
	curl -s -X POST http://localhost:9000/test-bucket/large-file.bin?uploads | head -5
	@echo "\n\nDemo completed. Stopping server..."
	@pkill -f $(BINARY_NAME) || true

# Справка
help:
	@echo "Available targets:"
	@echo "  build           - Build the application"
	@echo "  test            - Run all tests"
	@echo "  test-unit       - Run unit tests only"
	@echo "  test-integration - Run integration tests only"
	@echo "  test-coverage   - Run tests with coverage report"
	@echo "  fmt             - Format code"
	@echo "  vet             - Vet code"
	@echo "  lint            - Run linter (requires golangci-lint)"
	@echo "  run             - Build and run the application"
	@echo "  run-custom      - Run with custom parameters"
	@echo "  run-https       - Run with HTTPS (requires certificates)"
	@echo "  clean           - Clean build artifacts"
	@echo "  init            - Initialize Go module"
	@echo "  deps            - Update dependencies"
	@echo "  cert            - Generate self-signed certificate"
	@echo "  demo            - Run API demonstration"
	@echo "  help            - Show this help"
