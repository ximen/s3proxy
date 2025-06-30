# Примеры использования S3 Proxy API Gateway

## Запуск сервера

### Базовый запуск
```bash
./s3proxy
```

### Запуск с кастомными параметрами
```bash
./s3proxy -listen :8080 -read-timeout 60s -write-timeout 60s
```

### Запуск с HTTPS
```bash
# Сначала создайте сертификаты
make cert

# Затем запустите с TLS
./s3proxy -tls-cert server.crt -tls-key server.key
```

## Тестирование API с curl

### Операции с бакетами

#### Список всех бакетов
```bash
curl -v http://localhost:9000/
```

**Ответ:**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
    <Owner>
        <ID>mock-owner-id</ID>
        <DisplayName>mock-owner</DisplayName>
    </Owner>
    <Buckets>
        <Bucket>
            <Name>example-bucket</Name>
            <CreationDate>2025-06-20T20:00:00.000Z</CreationDate>
        </Bucket>
    </Buckets>
</ListAllMyBucketsResult>
```

### Операции с объектами

#### Список объектов в бакете
```bash
curl -v http://localhost:9000/my-bucket/
```

**Ответ:**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
    <Name>my-bucket</Name>
    <Contents>
        <Key>example-object.txt</Key>
        <LastModified>2025-06-20T20:00:00.000Z</LastModified>
        <ETag>"mock-etag-example"</ETag>
        <Size>100</Size>
    </Contents>
</ListBucketResult>
```

#### Загрузка объекта
```bash
curl -X PUT \
     -H "Content-Type: text/plain" \
     -d "Hello, World!" \
     http://localhost:9000/my-bucket/hello.txt
```

#### Получение объекта
```bash
curl http://localhost:9000/my-bucket/hello.txt
```

**Ответ:**
```
Mock content for object my-bucket/hello.txt
```

#### Получение метаданных объекта
```bash
curl -I http://localhost:9000/my-bucket/hello.txt
```

**Ответ:**
```
HTTP/1.1 200 OK
Content-Type: text/plain
Content-Length: 100
ETag: "mock-etag-12345"
Last-Modified: Wed, 20 Jun 2025 20:00:00 GMT
```

#### Удаление объекта
```bash
curl -X DELETE http://localhost:9000/my-bucket/hello.txt
```

### Multipart Upload операции

#### 1. Инициация multipart upload
```bash
curl -X POST http://localhost:9000/my-bucket/large-file.bin?uploads
```

**Ответ:**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<InitiateMultipartUploadResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
    <Bucket>my-bucket</Bucket>
    <Key>large-file.bin</Key>
    <UploadId>mock-upload-id-12345</UploadId>
</InitiateMultipartUploadResult>
```

#### 2. Загрузка части
```bash
curl -X PUT \
     -d "Part 1 data content here..." \
     "http://localhost:9000/my-bucket/large-file.bin?partNumber=1&uploadId=mock-upload-id-12345"
```

#### 3. Загрузка еще одной части
```bash
curl -X PUT \
     -d "Part 2 data content here..." \
     "http://localhost:9000/my-bucket/large-file.bin?partNumber=2&uploadId=mock-upload-id-12345"
```

#### 4. Завершение multipart upload
```bash
curl -X POST \
     -H "Content-Type: application/xml" \
     -d '<CompleteMultipartUpload>
           <Part>
             <PartNumber>1</PartNumber>
             <ETag>"etag1"</ETag>
           </Part>
           <Part>
             <PartNumber>2</PartNumber>
             <ETag>"etag2"</ETag>
           </Part>
         </CompleteMultipartUpload>' \
     "http://localhost:9000/my-bucket/large-file.bin?uploadId=mock-upload-id-12345"
```

#### 5. Отмена multipart upload
```bash
curl -X DELETE "http://localhost:9000/my-bucket/large-file.bin?uploadId=mock-upload-id-12345"
```

#### 6. Список активных multipart uploads
```bash
curl "http://localhost:9000/my-bucket/?uploads"
```

## Тестирование с AWS CLI

### Настройка AWS CLI для работы с локальным сервером

```bash
# Настройка профиля (используйте любые значения для тестирования)
aws configure set aws_access_key_id test-key
aws configure set aws_secret_access_key test-secret
aws configure set region us-east-1
```

### Примеры команд AWS CLI

#### Список бакетов
```bash
aws --endpoint-url http://localhost:9000 s3 ls
```

#### Список объектов в бакете
```bash
aws --endpoint-url http://localhost:9000 s3 ls s3://my-bucket/
```

#### Загрузка файла
```bash
echo "Test content" > test-file.txt
aws --endpoint-url http://localhost:9000 s3 cp test-file.txt s3://my-bucket/
```

#### Скачивание файла
```bash
aws --endpoint-url http://localhost:9000 s3 cp s3://my-bucket/test-file.txt downloaded-file.txt
```

## Тестирование с Python boto3

### Установка зависимостей
```bash
pip install boto3
```

### Пример кода
```python
import boto3
from botocore.config import Config

# Настройка клиента
s3_client = boto3.client(
    's3',
    endpoint_url='http://localhost:9000',
    aws_access_key_id='test-key',
    aws_secret_access_key='test-secret',
    config=Config(signature_version='s3v4'),
    region_name='us-east-1'
)

# Список бакетов
response = s3_client.list_buckets()
print("Buckets:", response['Buckets'])

# Список объектов в бакете
response = s3_client.list_objects_v2(Bucket='my-bucket')
print("Objects:", response.get('Contents', []))

# Загрузка объекта
s3_client.put_object(
    Bucket='my-bucket',
    Key='python-test.txt',
    Body=b'Hello from Python!'
)

# Получение объекта
response = s3_client.get_object(Bucket='my-bucket', Key='python-test.txt')
content = response['Body'].read()
print("Content:", content.decode())

# Получение метаданных
response = s3_client.head_object(Bucket='my-bucket', Key='python-test.txt')
print("Metadata:", response['Metadata'])

# Удаление объекта
s3_client.delete_object(Bucket='my-bucket', Key='python-test.txt')
```

## Тестирование ошибок

### Неподдерживаемый HTTP метод
```bash
curl -X PATCH http://localhost:9000/my-bucket/test.txt
```

**Ответ:**
```xml
<Error>
  <Code>InvalidRequest</Code>
  <Message>invalid request: unsupported HTTP method: PATCH</Message>
</Error>
```

### Некорректный URL
```bash
curl -X GET http://localhost:9000/
```

Этот запрос корректен и вернет список бакетов.

## Нагрузочное тестирование

### С помощью Apache Bench (ab)
```bash
# Тестирование GET запросов
ab -n 1000 -c 10 http://localhost:9000/my-bucket/test-object.txt

# Тестирование PUT запросов
ab -n 100 -c 5 -p test-data.txt -T text/plain http://localhost:9000/my-bucket/load-test.txt
```

### С помощью wrk
```bash
# Установка wrk (Ubuntu/Debian)
sudo apt-get install wrk

# Тестирование GET запросов
wrk -t4 -c100 -d30s http://localhost:9000/my-bucket/test-object.txt

# Тестирование с кастомным скриптом
wrk -t4 -c100 -d30s -s post.lua http://localhost:9000/my-bucket/
```

### Пример скрипта для wrk (post.lua)
```lua
wrk.method = "PUT"
wrk.body   = "Test data for load testing"
wrk.headers["Content-Type"] = "text/plain"
```

## Мониторинг и отладка

### Просмотр логов
```bash
# Запуск с выводом в файл
./s3proxy > s3proxy.log 2>&1 &

# Просмотр логов в реальном времени
tail -f s3proxy.log
```

### Проверка состояния сервера
```bash
# Проверка, что сервер отвечает
curl -I http://localhost:9000/

# Проверка с таймаутом
curl --max-time 5 http://localhost:9000/
```

### Отладка с verbose выводом
```bash
# Детальная информация о запросе/ответе
curl -v http://localhost:9000/my-bucket/test.txt

# Сохранение заголовков в файл
curl -D headers.txt http://localhost:9000/my-bucket/test.txt
```

## Интеграция с Docker

### Dockerfile
```dockerfile
FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY . .
RUN go build -o s3proxy

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/s3proxy .
EXPOSE 9000
CMD ["./s3proxy"]
```

### Сборка и запуск
```bash
# Сборка образа
docker build -t s3proxy .

# Запуск контейнера
docker run -p 9000:9000 s3proxy

# Запуск с кастомными параметрами
docker run -p 8080:8080 s3proxy ./s3proxy -listen :8080
```

### Docker Compose
```yaml
version: '3.8'
services:
  s3proxy:
    build: .
    ports:
      - "9000:9000"
    environment:
      - LISTEN_ADDR=:9000
    volumes:
      - ./certs:/certs
    command: ["./s3proxy", "-listen", ":9000"]
```

## Производственное развертывание

### С nginx как reverse proxy
```nginx
upstream s3proxy {
    server 127.0.0.1:9000;
}

server {
    listen 80;
    server_name s3.example.com;

    location / {
        proxy_pass http://s3proxy;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        
        # Для больших файлов
        client_max_body_size 100M;
        proxy_request_buffering off;
    }
}
```

### Systemd сервис
```ini
[Unit]
Description=S3 Proxy API Gateway
After=network.target

[Service]
Type=simple
User=s3proxy
Group=s3proxy
WorkingDirectory=/opt/s3proxy
ExecStart=/opt/s3proxy/s3proxy -listen :9000
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

### Запуск как сервис
```bash
# Копирование файлов
sudo cp s3proxy /opt/s3proxy/
sudo cp s3proxy.service /etc/systemd/system/

# Создание пользователя
sudo useradd -r -s /bin/false s3proxy
sudo chown -R s3proxy:s3proxy /opt/s3proxy

# Запуск сервиса
sudo systemctl daemon-reload
sudo systemctl enable s3proxy
sudo systemctl start s3proxy
sudo systemctl status s3proxy
```
